// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"net"
	"sort"
	"strings"

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
	configured := configuredInterfaceAddresses(h.opts.Router)
	native, nativeErr := net.Interfaces()
	byName := make(map[string]net.Interface, len(native))
	for _, iface := range native {
		byName[iface.Name] = iface
	}
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
		item.ConfiguredAddresses = configured[resource.Metadata.Name]
		if nativeErr != nil {
			item.ObservationError = nativeErr.Error()
		} else if ifi, present := byName[spec.IfName]; present {
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
			} else {
				item.ObservationError = err.Error()
			}
		} else {
			// A successful native inventory establishes absence; it is not
			// a failed read for which old addresses should be retained.
			item.Phase = "Absent"
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

func configuredInterfaceAddresses(router *api.Router) map[string][]string {
	out := map[string][]string{}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "IPv4StaticAddress" {
			continue
		}
		spec, err := resource.IPv4StaticAddressSpec()
		if err != nil || spec.Address == "" {
			continue
		}
		iface := spec.Interface
		if kind, name := splitResourceRef(iface); kind == "Interface" {
			iface = name
		}
		out[iface] = appendUnique(out[iface], spec.Address)
	}
	return out
}

func splitResourceRef(ref string) (string, string) {
	ref = strings.TrimSpace(ref)
	parts := strings.Split(ref, "/")
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}
