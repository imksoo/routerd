// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/lifecycle"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type DHCPv6InformationController struct {
	Router        *api.Router
	Bus           *bus.Bus
	Store         Store
	DaemonSockets map[string]string
	Logger        *slog.Logger
}

func (c DHCPv6InformationController) Start(ctx context.Context) {
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.dhcpv6.client.prefix.*", "routerd.dhcpv6.client.info.*"}}, 32)
	go func() {
		for event := range ch {
			if event.Resource == nil {
				continue
			}
			request := !strings.HasPrefix(event.Type, "routerd.dhcpv6.client.info.")
			if err := c.reconcile(ctx, event.Resource.Name, request); err != nil && c.Logger != nil {
				c.Logger.Warn("dhcpv6 information reconcile failed", "pd", event.Resource.Name, "error", err)
			}
		}
	}()
}

func (c DHCPv6InformationController) reconcile(ctx context.Context, pdName string, request bool) error {
	pdStatus := c.Store.ObjectStatus(api.NetAPIVersion, "DHCPv6PrefixDelegation", pdName)
	if pdStatus["phase"] != daemonapi.ResourcePhaseBound {
		return nil
	}
	socket := c.socketFor(pdName)
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DHCPv6Information" {
			continue
		}
		spec, err := resource.DHCPv6InformationSpec()
		if err != nil {
			return err
		}
		if !c.matchesPD(resource, spec, pdName) {
			continue
		}
		if !clientSocketReady(socket) {
			if err := c.saveDHCPv6InformationPending(resource.Metadata.Name, pdName, socket); err != nil {
				return err
			}
			continue
		}
		if request {
			_, _ = postDaemonCommand(ctx, socket, daemonapi.CommandInfoRequest)
		}
		status, err := daemonStatus(ctx, socket)
		if err != nil {
			return err
		}
		observed := daemonObserved(status, "DHCPv6PrefixDelegation", pdName)
		next := map[string]any{
			"phase":        "Ready",
			"aftrName":     observed["aftrName"],
			"dnsServers":   decodeStringList(observed["dnsServers"]),
			"sntpServers":  decodeStringList(observed["sntpServers"]),
			"domainSearch": decodeStringList(observed["domainSearch"]),
			"source":       pdName,
		}
		changed := objectStatusChanged("DHCPv6Information", c.Store.ObjectStatus(api.NetAPIVersion, "DHCPv6Information", resource.Metadata.Name), next)
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6Information", resource.Metadata.Name, next); err != nil {
			return err
		}
		if changed && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.dhcpv6.info.updated", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Information", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"aftrName": observed["aftrName"], "source": pdName}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c DHCPv6InformationController) saveDHCPv6InformationPending(name, pdName, socket string) error {
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6Information", name, map[string]any{
		"phase":   "Pending",
		"reason":  "DHCPv6ClientSocketPending",
		"source":  pdName,
		"socket":  socket,
		"message": fmt.Sprintf("waiting for DHCPv6 client socket %s", socket),
	})
}

func (c DHCPv6InformationController) matchesPD(resource api.Resource, spec api.DHCPv6InformationSpec, pdName string) bool {
	for _, owner := range resource.Metadata.OwnerRefs {
		if owner.Kind == "DHCPv6PrefixDelegation" && owner.Name == pdName {
			return true
		}
	}
	for _, candidate := range c.Router.Spec.Resources {
		if candidate.Kind != "DHCPv6PrefixDelegation" || candidate.Metadata.Name != pdName {
			continue
		}
		pdSpec, err := candidate.DHCPv6PrefixDelegationSpec()
		return err == nil && pdSpec.Interface == spec.Interface
	}
	return false
}

func (c DHCPv6InformationController) socketFor(resource string) string {
	if socket := c.DaemonSockets[resource]; socket != "" {
		return socket
	}
	return filepath.Join("/run/routerd/dhcpv6-client", resource+".sock")
}

type DSLiteTunnelController struct {
	OS                   platform.OS
	NativeScope          func(context.Context) (string, error)
	NativeInterfaceIndex func(string) (int, error)
	Router               *api.Router
	DeclaredRouter       *api.Router
	Bus                  *bus.Bus
	Store                Store
	DryRun               bool
	ResolverPort         int
	Logger               *slog.Logger
	Now                  func() time.Time
	CommandOutput        func(context.Context, string, ...string) ([]byte, error)
}

func (c DSLiteTunnelController) Start(ctx context.Context) {
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.dhcpv6.info.*", "routerd.dhcpv6.client.prefix.*", "routerd.dns.resolver.*", "routerd.resource.status.changed"}}, 32)
	go func() {
		if err := c.reconcile(ctx); err != nil && c.Logger != nil && ctx.Err() == nil {
			c.Logger.Warn("dslite tunnel initial reconcile failed", "error", err)
		}
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					return
				}
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
			if err := c.reconcile(ctx); err != nil && c.Logger != nil && ctx.Err() == nil {
				c.Logger.Warn("dslite tunnel reconcile failed", "error", err)
			}
		}
	}()
}

func (c DSLiteTunnelController) reconcile(ctx context.Context) error {
	if err := c.cleanupNativeTunnels(ctx); err != nil {
		return err
	}
	if err := c.cleanupDelegatedEndpoints(ctx); err != nil {
		return err
	}
	var failures []string
	declared := c.DeclaredRouter
	if declared == nil {
		declared = c.Router
	}
	desired := map[string]struct{}{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DSLiteTunnel" {
			continue
		}
		desired[resource.Metadata.Name] = struct{}{}
	}
	for _, resource := range declared.Spec.Resources {
		if resource.Kind != "DSLiteTunnel" {
			continue
		}
		spec, err := resource.DSLiteTunnelSpec()
		if err != nil {
			return err
		}
		ifname := firstNonEmpty(spec.TunnelName, spec.Interface, resource.Metadata.Name)
		if _, active := desired[resource.Metadata.Name]; active {
			continue
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": PhaseDisabled, "reason": "WhenFalse", "interface": ifname, "dryRun": c.DryRun}); err != nil {
			return err
		}
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DSLiteTunnel" {
			continue
		}
		spec, err := resource.DSLiteTunnelSpec()
		if err != nil {
			return err
		}
		if !api.BoolDefault(spec.Enabled, true) {
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": PhaseDisabled, "reason": "Disabled"}); err != nil {
				return err
			}
			continue
		}
		ready, err := resourcequery.ReadDependenciesReady(c.Store, spec.DependsOn)
		if err != nil {
			return err
		}
		if !ready {
			if err := c.withdrawNativeTunnelResource(ctx, resource.Metadata.Name); err != nil {
				return err
			}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "DependsOnFalse"}); err != nil {
				return err
			}
			continue
		}
		aftrSource, err := resourcequery.ReadValue(c.Store, spec.AFTRFrom)
		if err != nil {
			return err
		}
		aftrName, remote, err := c.resolveRemote(ctx, spec, aftrSource)
		if err != nil && aftrName == "" {
			if err := c.withdrawNativeTunnelResource(ctx, resource.Metadata.Name); err != nil {
				return err
			}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "AFTRMissing"}); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "AFTRResolveFailed", "aftrName": aftrName, "error": err.Error()}); err != nil {
				return err
			}
			continue
		}
		local, localIfName, err := c.localAddress(ctx, spec)
		if err != nil {
			return err
		}
		if local == "" {
			if err := c.withdrawNativeTunnelResource(ctx, resource.Metadata.Name); err != nil {
				return err
			}
			reason := "LocalIPv6Missing"
			if err != nil {
				reason = err.Error()
			}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": reason, "aftrName": aftrName, "aftrIPv6": remote}); err != nil {
				return err
			}
			continue
		}
		ifname := firstNonEmpty(spec.TunnelName, spec.Interface, resource.Metadata.Name)
		if c.currentOS() == platform.OSFreeBSD {
			ifname = api.DSLiteRuntimeIfName(ifname, "freebsd")
		}
		mtu := spec.MTU
		if mtu == 0 {
			mtu = 1460
		}
		innerLocal, innerLocalPending, err := dsliteInnerLocalIPv4(c.Router, c.Store, spec)
		if innerLocalPending != "" {
			if err := c.withdrawNativeTunnelResource(ctx, resource.Metadata.Name); err != nil {
				return err
			}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": innerLocalPending, "aftrName": aftrName, "aftrIPv6": remote, "dryRun": c.DryRun}); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		status := map[string]any{"phase": "Up", "interface": ifname, "tunnelName": ifname, "device": ifname, "localIPv6": local, "innerLocalIPv4": innerLocal, "innerRemoteIPv4": dsliteInnerRemoteIPv4, "localInterface": localIfName, "aftrName": aftrName, "aftrIPv6": remote, "mtu": mtu, "dryRun": c.DryRun}
		previous, err := c.endpointStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name)
		if err != nil {
			return err
		}
		changed := objectStatusChanged("DSLiteTunnel", previous, status)
		if c.DryRun {
			status["phase"] = "Rendered"
		}
		if !c.DryRun {
			if localIfName != "" {
				delegatedSource := firstNonEmpty(spec.LocalAddressSource, "interface") == "delegatedAddress"
				pending := ""
				var endpointErr error
				if delegatedSource {
					pending, endpointErr = c.ensureDelegatedEndpoint(ctx, resource.Metadata.Name, spec, localIfName, local)
				}
				if endpointErr != nil {
					err := endpointErr
					failures = append(failures, fmt.Sprintf("%s: %v", resource.Metadata.Name, err))
					if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Error", "reason": "LocalEndpointApplyFailed", "interface": ifname, "localIPv6": local, "aftrIPv6": remote, "error": err.Error(), "dryRun": c.DryRun}); err != nil {
						return err
					}
					continue
				}
				if pending != "" {
					if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": pending, "interface": ifname, "localIPv6": local, "aftrIPv6": remote, "dryRun": c.DryRun}); err != nil {
						return err
					}
					continue
				}
			}
			plan := c.nativeTunnelPlan(resource.Metadata.Name, spec, local, remote, innerLocal)
			if c.currentOS() == platform.OSFreeBSD && plan.EncapLimit != "none" {
				return fmt.Errorf("FreeBSD DS-Lite does not support custom encapsulationLimit")
			}
			if c.currentOS() == platform.OSFreeBSD && spec.DefaultRoute && spec.RouteMetric != 0 {
				return fmt.Errorf("FreeBSD DS-Lite default route does not support routeMetric")
			}
			if err := c.ensureNativeTunnel(ctx, resource.Metadata.Name, spec, plan); err != nil {
				if errors.Is(err, errDSLiteInputsChanged) {
					if err := c.cleanupDelegatedEndpoints(ctx); err != nil {
						return err
					}
					if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "SourceOrDependencyChanged", "interface": ifname, "dryRun": false}); err != nil {
						return err
					}
					continue
				}
				failures = append(failures, fmt.Sprintf("%s: %v", resource.Metadata.Name, err))
				if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Error", "reason": "TunnelApplyFailed", "interface": ifname, "localIPv6": local, "aftrIPv6": remote, "error": err.Error(), "dryRun": false}); err != nil {
					return err
				}
				continue
			}
			if spec.LocalAddressSource == "delegatedAddress" {
				pending, err := c.recheckDelegatedEndpoint(ctx, spec, localIfName, local)
				if err != nil {
					return err
				}
				if pending != "" {
					if err := c.withdrawNativeTunnelResource(ctx, resource.Metadata.Name); err != nil {
						return err
					}
					status["phase"], status["reason"] = "Pending", pending
				}
			}
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, status); err != nil {
			return err
		}
		if status["phase"] == "Up" && changed && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.tunnel.ds-lite.up", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"interface": ifname, "aftrIPv6": remote, "innerLocalIPv4": innerLocal, "dryRun": fmt.Sprintf("%t", c.DryRun)}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

func (c DSLiteTunnelController) resolveRemote(ctx context.Context, spec api.DSLiteTunnelSpec, observed string) (string, string, error) {
	var firstName string
	candidates := []string{
		observed,
		spec.AFTRFQDN,
		spec.AFTRIPv6,
		spec.RemoteAddress,
	}
	var lastErr error
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if firstName == "" {
			firstName = candidate
		}
		remote, err := resolveAFTRIPv6(ctx, candidate, c.ResolverPort)
		if err == nil {
			return candidate, remote, nil
		}
		lastErr = err
	}
	if firstName == "" {
		return "", "", fmt.Errorf("missing AFTR source")
	}
	return firstName, "", lastErr
}

type IPv4RouteController struct {
	ParentRouter      *api.Router
	Router            *api.Router
	Segments          *VXLANSegmentController
	DSLite            *DSLiteTunnelController
	Bridges           *BridgeController
	WireGuard         *WireGuardController
	VRFs              *VRFController
	Tunnels           *TunnelInterfaceController
	Bus               *bus.Bus
	Store             Store
	MobilityDataplane dynamicconfig.MobilityDataplanePlan
	MobilitySource    dynamicConfigPartLister
	DryRun            bool
	Logger            *slog.Logger
	Command           func(ctx context.Context, name string, args ...string) ([]byte, error)
	DevicePresent     func(context.Context, string) bool
	DeviceLookup      func(context.Context, string) (int, bool, error)
	NativeScope       func(context.Context) (string, error)
}

func (c IPv4RouteController) Start(ctx context.Context) {
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.tunnel.ds-lite.*", "routerd.lan.route.changed"}}, 32)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					return
				}
				if err := c.reconcile(ctx); err != nil && c.Logger != nil {
					c.Logger.Warn("ipv4 route reconcile failed", "error", err)
				}
			case <-ticker.C:
				if err := c.reconcile(ctx); err != nil && c.Logger != nil {
					c.Logger.Warn("ipv4 route periodic reconcile failed", "error", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c IPv4RouteController) reconcile(ctx context.Context) error {
	if err := c.cleanupRemovedRoutes(ctx); err != nil {
		return err
	}
	var failures []string
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "IPv4Route" {
			continue
		}
		spec, err := resource.IPv4RouteSpec()
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", resource.Metadata.Name, err))
			continue
		}
		if err := c.reconcileOrdinaryRoute(ctx, resource, spec); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", resource.Metadata.Name, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return c.reconcileMobilityRoutes(ctx)
}

func ipv4RouteInstalled(ctx context.Context, command outputCommandFunc, routeType, destination, device, gateway, preferredSource string, metric int) bool {
	if command == nil {
		command = runOutputCommandContext
	}
	queryDestination := destination
	if destination == "" || destination == "0.0.0.0/0" {
		queryDestination = "default"
	}
	out, err := command(ctx, "ip", "route", "show", queryDestination)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if ipv4RouteLineMatches(line, routeType, queryDestination, device, gateway, preferredSource, metric) {
			return true
		}
	}
	return false
}

func ipv4RouteLineMatches(line, routeType, destination, device, gateway, preferredSource string, metric int) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	if routeType == "blackhole" {
		if len(fields) < 2 || fields[0] != "blackhole" || fields[1] != destination {
			return false
		}
	} else if !routeLineMatchesDestination(fields[0], destination) {
		return false
	}
	if device != "" && !routeFieldsContainPair(fields, "dev", device) {
		return false
	}
	if gateway != "" && !routeFieldsContainPair(fields, "via", gateway) {
		return false
	}
	if preferredSource != "" && !routeFieldsContainPair(fields, "src", preferredSource) {
		return false
	}
	if metric > 0 && !routeFieldsContainPair(fields, "metric", fmt.Sprintf("%d", metric)) {
		return false
	}
	return true
}

func routeFieldsContainPair(fields []string, key, value string) bool {
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == key && fields[i+1] == value {
			return true
		}
	}
	return false
}

func ipv4PreferredSourceIsLocal(ctx context.Context, command outputCommandFunc, preferredSource string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(preferredSource))
	if err != nil || !addr.Is4() {
		return false
	}
	if command == nil {
		command = runOutputCommandContext
	}
	switch platform.CurrentOS() {
	case platform.OSFreeBSD:
		out, err := command(ctx, "ifconfig", "-a", "inet")
		return err == nil && ifconfigHasIPv4Address(out, addr.String())
	default:
		out, err := command(ctx, "ip", "-j", "-4", "addr", "show")
		return err == nil && ipJSONHasIPv4Address(out, addr.String())
	}
}

func ipJSONHasIPv4Address(data []byte, address string) bool {
	var links []ipJSONLink
	if err := json.Unmarshal(data, &links); err != nil {
		return false
	}
	for _, link := range links {
		for _, info := range link.AddrInfo {
			if info.Family == "inet" && info.Local == address {
				return true
			}
		}
	}
	return false
}

func (c IPv4RouteController) cleanupRemovedRoutes(ctx context.Context) error {
	if c.Store == nil {
		return errors.New("IPv4Route cleanup store is unavailable")
	}
	lister, ok := c.Store.(interface {
		ListObjectStatuses() ([]routerstate.ObjectStatus, error)
	})
	if !ok {
		return errors.New("IPv4Route cleanup requires status enumeration")
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return err
	}
	desired := map[string]bool{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion == api.NetAPIVersion && resource.Kind == "IPv4Route" {
			desired[lifecycle.OwnerKey(resource.APIVersion, resource.Kind, resource.Metadata.Name)] = true
		}
	}
	plan := lifecycle.PlanResourceTeardownGC(desired, statuses)
	for _, action := range plan.Actions {
		if action.Type != lifecycle.GCActionTeardownResource {
			continue
		}
		status := action.Status
		if status.APIVersion != api.NetAPIVersion || status.Kind != "IPv4Route" {
			continue
		}
		deleter, ok := c.Store.(interface {
			DeleteObject(apiVersion, kind, name string) error
		})
		if !ok {
			return errors.New("IPv4Route cleanup requires status deletion")
		}
		if err := c.teardownRemovedRoute(ctx, status, deleter); err != nil {
			return err
		}
	}
	return nil
}

func (c IPv4RouteController) teardownRemovedRoute(ctx context.Context, status routerstate.ObjectStatus, deleter routerstate.ObjectDeleteStore) error {
	if c.DryRun {
		return nil
	}
	receipt, err := ipv4RouteReceiptFromStatus(status.Status)
	if err != nil {
		return err
	}
	if receipt != nil {
		if err := c.withdrawOrdinaryReceipt(ctx, status.Name, map[string]any{"phase": "Withdrawing", "dryRun": false}, receipt); err != nil {
			return fmt.Errorf("delete removed IPv4Route %s: %w", status.Name, err)
		}
	}
	if err := deleter.DeleteObject(api.NetAPIVersion, "IPv4Route", status.Name); err != nil {
		return err
	}
	if c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.ipv4.route.removed", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv4Route", Name: status.Name}
		if receipt != nil {
			event.Attributes = map[string]string{"destination": receipt.Route.Destination, "device": receipt.Route.Device}
		}
		if err := c.Bus.Publish(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

// ipv4RouteRemoval identifies the exact route being withdrawn.  The regular
// IPv4Route controller constructs it at the persisted-status boundary; typed
// Mobility dataplane effects construct it directly from their applied-effect
// ledger.  Keeping the removal logic typed avoids treating a status map as an
// internal route-planning API.
type ipv4RouteRemoval struct {
	Type            string `json:"type"`
	Destination     string `json:"destination"`
	Device          string `json:"device"`
	Gateway         string `json:"gateway"`
	PreferredSource string `json:"preferredSource"`
	Metric          string `json:"metric"`
}

func ipv4RouteRemovalFromStatus(status map[string]any) ipv4RouteRemoval {
	removal := ipv4RouteRemoval{
		Type:        routeStatusString(status["type"]),
		Destination: routeStatusString(status["destination"]),
		Device:      routeStatusString(status["device"]),
		Gateway:     routeStatusString(status["gateway"]),
	}
	if removal.Type == "" {
		removal.Type = "unicast"
	}
	removal.PreferredSource = routeStatusString(status["effectivePreferredSource"])
	if removal.PreferredSource == "" {
		removal.PreferredSource = routeStatusString(status["preferredSource"])
	}
	removal.Metric = routeStatusString(status["metric"])
	return removal
}

func routeStatusString(value any) string {
	text := fmt.Sprint(value)
	if text == "<nil>" {
		return ""
	}
	return text
}

func removedIPv4RouteRemovalIsCurrentlyBGP(ctx context.Context, run outputCommandFunc, removal ipv4RouteRemoval) bool {
	destination := removal.Destination
	if destination == "" {
		return false
	}
	out, err := run(ctx, "ip", "route", "show", destination)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !routeLineMatchesDestination(fields[0], destination) {
			continue
		}
		if !ipv4RouteLineMatches(line, removal.Type, destination, removal.Device, removal.Gateway, removal.PreferredSource, removal.metricForMatch()) {
			continue
		}
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == "proto" && fields[i+1] == "bgp" {
				return true
			}
		}
	}
	return false
}

func (r ipv4RouteRemoval) metricForMatch() int {
	metric := 0
	if r.Metric != "" {
		_, _ = fmt.Sscanf(r.Metric, "%d", &metric)
	}
	return metric
}

func routeLineMatchesDestination(got, want string) bool {
	got = strings.TrimSpace(got)
	want = strings.TrimSpace(want)
	if got == want {
		return true
	}
	if strings.HasSuffix(want, "/32") && strings.TrimSuffix(want, "/32") == got {
		return true
	}
	return false
}

func (c IPv4RouteController) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if c.Command != nil {
		return c.Command(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func ipv4RouteRemovalDeleteArgs(removal ipv4RouteRemoval) []string {
	destination := removal.Destination
	if destination == "" {
		return nil
	}
	args := []string{"route", "del"}
	if removal.Type == "blackhole" {
		args = append(args, "blackhole", destination)
	} else {
		args = append(args, destination)
		if removal.Gateway != "" {
			args = append(args, "via", removal.Gateway)
		}
		if removal.Device != "" {
			args = append(args, "dev", removal.Device)
		}
	}
	if removal.Metric != "" && removal.Metric != "0" {
		args = append(args, "metric", removal.Metric)
	}
	return args
}

func missingIPv4RouteDelete(err error, output []byte) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(string(output))
	return strings.Contains(message, "no such process") || strings.Contains(message, "cannot find") || strings.Contains(message, "not in table")
}

func freeBSDIPv4RouteApplyCommand(routeType, destination, device, gateway, preferredSource string) (string, []string) {
	destArgs := freeBSDRouteDestinationArgs(destination)
	if routeType == "blackhole" {
		return "route", append([]string{"-n", "add"}, append(destArgs, "-blackhole")...)
	}
	args := append([]string{"-n", "change"}, destArgs...)
	if gateway != "" {
		args = append(args, gateway)
	} else {
		args = append(args, "-interface", device)
	}
	if preferredSource != "" {
		args = append(args, "-ifa", preferredSource)
	}
	return "route", args
}

func freeBSDRouteNeedsAdd(output []byte) bool {
	message := strings.ToLower(string(output))
	return strings.Contains(message, "not in table") || strings.Contains(message, "route has not been found")
}

func freeBSDIPv4RouteAddArgs(changeArgs []string) []string {
	out := append([]string(nil), changeArgs...)
	for i, arg := range out {
		if arg == "change" {
			out[i] = "add"
			return out
		}
	}
	return out
}

func freeBSDIPv4RouteRemovalDeleteCommand(removal ipv4RouteRemoval) (string, []string) {
	return "route", append([]string{"-n", "delete"}, freeBSDRouteDestinationArgs(removal.Destination)...)
}

func freeBSDRouteDestination(destination string) string {
	switch destination {
	case "", "<nil>", "0.0.0.0/0", "default":
		return "default"
	default:
		return destination
	}
}

func freeBSDRouteDestinationArgs(destination string) []string {
	dest := freeBSDRouteDestination(destination)
	if dest == "default" {
		return []string{"default"}
	}
	if prefix, err := netip.ParsePrefix(dest); err == nil {
		if prefix.Bits() == 32 && prefix.Addr().Is4() {
			return []string{"-host", prefix.Addr().String()}
		}
		return []string{"-net", prefix.String()}
	}
	if addr, err := netip.ParseAddr(dest); err == nil && addr.Is4() {
		return []string{"-host", addr.String()}
	}
	return []string{dest}
}

func routeInstallStatusChanged(previous, next map[string]any) bool {
	if previous["phase"] != next["phase"] {
		return true
	}
	for _, key := range []string{"type", "destination", "device", "gateway", "preferredSource", "metric", "dryRun"} {
		if fmt.Sprint(previous[key]) != fmt.Sprint(next[key]) {
			return true
		}
	}
	return false
}

type DHCPv6ServerController struct {
	Advertisements  *RouterAdvertisementService
	Router          *api.Router
	Bus             *bus.Bus
	Store           Store
	DryRun          bool
	Command         string
	ConfigPath      string
	PIDFile         string
	Port            int
	ListenAddresses []string
	Logger          *slog.Logger
	Now             func() time.Time
}

func (c DHCPv6ServerController) Start(ctx context.Context) {
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.lan.address.*", "routerd.dhcpv6.info.*", "routerd.resource.status.changed"}}, 32)
	go func() {
		if err := c.reconcile(ctx); err != nil && c.Logger != nil && ctx.Err() == nil {
			c.Logger.Warn("ipv6 dhcpv6 server initial reconcile failed", "error", err)
		}
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					return
				}
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
			if err := c.reconcile(ctx); err != nil && c.Logger != nil && ctx.Err() == nil {
				c.Logger.Warn("ipv6 dhcpv6 server reconcile failed", "error", err)
			}
		}
	}()
}

func (c DHCPv6ServerController) reconcile(ctx context.Context) error {
	effectiveRouter, err := c.effectiveRouter()
	if err != nil {
		return err
	}
	if !routerNeedsDnsmasq(c.Router) && !routerNeedsDnsmasq(effectiveRouter) {
		if !c.DryRun {
			if err := stopOwnedDnsmasq(ctx, c.Command, firstNonEmpty(c.ConfigPath, "/run/routerd/dnsmasq-phase1.conf"), firstNonEmpty(c.PIDFile, "/run/routerd/dnsmasq-phase1.pid")); err != nil {
				return err
			}
		}
		if err := c.reconcileAdvertisements(ctx, effectiveRouter); err != nil {
			return err
		}
		return c.saveSuppressedRAOnlyServers(effectiveRouter)
	}
	configPath := firstNonEmpty(c.ConfigPath, "/run/routerd/dnsmasq-phase1.conf")
	if err := validateDnsmasqArtifactOwnership(configPath); err != nil {
		if statusErr := c.saveDnsmasqOwnershipError(err); statusErr != nil {
			return statusErr
		}
		return err
	}
	pidFile := firstNonEmpty(c.PIDFile, "/run/routerd/dnsmasq-phase1.pid")
	port := c.Port
	if port == 0 {
		port = 1053
	}
	hostsSnapshot, err := snapshotDnsmasqHostsFile(dnsmasqHostsFile(configPath))
	if err != nil {
		return err
	}
	changed, reloadOnly, err := writeDnsmasqConfig(effectiveRouter, c.Store, configPath, pidFile, port, c.ListenAddresses)
	if err != nil {
		return err
	}
	if c.DryRun {
		command := firstNonEmpty(c.Command, "dnsmasq")
		if err := testDnsmasqConfig(ctx, command, configPath); err != nil {
			return err
		}
	} else {
		leaseChanged, err := pruneDHCPv4ProfileMismatchedLeases(effectiveRouter, configPath, pidFile)
		if err != nil {
			return err
		}
		changed = changed || leaseChanged
		if err := ensureDnsmasq(ctx, c.Command, configPath, pidFile, changed); err != nil {
			return restoreDnsmasqHostsForRetry(configPath, reloadOnly, hostsSnapshot, err)
		}
		if reloadOnly && !changed {
			if err := reloadDnsmasqHostsSidecar(ctx, pidFile, configPath, hostsSnapshot, func(ctx context.Context, pid string) error { return reloadDnsmasq(ctx, configPath, pid) }); err != nil {
				return err
			}
		}
	}
	if err := c.saveDHCPv4ServerStatuses(effectiveRouter, configPath, pidFile); err != nil {
		return err
	}
	if err := c.saveDHCPv4ReservationStatuses(effectiveRouter, configPath, pidFile); err != nil {
		return err
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DHCPv6Server" {
			continue
		}
		spec, err := resource.DHCPv6ServerSpec()
		if err != nil {
			return err
		}
		matches, err := c.resourceWhenMatches(resource)
		if err != nil {
			return err
		}
		if !matches {
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6Server", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "WhenFalse", "interface": spec.Interface, "mode": firstNonEmpty(spec.Mode, "stateless"), "configPath": configPath, "pidFile": pidFile, "dryRun": c.DryRun}); err != nil {
				return err
			}
			continue
		}
		ready, err := resourcequery.ReadDependenciesReady(c.Store, spec.DependsOn)
		if err != nil {
			return err
		}
		if !ready {
			dependencies, err := dependencyStatusSnapshot(c.Store, spec.DependsOn)
			if err != nil {
				return err
			}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6Server", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "DependsOnFalse", "dependencies": dependencies}); err != nil {
				return err
			}
			continue
		}
		pending, err := dhcpv6ServerPending(effectiveRouter, c.Store, spec)
		if err != nil {
			return err
		}
		if pending != "" {
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6Server", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": pending, "interface": spec.Interface, "mode": firstNonEmpty(spec.Mode, "stateless"), "configPath": configPath, "pidFile": pidFile, "dryRun": c.DryRun}); err != nil {
				return err
			}
			continue
		}
		dnsServerSources, _, err := expandServerSources(c.Store, spec.DNSServerFrom, "DNSServerFrom")
		if err != nil {
			return err
		}
		dnsServers := append(expandServers(spec.DNSServers), dnsServerSources...)
		phase := "Applied"
		if c.DryRun {
			phase = "Rendered"
		}
		status := map[string]any{"phase": phase, "interface": spec.Interface, "mode": firstNonEmpty(spec.Mode, "stateless"), "dnsServers": dnsServers, "configPath": configPath, "pidFile": pidFile, "dryRun": c.DryRun}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6Server", resource.Metadata.Name, status); err != nil {
			return err
		}
		if changed && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.lan.service.dhcpv6.applied", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"interface": spec.Interface, "dryRun": fmt.Sprintf("%t", c.DryRun)}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	if err := c.reconcileAdvertisements(ctx, effectiveRouter); err != nil {
		return err
	}
	return nil
}

const routerdGeneratedDNSMasqMarker = "# Generated by routerd. Do not edit by hand.\n"

// validateDnsmasqArtifactOwnership fails closed before the live dnsmasq
// chain creates directories, writes configuration/hosts, or invokes a
// service manager. A canonical markerless artifact is administrator-owned.
func validateDnsmasqArtifactOwnership(configPath string) error {
	defaults, features := platform.Current()
	servicePath := ""
	if features.HasSystemd || (defaults.OS == platform.OSFreeBSD && filepath.Clean(configPath) == filepath.Clean(defaults.DnsmasqConfigFile)) {
		servicePath = defaults.DnsmasqServiceFile
	}
	return validateDnsmasqArtifacts(configPath, dnsmasqHostsFile(configPath), servicePath, defaults.OS)
}

func validateDnsmasqArtifacts(configPath, hostsPath, servicePath string, osName platform.OS) error {
	configData, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read dnsmasq canonical config %s: %w", configPath, err)
	}
	if err == nil && !dnsmasqConfigOwned(configData) {
		return fmt.Errorf("refusing dnsmasq reconcile: foreign config %s", configPath)
	}
	var serviceData []byte
	if servicePath != "" {
		serviceData, err = os.ReadFile(servicePath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read dnsmasq service %s: %w", servicePath, err)
		}
		if err == nil && !dnsmasqServiceOwned(serviceData, osName) {
			return fmt.Errorf("refusing dnsmasq reconcile: foreign service %s", servicePath)
		}
	}
	hostsData, err := os.ReadFile(hostsPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read dnsmasq canonical hosts sidecar %s: %w", hostsPath, err)
	}
	if err == nil && !strings.HasPrefix(string(hostsData), routerdGeneratedDNSMasqMarker) {
		return fmt.Errorf("refusing dnsmasq reconcile: foreign hosts sidecar %s", hostsPath)
	}
	return nil
}

func dnsmasqConfigOwned(data []byte) bool {
	return strings.HasPrefix(string(data), routerdGeneratedDNSMasqMarker)
}

func dnsmasqServiceOwned(data []byte, osName platform.OS) bool {
	// Non-systemd dnsmasq has one native actor; an installed rc boot creator
	// is a conflicting lifecycle, even if a previous routerd exported it.
	return osName == platform.OSLinux && strings.HasPrefix(string(data), routerdGeneratedDNSMasqMarker) && strings.Contains(string(data), "Description=routerd managed dnsmasq DHCP service")
}

func (c DHCPv6ServerController) saveDnsmasqOwnershipError(reconcileErr error) error {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DHCPv4Server" && resource.Kind != "DHCPv6Server" && resource.Kind != "IPv6RouterAdvertisement" {
			continue
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, resource.Kind, resource.Metadata.Name, map[string]any{
			"phase":  "Error",
			"reason": "ForeignDnsmasqArtifact",
			"error":  reconcileErr.Error(),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (c DHCPv6ServerController) effectiveRouter() (*api.Router, error) {
	stateStore, _ := c.Store.(resourcequery.StateStore)
	return resourcequery.FilterRouterByWhen(c.Router, stateStore)
}

func (c DHCPv6ServerController) resourceWhenMatches(resource api.Resource) (bool, error) {
	stateStore, _ := c.Store.(resourcequery.StateStore)
	return resourcequery.ResourceWhenMatches(resourcequery.ResourceWhen(resource), stateStore)
}

func (c DHCPv6ServerController) saveDHCPv4ServerStatuses(renderRouter *api.Router, configPath, pidFile string) error {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DHCPv4Server" {
			continue
		}
		spec, err := resource.DHCPv4ServerSpec()
		if err != nil {
			return err
		}
		matches, err := c.resourceWhenMatches(resource)
		if err != nil {
			return err
		}
		if !matches {
			status := map[string]any{"phase": "Pending", "reason": "WhenFalse", "interface": spec.Interface, "configPath": configPath, "pidFile": pidFile, "dryRun": c.DryRun}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Server", resource.Metadata.Name, status); err != nil {
				return err
			}
			continue
		}
		pending, err := dhcpv4ServerPending(renderRouter, c.Store, spec)
		if err != nil {
			return err
		}
		if pending != "" {
			status := map[string]any{"phase": "Pending", "reason": pending, "interface": spec.Interface, "configPath": configPath, "pidFile": pidFile, "dryRun": c.DryRun}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Server", resource.Metadata.Name, status); err != nil {
				return err
			}
			continue
		}
		dnsServerSources, _, err := expandIPv4DHCPServerSources(c.Store, spec.DNSServerFrom, "DNSServerFrom")
		if err != nil {
			return err
		}
		dnsServers := append(expandIPv4DHCPServers(spec.DNSServers), dnsServerSources...)
		phase := "Applied"
		if c.DryRun {
			phase = "Rendered"
		}
		status := map[string]any{"phase": phase, "interface": spec.Interface, "dnsServers": dnsServers, "configPath": configPath, "pidFile": pidFile, "dryRun": c.DryRun}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Server", resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	return nil
}

func (c DHCPv6ServerController) saveDHCPv4ReservationStatuses(renderRouter *api.Router, configPath, pidFile string) error {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DHCPv4Reservation" {
			continue
		}
		spec, err := resource.DHCPv4ReservationSpec()
		if err != nil {
			return err
		}
		status := map[string]any{
			"server":         spec.Server,
			"macAddress":     strings.ToLower(spec.MACAddress),
			"ipAddress":      spec.IPAddress,
			"hostname":       spec.Hostname,
			"configPath":     configPath,
			"pidFile":        pidFile,
			"renderer":       "dnsmasq",
			"lifecycleClass": "renderer-input",
			"dryRun":         c.DryRun,
		}
		matches, err := c.resourceWhenMatches(resource)
		if err != nil {
			return err
		}
		if !matches {
			status["phase"] = "Pending"
			status["reason"] = "WhenFalse"
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Reservation", resource.Metadata.Name, status); err != nil {
				return err
			}
			continue
		}
		rendered, servers, err := dhcpv4ReservationRenderedByServers(renderRouter, c.Store, spec)
		if err != nil {
			return err
		}
		if !rendered {
			status["phase"] = "Pending"
			status["reason"] = "ServerPending"
			status["servers"] = servers
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Reservation", resource.Metadata.Name, status); err != nil {
				return err
			}
			continue
		}
		phase := "Applied"
		if c.DryRun {
			phase = "Rendered"
		}
		status["phase"] = phase
		status["servers"] = servers
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Reservation", resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	return nil
}

func dhcpv4ReservationRenderedByServers(router *api.Router, store Store, reservation api.DHCPv4ReservationSpec) (bool, []string, error) {
	if router == nil || reservation.Scope != "" {
		return false, nil, nil
	}
	var servers []string
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "DHCPv4Server" {
			continue
		}
		if reservation.Server != "" && reservation.Server != resource.Metadata.Name {
			continue
		}
		spec, err := resource.DHCPv4ServerSpec()
		if err != nil {
			continue
		}
		pending, err := dhcpv4ServerPending(router, store, spec)
		if err != nil {
			return false, nil, err
		}
		if pending != "" {
			continue
		}
		servers = append(servers, resource.Metadata.Name)
	}
	return len(servers) > 0, servers, nil
}

func suffixedPath(path, oldSuffix, newSuffix string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return strings.TrimSuffix(path, oldSuffix) + newSuffix
}

func dependencyStatusSnapshot(store resourcequery.StatusReader, dependencies []api.ResourceDependencySpec) ([]string, error) {
	var out []string
	for _, dependency := range dependencies {
		kind, name, ok := resourcequery.SplitResource(dependency.Resource)
		if !ok {
			out = append(out, dependency.Resource+" invalid")
			continue
		}
		status, err := store.ReadObjectStatus(resourcequery.APIVersionForKind(kind), kind, name)
		if err != nil {
			return nil, err
		}
		field := firstNonEmpty(dependency.Field, "phase")
		if dependency.Phase != "" {
			field = "phase"
		}
		out = append(out, fmt.Sprintf("%s %s=%v want=%s optional=%t", dependency.Resource, field, status[field], firstNonEmpty(dependency.Phase, dependency.Equals), dependency.Optional))
	}
	return out, nil
}

func postDaemonCommand(ctx context.Context, socketPath, command string) (daemonapi.CommandResult, error) {
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socketPath)
	}}}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/commands/"+command, nil)
	if err != nil {
		return daemonapi.CommandResult{}, err
	}
	req.Close = true
	resp, err := client.Do(req)
	if err != nil {
		return daemonapi.CommandResult{}, err
	}
	defer resp.Body.Close()
	var result daemonapi.CommandResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil && err != io.EOF {
		return result, err
	}
	if resp.StatusCode >= 400 {
		return result, fmt.Errorf("daemon command %s rejected: %s", command, result.Message)
	}
	return result, nil
}

func daemonObserved(status daemonapi.DaemonStatus, kind, name string) map[string]string {
	for _, resource := range status.Resources {
		if resource.Resource.Kind == kind && resource.Resource.Name == name {
			return resource.Observed
		}
	}
	return nil
}

func decodeStringList(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return out
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func valueFromStatusRef(store Store, ref string) string {
	ref = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(ref, "${"), "}"))
	if ref == "" || !strings.Contains(ref, ".status.") {
		return ref
	}
	parts := strings.SplitN(ref, ".status.", 2)
	left, field := parts[0], parts[1]
	segments := strings.Split(left, "/")
	if len(segments) != 2 {
		return ""
	}
	status := store.ObjectStatus(api.NetAPIVersion, segments[0], segments[1])
	value := status[field]
	switch typed := value.(type) {
	case string:
		return typed
	case []string:
		data, _ := json.Marshal(typed)
		return string(data)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
		data, _ := json.Marshal(out)
		return string(data)
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
}

func valueFromStatusRefOrLiteral(store Store, value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "${") && strings.Contains(trimmed, ".status.") {
		return valueFromStatusRef(store, trimmed)
	}
	return trimmed
}

func (c DSLiteTunnelController) localAddress(ctx context.Context, spec api.DSLiteTunnelSpec) (string, string, error) {
	switch spec.LocalAddressSource {
	case "delegatedAddress":
		return c.localDelegatedAddress(spec)
	case "static":
		local := localIPv6Address(spec.LocalAddress)
		if local == "" {
			return "", "", fmt.Errorf("invalid static local IPv6 address")
		}
		return local, "", nil
	case "", "interface":
		raw := spec.LocalAddress
		if spec.LocalAddressSuffix != "" {
			if raw == "" {
				ifname := interfaceName(c.Router, spec.Interface)
				if ifname == "" {
					return "", "", fmt.Errorf("missing Interface %q", spec.Interface)
				}
				discovered, _, err := c.interfaceGlobalIPv6(ctx, ifname)
				if err != nil {
					return "", "", err
				}
				raw = discovered
			}
			local, err := deriveIPv6AddressFromPrefix(raw, "", spec.LocalAddressSuffix)
			if err != nil {
				return "", "", err
			}
			return local, "", nil
		}
		if raw != "" {
			return localIPv6Address(raw), "", nil
		}
		ifname := interfaceName(c.Router, spec.Interface)
		if ifname == "" {
			return "", "", fmt.Errorf("missing Interface %q", spec.Interface)
		}
		return c.interfaceGlobalIPv6(ctx, ifname)
	default:
		return "", "", fmt.Errorf("unsupported localAddressSource %q", spec.LocalAddressSource)
	}
}

type ipJSONAddress struct {
	Family            string `json:"family"`
	Local             string `json:"local"`
	PrefixLen         int    `json:"prefixlen"`
	Scope             string `json:"scope"`
	Dynamic           bool   `json:"dynamic"`
	Temporary         bool   `json:"temporary"`
	Deprecated        bool   `json:"deprecated"`
	Tentative         bool   `json:"tentative"`
	PreferredLifeTime int    `json:"preferred_life_time"`
}

type ipJSONLink struct {
	AddrInfo []ipJSONAddress `json:"addr_info"`
}

func interfaceGlobalIPv6(ifname string) (string, string, error) {
	if platform.CurrentOS() == platform.OSFreeBSD {
		out, err := exec.Command("ifconfig", ifname).CombinedOutput()
		if err != nil {
			return "", "", fmt.Errorf("ifconfig %s: %w: %s", ifname, err, strings.TrimSpace(string(out)))
		}
		addr := firstUsableIfconfigGlobalIPv6(out)
		if addr == "" {
			return "", "", fmt.Errorf("no usable global IPv6 address on %s", ifname)
		}
		return addr, "", nil
	}
	out, err := exec.Command("ip", "-j", "-6", "addr", "show", "dev", ifname, "scope", "global").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("ip -j -6 addr show dev %s scope global: %w: %s", ifname, err, strings.TrimSpace(string(out)))
	}
	addr := firstUsableGlobalIPv6(out)
	if addr == "" {
		return "", "", fmt.Errorf("no usable global IPv6 address on %s", ifname)
	}
	return addr, "", nil
}

func firstUsableGlobalIPv6(data []byte) string {
	var links []ipJSONLink
	if err := json.Unmarshal(data, &links); err != nil {
		return ""
	}
	return firstUsableGlobalIPv6Links(links)
}

func firstUsableGlobalIPv6Links(links []ipJSONLink) string {
	var stableSLAAC, onLink, dynamic, fallback string
	for _, link := range links {
		for _, info := range link.AddrInfo {
			if info.Family != "inet6" || info.Local == "" || info.Scope != "global" || info.Deprecated || info.Temporary || info.Tentative || info.PreferredLifeTime == 0 {
				continue
			}
			if fallback == "" {
				fallback = info.Local
			}
			if info.Dynamic && info.PrefixLen == 64 && stableSLAAC == "" {
				stableSLAAC = info.Local
			}
			if info.PrefixLen == 64 && onLink == "" {
				onLink = info.Local
			}
			if info.Dynamic && dynamic == "" {
				dynamic = info.Local
			}
		}
	}
	if stableSLAAC != "" {
		return stableSLAAC
	}
	if onLink != "" {
		return onLink
	}
	if dynamic != "" {
		return dynamic
	}
	return fallback
}

func firstUsableIfconfigGlobalIPv6(data []byte) string {
	var stableSLAAC, onLink, autoconf, fallback string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || fields[0] != "inet6" {
			continue
		}
		raw := strings.Split(fields[1], "%")[0]
		raw = strings.TrimSuffix(raw, ",")
		addr, err := netip.ParseAddr(raw)
		if err != nil || !addr.Is6() || addr.IsLinkLocalUnicast() || addr.IsMulticast() {
			continue
		}
		text := strings.Join(fields[2:], " ")
		if strings.Contains(text, "deprecated") || strings.Contains(text, "temporary") || strings.Contains(text, "tentative") {
			continue
		}
		if fallback == "" {
			fallback = addr.String()
		}
		prefixLen := 0
		for i := 2; i+1 < len(fields); i++ {
			if fields[i] != "prefixlen" {
				continue
			}
			prefixLen, _ = strconv.Atoi(fields[i+1])
			break
		}
		isAutoconf := strings.Contains(text, "autoconf")
		if isAutoconf && prefixLen == 64 && stableSLAAC == "" {
			stableSLAAC = addr.String()
		}
		if prefixLen == 64 && onLink == "" {
			onLink = addr.String()
		}
		if isAutoconf && autoconf == "" {
			autoconf = addr.String()
		}
	}
	if stableSLAAC != "" {
		return stableSLAAC
	}
	if onLink != "" {
		return onLink
	}
	if autoconf != "" {
		return autoconf
	}
	return fallback
}

func (c DSLiteTunnelController) localDelegatedAddress(spec api.DSLiteTunnelSpec) (string, string, error) {
	source, active, err := c.delegatedEndpointSource(spec)
	if err != nil {
		return "", "", err
	}
	if !active {
		return "", "", nil
	}
	return source.Address, source.Interface, nil
}

func resolveAFTRIPv6(ctx context.Context, value string, resolverPort int) (string, error) {
	if addr, err := netip.ParseAddr(value); err == nil {
		if addr.Is6() {
			return addr.String(), nil
		}
		return "", fmt.Errorf("%s is not an IPv6 address", value)
	}
	if resolverPort == 0 {
		resolverPort = 1053
	}
	resolver := net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", strconv.Itoa(resolverPort)))
		},
	}
	addrs, err := resolver.LookupIPAddr(ctx, strings.TrimSuffix(value, "."))
	if err != nil {
		return "", err
	}
	for _, addr := range addrs {
		parsed, ok := netip.AddrFromSlice(addr.IP)
		if ok && parsed.Is6() {
			return parsed.String(), nil
		}
	}
	return "", fmt.Errorf("no IPv6 address for %s", value)
}

func localIPv6Address(value string) string {
	if value == "" {
		return ""
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Addr().String()
	}
	if addr, err := netip.ParseAddr(value); err == nil && addr.Is6() {
		return addr.String()
	}
	return ""
}

func prefixFromAddress(value string) string {
	if value == "" {
		return ""
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		if prefix.Bits() == 128 {
			return netip.PrefixFrom(prefix.Addr(), 64).Masked().String()
		}
		return prefix.Masked().String()
	}
	if addr, err := netip.ParseAddr(value); err == nil && addr.Is6() {
		return netip.PrefixFrom(addr, 64).Masked().String()
	}
	return ""
}

func readyWhenAll(store Store, predicates []api.ReadyWhenSpec) bool {
	for _, predicate := range predicates {
		if !readyWhen(store, predicate) {
			return false
		}
	}
	return true
}

func readyWhen(store Store, predicate api.ReadyWhenSpec) bool {
	if len(predicate.AnyOf) > 0 {
		for _, group := range predicate.AnyOf {
			ok := len(group) > 0
			for _, item := range group {
				if !readyWhenPredicate(store, item) {
					ok = false
					break
				}
			}
			if ok {
				return true
			}
		}
		return false
	}
	return readyWhenPredicate(store, api.ReadyWhenPredicateSpec{Field: predicate.Field, Equals: predicate.Equals, NotEmpty: predicate.NotEmpty})
}

func readyWhenPredicate(store Store, predicate api.ReadyWhenPredicateSpec) bool {
	value := strings.TrimSpace(valueFromStatusRef(store, predicate.Field))
	if predicate.NotEmpty && value == "" {
		return false
	}
	if predicate.Equals != "" && value != predicate.Equals {
		return false
	}
	if predicate.Field != "" && !predicate.NotEmpty && predicate.Equals == "" {
		return value != ""
	}
	return true
}

func deriveIPv6AddressFromPrefix(value, subnetID, suffix string) (string, error) {
	prefix, err := netip.ParsePrefix(value)
	if err != nil || !prefix.Addr().Is6() {
		return "", fmt.Errorf("invalid delegated IPv6 prefix %q", value)
	}
	if prefix.Bits() > 64 {
		return "", fmt.Errorf("delegated IPv6 prefix %q is longer than /64", value)
	}
	subnet, err := parseIPv6SubnetID(subnetID)
	if err != nil {
		return "", err
	}
	subnetBits := 64 - prefix.Bits()
	if subnetBits < 64 && subnet >= (uint64(1)<<subnetBits) {
		return "", fmt.Errorf("subnetID %q does not fit in delegated prefix %s", subnetID, value)
	}
	suffixAddr, err := netip.ParseAddr(firstNonEmpty(suffix, "::1"))
	if err != nil || !suffixAddr.Is6() {
		return "", fmt.Errorf("invalid IPv6 suffix %q", suffix)
	}
	addrBytes := prefix.Masked().Addr().As16()
	first64 := binary.BigEndian.Uint64(addrBytes[:8])
	first64 |= subnet
	binary.BigEndian.PutUint64(addrBytes[:8], first64)
	suffixBytes := suffixAddr.As16()
	for i := range addrBytes {
		addrBytes[i] |= suffixBytes[i]
	}
	return netip.AddrFrom16(addrBytes).String(), nil
}

func parseIPv6SubnetID(value string) (uint64, error) {
	if value == "" {
		return 0, nil
	}
	if parsed, err := strconv.ParseUint(value, 0, 64); err == nil {
		return parsed, nil
	}
	parsed, err := strconv.ParseUint(value, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid IPv6 subnetID %q", value)
	}
	return parsed, nil
}

func interfaceName(router *api.Router, name string) string {
	if router == nil || name == "" {
		return ""
	}
	for _, resource := range router.Spec.Resources {
		if resource.Metadata.Name != name {
			continue
		}
		switch resource.Kind {
		case "Interface":
			spec, err := resource.InterfaceSpec()
			if err == nil && spec.IfName != "" {
				return spec.IfName
			}
		case "Bridge":
			spec, err := resource.BridgeSpec()
			if err == nil && spec.IfName != "" {
				return spec.IfName
			}
		case "VXLANSegment":
			spec, err := resource.VXLANSegmentSpec()
			if err == nil && spec.IfName != "" {
				return spec.IfName
			}
		}
	}
	return name
}

func waitForIPv6AddressReady(ctx context.Context, ifname, address string, timeout time.Duration, ready func(context.Context, string, string) bool) bool {
	if ready(ctx, ifname, address) {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return false
		case <-ticker.C:
			if ready(ctx, ifname, address) {
				return true
			}
		}
	}
}

func canonicalIPAddress(value string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return addr.String()
}

func ifconfigHasIPv6Address(out []byte, address string) bool {
	want := localIPv6Address(address)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || fields[0] != "inet6" {
			continue
		}
		raw := strings.Split(fields[1], "%")[0]
		if localIPv6Address(raw) == want {
			return true
		}
	}
	return false
}

const (
	dsliteDefaultInnerLocalIPv4 = "192.0.0.2"
	dsliteInnerRemoteIPv4       = "192.0.0.1"
)

// dsliteInnerLocalIPv4 returns the inner IPv4 endpoint for the tunnel. The
// middle return value is a non-empty pending reason when a required (non-optional)
// localAddressFrom reference has not published a value yet — a normal bootstrap
// condition that the caller surfaces as Pending and re-reconciles, distinct from
// a genuinely invalid configured address, which is returned as an error.
func dsliteInnerLocalIPv4(router *api.Router, store Store, spec api.DSLiteTunnelSpec) (string, string, error) {
	value := ""
	if strings.TrimSpace(spec.LocalAddressFrom.Resource) != "" {
		values, err := resourcequery.ReadValuesFromStoreOrRouter(store, router, spec.LocalAddressFrom)
		if err != nil {
			return "", "", err
		}
		if len(values) > 0 {
			value = statusvalue.Address(values[0])
		}
		if value == "" {
			value = statusvalue.Address(addressFromRouterResource(router, spec.LocalAddressFrom))
		}
		if value == "" {
			if spec.LocalAddressFrom.Optional {
				value = dsliteDefaultInnerLocalIPv4
			} else {
				return "", "InnerLocalIPv4Unresolved: " + spec.LocalAddressFrom.Resource, nil
			}
		}
	}
	if value == "" {
		value = dsliteDefaultInnerLocalIPv4
	}
	addr, err := netip.ParseAddr(value)
	if err != nil || !addr.Is4() {
		return "", "", fmt.Errorf("innerLocalAddress %q is not an IPv4 address", value)
	}
	if addr.IsUnspecified() || addr.IsMulticast() || addr.IsLoopback() {
		return "", "", fmt.Errorf("innerLocalAddress %q must be a usable unicast IPv4 address", value)
	}
	return addr.String(), "", nil
}

func addressFromRouterResource(router *api.Router, source api.StatusValueSourceSpec) string {
	if router == nil || strings.TrimSpace(source.Resource) == "" {
		return ""
	}
	kind, name, ok := strings.Cut(strings.TrimSpace(source.Resource), "/")
	if !ok || kind == "" || name == "" {
		return ""
	}
	field := firstNonEmpty(source.Field, "address")
	for _, res := range router.Spec.Resources {
		if res.Kind != kind || res.Metadata.Name != name {
			continue
		}
		switch kind {
		case "IPv4StaticAddress":
			if field != "address" {
				return ""
			}
			spec, err := res.IPv4StaticAddressSpec()
			if err != nil {
				return ""
			}
			return spec.Address
		default:
			return ""
		}
	}
	return ""
}
