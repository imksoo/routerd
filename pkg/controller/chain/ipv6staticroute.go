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
	"github.com/imksoo/routerd/pkg/lifecycle"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// ipv6StaticRouteReceipt identifies the exact native route acquired by an
// IPv6StaticRoute. Routes have no private ownership cookie, so all mutation
// paths read back the complete tuple before changing or withdrawing it.
type ipv6StaticRouteReceipt struct {
	Route       ipv6StaticRoute `json:"route"`
	Scope       string          `json:"scope"`
	IfIndex     int             `json:"ifIndex"`
	ParentToken string          `json:"parentToken"`
	Pending     bool            `json:"pending"`
}

type ipv6StaticRoute struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway"`
	Device      string `json:"device"`
	Metric      string `json:"metric"`
}

type ipv6StaticRoutePremise struct {
	Route       ipv6StaticRoute
	Scope       string
	IfIndex     int
	ParentToken string
}

func normalizeIPv6StaticRoute(route ipv6StaticRoute) (ipv6StaticRoute, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(route.Destination))
	if err != nil || !prefix.Addr().Is6() {
		return route, fmt.Errorf("invalid IPv6StaticRoute destination %q", route.Destination)
	}
	gateway, err := netip.ParseAddr(strings.TrimSpace(route.Gateway))
	if err != nil || !gateway.Is6() {
		return route, fmt.Errorf("invalid IPv6StaticRoute gateway %q", route.Gateway)
	}
	if strings.TrimSpace(route.Device) == "" {
		return route, errors.New("IPv6StaticRoute device is missing")
	}
	metric := strings.TrimSpace(route.Metric)
	if metric == "" {
		metric = "0"
	}
	value, err := strconv.Atoi(metric)
	if err != nil || value < 0 {
		return route, fmt.Errorf("invalid IPv6StaticRoute metric %q", route.Metric)
	}
	route.Destination = prefix.Masked().String()
	route.Gateway = gateway.String()
	route.Device = strings.TrimSpace(route.Device)
	route.Metric = strconv.Itoa(value)
	return route, nil
}

func (c IPv4RouteController) ipv6StaticRoutePremise(ctx context.Context, spec api.IPv6StaticRouteSpec) (ipv6StaticRoutePremise, error) {
	route, err := normalizeIPv6StaticRoute(ipv6StaticRoute{
		Destination: spec.Destination,
		Gateway:     spec.Via,
		Device:      api.ResolveInterfaceIfName(c.Router, spec.Interface),
		Metric:      strconv.Itoa(spec.Metric),
	})
	if err != nil {
		return ipv6StaticRoutePremise{}, err
	}
	// Linux assigns priority 1024 when an IPv6 route metric is omitted or
	// zero. Persist that effective tuple so readback and withdrawal agree
	// with the kernel instead of leaving a successfully added route pending.
	if platform.CurrentOS() == platform.OSLinux && route.Metric == "0" {
		route.Metric = "1024"
	}
	result := ipv6StaticRoutePremise{Route: route}
	if c.DryRun {
		return result, nil
	}
	index, present, err := c.ordinaryRouteDevice(ctx, route.Device)
	if err != nil {
		return result, err
	}
	if !present {
		return result, fmt.Errorf("IPv6StaticRoute device %q is not ready", route.Device)
	}
	scope, err := c.ordinaryRouteScope(ctx)
	if err != nil {
		return result, err
	}
	if scope == "" {
		return result, errors.New("IPv6StaticRoute kernel scope is unavailable")
	}
	parentToken, err := c.ordinaryRouteNativeParent(ctx, route.Device, scope, index)
	if err != nil {
		return result, err
	}
	result.Scope, result.IfIndex, result.ParentToken = scope, index, parentToken
	return result, nil
}

func (c IPv4RouteController) reconcileIPv6StaticRoute(ctx context.Context, resource api.Resource, spec api.IPv6StaticRouteSpec) error {
	previous, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "IPv6StaticRoute", resource.Metadata.Name)
	if err != nil {
		return err
	}
	if ordinaryRouteExternallyManaged(previous) {
		return nil
	}
	receipt, err := ipv6StaticRouteReceiptFromStatus(previous)
	if err != nil {
		return err
	}
	premise, err := c.ipv6StaticRoutePremise(ctx, spec)
	if err != nil {
		return c.failIPv6StaticRoute(resource.Metadata.Name, ipv6StaticRouteStatus(spec, ipv6StaticRoute{}), receipt, "InvalidSpec", err)
	}
	status := ipv6StaticRouteStatus(spec, premise.Route)
	changed := ipv6StaticRouteStatusChanged(previous, status)
	if installedAt, ok := previous["installedAt"]; ok && !changed {
		status["installedAt"] = installedAt
	}
	if c.DryRun {
		status["phase"], status["dryRun"] = "Rendered", true
		return c.saveIPv6StaticRouteStatus(resource.Metadata.Name, status, nil)
	}
	if platform.CurrentOS() == platform.OSFreeBSD && premise.Route.Metric != "0" {
		return c.failIPv6StaticRoute(resource.Metadata.Name, status, receipt, "UnsupportedMetric", errors.New("FreeBSD IPv6StaticRoute does not support a Linux route metric"))
	}
	if receipt != nil {
		matching, _, err := c.observeIPv6StaticRoute(ctx, receipt.Route)
		if err != nil {
			return c.failIPv6StaticRoute(resource.Metadata.Name, status, receipt, "ObserveFailed", err)
		}
		if matching && receipt.Pending {
			return c.failIPv6StaticRoute(resource.Metadata.Name, status, receipt, "OwnershipUnknown", errors.New("pending IPv6StaticRoute is present without a private native ownership cookie; inspect and remove it before retrying"))
		}
		if matching && receipt.Route == premise.Route {
			if err := c.verifyIPv6StaticRouteIdentity(ctx, *receipt); err != nil {
				return c.failIPv6StaticRoute(resource.Metadata.Name, status, receipt, "OwnershipUnknown", err)
			}
			status["changed"], status["kernelRouteAlreadyCurrent"] = changed, true
			return c.saveIPv6StaticRouteStatus(resource.Metadata.Name, status, receipt)
		}
		if err := c.withdrawIPv6StaticRouteReceipt(ctx, receipt); err != nil {
			return c.failIPv6StaticRoute(resource.Metadata.Name, status, receipt, "WithdrawFailed", err)
		}
		receipt = nil
		if err := c.saveIPv6StaticRouteStatus(resource.Metadata.Name, map[string]any{"phase": "Pending", "dryRun": false}, nil); err != nil {
			return err
		}
	}
	_, collision, err := c.observeIPv6StaticRoute(ctx, premise.Route)
	if err != nil {
		return c.failIPv6StaticRoute(resource.Metadata.Name, status, nil, "ObserveFailed", err)
	}
	if collision {
		return c.failIPv6StaticRoute(resource.Metadata.Name, status, nil, "ForeignRoutePreserved", errors.New("existing IPv6 route has no matching applied ownership"))
	}
	receipt = &ipv6StaticRouteReceipt{Route: premise.Route, Scope: premise.Scope, IfIndex: premise.IfIndex, ParentToken: premise.ParentToken, Pending: true}
	status["phase"] = "Applying"
	if err := c.saveIPv6StaticRouteStatus(resource.Metadata.Name, status, receipt); err != nil {
		return err
	}
	name, args := ipv6StaticRouteApplyCommand(premise.Route)
	out, err := c.run(ctx, name, args...)
	if err != nil {
		return c.failIPv6StaticRoute(resource.Metadata.Name, status, receipt, "ApplyFailed", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out))))
	}
	matching, _, err := c.observeIPv6StaticRoute(ctx, premise.Route)
	if err != nil || !matching {
		return c.failIPv6StaticRoute(resource.Metadata.Name, status, receipt, "ApplyUnconfirmed", errors.Join(err, errors.New("IPv6StaticRoute add did not read back the exact native tuple")))
	}
	if err := c.verifyIPv6StaticRouteIdentity(ctx, *receipt); err != nil {
		return c.failIPv6StaticRoute(resource.Metadata.Name, status, receipt, "ApplyUnconfirmed", err)
	}
	receipt.Pending = false
	status["phase"], status["changed"], status["installedAt"] = "Installed", true, time.Now().UTC().Format(time.RFC3339Nano)
	if err := c.saveIPv6StaticRouteStatus(resource.Metadata.Name, status, receipt); err != nil {
		return err
	}
	if c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.ipv6.static_route.installed", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv6StaticRoute", Name: resource.Metadata.Name}
		event.Attributes = map[string]string{"destination": premise.Route.Destination, "gateway": premise.Route.Gateway, "device": premise.Route.Device, "dryRun": "false"}
		return c.Bus.Publish(ctx, event)
	}
	return nil
}

func ipv6StaticRouteStatus(spec api.IPv6StaticRouteSpec, route ipv6StaticRoute) map[string]any {
	return map[string]any{"phase": "Installed", "interface": spec.Interface, "device": route.Device, "destination": route.Destination, "gateway": route.Gateway, "metric": spec.Metric, "dryRun": false}
}

func ipv6StaticRouteStatusChanged(previous, next map[string]any) bool {
	for _, key := range []string{"interface", "device", "destination", "gateway", "metric", "dryRun"} {
		if fmt.Sprint(previous[key]) != fmt.Sprint(next[key]) {
			return true
		}
	}
	return false
}

func (c IPv4RouteController) saveIPv6StaticRouteStatus(name string, status map[string]any, receipt *ipv6StaticRouteReceipt) error {
	delete(status, "routeOwner")
	if receipt != nil {
		status["routeOwner"], status["managedBy"] = *receipt, "routerd"
	}
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6StaticRoute", name, status)
}

func (c IPv4RouteController) failIPv6StaticRoute(name string, status map[string]any, receipt *ipv6StaticRouteReceipt, reason string, effectErr error) error {
	status["phase"], status["reason"], status["error"] = "Error", reason, effectErr.Error()
	return errors.Join(effectErr, c.saveIPv6StaticRouteStatus(name, status, receipt))
}

func ipv6StaticRouteReceiptFromStatus(status map[string]any) (*ipv6StaticRouteReceipt, error) {
	if ordinaryRouteExternallyManaged(status) {
		return nil, nil
	}
	value, exists := status["routeOwner"]
	if !exists {
		return nil, nil
	}
	var receipt ipv6StaticRouteReceipt
	switch value := value.(type) {
	case ipv6StaticRouteReceipt:
		receipt = value
	case map[string]any:
		route, ok := value["route"].(map[string]any)
		if !ok {
			return nil, errors.New("invalid IPv6StaticRoute owned route")
		}
		receipt.Route = ipv6StaticRoute{Destination: routeStatusString(route["destination"]), Gateway: routeStatusString(route["gateway"]), Device: routeStatusString(route["device"]), Metric: routeStatusString(route["metric"])}
		var okScope, okToken, okPending bool
		receipt.Scope, okScope = value["scope"].(string)
		receipt.ParentToken, okToken = value["parentToken"].(string)
		receipt.Pending, okPending = value["pending"].(bool)
		index, ok := value["ifIndex"].(float64)
		if !ok || index < 1 || index > 2147483647 || index != float64(int(index)) || !okScope || !okToken || !okPending {
			return nil, errors.New("invalid IPv6StaticRoute ownership receipt")
		}
		receipt.IfIndex = int(index)
	default:
		return nil, errors.New("invalid IPv6StaticRoute ownership receipt")
	}
	var err error
	receipt.Route, err = normalizeIPv6StaticRoute(receipt.Route)
	if err != nil || receipt.Scope == "" || receipt.IfIndex < 1 {
		return nil, errors.New("IPv6StaticRoute receipt has no exact kernel/device identity")
	}
	return &receipt, nil
}

func (c IPv4RouteController) verifyIPv6StaticRouteIdentity(ctx context.Context, receipt ipv6StaticRouteReceipt) error {
	scope, err := c.ordinaryRouteScope(ctx)
	if err != nil {
		return err
	}
	if scope != receipt.Scope {
		return errors.New("IPv6StaticRoute receipt belongs to another kernel scope")
	}
	index, present, err := c.ordinaryRouteDevice(ctx, receipt.Route.Device)
	if err != nil {
		return err
	}
	if !present || index != receipt.IfIndex {
		return errors.New("IPv6StaticRoute native device was removed or replaced")
	}
	if receipt.ParentToken != "" {
		link, err := c.parentReader().observeRecorded(ctx, receipt.Route.Device, receipt.ParentToken)
		if err != nil {
			return err
		}
		if !link.Exists || link.Scope != scope || link.Index != index || link.IfName != receipt.Route.Device || link.Token != receipt.ParentToken {
			return errors.New("IPv6StaticRoute recorded native parent identity changed")
		}
		return nil
	}
	token, err := c.ordinaryRouteNativeParent(ctx, receipt.Route.Device, scope, index)
	if err != nil {
		return err
	}
	if token != "" {
		return errors.New("IPv6StaticRoute receipt lacks its native parent token")
	}
	return nil
}

func ipv6StaticRouteApplyCommand(route ipv6StaticRoute) (string, []string) {
	if platform.CurrentOS() == platform.OSFreeBSD {
		return "route", []string{"-n", "add", "-inet6", route.Destination, freeBSDIPv6Gateway(route)}
	}
	args := []string{"-6", "route", "add", route.Destination, "via", route.Gateway, "dev", route.Device}
	if route.Metric != "0" {
		args = append(args, "metric", route.Metric)
	}
	args = append(args, "proto", "boot")
	return "ip", args
}

func ipv6StaticRouteDeleteCommand(route ipv6StaticRoute) (string, []string) {
	if platform.CurrentOS() == platform.OSFreeBSD {
		return "route", []string{"-n", "delete", "-inet6", route.Destination, freeBSDIPv6Gateway(route)}
	}
	args := []string{"-6", "route", "del", route.Destination, "via", route.Gateway, "dev", route.Device}
	if route.Metric != "0" {
		args = append(args, "metric", route.Metric)
	}
	args = append(args, "proto", "boot")
	return "ip", args
}

func freeBSDIPv6Gateway(route ipv6StaticRoute) string {
	gateway, _ := netip.ParseAddr(route.Gateway)
	if gateway.IsLinkLocalUnicast() {
		return route.Gateway + "%" + route.Device
	}
	return route.Gateway
}

type ipv6StaticRouteObservation struct {
	Destination string `json:"dst"`
	Device      string `json:"dev"`
	Gateway     string `json:"gateway"`
	Metric      int    `json:"metric"`
	Protocol    string `json:"protocol"`
}

func (c IPv4RouteController) observeIPv6StaticRoute(ctx context.Context, wanted ipv6StaticRoute) (bool, bool, error) {
	if platform.CurrentOS() == platform.OSFreeBSD {
		return c.observeFreeBSDIPv6StaticRoute(ctx, wanted)
	}
	out, err := c.run(ctx, "ip", "-j", "-d", "-6", "route", "show", wanted.Destination)
	if err != nil {
		return false, false, fmt.Errorf("observe IPv6StaticRoute %s: %w: %s", wanted.Destination, err, strings.TrimSpace(string(out)))
	}
	var routes []ipv6StaticRouteObservation
	if err := json.Unmarshal(out, &routes); err != nil {
		return false, false, fmt.Errorf("decode IPv6StaticRoute observation: %w", err)
	}
	if routes == nil {
		return false, false, errors.New("IPv6StaticRoute observation is not a route array")
	}
	collision := false
	for _, observed := range routes {
		// iproute2 omits /128 for host routes and prints ::/0 as default.
		if observed.Destination == "default" {
			observed.Destination = "::/0"
		} else if address, err := netip.ParseAddr(observed.Destination); err == nil && address.Is6() {
			observed.Destination = netip.PrefixFrom(address, 128).String()
		}
		actual, err := normalizeIPv6StaticRoute(ipv6StaticRoute{Destination: observed.Destination, Gateway: observed.Gateway, Device: observed.Device, Metric: strconv.Itoa(observed.Metric)})
		if err != nil {
			return false, false, err
		}
		if actual.Destination != wanted.Destination || actual.Metric != wanted.Metric {
			continue
		}
		collision = true
		if actual == wanted && (observed.Protocol == "boot" || observed.Protocol == "3") {
			return true, true, nil
		}
	}
	return false, collision, nil
}

func (c IPv4RouteController) observeFreeBSDIPv6StaticRoute(ctx context.Context, wanted ipv6StaticRoute) (bool, bool, error) {
	out, err := c.run(ctx, "route", "-n", "get", "-inet6", wanted.Destination)
	if freeBSDRouteNeedsAdd(out) {
		return false, false, nil
	}
	if err != nil {
		if missingIPv4RouteDelete(err, out) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("observe FreeBSD IPv6StaticRoute: %w: %s", err, strings.TrimSpace(string(out)))
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok {
			values[key] = strings.TrimSpace(value)
		}
	}
	if values["destination"] == "" || values["gateway"] == "" || values["interface"] == "" || values["flags"] == "" {
		return false, false, errors.New("FreeBSD IPv6StaticRoute observation has no destination, gateway, interface, or flags")
	}
	destination, err := freeBSDIPv6RouteDestination(values["destination"], values["mask"], strings.Contains(values["flags"], "HOST"))
	if err != nil {
		return false, false, err
	}
	gateway := strings.SplitN(values["gateway"], "%", 2)[0]
	actual, err := normalizeIPv6StaticRoute(ipv6StaticRoute{Destination: destination, Gateway: gateway, Device: values["interface"], Metric: wanted.Metric})
	if err != nil {
		return false, false, err
	}
	collision := actual.Destination == wanted.Destination
	return actual == wanted && strings.Contains(values["flags"], "STATIC") && !strings.Contains(values["flags"], "REJECT"), collision, nil
}

func freeBSDIPv6RouteDestination(destination, mask string, host bool) (string, error) {
	address, err := netip.ParseAddr(destination)
	if err != nil || !address.Is6() {
		return "", errors.New("invalid FreeBSD IPv6StaticRoute destination")
	}
	if host {
		return netip.PrefixFrom(address, 128).String(), nil
	}
	parsedMask := net.ParseIP(mask)
	if parsedMask == nil {
		return "", errors.New("FreeBSD IPv6StaticRoute observation has no valid netmask")
	}
	ones, bits := net.IPMask(parsedMask).Size()
	if bits != 128 {
		return "", errors.New("FreeBSD IPv6StaticRoute observation has no IPv6 netmask")
	}
	return netip.PrefixFrom(address, ones).Masked().String(), nil
}

func (c IPv4RouteController) withdrawIPv6StaticRouteReceipt(ctx context.Context, receipt *ipv6StaticRouteReceipt) error {
	matching, _, err := c.observeIPv6StaticRoute(ctx, receipt.Route)
	if err != nil || !matching {
		return err
	}
	if receipt.Pending {
		return errors.New("pending IPv6StaticRoute is present; native ownership is unknown, inspect and remove it before retrying")
	}
	if err := c.verifyIPv6StaticRouteIdentity(ctx, *receipt); err != nil {
		return err
	}
	name, args := ipv6StaticRouteDeleteCommand(receipt.Route)
	out, err := c.run(ctx, name, args...)
	if err != nil && !missingIPv4RouteDelete(err, out) {
		return fmt.Errorf("withdraw IPv6StaticRoute: %s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	remaining, _, err := c.observeIPv6StaticRoute(ctx, receipt.Route)
	if err != nil {
		return err
	}
	if remaining {
		return errors.New("IPv6StaticRoute remains after delete")
	}
	return nil
}

func (c IPv4RouteController) cleanupRemovedIPv6StaticRoutes(ctx context.Context) error {
	lister, ok := c.Store.(interface {
		ListObjectStatuses() ([]routerstate.ObjectStatus, error)
	})
	if !ok {
		return errors.New("IPv6StaticRoute cleanup requires status enumeration")
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return err
	}
	desired := map[string]bool{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion == api.NetAPIVersion && resource.Kind == "IPv6StaticRoute" {
			desired[lifecycle.OwnerKey(resource.APIVersion, resource.Kind, resource.Metadata.Name)] = true
		}
	}
	for _, action := range lifecycle.PlanResourceTeardownGC(desired, statuses).Actions {
		if action.Type != lifecycle.GCActionTeardownResource || action.Status.APIVersion != api.NetAPIVersion || action.Status.Kind != "IPv6StaticRoute" {
			continue
		}
		if c.DryRun {
			continue
		}
		deleter, ok := c.Store.(interface {
			DeleteObject(apiVersion, kind, name string) error
		})
		if !ok {
			return errors.New("IPv6StaticRoute cleanup requires status deletion")
		}
		receipt, err := ipv6StaticRouteReceiptFromStatus(action.Status.Status)
		if err != nil {
			return err
		}
		if receipt != nil {
			if err := c.withdrawIPv6StaticRouteReceipt(ctx, receipt); err != nil {
				return fmt.Errorf("delete removed IPv6StaticRoute %s: %w", action.Status.Name, err)
			}
		}
		if err := deleter.DeleteObject(api.NetAPIVersion, "IPv6StaticRoute", action.Status.Name); err != nil {
			return err
		}
		if c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.ipv6.static_route.removed", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv6StaticRoute", Name: action.Status.Name}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}
