// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"net/netip"
	"strings"

	"routerd/pkg/api"
	"routerd/pkg/platform"
)

func validateDNSResource(res api.Resource, targetOS platform.OS) (bool, error) {
	switch res.Kind {
	case "DNSZone":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DNSZoneSpec()
		if err != nil {
			return true, err
		}
		if spec.Zone == "" {
			return true, fmt.Errorf("%s spec.zone is required", res.ID())
		}
		for i, record := range spec.Records {
			if record.Hostname == "" {
				return true, fmt.Errorf("%s spec.records[%d].hostname is required", res.ID(), i)
			}
			if strings.ContainsAny(record.Hostname, " \t\n,") {
				return true, fmt.Errorf("%s spec.records[%d].hostname is invalid", res.ID(), i)
			}
			if record.IPv4 != "" {
				addr, err := netip.ParseAddr(record.IPv4)
				if err != nil || !addr.Is4() {
					return true, fmt.Errorf("%s spec.records[%d].ipv4 must be an IPv4 address", res.ID(), i)
				}
			}
			if strings.TrimSpace(record.IPv4Source.Field) != "" {
				return true, fmt.Errorf("%s spec.records[%d].ipv4Source was removed; use ipv4From", res.ID(), i)
			}
			if record.IPv4From.Resource != "" && record.IPv4From.Field == "" {
				return true, fmt.Errorf("%s spec.records[%d].ipv4From.field is required", res.ID(), i)
			}
			if record.IPv6 != "" {
				addr, err := netip.ParseAddr(record.IPv6)
				if err != nil || !addr.Is6() {
					return true, fmt.Errorf("%s spec.records[%d].ipv6 must be an IPv6 address", res.ID(), i)
				}
			}
			if strings.TrimSpace(record.IPv6Source.Field) != "" {
				return true, fmt.Errorf("%s spec.records[%d].ipv6Source was removed; use ipv6From", res.ID(), i)
			}
			if record.IPv6From.Resource != "" && record.IPv6From.Field == "" {
				return true, fmt.Errorf("%s spec.records[%d].ipv6From.field is required", res.ID(), i)
			}
		}
	case "DNSResolver":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DNSResolverSpec()
		if err != nil {
			return true, err
		}
		if err := validateDNSResolverCore(spec); err != nil {
			return true, fmt.Errorf("%s: %w", res.ID(), err)
		}
		for i, listen := range spec.Listen {
			if len(listen.AddressSources) > 0 {
				return true, fmt.Errorf("%s spec.listen[%d].addressSources was removed; use addressFrom", res.ID(), i)
			}
		}
	case "DNSForwarder":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DNSForwarderSpec()
		if err != nil {
			return true, err
		}
		if strings.TrimSpace(spec.Resolver) == "" {
			return true, fmt.Errorf("%s spec.resolver is required", res.ID())
		}
		if len(spec.Match) == 0 {
			return true, fmt.Errorf("%s spec.match is required", res.ID())
		}
		if len(spec.ZoneRefs) > 0 && len(spec.Upstreams) > 0 {
			return true, fmt.Errorf("%s spec.zoneRefs and spec.upstreams cannot both be set", res.ID())
		}
		if len(spec.ZoneRefs) == 0 && len(spec.Upstreams) == 0 {
			return true, fmt.Errorf("%s requires either spec.zoneRefs or spec.upstreams", res.ID())
		}
		for i, match := range spec.Match {
			if strings.TrimSpace(match) == "" {
				return true, fmt.Errorf("%s spec.match[%d] is required", res.ID(), i)
			}
		}
		if err := validateDNSResolverHealthcheck(res.ID(), spec.Healthcheck); err != nil {
			return true, err
		}
	case "DNSUpstream":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DNSUpstreamSpec()
		if err != nil {
			return true, err
		}
		if err := validateDNSUpstream(res.ID(), spec); err != nil {
			return true, err
		}
	default:
		return false, nil
	}
	return true, nil
}
