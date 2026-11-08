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
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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
	IfName   string          `json:"ifname"`
	Flags    []string        `json:"flags"`
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
}

func networkdAddressSources(ifname string) (map[string]string, []string, error) {
	out, err := exec.Command("networkctl", "--json=short", "status", ifname).Output()
	if err != nil {
		return nil, nil, err
	}
	var status networkdImportStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return nil, nil, err
	}
	sources := make(map[string]string, len(status.Addresses))
	for _, address := range status.Addresses {
		bytes := make([]byte, len(address.Address))
		for i, value := range address.Address {
			if value < 0 || value > 255 {
				return nil, nil, fmt.Errorf("invalid networkd address byte %d", value)
			}
			bytes[i] = byte(value)
		}
		ip, ok := netip.AddrFromSlice(bytes)
		if !ok || address.PrefixLength < 0 || address.PrefixLength > ip.BitLen() {
			return nil, nil, fmt.Errorf("invalid networkd address or prefix length")
		}
		sources[netip.PrefixFrom(ip, address.PrefixLength).String()] = address.ConfigSource
	}
	var files []string
	if status.NetworkFile != "" {
		files = append(files, status.NetworkFile)
		files = append(files, status.NetworkFileDropins...)
	}
	return sources, files, nil
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
	staticAddresses := map[string]api.Resource{}
	usedStaticNames := map[string]bool{}
	staticIPv6 := map[string]api.Resource{}
	virtualIPv4 := map[string]api.Resource{}
	usedVirtualNames := map[string]bool{}
	if *configPath != "" {
		router, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		name = router.Metadata.Name
		for _, resource := range router.Spec.Resources {
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
		sources, networkFiles, sourceErr := networkdAddressSources(ifname)
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
		resource, found := aliases[ifname]
		if found {
			spec, err := resource.InterfaceSpec()
			if err != nil {
				return err
			}
			if spec.AdminUp && !up {
				warnings = append(warnings, fmt.Sprintf("%s: observed link settings differ from the existing Interface definition", ifname))
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
			resource = api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: resourceName}, Spec: api.InterfaceSpec{IfName: ifname, Managed: true, AdminUp: up}}
		}
		usedNames[resource.Metadata.Name] = true
		router.Spec.Resources = append(router.Spec.Resources, resource)
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
			}
			if source == "DHCPv4" && ip.Is4() {
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
			if source != "DHCPv4" && importStaticIPv4Address(address, ifname, ip) {
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
				warnings = append(warnings, fmt.Sprintf("%s: static address %s imported; review manager handoff and address attributes", ifname, prefix))
				continue
			}
			if (source == "static" || source == "" || source == "foreign") && importStaticIPv6Address(address, ifname, ip) {
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
