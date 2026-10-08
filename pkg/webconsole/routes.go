// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func (h Handler) routesStatus() RoutesStatus {
	status := RoutesStatus{GeneratedAt: time.Now().UTC()}
	resources, err := h.resourceStatuses()
	if err != nil {
		status.Errors = append(status.Errors, err.Error())
	}
	status.Routes = append(status.Routes, h.configuredRouteEntries(resources)...)
	status.Routes = append(status.Routes, bgpRouteEntries(resources)...)
	status.BGPPeers = bgpRoutePeers(resources)
	live, errors := liveKernelRouteEntries(time.Now().UTC())
	status.Routes = append(status.Routes, live...)
	status.Errors = append(status.Errors, errors...)
	sortRouteEntries(status.Routes)
	sort.Slice(status.BGPPeers, func(i, j int) bool {
		if status.BGPPeers[i].Router != status.BGPPeers[j].Router {
			return status.BGPPeers[i].Router < status.BGPPeers[j].Router
		}
		return status.BGPPeers[i].Peer < status.BGPPeers[j].Peer
	})
	return status
}

func (h Handler) configuredRouteEntries(resources []routerstate.ObjectStatus) []RouteEntry {
	statuses := map[string]map[string]any{}
	for _, resource := range resources {
		statuses[resource.APIVersion+"/"+resource.Kind+"/"+resource.Name] = resource.Status
	}
	var out []RouteEntry
	if h.opts.Router == nil {
		return out
	}
	expanded := api.ExpandClusterNetworkRoutes(h.opts.Router)
	for _, resource := range expanded.Spec.Resources {
		status := statuses[resource.APIVersion+"/"+resource.Kind+"/"+resource.Metadata.Name]
		switch resource.Kind {
		case "IPv4StaticRoute":
			spec, err := resource.IPv4StaticRouteSpec()
			if err != nil {
				continue
			}
			out = append(out, RouteEntry{
				Source:      "static",
				Resource:    resource.Kind + "/" + resource.Metadata.Name,
				Family:      "ipv4",
				Destination: stringutil.FirstNonBlank(stringFromMap(status, "destination"), spec.Destination),
				Gateway:     stringutil.FirstNonBlank(stringFromMap(status, "gateway"), spec.Via),
				Device:      stringutil.FirstNonBlank(stringFromMap(status, "device"), spec.Interface),
				Metric:      routeMetricText(stringutil.FirstNonBlank(stringFromMap(status, "metric"), strconv.Itoa(spec.Metric))),
				Phase:       stringFromMap(status, "phase"),
				ObservedAt:  stringutil.FirstNonBlank(stringFromMap(status, "observedAt"), stringFromMap(status, "updatedAt")),
			})
		case "IPv6StaticRoute":
			spec, err := resource.IPv6StaticRouteSpec()
			if err != nil {
				continue
			}
			out = append(out, RouteEntry{
				Source:      "static",
				Resource:    resource.Kind + "/" + resource.Metadata.Name,
				Family:      "ipv6",
				Destination: stringutil.FirstNonBlank(stringFromMap(status, "destination"), spec.Destination),
				Gateway:     stringutil.FirstNonBlank(stringFromMap(status, "gateway"), spec.Via),
				Device:      stringutil.FirstNonBlank(stringFromMap(status, "device"), spec.Interface),
				Metric:      routeMetricText(stringutil.FirstNonBlank(stringFromMap(status, "metric"), strconv.Itoa(spec.Metric))),
				Phase:       stringFromMap(status, "phase"),
				ObservedAt:  stringutil.FirstNonBlank(stringFromMap(status, "observedAt"), stringFromMap(status, "updatedAt")),
			})
		case "IPv4Route":
			spec, err := resource.IPv4RouteSpec()
			if err != nil {
				continue
			}
			out = append(out, RouteEntry{
				Source:      "static",
				Resource:    resource.Kind + "/" + resource.Metadata.Name,
				Family:      "ipv4",
				Destination: stringutil.FirstNonBlank(stringFromMap(status, "destination"), spec.Destination),
				Gateway:     stringutil.FirstNonBlank(stringFromMap(status, "gateway"), spec.Gateway),
				Device:      stringutil.FirstNonBlank(stringFromMap(status, "device"), spec.Device),
				Metric:      routeMetricText(stringutil.FirstNonBlank(stringFromMap(status, "metric"), strconv.Itoa(spec.Metric))),
				Type:        stringutil.FirstNonBlank(stringFromMap(status, "type"), spec.Type),
				Phase:       stringFromMap(status, "phase"),
				ObservedAt:  stringutil.FirstNonBlank(stringFromMap(status, "observedAt"), stringFromMap(status, "updatedAt")),
			})
		case "DHCPv4Client":
			spec, err := resource.DHCPv4ClientSpec()
			if err != nil {
				continue
			}
			gateway := stringutil.FirstNonBlank(stringFromMap(status, "appliedDefaultGateway"), stringFromMap(status, "defaultGateway"), stringFromMap(status, "gateway"))
			if gateway == "" {
				continue
			}
			out = append(out, RouteEntry{
				Source:      "dhcpv4",
				Resource:    resource.Kind + "/" + resource.Metadata.Name,
				Family:      "ipv4",
				Destination: "default",
				Gateway:     gateway,
				Device:      stringutil.FirstNonBlank(stringFromMap(status, "interface"), spec.Interface),
				Protocol:    "dhcp",
				Metric:      routeMetricText(stringutil.FirstNonBlank(stringFromMap(status, "routeMetric"), strconv.Itoa(spec.RouteMetric))),
				Phase:       stringFromMap(status, "phase"),
				ObservedAt:  stringutil.FirstNonBlank(stringFromMap(status, "observedAt"), stringFromMap(status, "updatedAt")),
			})
		case "EgressRoutePolicy":
			spec, err := resource.EgressRoutePolicySpec()
			if err != nil || stringutil.FirstNonBlank(spec.Mode, "") != "priority" {
				continue
			}
			for _, candidate := range spec.Candidates {
				if len(candidate.Targets) > 0 {
					continue
				}
				out = append(out, RouteEntry{
					Source:      "policy",
					Resource:    resource.Kind + "/" + resource.Metadata.Name,
					Family:      "ipv4",
					Destination: "default",
					Gateway:     candidate.Gateway,
					Device:      candidate.EffectiveInterface(),
					Table:       routeMetricText(strconv.Itoa(candidate.EffectiveTable())),
					Metric:      routeMetricText(strconv.Itoa(candidate.EffectiveMetric())),
					Phase:       stringFromMap(status, "phase"),
					ObservedAt:  stringutil.FirstNonBlank(stringFromMap(status, "observedAt"), stringFromMap(status, "updatedAt")),
				})
			}
		}
	}
	return out
}

func bgpRouteEntries(resources []routerstate.ObjectStatus) []RouteEntry {
	var out []RouteEntry
	for _, resource := range resources {
		if resource.Kind != "BGPRouter" {
			continue
		}
		for _, prefix := range statusList(resource.Status["prefixes"]) {
			destination := stringutil.FirstNonBlank(statusAnyText(prefix["prefix"]), statusAnyText(prefix["network"]))
			if destination == "" {
				continue
			}
			out = append(out, RouteEntry{
				Source:      "bgp",
				Resource:    resource.Kind + "/" + resource.Name,
				Family:      routeFamily(destination),
				Destination: destination,
				Protocol:    "bgp",
				Peer:        stringutil.FirstNonBlank(statusAnyText(prefix["peer"]), statusAnyText(prefix["nextHop"]), statusAnyText(prefix["nexthop"])),
				Phase:       statusText(resource.Status, "phase"),
				ObservedAt:  statusText(resource.Status, "observedAt"),
			})
		}
	}
	return out
}

func bgpRoutePeers(resources []routerstate.ObjectStatus) []RouteBGPPeer {
	var out []RouteBGPPeer
	for _, resource := range resources {
		if resource.Kind != "BGPRouter" {
			continue
		}
		for _, peer := range statusList(resource.Status["peers"]) {
			messages := ""
			if statusAnyText(peer["messagesReceived"]) != "" || statusAnyText(peer["messagesSent"]) != "" {
				messages = fmt.Sprintf("%d/%d", statusIntValue(peer["messagesReceived"]), statusIntValue(peer["messagesSent"]))
			}
			established, _ := statusBoolValue(peer["established"])
			out = append(out, RouteBGPPeer{
				Router:           resource.Name,
				Peer:             statusAnyText(peer["address"]),
				ASN:              statusAnyText(peer["asn"]),
				State:            statusAnyText(peer["state"]),
				Established:      established,
				PrefixesReceived: statusAnyText(peer["prefixesReceived"]),
				Messages:         messages,
				LastEstablished:  statusAnyText(peer["lastEstablishedAt"]),
				LastError:        statusAnyText(peer["lastErrorReason"]),
			})
		}
	}
	return out
}

type linuxRouteJSON struct {
	Dst      string `json:"dst"`
	Gateway  string `json:"gateway"`
	Dev      string `json:"dev"`
	Protocol string `json:"protocol"`
	Proto    string `json:"proto"`
	Table    any    `json:"table"`
	Metric   any    `json:"metric"`
	Scope    string `json:"scope"`
	Type     string `json:"type"`
	Prefsrc  string `json:"prefsrc"`
}

func liveKernelRouteEntries(now time.Time) ([]RouteEntry, []string) {
	var entries []RouteEntry
	var errors []string
	for _, family := range []struct {
		Name string
		Flag string
	}{
		{Name: "ipv4", Flag: "-4"},
		{Name: "ipv6", Flag: "-6"},
	} {
		out, err := commandOutputTimeout(2*time.Second, "ip", "-j", family.Flag, "route", "show", "table", "all")
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		routes, err := parseLinuxRoutesJSON(out, family.Name, now)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		entries = append(entries, routes...)
	}
	return entries, errors
}

func parseLinuxRoutesJSON(data []byte, family string, now time.Time) ([]RouteEntry, error) {
	var raw []linuxRouteJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse ip route %s json: %w", family, err)
	}
	out := make([]RouteEntry, 0, len(raw))
	for _, item := range raw {
		destination := stringutil.FirstNonBlank(item.Dst, "default")
		protocol := stringutil.FirstNonBlank(item.Protocol, item.Proto)
		out = append(out, RouteEntry{
			Source:      "kernel",
			Family:      family,
			Destination: destination,
			Gateway:     item.Gateway,
			Device:      item.Dev,
			Protocol:    protocol,
			Table:       routeAnyText(item.Table),
			Metric:      routeAnyText(item.Metric),
			Scope:       item.Scope,
			Type:        item.Type,
			Phase:       "installed",
			ObservedAt:  now.Format(time.RFC3339Nano),
		})
	}
	return out, nil
}

func sortRouteEntries(entries []RouteEntry) {
	sort.Slice(entries, func(i, j int) bool {
		left := entries[i]
		right := entries[j]
		if left.Family != right.Family {
			return left.Family < right.Family
		}
		if left.Destination != right.Destination {
			return left.Destination < right.Destination
		}
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		if left.Resource != right.Resource {
			return left.Resource < right.Resource
		}
		return left.Device < right.Device
	})
}

func routeAnyText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func routeMetricText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "0" {
		return ""
	}
	return value
}

func routeFamily(destination string) string {
	destination = strings.TrimSpace(destination)
	if destination == "" || destination == "default" {
		return ""
	}
	prefix, err := netip.ParsePrefix(destination)
	if err == nil {
		if prefix.Addr().Is6() {
			return "ipv6"
		}
		return "ipv4"
	}
	addr, err := netip.ParseAddr(destination)
	if err == nil && addr.Is6() {
		return "ipv6"
	}
	if err == nil {
		return "ipv4"
	}
	if strings.Contains(destination, ":") {
		return "ipv6"
	}
	return "ipv4"
}
