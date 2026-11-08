// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"net/netip"
	"os/exec"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/pkg/api"
)

func bgpRouterSpecs(router *api.Router) map[string]api.BGPRouterSpec {
	out := map[string]api.BGPRouterSpec{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "BGPRouter" {
			continue
		}
		spec, err := resource.BGPRouterSpec()
		if err == nil {
			out[resource.Metadata.Name] = spec
		}
	}
	return out
}

type virtualAddressShowSpec struct {
	Interface string
	Address   string
	Family    string
	Mode      string
	Peers     []string
}

func virtualAddressShowSpecs(router *api.Router) map[string]virtualAddressShowSpec {
	out := map[string]virtualAddressShowSpec{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "VirtualAddress":
			spec, err := resource.VirtualAddressSpec()
			if err == nil {
				out[resource.Metadata.Name] = virtualAddressShowSpec{Interface: spec.Interface, Address: spec.Address, Family: spec.Family, Mode: spec.Mode, Peers: spec.VRRP.Peers}
			}
		}
	}
	return out
}

func liveVRRPRole(status map[string]any, spec virtualAddressShowSpec, aliases map[string]string) (string, bool) {
	ifname := statusvalue.Text(status["ifname"])
	if ifname == "" {
		ifname = aliases[spec.Interface]
	}
	address := statusvalue.Text(status["address"])
	if address == "" {
		address = spec.Address
	}
	if strings.TrimSpace(ifname) == "" || strings.TrimSpace(address) == "" {
		return "", false
	}
	family := spec.Family
	if family == "" {
		family = "ipv4"
		if strings.Contains(address, ":") {
			family = "ipv6"
		}
	}
	ipFamily := "-4"
	if family == "ipv6" {
		ipFamily = "-6"
	}
	out, err := exec.Command("ip", ipFamily, "addr", "show", "dev", ifname).CombinedOutput()
	if err != nil {
		return "", false
	}
	if ipOutputHasAddress(string(out), address, family) {
		return "master", true
	}
	return "backup", true
}

func ipOutputHasAddress(output, address, family string) bool {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(address))
	if err != nil {
		addr, addrErr := netip.ParseAddr(strings.TrimSpace(address))
		if addrErr != nil {
			return false
		}
		bits := 32
		if family == "ipv6" {
			bits = 128
		}
		prefix = netip.PrefixFrom(addr, bits)
	}
	token := "inet "
	if family == "ipv6" {
		token = "inet6 "
	}
	needle := token + prefix.Addr().String() + "/" + strconv.Itoa(prefix.Bits())
	return strings.Contains(output, needle)
}
