// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/miekg/dns"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
)

type IPAddressSetController struct {
	Router         *api.Router
	Store          Store
	DryRun         bool
	DryRunNAT      bool
	DryRunRoute    bool
	DryRunFirewall bool
	NftCommand     string
	RuntimeDir     string
}

type ipAddressSetRecord struct {
	Address string
	TTL     time.Duration
}

func (c IPAddressSetController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	now := time.Now().UTC()
	resolver := systemDNSResolver{}
	command := runOutputCommandContext
	referenced := referencedIPAddressSetTargets(c.Router)
	var reconcileErrors []error
resources:
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "IPAddressSet" {
			continue
		}
		spec, err := resource.IPAddressSetSpec()
		if err != nil {
			return err
		}
		setName := render.NftIPAddressSetName(resource.Metadata.Name)
		targets := referenced[resource.Metadata.Name]
		activeTargets := c.activeIPAddressSetTargets(targets)
		if len(targets) == 0 {
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPAddressSet", resource.Metadata.Name, map[string]any{
				"phase":      "Observed",
				"referenced": false,
			}); err != nil {
				return err
			}
			continue
		}
		current := c.Store.ObjectStatus(api.NetAPIVersion, "IPAddressSet", resource.Metadata.Name)
		cachedResult, cached := cachedIPAddressSetResult(current)
		configuredAddresses := append([]string(nil), spec.Addresses...)
		configuredNames := append([]string(nil), spec.Names...)
		for i := range configuredNames {
			configuredNames[i] = strings.TrimSpace(configuredNames[i])
		}
		sort.Strings(configuredAddresses)
		sort.Strings(configuredNames)
		_, hasConfiguredAddresses := current["configuredAddresses"]
		cached = cached && hasConfiguredAddresses && !ipAddressSetRefreshDue(current, now) &&
			slices.Equal(configuredAddresses, statusStringSlice(current["configuredAddresses"])) &&
			slices.Equal(configuredNames, cachedResult.Names) && current["refreshInterval"] == spec.RefreshInterval
		existingTargets := map[string]ipAddressSetTarget{}
		existingOutputs := map[string]string{}
		if len(activeTargets) > 0 {
			nft := firstNonEmpty(c.NftCommand, "nft")
			var observationErrors []error
			for _, target := range activeTargets {
				exists, output, err := nftSetSnapshot(ctx, command, nft, target.TableFamily, target.Table, target.SetName)
				if err != nil {
					observationErrors = append(observationErrors, err)
					continue
				}
				if exists {
					existingTargets[target.key()] = target
					existingOutputs[target.key()] = output
				}
			}
			if len(observationErrors) > 0 {
				observeErr := errors.Join(observationErrors...)
				status := copyStatusMap(current)
				status["phase"] = "Error"
				status["reason"] = "ObserveFailed"
				status["error"] = observeErr.Error()
				status["referenced"] = true
				status["setName"] = setName
				status["dryRun"] = false
				if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPAddressSet", resource.Metadata.Name, status); err != nil {
					reconcileErrors = append(reconcileErrors, err)
				}
				reconcileErrors = append(reconcileErrors, fmt.Errorf("%s: %w", resource.ID(), observeErr))
				continue
			}
		}
		if len(activeTargets) > 0 && len(existingTargets) > 0 && cached && current["phase"] == "Applied" {
			if ipAddressSetTargetsCurrent(cachedResult, activeTargets, existingOutputs) {
				continue
			}
		}
		result := cachedResult
		if !cached {
			result, err = resolveIPAddressSet(ctx, resolver, spec)
		}
		if err != nil {
			if saveErr := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPAddressSet", resource.Metadata.Name, map[string]any{
				"phase":      "Pending",
				"reason":     "ResolveFailed",
				"error":      err.Error(),
				"referenced": true,
				"setName":    setName,
				"dryRun":     len(activeTargets) == 0,
			}); saveErr != nil {
				return errors.Join(append(reconcileErrors, saveErr)...)
			}
			continue
		}
		nextRefresh := now.Add(nextIPAddressSetRefresh(result.MinTTL, spec.RefreshInterval))
		resolvedAt := any(now.Format(time.RFC3339Nano))
		minTTLSeconds := any(int(result.MinTTL.Seconds()))
		if cached {
			nextRefresh, _ = time.Parse(time.RFC3339Nano, current["nextRefreshAt"].(string))
			resolvedAt = current["resolvedAt"]
			minTTLSeconds = current["minTTLSeconds"]
		}
		if len(activeTargets) > 0 {
			missingTargets := missingIPAddressSetTargets(result, activeTargets, existingTargets)
			if len(missingTargets) > 0 {
				if saveErr := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPAddressSet", resource.Metadata.Name, map[string]any{
					"phase":               "Pending",
					"reason":              "SetMissing",
					"message":             "nftables set is not present yet; renderer must create it before runtime refresh",
					"referenced":          true,
					"setName":             setName,
					"addresses":           result.Addresses,
					"configuredAddresses": configuredAddresses,
					"refreshInterval":     spec.RefreshInterval,
					"ipv4Addresses":       result.IPv4Addresses,
					"ipv6Addresses":       result.IPv6Addresses,
					"missingTargets":      missingTargets,
					"names":               result.Names,
					"resolveErrors":       result.Errors,
					"nextRefreshAt":       nextRefresh.Format(time.RFC3339Nano),
					"resolvedAt":          resolvedAt,
					"minTTLSeconds":       minTTLSeconds,
					"dryRun":              false,
				}); saveErr != nil {
					return errors.Join(append(reconcileErrors, saveErr)...)
				}
				continue
			}
			for _, target := range sortedIPAddressSetTargets(existingTargets) {
				if err := c.applyNftSet(ctx, command, target.TableFamily, target.Table, target.SetName, result.addressesForFamily(target.AddressFamily), existingOutputs[target.key()]); err != nil {
					reconcileErrors = append(reconcileErrors, fmt.Errorf("%s target %s: %w", resource.ID(), target.String(), err))
					if saveErr := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPAddressSet", resource.Metadata.Name, map[string]any{
						"phase":               "Error",
						"reason":              "ApplyFailed",
						"error":               err.Error(),
						"referenced":          true,
						"setName":             setName,
						"target":              target.String(),
						"addresses":           result.Addresses,
						"configuredAddresses": configuredAddresses,
						"ipv4Addresses":       result.IPv4Addresses,
						"ipv6Addresses":       result.IPv6Addresses,
						"names":               result.Names,
						"resolveErrors":       result.Errors,
						"refreshInterval":     spec.RefreshInterval,
						"nextRefreshAt":       nextRefresh.Format(time.RFC3339Nano),
						"resolvedAt":          resolvedAt,
						"minTTLSeconds":       minTTLSeconds,
						"dryRun":              false,
					}); saveErr != nil {
						return errors.Join(append(reconcileErrors, saveErr)...)
					}
					continue resources
				}
			}
			if len(existingTargets) == 0 {
				if saveErr := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPAddressSet", resource.Metadata.Name, map[string]any{
					"phase":               "Error",
					"reason":              "ApplyFailed",
					"error":               "no nftables address set is present",
					"referenced":          true,
					"setName":             setName,
					"addresses":           result.Addresses,
					"configuredAddresses": configuredAddresses,
					"ipv4Addresses":       result.IPv4Addresses,
					"ipv6Addresses":       result.IPv6Addresses,
					"names":               result.Names,
					"resolveErrors":       result.Errors,
					"refreshInterval":     spec.RefreshInterval,
					"nextRefreshAt":       nextRefresh.Format(time.RFC3339Nano),
					"resolvedAt":          resolvedAt,
					"minTTLSeconds":       minTTLSeconds,
					"dryRun":              false,
				}); saveErr != nil {
					return errors.Join(append(reconcileErrors, saveErr)...)
				}
				continue
			}
		}
		status := map[string]any{
			"phase":               "Applied",
			"referenced":          true,
			"setName":             setName,
			"addresses":           result.Addresses,
			"configuredAddresses": configuredAddresses,
			"ipv4Addresses":       result.IPv4Addresses,
			"ipv6Addresses":       result.IPv6Addresses,
			"targets":             appliedIPAddressSetTargets(existingTargets, targets, len(activeTargets) == 0),
			"dryRunTargets":       dryRunIPAddressSetTargets(targets, c),
			"names":               result.Names,
			"minTTLSeconds":       minTTLSeconds,
			"nextRefreshAt":       nextRefresh.Format(time.RFC3339Nano),
			"refreshInterval":     spec.RefreshInterval,
			"resolveErrors":       result.Errors,
			"resolvedAt":          resolvedAt,
			"dryRun":              len(activeTargets) == 0,
			"runtimeController":   len(activeTargets) > 0,
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPAddressSet", resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	return errors.Join(reconcileErrors...)
}

type ipAddressSetResolveResult struct {
	Addresses     []string
	IPv4Addresses []string
	IPv6Addresses []string
	Names         []string
	MinTTL        time.Duration
	Errors        []string
}

func (r ipAddressSetResolveResult) addressesForFamily(family string) []string {
	switch family {
	case "ip":
		return r.IPv4Addresses
	case "ip6":
		return r.IPv6Addresses
	default:
		return nil
	}
}

func resolveIPAddressSet(ctx context.Context, resolver systemDNSResolver, spec api.IPAddressSetSpec) (ipAddressSetResolveResult, error) {
	seen := map[string]bool{}
	var addresses []string
	var ipv4Addresses []string
	var ipv6Addresses []string
	for _, value := range spec.Addresses {
		addr, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil {
			return ipAddressSetResolveResult{}, fmt.Errorf("invalid IP address %q", value)
		}
		addr = addr.Unmap()
		key := addr.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		addresses = append(addresses, key)
		if addr.Is4() {
			ipv4Addresses = append(ipv4Addresses, key)
		} else if addr.Is6() {
			ipv6Addresses = append(ipv6Addresses, key)
		}
	}
	var names []string
	var minTTL time.Duration
	var errors []string
	for _, name := range spec.Names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		names = append(names, name)
		records, err := resolver.ResolveIP(ctx, name)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		for _, record := range records {
			addr, err := netip.ParseAddr(record.Address)
			if err != nil {
				continue
			}
			addr = addr.Unmap()
			key := addr.String()
			if !seen[key] {
				seen[key] = true
				addresses = append(addresses, key)
				if addr.Is4() {
					ipv4Addresses = append(ipv4Addresses, key)
				} else if addr.Is6() {
					ipv6Addresses = append(ipv6Addresses, key)
				}
			}
			if record.TTL > 0 && (minTTL == 0 || record.TTL < minTTL) {
				minTTL = record.TTL
			}
		}
	}
	if len(addresses) == 0 {
		if len(errors) > 0 {
			return ipAddressSetResolveResult{}, fmt.Errorf("no IP addresses resolved: %s", strings.Join(errors, "; "))
		}
		return ipAddressSetResolveResult{}, fmt.Errorf("no IP addresses configured or resolved")
	}
	if minTTL == 0 {
		minTTL = 5 * time.Minute
	}
	sort.Strings(addresses)
	sort.Strings(ipv4Addresses)
	sort.Strings(ipv6Addresses)
	sort.Strings(names)
	sort.Strings(errors)
	return ipAddressSetResolveResult{Addresses: addresses, IPv4Addresses: ipv4Addresses, IPv6Addresses: ipv6Addresses, Names: names, MinTTL: minTTL, Errors: errors}, nil
}

func nextIPAddressSetRefresh(minTTL time.Duration, refreshInterval string) time.Duration {
	if minTTL <= 0 {
		minTTL = 5 * time.Minute
	}
	next := minTTL / 2
	if next < time.Minute {
		next = time.Minute
	}
	if refreshInterval != "" {
		if configured, err := time.ParseDuration(refreshInterval); err == nil && configured > 0 && configured < next {
			next = configured
		}
	}
	return next
}

func ipAddressSetRefreshDue(status map[string]any, now time.Time) bool {
	if len(status) == 0 {
		return true
	}
	next, ok := status["nextRefreshAt"].(string)
	if !ok || strings.TrimSpace(next) == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339Nano, next)
	if err != nil {
		return true
	}
	return !now.Before(t)
}

func (c IPAddressSetController) applyNftSet(ctx context.Context, command outputCommandFunc, family, table, setName string, addresses []string, currentOutput string) error {
	additions, removals := ipAddressSetElementDiff(currentOutput, addresses)
	if len(additions) == 0 && len(removals) == 0 {
		return nil
	}
	dir := firstNonEmpty(c.RuntimeDir, os.TempDir())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".routerd-ip-address-set-*.nft")
	if err != nil {
		return err
	}
	path := file.Name()
	defer os.Remove(path)
	var b strings.Builder
	if len(removals) > 0 {
		b.WriteString("delete element " + family + " " + table + " " + setName + " { " + strings.Join(removals, ", ") + " }\n")
	}
	if len(additions) > 0 {
		b.WriteString("add element " + family + " " + table + " " + setName + " { " + strings.Join(additions, ", ") + " }\n")
	}
	if _, err := file.WriteString(b.String()); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	nft := firstNonEmpty(c.NftCommand, "nft")
	if out, err := command(ctx, nft, "-f", path); err != nil {
		return fmt.Errorf("%s -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ipAddressSetElementDiff(currentOutput string, desired []string) (additions, removals []string) {
	current := ipAddressSetElements(currentOutput)
	wanted := map[string]bool{}
	for _, address := range desired {
		address = strings.TrimSpace(address)
		if address == "" {
			continue
		}
		wanted[address] = true
		if !current[address] {
			additions = append(additions, address)
		}
	}
	for address := range current {
		if !wanted[address] {
			removals = append(removals, address)
		}
	}
	sort.Strings(additions)
	sort.Strings(removals)
	return additions, removals
}

func ipAddressSetElements(output string) map[string]bool {
	out := map[string]bool{}
	for _, token := range strings.FieldsFunc(output, func(r rune) bool {
		switch r {
		case ' ', '\n', '\t', ',', '{', '}', ';':
			return true
		default:
			return false
		}
	}) {
		token = strings.Trim(token, "\"'")
		addr, err := netip.ParseAddr(token)
		if err != nil {
			continue
		}
		out[addr.Unmap().String()] = true
	}
	return out
}

func nftSetSnapshot(ctx context.Context, command outputCommandFunc, nft, family, table, setName string) (bool, string, error) {
	out, err := command(ctx, nft, "list", "set", family, table, setName)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && nftListMissing(out, err) {
			return false, "", nil
		}
		return false, "", fmt.Errorf("%s list set %s %s %s: %w: %s", nft, family, table, setName, err, strings.TrimSpace(string(out)))
	}
	return true, string(out), nil
}

type ipAddressSetTarget struct {
	TableFamily   string
	AddressFamily string
	Table         string
	SetName       string
	Controller    string
}

func (t ipAddressSetTarget) key() string {
	return t.TableFamily + "/" + t.Table + "/" + t.SetName
}

func (t ipAddressSetTarget) String() string {
	return t.key()
}

func missingIPAddressSetTargets(result ipAddressSetResolveResult, targets []ipAddressSetTarget, existing map[string]ipAddressSetTarget) []string {
	var missing []string
	for _, target := range targets {
		if len(result.addressesForFamily(target.AddressFamily)) == 0 {
			continue
		}
		if _, ok := existing[target.key()]; !ok {
			missing = append(missing, target.String())
		}
	}
	sort.Strings(missing)
	return missing
}

func appliedIPAddressSetTargets(existing map[string]ipAddressSetTarget, desired []ipAddressSetTarget, dryRun bool) []string {
	if dryRun {
		out := make([]string, 0, len(desired))
		for _, target := range desired {
			out = append(out, target.String())
		}
		sort.Strings(out)
		return out
	}
	targets := sortedIPAddressSetTargets(existing)
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		out = append(out, target.String())
	}
	return out
}

func dryRunIPAddressSetTargets(targets []ipAddressSetTarget, c IPAddressSetController) []string {
	var out []string
	for _, target := range targets {
		if c.targetDryRun(target) {
			out = append(out, target.String())
		}
	}
	sort.Strings(out)
	return out
}

func (c IPAddressSetController) activeIPAddressSetTargets(targets []ipAddressSetTarget) []ipAddressSetTarget {
	var out []ipAddressSetTarget
	for _, target := range targets {
		if !c.targetDryRun(target) {
			out = append(out, target)
		}
	}
	return out
}

func (c IPAddressSetController) targetDryRun(target ipAddressSetTarget) bool {
	if c.DryRun {
		return true
	}
	switch target.Controller {
	case "nat":
		return c.DryRunNAT
	case "route":
		return c.DryRunRoute
	case "firewall":
		return c.DryRunFirewall
	default:
		return false
	}
}

func cachedIPAddressSetResult(status map[string]any) (ipAddressSetResolveResult, bool) {
	result := ipAddressSetResolveResult{
		Addresses:     statusStringSlice(status["addresses"]),
		IPv4Addresses: statusStringSlice(status["ipv4Addresses"]),
		IPv6Addresses: statusStringSlice(status["ipv6Addresses"]),
		Names:         statusStringSlice(status["names"]),
		Errors:        statusStringSlice(status["resolveErrors"]),
	}
	if len(result.Addresses) == 0 && len(result.IPv4Addresses) == 0 && len(result.IPv6Addresses) == 0 {
		return ipAddressSetResolveResult{}, false
	}
	return result, true
}

func statusStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		out := append([]string(nil), typed...)
		sort.Strings(out)
		return out
	case []any:
		var out []string
		for _, item := range typed {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		sort.Strings(out)
		return out
	default:
		return nil
	}
}

func ipAddressSetTargetsCurrent(cached ipAddressSetResolveResult, targets []ipAddressSetTarget, outputs map[string]string) bool {
	for _, target := range targets {
		addresses := cached.addressesForFamily(target.AddressFamily)
		output, ok := outputs[target.key()]
		if !ok {
			if len(addresses) > 0 {
				return false
			}
			continue
		}
		additions, removals := ipAddressSetElementDiff(output, addresses)
		if len(additions) > 0 || len(removals) > 0 {
			return false
		}
	}
	return true
}

func sortedIPAddressSetTargets(targets map[string]ipAddressSetTarget) []ipAddressSetTarget {
	out := make([]ipAddressSetTarget, 0, len(targets))
	for _, target := range targets {
		out = append(out, target)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key() < out[j].key() })
	return out
}

func referencedIPAddressSetTargets(router *api.Router) map[string][]ipAddressSetTarget {
	out := map[string][]ipAddressSetTarget{}
	if router == nil {
		return out
	}
	add := func(ref string, targets ...ipAddressSetTarget) {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return
		}
		name := ref
		if kind, rest, ok := strings.Cut(ref, "/"); ok && kind == "IPAddressSet" {
			name = rest
		}
		for i := range targets {
			if targets[i].SetName == "" {
				targets[i].SetName = render.NftIPAddressSetName(name)
			}
		}
		seen := map[string]bool{}
		for _, existing := range out[name] {
			seen[existing.key()] = true
		}
		for _, target := range targets {
			if seen[target.key()] {
				continue
			}
			out[name] = append(out[name], target)
			seen[target.key()] = true
		}
		sort.Slice(out[name], func(i, j int) bool { return out[name][i].key() < out[name][j].key() })
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "LocalServiceRedirect":
			if resource.APIVersion != api.FirewallAPIVersion {
				continue
			}
			spec, err := resource.LocalServiceRedirectSpec()
			if err != nil {
				continue
			}
			for _, rule := range spec.Rules {
				add(rule.DestinationSetRef,
					ipAddressSetTarget{TableFamily: "ip", AddressFamily: "ip", Table: "routerd_nat", Controller: "nat"},
					ipAddressSetTarget{TableFamily: "ip6", AddressFamily: "ip6", Table: "routerd_nat", Controller: "nat"},
				)
			}
		case "NAT44Rule":
			if resource.APIVersion != api.NetAPIVersion {
				continue
			}
			spec, err := resource.NAT44RuleSpec()
			if err != nil {
				continue
			}
			for _, ref := range append(append([]string{}, spec.DestinationSetRefs...), spec.ExcludeDestinationSetRefs...) {
				add(ref, ipAddressSetTarget{TableFamily: "ip", AddressFamily: "ip", Table: "routerd_nat", Controller: "nat"})
			}
		case "NAT44FlowDNATPinhole":
			if resource.APIVersion != api.NetAPIVersion {
				continue
			}
			spec, err := resource.NAT44FlowDNATPinholeSpec()
			if err != nil {
				continue
			}
			add(spec.Outbound.SourceSetRef, ipAddressSetTarget{TableFamily: "ip", AddressFamily: "ip", Table: "routerd_nat", Controller: "nat"})
			for _, ref := range spec.Outbound.DestinationSetRefs {
				add(ref, ipAddressSetTarget{TableFamily: "ip", AddressFamily: "ip", Table: "routerd_nat", Controller: "nat"})
			}
		case "EgressRoutePolicy":
			if resource.APIVersion != api.NetAPIVersion {
				continue
			}
			spec, err := resource.EgressRoutePolicySpec()
			if err != nil {
				continue
			}
			for _, ref := range append(append([]string{}, spec.DestinationSetRefs...), spec.ExcludeDestinationSetRefs...) {
				add(ref, ipAddressSetTarget{TableFamily: "ip", AddressFamily: "ip", Table: "routerd_policy", Controller: "route"})
			}
		case "FirewallRule":
			if resource.APIVersion != api.FirewallAPIVersion {
				continue
			}
			spec, err := resource.FirewallRuleSpec()
			if err != nil {
				continue
			}
			for _, ref := range append(append([]string{}, spec.DestinationSetRefs...), spec.ExcludeDestinationSetRefs...) {
				name := strings.TrimSpace(ref)
				if kind, rest, ok := strings.Cut(name, "/"); ok && kind == "IPAddressSet" {
					name = rest
				}
				if name == "" {
					continue
				}
				add(ref,
					ipAddressSetTarget{TableFamily: "inet", AddressFamily: "ip", Table: "routerd_filter", SetName: render.NftFirewallIPAddressSetName(name, "ip"), Controller: "firewall"},
					ipAddressSetTarget{TableFamily: "inet", AddressFamily: "ip6", Table: "routerd_filter", SetName: render.NftFirewallIPAddressSetName(name, "ip6"), Controller: "firewall"},
				)
			}
		case "FirewallFlowPinhole":
			if resource.APIVersion != api.FirewallAPIVersion {
				continue
			}
			spec, err := resource.FirewallFlowPinholeSpec()
			if err != nil {
				continue
			}
			refs := append([]string{}, spec.Outbound.DestinationSetRefs...)
			if spec.Outbound.SourceSetRef != "" {
				refs = append(refs, spec.Outbound.SourceSetRef)
			}
			for _, ref := range refs {
				name := strings.TrimSpace(ref)
				if kind, rest, ok := strings.Cut(name, "/"); ok && kind == "IPAddressSet" {
					name = rest
				}
				if name == "" {
					continue
				}
				add(ref, ipAddressSetTarget{TableFamily: "inet", AddressFamily: "ip", Table: "routerd_filter", SetName: render.NftFirewallIPAddressSetName(name, "ip"), Controller: "firewall"})
			}
		}
	}
	return out
}

type systemDNSResolver struct{}

func (systemDNSResolver) ResolveIP(ctx context.Context, name string) ([]ipAddressSetRecord, error) {
	cfg, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		return nil, fmt.Errorf("read system DNS configuration: %w", err)
	}
	if len(cfg.Servers) == 0 {
		return nil, errors.New("system DNS configuration has no nameservers")
	}
	var records []ipAddressSetRecord
	var errs []string
	for _, qtype := range []uint16{dns.TypeA, dns.TypeAAAA} {
		resolved, err := resolveSystemDNSRecords(ctx, cfg, name, qtype)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		records = append(records, resolved...)
	}
	if len(records) > 0 {
		return records, nil
	}
	return nil, fmt.Errorf("resolve %s: %s", name, strings.Join(errs, "; "))
}

func resolveSystemDNSRecords(ctx context.Context, cfg *dns.ClientConfig, name string, qtype uint16) ([]ipAddressSetRecord, error) {
	queryName := dns.Fqdn(name)
	msg := new(dns.Msg)
	msg.SetQuestion(queryName, qtype)
	client := &dns.Client{Net: "udp", Timeout: 2 * time.Second}
	var errs []string
	for _, server := range cfg.Servers {
		addr := net.JoinHostPort(server, firstNonEmpty(cfg.Port, "53"))
		resp, _, err := client.ExchangeContext(ctx, msg, addr)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", addr, err))
			continue
		}
		if resp == nil {
			errs = append(errs, fmt.Sprintf("%s: empty response", addr))
			continue
		}
		if resp.Rcode != dns.RcodeSuccess {
			errs = append(errs, fmt.Sprintf("%s: rcode %s", addr, dns.RcodeToString[resp.Rcode]))
			continue
		}
		var out []ipAddressSetRecord
		for _, rr := range resp.Answer {
			var addr netip.Addr
			var ttl uint32
			switch record := rr.(type) {
			case *dns.A:
				if qtype != dns.TypeA {
					continue
				}
				var ok bool
				addr, ok = netip.AddrFromSlice(record.A)
				if !ok {
					continue
				}
				addr = addr.Unmap()
				ttl = record.Hdr.Ttl
			case *dns.AAAA:
				if qtype != dns.TypeAAAA {
					continue
				}
				var ok bool
				addr, ok = netip.AddrFromSlice(record.AAAA)
				if !ok {
					continue
				}
				ttl = record.Hdr.Ttl
			default:
				continue
			}
			out = append(out, ipAddressSetRecord{Address: addr.String(), TTL: time.Duration(ttl) * time.Second})
		}
		if len(out) > 0 {
			return out, nil
		}
		errs = append(errs, fmt.Sprintf("%s: no %s records", addr, dns.TypeToString[qtype]))
	}
	return nil, fmt.Errorf("%s %s: %s", name, dns.TypeToString[qtype], strings.Join(errs, "; "))
}
