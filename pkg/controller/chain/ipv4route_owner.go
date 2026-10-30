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

// A pending receipt fences an uncertain add or delete. A plain native route
// has no private ownership cookie, so a restart may retire/retry pending work
// only after observing the exact tuple absent. Presence requires operator
// inspection and removal. A different identity waits for the old withdrawal.
type ipv4RouteReceipt struct {
	Route       ipv4RouteRemoval `json:"route"`
	Scope       string           `json:"scope"`
	IfIndex     int              `json:"ifIndex"`
	ParentToken string           `json:"parentToken"`
	Pending     bool             `json:"pending"`
}

type ipv4RouteStatusReader interface {
	ReadObjectStatus(string, string, string) (map[string]any, error)
}

type ordinaryRoutePremise struct {
	Route       ipv4RouteRemoval
	Scope       string
	IfIndex     int
	ParentToken string
	Active      bool
	Reason      string
}

// Unknown device enumeration is an error. Only a successful complete native
// enumeration can establish that a required device has disappeared.
func (c IPv4RouteController) ordinaryRouteDevice(ctx context.Context, name string) (int, bool, error) {
	if c.DeviceLookup != nil {
		index, present, err := c.DeviceLookup(ctx, name)
		if err == nil && present && index < 1 {
			return 0, false, errors.New("IPv4Route device observation has no index")
		}
		return index, present, err
	}
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	iface, err := (LinkController{}).lookupInterface(name)
	if err != nil {
		return 0, false, err
	}
	if iface == nil {
		return 0, false, nil
	}
	if iface.Index < 1 {
		return 0, false, errors.New("IPv4Route device has no native index")
	}
	return iface.Index, true, nil
}
func (c IPv4RouteController) ordinaryRouteScope(ctx context.Context) (string, error) {
	if c.NativeScope != nil {
		return c.NativeScope(ctx)
	}
	return platform.KernelNetworkScope(ctx)
}

func (c IPv4RouteController) parentReader() nativeParentReader {
	router := c.ParentRouter
	if router == nil {
		router = c.Router
	}
	return nativeParentReader{Router: router, Store: c.Store, Segments: c.Segments, DSLite: c.DSLite, Bridges: c.Bridges, WireGuard: c.WireGuard, VRFs: c.VRFs, Tunnels: c.Tunnels}
}

// Read the concrete parent's applied journal and native cookie after waits.
func (c IPv4RouteController) ordinaryRouteNativeParent(ctx context.Context, device, scope string, index int) (string, error) {
	reader := c.parentReader()
	link, err := reader.observeDeclared(ctx, device)
	if err != nil {
		return "", err
	}
	if !link.Declared {
		return "", nil
	}
	if !link.Exists || link.IfName != device || link.Scope != scope || link.Index != index {
		return "", errors.New("IPv4Route native parent identity is not proven")
	}
	return link.Token, nil
}

func (c IPv4RouteController) ordinaryRoutePremise(ctx context.Context, spec api.IPv4RouteSpec) (ordinaryRoutePremise, error) {
	var result ordinaryRoutePremise
	ready, err := resourcequery.ReadDependenciesReady(c.Store, spec.DependsOn)
	if err != nil {
		return result, err
	}
	if !ready {
		result.Reason = "DependsOnFalse"
		return result, nil
	}
	routeType := firstNonEmpty(strings.TrimSpace(spec.Type), "unicast")
	gateway, err := resourcequery.ReadValue(c.Store, spec.GatewayFrom)
	if err != nil {
		return result, err
	}
	device := ""
	if routeType != "blackhole" {
		device, err = resourcequery.ReadValue(c.Store, spec.DeviceFrom)
		if err != nil {
			return result, err
		}
		device = firstNonEmpty(device, strings.TrimSpace(spec.Device))
		if device == "" {
			result.Reason = "DeviceMissing"
			return result, nil
		}
		if !c.DryRun {
			var present bool
			result.IfIndex, present, err = c.ordinaryRouteDevice(ctx, device)
			if err != nil {
				return result, err
			}
			if !present {
				result.Reason = "DeviceNotReady"
				return result, nil
			}
		}
	}
	preferred := strings.TrimSpace(spec.PreferredSource)
	if !c.DryRun && preferred != "" && routeType != "blackhole" {
		local, err := c.ordinaryRoutePreferredSourceIsLocal(ctx, preferred)
		if err != nil {
			return result, err
		}
		if !local {
			preferred = ""
		}
	}
	result.Route, err = normalizeOrdinaryRoute(ipv4RouteRemoval{Type: routeType, Destination: firstNonEmpty(spec.Destination, "0.0.0.0/0"), Device: device, Gateway: firstNonEmpty(gateway, strings.TrimSpace(spec.Gateway)), PreferredSource: preferred, Metric: strconv.Itoa(spec.Metric)})
	if err != nil {
		return result, err
	}
	if !c.DryRun {
		result.Scope, err = c.ordinaryRouteScope(ctx)
		if err != nil {
			return result, err
		}
		if result.Scope == "" {
			return result, errors.New("IPv4Route kernel scope is unavailable")
		}
		if result.Route.Type != "blackhole" {
			result.ParentToken, err = c.ordinaryRouteNativeParent(ctx, device, result.Scope, result.IfIndex)
			if err != nil {
				return result, err
			}
		}
	}
	result.Active = true
	return result, nil
}

func (c IPv4RouteController) reconcileOrdinaryRoute(ctx context.Context, resource api.Resource, spec api.IPv4RouteSpec) error {
	previous, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "IPv4Route", resource.Metadata.Name)
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
	premise, err := c.ordinaryRoutePremise(ctx, spec)
	if err != nil {
		return err
	}
	status := map[string]any{"phase": "Pending", "reason": premise.Reason, "dryRun": c.DryRun}
	if !premise.Active {
		if premise.Reason == "DependsOnFalse" {
			phase, err := dependencyUnavailablePhase(c.Router, c.Store, spec.DependsOn, standbyHealthcheckRoute(resource.Metadata.Name, spec))
			if err != nil {
				return err
			}
			dependencies, err := dependencyStatusSnapshot(c.Store, spec.DependsOn)
			if err != nil {
				return err
			}
			status["phase"], status["dependencies"] = phase, dependencies
		}
		return c.withdrawInactiveOrdinaryRoute(ctx, resource.Metadata.Name, status, receipt)
	}
	desired := premise.Route
	status = map[string]any{"phase": "Installed", "type": desired.Type, "destination": desired.Destination, "device": desired.Device, "gateway": desired.Gateway, "preferredSource": strings.TrimSpace(spec.PreferredSource), "effectivePreferredSource": desired.PreferredSource, "metric": spec.Metric, "dryRun": c.DryRun}
	if spec.PreferredSource != "" && desired.PreferredSource == "" && desired.Type != "blackhole" {
		status["preferredSourceSkipped"], status["preferredSourceSkipReason"] = true, "LocalAddressMissing"
	}
	changed := routeInstallStatusChanged(previous, status)
	if value, ok := previous["installedAt"]; ok && !changed {
		status["installedAt"] = value
	}
	if c.DryRun {
		status["phase"] = "Rendered"
		return c.saveOrdinaryRouteStatus(resource.Metadata.Name, status, receipt)
	}
	if platform.CurrentOS() == platform.OSFreeBSD && spec.Metric != 0 {
		return c.failOrdinaryRoute(resource.Metadata.Name, status, receipt, "UnsupportedMetric", errors.New("FreeBSD IPv4Route does not support a Linux route metric"))
	}
	if receipt != nil {
		matching, _, err := c.observeOrdinaryRoute(ctx, receipt.Route)
		if err != nil {
			return c.failOrdinaryRoute(resource.Metadata.Name, status, receipt, "ObserveFailed", err)
		}
		if matching && receipt.Pending {
			return c.failOrdinaryRoute(resource.Metadata.Name, status, receipt, "OwnershipUnknown", errors.New("pending IPv4Route is present without a private native ownership cookie; inspect and remove it before retrying"))
		}
		if matching && receipt.Route == desired {
			if err := c.verifyOrdinaryRouteIdentity(ctx, *receipt); err != nil {
				return c.failOrdinaryRoute(resource.Metadata.Name, status, receipt, "OwnershipUnknown", err)
			}
			status["changed"], status["kernelRouteAlreadyCurrent"] = changed, true
			return c.saveOrdinaryRouteStatus(resource.Metadata.Name, status, receipt)
		}
		if err := c.withdrawOrdinaryReceipt(ctx, resource.Metadata.Name, status, receipt); err != nil {
			return c.failOrdinaryRoute(resource.Metadata.Name, status, receipt, "WithdrawFailed", err)
		}
		receipt = nil
		if err := c.saveOrdinaryRouteStatus(resource.Metadata.Name, map[string]any{"phase": "Pending", "dryRun": false}, nil); err != nil {
			return err
		}
	}
	_, collision, err := c.observeOrdinaryRoute(ctx, desired)
	if err != nil {
		return c.failOrdinaryRoute(resource.Metadata.Name, status, receipt, "ObserveFailed", err)
	}
	if collision {
		return c.failOrdinaryRoute(resource.Metadata.Name, status, nil, "ForeignRoutePreserved", errors.New("existing route has no matching applied ownership"))
	}
	receipt = &ipv4RouteReceipt{Route: desired, Scope: premise.Scope, IfIndex: premise.IfIndex, ParentToken: premise.ParentToken, Pending: true}
	status["phase"] = "Applying"
	if err := c.saveOrdinaryRouteStatus(resource.Metadata.Name, status, receipt); err != nil {
		return err
	}
	// Persistence can wait. Recheck both observations and desired authority before
	// the exclusive acquisition; this attempt has not issued a native write yet.
	fresh, err := c.ordinaryRoutePremise(ctx, spec)
	if err != nil {
		return c.failOrdinaryRoute(resource.Metadata.Name, status, receipt, "ObserveFailed", err)
	}
	_, collision, err = c.observeOrdinaryRoute(ctx, desired)
	if err != nil {
		return c.failOrdinaryRoute(resource.Metadata.Name, status, receipt, "ObserveFailed", err)
	}
	if fresh != premise || collision {
		return c.failOrdinaryRoute(resource.Metadata.Name, status, nil, "AcquisitionPremiseChanged", errors.New("IPv4Route premise changed after pending persistence; no native add was issued"))
	}
	name, args := ordinaryRouteApplyCommand(desired)
	out, err := c.run(ctx, name, args...)
	if err != nil {
		return c.failOrdinaryRoute(resource.Metadata.Name, status, receipt, "ApplyFailed", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out))))
	}
	matching, _, err := c.observeOrdinaryRoute(ctx, desired)
	if err != nil || !matching {
		return c.failOrdinaryRoute(resource.Metadata.Name, status, receipt, "ApplyUnconfirmed", errors.Join(err, errors.New("IPv4Route add did not read back the exact native tuple")))
	}
	if err := c.verifyOrdinaryRouteIdentity(ctx, *receipt); err != nil {
		return c.failOrdinaryRoute(resource.Metadata.Name, status, receipt, "ApplyUnconfirmed", err)
	}
	fresh, err = c.ordinaryRoutePremise(ctx, spec)
	if err != nil {
		return c.failOrdinaryRoute(resource.Metadata.Name, status, receipt, "ObserveFailed", err)
	}
	if fresh != premise {
		// This invocation has the exclusive-add acknowledgement and exact readback.
		// It may withdraw that known effect without publishing Installed first.
		known := *receipt
		known.Pending = false
		if err := c.withdrawOrdinaryReceipt(ctx, resource.Metadata.Name, status, &known); err != nil {
			return c.failOrdinaryRoute(resource.Metadata.Name, status, &known, "WithdrawFailed", err)
		}
		return c.saveOrdinaryRouteStatus(resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "PremiseChanged", "dryRun": false}, nil)
	}
	receipt.Pending = false
	status["phase"], status["changed"], status["installedAt"] = "Installed", true, time.Now().UTC().Format(time.RFC3339Nano)
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

func (c IPv4RouteController) verifyOrdinaryRouteIdentity(ctx context.Context, receipt ipv4RouteReceipt) error {
	scope, err := c.ordinaryRouteScope(ctx)
	if err != nil {
		return err
	}
	if scope != receipt.Scope {
		return errors.New("IPv4Route receipt belongs to another kernel scope")
	}
	if receipt.Route.Type == "blackhole" {
		return nil
	}
	index, present, err := c.ordinaryRouteDevice(ctx, receipt.Route.Device)
	if err != nil {
		return err
	}
	if !present || index != receipt.IfIndex {
		return errors.New("IPv4Route native device was removed or replaced")
	}
	if receipt.ParentToken != "" {
		reader := c.parentReader()
		link, err := reader.observeRecorded(ctx, receipt.Route.Device, receipt.ParentToken)
		if err != nil {
			return err
		}
		if !link.Exists || link.Scope != scope || link.Index != index || link.IfName != receipt.Route.Device || link.Token != receipt.ParentToken {
			return errors.New("IPv4Route recorded native parent identity changed")
		}
		return nil
	}
	token, err := c.ordinaryRouteNativeParent(ctx, receipt.Route.Device, scope, index)
	if err != nil {
		return err
	}
	if token != "" {
		return errors.New("IPv4Route receipt lacks its native parent token")
	}
	return nil
}

func (c IPv4RouteController) withdrawOrdinaryReceipt(ctx context.Context, name string, status map[string]any, receipt *ipv4RouteReceipt) error {
	matching, _, err := c.observeOrdinaryRoute(ctx, receipt.Route)
	if err != nil || !matching {
		return err
	}
	if receipt.Pending {
		return errors.New("pending IPv4Route is present; native ownership is unknown, inspect and remove it before retrying")
	}
	if err := c.verifyOrdinaryRouteIdentity(ctx, *receipt); err != nil {
		return err
	}
	previous, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "IPv4Route", name)
	if err != nil {
		return err
	}
	known := *receipt
	pending := *receipt
	pending.Pending = true
	pendingStatus := copyStatusMap(status)
	pendingStatus["phase"] = "Withdrawing"
	if err := c.saveOrdinaryRouteStatus(name, pendingStatus, &pending); err != nil {
		return err
	}
	*receipt = pending
	current, err := c.ordinaryRoutePremiseForName(ctx, name)
	if err != nil {
		return err
	}
	if err := c.verifyOrdinaryRouteIdentity(ctx, pending); err != nil {
		return err
	}
	if current.Active && current.Route == known.Route {
		// The dependency/source may have recovered while the pending write
		// waited. No delete has been issued; retain the known native effect.
		matching, _, err := c.observeOrdinaryRoute(ctx, known.Route)
		if err != nil || !matching {
			return errors.Join(err, errors.New("reactivated IPv4Route native presence is unconfirmed"))
		}
		if err := c.saveOrdinaryRouteStatus(name, copyStatusMap(previous), &known); err != nil {
			return err
		}
		*receipt = known
		// An explicit error prevents both inactive reconciliation and removed
		// resource cleanup from forgetting the restored receipt as withdrawn.
		return errors.New("IPv4Route current intent became active; no native delete was issued")
	}
	return c.removeOrdinaryRoute(ctx, pending.Route)
}

func (c IPv4RouteController) ordinaryRoutePremiseForName(ctx context.Context, name string) (ordinaryRoutePremise, error) {
	if c.Router == nil {
		return ordinaryRoutePremise{}, errors.New("IPv4Route current router is unavailable")
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "IPv4Route" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.IPv4RouteSpec()
		if err != nil {
			return ordinaryRoutePremise{}, err
		}
		return c.ordinaryRoutePremise(ctx, spec)
	}
	return ordinaryRoutePremise{}, nil
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
		if link.AddrInfo == nil {
			return false, errors.New("preferred source observation has no address array")
		}
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
		if err := c.withdrawOrdinaryReceipt(ctx, name, status, receipt); err != nil {
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
		receipt.Scope, ok = value["scope"].(string)
		if !ok {
			return nil, errors.New("invalid IPv4Route scope")
		}
		index, ok := value["ifIndex"].(float64)
		if !ok || index < 0 || index > 2147483647 || index != float64(int(index)) {
			return nil, errors.New("invalid IPv4Route index")
		}
		receipt.IfIndex = int(index)
		receipt.ParentToken, ok = value["parentToken"].(string)
		if !ok {
			return nil, errors.New("invalid IPv4Route parent ownership")
		}
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
	if receipt.Scope == "" || receipt.Route.Type != "blackhole" && receipt.IfIndex < 1 || receipt.Route.Type == "blackhole" && receipt.IfIndex != 0 {
		return nil, errors.New("IPv4Route receipt has no exact kernel/device identity")
	}
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
		name, args := freeBSDIPv4RouteApplyCommand(route.Type, route.Destination, route.Device, route.Gateway, route.PreferredSource)
		return name, freeBSDIPv4RouteAddArgs(args)
	}
	args := []string{"route", "add", route.Destination}
	if route.Type == "blackhole" {
		args = []string{"route", "add", "blackhole", route.Destination}
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
	args = append(args, "proto", "boot")
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
	remaining, _, readErr := c.observeOrdinaryRoute(ctx, route)
	if readErr != nil {
		return readErr
	}
	if remaining {
		return errors.New("IPv4Route remains after delete")
	}
	return nil
}

func (c IPv4RouteController) observeFreeBSDOrdinaryRoute(ctx context.Context, wanted ipv4RouteRemoval) (bool, bool, error) {
	args := append([]string{"-n", "get"}, freeBSDRouteDestinationArgs(wanted.Destination)...)
	out, err := c.run(ctx, "route", args...)
	// FreeBSD route get can report an absent route while exiting successfully.
	if freeBSDRouteNeedsAdd(out) {
		return false, false, nil
	}
	if err != nil {
		if missingIPv4RouteDelete(err, out) {
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
	return actual == comparison && strings.Contains(values["flags"], "STATIC") && !strings.Contains(values["flags"], "PROTO1") && !strings.Contains(values["flags"], "PROTO2") && !strings.Contains(values["flags"], "REJECT"), collision, nil
}
