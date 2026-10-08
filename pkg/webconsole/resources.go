// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"fmt"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/controlapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func (h Handler) operationalResources(kind string) ([]routerstate.ObjectStatus, error) {
	resources, err := h.resourceStatuses()
	if err != nil {
		return nil, err
	}
	var out []routerstate.ObjectStatus
	for _, resource := range resources {
		switch kind {
		case "bgp":
			if resource.Kind == "BGPRouter" || resource.Kind == "BGPPeer" {
				out = append(out, resource)
			}
		case "vrrp":
			if resource.Kind == "VirtualAddress" {
				out = append(out, resource)
			}
		case "ingress":
			if resource.Kind == "IngressService" {
				out = append(out, resource)
			}
		}
	}
	return out, nil
}

func (h Handler) resourceStatuses() ([]routerstate.ObjectStatus, error) {
	var resources []routerstate.ObjectStatus
	if lister, ok := h.opts.Store.(routerstate.ObjectStatusLister); ok {
		listed, err := lister.ListObjectStatuses()
		if err != nil {
			return nil, err
		}
		resources = listed
	}
	resources = h.mergeConfiguredResourceStatuses(resources)
	resources = h.filterStaleObjectStatuses(resources)
	return annotateResourceOwnership(resources, h.controllerStatuses()), nil
}

func (h Handler) mergeConfiguredResourceStatuses(resources []routerstate.ObjectStatus) []routerstate.ObjectStatus {
	if h.opts.Router == nil {
		return resources
	}
	out := append([]routerstate.ObjectStatus(nil), resources...)
	seen := make(map[string]int, len(out))
	for i := range out {
		seen[objectStatusKey(out[i].APIVersion, out[i].Kind, out[i].Name)] = i
	}
	for _, resource := range h.opts.Router.Spec.Resources {
		if resource.Kind == "" || resource.Metadata.Name == "" {
			continue
		}
		key := objectStatusKey(resource.APIVersion, resource.Kind, resource.Metadata.Name)
		if idx, ok := seen[key]; ok {
			if out[idx].Status == nil {
				out[idx].Status = map[string]any{}
			}
			out[idx].Status["configured"] = true
			if _, exists := out[idx].Status["observed"]; !exists {
				out[idx].Status["observed"] = true
			}
			continue
		}
		out = append(out, routerstate.ObjectStatus{
			APIVersion: resource.APIVersion,
			Kind:       resource.Kind,
			Name:       resource.Metadata.Name,
			Status: map[string]any{
				"phase":      "NotObserved",
				"reason":     "NoObservedStatus",
				"message":    "resource is declared in config but no controller status has been written yet",
				"configured": true,
				"observed":   false,
			},
		})
	}
	return out
}

func objectStatusKey(apiVersion, kind, name string) string {
	return apiVersion + "/" + kind + "/" + name
}

func (h Handler) controllerStatuses() []controlapi.ControllerStatus {
	if h.opts.ControllerStatuses != nil {
		return h.opts.ControllerStatuses()
	}
	return h.opts.ControllerModes
}

func (h Handler) filterStaleObjectStatuses(resources []routerstate.ObjectStatus) []routerstate.ObjectStatus {
	if h.opts.Router == nil {
		return resources
	}
	declared := map[string]struct{}{}
	for _, resource := range h.opts.Router.Spec.Resources {
		declared[resource.APIVersion+"/"+resource.Kind+"/"+resource.Metadata.Name] = struct{}{}
	}
	out := resources[:0]
	for _, resource := range resources {
		if resource.Kind == "WireGuardPeer" {
			key := resource.APIVersion + "/" + resource.Kind + "/" + resource.Name
			if _, ok := declared[key]; !ok {
				continue
			}
		}
		out = append(out, resource)
	}
	return out
}

func annotateResourceOwnership(resources []routerstate.ObjectStatus, controllers []controlapi.ControllerStatus) []routerstate.ObjectStatus {
	ownerByKind := map[string]string{}
	for _, controller := range controllers {
		for _, kind := range controller.ResourceKinds {
			if _, exists := ownerByKind[kind]; !exists {
				ownerByKind[kind] = controller.Name
			}
		}
	}
	for i := range resources {
		status := resources[i].Status
		if status == nil {
			status = map[string]any{}
			resources[i].Status = status
		}
		if resources[i].Owner == "" {
			resources[i].Owner = statusText(status, "owner")
		}
		if resources[i].Owner == "" {
			resources[i].Owner = ownerByKind[resources[i].Kind]
		}
		if resources[i].Owner == "" {
			resources[i].Owner = defaultResourceOwnerController(resources[i].Kind)
		}
		if resources[i].Owner != "" {
			status["owner"] = resources[i].Owner
		}
		if resources[i].ManagedBy == "" {
			resources[i].ManagedBy = statusText(status, "managedBy")
		}
		if resources[i].ManagedBy == "" {
			if managed, ok := statusBoolValue(status["managed"]); ok && !managed {
				resources[i].ManagedBy = "external"
			} else {
				resources[i].ManagedBy = "routerd"
			}
		}
		status["managedBy"] = resources[i].ManagedBy
		if resources[i].Management == "" {
			resources[i].Management = statusText(status, "management")
		}
		if resources[i].Management == "" {
			if managed, ok := statusBoolValue(status["managed"]); ok && !managed {
				resources[i].Management = "adopted"
			} else if strings.EqualFold(resources[i].ManagedBy, "external") {
				resources[i].Management = "adopted"
			} else {
				resources[i].Management = "managed"
			}
		}
		status["management"] = resources[i].Management
	}
	return resources
}

func defaultResourceOwnerController(kind string) string {
	switch kind {
	case "IPv4StaticAddress", "IPv6DelegatedAddress", "IPv6RAAddress", "Interface", "Link":
		return "address"
	case "DHCPv4Client":
		return "dhcpv4client"
	case "DHCPv4Server", "DHCPv6Server", "DHCPv6Information", "IPv6RouterAdvertisement":
		return "dhcpv6"
	case "DNSResolver", "DNSZone":
		return "dns-resolver"
	case "DSLiteTunnel":
		return "dslite"
	case "FirewallZone", "FirewallPolicy", "FirewallRule", "ClientPolicy":
		return "firewall"
	case "NAT44Rule":
		return "nat"
	case "NetworkAdoption":
		return "network-adoption"
	case "Package", "KernelModule":
		return "package"
	case "PPPoESession":
		return "pppoesession"
	case "IPv4Route", "IPv4StaticRoute", "IPv6StaticRoute", "ClusterNetworkRoute", "EgressRoutePolicy":
		return "route"
	case "ServiceUnit", "TailscaleNode", "HealthCheck", "NTPClient", "NTPServer", "SysctlProfile", "Sysctl", "LogRetention", "Hostname", "ConntrackTuning":
		return "service-unit"
	case "ConntrackObserver", "TrafficFlowLog":
		return "conntrack"
	default:
		return ""
	}
}

func phaseCounts(resources []routerstate.ObjectStatus) map[string]int {
	out := map[string]int{}
	for _, resource := range resources {
		phase := fmt.Sprint(resource.Status["phase"])
		if strings.TrimSpace(phase) == "" || phase == "<nil>" {
			phase = "Unknown"
		}
		out[phase]++
	}
	return out
}

func SortResources(resources []routerstate.ObjectStatus) {
	sort.Slice(resources, func(i, j int) bool {
		a := resources[i].Kind + "/" + resources[i].Name
		b := resources[j].Kind + "/" + resources[j].Name
		return a < b
	})
}
