// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"net"
	"sort"
	"strings"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func (h Handler) interfaceSummaries(resources []routerstate.ObjectStatus) []InterfaceSummary {
	if h.opts.Router == nil {
		return nil
	}
	statuses := map[string]map[string]any{}
	for _, resource := range resources {
		statuses[resource.APIVersion+"/"+resource.Kind+"/"+resource.Name] = resource.Status
	}
	type zoneInfo struct {
		role string
		zone string
	}
	zones := map[string]zoneInfo{}
	for _, resource := range h.opts.Router.Spec.Resources {
		if resource.APIVersion != api.FirewallAPIVersion || resource.Kind != "FirewallZone" {
			continue
		}
		spec, err := resource.FirewallZoneSpec()
		if err != nil {
			continue
		}
		for _, ref := range spec.Interfaces {
			kind, name := splitResourceRef(ref)
			if kind == "" || name == "" {
				continue
			}
			zones[kind+"/"+name] = zoneInfo{role: spec.Role, zone: resource.Metadata.Name}
		}
	}
	addresses := interfaceConfiguredAddresses(h.opts.Router, statuses)
	var out []InterfaceSummary
	for _, resource := range h.opts.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "Interface" {
			continue
		}
		spec, err := resource.InterfaceSpec()
		if err != nil {
			continue
		}
		status := statuses[api.NetAPIVersion+"/Interface/"+resource.Metadata.Name]
		item := InterfaceSummary{
			Name:    resource.Metadata.Name,
			IfName:  spec.IfName,
			Phase:   stringFromMap(status, "phase"),
			Managed: spec.Managed,
			Owner:   spec.Owner,
		}
		if zone, ok := zones["Interface/"+resource.Metadata.Name]; ok {
			item.Role = zone.role
			item.Zone = zone.zone
		}
		if ifi, err := net.InterfaceByName(spec.IfName); err == nil {
			item.MTU = ifi.MTU
			item.Flags = ifi.Flags.String()
			item.HardwareAddress = ifi.HardwareAddr.String()
			if item.Phase == "" {
				if ifi.Flags&net.FlagUp != 0 {
					item.Phase = "Up"
				} else {
					item.Phase = "Down"
				}
			}
			if addrs, err := ifi.Addrs(); err == nil {
				for _, addr := range addrs {
					item.Addresses = appendUnique(item.Addresses, addr.String())
				}
			}
		}
		for _, addr := range addresses[resource.Metadata.Name] {
			item.Addresses = appendUnique(item.Addresses, addr)
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		roleOrder := map[string]int{"untrust": 0, "trust": 1, "mgmt": 2}
		if roleOrder[out[i].Role] != roleOrder[out[j].Role] {
			return roleOrder[out[i].Role] < roleOrder[out[j].Role]
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func interfaceConfiguredAddresses(router *api.Router, statuses map[string]map[string]any) map[string][]string {
	out := map[string][]string{}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "IPv4StaticAddress":
			spec, err := resource.IPv4StaticAddressSpec()
			if err != nil {
				continue
			}
			addr := stringutil.FirstNonBlank(stringFromMap(statuses[api.NetAPIVersion+"/IPv4StaticAddress/"+resource.Metadata.Name], "address"), spec.Address)
			if addr != "" {
				out[spec.Interface] = appendUnique(out[spec.Interface], addr)
			}
		case "IPv6DelegatedAddress":
			spec, err := resource.IPv6DelegatedAddressSpec()
			if err != nil {
				continue
			}
			addr := stringFromMap(statuses[api.NetAPIVersion+"/IPv6DelegatedAddress/"+resource.Metadata.Name], "address")
			if addr != "" {
				out[spec.Interface] = appendUnique(out[spec.Interface], addr)
			}
		case "DHCPv4Client":
			iface, addr := addressStatusForInterface(resource, statuses)
			if iface != "" && addr != "" {
				out[iface] = appendUnique(out[iface], addr)
			}
		}
	}
	return out
}

func addressStatusForInterface(resource api.Resource, statuses map[string]map[string]any) (string, string) {
	status := statuses[resource.APIVersion+"/"+resource.Kind+"/"+resource.Metadata.Name]
	iface := stringFromMap(status, "interface")
	addr := stringutil.FirstNonBlank(stringFromMap(status, "address"), stringFromMap(status, "ip"))
	if iface != "" {
		return iface, addr
	}
	switch resource.Kind {
	case "DHCPv4Client":
		spec, err := resource.DHCPv4ClientSpec()
		if err == nil {
			return spec.Interface, addr
		}
	}
	return "", addr
}

func splitResourceRef(ref string) (string, string) {
	ref = strings.TrimSpace(ref)
	parts := strings.Split(ref, "/")
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}
