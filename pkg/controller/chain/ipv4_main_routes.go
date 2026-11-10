// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type observedIPv4MainRoute struct {
	Destination string          `json:"dst"`
	Protocol    json.RawMessage `json:"protocol"`
	Type        string          `json:"type"`
	Device      string          `json:"dev"`
	Gateway     string          `json:"gateway"`
	Metric      int             `json:"metric"`
}

func ipv4MainPrefix(s string) (string, error) {
	if s == "" || s == "default" {
		return "0.0.0.0/0", nil
	}
	if addr, err := netip.ParseAddr(s); err == nil && addr.Is4() {
		return netip.PrefixFrom(addr, 32).String(), nil
	}
	if address, bits, ok := strings.Cut(s, "/"); ok {
		parts := strings.Split(address, ".")
		if len(parts) > 0 && len(parts) < 4 {
			for len(parts) < 4 {
				parts = append(parts, "0")
			}
			s = strings.Join(parts, ".") + "/" + bits
		}
	}
	p, err := netip.ParsePrefix(s)
	if err != nil || !p.Addr().Is4() {
		return "", fmt.Errorf("invalid IPv4 route destination %q", s)
	}
	return p.Masked().String(), nil
}

// cleanupIPv4MainRoutes reconciles boot/static routes against the current
// effective configuration and live protocol state, without previous ownership.
func (c IPv4RouteController) cleanupIPv4MainRoutes(ctx context.Context) error {
	if c.DryRun || c.Router == nil || c.Store == nil {
		return nil
	}
	osName := platform.CurrentOS()
	if osName != platform.OSLinux && osName != platform.OSFreeBSD {
		return fmt.Errorf("IPv4 route cleanup is unavailable on %s", osName)
	}
	type routeKey struct {
		destination, routeType, device, gateway string
		metric                                  int
	}
	wanted := map[routeKey]bool{}
	configured := map[string]bool{}
	protectedDestinations := map[string]bool{}
	protectedScopes := map[string]bool{}
	protectedDevices := map[string]bool{}
	pppDevices := map[string]bool{}
	dhcpGateways := map[string]map[string]bool{}
	add := func(dst, routeType, device, gateway string, metric int) error {
		prefix, err := ipv4MainPrefix(dst)
		if err != nil {
			return err
		}
		if routeType == "" {
			routeType = "unicast"
		}
		if osName == platform.OSFreeBSD {
			metric = 0
		}
		wanted[routeKey{prefix, routeType, device, gateway, metric}] = true
		return nil
	}
	records, err := c.Store.ListDynamicConfigParts()
	if err != nil {
		return fmt.Errorf("list dynamic route state: %w", err)
	}
	active, invalid := codec.ActiveMobilityPoolPlanRecords(records, time.Now().UTC())
	readyPools := map[string]bool{}
	for _, record := range active {
		if !record.Source.ARPObserver {
			readyPools[record.Source.PoolRef] = true
		}
	}
	if len(invalid) > 0 {
		return fmt.Errorf("invalid mobility route state")
	}
	for _, resource := range api.ExpandClusterNetworkRoutes(c.Router).Spec.Resources {
		switch resource.Kind {
		case "IPv4Route":
			configured[resource.Metadata.Name] = true
			spec, err := resource.IPv4RouteSpec()
			if err != nil {
				return err
			}
			device := firstNonEmpty(resourcequery.Value(c.Store, spec.DeviceFrom), strings.TrimSpace(spec.Device))
			gateway := firstNonEmpty(resourcequery.Value(c.Store, spec.GatewayFrom), strings.TrimSpace(spec.Gateway))
			if (spec.GatewayFrom.Resource != "" && gateway == "") || (spec.DeviceFrom.Resource != "" && device == "") {
				prefix, err := ipv4MainPrefix(spec.Destination)
				if err != nil {
					return err
				}
				protectedDestinations[prefix] = true
				continue
			}
			if err := add(spec.Destination, spec.Type, interfaceIfName(c.Router, device), gateway, spec.Metric); err != nil {
				return err
			}
		case "IPv4StaticRoute":
			spec, err := resource.IPv4StaticRouteSpec()
			if err != nil {
				return err
			}
			if err := add(spec.Destination, "unicast", interfaceIfName(c.Router, spec.Interface), spec.Via, spec.Metric); err != nil {
				return err
			}
		case "DHCPv4Client":
			spec, err := resource.DHCPv4ClientSpec()
			if err != nil {
				return err
			}
			if !api.BoolDefault(spec.UseRoutes, true) {
				continue
			}
			status := c.Store.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", resource.Metadata.Name)
			phase := statusvalue.Field(status, "phase")
			expiry, err := time.Parse(time.RFC3339Nano, statusvalue.Field(status, "expiresAt"))
			if err != nil || !time.Now().Before(expiry) || (phase != daemonapi.ResourcePhaseBound && phase != daemonapi.ResourcePhaseRefreshing && phase != daemonapi.ResourcePhaseRebinding) {
				protectedDevices[interfaceIfName(c.Router, spec.Interface)] = true
				continue
			}
			if gateway := statusvalue.Field(status, "defaultGateway"); gateway != "" {
				device := interfaceIfName(c.Router, spec.Interface)
				if err := add("default", "unicast", device, gateway, spec.RouteMetric); err != nil {
					return err
				}
				if dhcpGateways[device] == nil {
					dhcpGateways[device] = map[string]bool{}
				}
				dhcpGateways[device][gateway] = true
			}
		case "PPPoESession":
			spec, err := resource.PPPoESessionSpec()
			if err != nil {
				return err
			}
			if api.BoolDefault(spec.Enabled, true) && spec.DefaultRoute {
				status := c.Store.ObjectStatus(api.NetAPIVersion, "PPPoESession", resource.Metadata.Name)
				device := statusvalue.Field(status, "device")
				if device == "" {
					protectedDestinations["0.0.0.0/0"] = true
					continue
				}
				pppDevices[device] = true
			}
		case "MobilityPool":
			if !readyPools[resource.Metadata.Name] {
				spec, err := resource.MobilityPoolSpec()
				if err != nil {
					return err
				}
				prefix, err := ipv4MainPrefix(spec.Prefix)
				if err != nil {
					return err
				}
				protectedScopes[prefix] = true
				continue
			}
		}
	}
	for _, route := range c.MobilityDataplane.Routes {
		if err := add(route.Destination, "unicast", route.Device, "", route.Metric); err != nil {
			return err
		}
	}
	for _, capture := range c.MobilityDataplane.Captures {
		if capture.Disposition == dynamicconfig.CaptureHold {
			protectedScopes[capture.PoolPrefix] = true
		}
	}
	var routes []observedIPv4MainRoute
	if osName == platform.OSFreeBSD {
		out, err := runOutputCommandContext(ctx, "netstat", "-rn", "-f", "inet", "-W")
		if err != nil {
			return fmt.Errorf("list FreeBSD IPv4 routes: %w: %s", err, strings.TrimSpace(string(out)))
		}
		routes, err = freeBSDIPv4MainRoutes(string(out))
		if err != nil {
			return err
		}
	} else {
		out, err := runOutputCommandContext(ctx, "ip", "-j", "-4", "route", "show", "table", "main")
		if err != nil {
			return fmt.Errorf("list IPv4 main routes: %w: %s", err, strings.TrimSpace(string(out)))
		}
		if err := json.Unmarshal(out, &routes); err != nil {
			return fmt.Errorf("decode IPv4 main routes: %w", err)
		}
	}
	// Keep an old route to a desired destination until its replacement is present.
	observed := map[routeKey]bool{}
	for _, route := range routes {
		dst, err := ipv4MainPrefix(route.Destination)
		if err != nil {
			return err
		}
		typ := route.Type
		if typ == "" {
			typ = "unicast"
		}
		observed[routeKey{dst, typ, route.Device, route.Gateway, route.Metric}] = true
	}
	for key := range wanted {
		if !observed[key] {
			protectedDestinations[key.destination] = true
		}
	}
	for _, route := range routes {
		protocol := strings.Trim(string(route.Protocol), `"`)
		if protocol == "" || protocol == "null" {
			protocol = "boot"
		}
		switch protocol {
		case "kernel", "dhcp", "bgp", "2", "16", "186":
			continue
		}
		dst, err := ipv4MainPrefix(route.Destination)
		if err != nil {
			return err
		}
		routeType := route.Type
		if routeType == "" {
			routeType = "unicast"
		}
		if (dst == "0.0.0.0/0" && pppDevices[route.Device]) || dhcpGateways[route.Device][strings.TrimSuffix(dst, "/32")] {
			continue
		}
		if wanted[routeKey{dst, routeType, route.Device, route.Gateway, route.Metric}] || protectedDevices[route.Device] || protectedDestinations[dst] {
			continue
		}
		// Pending sources and held captures protect only their affected scope.
		protected := false
		for prefix := range protectedScopes {
			scope, se := netip.ParsePrefix(prefix)
			candidate, ce := netip.ParsePrefix(dst)
			if se != nil || ce != nil {
				return fmt.Errorf("invalid pending route scope")
			}
			if scope.Contains(candidate.Addr()) && candidate.Bits() >= scope.Bits() {
				protected = true
				break
			}
		}
		if protected {
			continue
		}
		name := "ip"
		args := []string{"-4", "route", "del"}
		if osName == platform.OSFreeBSD {
			name = "route"
			args = append([]string{"-n", "delete"}, freeBSDRouteDestinationArgs(dst)...)
			if route.Gateway != "" {
				args = append(args, route.Gateway)
			} else if route.Device != "" {
				args = append(args, "-interface", route.Device)
			}
		} else {
			if route.Type != "" && route.Type != "unicast" {
				args = append(args, route.Type)
			}
			args = append(args, dst, "table", "main")
			if route.Gateway != "" {
				args = append(args, "via", route.Gateway)
			}
			if route.Device != "" {
				args = append(args, "dev", route.Device)
			}
			args = append(args, "metric", strconv.Itoa(route.Metric), "proto", protocol)
		}
		output, err := runOutputCommandContext(ctx, name, args...)
		if err != nil && !missingIPv4RouteDelete(err, output) {
			return fmt.Errorf("delete IPv4 route: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	// Status is an observation, not the source of route deletion candidates.
	if lister, ok := c.Store.(routerstate.ObjectStatusLister); ok {
		if deleter, ok := c.Store.(routerstate.ObjectDeleteStore); ok {
			statuses, err := lister.ListObjectStatuses()
			if err != nil {
				return err
			}
			for _, status := range statuses {
				if status.APIVersion == api.NetAPIVersion && status.Kind == "IPv4Route" && !configured[status.Name] {
					if err := deleter.DeleteObject(status.APIVersion, status.Kind, status.Name); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// freeBSDIPv4MainRoutes selects the kernel's static IPv4 routes. Connected,
// redirected, neighbour, loopback and BGP routes remain protocol-owned.
func freeBSDIPv4MainRoutes(output string) ([]observedIPv4MainRoute, error) {
	var routes []observedIPv4MainRoute
	header := false
	deviceColumn := -1
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "Destination" && fields[1] == "Gateway" && fields[2] == "Flags" {
			header = true
			for i, field := range fields {
				if field == "Netif" {
					deviceColumn = i
					break
				}
			}
			if deviceColumn < 0 {
				return nil, fmt.Errorf("FreeBSD route table has no Netif column")
			}
			continue
		}
		if !header || len(fields) <= deviceColumn {
			continue
		}
		flags := fields[2]
		if !strings.Contains(flags, "S") || strings.ContainsAny(flags, "123DML") {
			continue
		}
		device := fields[deviceColumn]
		iface, err := net.InterfaceByName(device)
		if err != nil {
			return nil, fmt.Errorf("observe FreeBSD route interface %s: %w", device, err)
		}
		if iface.Flags&net.FlagLoopback != 0 && !strings.Contains(flags, "B") {
			continue
		}
		if _, err := ipv4MainPrefix(fields[0]); err != nil {
			return nil, fmt.Errorf("decode FreeBSD route destination: %w", err)
		}
		route := observedIPv4MainRoute{Destination: fields[0], Device: device, Protocol: json.RawMessage(`"static"`)}
		if strings.Contains(flags, "B") {
			route.Type = "blackhole"
			route.Device = ""
		}
		if route.Type != "blackhole" {
			if gateway, err := netip.ParseAddr(fields[1]); err == nil && gateway.Is4() {
				route.Gateway = gateway.String()
			}
		}
		routes = append(routes, route)
	}
	if !header {
		return nil, fmt.Errorf("FreeBSD IPv4 route table has no header")
	}
	return routes, nil
}
