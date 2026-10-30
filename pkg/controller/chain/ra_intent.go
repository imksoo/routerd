// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"math"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/resourcequery"
	"github.com/mdlayher/ndp"
)

func prepareRAPlans(declared *api.Router) ([]raInterfacePlan, error) {
	var plans []raInterfacePlan
	if declared == nil {
		return plans, nil
	}
	mtus, err := render.PathMTURAByScope(declared)
	if err != nil {
		return nil, err
	}
	explicit := map[string]bool{}
	for _, resource := range declared.Spec.Resources {
		if resource.Kind == "IPv6RouterAdvertisement" {
			spec, err := resource.IPv6RouterAdvertisementSpec()
			if err != nil {
				return nil, err
			}
			explicit[api.ResolveInterfaceIfName(declared, spec.Interface)] = true
		}
	}
	for _, resource := range declared.Spec.Resources {
		if resource.Kind != "IPv6RouterAdvertisement" && resource.Kind != "DHCPv6Server" {
			continue
		}
		intent := raResourceIntent{Autonomous: true, ResourceID: resource.ID()}
		iface := ""
		if resource.Kind == "IPv6RouterAdvertisement" {
			spec, err := resource.IPv6RouterAdvertisementSpec()
			if err != nil {
				return nil, err
			}

			intent.Name, intent.Spec = resource.Metadata.Name, spec
			intent.StatusKind = "IPv6RouterAdvertisement"
			intent.Spec.MTU = chainFirstNonZero(mtus[resource.Metadata.Name], spec.MTU)
			intent.Limits, err = api.ResolveRouterAdvertisementLifetimes(spec)
			if err != nil {
				return nil, err
			}
			iface = spec.Interface
			if spec.PrefixFrom.Resource != "" {
				kind, name, ok := resourcequery.SplitResource(spec.PrefixFrom.Resource)
				if !ok || kind != "IPv6DelegatedAddress" || spec.PrefixFrom.Field != "" && spec.PrefixFrom.Field != "address" {
					return nil, fmt.Errorf("%s prefixFrom must select IPv6DelegatedAddress.address with canonical lease lifetimes", resource.ID())
				}
				for _, candidate := range declared.Spec.Resources {
					if candidate.Kind == kind && candidate.Metadata.Name == name {
						value, err := candidate.IPv6DelegatedAddressSpec()
						if err != nil {
							return nil, err
						}
						intent.Delegated = &value
						break
					}
				}
				if intent.Delegated == nil {
					return nil, fmt.Errorf("%s prefixFrom resource unavailable", resource.ID())
				}
				intent.DelegatedName = name
			}
		} else {
			server, err := resource.DHCPv6ServerSpec()
			if err != nil {
				return nil, err
			}
			iface = server.Interface
			if server.DelegatedAddress != "" {
				for _, candidate := range declared.Spec.Resources {
					if candidate.Kind == "IPv6DelegatedAddress" && candidate.Metadata.Name == server.DelegatedAddress {
						value, err := candidate.IPv6DelegatedAddressSpec()
						if err != nil {
							return nil, err
						}
						intent.Delegated = &value
						iface = value.Interface
						break
					}
				}
				if intent.Delegated == nil {
					return nil, fmt.Errorf("%s delegated address unavailable", resource.ID())
				}
				intent.DelegatedName = server.DelegatedAddress
			}
			if iface == "" || explicit[api.ResolveInterfaceIfName(declared, iface)] {
				continue
			}

			intent.Spec = api.IPv6RouterAdvertisementSpec{Interface: iface, RDNSS: server.DNSServers, RDNSSFrom: server.DNSServerFrom, DNSSL: server.DomainSearch, DNSSLFrom: server.DomainSearchFrom, DependsOn: server.DependsOn, When: resourcequery.ResourceWhen(resource)}
			intent.Limits = api.RouterAdvertisementLifetimes{Preferred: math.MaxUint32, Valid: math.MaxUint32}
			if server.DefaultRoute {
				intent.Limits.Router = 1800
			}
			switch firstNonEmpty(server.Mode, "stateless") {
			case "stateful":
				intent.Spec.MFlag = true
				intent.Spec.OFlag = true
				intent.Autonomous = false
			case "both":
				intent.Spec.MFlag = true
				intent.Spec.OFlag = true
			case "stateless":
				intent.Spec.OFlag = true
			}
			if server.DNSSource == "none" {
				intent.Spec.RDNSS = nil
				intent.Spec.RDNSSFrom = nil
			}
			intent.SelfDNS, err = prepareRASelfDNS(declared, server, intent)
			if err != nil {
				return nil, fmt.Errorf("%s self DNS: %w", resource.ID(), err)
			}
			if server.Mode == "ra-only" {
				intent.Name = resource.Metadata.Name
				intent.StatusKind = "DHCPv6Server"
			}
			intent.Native = intent.Delegated == nil
			intent.Spec.MTU = mtus[resource.Metadata.Name]
		}
		ifname := api.ResolveInterfaceIfName(declared, iface)
		if ifname == "" {
			ifname = iface
		}
		plan := raInterfacePlan{}
		plan.Interface = ifname
		plan.MFlag = intent.Spec.MFlag
		plan.OFlag = intent.Spec.OFlag
		plan.MTU = intent.Spec.MTU
		plan.Preference = intent.Spec.PRFPreference
		plan.RouterLifetime = intent.Limits.Router
		plan.Resources = append(plan.Resources, intent)
		plans = append(plans, plan)
	}
	return plans, nil
}

type raPrefixValue struct {
	Prefix           netip.Prefix
	Preferred, Valid uint32
	Autonomous       bool
}

type raPlanConflicts struct{ Interfaces []string }

func (e *raPlanConflicts) Error() string {
	return fmt.Sprintf("RA intents disagree on interface flags, MTU, preference, or router lifetime: %s", strings.Join(e.Interfaces, ", "))
}

// A conflicting interface has no usable current intent. Preserve independent
// interfaces while allowing the owner to withdraw the conflicting one's receipt.
func selectRAPlans(prepared []raInterfacePlan, effective *api.Router) (map[string]raInterfacePlan, error) {
	selected := map[string]bool{}
	if effective != nil {
		for _, resource := range effective.Spec.Resources {
			selected[resource.ID()] = true
		}
	}
	plans := map[string]raInterfacePlan{}
	conflicts := map[string]bool{}
	for _, candidate := range prepared {
		if !selected[candidate.Resources[0].ResourceID] || conflicts[candidate.Interface] {
			continue
		}
		plan, exists := plans[candidate.Interface]
		if !exists {
			plans[candidate.Interface] = candidate
			continue
		}
		if plan.MFlag != candidate.MFlag || plan.OFlag != candidate.OFlag || plan.MTU != candidate.MTU || firstNonEmpty(plan.Preference, "medium") != firstNonEmpty(candidate.Preference, "medium") || plan.RouterLifetime != candidate.RouterLifetime {
			conflicts[candidate.Interface] = true
			delete(plans, candidate.Interface)
			continue
		}
		plan.Resources = append(plan.Resources, candidate.Resources...)
		plans[candidate.Interface] = plan
	}
	if len(conflicts) > 0 {
		names := make([]string, 0, len(conflicts))
		for name := range conflicts {
			names = append(names, name)
		}
		sort.Strings(names)
		return plans, &raPlanConflicts{Interfaces: names}
	}
	return plans, nil
}

func buildRAAdvertisement(ctx context.Context, router *api.Router, store Store, plan raInterfacePlan, now time.Time) (*ndp.RouterAdvertisement, []raResourceStatus, error) {
	packet := &ndp.RouterAdvertisement{CurrentHopLimit: 64, ManagedConfiguration: plan.MFlag, OtherConfiguration: plan.OFlag, RouterLifetime: time.Duration(plan.RouterLifetime) * time.Second}
	switch plan.Preference {
	case "high":
		packet.RouterSelectionPreference = ndp.High
	case "low":
		packet.RouterSelectionPreference = ndp.Low
	}
	if plan.MTU != 0 {
		packet.Options = append(packet.Options, ndp.NewMTU(uint32(plan.MTU)))
	}
	prefixes := map[netip.Prefix]raPrefixValue{}
	var statuses []raResourceStatus
	dns := map[netip.Addr]uint32{}
	domains := map[string]bool{}
	for _, intent := range plan.Resources {
		stateStore, _ := store.(resourcequery.StateStore)
		matches, err := resourcequery.ResourceWhenMatches(intent.Spec.When, stateStore)
		if err != nil {
			return nil, nil, err
		}
		ready, err := resourcequery.ReadDependenciesReady(store, intent.Spec.DependsOn)
		if err != nil {
			return nil, nil, err
		}
		var values []raPrefixValue
		if matches && ready {
			values, err = resolveRAPrefixes(ctx, router, store, plan.Interface, intent, now, plan.NativeIndex, plan.NativeScope, plan.NativeParentToken)
		}
		if err != nil {
			return nil, nil, err
		}
		status := raResourceStatus{IfName: plan.Interface, Kind: intent.StatusKind, Name: intent.Name, Interface: intent.Spec.Interface}
		for _, value := range values {
			value.Autonomous = intent.Autonomous
			if prior, exists := prefixes[value.Prefix]; exists && (prior.Preferred != value.Preferred || prior.Valid != value.Valid || prior.Autonomous != value.Autonomous) {
				return nil, nil, fmt.Errorf("RA prefix %s has conflicting lifetime/autoconfiguration intents", value.Prefix)
			}
			prefixes[value.Prefix] = value
			status.Prefixes = append(status.Prefixes, value.Prefix.String())
		}
		if len(values) == 0 {
			status.Pending = "PrefixNotReady"
		}
		if !matches {
			status.Pending = "WhenFalse"
		} else if !ready {
			status.Pending = "DependsOnFalse"
		}
		if !matches || !ready || len(values) == 0 {
			if intent.Name != "" {
				statuses = append(statuses, status)
			}
			continue
		}
		servers, _, err := expandServerSources(store, intent.Spec.RDNSSFrom, "RDNSSFrom")
		if err != nil {
			return nil, nil, err
		}
		selfDNSSelected := len(intent.SelfDNS) == 0
		if len(intent.SelfDNS) > 0 {
			selected, err := selectRASelfDNS(ctx, router, store, plan, intent.SelfDNS, now)
			if err != nil {
				return nil, nil, err
			}
			if selected.Address.IsValid() {
				selfDNSSelected = true
				dns[selected.Address] = max(dns[selected.Address], selected.Valid)
				status.RDNSS = append(status.RDNSS, selected.Address.String())
			}
		}
		for _, value := range append(expandServers(intent.Spec.RDNSS), servers...) {
			address, err := netip.ParseAddr(dnsmasqIPv6Address(value))
			if err != nil || !address.Is6() || address.Is4In6() || address.IsMulticast() || address.IsUnspecified() {
				return nil, nil, fmt.Errorf("RA RDNSS address %q is invalid", value)
			}
			dns[address] = math.MaxUint32
			status.RDNSS = append(status.RDNSS, address.String())
		}
		if !selfDNSSelected && len(status.RDNSS) == 0 {
			status.Pending = "SelfDNSNotReady"
		}
		search, _, err := expandDomainValues(router, store, intent.Spec.DNSSL, intent.Spec.DNSSLFrom, "DNSSLFrom")
		if err != nil {
			return nil, nil, err
		}
		for _, domain := range search {
			domains[strings.TrimSuffix(domain, ".")] = true
		}
		if intent.Name != "" {
			statuses = append(statuses, status)
		}
	}
	keys := make([]netip.Prefix, 0, len(prefixes))
	for prefix := range prefixes {
		keys = append(keys, prefix)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Addr().Less(keys[j].Addr()) })
	dnsLifetime := uint32(1800)
	for _, prefix := range keys {
		value := prefixes[prefix]
		packet.Options = append(packet.Options, &ndp.PrefixInformation{Prefix: prefix.Addr(), PrefixLength: uint8(prefix.Bits()), OnLink: true, AutonomousAddressConfiguration: value.Autonomous, PreferredLifetime: time.Duration(value.Preferred) * time.Second, ValidLifetime: time.Duration(value.Valid) * time.Second})
		if value.Valid < dnsLifetime {
			dnsLifetime = value.Valid
		}
	}
	if len(prefixes) == 0 {
		packet.RouterLifetime = 0
		dnsLifetime = 0
	}
	if len(dns) > 0 {
		byLifetime := map[uint32][]netip.Addr{}
		for server, valid := range dns {
			lifetime := min(valid, dnsLifetime)
			byLifetime[lifetime] = append(byLifetime[lifetime], server)
		}
		lifetimes := make([]uint32, 0, len(byLifetime))
		for lifetime := range byLifetime {
			lifetimes = append(lifetimes, lifetime)
		}
		sort.Slice(lifetimes, func(i, j int) bool { return lifetimes[i] < lifetimes[j] })
		for _, lifetime := range lifetimes {
			servers := byLifetime[lifetime]
			sort.Slice(servers, func(i, j int) bool { return servers[i].Less(servers[j]) })
			packet.Options = append(packet.Options, &ndp.RecursiveDNSServer{Servers: servers, Lifetime: time.Duration(lifetime) * time.Second})
		}
	}
	if len(domains) > 0 {
		names := make([]string, 0, len(domains))
		for name := range domains {
			names = append(names, name)
		}
		sort.Strings(names)
		packet.Options = append(packet.Options, &ndp.DNSSearchList{DomainNames: names, Lifetime: time.Duration(dnsLifetime) * time.Second})
	}
	return packet, statuses, nil
}

func resolveRAPrefixes(ctx context.Context, router *api.Router, store Store, ifname string, intent raResourceIntent, now time.Time, nativeIndex int, nativeScope string, nativeParentToken string) ([]raPrefixValue, error) {
	if intent.Native {
		return nativeRAPrefixes(ctx, ifname)
	}
	if intent.Delegated == nil {
		prefix, err := netip.ParsePrefix(intent.Spec.Prefix)
		if err != nil {
			return nil, err
		}
		return []raPrefixValue{{Prefix: prefix.Masked(), Preferred: intent.Limits.Preferred, Valid: intent.Limits.Valid}}, nil
	}
	delegated := *intent.Delegated
	// Current intent and the canonical association own validity. A generic
	// Pending result during renew/DAD is not an upstream revocation.
	stateStore, _ := store.(resourcequery.StateStore)
	enabled, err := resourcequery.ResourceWhenMatches(delegated.When, stateStore)
	if err != nil {
		return nil, err
	}
	ready, err := resourcequery.ReadDependenciesReady(store, delegated.DependsOn)
	if err != nil {
		return nil, err
	}
	if !enabled || !ready {
		return nil, nil
	}
	lease, found, err := livePDLease(store, delegated.PrefixDelegation, now)
	if err != nil || !found {
		return nil, err
	}
	if !lease.HasFreshTransactionEvidence(now) {
		return nil, nil
	}
	address, err := DeriveIPv6Address(lease.CurrentPrefix, delegated.SubnetID, delegated.AddressSuffix)
	if err != nil {
		return nil, err
	}
	prefix, err := netip.ParsePrefix(prefixFromAddress(address))
	if err != nil {
		return nil, err
	}
	preferred, valid, err := currentPDAddressLifetimes(lease, now, false)
	if err != nil {
		return nil, nil
	}
	preferred = min(preferred, intent.Limits.Preferred)
	valid = min(valid, intent.Limits.Valid)
	preferred = min(preferred, valid)
	values := []raPrefixValue{{Prefix: prefix.Masked(), Preferred: preferred, Valid: valid}}
	previous, err := derivePreviousPDLeaseAddresses(lease, lease.CurrentPrefix, delegated, now)
	if err != nil {
		return nil, err
	}
	for _, old := range previous {
		prefix, err := netip.ParsePrefix(prefixFromAddress(old.Address))
		if err != nil {
			return nil, err
		}
		seconds := remainingIPv6ValidLifetime(now, old.ExpiresAt)
		if seconds > 0 {
			values = append(values, raPrefixValue{Prefix: prefix.Masked(), Valid: min(uint32(seconds), intent.Limits.Valid)})
		}
	}
	if intent.DelegatedName != "" {
		status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", intent.DelegatedName)
		if err != nil {
			return nil, err
		}
		receipt, err := delegatedReceipt(status)
		if err != nil {
			return nil, err
		}
		if status["reason"] == "AddressNotReady" || status["reason"] == "AddressDADFailed" || status["reason"] == "NativeOwnershipUnknown" {
			return nil, fmt.Errorf("RA LAN readiness is unresolved")
		}

		if receipt == nil {
			return nil, nil
		}
		if receipt.IfName != ifname || receipt.PrefixDelegation != delegated.PrefixDelegation {
			return nil, fmt.Errorf("RA LAN native source identity differs")
		}
		// The service fills native identity for actual sends. Dry-run planning does
		// not acquire or advertise a native interface.
		if nativeIndex != 0 && (receipt.IfIndex != nativeIndex || receipt.Scope != nativeScope || receipt.ParentToken != nativeParentToken) {
			return nil, fmt.Errorf("RA LAN native interface identity differs")
		}
		var applied []raPrefixValue
		for _, value := range values {
			address, err := DeriveIPv6Address(value.Prefix.String(), "0", delegated.AddressSuffix)
			if err != nil {
				return nil, err
			}
			address, err = delegatedAddressWithPrefixLength(address, delegated.PrefixLength)
			if err != nil {
				return nil, err
			}
			for _, entry := range receipt.Addresses {
				if entry.Address != address {
					continue
				}
				if !entry.Held || (entry.Pending != nil && entry.Pending.Operation != "apply") {
					return nil, fmt.Errorf("RA LAN native apply is pending")
				}
				valid := remainingIPv6ValidLifetime(now, entry.ValidUntil)
				if valid == 0 {
					break
				}
				preferred := remainingIPv6ValidLifetime(now, entry.PreferredUntil)
				value.Valid = min(value.Valid, uint32(valid))
				value.Preferred = min(value.Preferred, uint32(preferred), value.Valid)
				applied = append(applied, value)
				break
			}
		}
		return applied, nil
	}
	return values, nil
}
