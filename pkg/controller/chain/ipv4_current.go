// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
)

type currentIPv4Link struct {
	IfName    string
	Addresses []netip.Prefix
}

// reconcileCurrentIPv4Addresses compares current configured and dynamic addresses with the kernel.
func (c IPv4StaticAddressController) reconcileCurrentIPv4Addresses(ctx context.Context) error {
	if c.DryRun {
		return nil
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("observe interfaces: %w", err)
	}
	var links []currentIPv4Link
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			if c.Logger != nil {
				c.Logger.Warn("IPv4 address observation skipped", "interface", iface.Name, "error", err)
			}
			continue
		}
		link := currentIPv4Link{IfName: iface.Name}
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address.String())
			if err == nil && prefix.Addr().Is4() {
				link.Addresses = append(link.Addresses, prefix)
			}
		}
		links = append(links, link)
	}
	desired := map[string]map[string]bool{}
	pendingInterfaces := map[string]bool{}
	add := func(ifname, address string) {
		ifname, address = strings.TrimSpace(ifname), strings.TrimSpace(address)
		if ifname == "" || address == "" {
			return
		}
		if _, err := netip.ParsePrefix(address); err != nil {
			addr, err := netip.ParseAddr(address)
			if err != nil || !addr.Is4() {
				return
			}
			address = netip.PrefixFrom(addr, 32).String()
		}
		if desired[ifname] == nil {
			desired[ifname] = map[string]bool{}
		}
		desired[ifname][address] = true
	}
	for _, resource := range c.Router.Spec.Resources {
		switch resource.Kind {
		case "IPv4StaticAddress":
			spec, err := resource.IPv4StaticAddressSpec()
			if err != nil {
				return err
			}
			add(interfaceIfName(c.Router, spec.Interface), spec.Address)
		case "VirtualAddress":
			spec, err := resource.VirtualAddressSpec()
			if err != nil {
				return err
			}
			if spec.Family != "ipv4" {
				continue
			}
			address, err := render.VirtualAddress(c.Router, spec)
			if err != nil {
				pendingInterfaces[interfaceIfName(c.Router, spec.Interface)] = true
				continue
			}
			add(interfaceIfName(c.Router, spec.Interface), address)
		case "TunnelInterface":
			spec, err := resource.TunnelInterfaceSpec()
			if err != nil {
				return err
			}
			add(resource.Metadata.Name, spec.Address)
		case "DSLiteTunnel":
			spec, err := resource.DSLiteTunnelSpec()
			if err != nil {
				return err
			}
			if !api.BoolDefault(spec.Enabled, true) {
				continue
			}
			address, pending, err := dsliteInnerLocalIPv4(c.Router, c.Store, spec)
			if err != nil {
				return err
			}
			ifname := firstNonEmpty(spec.TunnelName, resource.Metadata.Name)
			if platform.CurrentOS() == platform.OSFreeBSD {
				ifname = freeBSDDSLiteRuntimeIfName(ifname)
			}
			if pending != "" {
				pendingInterfaces[ifname] = true
				continue
			}
			add(ifname, address+"/32")
		case "DHCPv4Client":
			spec, err := resource.DHCPv4ClientSpec()
			if err != nil {
				return err
			}
			status := c.Store.ObjectStatus(api.NetAPIVersion, resource.Kind, resource.Metadata.Name)
			address := cleanStatusString(status["appliedAddress"])
			if address == "" {
				pendingInterfaces[interfaceIfName(c.Router, spec.Interface)] = true
				continue
			}
			add(interfaceIfName(c.Router, spec.Interface), address)
		case "PPPoESession":
			spec, err := resource.PPPoESessionSpec()
			if err != nil {
				return err
			}
			if !api.BoolDefault(spec.Enabled, true) {
				continue
			}
			status := c.Store.ObjectStatus(api.NetAPIVersion, resource.Kind, resource.Metadata.Name)
			device, address := cleanStatusString(status["device"]), cleanStatusString(status["currentAddress"])
			if device == "" || address == "" {
				pendingInterfaces[firstNonEmpty(device, spec.IfName, "ppp-"+resource.Metadata.Name)] = true
				continue
			}
			add(device, address)
		case "TailscaleNode":
			spec, err := resource.TailscaleNodeSpec()
			if err != nil {
				return err
			}
			if spec.State == "absent" {
				continue
			}
			status, err := observeTailscaleRuntime(ctx, spec)
			var addresses []string
			if err == nil {
				addresses = status.TailscaleIPs
			} else if c.Logger != nil {
				c.Logger.Warn("Tailscale address observation skipped", "error", err)
			}
			hasIPv4 := false
			for _, address := range addresses {
				if ip, err := netip.ParseAddr(address); err == nil && ip.Is4() {
					hasIPv4 = true
				}
			}
			if !hasIPv4 {
				pendingInterfaces["tailscale0"] = true
				pendingInterfaces[interfaceIfName(c.Router, "tailscale")] = true
			}
			for _, address := range addresses {
				for _, link := range links {
					for _, actual := range link.Addresses {
						if actual.Addr().String() == address {
							add(link.IfName, actual.String())
						}
					}
				}
			}
		}
	}
	for _, address := range c.MobilityDataplane.StaticAddresses {
		add(address.Interface, address.Address)
	}
	seen := map[string]bool{}
	if platform.CurrentOS() == platform.OSFreeBSD {
		for _, link := range links {
			seen[link.IfName] = true
		}
	} else {
		out, err := runOutputCommandContext(ctx, "networkctl", "--json=short", "list")
		if err != nil {
			return fmt.Errorf("observe networkd interfaces: %w", err)
		}
		var networkd struct {
			Interfaces []struct {
				Name  string `json:"Name"`
				State string `json:"AdministrativeState"`
			} `json:"Interfaces"`
		}
		if err := json.Unmarshal(out, &networkd); err != nil {
			return fmt.Errorf("decode networkd interfaces: %w", err)
		}
		for _, link := range networkd.Interfaces {
			seen[link.Name] = true
			if link.State == "unmanaged" || link.Name == "lo" {
				continue
			}
			if link.State != "configured" {
				pendingInterfaces[link.Name] = true
				continue
			}
			out, err := runOutputCommandContext(ctx, "networkctl", "--json=short", "status", link.Name)
			if err != nil {
				if c.Logger != nil {
					c.Logger.Warn("networkd address observation skipped", "interface", link.Name, "error", err)
				}
				pendingInterfaces[link.Name] = true
				continue
			}
			var status struct {
				Addresses []struct {
					Family  int    `json:"Family"`
					Address []int  `json:"Address"`
					Prefix  int    `json:"PrefixLength"`
					Source  string `json:"ConfigSource"`
				} `json:"Addresses"`
			}
			if err := json.Unmarshal(out, &status); err != nil {
				return fmt.Errorf("decode networkd IPv4 addresses on %s: %w", link.Name, err)
			}
			for _, address := range status.Addresses {
				if address.Family == 2 && address.Source == "DHCPv4" && len(address.Address) == 4 {
					ip := net.IPv4(byte(address.Address[0]), byte(address.Address[1]), byte(address.Address[2]), byte(address.Address[3]))
					add(link.Name, fmt.Sprintf("%s/%d", ip.String(), address.Prefix))
				}
			}
		}
	}
	for _, link := range links {
		if pendingInterfaces[link.IfName] || !seen[link.IfName] {
			continue
		}
		for _, prefix := range link.Addresses {
			if prefix.Addr().IsLoopback() || prefix.Addr().IsLinkLocalUnicast() || desired[link.IfName][prefix.String()] {
				continue
			}
			name, args := ipv4StaticAddressDeleteCommand(platform.CurrentOS(), link.IfName, prefix.String())
			if err := runCommandContext(ctx, name, args...); err != nil {
				return fmt.Errorf("delete IPv4 address on %s: %w", link.IfName, err)
			}
		}
	}
	return nil
}
