// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"net/netip"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

// DHCPAddressScope is configured allocation scope, including explicit reserved
// addresses. A shared dnsmasq lease file carries no server tag: a row may be
// attributed only when exactly one current named server can allocate its IP.
type DHCPAddressScope struct {
	Source string `json:"source"`
	Start  string `json:"start"`
	End    string `json:"end"`
}

func DHCPScopes(router *api.Router) []DHCPAddressScope {
	var scopes []DHCPAddressScope
	if router == nil {
		return nil
	}
	add := func(source, start, end string) {
		if start != "" && end != "" {
			scopes = append(scopes, DHCPAddressScope{Source: source, Start: start, End: end})
		}
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion {
			continue
		}
		source := resource.Kind + "/" + resource.Metadata.Name
		switch resource.Kind {
		case "DHCPv4Server":
			spec, err := resource.DHCPv4ServerSpec()
			if err != nil || spec.Server != "" && spec.Server != "dnsmasq" {
				continue
			}
			start, end := spec.AddressPool.Start, spec.AddressPool.End
			if start == "" {
				start = spec.RangeStart
			}
			if end == "" {
				end = spec.RangeEnd
			}
			add(source, start, end)
			for _, scope := range spec.Scopes {
				pool := scope.AddressPool
				if scope.ProfileRef != "" {
					for _, profile := range spec.Profiles {
						if profile.Name == scope.ProfileRef {
							pool = profile.AddressPool
							break
						}
					}
				}
				add(source, pool.Start, pool.End)
			}
		case "DHCPv6Server":
			spec, err := resource.DHCPv6ServerSpec()
			if err != nil || spec.Server != "" && spec.Server != "dnsmasq" || spec.Mode == "ra-only" {
				continue
			}
			add(source, spec.AddressPool.Start, spec.AddressPool.End)
		}
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "DHCPv4Reservation" {
			continue
		}
		spec, err := resource.DHCPv4ReservationSpec()
		if err != nil {
			continue
		}
		serverName := spec.Server
		if serverName == "" {
			serverName = spec.Scope
		}
		if serverName == "" {
			continue
		}
		source := "DHCPv4Server/" + serverName
		for _, server := range router.Spec.Resources {
			if server.APIVersion == api.NetAPIVersion && server.Kind == "DHCPv4Server" && server.Metadata.Name == serverName {
				add(source, spec.IPAddress, spec.IPAddress)
				break
			}
		}
	}
	return scopes
}

func DHCPSourceForAddress(scopes []DHCPAddressScope, ip string) string {
	address, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	source := ""
	for _, scope := range scopes {
		start, e1 := netip.ParseAddr(scope.Start)
		end, e2 := netip.ParseAddr(scope.End)
		if e1 != nil || e2 != nil || start.BitLen() != address.BitLen() || end.BitLen() != address.BitLen() || address.Compare(start) < 0 || address.Compare(end) > 0 {
			continue
		}
		if source != "" && source != scope.Source {
			return ""
		}
		source = scope.Source
	}
	return source
}

func DHCPSourceKind(source string, ipv6 bool) bool {
	kind, name, ok := strings.Cut(source, "/")
	return ok && name != "" && !strings.Contains(name, "/") && ((!ipv6 && kind == "DHCPv4Server") || (ipv6 && kind == "DHCPv6Server"))
}
