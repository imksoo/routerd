// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"gopkg.in/yaml.v3"
)

type importInterfaces []string

func (v *importInterfaces) String() string { return strings.Join(*v, ", ") }
func (v *importInterfaces) Set(value string) error {
	if value == "" || filepath.Base(value) != value || value == "." || value == ".." {
		return fmt.Errorf("invalid interface name %q", value)
	}
	for _, existing := range *v {
		if existing == value {
			return nil
		}
	}
	*v = append(*v, value)
	return nil
}

type importAddress struct {
	Family            string          `json:"family"`
	Local             string          `json:"local"`
	PrefixLen         int             `json:"prefixlen"`
	Scope             string          `json:"scope"`
	Dynamic           bool            `json:"dynamic"`
	Peer              string          `json:"peer"`
	Label             string          `json:"label"`
	Broadcast         string          `json:"broadcast"`
	Metric            int             `json:"metric"`
	NoPrefixRoute     bool            `json:"noprefixroute"`
	ValidLifetime     *uint64         `json:"valid_life_time"`
	PreferredLifetime *uint64         `json:"preferred_life_time"`
	Unsupported       []string        `json:"-"`
	Raw               json.RawMessage `json:"-"`
}

func (address *importAddress) UnmarshalJSON(data []byte) error {
	type wireAddress importAddress
	var decoded wireAddress
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for key := range fields {
		switch key {
		case "family", "local", "prefixlen", "scope", "dynamic", "peer", "label", "broadcast", "metric", "noprefixroute", "valid_life_time", "preferred_life_time":
		default:
			decoded.Unsupported = append(decoded.Unsupported, key)
		}
	}
	sort.Strings(decoded.Unsupported)
	decoded.Raw = append(json.RawMessage(nil), data...)
	*address = importAddress(decoded)
	return nil
}

type importLink struct {
	IfIndex  int             `json:"ifindex"`
	Address  string          `json:"address"`
	IfName   string          `json:"ifname"`
	Flags    []string        `json:"flags"`
	MTU      int             `json:"mtu"`
	Master   string          `json:"master"`
	AddrInfo []importAddress `json:"addr_info"`
}

type networkdImportStatus struct {
	NetworkFile        string   `json:"NetworkFile"`
	NetworkFileDropins []string `json:"NetworkFileDropins"`
	Addresses          []struct {
		Address      []int  `json:"Address"`
		PrefixLength int    `json:"PrefixLength"`
		ConfigSource string `json:"ConfigSource"`
	} `json:"Addresses"`
	DNS []struct {
		Address      []int  `json:"Address"`
		ConfigSource string `json:"ConfigSource"`
	} `json:"DNS"`
	SearchDomains []struct {
		Domain       string `json:"Domain"`
		ConfigSource string `json:"ConfigSource"`
		RouteOnly    bool   `json:"RouteOnly"`
	} `json:"SearchDomains"`
	Routes []struct {
		Family                  int    `json:"Family"`
		Destination             []int  `json:"Destination"`
		DestinationPrefixLength int    `json:"DestinationPrefixLength"`
		Gateway                 []int  `json:"Gateway"`
		PreferredSource         []int  `json:"PreferredSource"`
		Priority                int    `json:"Priority"`
		Table                   int    `json:"Table"`
		Flags                   int    `json:"Flags"`
		Type                    int    `json:"Type"`
		Scope                   int    `json:"Scope"`
		TOS                     int    `json:"TOS"`
		ProtocolString          string `json:"ProtocolString"`
		ConfigSource            string `json:"ConfigSource"`
		ConfigState             string `json:"ConfigState"`
	} `json:"Routes"`
}

func networkdAddressSources(ifname string) (map[string]string, map[string]bool, []string, *api.InterfaceDNSSpec, error) {
	out, err := exec.Command("networkctl", "--json=short", "status", ifname).Output()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var status networkdImportStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return nil, nil, nil, nil, err
	}
	sources := make(map[string]string, len(status.Addresses))
	for _, address := range status.Addresses {
		bytes := make([]byte, len(address.Address))
		for i, value := range address.Address {
			if value < 0 || value > 255 {
				return nil, nil, nil, nil, fmt.Errorf("invalid networkd address byte %d", value)
			}
			bytes[i] = byte(value)
		}
		ip, ok := netip.AddrFromSlice(bytes)
		if !ok || address.PrefixLength < 0 || address.PrefixLength > ip.BitLen() {
			return nil, nil, nil, nil, fmt.Errorf("invalid networkd address or prefix length")
		}
		sources[netip.PrefixFrom(ip, address.PrefixLength).String()] = address.ConfigSource
	}
	staticRoutes := map[string]bool{}
	for _, route := range status.Routes {
		if route.Family != 2 || route.ConfigSource != "static" || route.ConfigState != "configured" || route.ProtocolString != "static" || route.Table != 254 || route.Type != 1 || route.Scope != 0 || route.TOS != 0 || (route.Flags != 0 && route.Flags != 4) || len(route.PreferredSource) != 0 {
			continue
		}
		destination, ok := importIPv4Bytes(route.Destination)
		if !ok || route.DestinationPrefixLength < 0 || route.DestinationPrefixLength > 32 {
			continue
		}
		gateway, ok := importIPv4Bytes(route.Gateway)
		if !ok {
			continue
		}
		prefix := netip.PrefixFrom(destination, route.DestinationPrefixLength).Masked().String()
		staticRoutes[importRouteKey(prefix, gateway.String(), route.Priority, route.Flags == 4)] = true
	}
	var files []string
	if status.NetworkFile != "" {
		files = append(files, status.NetworkFile)
		files = append(files, status.NetworkFileDropins...)
	}
	var dns api.InterfaceDNSSpec
	for _, server := range status.DNS {
		if server.ConfigSource != "static" {
			continue
		}
		bytes := make([]byte, len(server.Address))
		for i, value := range server.Address {
			if value < 0 || value > 255 {
				return nil, nil, nil, nil, fmt.Errorf("invalid networkd DNS address byte %d", value)
			}
			bytes[i] = byte(value)
		}
		ip, ok := netip.AddrFromSlice(bytes)
		if !ok {
			return nil, nil, nil, nil, errors.New("invalid networkd DNS address")
		}
		dns.Servers = append(dns.Servers, ip.String())
	}
	for _, domain := range status.SearchDomains {
		if domain.ConfigSource == "static" && domain.Domain != "" {
			value := domain.Domain
			if domain.RouteOnly {
				value = "~" + value
			}
			dns.Domains = append(dns.Domains, value)
		}
	}
	if len(dns.Servers) == 0 && len(dns.Domains) == 0 {
		return sources, staticRoutes, files, nil, nil
	}
	return sources, staticRoutes, files, &dns, nil
}

func importIPv4Bytes(values []int) (netip.Addr, bool) {
	if len(values) != 4 {
		return netip.Addr{}, false
	}
	var bytes [4]byte
	for i, value := range values {
		if value < 0 || value > 255 {
			return netip.Addr{}, false
		}
		bytes[i] = byte(value)
	}
	return netip.AddrFrom4(bytes), true
}

func importRouteKey(destination, gateway string, metric int, onLink bool) string {
	return fmt.Sprintf("%s|%s|%d|%t", destination, gateway, metric, onLink)
}

func importDerivedIPv4ConnectedRoute(raw json.RawMessage, ifname string, addresses []importAddress, sources map[string]string, importedStatic map[string]bool) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	for key := range fields {
		switch key {
		case "dst", "dev", "protocol", "scope", "prefsrc", "metric", "flags":
		default:
			return false
		}
	}
	var route struct {
		Destination     string   `json:"dst"`
		Device          string   `json:"dev"`
		Protocol        string   `json:"protocol"`
		Scope           string   `json:"scope"`
		PreferredSource string   `json:"prefsrc"`
		Flags           []string `json:"flags"`
	}
	if json.Unmarshal(raw, &route) != nil || route.Protocol != "kernel" || route.Scope != "link" || (route.Device != "" && route.Device != ifname) || len(route.Flags) != 0 {
		return false
	}
	destination, err := netip.ParsePrefix(route.Destination)
	if err != nil || !destination.Addr().Is4() || destination != destination.Masked() {
		return false
	}
	for _, address := range addresses {
		ip, err := netip.ParseAddr(address.Local)
		if err != nil || !ip.Is4() || address.NoPrefixRoute || address.PrefixLen < 0 || address.PrefixLen > 32 || route.PreferredSource != ip.String() {
			continue
		}
		prefix := netip.PrefixFrom(ip, address.PrefixLen)
		if prefix.Masked() == destination && (sources[prefix.String()] == "DHCPv4" || importedStatic[prefix.String()]) {
			return true
		}
	}
	return false
}

func importKernelStaticIPv4Route(raw json.RawMessage, ifname string) (api.IPv4RouteSpec, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return api.IPv4RouteSpec{}, false
	}
	for key := range fields {
		switch key {
		case "dst", "gateway", "dev", "protocol", "metric", "flags", "table", "type", "scope":
		default:
			return api.IPv4RouteSpec{}, false
		}
	}
	var route struct {
		Destination string          `json:"dst"`
		Gateway     string          `json:"gateway"`
		Device      string          `json:"dev"`
		Protocol    string          `json:"protocol"`
		Metric      int             `json:"metric"`
		Flags       []string        `json:"flags"`
		Table       json.RawMessage `json:"table"`
		Type        string          `json:"type"`
		Scope       string          `json:"scope"`
	}
	if json.Unmarshal(raw, &route) != nil || (route.Protocol != "" && route.Protocol != "static") || (route.Device != "" && route.Device != ifname) || (route.Type != "" && route.Type != "unicast") || (route.Scope != "" && route.Scope != "global" && !(route.Scope == "link" && route.Gateway == "")) || (len(route.Table) != 0 && string(route.Table) != `"main"` && string(route.Table) != "254") {
		return api.IPv4RouteSpec{}, false
	}
	destination := route.Destination
	if destination == "default" {
		destination = "0.0.0.0/0"
	}
	if address, err := netip.ParseAddr(destination); err == nil && address.Is4() {
		destination = netip.PrefixFrom(address, 32).String()
	}
	prefix, err := netip.ParsePrefix(destination)
	if err != nil || !prefix.Addr().Is4() {
		return api.IPv4RouteSpec{}, false
	}
	gateway := ""
	if route.Gateway != "" {
		address, err := netip.ParseAddr(route.Gateway)
		if err != nil || !address.Is4() {
			return api.IPv4RouteSpec{}, false
		}
		gateway = address.String()
	}
	onLink := false
	for _, flag := range route.Flags {
		if flag != "onlink" || onLink {
			return api.IPv4RouteSpec{}, false
		}
		onLink = true
	}
	return api.IPv4RouteSpec{Destination: prefix.Masked().String(), Device: ifname, Gateway: gateway, Metric: route.Metric, GatewayOnLink: onLink}, true
}

func importStaticIPv4Address(address importAddress, ifname string, ip netip.Addr) bool {
	if !ip.Is4() || address.Scope != "global" || address.Dynamic || address.Peer != "" || address.NoPrefixRoute || address.Metric != 0 || len(address.Unsupported) != 0 || (address.Label != "" && address.Label != ifname) {
		return false
	}
	if address.PrefixLen < 0 || address.PrefixLen > 32 || address.ValidLifetime == nil || address.PreferredLifetime == nil || *address.ValidLifetime != 0xffffffff || *address.PreferredLifetime != 0xffffffff {
		return false
	}
	if address.Broadcast != "" {
		broadcast, err := netip.ParseAddr(address.Broadcast)
		if err != nil || !broadcast.Is4() {
			return false
		}
		prefix := netip.PrefixFrom(ip, address.PrefixLen).Masked()
		network := prefix.Addr().As4()
		mask := uint32(0xffffffff) >> uint(address.PrefixLen)
		expected := binary.BigEndian.Uint32(network[:]) | mask
		actual := broadcast.As4()
		if binary.BigEndian.Uint32(actual[:]) != expected {
			return false
		}
	}
	return true
}

func importStaticIPv6Address(address importAddress, ifname string, ip netip.Addr) bool {
	return ip.Is6() && ip.IsGlobalUnicast() && address.Scope == "global" && !address.Dynamic && address.Peer == "" && address.Broadcast == "" && !address.NoPrefixRoute && address.Metric == 0 && len(address.Unsupported) == 0 && (address.Label == "" || address.Label == ifname) && address.PrefixLen >= 0 && address.PrefixLen <= 128 && address.ValidLifetime != nil && address.PreferredLifetime != nil && *address.ValidLifetime == 0xffffffff && *address.PreferredLifetime == 0xffffffff
}

func networkdDHCPv4Enabled(files []string) (bool, error) {
	enabled := false
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		section := ""
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				section = strings.TrimSpace(line[1 : len(line)-1])
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok || section != "Network" || strings.TrimSpace(key) != "DHCP" {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "yes", "true", "1", "on", "ipv4":
				enabled = true
			case "", "no", "false", "0", "off", "ipv6":
				enabled = false
			default:
				return false, fmt.Errorf("%s: unsupported DHCP=%q", path, value)
			}
		}
	}
	return enabled, nil
}

func networkdDHCPv4Options(files []string) (useRoutes, useDNS *bool, metric int, useMTU bool, hostname string, err error) {
	if len(files) == 0 {
		return nil, nil, 0, false, "", errors.New("networkd configuration file is unavailable")
	}
	sendHostname := true
	for _, path := range files {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, nil, 0, false, "", readErr
		}
		section := ""
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				section = strings.TrimSpace(line[1 : len(line)-1])
				continue
			}
			if section != "DHCP" && section != "DHCPv4" {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
				continue
			}
			key, value = strings.TrimSpace(key), strings.TrimSpace(value)
			switch key {
			case "UseRoutes", "UseDNS", "UseMTU", "SendHostname":
				var v bool
				switch strings.ToLower(value) {
				case "yes", "true", "1", "on":
					v = true
				case "no", "false", "0", "off":
					v = false
				default:
					return nil, nil, 0, false, "", fmt.Errorf("%s: invalid %s=%q", path, key, value)
				}
				if key == "UseRoutes" {
					useRoutes = &v
				} else if key == "UseDNS" {
					useDNS = &v
				} else if key == "UseMTU" {
					useMTU = v
				} else {
					sendHostname = v
				}
			case "Hostname":
				hostname = value
			case "RouteMetric":
				n, parseErr := strconv.Atoi(value)
				if parseErr != nil || n < 0 {
					return nil, nil, 0, false, "", fmt.Errorf("%s: invalid RouteMetric=%q", path, value)
				}
				metric = n
			}
		}
	}
	if sendHostname {
		if hostname == "" {
			hostname, err = os.Hostname()
			if err != nil {
				return nil, nil, 0, false, "", err
			}
		}
		if len(hostname) > 63 || strings.HasPrefix(hostname, "-") || strings.HasSuffix(hostname, "-") || strings.Trim(hostname, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
			hostname = ""
		}
	} else {
		hostname = ""
	}
	return useRoutes, useDNS, metric, useMTU, hostname, nil
}

func networkdDHCPv4ClientID(ifindex int, address string) (string, error) {
	if ifindex <= 0 || address == "" {
		return "", errors.New("DHCPv4 lease interface or address is unavailable")
	}
	data, err := os.ReadFile(filepath.Join("/run/systemd/netif/leases", fmt.Sprint(ifindex)))
	if err != nil {
		return "", err
	}
	var leaseAddress, clientID string
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "ADDRESS":
			leaseAddress = value
		case "CLIENTID":
			clientID = value
		}
	}
	if leaseAddress != address {
		return "", fmt.Errorf("networkd lease address does not match the observed DHCPv4 address")
	}
	decoded, err := hex.DecodeString(clientID)
	if err != nil || len(decoded) == 0 || len(decoded) > 255 {
		return "", errors.New("networkd lease has no valid DHCPv4 client identifier")
	}
	return hex.EncodeToString(decoded), nil
}

func configuredDHCPv4ClientID(spec api.DHCPv4ClientSpec, hardwareAddress string) string {
	if spec.ClientIDHex != "" {
		return strings.ToLower(spec.ClientIDHex)
	}
	if spec.ClientID != "" {
		return hex.EncodeToString([]byte(spec.ClientID))
	}
	mac, err := net.ParseMAC(hardwareAddress)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(append([]byte{1}, mac...))
}

func importCommand(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var interfaces importInterfaces
	fs.Var(&interfaces, "interface", "physical interface to inspect (repeatable)")
	configPath := fs.String("config", "", "existing router configuration for interface names")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: routerctl import --interface <name> [--interface <name> ...] [--config <router.yaml>]")
		fmt.Fprintln(stderr, "Outputs an incomplete YAML candidate; merge it into a complete configuration before applying.")
		fmt.Fprintln(stderr, "Run with sufficient privileges to read the active network manager configuration and leases; review all warnings.")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 || len(interfaces) == 0 {
		return errors.New("import requires at least one --interface and no positional arguments")
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("interface import is not yet supported on %s", runtime.GOOS)
	}
	name := "imported"
	aliases := map[string]api.Resource{}
	usedNames := map[string]bool{}
	dhcpClients := map[string]api.Resource{}
	usedDHCPNames := map[string]bool{}
	raAddresses := map[string]api.Resource{}
	usedRANames := map[string]bool{}
	staticAddresses := map[string]api.Resource{}
	usedStaticNames := map[string]bool{}
	staticIPv6 := map[string]api.Resource{}
	virtualIPv4 := map[string]api.Resource{}
	usedVirtualNames := map[string]bool{}
	existingRoutes := map[string]api.Resource{}
	usedRouteNames := map[string]bool{}
	if *configPath != "" {
		router, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		name = router.Metadata.Name
		for _, resource := range router.Spec.Resources {
			if resource.Kind == "IPv6RAAddress" {
				spec, err := resource.IPv6RAAddressSpec()
				if err != nil {
					return err
				}
				raAddresses[spec.Interface] = resource
				usedRANames[resource.Metadata.Name] = true
				continue
			}
			if resource.Kind == "VirtualAddress" {
				usedVirtualNames[resource.Metadata.Name] = true
				spec, err := resource.VirtualAddressSpec()
				if err != nil {
					return err
				}
				if spec.Family == "ipv4" && (spec.Mode == "" || spec.Mode == "static" || spec.Mode == "vrrp") && spec.Address != "" {
					prefix, err := netip.ParsePrefix(spec.Address)
					if err != nil {
						return fmt.Errorf("existing VirtualAddress/%s: %w", resource.Metadata.Name, err)
					}
					virtualIPv4[spec.Interface+"|"+prefix.String()] = resource
				}
				if spec.Family == "ipv6" && (spec.Mode == "" || spec.Mode == "static") && spec.Address != "" {
					prefix, err := netip.ParsePrefix(spec.Address)
					if err != nil {
						return fmt.Errorf("existing VirtualAddress/%s: %w", resource.Metadata.Name, err)
					}
					staticIPv6[spec.Interface+"|"+prefix.String()] = resource
				}
				continue
			}
			if resource.Kind == "IPv4Route" {
				spec, err := resource.IPv4RouteSpec()
				if err != nil {
					return err
				}
				usedRouteNames[resource.Metadata.Name] = true
				device := ""
				if spec.DeviceFrom.Resource == "" {
					device = spec.Device
				} else if strings.HasPrefix(spec.DeviceFrom.Resource, "Interface/") && spec.DeviceFrom.Field == "ifname" {
					device = strings.TrimPrefix(spec.DeviceFrom.Resource, "Interface/")
				}
				if device != "" && spec.GatewayFrom.Resource == "" && spec.PreferredSource == "" && (spec.Type == "" || spec.Type == "unicast") {
					key := device + "|" + importRouteKey(spec.Destination, spec.Gateway, spec.Metric, spec.GatewayOnLink)
					existingRoutes[key] = resource
				}
				continue
			}
			if resource.Kind == "IPv4StaticAddress" {
				spec, err := resource.IPv4StaticAddressSpec()
				if err != nil {
					return err
				}
				prefix, err := netip.ParsePrefix(spec.Address)
				if err != nil {
					return fmt.Errorf("existing IPv4StaticAddress/%s: %w", resource.Metadata.Name, err)
				}
				staticAddresses[spec.Interface+"|"+prefix.String()] = resource
				usedStaticNames[resource.Metadata.Name] = true
				continue
			}
			if resource.Kind == "DHCPv4Client" {
				spec, err := resource.DHCPv4ClientSpec()
				if err != nil {
					return err
				}
				if _, found := dhcpClients[spec.Interface]; found {
					return fmt.Errorf("existing configuration defines multiple DHCPv4 clients for %q", spec.Interface)
				}
				dhcpClients[spec.Interface] = resource
				usedDHCPNames[resource.Metadata.Name] = true
				continue
			}
			if resource.Kind != "Interface" {
				continue
			}
			spec, err := resource.InterfaceSpec()
			if err != nil {
				return err
			}
			if previous, found := aliases[spec.IfName]; found && previous.Metadata.Name != resource.Metadata.Name {
				return fmt.Errorf("existing configuration defines interface %q more than once", spec.IfName)
			}
			aliases[spec.IfName] = resource
			usedNames[resource.Metadata.Name] = true
		}
	}
	router := api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: name}}
	var warnings []string
	for _, ifname := range interfaces {
		if _, err := os.Stat(filepath.Join("/sys/class/net", ifname, "device")); err != nil {
			return fmt.Errorf("interface %s is not a discoverable physical interface: %w", ifname, err)
		}
		out, err := exec.Command("ip", "-j", "address", "show", "dev", ifname).CombinedOutput()
		if err != nil {
			return fmt.Errorf("observe interface %s: %w: %s", ifname, err, strings.TrimSpace(string(out)))
		}
		var links []importLink
		if err := json.Unmarshal(out, &links); err != nil {
			return fmt.Errorf("decode interface %s: %w", ifname, err)
		}
		if len(links) != 1 || links[0].IfName != ifname {
			return fmt.Errorf("interface %s not found in kernel output", ifname)
		}
		link := links[0]
		sources, staticRoutes, networkFiles, observedDNS, sourceErr := networkdAddressSources(ifname)
		if sourceErr != nil {
			warnings = append(warnings, fmt.Sprintf("%s: networkd address sources unavailable: %v", ifname, sourceErr))
		} else if len(networkFiles) != 0 {
			warnings = append(warnings, fmt.Sprintf("%s: networkd manages this link via %s; transfer management before apply", ifname, networkFiles[0]))
		}
		up := false
		for _, f := range link.Flags {
			if f == "UP" {
				up = true
			}
		}
		if !up {
			warnings = append(warnings, fmt.Sprintf("%s: administratively down; current schema does not express explicit down", ifname))
		}
		if link.Master != "" {
			warnings = append(warnings, fmt.Sprintf("%s: master %s must be imported separately", ifname, link.Master))
		}
		mtu := link.MTU
		if mtu < 576 || mtu > 9216 {
			warnings = append(warnings, fmt.Sprintf("%s: MTU %d cannot be represented by Interface", ifname, mtu))
			mtu = 0
		}
		resource, found := aliases[ifname]
		if found {
			spec, err := resource.InterfaceSpec()
			if err != nil {
				return err
			}
			if (spec.AdminUp && !up) || (spec.MTU != 0 && spec.MTU != mtu) {
				warnings = append(warnings, fmt.Sprintf("%s: observed link settings differ from the existing Interface definition", ifname))
			}
			if observedDNS != nil {
				if spec.DNS == nil {
					spec.DNS = observedDNS
				} else if !slices.Equal(spec.DNS.Servers, observedDNS.Servers) || !slices.Equal(spec.DNS.Domains, observedDNS.Domains) {
					warnings = append(warnings, fmt.Sprintf("%s: existing Interface DNS differs from networkd", ifname))
				}
			}
			resource.Spec = spec
			resource.Status = nil
		} else {
			resourceName := ifname
			if usedNames[resourceName] {
				resourceName = "import-" + ifname
				for suffix := 2; usedNames[resourceName]; suffix++ {
					resourceName = fmt.Sprintf("import-%s-%d", ifname, suffix)
				}
			}
			resource = api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: resourceName}, Spec: api.InterfaceSpec{IfName: ifname, Managed: true, AdminUp: up, MTU: mtu, DNS: observedDNS}}
		}
		usedNames[resource.Metadata.Name] = true
		router.Spec.Resources = append(router.Spec.Resources, resource)
		dhcp4 := false
		dhcp4Address := ""
		raAddress := false
		importedStatic := map[string]bool{}
		dhcp4AddressCount := 0
		for _, address := range link.AddrInfo {
			ip, err := netip.ParseAddr(address.Local)
			if err == nil && ip.Is4() && sources[netip.PrefixFrom(ip, address.PrefixLen).String()] == "DHCPv4" {
				dhcp4AddressCount++
			}
		}
		for _, address := range link.AddrInfo {
			ip, err := netip.ParseAddr(address.Local)
			if err != nil {
				return fmt.Errorf("interface %s has invalid address %q: %w", ifname, address.Local, err)
			}
			origin := "origin unknown"
			source := sources[netip.PrefixFrom(ip, address.PrefixLen).String()]
			if address.Dynamic {
				origin = "dynamic"
			}
			if source != "" && source != "foreign" {
				origin = "networkd " + source
				if source == "NDisc" && ip.Is6() && ip.IsGlobalUnicast() && address.Dynamic {
					raAddress = true
				}
				if source == "DHCPv4" && ip.Is4() {
					if dhcp4 && dhcp4Address != ip.String() {
						dhcp4Address = ""
					} else if !dhcp4 {
						dhcp4Address = ip.String()
					}
					dhcp4 = true
				}
			}
			if source == "DHCPv4" && ip.Is4() && dhcp4AddressCount == 1 && address.Peer == "" && !address.NoPrefixRoute && len(address.Unsupported) == 0 && (address.Label == "" || address.Label == ifname) {
				continue
			}
			prefix := netip.PrefixFrom(ip, address.PrefixLen).String()
			virtual, existingVirtual := virtualIPv4[resource.Metadata.Name+"|"+prefix]
			if !existingVirtual {
				virtual, existingVirtual = virtualIPv4[ifname+"|"+prefix]
			}
			if existingVirtual && source != "DHCPv4" && importStaticIPv4Address(address, ifname, ip) {
				virtual.Status = nil
				router.Spec.Resources = append(router.Spec.Resources, virtual)
				continue
			}
			_, existingIPv4 := staticAddresses[resource.Metadata.Name+"|"+prefix]
			if !existingIPv4 {
				_, existingIPv4 = staticAddresses[ifname+"|"+prefix]
			}
			if (source == "static" || (source != "DHCPv4" && existingIPv4)) && importStaticIPv4Address(address, ifname, ip) {
				static, exists := staticAddresses[resource.Metadata.Name+"|"+prefix]
				if !exists {
					static, exists = staticAddresses[ifname+"|"+prefix]
				}
				if exists {
					static.Status = nil
				} else {
					base := resource.Metadata.Name + "-ipv4-" + strings.ReplaceAll(ip.String(), ".", "-") + fmt.Sprintf("-%d", address.PrefixLen)
					name := base
					for suffix := 2; usedStaticNames[name]; suffix++ {
						name = fmt.Sprintf("%s-%d", base, suffix)
					}
					static = api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: name}, Spec: api.IPv4StaticAddressSpec{Interface: resource.Metadata.Name, Address: prefix}}
				}
				usedStaticNames[static.Metadata.Name] = true
				router.Spec.Resources = append(router.Spec.Resources, static)
				importedStatic[prefix] = true
				warnings = append(warnings, fmt.Sprintf("%s: static address %s imported; review manager handoff and address attributes", ifname, prefix))
				continue
			}
			_, existingIPv6 := staticIPv6[resource.Metadata.Name+"|"+prefix]
			if !existingIPv6 {
				_, existingIPv6 = staticIPv6[ifname+"|"+prefix]
			}
			if (source == "static" || ((source == "" || source == "foreign") && existingIPv6)) && importStaticIPv6Address(address, ifname, ip) {
				static, exists := staticIPv6[resource.Metadata.Name+"|"+prefix]
				if !exists {
					static, exists = staticIPv6[ifname+"|"+prefix]
				}
				if exists {
					static.Status = nil
				} else {
					raw := ip.As16()
					base := resource.Metadata.Name + "-ipv6-" + hex.EncodeToString(raw[:]) + fmt.Sprintf("-%d", address.PrefixLen)
					name := base
					for suffix := 2; usedVirtualNames[name]; suffix++ {
						name = fmt.Sprintf("%s-%d", base, suffix)
					}
					static = api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: name}, Spec: api.VirtualAddressSpec{Family: "ipv6", Interface: resource.Metadata.Name, Address: prefix, Mode: "static"}}
				}
				usedVirtualNames[static.Metadata.Name] = true
				router.Spec.Resources = append(router.Spec.Resources, static)
				warnings = append(warnings, fmt.Sprintf("%s: static IPv6 address %s imported; review manager handoff and address attributes", ifname, prefix))
				continue
			}
			warnings = append(warnings, fmt.Sprintf("%s: address %s/%d (%s, scope %s) requires source configuration review: %s", ifname, address.Local, address.PrefixLen, origin, address.Scope, address.Raw))
		}
		if raAddress {
			client, exists := raAddresses[resource.Metadata.Name]
			if !exists {
				client, exists = raAddresses[ifname]
			}
			if exists {
				client.Status = nil
			} else {
				name := resource.Metadata.Name + "-ra"
				for suffix := 2; usedRANames[name]; suffix++ {
					name = fmt.Sprintf("%s-ra-%d", resource.Metadata.Name, suffix)
				}
				client = api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RAAddress"}, Metadata: api.ObjectMeta{Name: name}, Spec: api.IPv6RAAddressSpec{Interface: resource.Metadata.Name}}
			}
			usedRANames[client.Metadata.Name] = true
			router.Spec.Resources = append(router.Spec.Resources, client)
			warnings = append(warnings, fmt.Sprintf("%s: RA/SLAAC candidate requires review of networkd settings and manager handoff", ifname))
		}
		if !dhcp4 && len(networkFiles) > 0 {
			enabled, err := networkdDHCPv4Enabled(networkFiles)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: networkd DHCPv4 configuration could not be inspected: %v", ifname, err))
			} else if enabled {
				dhcp4 = true
				warnings = append(warnings, fmt.Sprintf("%s: networkd DHCPv4 is enabled but no lease address is currently observed", ifname))
			}
		}
		if dhcp4 {
			useRoutes, useDNS, routeMetric, useMTU, hostname, optionErr := networkdDHCPv4Options(networkFiles)
			if optionErr != nil {
				warnings = append(warnings, fmt.Sprintf("%s: DHCPv4 options could not be imported: %v", ifname, optionErr))
			}
			clientID, idErr := networkdDHCPv4ClientID(link.IfIndex, dhcp4Address)
			if idErr != nil {
				warnings = append(warnings, fmt.Sprintf("%s: DHCPv4 client identifier could not be imported: %v", ifname, idErr))
			}
			client, exists := dhcpClients[resource.Metadata.Name]
			if !exists {
				client, exists = dhcpClients[ifname]
			}
			if exists {
				client.Status = nil
				spec, err := client.DHCPv4ClientSpec()
				if err != nil {
					return err
				}
				if idErr == nil && configuredDHCPv4ClientID(spec, link.Address) != clientID {
					warnings = append(warnings, fmt.Sprintf("%s: existing DHCPv4Client identifier differs from the networkd lease", ifname))
				}
				if optionErr == nil {
					if spec.Hostname != hostname {
						warnings = append(warnings, fmt.Sprintf("%s: existing DHCPv4Client hostname differs from networkd", ifname))
					}
					if spec.UseMTU != useMTU {
						warnings = append(warnings, fmt.Sprintf("%s: existing DHCPv4Client useMTU differs from networkd", ifname))
					}
					if api.BoolDefault(spec.UseRoutes, true) != api.BoolDefault(useRoutes, true) {
						warnings = append(warnings, fmt.Sprintf("%s: existing DHCPv4Client useRoutes differs from networkd", ifname))
					}
					if api.BoolDefault(spec.UseDNS, true) != api.BoolDefault(useDNS, true) {
						warnings = append(warnings, fmt.Sprintf("%s: existing DHCPv4Client useDNS differs from networkd", ifname))
					}
					if routeMetric != 0 && spec.RouteMetric != routeMetric {
						warnings = append(warnings, fmt.Sprintf("%s: existing DHCPv4Client routeMetric differs from networkd", ifname))
					}
				}
			} else {
				clientName := resource.Metadata.Name + "-dhcp4"
				for suffix := 2; usedDHCPNames[clientName]; suffix++ {
					clientName = fmt.Sprintf("%s-dhcp4-%d", resource.Metadata.Name, suffix)
				}
				client = api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"}, Metadata: api.ObjectMeta{Name: clientName}, Spec: api.DHCPv4ClientSpec{Interface: resource.Metadata.Name, Hostname: hostname, ClientIDHex: clientID, UseRoutes: useRoutes, UseDNS: useDNS, UseMTU: useMTU, RouteMetric: routeMetric}}
			}
			usedDHCPNames[client.Metadata.Name] = true
			router.Spec.Resources = append(router.Spec.Resources, client)
			warnings = append(warnings, fmt.Sprintf("%s: DHCPv4 candidate requires review of client identity, route and DNS options and manager handoff", ifname))
		}
		for _, family := range []string{"-4", "-6"} {
			out, err := exec.Command("ip", "-j", family, "route", "show", "table", "all", "dev", ifname).CombinedOutput()
			if err != nil {
				return fmt.Errorf("observe %s routes for %s: %w: %s", family, ifname, err, strings.TrimSpace(string(out)))
			}
			var routes []json.RawMessage
			if err := json.Unmarshal(out, &routes); err != nil {
				return fmt.Errorf("decode %s routes for %s: %w", family, ifname, err)
			}
			for _, route := range routes {
				if family == "-4" {
					if importDerivedIPv4ConnectedRoute(route, ifname, link.AddrInfo, sources, importedStatic) {
						continue
					}
					spec, ok := importKernelStaticIPv4Route(route, ifname)
					key := importRouteKey(spec.Destination, spec.Gateway, spec.Metric, spec.GatewayOnLink)
					candidate, exists := existingRoutes[resource.Metadata.Name+"|"+key]
					if !exists {
						candidate, exists = existingRoutes[ifname+"|"+key]
					}
					if ok && (staticRoutes[key] || exists) {
						if exists {
							candidate.Status = nil
						} else {
							clean := strings.NewReplacer(".", "-", "/", "-")
							base := fmt.Sprintf("%s-route-%s-via-%s-%d", resource.Metadata.Name, clean.Replace(spec.Destination), clean.Replace(spec.Gateway), spec.Metric)
							name := base
							for suffix := 2; usedRouteNames[name]; suffix++ {
								name = fmt.Sprintf("%s-%d", base, suffix)
							}
							candidate = api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"}, Metadata: api.ObjectMeta{Name: name}, Spec: spec}
						}
						usedRouteNames[candidate.Metadata.Name] = true
						router.Spec.Resources = append(router.Spec.Resources, candidate)
						warnings = append(warnings, fmt.Sprintf("%s: static route %s imported; review manager handoff", ifname, spec.Destination))
						continue
					}
				}
				warnings = append(warnings, fmt.Sprintf("%s: %s route requires source configuration: %s", ifname, family, string(route)))
			}
		}
	}
	data, err := yaml.Marshal(router)
	if err != nil {
		return err
	}
	fmt.Fprintln(stderr, "WARNING: this is a partial candidate, not a complete machine configuration; merge and review all networking before apply.")
	fmt.Fprintln(stderr, "WARNING: addresses, routes, policy rules, most dynamic network options, link attributes, and existing network managers are not fully imported.")
	for _, warning := range warnings {
		fmt.Fprintln(stderr, "WARNING:", warning)
	}
	if _, err := io.WriteString(stdout, "# Partial candidate: merge into a complete machine configuration before apply.\n"); err != nil {
		return err
	}
	_, err = stdout.Write(data)
	return err
}
