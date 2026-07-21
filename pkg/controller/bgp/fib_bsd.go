// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"sort"
	"strings"
)

const (
	freeBSDRoutePath    = "/sbin/route"
	freeBSDNetstatPath  = "/usr/bin/netstat"
	freeBSDIfconfigPath = "/sbin/ifconfig"
)

type freeBSDCommandRunner func(context.Context, string, ...string) ([]byte, error)

type freeBSDFIBSyncer struct {
	run             freeBSDCommandRunner
	installed       map[string]FIBRoute
	sourceKnown     map[string]bool
	retainOnMissing map[string]bool
}

func newFreeBSDFIBSyncer(run freeBSDCommandRunner) *freeBSDFIBSyncer {
	if run == nil {
		run = freeBSDRunCommand
	}
	return &freeBSDFIBSyncer{
		run:             run,
		installed:       map[string]FIBRoute{},
		sourceKnown:     map[string]bool{},
		retainOnMissing: map[string]bool{},
	}
}

func freeBSDRunCommand(ctx context.Context, path string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, path, args...).CombinedOutput()
}

func (s *freeBSDFIBSyncer) SyncBGP(ctx context.Context, routes []FIBRoute) (FIBSyncResult, error) {
	result := newFreeBSDFIBSyncResult()
	installed, err := s.kernelBGPRoutes(ctx)
	if err != nil {
		return result, err
	}
	previous := s.installed
	previousSourceKnown := s.sourceKnown
	s.sourceKnown = map[string]bool{}
	for prefix, observed := range installed {
		if known, ok := previous[prefix]; ok && previousSourceKnown[prefix] && sameFreeBSDRouteTopology(known, observed) {
			// netstat does not expose the route IFA. Preserve metadata only for
			// routes this process has already reconciled successfully.
			observed.PreferredSource = known.PreferredSource
			observed.RetainOnMissing = known.RetainOnMissing
			s.sourceKnown[prefix] = true
		}
		installed[prefix] = observed
	}
	s.installed = installed
	localAddresses, err := s.localAddresses(ctx)
	if err != nil {
		return result, err
	}
	localHostPrefixes := freeBSDLocalHostPrefixes(localAddresses)

	desired := map[string]FIBRoute{}
	for _, route := range routes {
		route = normalizeFreeBSDFIBRoute(route)
		if route.Prefix == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(route.Prefix)
		if err != nil || (!prefix.Addr().Is4() && !prefix.Addr().Is6()) {
			result.Unsupported[route.Prefix] = "GoBGPFIBRouteUnsupported"
			continue
		}
		if len(route.NextHops) == 0 {
			result.Unsupported[route.Prefix] = "GoBGPFIBRouteUnsupported"
			continue
		}
		if localHostPrefixes[route.Prefix] {
			continue
		}
		if route.PreferredSource == "" {
			route.PreferredSource = inferFreeBSDPreferredSource(route.Prefix, localAddresses)
		}
		if route.PreferredSource != "" && !freeBSDPreferredSourceIsLocal(route.PreferredSource, localAddresses) {
			result.PreferredSourceSkipped[route.Prefix] = true
			result.PreferredSourceSkippedReason[route.Prefix] = "LocalAddressMissing"
			route.PreferredSource = ""
		}
		if route.RetainOnMissing {
			s.retainOnMissing[route.Prefix] = true
		} else {
			delete(s.retainOnMissing, route.Prefix)
		}
		desired[route.Prefix] = route
	}

	keys := make([]string, 0, len(desired))
	for prefix := range desired {
		keys = append(keys, prefix)
	}
	sort.Strings(keys)
	for _, prefix := range keys {
		route := desired[prefix]
		current, found := s.installed[prefix]
		if found && s.sourceKnown[prefix] && equalFreeBSDFIBRoute(current, route) {
			// RetainOnMissing is controller metadata, not visible in netstat.
			// Keep the latest desired value even when the kernel topology and
			// preferred source already match.
			s.installed[prefix] = route
			result.Installed[prefix] = true
			if route.PreferredSource != "" {
				result.PreferredSource[prefix] = route.PreferredSource
			}
			continue
		}
		if err := s.replaceRoute(ctx, current, found, !s.sourceKnown[prefix], route); err != nil {
			return result, fmt.Errorf("replace FreeBSD BGP route %s via %v: %w", prefix, route.NextHops, err)
		}
		s.installed[prefix] = route
		s.sourceKnown[prefix] = true
		result.Installed[prefix] = true
		if route.PreferredSource != "" {
			result.PreferredSource[prefix] = route.PreferredSource
		}
	}

	stale := make([]string, 0, len(s.installed))
	for prefix := range s.installed {
		if _, ok := desired[prefix]; !ok {
			stale = append(stale, prefix)
		}
	}
	sort.Strings(stale)
	for _, prefix := range stale {
		route := s.installed[prefix]
		if route.RetainOnMissing || s.retainOnMissing[prefix] {
			result.Installed[prefix] = true
			result.Retained[prefix] = true
			result.RetainedNextHops[prefix] = append([]string(nil), route.NextHops...)
			continue
		}
		if err := s.deleteRoute(ctx, route); err != nil {
			return result, fmt.Errorf("delete stale FreeBSD BGP route %s: %w", prefix, err)
		}
		delete(s.installed, prefix)
		delete(s.sourceKnown, prefix)
	}
	return result, nil
}

func newFreeBSDFIBSyncResult() FIBSyncResult {
	return FIBSyncResult{
		Installed:                    map[string]bool{},
		Unsupported:                  map[string]string{},
		Retained:                     map[string]bool{},
		RetainedNextHops:             map[string][]string{},
		PreferredSource:              map[string]string{},
		PreferredSourceSkipped:       map[string]bool{},
		PreferredSourceSkippedReason: map[string]string{},
	}
}

func (s *freeBSDFIBSyncer) localAddresses(ctx context.Context) ([]freeBSDLocalAddress, error) {
	out, err := s.run(ctx, freeBSDIfconfigPath, "-a")
	if err != nil {
		return nil, fmt.Errorf("list FreeBSD local addresses: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return parseFreeBSDLocalAddresses(string(out)), nil
}

func freeBSDLocalHostPrefixes(addresses []freeBSDLocalAddress) map[string]bool {
	out := map[string]bool{}
	for _, local := range addresses {
		bits := 128
		if local.Address.Is4() {
			bits = 32
		}
		out[netip.PrefixFrom(local.Address, bits).String()] = true
	}
	return out
}

func inferFreeBSDPreferredSource(routePrefix string, addresses []freeBSDLocalAddress) string {
	prefix, err := netip.ParsePrefix(routePrefix)
	if err != nil || (!prefix.Addr().Is4() && !prefix.Addr().Is6()) {
		return ""
	}
	var best freeBSDLocalAddress
	bestBits := -1
	for _, local := range addresses {
		if local.Address.Is4() != prefix.Addr().Is4() || local.Address == prefix.Addr() || !local.Prefix.Contains(prefix.Addr()) {
			continue
		}
		if bits := local.Prefix.Bits(); bits > bestBits {
			best = local
			bestBits = bits
		}
	}
	if bestBits < 0 {
		return ""
	}
	return best.Address.String()
}

func freeBSDPreferredSourceIsLocal(value string, addresses []freeBSDLocalAddress) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || (!addr.Is4() && !addr.Is6()) {
		return false
	}
	for _, local := range addresses {
		if local.Address == addr {
			return true
		}
	}
	return false
}

func (s *freeBSDFIBSyncer) kernelBGPRoutes(ctx context.Context) (map[string]FIBRoute, error) {
	owned := map[string]FIBRoute{}
	for _, family := range []struct {
		name string
		ipv4 bool
	}{{name: "inet", ipv4: true}, {name: "inet6", ipv4: false}} {
		out, err := s.run(ctx, freeBSDNetstatPath, "-rn", "-f", family.name)
		if err != nil {
			return nil, fmt.Errorf("list FreeBSD RTF_PROTO1 %s routes: %w: %s", family.name, err, strings.TrimSpace(string(out)))
		}
		for prefix, route := range parseFreeBSDOwnedBGPRoutesForFamily(string(out), family.ipv4) {
			owned[prefix] = route
		}
	}
	return owned, nil
}

func (s *freeBSDFIBSyncer) replaceRoute(ctx context.Context, current FIBRoute, found, forceRecreate bool, desired FIBRoute) error {
	// route change preserves the kernel route object for the common one-hop case.
	if found && !forceRecreate && current.PreferredSource == desired.PreferredSource && len(current.NextHops) == 1 && len(desired.NextHops) == 1 {
		if err := s.runRoute(ctx, "change", desired, desired.NextHops[0]); err == nil {
			return nil
		}
	}
	if found {
		if err := s.deleteRoute(ctx, current); err != nil {
			return err
		}
	}
	for _, nextHop := range desired.NextHops {
		if err := s.runRoute(ctx, "add", desired, nextHop); err != nil {
			return err
		}
	}
	return nil
}

func (s *freeBSDFIBSyncer) deleteRoute(ctx context.Context, route FIBRoute) error {
	prefix, err := netip.ParsePrefix(route.Prefix)
	if err != nil {
		return fmt.Errorf("parse route prefix %q: %w", route.Prefix, err)
	}
	for _, nextHop := range normalizeFreeBSDNextHopsForFamily(route.NextHops, prefix.Addr().Is4()) {
		if err := s.runRoute(ctx, "delete", route, nextHop); err != nil {
			return err
		}
	}
	return nil
}

func (s *freeBSDFIBSyncer) runRoute(ctx context.Context, action string, route FIBRoute, nextHop string) error {
	args := []string{"-n", action, "-proto1", "-net", route.Prefix}
	if prefix, err := netip.ParsePrefix(route.Prefix); err == nil && prefix.Addr().Is6() {
		args = []string{"-n", action, "-inet6", "-proto1", "-net", route.Prefix}
	}
	if route.PreferredSource != "" {
		args = append(args, "-ifa", route.PreferredSource)
	}
	args = append(args, nextHop)
	out, err := s.run(ctx, freeBSDRoutePath, args...)
	if err != nil {
		return fmt.Errorf("route %s: %w: %s", action, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func normalizeFreeBSDFIBRoute(route FIBRoute) FIBRoute {
	route.Prefix = normalizeRoutePrefix(route.Prefix)
	prefix, err := netip.ParsePrefix(route.Prefix)
	if err != nil || (!prefix.Addr().Is4() && !prefix.Addr().Is6()) {
		route.Prefix = ""
		route.NextHops = nil
		route.PreferredSource = ""
		return route
	}
	wantIPv4 := prefix.Addr().Is4()
	route.NextHops = normalizeFreeBSDNextHopsForFamily(route.NextHops, wantIPv4)
	if source, err := netip.ParseAddr(strings.TrimSpace(route.PreferredSource)); err == nil && source.Zone() == "" && source.Is4() == wantIPv4 {
		route.PreferredSource = source.String()
	} else {
		route.PreferredSource = ""
	}
	return route
}

func normalizeFreeBSDNextHops(values []string) []string {
	return normalizeFreeBSDNextHopsForFamily(values, true)
}

func normalizeFreeBSDNextHopsForFamily(values []string, wantIPv4 bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		addr, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil || addr.Zone() != "" || addr.Is4() != wantIPv4 {
			continue
		}
		key := addr.String()
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func equalFreeBSDFIBRoute(a, b FIBRoute) bool {
	a = normalizeFreeBSDFIBRoute(a)
	b = normalizeFreeBSDFIBRoute(b)
	return a.Prefix == b.Prefix && a.PreferredSource == b.PreferredSource && strings.Join(a.NextHops, ",") == strings.Join(b.NextHops, ",")
}

func sameFreeBSDRouteTopology(a, b FIBRoute) bool {
	a = normalizeFreeBSDFIBRoute(a)
	b = normalizeFreeBSDFIBRoute(b)
	return a.Prefix == b.Prefix && strings.Join(a.NextHops, ",") == strings.Join(b.NextHops, ",")
}
