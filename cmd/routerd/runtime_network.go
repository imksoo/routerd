// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resource"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func applyIPv6DelegatedAddressesWithState(router *api.Router, store routerstate.Store) ([]string, error) {
	aliases := map[string]string{}
	pdPrefixes := map[string]string{}
	pdResources := map[string]bool{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Interface":
			spec, err := res.InterfaceSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = spec.IfName
		case "DHCPv6PrefixDelegation":
			pdResources[res.Metadata.Name] = true
			if store == nil {
				continue
			}
			base := "ipv6PrefixDelegation." + res.Metadata.Name
			lease, _ := routerstate.PDLeaseFromStore(store, base)
			if lease.CurrentPrefix != "" {
				pdPrefixes[res.Metadata.Name] = lease.CurrentPrefix
			}
		}
	}
	var applied []string
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv6DelegatedAddress" {
			continue
		}
		spec, err := res.IPv6DelegatedAddressSpec()
		if err != nil {
			return nil, err
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			return nil, fmt.Errorf("%s references interface with empty ifname", res.ID())
		}
		var address string
		if store != nil && pdResources[spec.PrefixDelegation] {
			prefix := pdPrefixes[spec.PrefixDelegation]
			if prefix == "" {
				applied = append(applied, "skipped-unavailable:"+ifname)
				continue
			}
			var err error
			address, err = deriveIPv6AddressFromDelegatedPrefix(prefix, spec.SubnetID, spec.AddressSuffix)
			if err != nil {
				return nil, fmt.Errorf("%s derive delegated address from state: %w", res.ID(), err)
			}
		} else {
			var err error
			address, err = deriveIPv6AddressFromInterface(ifname, spec.AddressSuffix)
			if err != nil {
				if errors.Is(err, errNoIPv6PrefixAvailable) {
					applied = append(applied, "skipped-unavailable:"+ifname)
					continue
				}
				return nil, fmt.Errorf("%s derive delegated address: %w", res.ID(), err)
			}
		}
		removed, err := cleanupConflictingIPv6SuffixAddresses(ifname, address, spec.AddressSuffix)
		if err != nil {
			return nil, fmt.Errorf("%s cleanup stale delegated address: %w", res.ID(), err)
		}
		applied = append(applied, removed...)
		ensured, err := ensureIPv6LocalAddress(ifname, address)
		if err != nil {
			return nil, fmt.Errorf("%s ensure delegated address: %w", res.ID(), err)
		}
		if ensured {
			applied = append(applied, ifname+":"+address)
		}
	}
	return applied, nil
}

func cleanupConflictingIPv6SuffixAddresses(ifname, desiredAddress, suffix string) ([]string, error) {
	suffixAddr, err := netip.ParseAddr(defaultString(suffix, "::"))
	if err != nil || !suffixAddr.Is6() {
		return nil, fmt.Errorf("invalid IPv6 suffix %q", suffix)
	}
	var removed []string
	for _, entry := range conflictingManagedIPv6Addresses(ipv6AddressEntries(ifname), desiredAddress, ipv6HostSuffix64(suffixAddr)) {
		if err := deleteIPv6LocalAddress(ifname, entry.Address, entry.PrefixLen); err != nil {
			return removed, err
		}
		removed = append(removed, ifname+":"+entry.Address)
	}
	return removed, nil
}

func conflictingManagedIPv6Addresses(entries []ipv6AddressEntry, desiredAddress string, suffix uint64) []ipv6AddressEntry {
	var out []ipv6AddressEntry
	for _, entry := range entries {
		addr, err := netip.ParseAddr(entry.Address)
		if err != nil || !addr.Is6() || addr.IsLinkLocalUnicast() {
			continue
		}
		if addr.String() == desiredAddress {
			continue
		}
		if ipv6HostSuffix64(addr) != suffix {
			continue
		}
		out = append(out, entry)
	}
	return out
}

type delegatedIPv6Targets struct {
	DesiredByInterface  map[string]map[string]bool
	SuffixesByInterface map[string]map[uint64]bool
}

func managedDelegatedIPv6Targets(router *api.Router, store routerstate.Store) (delegatedIPv6Targets, error) {
	targets := delegatedIPv6Targets{
		DesiredByInterface:  map[string]map[string]bool{},
		SuffixesByInterface: map[string]map[uint64]bool{},
	}
	if store == nil {
		return targets, nil
	}
	aliases := map[string]string{}
	pdPrefixes := map[string]string{}
	delegated := map[string]api.IPv6DelegatedAddressSpec{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Interface":
			spec, err := res.InterfaceSpec()
			if err != nil {
				return targets, err
			}
			aliases[res.Metadata.Name] = spec.IfName
		case "DHCPv6PrefixDelegation":
			base := "ipv6PrefixDelegation." + res.Metadata.Name
			lease, _ := routerstate.PDLeaseFromStore(store, base)
			if lease.CurrentPrefix != "" {
				pdPrefixes[res.Metadata.Name] = lease.CurrentPrefix
			}
		case "IPv6DelegatedAddress":
			spec, err := res.IPv6DelegatedAddressSpec()
			if err != nil {
				return targets, err
			}
			delegated[res.Metadata.Name] = spec
			if err := addManagedDelegatedIPv6Target(targets, aliases[spec.Interface], pdPrefixes[spec.PrefixDelegation], spec.SubnetID, spec.AddressSuffix); err != nil {
				return targets, fmt.Errorf("%s target: %w", res.ID(), err)
			}
		}
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "DSLiteTunnel" {
			continue
		}
		spec, err := res.DSLiteTunnelSpec()
		if err != nil {
			return targets, err
		}
		if !api.BoolDefault(spec.Enabled, true) {
			continue
		}
		if defaultString(spec.LocalAddressSource, "interface") != "delegatedAddress" {
			continue
		}
		delegatedSpec, ok := delegated[spec.LocalDelegatedAddress]
		if !ok {
			continue
		}
		suffix := defaultString(spec.LocalAddressSuffix, delegatedSpec.AddressSuffix)
		if err := addManagedDelegatedIPv6Target(targets, aliases[delegatedSpec.Interface], pdPrefixes[delegatedSpec.PrefixDelegation], delegatedSpec.SubnetID, suffix); err != nil {
			return targets, fmt.Errorf("%s target: %w", res.ID(), err)
		}
	}
	return targets, nil
}

func addManagedDelegatedIPv6Target(targets delegatedIPv6Targets, ifname, prefix, subnetID, suffix string) error {
	if ifname == "" {
		return nil
	}
	suffixAddr, err := netip.ParseAddr(suffix)
	if err != nil || !suffixAddr.Is6() {
		return fmt.Errorf("invalid IPv6 suffix %q", suffix)
	}
	if targets.SuffixesByInterface[ifname] == nil {
		targets.SuffixesByInterface[ifname] = map[uint64]bool{}
	}
	targets.SuffixesByInterface[ifname][ipv6HostSuffix64(suffixAddr)] = true
	if prefix == "" {
		return nil
	}
	address, err := deriveIPv6AddressFromDelegatedPrefix(prefix, subnetID, suffix)
	if err != nil {
		return err
	}
	if targets.DesiredByInterface[ifname] == nil {
		targets.DesiredByInterface[ifname] = map[string]bool{}
	}
	targets.DesiredByInterface[ifname][address] = true
	return nil
}

func cleanupStaleDelegatedIPv6Addresses(router *api.Router, store routerstate.Store) ([]string, error) {
	targets, err := managedDelegatedIPv6Targets(router, store)
	if err != nil {
		return nil, err
	}
	var removed []string
	for ifname, suffixes := range targets.SuffixesByInterface {
		desired := targets.DesiredByInterface[ifname]
		for _, entry := range ipv6AddressEntries(ifname) {
			addr, err := netip.ParseAddr(entry.Address)
			if err != nil || !addr.Is6() || addr.IsLinkLocalUnicast() {
				continue
			}
			if desired[addr.String()] {
				continue
			}
			if !suffixes[ipv6HostSuffix64(addr)] {
				continue
			}
			if err := deleteIPv6LocalAddress(ifname, entry.Address, entry.PrefixLen); err != nil {
				return removed, err
			}
			removed = append(removed, ifname+":"+entry.Address)
		}
	}
	return removed, nil
}

func applyIPv4PolicyRoutes(router *api.Router) ([]string, error) {
	aliases := map[string]string{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Interface":
			spec, err := res.InterfaceSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = spec.IfName
		case "PPPoESession":
			spec, err := res.PPPoESessionSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = defaultString(spec.IfName, "ppp-"+res.Metadata.Name)
		case "DSLiteTunnel":
			spec, err := res.DSLiteTunnelSpec()
			if err != nil {
				return nil, err
			}
			if !api.BoolDefault(spec.Enabled, true) {
				continue
			}
			aliases[res.Metadata.Name] = defaultString(spec.TunnelName, res.Metadata.Name)
		}
	}
	var applied []string
	for _, res := range router.Spec.Resources {
		if res.Kind != "EgressRoutePolicy" {
			continue
		}
		spec, err := res.EgressRoutePolicySpec()
		if err != nil {
			return nil, err
		}
		mode := defaultString(spec.Mode, "")
		if mode != "mark" && mode != "hash" {
			continue
		}
		for i, candidate := range spec.Candidates {
			if len(candidate.Targets) > 0 {
				for j, target := range candidate.Targets {
					targetName := target.Name
					if targetName == "" {
						targetName = fmt.Sprintf("%s-%d-%d", res.Metadata.Name, i, j)
					}
					target.Name = targetName
					label, err := applyIPv4PolicyRouteTarget(res.ID(), aliases, target, true)
					if err != nil {
						return nil, err
					}
					if label == "" {
						continue
					}
					applied = append(applied, label)
				}
				continue
			}
			if candidate.Mark == 0 {
				continue
			}
			target := egressRouteTargetFromCandidate(candidate)
			if target.Name == "" {
				target.Name = defaultString(candidate.Name, res.Metadata.Name)
			}
			label, err := applyIPv4PolicyRouteTarget(res.ID(), aliases, target, false)
			if err != nil {
				return nil, err
			}
			applied = append(applied, label)
		}
	}
	return applied, nil
}

func applyIPv4DefaultRoutePolicies(router *api.Router) ([]string, error) {
	aliases, err := outboundAliases(router)
	if err != nil {
		return nil, err
	}
	healthChecks, err := evaluateHealthChecks(router, aliases)
	if err != nil {
		return nil, err
	}
	var applied []string
	for _, res := range router.Spec.Resources {
		if res.Kind != "EgressRoutePolicy" {
			continue
		}
		spec, err := res.EgressRoutePolicySpec()
		if err != nil {
			return nil, err
		}
		if defaultString(spec.Mode, "") != "priority" {
			continue
		}
		available := availableIPv4DefaultRouteCandidates(effectiveRouterAvailability{Router: router, Aliases: aliases, Health: healthChecks, LinkExists: linkExists}, spec.Candidates)
		candidate, ok := selectIPv4DefaultRouteCandidate(available, healthChecks)
		if !ok {
			return nil, fmt.Errorf("%s has no healthy IPv4 default route candidate", res.ID())
		}
		var healthy []api.EgressRoutePolicyCandidate
		for _, target := range available {
			healthy = append(healthy, target)
			if len(target.Targets) > 0 {
				continue
			}
			label, err := applyIPv4DefaultRouteCandidate(res.ID(), aliases, target)
			if err != nil {
				return nil, err
			}
			applied = append(applied, label)
		}
		if err := applyEgressRoutePolicyDefaultMarks(res.ID(), spec, candidate, healthy); err != nil {
			return nil, err
		}
		applied = append(applied, "active="+defaultRouteCandidateLabel(candidate))
	}
	return applied, nil
}

type effectiveRouterAvailability struct {
	Router     *api.Router
	Aliases    map[string]string
	Health     map[string]bool
	LinkExists func(string) bool
}

func availableIPv4DefaultRouteCandidates(ctx effectiveRouterAvailability, candidates []api.EgressRoutePolicyCandidate) []api.EgressRoutePolicyCandidate {
	var available []api.EgressRoutePolicyCandidate
	for _, candidate := range candidates {
		if !api.BoolDefault(candidate.Enabled, true) {
			continue
		}
		if candidate.HealthCheck != "" && !ctx.Health[candidate.HealthCheck] {
			continue
		}
		if len(candidate.Targets) > 0 {
			if !egressRouteCandidateHasAvailableTarget(ctx, candidate) {
				continue
			}
			available = append(available, candidate)
			continue
		}
		ifname := ctx.Aliases[candidate.EffectiveInterface()]
		if ifname == "" || !ctx.LinkExists(ifname) {
			continue
		}
		if !egressTargetUsable(ctx, candidate.EffectiveInterface()) {
			continue
		}
		available = append(available, candidate)
	}
	return available
}

func egressRouteCandidateHasAvailableTarget(ctx effectiveRouterAvailability, candidate api.EgressRoutePolicyCandidate) bool {
	for _, target := range candidate.Targets {
		outboundInterface := target.EffectiveInterface()
		ifname := ctx.Aliases[outboundInterface]
		if ifname != "" && ctx.LinkExists(ifname) && egressTargetUsable(ctx, outboundInterface) {
			return true
		}
	}
	return false
}

func egressTargetUsable(ctx effectiveRouterAvailability, name string) bool {
	for _, res := range ctx.Router.Spec.Resources {
		if res.Metadata.Name != name {
			continue
		}
		if res.Kind != "DSLiteTunnel" {
			return true
		}
		spec, err := res.DSLiteTunnelSpec()
		if err != nil {
			return false
		}
		if !api.BoolDefault(spec.Enabled, true) {
			return false
		}
		ifname := ctx.Aliases[spec.Interface]
		delegated, err := ipv6DelegatedAddressSpecs(ctx.Router)
		if err != nil {
			return false
		}
		_, _, err = dsliteLocalAddressWithPrefixes(spec, ifname, ctx.Aliases, delegated, map[string]string{})
		return err == nil
	}
	return true
}

func ipv6DelegatedAddressSpecs(router *api.Router) (map[string]api.IPv6DelegatedAddressSpec, error) {
	delegated := map[string]api.IPv6DelegatedAddressSpec{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv6DelegatedAddress" {
			continue
		}
		spec, err := res.IPv6DelegatedAddressSpec()
		if err != nil {
			return nil, err
		}
		delegated[res.Metadata.Name] = spec
	}
	return delegated, nil
}

func defaultRouteCandidateLabel(candidate api.EgressRoutePolicyCandidate) string {
	if candidate.Name != "" {
		return candidate.Name
	}
	if len(candidate.Targets) > 0 {
		return "targets"
	}
	return candidate.EffectiveInterface()
}

func outboundAliases(router *api.Router) (map[string]string, error) {
	aliases := map[string]string{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Interface":
			spec, err := res.InterfaceSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = spec.IfName
		case "PPPoESession":
			spec, err := res.PPPoESessionSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = defaultString(spec.IfName, "ppp-"+res.Metadata.Name)
		case "DSLiteTunnel":
			spec, err := res.DSLiteTunnelSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = defaultString(spec.TunnelName, res.Metadata.Name)
		}
	}
	return aliases, nil
}

func evaluateHealthChecks(router *api.Router, aliases map[string]string) (map[string]bool, error) {
	result := map[string]bool{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "HealthCheck" {
			continue
		}
		spec, err := res.HealthCheckSpec()
		if err != nil {
			return nil, err
		}
		healthy, err := runHealthCheck(router, spec, aliases)
		if err != nil {
			return nil, fmt.Errorf("%s health check: %w", res.ID(), err)
		}
		result[res.Metadata.Name] = healthy
	}
	return result, nil
}

func runHealthCheck(router *api.Router, spec api.HealthCheckSpec, aliases map[string]string) (bool, error) {
	if defaultString(spec.Type, "ping") != "ping" {
		return false, fmt.Errorf("unsupported health check type %q", spec.Type)
	}
	target, family, err := resolveHealthCheckTarget(router, spec, aliases)
	if err != nil {
		return false, nil
	}
	timeout := defaultString(spec.Timeout, "3s")
	duration, err := time.ParseDuration(timeout)
	if err != nil {
		return false, err
	}
	if duration < time.Second {
		duration = time.Second
	}
	cmdName := "ping"
	args := []string{"-c", "1", "-W", fmt.Sprintf("%d", int(duration.Seconds()))}
	if family == "ipv6" {
		cmdName = "ping"
		args = append([]string{"-6"}, args...)
	} else {
		args = append([]string{"-4"}, args...)
	}
	if spec.Interface != "" || spec.SourceInterface != "" || spec.SourceAddress != "" {
		source := healthCheckPingSource(router, spec, aliases)
		if source == "" {
			if defaultString(spec.TargetSource, "auto") == "dsliteRemote" || (spec.TargetSource == "" && healthInterfaceKind(router, spec.Interface) == "DSLiteTunnel") {
				return false, nil
			}
			return false, fmt.Errorf("missing ping source for %s", spec.Interface)
		}
		args = append(args, "-I", source)
	}
	args = append(args, target)
	ctx, cancel := context.WithTimeout(context.Background(), duration+time.Second)
	defer cancel()
	err = exec.CommandContext(ctx, cmdName, args...).Run()
	return err == nil, nil
}

func healthCheckPingSource(router *api.Router, spec api.HealthCheckSpec, aliases map[string]string) string {
	if spec.SourceAddress != "" {
		return spec.SourceAddress
	}
	if spec.SourceInterface != "" {
		return defaultString(aliases[spec.SourceInterface], spec.SourceInterface)
	}
	if defaultString(spec.TargetSource, "auto") == "dsliteRemote" || (spec.TargetSource == "" && healthInterfaceKind(router, spec.Interface) == "DSLiteTunnel") {
		for _, res := range router.Spec.Resources {
			if res.Kind != "DSLiteTunnel" || res.Metadata.Name != spec.Interface {
				continue
			}
			tunnel, err := res.DSLiteTunnelSpec()
			if err != nil {
				return ""
			}
			delegated, err := ipv6DelegatedAddressSpecs(router)
			if err != nil {
				return ""
			}
			local, _, err := dsliteLocalAddress(tunnel, aliases[tunnel.Interface], aliases, delegated)
			if err != nil {
				return ""
			}
			return local
		}
	}
	return aliases[spec.Interface]
}

func resolveHealthCheckTarget(router *api.Router, spec api.HealthCheckSpec, aliases map[string]string) (string, string, error) {
	if spec.Target != "" {
		family := spec.AddressFamily
		if family == "" {
			addr, err := netip.ParseAddr(spec.Target)
			if err != nil {
				return "", "", err
			}
			if addr.Is6() {
				family = "ipv6"
			} else {
				family = "ipv4"
			}
		}
		return spec.Target, family, nil
	}
	source := defaultString(spec.TargetSource, "auto")
	if source == "auto" {
		if healthInterfaceKind(router, spec.Interface) == "DSLiteTunnel" {
			source = "dsliteRemote"
		} else {
			source = "defaultGateway"
		}
	}
	switch source {
	case "defaultGateway":
		ifname := aliases[spec.Interface]
		if ifname == "" {
			return "", "", fmt.Errorf("missing ifname for %s", spec.Interface)
		}
		target, err := currentIPv4HealthTargetForInterface(ifname)
		if err != nil {
			return "", "", err
		}
		return target, "ipv4", nil
	case "dsliteRemote":
		target, err := dsliteRemoteAddress(router, spec.Interface)
		if err != nil {
			return "", "", err
		}
		return target, "ipv6", nil
	case "static":
		return "", "", fmt.Errorf("target is required when targetSource is static")
	default:
		return "", "", fmt.Errorf("unsupported targetSource %q", source)
	}
}

func healthInterfaceKind(router *api.Router, name string) string {
	for _, res := range router.Spec.Resources {
		if res.Metadata.Name == name {
			return res.Kind
		}
	}
	return ""
}

func dsliteRemoteAddress(router *api.Router, name string) (string, error) {
	for _, res := range router.Spec.Resources {
		if res.Kind != "DSLiteTunnel" || res.Metadata.Name != name {
			continue
		}
		spec, err := res.DSLiteTunnelSpec()
		if err != nil {
			return "", err
		}
		if spec.RemoteAddress != "" {
			return spec.RemoteAddress, nil
		}
		if spec.AFTRFQDN == "" {
			return "", fmt.Errorf("%s has no remoteAddress or aftrFQDN", res.ID())
		}
		return resolveDSLiteRemoteWithState(spec, nil)
	}
	return "", fmt.Errorf("missing DSLiteTunnel %q", name)
}

func selectIPv4DefaultRouteCandidate(candidates []api.EgressRoutePolicyCandidate, health map[string]bool) (api.EgressRoutePolicyCandidate, bool) {
	ordered := append([]api.EgressRoutePolicyCandidate{}, candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Priority < ordered[j].Priority
	})
	for _, candidate := range ordered {
		if candidate.HealthCheck != "" && !health[candidate.HealthCheck] {
			continue
		}
		return candidate, true
	}
	return api.EgressRoutePolicyCandidate{}, false
}

func applyIPv4DefaultRouteCandidate(resourceID string, aliases map[string]string, candidate api.EgressRoutePolicyCandidate) (string, error) {
	ifname := aliases[candidate.EffectiveInterface()]
	if ifname == "" {
		return "", fmt.Errorf("%s references default route interface with empty ifname", resourceID)
	}
	metric := candidate.EffectiveMetric()
	if metric == 0 {
		metric = 50
	}
	source := defaultString(candidate.GatewaySource, "none")
	args := []string{"-4", "route", "replace", "default"}
	switch source {
	case "none":
		args = append(args, "dev", ifname)
	case "static":
		args = append(args, "via", candidate.Gateway, "dev", ifname)
	case "dhcpv4":
		gateway, err := currentIPv4DefaultGatewayForInterface(ifname)
		if err != nil {
			return "", fmt.Errorf("%s DHCPv4 gateway on %s: %w", resourceID, ifname, err)
		}
		args = append(args, "via", gateway, "dev", ifname)
	default:
		return "", fmt.Errorf("unsupported gatewaySource %q", source)
	}
	table := candidate.EffectiveTable()
	args = append(args, "table", fmt.Sprintf("%d", table), "metric", fmt.Sprintf("%d", metric))
	if err := runLogged("ip", args...); err != nil {
		return "", err
	}
	if err := ensureIPv4FwmarkRule(candidate.Priority, candidate.Mark, table); err != nil {
		return "", err
	}
	name := defaultString(candidate.Name, candidate.EffectiveInterface())
	return fmt.Sprintf("%s(%s,table=%d,mark=0x%x,metric=%d)", name, ifname, table, candidate.Mark, metric), nil
}

type ipv4FwmarkRule struct {
	Priority int
	Mark     int
	Table    int
}

func cleanupIPv4ManagedFwmarkRules(router *api.Router) ([]string, error) {
	desired, err := desiredIPv4FwmarkArtifacts(router)
	if err != nil {
		return nil, err
	}
	desiredTables := map[int]bool{}
	for _, artifact := range desired {
		rule, ok := ipv4FwmarkRuleFromArtifact(artifact)
		if ok {
			desiredTables[rule.Table] = true
		}
	}
	current, err := currentIPv4FwmarkArtifacts()
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, artifact := range resource.Orphans(desired, current, managedIPv4FwmarkArtifact) {
		rule, ok := ipv4FwmarkRuleFromArtifact(artifact)
		if !ok {
			continue
		}
		if err := deleteIPv4FwmarkRule(rule); err != nil {
			return nil, err
		}
		label := fmt.Sprintf("priority=%d mark=0x%x table=%d", rule.Priority, rule.Mark, rule.Table)
		if !desiredTables[rule.Table] {
			if err := flushIPv4RouteTable(rule.Table); err != nil {
				return nil, err
			}
			label += " table=flushed"
		}
		removed = append(removed, label)
	}
	return removed, nil
}

func staleIPv4ManagedFwmarkRules(desired map[ipv4FwmarkRule]bool, current []ipv4FwmarkRule) []ipv4FwmarkRule {
	var desiredArtifacts []resource.Artifact
	for rule := range desired {
		desiredArtifacts = append(desiredArtifacts, ipv4FwmarkRuleArtifact("", rule))
	}
	var currentArtifacts []resource.Artifact
	for _, rule := range current {
		currentArtifacts = append(currentArtifacts, ipv4FwmarkRuleArtifact("", rule))
	}
	orphanArtifacts := resource.Orphans(desiredArtifacts, currentArtifacts, managedIPv4FwmarkArtifact)
	stale := make([]ipv4FwmarkRule, 0, len(orphanArtifacts))
	for _, artifact := range orphanArtifacts {
		if rule, ok := ipv4FwmarkRuleFromArtifact(artifact); ok {
			stale = append(stale, rule)
		}
	}
	return stale
}

func desiredIPv4FwmarkArtifacts(router *api.Router) ([]resource.Artifact, error) {
	var desired []resource.Artifact
	add := func(priority, mark, table int) {
		if priority == 0 || mark == 0 || table == 0 {
			return
		}
		desired = append(desired, ipv4FwmarkRuleArtifact("", ipv4FwmarkRule{Priority: priority, Mark: mark, Table: table}))
	}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "EgressRoutePolicy":
			spec, err := res.EgressRoutePolicySpec()
			if err != nil {
				return nil, err
			}
			for _, candidate := range spec.Candidates {
				if len(candidate.Targets) > 0 {
					for _, target := range candidate.Targets {
						add(target.Priority, target.Mark, target.EffectiveTable())
					}
					continue
				}
				add(candidate.Priority, candidate.Mark, candidate.EffectiveTable())
			}
		}
	}
	return desired, nil
}

func currentIPv4FwmarkArtifacts() ([]resource.Artifact, error) {
	out, err := exec.Command("ip", "-4", "rule", "show").CombinedOutput()
	if err != nil {
		return nil, err
	}
	var rules []resource.Artifact
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		rule := ipv4FwmarkRule{}
		priority, err := strconv.Atoi(strings.TrimSuffix(fields[0], ":"))
		if err != nil {
			continue
		}
		rule.Priority = priority
		for i, field := range fields {
			switch field {
			case "fwmark":
				if i+1 >= len(fields) {
					continue
				}
				mark, err := strconv.ParseInt(strings.SplitN(fields[i+1], "/", 2)[0], 0, 64)
				if err != nil {
					continue
				}
				rule.Mark = int(mark)
			case "lookup":
				if i+1 >= len(fields) {
					continue
				}
				table, err := strconv.Atoi(fields[i+1])
				if err != nil {
					continue
				}
				rule.Table = table
			}
		}
		if rule.Mark != 0 && rule.Table != 0 {
			rules = append(rules, ipv4FwmarkRuleArtifact("", rule))
		}
	}
	return rules, nil
}

func ipv4FwmarkRuleArtifact(owner string, rule ipv4FwmarkRule) resource.Artifact {
	return resource.Artifact{
		Kind:  "linux.ipv4.fwmarkRule",
		Name:  fmt.Sprintf("priority=%d,mark=0x%x,table=%d", rule.Priority, rule.Mark, rule.Table),
		Owner: owner,
		Attributes: map[string]string{
			"priority": fmt.Sprintf("%d", rule.Priority),
			"mark":     fmt.Sprintf("0x%x", rule.Mark),
			"table":    fmt.Sprintf("%d", rule.Table),
		},
	}
}

func ipv4FwmarkRuleFromArtifact(artifact resource.Artifact) (ipv4FwmarkRule, bool) {
	priority, err := strconv.Atoi(artifact.Attributes["priority"])
	if err != nil {
		return ipv4FwmarkRule{}, false
	}
	mark, err := strconv.ParseInt(artifact.Attributes["mark"], 0, 64)
	if err != nil {
		return ipv4FwmarkRule{}, false
	}
	table, err := strconv.Atoi(artifact.Attributes["table"])
	if err != nil {
		return ipv4FwmarkRule{}, false
	}
	return ipv4FwmarkRule{Priority: priority, Mark: int(mark), Table: table}, true
}

func managedIPv4FwmarkArtifact(artifact resource.Artifact) bool {
	rule, ok := ipv4FwmarkRuleFromArtifact(artifact)
	return ok && routerdManagedMark(rule.Mark)
}

func routerdManagedMark(mark int) bool {
	return mark >= 0x100 && mark <= 0x1ff
}

func deleteIPv4FwmarkRule(rule ipv4FwmarkRule) error {
	return runLogged(
		"ip", "-4", "rule", "del",
		"priority", fmt.Sprintf("%d", rule.Priority),
		"fwmark", fmt.Sprintf("0x%x", rule.Mark),
		"table", fmt.Sprintf("%d", rule.Table),
	)
}

func flushIPv4RouteTable(table int) error {
	return runLogged("ip", "-4", "route", "flush", "table", fmt.Sprintf("%d", table))
}

func applyEgressRoutePolicyDefaultMarks(resourceID string, spec api.EgressRoutePolicySpec, active api.EgressRoutePolicyCandidate, healthy []api.EgressRoutePolicyCandidate) error {
	if _, err := exec.LookPath("nft"); err != nil {
		return fmt.Errorf("nft is required for IPv4 default route policy: %w", err)
	}
	data, err := renderEgressRoutePolicyDefaultMarks(resourceID, spec, active, healthy)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepathDir(defaultRouteNftablesPath), 0755); err != nil {
		return err
	}
	changed, err := writeFileIfChanged(defaultRouteNftablesPath, data, 0644)
	if err != nil {
		return err
	}
	missing := exec.Command("nft", "list", "table", "ip", "routerd_default_route").Run() != nil
	if !changed && !missing {
		return nil
	}
	if err := runLogged("nft", "-c", "-f", defaultRouteNftablesPath); err != nil {
		return err
	}
	return runLogged("nft", "-f", defaultRouteNftablesPath)
}

func renderEgressRoutePolicyDefaultMarks(resourceID string, spec api.EgressRoutePolicySpec, active api.EgressRoutePolicyCandidate, healthy []api.EgressRoutePolicyCandidate) ([]byte, error) {
	matches, err := ipv4PolicyMatches(resourceID, spec.SourceCIDRs, spec.DestinationCIDRs)
	if err != nil {
		return nil, err
	}
	healthyMarks := make([]string, 0, len(healthy))
	for _, candidate := range healthy {
		if len(candidate.Targets) > 0 {
			for _, target := range candidate.Targets {
				healthyMarks = append(healthyMarks, fmt.Sprintf("0x%x", target.Mark))
			}
			continue
		}
		healthyMarks = append(healthyMarks, fmt.Sprintf("0x%x", candidate.Mark))
	}
	sort.Strings(healthyMarks)
	var buf bytes.Buffer
	buf.WriteString("# Generated by routerd. Do not edit by hand.\n")
	buf.WriteString("add table ip routerd_default_route\n")
	buf.WriteString("flush table ip routerd_default_route\n")
	buf.WriteString("table ip routerd_default_route {\n")
	buf.WriteString("  chain prerouting {\n")
	buf.WriteString("    type filter hook prerouting priority -151; policy accept;\n")
	activeTargetCandidate := len(active.Targets) > 0
	activeMark := fmt.Sprintf("0x%x", active.Mark)
	for _, match := range matches {
		prefix := strings.TrimSpace(match)
		if prefix != "" {
			prefix += " "
		}
		if len(healthyMarks) > 0 {
			set := "{ " + strings.Join(healthyMarks, ", ") + " }"
			buf.WriteString("    " + prefix + "ct mark " + set + " meta mark set ct mark\n")
			if activeTargetCandidate {
				buf.WriteString("    " + prefix + "ct mark != 0x0 ct mark != " + set + " meta mark set 0x0 ct mark set meta mark\n")
			} else {
				buf.WriteString("    " + prefix + "ct mark != 0x0 ct mark != " + set + " meta mark set " + activeMark + " ct mark set meta mark\n")
			}
		}
		if !activeTargetCandidate {
			buf.WriteString("    " + prefix + "ct mark 0x0 meta mark set " + activeMark + " ct mark set meta mark\n")
		}
	}
	buf.WriteString("  }\n")
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}

func ipv4PolicyMatches(resourceID string, sourceCIDRs, destinationCIDRs []string) ([]string, error) {
	var sources []string
	if len(sourceCIDRs) == 0 {
		sources = []string{""}
	} else {
		for _, cidr := range sourceCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return nil, fmt.Errorf("%s has invalid IPv4 source CIDR %q", resourceID, cidr)
			}
			sources = append(sources, "ip saddr "+prefix.Masked().String())
		}
	}
	var destinations []string
	if len(destinationCIDRs) == 0 {
		destinations = []string{""}
	} else {
		for _, cidr := range destinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return nil, fmt.Errorf("%s has invalid IPv4 destination CIDR %q", resourceID, cidr)
			}
			destinations = append(destinations, "ip daddr "+prefix.Masked().String())
		}
	}
	var matches []string
	for _, source := range sources {
		for _, destination := range destinations {
			matches = append(matches, strings.TrimSpace(strings.Join([]string{source, destination}, " ")))
		}
	}
	return matches, nil
}

func currentIPv4DefaultGatewayForInterface(ifname string) (string, error) {
	out, err := exec.Command("ip", "-4", "route", "show", "default", "dev", ifname).CombinedOutput()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	for i, field := range fields {
		if field == "via" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}
	return "", fmt.Errorf("no gateway found")
}

func currentIPv4HealthTargetForInterface(ifname string) (string, error) {
	if gateway, err := currentIPv4DefaultGatewayForInterface(ifname); err == nil {
		return gateway, nil
	}
	out, err := exec.Command("ip", "-4", "addr", "show", "dev", ifname).CombinedOutput()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	for i, field := range fields {
		if field == "peer" && i+1 < len(fields) {
			addr := strings.SplitN(fields[i+1], "/", 2)[0]
			if parsed, err := netip.ParseAddr(addr); err == nil && parsed.Is4() {
				return parsed.String(), nil
			}
		}
	}
	return "", fmt.Errorf("no IPv4 default gateway or peer found")
}

func applyIPv4PolicyRouteTarget(resourceID string, aliases map[string]string, target api.EgressRoutePolicyTarget, skipMissingLink bool) (string, error) {
	outboundInterface := target.EffectiveInterface()
	ifname := aliases[outboundInterface]
	if ifname == "" {
		return "", fmt.Errorf("%s references outbound interface with empty ifname", resourceID)
	}
	if !linkExists(ifname) {
		if skipMissingLink {
			return "", nil
		}
		return "", fmt.Errorf("%s outbound interface %s does not exist", resourceID, ifname)
	}
	metric := target.EffectiveMetric()
	if metric == 0 {
		metric = 50
	}
	table := target.EffectiveTable()
	if err := runLogged("ip", "-4", "route", "replace", "default", "dev", ifname, "table", fmt.Sprintf("%d", table), "metric", fmt.Sprintf("%d", metric)); err != nil {
		return "", fmt.Errorf("%s route table: %w", resourceID, err)
	}
	if err := ensureIPv4FwmarkRule(target.Priority, target.Mark, table); err != nil {
		return "", fmt.Errorf("%s policy rule: %w", resourceID, err)
	}
	name := target.Name
	if name == "" {
		name = outboundInterface
	}
	return fmt.Sprintf("%s(table=%d,mark=0x%x)", name, table, target.Mark), nil
}

func egressRouteTargetFromCandidate(candidate api.EgressRoutePolicyCandidate) api.EgressRoutePolicyTarget {
	return api.EgressRoutePolicyTarget{
		Name:        candidate.Name,
		Interface:   candidate.EffectiveInterface(),
		Table:       candidate.Table,
		RouteTable:  candidate.RouteTable,
		Priority:    candidate.Priority,
		Mark:        candidate.Mark,
		RouteMetric: candidate.RouteMetric,
		Metric:      candidate.Metric,
		HealthCheck: candidate.HealthCheck,
	}
}

func linkExists(ifname string) bool {
	return exec.Command("ip", "link", "show", "dev", ifname).Run() == nil
}

func ensureIPv4FwmarkRule(priority, mark, table int) error {
	priorityText := fmt.Sprintf("%d", priority)
	markText := fmt.Sprintf("0x%x", mark)
	tableText := fmt.Sprintf("%d", table)
	out, err := exec.Command("ip", "-4", "rule", "show", "priority", priorityText).CombinedOutput()
	if err == nil {
		line := string(out)
		if strings.Contains(line, "fwmark "+markText) && strings.Contains(line, "lookup "+tableText) {
			return nil
		}
	}
	for {
		out, err := exec.Command("ip", "-4", "rule", "show", "priority", priorityText).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			break
		}
		if err := exec.Command("ip", "-4", "rule", "del", "priority", priorityText).Run(); err != nil {
			break
		}
	}
	return runLogged("ip", "-4", "rule", "add", "priority", priorityText, "fwmark", markText, "table", tableText)
}

func applyDSLiteTunnelsWithState(router *api.Router, store routerstate.Store) ([]string, error) {
	aliases := map[string]string{}
	pdPrefixes := map[string]string{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Interface":
			spec, err := res.InterfaceSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = spec.IfName
		case "DHCPv6PrefixDelegation":
			if store == nil {
				continue
			}
			base := "ipv6PrefixDelegation." + res.Metadata.Name
			lease, _ := routerstate.PDLeaseFromStore(store, base)
			if lease.CurrentPrefix != "" {
				pdPrefixes[res.Metadata.Name] = lease.CurrentPrefix
			}
		}
	}
	delegated, err := ipv6DelegatedAddressSpecs(router)
	if err != nil {
		return nil, err
	}
	var applied []string
	for _, res := range router.Spec.Resources {
		if res.Kind != "DSLiteTunnel" {
			continue
		}
		spec, err := res.DSLiteTunnelSpec()
		if err != nil {
			return nil, err
		}
		tunnelName := defaultString(spec.TunnelName, res.Metadata.Name)
		if !api.BoolDefault(spec.Enabled, true) {
			_ = deleteDSLiteTunnel(tunnelName)
			applied = append(applied, "removed-disabled:"+tunnelName)
			continue
		}
		ifname := aliases[spec.Interface]
		local, localIfName, err := dsliteLocalAddressWithPrefixes(spec, ifname, aliases, delegated, pdPrefixes)
		if err != nil {
			if !errors.Is(err, errNoIPv6PrefixAvailable) {
				return nil, fmt.Errorf("%s local address: %w", res.ID(), err)
			}
			_ = deleteDSLiteTunnel(tunnelName)
			applied = append(applied, "removed-unusable:"+tunnelName)
			continue
		}
		remote, err := resolveDSLiteRemoteWithState(spec, store)
		if err != nil {
			return nil, fmt.Errorf("%s resolve AFTR: %w", res.ID(), err)
		}
		if localIfName != "" {
			ensured, err := ensureIPv6LocalAddress(localIfName, local)
			if err != nil {
				return nil, fmt.Errorf("%s ensure local address: %w", res.ID(), err)
			}
			if ensured {
				applied = append(applied, localIfName+":"+local)
			}
		}
		changed, err := ensureDSLiteTunnel(router, tunnelName, ifname, local, remote, spec)
		if err != nil {
			return nil, fmt.Errorf("%s apply tunnel: %w", res.ID(), err)
		}
		if changed {
			applied = append(applied, tunnelName)
		}
	}
	return applied, nil
}

func resolveDSLiteRemoteWithState(spec api.DSLiteTunnelSpec, store routerstate.Store) (string, error) {
	for _, candidate := range []string{
		spec.RemoteAddress,
		spec.AFTRIPv6,
		resourcequery.Value(objectStatusStore(store), spec.AFTRFrom),
		spec.AFTRFQDN,
	} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if addr, err := netip.ParseAddr(candidate); err == nil {
			if addr.Is6() {
				return addr.String(), nil
			}
			return "", fmt.Errorf("%s is not an IPv6 address", candidate)
		}
		return resolveAAAAWithServers(candidate, dsliteAFTRDNSServersWithState(spec, store), spec.AFTRAddressOrdinal, spec.AFTRAddressSelection)
	}
	return "", fmt.Errorf("missing AFTR source")
}

func dsliteAFTRDNSServersWithState(spec api.DSLiteTunnelSpec, store routerstate.Store) []string {
	servers := append([]string{}, spec.AFTRDNSServers...)
	sourceStore := objectStatusStore(store)
	if sourceStore != nil && strings.TrimSpace(spec.AFTRFrom.Resource) != "" {
		source := spec.AFTRFrom
		source.Field = "dnsServers"
		servers = append(servers, resourcequery.Values(sourceStore, source)...)
	}
	return compactStringList(servers)
}

func objectStatusStore(store routerstate.Store) resourcequery.Store {
	if store == nil {
		return nil
	}
	statusStore, ok := store.(interface {
		ObjectStatus(apiVersion, kind, name string) map[string]any
	})
	if !ok {
		return nil
	}
	return statusStore
}

func deleteDSLiteTunnel(name string) error {
	if name == "" {
		return nil
	}
	if platformDefaults.OS == platform.OSFreeBSD {
		name = freeBSDDSLiteRuntimeIfName(name)
		return exec.Command("ifconfig", name, "destroy").Run()
	}
	return exec.Command("ip", "-6", "tunnel", "del", name).Run()
}

func dsliteLocalAddress(spec api.DSLiteTunnelSpec, ifname string, aliases map[string]string, delegated map[string]api.IPv6DelegatedAddressSpec) (string, string, error) {
	return dsliteLocalAddressWithPrefixes(spec, ifname, aliases, delegated, nil)
}

func dsliteLocalAddressWithPrefixes(spec api.DSLiteTunnelSpec, ifname string, aliases map[string]string, delegated map[string]api.IPv6DelegatedAddressSpec, pdPrefixes map[string]string) (string, string, error) {
	switch defaultString(spec.LocalAddressSource, "interface") {
	case "interface":
		if spec.LocalAddress != "" {
			return spec.LocalAddress, "", nil
		}
		local := firstGlobalIPv6(ipv6Addresses(ifname))
		if local == "" {
			return "", "", fmt.Errorf("no global IPv6 address on %s", ifname)
		}
		return local, "", nil
	case "static":
		if spec.LocalAddress == "" {
			return "", "", fmt.Errorf("localAddress is required")
		}
		return spec.LocalAddress, "", nil
	case "delegatedAddress":
		delegatedSpec, ok := delegated[spec.LocalDelegatedAddress]
		if !ok {
			return "", "", fmt.Errorf("missing IPv6DelegatedAddress %q", spec.LocalDelegatedAddress)
		}
		localIfName := aliases[delegatedSpec.Interface]
		if localIfName == "" {
			return "", "", fmt.Errorf("missing Interface %q for delegated address %q", delegatedSpec.Interface, spec.LocalDelegatedAddress)
		}
		suffix := defaultString(spec.LocalAddressSuffix, delegatedSpec.AddressSuffix)
		if prefix := pdPrefixes[delegatedSpec.PrefixDelegation]; prefix != "" {
			local, err := deriveIPv6AddressFromDelegatedPrefix(prefix, delegatedSpec.SubnetID, suffix)
			if err != nil {
				return "", "", err
			}
			return local, localIfName, nil
		}
		if pdPrefixes != nil {
			return "", "", errNoIPv6PrefixAvailable
		}
		local, err := deriveIPv6AddressFromInterface(localIfName, suffix)
		if err != nil {
			return "", "", err
		}
		return local, localIfName, nil
	default:
		return "", "", fmt.Errorf("unsupported localAddressSource %q", spec.LocalAddressSource)
	}
}

func deriveIPv6AddressFromInterface(ifname, suffix string) (string, error) {
	if address, err := deriveIPv6Address(ipv6Prefixes(ifname), suffix); err == nil {
		return address, nil
	}
	return deriveIPv6AddressFromGlobalAddress(ipv6Addresses(ifname), suffix)
}

func deriveIPv6AddressFromDelegatedPrefix(value, subnetID, suffix string) (string, error) {
	prefix, err := netip.ParsePrefix(value)
	if err != nil || !prefix.Addr().Is6() {
		return "", fmt.Errorf("invalid delegated IPv6 prefix %q", value)
	}
	if prefix.Bits() > 64 {
		return "", fmt.Errorf("delegated IPv6 prefix %q is longer than /64", value)
	}
	subnet, err := parseIPv6SubnetID(defaultString(subnetID, "0"))
	if err != nil {
		return "", err
	}
	subnetBits := 64 - prefix.Bits()
	if subnetBits < 64 && subnet >= (uint64(1)<<subnetBits) {
		return "", fmt.Errorf("subnetID %q does not fit in delegated prefix %s", subnetID, value)
	}
	suffixAddr, err := netip.ParseAddr(suffix)
	if err != nil || !suffixAddr.Is6() {
		return "", fmt.Errorf("invalid IPv6 suffix %q", suffix)
	}
	addrBytes := prefix.Masked().Addr().As16()
	first64 := binary.BigEndian.Uint64(addrBytes[:8])
	first64 |= subnet
	binary.BigEndian.PutUint64(addrBytes[:8], first64)
	suffixBytes := suffixAddr.As16()
	for i := range addrBytes {
		addrBytes[i] |= suffixBytes[i]
	}
	return netip.AddrFrom16(addrBytes).String(), nil
}

func parseIPv6SubnetID(value string) (uint64, error) {
	if value == "" {
		return 0, nil
	}
	if parsed, err := strconv.ParseUint(value, 0, 64); err == nil {
		return parsed, nil
	}
	parsed, err := strconv.ParseUint(value, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid IPv6 subnetID %q", value)
	}
	return parsed, nil
}

func deriveIPv6Address(prefixes []string, suffix string) (string, error) {
	suffixAddr, err := netip.ParseAddr(suffix)
	if err != nil || !suffixAddr.Is6() {
		return "", fmt.Errorf("invalid IPv6 suffix %q", suffix)
	}
	suffixBytes := suffixAddr.As16()
	for _, value := range prefixes {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !prefix.Addr().Is6() {
			continue
		}
		if prefix.Addr().IsLinkLocalUnicast() {
			continue
		}
		addrBytes := prefix.Masked().Addr().As16()
		for i := range addrBytes {
			addrBytes[i] |= suffixBytes[i]
		}
		return netip.AddrFrom16(addrBytes).String(), nil
	}
	return "", errNoIPv6PrefixAvailable
}

func deriveIPv6AddressFromGlobalAddress(addresses []string, suffix string) (string, error) {
	suffixAddr, err := netip.ParseAddr(suffix)
	if err != nil || !suffixAddr.Is6() {
		return "", fmt.Errorf("invalid IPv6 suffix %q", suffix)
	}
	suffixBytes := suffixAddr.As16()
	for _, value := range addresses {
		addr, err := netip.ParseAddr(value)
		if err != nil || !addr.Is6() || addr.IsLinkLocalUnicast() {
			continue
		}
		addrBytes := addr.As16()
		for i := 8; i < len(addrBytes); i++ {
			addrBytes[i] = 0
		}
		for i := range addrBytes {
			addrBytes[i] |= suffixBytes[i]
		}
		return netip.AddrFrom16(addrBytes).String(), nil
	}
	return "", errNoIPv6PrefixAvailable
}

func ipv6HostSuffix64(addr netip.Addr) uint64 {
	bytes := addr.As16()
	return binary.BigEndian.Uint64(bytes[8:])
}

func ensureIPv6LocalAddress(ifname, address string) (bool, error) {
	for _, value := range ipv6Addresses(ifname) {
		if value == address {
			return false, nil
		}
	}
	if platformDefaults.OS == platform.OSFreeBSD {
		if err := runLogged("ifconfig", ifname, "inet6", address, "prefixlen", "64", "alias"); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := runLogged("ip", "-6", "addr", "add", address+"/128", "dev", ifname); err != nil {
		return false, err
	}
	return true, nil
}

func deleteIPv6LocalAddress(ifname, address string, prefixLen int) error {
	if prefixLen <= 0 || prefixLen > 128 {
		prefixLen = 128
	}
	if platformDefaults.OS == platform.OSFreeBSD {
		return runLogged("ifconfig", ifname, "inet6", address, "-alias")
	}
	return runLogged("ip", "-6", "addr", "del", fmt.Sprintf("%s/%d", address, prefixLen), "dev", ifname)
}

func ensureDSLiteTunnel(router *api.Router, name, ifname, local, remote string, spec api.DSLiteTunnelSpec) (bool, error) {
	if platformDefaults.OS == platform.OSFreeBSD {
		return ensureFreeBSDDSLiteTunnel(router, name, ifname, local, remote, spec)
	}
	innerLocal, err := dsliteInnerLocalIPv4(router, spec)
	if err != nil {
		return false, err
	}
	desiredRouteMetric := spec.RouteMetric
	if desiredRouteMetric == 0 {
		desiredRouteMetric = 50
	}
	encapLimit := defaultString(spec.EncapsulationLimit, "none")
	show, showErr := exec.Command("ip", "-6", "tunnel", "show", name).CombinedOutput()
	needsRecreate := showErr != nil || !strings.Contains(string(show), "remote "+remote) || !strings.Contains(string(show), "local "+local)
	if needsRecreate {
		_ = exec.Command("ip", "-6", "tunnel", "del", name).Run()
		args := []string{"-6", "tunnel", "add", name, "mode", "ipip6", "remote", remote, "local", local, "dev", ifname, "encaplimit", encapLimit}
		if err := runLogged("ip", args...); err != nil {
			return false, err
		}
	}
	if spec.MTU != 0 {
		if err := runLogged("ip", "link", "set", "dev", name, "mtu", fmt.Sprintf("%d", spec.MTU)); err != nil {
			return false, err
		}
	}
	if err := runLogged("ip", "link", "set", "dev", name, "up"); err != nil {
		return false, err
	}
	if err := ensureLinuxDSLiteInnerIPv4(name, innerLocal); err != nil {
		return false, err
	}
	if spec.DefaultRoute {
		routeOut, routeErr := exec.Command("ip", "-4", "route", "show", "default", "dev", name, "metric", fmt.Sprintf("%d", desiredRouteMetric)).CombinedOutput()
		routeMissing := routeErr != nil || strings.TrimSpace(string(routeOut)) == ""
		if routeMissing {
			if err := runLogged("ip", "-4", "route", "replace", "default", "dev", name, "metric", fmt.Sprintf("%d", desiredRouteMetric)); err != nil {
				return false, err
			}
			needsRecreate = true
		}
	}
	return needsRecreate, nil
}

func ensureFreeBSDDSLiteTunnel(router *api.Router, name, ifname, local, remote string, spec api.DSLiteTunnelSpec) (bool, error) {
	name = freeBSDDSLiteRuntimeIfName(name)
	innerLocal, err := dsliteInnerLocalIPv4(router, spec)
	if err != nil {
		return false, err
	}
	show, showErr := exec.Command("ifconfig", name).CombinedOutput()
	mtuText := ""
	if spec.MTU != 0 {
		mtuText = "mtu " + strconv.Itoa(spec.MTU)
	}
	innerIPv4Text := "inet " + innerLocal + " --> " + dsliteInnerRemoteIPv4
	needsRecreate := showErr != nil ||
		!strings.Contains(string(show), "tunnel inet6 "+local+" --> "+remote) ||
		!strings.Contains(string(show), innerIPv4Text) ||
		(mtuText != "" && !strings.Contains(string(show), mtuText))
	if needsRecreate {
		_ = exec.Command("ifconfig", name, "destroy").Run()
		if err := runLogged("ifconfig", name, "create"); err != nil {
			return false, err
		}
		if err := runLogged("ifconfig", name, "inet6", "tunnel", local, remote); err != nil {
			return false, err
		}
		if err := runLogged("ifconfig", name, "inet", innerLocal, dsliteInnerRemoteIPv4, "netmask", "255.255.255.255"); err != nil {
			return false, err
		}
		if spec.MTU != 0 {
			if err := runLogged("ifconfig", name, "mtu", strconv.Itoa(spec.MTU)); err != nil {
				return false, err
			}
		}
		if err := runLogged("ifconfig", name, "up"); err != nil {
			return false, err
		}
	}
	if spec.DefaultRoute {
		routeOut, routeErr := exec.Command("route", "-n", "get", "default").CombinedOutput()
		routeMissing := routeErr != nil ||
			!strings.Contains(string(routeOut), "gateway: "+dsliteInnerRemoteIPv4) ||
			!strings.Contains(string(routeOut), "interface: "+name)
		if routeMissing {
			if out, err := exec.Command("route", "-n", "change", "default", dsliteInnerRemoteIPv4).CombinedOutput(); err != nil {
				if addErr := runLogged("route", "-n", "add", "default", dsliteInnerRemoteIPv4); addErr != nil {
					return false, fmt.Errorf("route change default: %w: %s; route add default: %w", err, strings.TrimSpace(string(out)), addErr)
				}
			}
			needsRecreate = true
		}
	}
	return needsRecreate, nil
}

const (
	dsliteDefaultInnerLocalIPv4 = "192.0.0.2"
	dsliteInnerRemoteIPv4       = "192.0.0.1"
)

func dsliteInnerLocalIPv4(router *api.Router, spec api.DSLiteTunnelSpec) (string, error) {
	value := ""
	if strings.TrimSpace(spec.LocalAddressFrom.Resource) != "" {
		value = statusAddressValue(addressFromRouterResource(router, spec.LocalAddressFrom))
		if value == "" {
			if spec.LocalAddressFrom.Optional {
				value = dsliteDefaultInnerLocalIPv4
			} else {
				return "", fmt.Errorf("localAddressFrom %s.%s is unresolved", spec.LocalAddressFrom.Resource, defaultString(spec.LocalAddressFrom.Field, "address"))
			}
		}
	}
	if value == "" {
		value = dsliteDefaultInnerLocalIPv4
	}
	addr, err := netip.ParseAddr(value)
	if err != nil || !addr.Is4() {
		return "", fmt.Errorf("innerLocalAddress %q is not an IPv4 address", value)
	}
	if addr.IsUnspecified() || addr.IsMulticast() || addr.IsLoopback() {
		return "", fmt.Errorf("innerLocalAddress %q must be a usable unicast IPv4 address", value)
	}
	return addr.String(), nil
}

func addressFromRouterResource(router *api.Router, source api.StatusValueSourceSpec) string {
	if router == nil || strings.TrimSpace(source.Resource) == "" {
		return ""
	}
	kind, name, ok := strings.Cut(strings.TrimSpace(source.Resource), "/")
	if !ok || kind == "" || name == "" {
		return ""
	}
	field := defaultString(source.Field, "address")
	for _, res := range router.Spec.Resources {
		if res.Kind != kind || res.Metadata.Name != name {
			continue
		}
		if kind != "IPv4StaticAddress" || field != "address" {
			return ""
		}
		spec, err := res.IPv4StaticAddressSpec()
		if err != nil {
			return ""
		}
		return spec.Address
	}
	return ""
}

func ensureLinuxDSLiteInnerIPv4(ifname, innerLocal string) error {
	out, err := exec.Command("ip", "-4", "addr", "show", "dev", ifname).CombinedOutput()
	if err == nil && strings.Contains(string(out), "inet "+innerLocal+" ") && strings.Contains(string(out), "peer "+dsliteInnerRemoteIPv4+" ") {
		return nil
	}
	args := []string{"-4", "addr", "replace", innerLocal + "/32", "peer", dsliteInnerRemoteIPv4 + "/32", "dev", ifname}
	if out, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("ip %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

var freeBSDDSLiteRuntimeGIFNamePattern = regexp.MustCompile(`^gif[0-9]+$`)

func freeBSDDSLiteRuntimeIfName(name string) string {
	name = strings.TrimSpace(name)
	if freeBSDDSLiteRuntimeGIFNamePattern.MatchString(name) {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	index := 100 + int(binary.BigEndian.Uint16(sum[:2])%900)
	return "gif" + strconv.Itoa(index)
}

func resolveAAAAWithServers(host string, servers []string, ordinal int, selection string) (string, error) {
	if len(servers) == 0 {
		value, err := resolveAAAA(host, "", ordinal, selection)
		if err == nil {
			return value, nil
		}
		localValue, localErr := resolveAAAA(host, "127.0.0.1", ordinal, selection)
		if localErr == nil {
			return localValue, nil
		}
		return "", err
	}
	var lastErr error
	for _, server := range servers {
		value, err := resolveAAAA(host, server, ordinal, selection)
		if err == nil {
			return value, nil
		}
		lastErr = err
	}
	return "", lastErr
}

func resolveAAAA(host, server string, ordinal int, selection string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resolver := net.DefaultResolver
	if server != "" {
		resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "udp", dnsServerAddress(server))
			},
		}
	}
	addrs, err := resolver.LookupNetIP(ctx, "ip6", host)
	if err != nil {
		return "", err
	}
	var values []string
	for _, addr := range addrs {
		if addr.Is6() {
			values = append(values, addr.String())
		}
	}
	sort.Strings(values)
	if len(values) == 0 {
		return "", fmt.Errorf("no AAAA records for %s", host)
	}
	return selectAAAA(values, ordinal, selection)
}

func dnsServerAddress(server string) string {
	server = strings.TrimSpace(server)
	if server == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(server); err == nil {
		return server
	}
	return net.JoinHostPort(server, "53")
}

func selectAAAA(values []string, ordinal int, selection string) (string, error) {
	if len(values) == 0 {
		return "", fmt.Errorf("no AAAA records")
	}
	if ordinal == 0 {
		ordinal = 1
	}
	if selection == "" {
		selection = "ordinal"
	}
	if selection == "ordinalModulo" {
		index := (ordinal - 1) % len(values)
		return values[index], nil
	}
	if ordinal < 1 || ordinal > len(values) {
		return "", fmt.Errorf("AAAA ordinal %d is outside available record count %d", ordinal, len(values))
	}
	return values[ordinal-1], nil
}

func firstGlobalIPv6(values []string) string {
	for _, value := range values {
		addr, err := netip.ParseAddr(value)
		if err == nil && addr.Is6() && !addr.IsLinkLocalUnicast() {
			return addr.String()
		}
	}
	return ""
}
