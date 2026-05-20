// SPDX-License-Identifier: BSD-3-Clause

package hostdeps

import (
	"sort"
	"strings"

	"routerd/pkg/api"
)

type NetworkAdoptionResource struct {
	Name string
	Spec api.NetworkAdoptionSpec
}

func PackageResources(router *api.Router) []api.Resource {
	if router == nil {
		return nil
	}
	var out []api.Resource
	for _, resource := range router.Spec.Resources {
		if resource.Kind == "Package" {
			out = append(out, resource)
		}
	}
	out = append(out, DerivedPackageResources(router)...)
	return out
}

func DerivedPackageResources(router *api.Router) []api.Resource {
	sets := PackageSets(router)
	if len(sets) == 0 {
		return nil
	}
	return []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Package"},
		Metadata: api.ObjectMeta{Name: "router-runtime"},
		Spec:     api.PackageSpec{State: "present", Packages: sets},
	}}
}

func PackageSets(router *api.Router) []api.OSPackageSetSpec {
	features := packageFeatures(router)
	if len(features) == 0 {
		return nil
	}
	return []api.OSPackageSetSpec{
		{OS: "ubuntu", Manager: "apt", Names: packageNamesForOS(features, ubuntuPackages)},
		{OS: "debian", Manager: "apt", Names: packageNamesForOS(features, debianPackages)},
		{OS: "nixos", Manager: "nix", Optional: true, Names: packageNamesForOS(features, nixosPackages)},
		{OS: "alpine", Manager: "apk", Optional: true, Names: packageNamesForOS(features, alpinePackages)},
		{OS: "freebsd", Manager: "pkg", Optional: true, Names: packageNamesForOS(features, freebsdPackages)},
	}
}

var ubuntuPackages = map[string][]string{
	"base":      {"iproute2", "systemd"},
	"bgp":       {"frr"},
	"conntrack": {"conntrack"},
	"dhcp-dns":  {"dnsmasq-base"},
	"dpi":       {"libnetfilter-log1", "libndpi-bin"},
	"ipsec":     {"strongswan-swanctl"},
	"kmod":      {"kmod"},
	"nat":       {"nftables"},
	"nft":       {"nftables"},
	"ntp":       {"chrony"},
	"pppoe":     {"ppp"},
	"tailscale": {"tailscale", "tailscale-archive-keyring"},
	"wireguard": {"wireguard-tools"},
}

var debianPackages = ubuntuPackages

var nixosPackages = map[string][]string{
	"base":      {"iproute2", "systemd"},
	"bgp":       {"frr"},
	"conntrack": {"conntrack-tools"},
	"dhcp-dns":  {"dnsmasq"},
	"dpi":       {"libnetfilter_log", "ndpi"},
	"ipsec":     {"strongswan"},
	"kmod":      {"kmod"},
	"nat":       {"nftables"},
	"nft":       {"nftables"},
	"ntp":       {"chrony"},
	"pppoe":     {"ppp"},
	"tailscale": {"tailscale"},
	"wireguard": {"wireguard-tools"},
}

var alpinePackages = map[string][]string{
	"base":      {"iproute2"},
	"bgp":       {"frr"},
	"conntrack": {"conntrack-tools"},
	"dhcp-dns":  {"dnsmasq"},
	"dpi":       {"ndpi"},
	"ipsec":     {"strongswan"},
	"kmod":      {"kmod"},
	"nat":       {"nftables"},
	"nft":       {"nftables"},
	"ntp":       {"chrony"},
	"pppoe":     {"ppp", "ppp-pppoe"},
	"tailscale": {"tailscale"},
	"wireguard": {"wireguard-tools"},
}

var freebsdPackages = map[string][]string{
	"bgp":       {"frr"},
	"dhcp-dns":  {"dnsmasq"},
	"dpi":       {"ndpi"},
	"ipsec":     {"strongswan"},
	"ntp":       {"chrony"},
	"pppoe":     {"mpd5"},
	"tailscale": {"tailscale"},
	"wireguard": {"wireguard-tools"},
}

func packageFeatures(router *api.Router) map[string]bool {
	if router == nil {
		return nil
	}
	features := map[string]bool{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Interface", "Bridge", "VXLANSegment", "VRF", "VXLANTunnel", "IPv4StaticAddress", "IPv6DelegatedAddress", "IPv4Route", "IPv4StaticRoute", "IPv6StaticRoute", "IPv4PolicyRoute", "IPv4PolicyRouteSet", "IPv4DefaultRoutePolicy", "ClusterNetworkRoute", "DSLiteTunnel", "DHCPv4Lease", "DHCPv6Address", "DHCPv6PrefixDelegation", "DHCPv6Information":
			features["base"] = true
		case "BGPRouter", "BGPPeer":
			features["bgp"] = true
		case "DHCPv4Server", "DHCPv4Scope", "DHCPv4Reservation", "DHCPv6Server", "DHCPv6Scope", "IPv6RouterAdvertisement", "DNSResolver", "DNSZone", "DHCPv4Relay":
			features["dhcp-dns"] = true
		case "NAT44Rule", "IPv4SourceNAT":
			features["nat"] = true
			features["conntrack"] = true
		case "FirewallZone", "FirewallPolicy", "FirewallRule", "FirewallLog", "ClientPolicy", "PathMTUPolicy", "IPAddressSet", "LocalServiceRedirect":
			features["nft"] = true
		case "TrafficFlowLog", "ConntrackObserver":
			features["conntrack"] = true
			features["dpi"] = true
		case "NTPClient", "NTPServer":
			features["ntp"] = true
		case "PPPoEInterface", "PPPoESession":
			features["pppoe"] = true
		case "WireGuardInterface", "WireGuardPeer":
			features["wireguard"] = true
			features["kmod"] = true
		case "IPsecConnection":
			features["ipsec"] = true
		case "TailscaleNode":
			features["tailscale"] = true
		}
	}
	if len(KernelModules(router)) > 0 {
		features["kmod"] = true
	}
	return features
}

func packageNamesForOS(features map[string]bool, byFeature map[string][]string) []string {
	seen := map[string]bool{}
	var out []string
	var keys []string
	for feature := range features {
		keys = append(keys, feature)
	}
	sort.Strings(keys)
	for _, feature := range keys {
		for _, name := range byFeature[feature] {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func KernelModuleResources(router *api.Router) []api.Resource {
	if router == nil {
		return nil
	}
	var out []api.Resource
	for _, resource := range router.Spec.Resources {
		if resource.Kind == "KernelModule" {
			out = append(out, resource)
		}
	}
	modules := KernelModules(router)
	if len(modules) == 0 {
		return out
	}
	out = append(out, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "KernelModule"},
		Metadata: api.ObjectMeta{Name: "router-runtime"},
		Spec: api.KernelModuleSpec{
			State:      "present",
			Modules:    modules,
			Runtime:    boolPtr(true),
			Persistent: true,
			Optional:   true,
		},
	})
	return out
}

func KernelModules(router *api.Router) []string {
	if router == nil {
		return nil
	}
	needed := map[string]bool{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "NAT44Rule", "IPv4SourceNAT", "FirewallZone", "FirewallPolicy", "FirewallRule", "ClientPolicy", "ConntrackObserver":
			needed["nf_conntrack"] = true
		case "TrafficFlowLog", "FirewallLog":
			needed["nf_conntrack"] = true
			needed["nfnetlink_log"] = true
		case "WireGuardInterface", "WireGuardPeer":
			needed["wireguard"] = true
		}
	}
	return sortedKeys(needed)
}

func NetworkAdoptionResources(router *api.Router) []api.Resource {
	if router == nil {
		return nil
	}
	var out []api.Resource
	for _, resource := range router.Spec.Resources {
		if resource.Kind == "NetworkAdoption" {
			out = append(out, resource)
		}
	}
	adoptions := NetworkAdoptions(router)
	for _, adoption := range adoptions {
		out = append(out, api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NetworkAdoption"},
			Metadata: api.ObjectMeta{Name: adoption.Name},
			Spec:     adoption.Spec,
		})
	}
	return out
}

func NetworkAdoptions(router *api.Router) []NetworkAdoptionResource {
	if router == nil {
		return nil
	}
	protected := map[string]bool{}
	for _, name := range router.Spec.Apply.ProtectedInterfaces {
		protected[name] = true
	}
	type desired struct {
		disableDHCPv4 bool
		disableDHCPv6 bool
		disableIPv6RA bool
	}
	byInterface := map[string]*desired{}
	ensure := func(name string) *desired {
		name = strings.TrimSpace(name)
		if name == "" || protected[name] {
			return nil
		}
		item := byInterface[name]
		if item == nil {
			item = &desired{}
			byInterface[name] = item
		}
		return item
	}
	resolved := false
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "DHCPv4Lease":
			if spec, err := res.DHCPv4LeaseSpec(); err == nil {
				if item := ensure(spec.Interface); item != nil {
					item.disableDHCPv4 = true
				}
			}
		case "DHCPv4Server":
			if spec, err := res.DHCPv4ServerSpec(); err == nil {
				if item := ensure(spec.Interface); item != nil {
					item.disableDHCPv4 = true
				}
			}
		case "DHCPv6Address":
			if spec, err := res.DHCPv6AddressSpec(); err == nil {
				if item := ensure(spec.Interface); item != nil {
					item.disableDHCPv6 = true
				}
			}
		case "DHCPv6PrefixDelegation":
			if spec, err := res.DHCPv6PrefixDelegationSpec(); err == nil {
				if item := ensure(spec.Interface); item != nil {
					item.disableDHCPv4 = true
					item.disableDHCPv6 = true
				}
			}
		case "DHCPv6Information":
			if spec, err := res.DHCPv6InformationSpec(); err == nil {
				if item := ensure(spec.Interface); item != nil {
					item.disableDHCPv6 = true
				}
			}
		case "DHCPv6Server":
			if spec, err := res.DHCPv6ServerSpec(); err == nil {
				if item := ensure(spec.Interface); item != nil {
					item.disableDHCPv6 = true
				}
			}
		case "IPv6RouterAdvertisement", "IPv6RAAddress":
			if spec, err := interfaceSpecForRouterAdvertisement(res); err == nil {
				if item := ensure(spec); item != nil {
					item.disableIPv6RA = true
				}
			}
		case "DNSResolver":
			resolved = true
		}
	}

	names := sortedKeysPtr(byInterface)
	out := make([]NetworkAdoptionResource, 0, len(names)+1)
	resolvedAttached := false
	for _, name := range names {
		item := byInterface[name]
		spec := api.NetworkAdoptionSpec{
			Interface: name,
			SystemdNetworkd: api.NetworkAdoptionNetworkdSpec{
				DisableDHCPv4: item.disableDHCPv4,
				DisableDHCPv6: item.disableDHCPv6,
				DisableIPv6RA: item.disableIPv6RA,
			},
		}
		if resolved && !resolvedAttached {
			spec.SystemdResolved = api.NetworkAdoptionResolvedSpec{
				DisableDNSStubListener: true,
				DNSServers:             []string{"127.0.0.1"},
			}
			resolvedAttached = true
		}
		out = append(out, NetworkAdoptionResource{Name: name + "-networkd-owned-by-routerd", Spec: spec})
	}
	if resolved && !resolvedAttached {
		out = append(out, NetworkAdoptionResource{
			Name: "resolved-owned-by-routerd",
			Spec: api.NetworkAdoptionSpec{
				SystemdResolved: api.NetworkAdoptionResolvedSpec{
					DisableDNSStubListener: true,
					DNSServers:             []string{"127.0.0.1"},
				},
			},
		})
	}
	return out
}

func interfaceSpecForRouterAdvertisement(res api.Resource) (string, error) {
	if res.Kind == "IPv6RAAddress" {
		spec, err := res.IPv6RAAddressSpec()
		return spec.Interface, err
	}
	spec, err := res.IPv6RouterAdvertisementSpec()
	return spec.Interface, err
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for key, ok := range values {
		if ok {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeysPtr[T any](values map[string]*T) []string {
	out := make([]string, 0, len(values))
	for key, value := range values {
		if value != nil {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func boolPtr(value bool) *bool {
	return &value
}
