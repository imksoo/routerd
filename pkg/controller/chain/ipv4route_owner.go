// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

// A pending receipt is saved before the command: a restart must also recover
// the case where the kernel changed but the command's result was not recorded.
// There is at most one possibly owned route. An old identity is withdrawn before
// a different identity is staged.
type ipv4RouteReceipt struct {
	Route   ipv4RouteRemoval `json:"route"`
	Pending bool             `json:"pending"`
}

type ipv4RouteStatusReader interface {
	ReadObjectStatus(string, string, string) (map[string]any, error)
}

func (c IPv4RouteController) reconcileOrdinaryRoute(ctx context.Context, resource api.Resource, spec api.IPv4RouteSpec) error {
	reader, ok := c.Store.(ipv4RouteStatusReader)
	if !ok {
		return errors.New("IPv4Route status reading is unavailable")
	}
	previous, err := reader.ReadObjectStatus(api.NetAPIVersion, "IPv4Route", resource.Metadata.Name)
	if err != nil {
		return err
	}
	if ordinaryRouteExternallyManaged(previous) {
		return nil
	}
	receipt, err := ipv4RouteReceiptFromStatus(previous)
	if err != nil {
		return err
	}
	ready, err := resourcequery.ReadDependenciesReady(reader, spec.DependsOn)
	if err != nil {
		return err
	}
	if !ready {
		phase, err := dependencyUnavailablePhase(c.Router, reader, spec.DependsOn, standbyHealthcheckRoute(resource.Metadata.Name, spec))
		if err != nil {
			return err
		}
		dependencies, err := dependencyStatusSnapshot(reader, spec.DependsOn)
		if err != nil {
			return err
		}
		status := map[string]any{"phase": phase, "reason": "DependsOnFalse", "dependencies": dependencies, "dryRun": c.DryRun}
		return c.withdrawInactiveOrdinaryRoute(ctx, resource.Metadata.Name, status, receipt)
	}
	routeType := firstNonEmpty(strings.TrimSpace(spec.Type), "unicast")
	device := ""
	gatewayValue, err := resourcequery.ReadValue(reader, spec.GatewayFrom)
	if err != nil {
		return err
	}
	gateway := firstNonEmpty(gatewayValue, strings.TrimSpace(spec.Gateway))
	if routeType != "blackhole" {
		deviceValue, err := resourcequery.ReadValue(reader, spec.DeviceFrom)
		if err != nil {
			return err
		}
		device = firstNonEmpty(deviceValue, strings.TrimSpace(spec.Device))
	}
	if routeType != "blackhole" {
		if device == "" {
			return c.withdrawInactiveOrdinaryRoute(ctx, resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "DeviceMissing", "dryRun": c.DryRun}, receipt)
		}
		present := c.DevicePresent
		if present == nil {
			present = interfaceDevicePresent
		}
		if !c.DryRun && !present(ctx, device) {
			return c.withdrawInactiveOrdinaryRoute(ctx, resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "DeviceNotReady", "dryRun": c.DryRun}, receipt)
		}
	}
	destination := firstNonEmpty(spec.Destination, "0.0.0.0/0")
	preferredSource := strings.TrimSpace(spec.PreferredSource)
	phase := "Installed"
	if c.DryRun {
		phase = "Rendered"
	}
	status := map[string]any{"phase": phase, "type": routeType, "destination": destination, "device": device, "gateway": gateway, "preferredSource": preferredSource, "metric": spec.Metric, "dryRun": c.DryRun}
	changed := routeInstallStatusChanged(previous, status)
	if value, ok := previous["installedAt"]; ok && !changed {
		status["installedAt"] = value
	}
	if c.DryRun {
		return c.saveOrdinaryRouteStatus(resource.Metadata.Name, status, receipt)
	}
	if platform.CurrentOS() == platform.OSFreeBSD && spec.Metric != 0 {
		return c.failOrdinaryRoute(resource.Metadata.Name, status, receipt, "UnsupportedMetric", errors.New("FreeBSD IPv4Route does not support a Linux route metric"))
	}
	effectiveSource := preferredSource
	if preferredSource != "" && routeType != "blackhole" {
		local, err := c.ordinaryRoutePreferredSourceIsLocal(ctx, preferredSource)
		if err != nil {
			return c.failOrdinaryRoute(resource.Metadata.Name, status, receipt, "ObserveFailed", err)
		}
		if !local {
			effectiveSource = ""
			status["preferredSourceSkipped"], status["preferredSourceSkipReason"] = true, "LocalAddressMissing"
		}
	}
	status["effectivePreferredSource"] = effectiveSource
	desired, err := normalizeOrdinaryRoute(ipv4RouteRemoval{Type: routeType, Destination: destination, Device: device, Gateway: gateway, PreferredSource: effectiveSource, Metric: strconv.Itoa(spec.Metric)})
	if err != nil {
		return err
	}
	if receipt != nil && receipt.Route != desired {
		if err := c.removeOrdinaryRoute(ctx, receipt.Route); err != nil {
			return c.failOrdinaryRoute(resource.Metadata.Name, status, receipt, "WithdrawFailed", err)
		}
		receipt = nil
		status["phase"] = "Pending"
		if err := c.saveOrdinaryRouteStatus(resource.Metadata.Name, status, nil); err != nil {
			return err
		}
	}
	matching, collision, err := c.observeOrdinaryRoute(ctx, desired)
	if err != nil {
		return c.failOrdinaryRoute(resource.Metadata.Name, status, receipt, "ObserveFailed", err)
	}
	if matching && receipt != nil {
		receipt.Pending = false
		if changed {
			status["installedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		}
		status["phase"], status["changed"], status["kernelRouteAlreadyCurrent"] = "Installed", changed, true
		return c.saveOrdinaryRouteStatus(resource.Metadata.Name, status, receipt)
	}
	if collision {
		return c.failOrdinaryRoute(resource.Metadata.Name, status, nil, "ForeignRoutePreserved", errors.New("existing route has no matching applied ownership"))
	}
	receipt = &ipv4RouteReceipt{Route: desired, Pending: true}
	status["phase"] = "Applying"
	if err := c.saveOrdinaryRouteStatus(resource.Metadata.Name, status, receipt); err != nil {
		return err
	}
	name, args := ordinaryRouteApplyCommand(desired)
	out, err := c.run(ctx, name, args...)
	if err != nil && platform.CurrentOS() == platform.OSFreeBSD && freeBSDRouteNeedsAdd(out) {
		args = freeBSDIPv4RouteAddArgs(args)
		out, err = c.run(ctx, name, args...)
	}
	if err != nil {
		return c.failOrdinaryRoute(resource.Metadata.Name, status, receipt, "ApplyFailed", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out))))
	}
	receipt.Pending = false
	status["phase"], status["changed"] = "Installed", true
	status["installedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	if err := c.saveOrdinaryRouteStatus(resource.Metadata.Name, status, receipt); err != nil {
		return err
	}
	if c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.ipv4.route.installed", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv4Route", Name: resource.Metadata.Name}
		event.Attributes = map[string]string{"type": desired.Type, "destination": desired.Destination, "device": desired.Device, "gateway": desired.Gateway, "dryRun": "false"}
		return c.Bus.Publish(ctx, event)
	}
	return nil
}

func (c IPv4RouteController) saveOrdinaryRouteStatus(name string, status map[string]any, receipt *ipv4RouteReceipt) error {
	delete(status, "routeOwner")
	if receipt != nil {
		status["routeOwner"], status["managedBy"] = *receipt, "routerd"
	}
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", name, status)
}

func (c IPv4RouteController) ordinaryRoutePreferredSourceIsLocal(ctx context.Context, source string) (bool, error) {
	addr, err := netip.ParseAddr(source)
	if err != nil || !addr.Is4() {
		return false, fmt.Errorf("invalid IPv4Route preferred source %q", source)
	}
	if platform.CurrentOS() == platform.OSFreeBSD {
		out, err := c.run(ctx, "ifconfig", "-a", "inet")
		if err != nil {
			return false, fmt.Errorf("observe preferred source: %w", err)
		}
		return ifconfigHasIPv4Address(out, addr.String()), nil
	}
	out, err := c.run(ctx, "ip", "-j", "-4", "addr", "show")
	if err != nil {
		return false, fmt.Errorf("observe preferred source: %w", err)
	}
	var links []ipJSONLink
	if err := json.Unmarshal(out, &links); err != nil {
		return false, fmt.Errorf("decode preferred source observation: %w", err)
	}
	if links == nil {
		return false, errors.New("preferred source observation is not an address array")
	}
	for _, link := range links {
		for _, info := range link.AddrInfo {
			if info.Family == "inet" && info.Local == addr.String() {
				return true, nil
			}
		}
	}
	return false, nil
}

func (c IPv4RouteController) failOrdinaryRoute(name string, status map[string]any, receipt *ipv4RouteReceipt, reason string, effectErr error) error {
	status["phase"], status["reason"], status["error"] = "Error", reason, effectErr.Error()
	return errors.Join(effectErr, c.saveOrdinaryRouteStatus(name, status, receipt))
}

func (c IPv4RouteController) withdrawInactiveOrdinaryRoute(ctx context.Context, name string, status map[string]any, receipt *ipv4RouteReceipt) error {
	if receipt != nil && !c.DryRun {
		if err := c.removeOrdinaryRoute(ctx, receipt.Route); err != nil {
			return c.failOrdinaryRoute(name, status, receipt, "WithdrawFailed", err)
		}
		receipt = nil
	}
	return c.saveOrdinaryRouteStatus(name, status, receipt)
}

func ordinaryRouteExternallyManaged(status map[string]any) bool {
	return strings.EqualFold(routeStatusString(status["managedBy"]), "external") || strings.EqualFold(routeStatusString(status["management"]), "adopted") || status["managed"] == false
}

func ipv4RouteReceiptFromStatus(status map[string]any) (*ipv4RouteReceipt, error) {
	if ordinaryRouteExternallyManaged(status) {
		return nil, nil
	}
	value, exists := status["routeOwner"]
	if !exists {
		return nil, nil
	}
	var receipt ipv4RouteReceipt
	switch value := value.(type) {
	case ipv4RouteReceipt:
		receipt = value
	case map[string]any:
		route, ok := value["route"].(map[string]any)
		if !ok {
			return nil, errors.New("invalid IPv4Route owned route")
		}
		receipt.Route = ipv4RouteRemovalFromStatus(route)
		var valid bool
		receipt.Pending, valid = value["pending"].(bool)
		if !valid {
			return nil, errors.New("invalid IPv4Route pending ownership")
		}
	default:
		return nil, errors.New("invalid IPv4Route ownership receipt")
	}
	var err error
	receipt.Route, err = normalizeOrdinaryRoute(receipt.Route)
	return &receipt, err
}

func normalizeOrdinaryRoute(route ipv4RouteRemoval) (ipv4RouteRemoval, error) {
	if route.Type == "" {
		route.Type = "unicast"
	}
	if route.Type != "unicast" && route.Type != "blackhole" {
		return route, fmt.Errorf("unsupported owned IPv4Route type %q", route.Type)
	}
	var err error
	route.Destination, err = ordinaryRouteDestination(route.Destination)
	if err != nil {
		return route, err
	}
	if route.Type == "blackhole" {
		route.Device, route.Gateway, route.PreferredSource = "", "", ""
	} else if route.Device == "" {
		return route, errors.New("owned IPv4Route device is missing")
	}
	for _, value := range []*string{&route.Gateway, &route.PreferredSource} {
		if *value == "" {
			continue
		}
		addr, err := netip.ParseAddr(*value)
		if err != nil || !addr.Is4() {
			return route, fmt.Errorf("invalid owned IPv4Route address %q", *value)
		}
		*value = addr.String()
	}
	if route.Metric == "" {
		route.Metric = "0"
	}
	metric, err := strconv.Atoi(route.Metric)
	if err != nil || metric < 0 {
		return route, fmt.Errorf("invalid owned IPv4Route metric %q", route.Metric)
	}
	route.Metric = strconv.Itoa(metric)
	return route, nil
}

func ordinaryRouteApplyCommand(route ipv4RouteRemoval) (string, []string) {
	if platform.CurrentOS() == platform.OSFreeBSD {
		return freeBSDIPv4RouteApplyCommand(route.Type, route.Destination, route.Device, route.Gateway, route.PreferredSource)
	}
	args := []string{"route", "replace", route.Destination}
	if route.Type == "blackhole" {
		args = []string{"route", "replace", "blackhole", route.Destination}
	} else {
		if route.Gateway != "" {
			args = append(args, "via", route.Gateway)
		}
		args = append(args, "dev", route.Device)
		if route.PreferredSource != "" {
			args = append(args, "src", route.PreferredSource)
		}
	}
	if route.Metric != "0" {
		args = append(args, "metric", route.Metric)
	}
	return "ip", args
}

type ordinaryIPv4RouteObservation struct {
	Type            string `json:"type"`
	Destination     string `json:"dst"`
	Device          string `json:"dev"`
	Gateway         string `json:"gateway"`
	PreferredSource string `json:"prefsrc"`
	Metric          int    `json:"metric"`
	Protocol        string `json:"protocol"`
}

func (c IPv4RouteController) observeOrdinaryRoute(ctx context.Context, wanted ipv4RouteRemoval) (bool, bool, error) {
	if platform.CurrentOS() == platform.OSFreeBSD {
		return c.observeFreeBSDOrdinaryRoute(ctx, wanted)
	}
	query := wanted.Destination
	if query == "0.0.0.0/0" {
		query = "default"
	}
	out, err := c.run(ctx, "ip", "-j", "-d", "-4", "route", "show", query)
	if err != nil {
		return false, false, fmt.Errorf("observe IPv4Route %s: %w: %s", query, err, strings.TrimSpace(string(out)))
	}
	var routes []ordinaryIPv4RouteObservation
	if err := json.Unmarshal(out, &routes); err != nil {
		return false, false, fmt.Errorf("decode IPv4Route observation: %w", err)
	}
	if routes == nil {
		return false, false, errors.New("IPv4Route observation is not a route array")
	}
	collision := false
	for _, observed := range routes {
		destination, err := ordinaryRouteDestination(observed.Destination)
		if err != nil {
			return false, false, err
		}
		if destination != wanted.Destination || strconv.Itoa(observed.Metric) != wanted.Metric {
			continue
		}
		collision = true
		if observed.Protocol == "" {
			return false, false, errors.New("IPv4Route observation has no protocol ownership")
		}
		actual, err := normalizeOrdinaryRoute(ipv4RouteRemoval{
			Type: observed.Type, Destination: destination, Device: observed.Device,
			Gateway: observed.Gateway, PreferredSource: observed.PreferredSource,
			Metric: strconv.Itoa(observed.Metric),
		})
		if err != nil {
			return false, false, err
		}
		if actual == wanted && (observed.Protocol == "boot" || observed.Protocol == "3") {
			return true, true, nil
		}
	}
	return false, collision, nil
}

func ordinaryRouteDestination(value string) (string, error) {
	if value == "default" {
		value = "0.0.0.0/0"
	}
	if addr, err := netip.ParseAddr(value); err == nil && addr.Is4() {
		return netip.PrefixFrom(addr, 32).String(), nil
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil || !prefix.Addr().Is4() {
		return "", fmt.Errorf("invalid IPv4Route destination %q", value)
	}
	return prefix.Masked().String(), nil
}

func (c IPv4RouteController) removeOrdinaryRoute(ctx context.Context, route ipv4RouteRemoval) error {
	matching, _, err := c.observeOrdinaryRoute(ctx, route)
	if err != nil || !matching {
		return err
	}
	name := "ip"
	args := ipv4RouteRemovalDeleteArgs(route)
	if platform.CurrentOS() == platform.OSFreeBSD {
		name, args = freeBSDIPv4RouteRemovalDeleteCommand(route)
		if route.Gateway != "" {
			args = append(args, route.Gateway)
		} else if route.Type != "blackhole" {
			args = append(args, "-interface", route.Device)
		}
		if route.Device != "" {
			args = append(args, "-ifp", route.Device)
		}
	} else {
		if route.Metric == "0" {
			args = append(args, "metric", "0")
		}
		args = append(args, "proto", "boot")
	}
	out, err := c.run(ctx, name, args...)
	if err != nil && !missingIPv4RouteDelete(err, out) {
		return fmt.Errorf("withdraw IPv4Route: %s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c IPv4RouteController) observeFreeBSDOrdinaryRoute(ctx context.Context, wanted ipv4RouteRemoval) (bool, bool, error) {
	args := append([]string{"-n", "get"}, freeBSDRouteDestinationArgs(wanted.Destination)...)
	out, err := c.run(ctx, "route", args...)
	if err != nil {
		if freeBSDRouteNeedsAdd(out) || missingIPv4RouteDelete(err, out) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("observe FreeBSD IPv4Route: %w: %s", err, strings.TrimSpace(string(out)))
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok {
			values[key] = strings.TrimSpace(value)
		}
	}
	destination := values["destination"]
	if destination == "" || values["flags"] == "" {
		return false, false, errors.New("FreeBSD IPv4Route observation has no destination or flags")
	}
	if destination != "default" {
		addr, parseErr := netip.ParseAddr(destination)
		if parseErr != nil || !addr.Is4() {
			return false, false, errors.New("invalid FreeBSD IPv4Route destination")
		}
		bits := 32
		if !strings.Contains(values["flags"], "HOST") {
			mask := net.ParseIP(values["mask"]).To4()
			ones, size := net.IPMask(mask).Size()
			if size != 32 {
				return false, false, errors.New("FreeBSD IPv4Route observation has no valid netmask")
			}
			bits = ones
		}
		destination = netip.PrefixFrom(addr, bits).Masked().String()
	}
	actual := ipv4RouteRemoval{Type: "unicast", Destination: destination, Device: values["interface"], Metric: wanted.Metric}
	if strings.Contains(values["flags"], "BLACKHOLE") {
		actual.Type = "blackhole"
	}
	if gateway, err := netip.ParseAddr(values["gateway"]); err == nil && gateway.Is4() {
		actual.Gateway = gateway.String()
	}
	actual, err = normalizeOrdinaryRoute(actual)
	if err != nil {
		return false, false, err
	}
	comparison := wanted
	comparison.PreferredSource = "" // route get exposes gateway/interface, not -ifa.
	collision := actual.Destination == wanted.Destination
	return actual == comparison && strings.Contains(values["flags"], "STATIC") && !strings.Contains(values["flags"], "PROTO1") && !strings.Contains(values["flags"], "PROTO2"), collision, nil
}
