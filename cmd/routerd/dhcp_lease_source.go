// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"net/netip"
	"slices"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dnsresolver"
)

// The shared dnsmasq process labels each allocating range with its configured
// server tag. Resolve it to exactly one current named resource; tag collisions
// never grant multiple source namespaces to one callback.
func prepareDHCPLeaseSource(router *api.Router, lease *controlapi.DHCPLeaseEventRequest) string {
	if router == nil {
		return "history-only-no-dhcp-source"
	}
	address, err := netip.ParseAddr(lease.IP)
	if err != nil {
		return "history-only-invalid-dhcp-address"
	}
	wantKind := "DHCPv4Server"
	if address.Is6() {
		wantKind = "DHCPv6Server"
	}
	scopeSource := dnsresolver.DHCPSourceForAddress(dnsresolver.DHCPScopes(router), lease.IP)
	var source string
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != wantKind || resource.Metadata.Name == "" {
			continue
		}
		identity := resource.Kind + "/" + resource.Metadata.Name
		if lease.Source != "" && lease.Source != identity {
			continue
		}
		if lease.Source == "" && !slices.Contains(lease.Tags, dhcpServerTag(resource.Metadata.Name)) && !(len(lease.Tags) == 0 && scopeSource == identity) {
			continue
		}
		if source != "" && source != identity {
			return "history-only-ambiguous-dhcp-source"
		}
		source = identity
	}
	if source == "" {
		return "history-only-unverified-dhcp-source"
	}
	lease.Source = source
	if lease.Action != daemonapi.DHCPLeaseActionRemoved && !lease.Infinite && lease.ExpiresAt.IsZero() {
		return "history-only-unverified-dhcp-deadline"
	}
	return ""
}

func dhcpServerTag(name string) string {
	var tag strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			tag.WriteRune(r)
		} else {
			tag.WriteByte('-')
		}
	}
	return tag.String()
}
