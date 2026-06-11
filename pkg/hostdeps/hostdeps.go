// SPDX-License-Identifier: BSD-3-Clause

package hostdeps

import (
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/sam"
	"github.com/imksoo/routerd/pkg/sysctlprofile"
)

type NetworkAdoptionResource struct {
	Name string
	Spec api.NetworkAdoptionSpec
}

type sysctlResource struct {
	Name string
	Spec api.SysctlSpec
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
	"arping":        {"iputils-arping"},
	"base":          {"iproute2", "systemd"},
	"bgp":           {},
	"cloud-aws-cli": {"awscli"},
	"conntrack":     {"conntrack"},
	"dhcp-dns":      {"dnsmasq-base"},
	"dpi":           {"libnetfilter-log1", "libndpi-bin"},
	"ipsec":         {"strongswan-swanctl"},
	"kmod":          {"kmod"},
	"nat":           {"nftables"},
	"nft":           {"nftables"},
	"ntp":           {"chrony"},
	"pppoe":         {"ppp"},
	"network-utils": {"iputils-ping", "dnsutils", "traceroute"},
	"tailscale":     {"tailscale", "tailscale-archive-keyring"},
	"vrrp":          {"keepalived"},
	"wireguard":     {"wireguard-tools"},
}

var debianPackages = ubuntuPackages

var nixosPackages = map[string][]string{
	"arping":        {"iputils"},
	"base":          {"iproute2", "systemd"},
	"bgp":           {},
	"cloud-aws-cli": {"awscli2"},
	"conntrack":     {"conntrack-tools"},
	"dhcp-dns":      {"dnsmasq"},
	"dpi":           {"libnetfilter_log", "ndpi"},
	"ipsec":         {"strongswan"},
	"kmod":          {"kmod"},
	"nat":           {"nftables"},
	"nft":           {"nftables"},
	"ntp":           {"chrony"},
	"pppoe":         {"ppp"},
	"network-utils": {"iputils", "dnsutils", "traceroute"},
	"tailscale":     {"tailscale"},
	"vrrp":          {"keepalived"},
	"wireguard":     {"wireguard-tools"},
}

var alpinePackages = map[string][]string{
	"arping":        {"iputils"},
	"base":          {"iproute2"},
	"bgp":           {},
	"conntrack":     {"conntrack-tools"},
	"dhcp-dns":      {"dnsmasq"},
	"dpi":           {"ndpi"},
	"ipsec":         {"strongswan"},
	"kmod":          {"kmod"},
	"nat":           {"nftables"},
	"nft":           {"nftables"},
	"ntp":           {"chrony"},
	"pppoe":         {"ppp", "ppp-pppoe"},
	"network-utils": {"iputils", "bind-tools", "traceroute"},
	"tailscale":     {"tailscale"},
	"vrrp":          {"keepalived"},
	"wireguard":     {"wireguard-tools"},
}

var freebsdPackages = map[string][]string{
	"bgp":           {},
	"dhcp-dns":      {"dnsmasq"},
	"dpi":           {"ndpi"},
	"ipsec":         {"strongswan"},
	"ntp":           {"chrony"},
	"pppoe":         {"mpd5"},
	"network-utils": {"bind-tools"},
	"tailscale":     {"tailscale"},
	"wireguard":     {"wireguard-tools"},
}

func packageFeatures(router *api.Router) map[string]bool {
	if router == nil {
		return nil
	}
	features := map[string]bool{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Interface", "Bridge", "VXLANSegment", "VRF", "VXLANTunnel", "IPv4StaticAddress", "IPv6DelegatedAddress", "VirtualAddress", "IPv4Route", "IPv4StaticRoute", "IPv6StaticRoute", "ClusterNetworkRoute", "DHCPv4Client", "DHCPv6Address", "DHCPv6PrefixDelegation", "DHCPv6Information":
			features["base"] = true
			if res.Kind == "VirtualAddress" {
				if spec, err := res.VirtualAddressSpec(); err == nil && spec.Mode == "vrrp" {
					features["vrrp"] = true
				}
			}
		case "DSLiteTunnel":
			features["base"] = true
			features["nft"] = true
		case "EgressRoutePolicy":
			features["base"] = true
			features["nft"] = true
		case "BGPRouter", "BGPPeer", "BFD":
			features["bgp"] = true
		case "DHCPv4Server", "DHCPv4Reservation", "DHCPv6Server", "IPv6RouterAdvertisement", "DNSResolver", "DNSForwarder", "DNSUpstream", "DNSZone", "DHCPv4Relay":
			features["dhcp-dns"] = true
		case "NAT44Rule":
			features["nat"] = true
			features["conntrack"] = true
		case "FirewallZone", "FirewallPolicy", "FirewallRule", "FirewallEventLog", "ClientPolicy", "IPAddressSet", "PortForward", "IngressService", "LocalServiceRedirect":
			features["nft"] = true
		case "TrafficFlowLog", "ConntrackObserver":
			features["conntrack"] = true
			features["dpi"] = true
		case "NTPClient", "NTPServer":
			features["ntp"] = true
		case "HealthCheck":
			features["network-utils"] = true
		case "PPPoESession":
			features["pppoe"] = true
			features["nft"] = true
		case "WireGuardInterface", "WireGuardPeer":
			features["wireguard"] = true
			features["kmod"] = true
			features["nft"] = true
		case "TunnelInterface":
			features["base"] = true
			features["kmod"] = true
		case "IPsecConnection":
			features["ipsec"] = true
		case "TailscaleNode":
			features["tailscale"] = true
		case "RemoteAddressClaim":
			if spec, err := res.RemoteAddressClaimSpec(); err == nil && spec.Capture.Type == "proxy-arp" && (spec.Capture.GratuitousARP || spec.Capture.ActiveWhen.Type == "vrrp-master") {
				features["arping"] = true
			}
		case "CloudProviderProfile":
			spec, err := res.CloudProviderProfileSpec()
			if err != nil {
				continue
			}
			if strings.TrimSpace(spec.Provider) == "aws" {
				features["cloud-aws-cli"] = true
			}
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
		case "NAT44Rule", "FirewallZone", "FirewallPolicy", "FirewallRule", "ClientPolicy", "ConntrackObserver":
			needed["nf_conntrack"] = true
		case "TrafficFlowLog", "FirewallEventLog":
			needed["nf_conntrack"] = true
			needed["nfnetlink_log"] = true
		case "WireGuardInterface", "WireGuardPeer":
			needed["wireguard"] = true
		case "TunnelInterface":
			spec, err := res.TunnelInterfaceSpec()
			if err != nil {
				continue
			}
			switch spec.Mode {
			case "ipip":
				needed["ipip"] = true
			case "gre":
				needed["ip_gre"] = true
			case "fou", "gue":
				needed["ipip"] = true
				needed["fou"] = true
			}
		}
	}
	return sortedKeys(needed)
}

func SysctlResources(router *api.Router) []api.Resource {
	if router == nil {
		return nil
	}
	var out []api.Resource
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "Sysctl", "SysctlProfile":
			out = append(out, resource)
		}
	}
	out = append(out, DerivedSysctlResources(router)...)
	return out
}

func DerivedSysctlResources(router *api.Router) []api.Resource {
	if router == nil {
		return nil
	}
	if platform.CurrentOS() != platform.OSLinux {
		return nil
	}
	explicit := explicitSysctlKeys(router)
	var out []api.Resource
	if spec, ok := derivedRouterProfile(router); ok {
		entries, err := sysctlprofile.Entries(spec.Profile, spec.Overrides)
		if err == nil {
			if intersectsEntryKeys(entries, explicit) {
				for _, entry := range entries {
					if explicit[entry.Key] {
						continue
					}
					out = append(out, sysctlResourceFor("router-runtime-"+safeResourceName(entry.Key), sysctlSpecFromEntry(entry, spec.Runtime, spec.Persistent)))
				}
			} else {
				out = append(out, api.Resource{
					TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "SysctlProfile"},
					Metadata: api.ObjectMeta{Name: "router-runtime"},
					Spec:     spec,
				})
				for _, entry := range entries {
					explicit[entry.Key] = true
				}
			}
		}
	}
	for _, setting := range derivedSAMSysctls(router) {
		if explicit[setting.Spec.Key] {
			continue
		}
		out = append(out, sysctlResourceFor(setting.Name, setting.Spec))
		explicit[setting.Spec.Key] = true
	}
	for _, setting := range derivedInterfaceSysctls(router) {
		if explicit[setting.Spec.Key] {
			continue
		}
		out = append(out, sysctlResourceFor(setting.Name, setting.Spec))
		explicit[setting.Spec.Key] = true
	}
	return out
}

func derivedSAMSysctls(router *api.Router) []sysctlResource {
	if router == nil || !sam.HasRemoteAddressClaims(router) {
		return nil
	}
	var out []sysctlResource
	out = append(out, sysctlResource{
		Name: "sam-ip-forward",
		Spec: api.SysctlSpec{
			Key:        "net.ipv4.ip_forward",
			Value:      "1",
			Runtime:    boolPtr(true),
			Persistent: true,
		},
	})
	for _, iface := range sam.ProxyARPInterfaces(router) {
		out = append(out, sysctlResource{
			Name: "sam-proxy-arp-" + safeResourceName(iface),
			Spec: api.SysctlSpec{
				Key:      "net.ipv4.conf." + iface + ".proxy_arp",
				Value:    "1",
				Runtime:  boolPtr(true),
				Optional: true,
			},
		})
	}
	return out
}

func derivedRouterProfile(router *api.Router) (api.SysctlProfileSpec, bool) {
	if !sysctlprofile.NeedsForwarding(router) {
		return api.SysctlProfileSpec{}, false
	}
	return api.SysctlProfileSpec{
		Profile:    "router-linux",
		Runtime:    boolPtr(true),
		Persistent: true,
		Overrides: map[string]string{
			"net.netfilter.nf_conntrack_udp_timeout": "60",
		},
	}, true
}

func derivedInterfaceSysctls(router *api.Router) []sysctlResource {
	aliases := interfaceAliases(router)
	var out []sysctlResource
	for _, tunnel := range ipv4TunnelInterfaceNames(router) {
		out = append(out, sysctlResource{
			Name: "rp-filter-" + safeResourceName(tunnel),
			Spec: api.SysctlSpec{
				Key:      "net.ipv4.conf." + tunnel + ".rp_filter",
				Value:    "0",
				Runtime:  boolPtr(true),
				Optional: true,
			},
		})
	}
	for _, iface := range raAcceptInterfaceNames(router, aliases) {
		out = append(out, sysctlResource{
			Name: "accept-ra-" + safeResourceName(iface),
			Spec: api.SysctlSpec{
				Key:        "net.ipv6.conf." + iface + ".accept_ra",
				Value:      "2",
				Runtime:    boolPtr(true),
				Persistent: true,
			},
		})
	}
	for _, iface := range routedInterfaceNames(router, aliases) {
		out = append(out, sysctlResource{
			Name: "disable-ra-default-route-" + safeResourceName(iface),
			Spec: api.SysctlSpec{
				Key:        "net.ipv6.conf." + iface + ".accept_ra_defrtr",
				Value:      "0",
				Runtime:    boolPtr(true),
				Persistent: true,
				Optional:   true,
			},
		})
	}
	for _, iface := range routedInterfaceNames(router, aliases) {
		out = append(out, sysctlResource{
			Name: "disable-send-redirects-" + safeResourceName(iface),
			Spec: api.SysctlSpec{
				Key:        "net.ipv4.conf." + iface + ".send_redirects",
				Value:      "0",
				Runtime:    boolPtr(true),
				Persistent: true,
				Optional:   true,
			},
		})
	}
	return out
}

func ipv4TunnelInterfaceNames(router *api.Router) []string {
	names := map[string]bool{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "DSLiteTunnel":
			spec, err := res.DSLiteTunnelSpec()
			if err != nil {
				continue
			}
			name := strings.TrimSpace(spec.TunnelName)
			if name == "" {
				name = res.Metadata.Name
			}
			if name != "" {
				names[name] = true
			}
		case "PPPoESession":
			spec, err := res.PPPoESessionSpec()
			if err != nil {
				continue
			}
			name := strings.TrimSpace(spec.IfName)
			if name == "" {
				name = "ppp-" + res.Metadata.Name
			}
			if name != "" {
				names[name] = true
			}
		case "WireGuardInterface":
			if res.Metadata.Name != "" {
				names[res.Metadata.Name] = true
			}
		}
	}
	return sortedKeys(names)
}

func raAcceptInterfaceNames(router *api.Router, aliases map[string]string) []string {
	names := map[string]bool{}
	add := func(name string) {
		if resolved := resolveInterfaceName(name, aliases); resolved != "" {
			names[resolved] = true
		}
	}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "DHCPv6Address":
			if spec, err := res.DHCPv6AddressSpec(); err == nil {
				add(spec.Interface)
			}
		case "DHCPv6Information":
			if spec, err := res.DHCPv6InformationSpec(); err == nil {
				add(spec.Interface)
			}
		case "DHCPv6PrefixDelegation":
			if spec, err := res.DHCPv6PrefixDelegationSpec(); err == nil {
				add(spec.Interface)
			}
		case "IPv6RAAddress":
			if spec, err := res.IPv6RAAddressSpec(); err == nil {
				add(spec.Interface)
			}
		case "IPv6DelegatedAddress":
			if spec, err := res.IPv6DelegatedAddressSpec(); err == nil {
				add(spec.Interface)
			}
		}
	}
	return sortedKeys(names)
}

func routedInterfaceNames(router *api.Router, aliases map[string]string) []string {
	names := map[string]bool{}
	add := func(name string) {
		if resolved := resolveInterfaceName(name, aliases); resolved != "" && resolved != "lo" {
			names[resolved] = true
		}
	}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "DHCPv4Server":
			if spec, err := res.DHCPv4ServerSpec(); err == nil {
				add(spec.Interface)
			}
		case "DHCPv6Server":
			if spec, err := res.DHCPv6ServerSpec(); err == nil {
				add(spec.Interface)
			}
		case "IPv6RouterAdvertisement":
			if spec, err := res.IPv6RouterAdvertisementSpec(); err == nil {
				add(spec.Interface)
			}
		case "IPv6DelegatedAddress":
			if spec, err := res.IPv6DelegatedAddressSpec(); err == nil && (spec.SendRA || spec.Announce) {
				add(spec.Interface)
			}
		}
	}
	return sortedKeys(names)
}

func interfaceAliases(router *api.Router) map[string]string {
	aliases := map[string]string{}
	if router == nil {
		return aliases
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "Interface" {
			continue
		}
		spec, err := res.InterfaceSpec()
		if err != nil {
			continue
		}
		if res.Metadata.Name != "" && spec.IfName != "" {
			aliases[res.Metadata.Name] = spec.IfName
		}
	}
	return aliases
}

func resolveInterfaceName(name string, aliases map[string]string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if resolved := strings.TrimSpace(aliases[name]); resolved != "" {
		return resolved
	}
	return name
}

func explicitSysctlKeys(router *api.Router) map[string]bool {
	keys := map[string]bool{}
	if router == nil {
		return keys
	}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Sysctl":
			spec, err := res.SysctlSpec()
			if err == nil && spec.Key != "" {
				keys[spec.Key] = true
			}
		case "SysctlProfile":
			spec, err := res.SysctlProfileSpec()
			if err != nil {
				continue
			}
			entries, err := sysctlprofile.Entries(spec.Profile, spec.Overrides)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				keys[entry.Key] = true
			}
		}
	}
	return keys
}

func intersectsEntryKeys(entries []sysctlprofile.Entry, keys map[string]bool) bool {
	for _, entry := range entries {
		if keys[entry.Key] {
			return true
		}
	}
	return false
}

func sysctlSpecFromEntry(entry sysctlprofile.Entry, runtime *bool, persistent bool) api.SysctlSpec {
	return api.SysctlSpec{
		Key:        entry.Key,
		Value:      entry.Value,
		Compare:    entry.Compare,
		Runtime:    runtime,
		Persistent: persistent,
		Optional:   entry.Optional,
	}
}

func sysctlResourceFor(name string, spec api.SysctlSpec) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     spec,
	}
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
		case "DHCPv4Client":
			if spec, err := res.DHCPv4ClientSpec(); err == nil {
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

func safeResourceName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.NewReplacer("/", "-", " ", "-", "\t", "-", "\n", "-").Replace(name)
	name = strings.Trim(name, "-.")
	if name == "" {
		return "default"
	}
	return name
}

func boolPtr(value bool) *bool {
	return &value
}
