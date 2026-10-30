// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"math"
	"net/netip"
	"sort"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

type raSelfDNSCandidate struct {
	Spec      api.SelfAddressPolicyCandidate
	IfName    string
	Delegated *api.IPv6DelegatedAddressSpec
	Static    netip.Addr
	Suffix    netip.Addr
}

type raSelfDNSAddress struct {
	Address netip.Addr
	Valid   uint32
}

// Resolve declarations once. Observation updates only read each candidate's
// current source; configuration and historical display values are not sources.
func prepareRASelfDNS(router *api.Router, server api.DHCPv6ServerSpec, intent raResourceIntent) ([]raSelfDNSCandidate, error) {
	if firstNonEmpty(server.DNSSource, "self") != "self" {
		return nil, nil
	}
	var candidates []api.SelfAddressPolicyCandidate
	if server.SelfAddressPolicy == "" {
		if intent.Delegated == nil || len(intent.Spec.RDNSS) > 0 || len(intent.Spec.RDNSSFrom) > 0 {
			return nil, nil
		}
		candidates = []api.SelfAddressPolicyCandidate{{Source: "delegatedAddress", DelegatedAddress: intent.DelegatedName}}
	} else {
		found := false
		for _, resource := range router.Spec.Resources {
			if resource.Kind != "SelfAddressPolicy" || resource.Metadata.Name != server.SelfAddressPolicy {
				continue
			}
			policy, err := resource.SelfAddressPolicySpec()
			if err != nil {
				return nil, err
			}
			if policy.AddressFamily != "ipv6" || len(policy.Candidates) == 0 {
				return nil, fmt.Errorf("SelfAddressPolicy/%s requires IPv6 candidates", server.SelfAddressPolicy)
			}
			candidates, found = policy.Candidates, true
			break
		}
		if !found {
			return nil, fmt.Errorf("SelfAddressPolicy/%s is unavailable", server.SelfAddressPolicy)
		}
	}
	prepared := make([]raSelfDNSCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		value := raSelfDNSCandidate{Spec: candidate}
		switch candidate.Source {
		case "static":
			address, err := netip.ParseAddr(candidate.Address)
			if err != nil || !usableRASelfAddress(address) {
				return nil, fmt.Errorf("invalid static self DNS address %q", candidate.Address)
			}
			value.Static = address
		case "interfaceAddress":
			if candidate.Interface == "" || candidate.Ordinal < 0 {
				return nil, fmt.Errorf("invalid interface self DNS candidate")
			}
			value.IfName = api.ResolveInterfaceIfName(router, candidate.Interface)
			if candidate.MatchSuffix != "" {
				suffix, err := netip.ParseAddr(candidate.MatchSuffix)
				if err != nil || !suffix.Is6() || suffix.Is4In6() {
					return nil, fmt.Errorf("invalid self DNS suffix %q", candidate.MatchSuffix)
				}
				value.Suffix = suffix
			}
		case "delegatedAddress":
			for _, resource := range router.Spec.Resources {
				if resource.Kind != "IPv6DelegatedAddress" || resource.Metadata.Name != candidate.DelegatedAddress {
					continue
				}
				delegated, err := resource.IPv6DelegatedAddressSpec()
				if err != nil {
					return nil, err
				}
				value.Delegated = &delegated
				value.IfName = api.ResolveInterfaceIfName(router, delegated.Interface)
				break
			}
			if value.Delegated == nil {
				return nil, fmt.Errorf("IPv6DelegatedAddress/%s is unavailable", candidate.DelegatedAddress)
			}
		default:
			return nil, fmt.Errorf("unsupported self DNS candidate %q", candidate.Source)
		}
		prepared = append(prepared, value)
	}
	return prepared, nil
}

func selectRASelfDNS(ctx context.Context, store Store, plan raInterfacePlan, candidates []raSelfDNSCandidate, now time.Time) (raSelfDNSAddress, error) {
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return raSelfDNSAddress{}, err
		}
		var selected raSelfDNSAddress
		var err error
		switch candidate.Spec.Source {
		case "static":
			selected.Address = candidate.Static
			selected.Valid = math.MaxUint32
		case "delegatedAddress":
			selected, err = currentDelegatedRASelfDNS(ctx, store, plan, candidate, now)
		case "interfaceAddress":
			selected, err = currentNativeRASelfDNS(ctx, candidate)
		}
		if err != nil {
			return raSelfDNSAddress{}, err
		}
		if selected.Address.IsValid() && selected.Valid > 0 {
			return selected, nil
		}
	}
	return raSelfDNSAddress{}, nil
}

func currentDelegatedRASelfDNS(ctx context.Context, store Store, plan raInterfacePlan, candidate raSelfDNSCandidate, now time.Time) (raSelfDNSAddress, error) {
	delegated := candidate.Delegated
	stateStore, _ := store.(resourcequery.StateStore)
	enabled, err := resourcequery.ResourceWhenMatches(delegated.When, stateStore)
	if err != nil {
		return raSelfDNSAddress{}, err
	}
	ready, err := resourcequery.ReadDependenciesReady(store, delegated.DependsOn)
	if err != nil || !enabled || !ready {
		return raSelfDNSAddress{}, err
	}
	lease, found, err := livePDLease(store, delegated.PrefixDelegation, now)
	if err != nil || !found || !lease.HasFreshTransactionEvidence(now) {
		return raSelfDNSAddress{}, err
	}
	_, valid, err := currentPDAddressLifetimes(lease, now, false)
	if err != nil {
		return raSelfDNSAddress{}, nil
	}
	address, err := DeriveIPv6Address(lease.CurrentPrefix, delegated.SubnetID, firstNonEmpty(candidate.Spec.AddressSuffix, delegated.AddressSuffix))
	if err != nil {
		return raSelfDNSAddress{}, err
	}
	address, err = delegatedAddressWithPrefixLength(address, delegated.PrefixLength)
	if err != nil {
		return raSelfDNSAddress{}, err
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", candidate.Spec.DelegatedAddress)
	if err != nil {
		return raSelfDNSAddress{}, err
	}
	receipt, err := delegatedReceipt(status)
	if err != nil || receipt == nil {
		return raSelfDNSAddress{}, err
	}
	if receipt.IfName != candidate.IfName || receipt.PrefixDelegation != delegated.PrefixDelegation || plan.NativeScope != "" && receipt.Scope != plan.NativeScope {
		return raSelfDNSAddress{}, fmt.Errorf("self DNS delegated native identity differs")
	}
	if candidate.IfName == plan.Interface && plan.NativeIndex != 0 && (receipt.IfIndex != plan.NativeIndex || receipt.ParentToken != plan.NativeParentToken) {
		return raSelfDNSAddress{}, fmt.Errorf("self DNS delegated native interface differs")
	}
	entry := receipt.entry(address)
	if !receipt.ownedAt(address, now) {
		return raSelfDNSAddress{}, nil
	}
	valid = min(valid, uint32(remainingIPv6ValidLifetime(now, entry.ValidUntil)))
	prefix, _ := netip.ParsePrefix(address)
	if candidate.IfName != plan.Interface {
		// A different interface has no identity established by this RA socket.
		// Confirm its current address instead of relying only on a saved receipt.
		addresses, err := nativeRAAddresses(ctx, candidate.IfName)
		if err != nil {
			return raSelfDNSAddress{}, err
		}
		found := false
		for _, observed := range addresses {
			if observed.Address != prefix {
				continue
			}
			valid, found = min(valid, observed.Valid), true
			break
		}
		if !found {
			return raSelfDNSAddress{}, nil
		}
	}
	return raSelfDNSAddress{Address: prefix.Addr(), Valid: valid}, nil
}

func currentNativeRASelfDNS(ctx context.Context, candidate raSelfDNSCandidate) (raSelfDNSAddress, error) {
	addresses, err := nativeRAAddresses(ctx, candidate.IfName)
	if err != nil {
		return raSelfDNSAddress{}, err
	}
	sort.Slice(addresses, func(i, j int) bool {
		return addresses[i].Address.Addr().String() < addresses[j].Address.Addr().String()
	})
	ordinal := max(candidate.Spec.Ordinal, 1)
	for _, address := range addresses {
		if candidate.Suffix.IsValid() && !raAddressMatchesSuffix(address.Address.Addr(), candidate.Suffix) {
			continue
		}
		if candidate.Suffix.IsValid() || ordinal == 1 {
			return raSelfDNSAddress{Address: address.Address.Addr(), Valid: address.Valid}, nil
		}
		ordinal--
	}
	return raSelfDNSAddress{}, nil
}

func usableRASelfAddress(address netip.Addr) bool {
	return address.Is6() && !address.Is4In6() && !address.IsMulticast() && !address.IsUnspecified()
}

func raAddressMatchesSuffix(address, suffix netip.Addr) bool {
	left, right := address.As16(), suffix.As16()
	for i, value := range right {
		if value != 0 && value != left[i] {
			return false
		}
	}
	return true
}
