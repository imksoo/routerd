// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"encoding/base64"
	"fmt"
	"net/netip"
	"os"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

func validateClusterNetworkRoute(resourceID string, spec api.ClusterNetworkRouteSpec) error {
	var prefixes []netip.Prefix
	for _, item := range []struct {
		path  string
		cidrs []string
	}{
		{path: "spec.pods.cidrs", cidrs: spec.Pods.CIDRs},
		{path: "spec.services.cidrs", cidrs: spec.Services.CIDRs},
	} {
		for i, cidr := range item.cidrs {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s %s[%d] must be an IPv4 CIDR", resourceID, item.path, i)
			}
			prefix = prefix.Masked()
			for _, existing := range prefixes {
				if bgpPrefixesOverlap(existing, prefix) {
					return fmt.Errorf("%s %s[%d] %q overlaps %q", resourceID, item.path, i, prefix.String(), existing.String())
				}
			}
			prefixes = append(prefixes, prefix)
		}
	}
	if len(prefixes) == 0 {
		return fmt.Errorf("%s spec.pods.cidrs or spec.services.cidrs is required", resourceID)
	}
	if len(spec.Via) == 0 {
		return fmt.Errorf("%s spec.via is required", resourceID)
	}
	seenVia := map[string]int{}
	for i, via := range spec.Via {
		if strings.TrimSpace(via.Interface) == "" {
			return fmt.Errorf("%s spec.via[%d].interface is required", resourceID, i)
		}
		nextHop, err := netip.ParseAddr(strings.TrimSpace(via.NextHop))
		if err != nil || !nextHop.Is4() {
			return fmt.Errorf("%s spec.via[%d].nextHop must be an IPv4 address", resourceID, i)
		}
		if via.Weight < 0 || via.Weight > 999 {
			return fmt.Errorf("%s spec.via[%d].weight must be within 0-999", resourceID, i)
		}
		key := strings.TrimSpace(via.Interface) + "|" + nextHop.String()
		if first, ok := seenVia[key]; ok {
			return fmt.Errorf("%s spec.via[%d] duplicates spec.via[%d]", resourceID, i, first)
		}
		seenVia[key] = i
	}
	return nil
}

func validateSecretValueSource(resourceID, plainPath, plain string, sourcePath string, source api.SecretValueSourceSpec) error {
	hasPlain := strings.TrimSpace(plain) != ""
	hasSource := strings.TrimSpace(source.File) != "" || strings.TrimSpace(source.Env) != ""
	if hasPlain && hasSource {
		return fmt.Errorf("%s %s and %s are mutually exclusive", resourceID, plainPath, sourcePath)
	}
	if !hasSource {
		return nil
	}
	if (strings.TrimSpace(source.File) == "") == (strings.TrimSpace(source.Env) == "") {
		return fmt.Errorf("%s %s.file or %s.env must be set, but not both", resourceID, sourcePath, sourcePath)
	}
	value, ok, err := availableSecretSourceValue(source)
	if err != nil {
		return fmt.Errorf("%s %s: %w", resourceID, sourcePath, err)
	}
	if ok && source.Base64 {
		if _, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value)); err != nil {
			return fmt.Errorf("%s %s base64 value is invalid: %w", resourceID, sourcePath, err)
		}
	}
	return nil
}

func availableSecretSourceValue(source api.SecretValueSourceSpec) (string, bool, error) {
	if strings.TrimSpace(source.File) != "" {
		data, err := os.ReadFile(strings.TrimSpace(source.File))
		if err != nil {
			if os.IsNotExist(err) {
				return "", false, nil
			}
			return "", false, err
		}
		return string(data), true, nil
	}
	if strings.TrimSpace(source.Env) != "" {
		value, ok := os.LookupEnv(strings.TrimSpace(source.Env))
		return value, ok, nil
	}
	return "", false, nil
}

func Warnings(router *api.Router) []string {
	if router == nil {
		return nil
	}
	var warnings []string
	dnsZones, dnsResolverZones := dnsZoneCoverage(router)
	wireGuardInterfaces := map[string]bool{}
	interfaces := map[string]bool{}
	cloudProviderProfiles := map[string]api.CloudProviderProfileSpec{}
	for _, res := range router.Spec.Resources {
		if res.APIVersion == api.NetAPIVersion && res.Kind == "Interface" {
			interfaces[res.Metadata.Name] = true
			spec, err := res.InterfaceSpec()
			if err == nil && spec.IfName != "" {
				interfaces[spec.IfName] = true
			}
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "WireGuardInterface" {
			wireGuardInterfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.HybridAPIVersion && res.Kind == "CloudProviderProfile" {
			spec, err := res.CloudProviderProfileSpec()
			if err == nil {
				cloudProviderProfiles[res.Metadata.Name] = spec
			}
		}
	}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "BGPPeer":
			spec, err := res.BGPPeerSpec()
			if err == nil {
				warnings = append(warnings, secretSourceWarnings(res.ID(), "spec.passwordFrom", spec.PasswordFrom)...)
			}
		case "VirtualAddress":
			spec, err := res.VirtualAddressSpec()
			if err == nil {
				warnings = append(warnings, secretSourceWarnings(res.ID(), "spec.vrrp.authenticationFrom", spec.VRRP.AuthenticationFrom)...)
				warnings = append(warnings, hostnameDNSCoverageWarnings(res.ID(), spec.Hostname, spec.ExternalDNS, dnsZones, dnsResolverZones)...)
			}
		case "IngressService":
			spec, err := res.IngressServiceSpec()
			if err == nil {
				warnings = append(warnings, hostnameDNSCoverageWarnings(res.ID(), spec.Hostname, spec.ExternalDNS, dnsZones, dnsResolverZones)...)
			}
		case "OverlayPeer":
			spec, err := res.OverlayPeerSpec()
			if err == nil && spec.Underlay.Type == "wireguard" && spec.Underlay.Interface != "" && !wireGuardInterfaces[spec.Underlay.Interface] {
				warnings = append(warnings, fmt.Sprintf("%s spec.underlay.interface references WireGuardInterface %q which is not declared; assuming the interface is managed externally", res.ID(), spec.Underlay.Interface))
			}
		case "RemoteAddressClaim":
			spec, err := res.RemoteAddressClaimSpec()
			if err != nil {
				continue
			}
			if spec.Capture.Type == "provider-secondary-ip" && spec.Capture.ProviderRef != "" {
				if profile, ok := cloudProviderProfiles[spec.Capture.ProviderRef]; ok && !stringInSlice(spec.Capture.ProviderMode, profile.Capabilities) {
					warnings = append(warnings, fmt.Sprintf("%s spec.capture.providerMode %q is not declared in CloudProviderProfile %q capabilities; the provider profile may not support this capture mode", res.ID(), spec.Capture.ProviderMode, spec.Capture.ProviderRef))
				}
			}
			if spec.Capture.Type == "proxy-arp" && spec.Capture.Interface != "" && !interfaces[spec.Capture.Interface] {
				warnings = append(warnings, fmt.Sprintf("%s spec.capture.interface references Interface %q which is not declared; assuming the interface is managed externally", res.ID(), spec.Capture.Interface))
			}
		}
	}
	return warnings
}

func dnsZoneCoverage(router *api.Router) (map[string]string, map[string]bool) {
	dnsZones := map[string]string{}
	dnsResolverZones := map[string]bool{}
	for _, res := range router.Spec.Resources {
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DNSZone" {
			spec, err := res.DNSZoneSpec()
			if err == nil {
				dnsZones[res.Metadata.Name] = spec.Zone
			}
			continue
		}
		if res.APIVersion != api.NetAPIVersion {
			continue
		}
		switch res.Kind {
		case "DNSResolver":
			spec, err := res.DNSResolverSpec()
			if err != nil {
				continue
			}
			for _, source := range spec.Sources {
				for _, ref := range source.ZoneRef {
					name := refName(ref)
					if name != "" {
						dnsResolverZones[name] = true
					}
				}
			}
		case "DNSForwarder":
			spec, err := res.DNSForwarderSpec()
			if err != nil {
				continue
			}
			for _, ref := range spec.ZoneRefs {
				name := refName(ref)
				if name != "" {
					dnsResolverZones[name] = true
				}
			}
		}
	}
	return dnsZones, dnsResolverZones
}

func hostnameDNSCoverageWarnings(resourceID, hostname string, externalDNS bool, dnsZones map[string]string, dnsResolverZones map[string]bool) []string {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" || externalDNS {
		return nil
	}
	zoneName, ok := dnsHostnameCovered(hostname, dnsZones)
	if !ok {
		return []string{fmt.Sprintf("%s spec.hostname %q is not covered by any DNSZone; routerd will not publish it automatically unless externalDNS is true or a matching DNSZone/DNSResolver source is added", resourceID, hostname)}
	}
	if !dnsResolverZones[zoneName] {
		return []string{fmt.Sprintf("%s spec.hostname %q is covered by DNSZone/%s but no DNSResolver source references that zone; routerd will not publish it automatically unless externalDNS is true or the zone is served", resourceID, hostname, zoneName)}
	}
	return nil
}

func secretSourceWarnings(resourceID, path string, source api.SecretValueSourceSpec) []string {
	file := strings.TrimSpace(source.File)
	if file == "" {
		return nil
	}
	if _, err := os.Stat(file); err != nil && os.IsNotExist(err) {
		return []string{fmt.Sprintf("%s %s.file %q does not exist yet; render/apply will require it", resourceID, path, file)}
	}
	return nil
}
