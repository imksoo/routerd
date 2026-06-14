// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
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
	Router       *api.Router
	Bus          *bus.Bus
	Store        Store
	DryRun       bool
	ResolverPort int
	Logger       *slog.Logger
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
	var failures []string
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DSLiteTunnel" {
			continue
		}
		spec, err := resource.DSLiteTunnelSpec()
		if err != nil {
			return err
		}
		if !api.BoolDefault(spec.Enabled, true) {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": PhaseDisabled, "reason": "Disabled"})
			continue
		}
		if !resourcequery.DependenciesReady(c.Store, spec.DependsOn) {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "DependsOnFalse"})
			continue
		}
		aftrName, remote, err := c.resolveRemote(ctx, spec)
		if err != nil && aftrName == "" {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "AFTRMissing"})
			continue
		}
		if err != nil {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "AFTRResolveFailed", "aftrName": aftrName, "error": err.Error()})
			continue
		}
		local, localIfName, err := c.localAddress(spec)
		if err != nil || local == "" {
			reason := "LocalIPv6Missing"
			if err != nil {
				reason = err.Error()
			}
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": reason, "aftrName": aftrName, "aftrIPv6": remote})
			continue
		}
		ifname := firstNonEmpty(spec.TunnelName, spec.Interface, resource.Metadata.Name)
		actualIfName := ifname
		mtu := spec.MTU
		if mtu == 0 {
			mtu = 1460
		}
		innerLocal, innerLocalPending, err := dsliteInnerLocalIPv4(c.Router, c.Store, spec)
		if innerLocalPending != "" {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": innerLocalPending, "aftrName": aftrName, "aftrIPv6": remote, "dryRun": c.DryRun})
			continue
		}
		if err != nil {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Error", "reason": "InnerLocalIPv4Invalid", "error": err.Error(), "dryRun": c.DryRun})
			continue
		}
		status := map[string]any{"phase": "Up", "interface": ifname, "tunnelName": ifname, "device": ifname, "localIPv6": local, "innerLocalIPv4": innerLocal, "innerRemoteIPv4": dsliteInnerRemoteIPv4, "localInterface": localIfName, "aftrName": aftrName, "aftrIPv6": remote, "mtu": mtu, "dryRun": c.DryRun}
		changed := objectStatusChanged("DSLiteTunnel", c.Store.ObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name), status)
		if !c.DryRun {
			if localIfName != "" {
				if err := ensureIPv6LocalEndpoint(ctx, localIfName, local); err != nil {
					failures = append(failures, fmt.Sprintf("%s: %v", resource.Metadata.Name, err))
					_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Error", "reason": "LocalEndpointApplyFailed", "interface": ifname, "localIPv6": local, "aftrIPv6": remote, "error": err.Error(), "dryRun": c.DryRun})
					continue
				}
			}
			resolvedIfName, err := ensureDSLiteTunnel(ctx, c.Router, spec, ifname, remote, local, innerLocal)
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", resource.Metadata.Name, err))
				_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Error", "reason": "TunnelApplyFailed", "interface": ifname, "localIPv6": local, "aftrIPv6": remote, "error": err.Error(), "dryRun": c.DryRun})
				continue
			}
			actualIfName = firstNonEmpty(resolvedIfName, ifname)
			if actualIfName != ifname {
				status["interface"] = actualIfName
				status["device"] = actualIfName
				status["aliasOf"] = actualIfName
				status["desiredTunnelName"] = ifname
			}
			if err := setDSLiteTunnelLinkUp(ctx, actualIfName, mtu); err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", resource.Metadata.Name, err))
				_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, map[string]any{"phase": "Error", "reason": "LinkApplyFailed", "interface": ifname, "localIPv6": local, "aftrIPv6": remote, "error": err.Error(), "dryRun": c.DryRun})
				continue
			}
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", resource.Metadata.Name, status); err != nil {
			return err
		}
		if changed && c.Bus != nil {
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

func (c DSLiteTunnelController) resolveRemote(ctx context.Context, spec api.DSLiteTunnelSpec) (string, string, error) {
	var firstName string
	candidates := []string{
		resourcequery.Value(c.Store, spec.AFTRFrom),
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
	Router        *api.Router
	Bus           *bus.Bus
	Store         Store
	DryRun        bool
	Logger        *slog.Logger
	Command       func(ctx context.Context, name string, args ...string) ([]byte, error)
	DevicePresent func(context.Context, string) bool
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
		if !resourcequery.DependenciesReady(c.Store, spec.DependsOn) {
			phase := dependencyUnavailablePhase(c.Router, c.Store, spec.DependsOn, standbyHealthcheckRoute(resource.Metadata.Name, spec))
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", resource.Metadata.Name, map[string]any{"phase": phase, "reason": "DependsOnFalse", "dependencies": dependencyStatusSnapshot(c.Store, spec.DependsOn)})
			continue
		}
		routeType := firstNonEmpty(strings.TrimSpace(spec.Type), "unicast")
		destination := firstNonEmpty(spec.Destination, "0.0.0.0/0")
		device := ""
		gateway := firstNonEmpty(resourcequery.Value(c.Store, spec.GatewayFrom), strings.TrimSpace(spec.Gateway))
		preferredSource := strings.TrimSpace(spec.PreferredSource)
		if routeType != "blackhole" {
			device = firstNonEmpty(resourcequery.Value(c.Store, spec.DeviceFrom), strings.TrimSpace(spec.Device))
			if device == "" {
				_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "DeviceMissing"})
				continue
			}
			devicePresent := c.DevicePresent
			if devicePresent == nil {
				devicePresent = interfaceDevicePresent
			}
			if !c.DryRun && !devicePresent(ctx, device) {
				_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "DeviceNotReady", "destination": destination, "device": device, "gateway": gateway, "preferredSource": preferredSource, "metric": spec.Metric, "dryRun": c.DryRun})
				continue
			}
		}
		status := map[string]any{"phase": "Installed", "type": routeType, "destination": destination, "device": device, "gateway": gateway, "preferredSource": preferredSource, "metric": spec.Metric, "dryRun": c.DryRun}
		previous := c.Store.ObjectStatus(api.NetAPIVersion, "IPv4Route", resource.Metadata.Name)
		changed := routeInstallStatusChanged(previous, status)
		if changed {
			status["installedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		} else if installedAt, ok := previous["installedAt"]; ok {
			status["installedAt"] = installedAt
		}
		effectivePreferredSource := preferredSource
		if !c.DryRun {
			if preferredSource != "" && routeType != "blackhole" && !ipv4PreferredSourceIsLocal(ctx, c.run, preferredSource) {
				effectivePreferredSource = ""
				status["preferredSourceSkipped"] = true
				status["preferredSourceSkipReason"] = "LocalAddressMissing"
				status["effectivePreferredSource"] = ""
				if c.Logger != nil {
					c.Logger.Warn("ipv4 route preferred source is not a local OS address; installing route without src", "resource", resource.Metadata.Name, "destination", destination, "preferredSource", preferredSource)
				}
			} else if preferredSource != "" {
				status["effectivePreferredSource"] = preferredSource
			}
			if platform.CurrentOS() != platform.OSFreeBSD && ipv4RouteInstalled(ctx, c.run, routeType, destination, device, gateway, effectivePreferredSource, spec.Metric) {
				status["kernelRouteAlreadyCurrent"] = true
				status["changed"] = changed
				if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", resource.Metadata.Name, status); err != nil {
					return err
				}
				continue
			}
			args := []string{"route", "replace", destination, "dev", device}
			if routeType == "blackhole" {
				args = []string{"route", "replace", "blackhole", destination}
			} else if gateway != "" {
				args = []string{"route", "replace", destination, "via", gateway, "dev", device}
			}
			if effectivePreferredSource != "" && routeType != "blackhole" {
				args = append(args, "src", effectivePreferredSource)
			}
			if spec.Metric > 0 {
				args = append(args, "metric", fmt.Sprintf("%d", spec.Metric))
			}
			name := "ip"
			if platform.CurrentOS() == platform.OSFreeBSD {
				name, args = freeBSDIPv4RouteApplyCommand(routeType, destination, device, gateway, effectivePreferredSource)
			}
			out, err := c.run(ctx, name, args...)
			if err != nil && platform.CurrentOS() == platform.OSFreeBSD && freeBSDRouteNeedsAdd(out) {
				args = freeBSDIPv4RouteAddArgs(args)
				out, err = c.run(ctx, name, args...)
			}
			if err != nil {
				message := strings.TrimSpace(string(out))
				status := map[string]any{"phase": "Error", "reason": "ApplyFailed", "destination": destination, "device": device, "gateway": gateway, "preferredSource": preferredSource, "metric": spec.Metric, "dryRun": c.DryRun, "error": err.Error(), "command": name + " " + strings.Join(args, " ")}
				if message != "" {
					status["message"] = message
				}
				_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", resource.Metadata.Name, status)
				failures = append(failures, fmt.Sprintf("%s: %s: %v", resource.Metadata.Name, status["command"], err))
				continue
			}
		}
		status["changed"] = changed
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", resource.Metadata.Name, status); err != nil {
			return err
		}
		if changed && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.ipv4.route.installed", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv4Route", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"type": routeType, "destination": destination, "device": device, "gateway": gateway, "preferredSource": preferredSource, "dryRun": fmt.Sprintf("%t", c.DryRun)}
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
		return nil
	}
	lister, ok := c.Store.(interface {
		ListObjectStatuses() ([]routerstate.ObjectStatus, error)
	})
	if !ok {
		return nil
	}
	deleter, ok := c.Store.(interface {
		DeleteObject(apiVersion, kind, name string) error
	})
	if !ok {
		return nil
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
		if err := c.teardownRemovedRoute(ctx, status, deleter); err != nil {
			return err
		}
	}
	return nil
}

func (c IPv4RouteController) teardownRemovedRoute(ctx context.Context, status routerstate.ObjectStatus, deleter routerstate.ObjectDeleteStore) error {
	if !c.DryRun {
		args := ipv4RouteDeleteArgs(status.Status)
		if len(args) > 0 {
			name := "ip"
			if platform.CurrentOS() == platform.OSFreeBSD {
				name, args = freeBSDIPv4RouteDeleteCommand(status.Status)
			} else if removedIPv4RouteIsCurrentlyBGP(ctx, c.run, status.Status) {
				args = nil
			}
			if len(args) > 0 {
				out, err := c.run(ctx, name, args...)
				if err != nil && !missingIPv4RouteDelete(err, out) {
					return fmt.Errorf("delete removed IPv4Route %s: %s %s: %w: %s", status.Name, name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
				}
			}
		}
	}
	if err := deleter.DeleteObject(api.NetAPIVersion, "IPv4Route", status.Name); err != nil {
		return err
	}
	if c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.ipv4.route.removed", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv4Route", Name: status.Name}
		event.Attributes = map[string]string{"destination": fmt.Sprint(status.Status["destination"]), "device": fmt.Sprint(status.Status["device"])}
		if err := c.Bus.Publish(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func removedIPv4RouteIsCurrentlyBGP(ctx context.Context, run outputCommandFunc, status map[string]any) bool {
	destination := fmt.Sprint(status["destination"])
	if destination == "" || destination == "<nil>" {
		return false
	}
	routeType := fmt.Sprint(status["type"])
	if routeType == "" || routeType == "<nil>" {
		routeType = "unicast"
	}
	device := fmt.Sprint(status["device"])
	if device == "<nil>" {
		device = ""
	}
	gateway := fmt.Sprint(status["gateway"])
	if gateway == "<nil>" {
		gateway = ""
	}
	preferredSource := fmt.Sprint(status["effectivePreferredSource"])
	if preferredSource == "" || preferredSource == "<nil>" {
		preferredSource = fmt.Sprint(status["preferredSource"])
	}
	if preferredSource == "<nil>" {
		preferredSource = ""
	}
	metric := 0
	if raw := fmt.Sprint(status["metric"]); raw != "" && raw != "<nil>" {
		_, _ = fmt.Sscanf(raw, "%d", &metric)
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
		if !ipv4RouteLineMatches(line, routeType, destination, device, gateway, preferredSource, metric) {
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

func ipv4RouteDeleteArgs(status map[string]any) []string {
	destination := fmt.Sprint(status["destination"])
	if destination == "" || destination == "<nil>" {
		return nil
	}
	routeType := fmt.Sprint(status["type"])
	if routeType == "" || routeType == "<nil>" {
		routeType = "unicast"
	}
	args := []string{"route", "del"}
	if routeType == "blackhole" {
		args = append(args, "blackhole", destination)
	} else {
		args = append(args, destination)
		gateway := fmt.Sprint(status["gateway"])
		if gateway != "" && gateway != "<nil>" {
			args = append(args, "via", gateway)
		}
		device := fmt.Sprint(status["device"])
		if device != "" && device != "<nil>" {
			args = append(args, "dev", device)
		}
	}
	metric := fmt.Sprint(status["metric"])
	if metric != "" && metric != "0" && metric != "<nil>" {
		args = append(args, "metric", metric)
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

func freeBSDIPv4RouteDeleteCommand(status map[string]any) (string, []string) {
	return "route", append([]string{"-n", "delete"}, freeBSDRouteDestinationArgs(fmt.Sprint(status["destination"]))...)
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
	effectiveRouter := c.effectiveRouter()
	if !routerNeedsDnsmasq(c.Router) && !routerNeedsDnsmasq(effectiveRouter) {
		return nil
	}
	configPath := firstNonEmpty(c.ConfigPath, "/run/routerd/dnsmasq-phase1.conf")
	pidFile := firstNonEmpty(c.PIDFile, "/run/routerd/dnsmasq-phase1.pid")
	port := c.Port
	if port == 0 {
		port = 1053
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
		if err := ensureDnsmasq(ctx, c.Command, configPath, pidFile, changed); err != nil {
			return err
		}
		if reloadOnly && !changed {
			if err := reloadDnsmasq(ctx, pidFile); err != nil {
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
		if !c.resourceWhenMatches(resource) {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6Server", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "WhenFalse", "interface": spec.Interface, "mode": firstNonEmpty(spec.Mode, "stateless"), "configPath": configPath, "pidFile": pidFile, "dryRun": c.DryRun})
			continue
		}
		if !resourcequery.DependenciesReady(c.Store, spec.DependsOn) {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6Server", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "DependsOnFalse", "dependencies": dependencyStatusSnapshot(c.Store, spec.DependsOn)})
			continue
		}
		if pending := dhcpv6ServerPending(effectiveRouter, c.Store, spec); pending != "" {
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6Server", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": pending, "interface": spec.Interface, "mode": firstNonEmpty(spec.Mode, "stateless"), "configPath": configPath, "pidFile": pidFile, "dryRun": c.DryRun}); err != nil {
				return err
			}
			continue
		}
		dnsServerSources, _ := expandServerSources(c.Store, spec.DNSServerFrom, "DNSServerFrom")
		dnsServers := append(expandServers(c.Store, spec.DNSServers), dnsServerSources...)
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
	if err := c.reconcileRouterAdvertisements(ctx, configPath, pidFile, changed); err != nil {
		return err
	}
	return nil
}

func (c DHCPv6ServerController) effectiveRouter() *api.Router {
	stateStore, ok := c.Store.(resourcequery.StateStore)
	if !ok {
		return c.Router
	}
	return resourcequery.FilterRouterByWhen(c.Router, stateStore)
}

func (c DHCPv6ServerController) resourceWhenMatches(resource api.Resource) bool {
	stateStore, ok := c.Store.(resourcequery.StateStore)
	if !ok {
		return true
	}
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
		if !c.resourceWhenMatches(resource) {
			status := map[string]any{"phase": "Pending", "reason": "WhenFalse", "interface": spec.Interface, "configPath": configPath, "pidFile": pidFile, "dryRun": c.DryRun}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Server", resource.Metadata.Name, status); err != nil {
				return err
			}
			continue
		}
		if pending := dhcpv4ServerPending(renderRouter, c.Store, spec); pending != "" {
			status := map[string]any{"phase": "Pending", "reason": pending, "interface": spec.Interface, "configPath": configPath, "pidFile": pidFile, "dryRun": c.DryRun}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Server", resource.Metadata.Name, status); err != nil {
				return err
			}
			continue
		}
		dnsServerSources, _ := expandIPv4DHCPServerSources(c.Store, spec.DNSServerFrom, "DNSServerFrom")
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
		if !c.resourceWhenMatches(resource) {
			status["phase"] = "Pending"
			status["reason"] = "WhenFalse"
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Reservation", resource.Metadata.Name, status); err != nil {
				return err
			}
			continue
		}
		rendered, servers := dhcpv4ReservationRenderedByServers(renderRouter, c.Store, spec)
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

func dhcpv4ReservationRenderedByServers(router *api.Router, store Store, reservation api.DHCPv4ReservationSpec) (bool, []string) {
	if router == nil || reservation.Scope != "" {
		return false, nil
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
		if dhcpv4ServerPending(router, store, spec) != "" {
			continue
		}
		servers = append(servers, resource.Metadata.Name)
	}
	return len(servers) > 0, servers
}

func (c DHCPv6ServerController) reconcileRouterAdvertisements(ctx context.Context, configPath, pidFile string, changed bool) error {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "IPv6RouterAdvertisement" {
			continue
		}
		spec, err := resource.IPv6RouterAdvertisementSpec()
		if err != nil {
			return err
		}
		if !c.resourceWhenMatches(resource) {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "WhenFalse", "interface": spec.Interface, "configPath": configPath, "pidFile": pidFile, "renderer": "dnsmasq", "dryRun": c.DryRun})
			continue
		}
		if !resourcequery.DependenciesReady(c.Store, spec.DependsOn) {
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", resource.Metadata.Name, map[string]any{"phase": "Pending", "reason": "DependsOnFalse", "dependencies": dependencyStatusSnapshot(c.Store, spec.DependsOn)})
			continue
		}
		rdnssFrom, _ := expandServerSources(c.Store, spec.RDNSSFrom, "RDNSSFrom")
		rdnss := append(expandServers(c.Store, spec.RDNSS), rdnssFrom...)
		prefix := firstNonEmpty(prefixFromStatusOrAddress(c.Store, resourcequery.Value(c.Store, spec.PrefixFrom)), prefixFromStatusOrAddress(c.Store, spec.Prefix), prefixFromStatusOrAddress(c.Store, spec.PrefixSource))
		phase := "Applied"
		if c.DryRun {
			phase = "Rendered"
		}
		status := map[string]any{
			"phase":      phase,
			"interface":  spec.Interface,
			"prefix":     prefix,
			"rdnss":      rdnss,
			"configPath": configPath,
			"pidFile":    pidFile,
			"renderer":   "dnsmasq",
			"dryRun":     c.DryRun,
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPv6RouterAdvertisement", resource.Metadata.Name, status); err != nil {
			return err
		}
		if changed && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.ra.applied", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"interface": spec.Interface, "prefix": prefix, "renderer": "dnsmasq", "dryRun": fmt.Sprintf("%t", c.DryRun)}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

func suffixedPath(path, oldSuffix, newSuffix string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return strings.TrimSuffix(path, oldSuffix) + newSuffix
}

func dependencyStatusSnapshot(store Store, dependencies []api.ResourceDependencySpec) []string {
	var out []string
	for _, dependency := range dependencies {
		kind, name, ok := resourcequery.SplitResource(dependency.Resource)
		if !ok {
			out = append(out, dependency.Resource+" invalid")
			continue
		}
		status := store.ObjectStatus(resourcequery.APIVersionForKind(kind), kind, name)
		field := firstNonEmpty(dependency.Field, "phase")
		if dependency.Phase != "" {
			field = "phase"
		}
		out = append(out, fmt.Sprintf("%s %s=%v want=%s optional=%t", dependency.Resource, field, status[field], firstNonEmpty(dependency.Phase, dependency.Equals), dependency.Optional))
	}
	return out
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

func (c DSLiteTunnelController) localAddress(spec api.DSLiteTunnelSpec) (string, string, error) {
	switch spec.LocalAddressSource {
	case "delegatedAddress":
		return c.localDelegatedAddress(spec)
	case "static":
		local := localIPv6Address(firstNonEmpty(valueFromStatusRef(c.Store, spec.LocalAddress), spec.LocalAddress))
		if local == "" {
			return "", "", fmt.Errorf("invalid static local IPv6 address")
		}
		return local, "", nil
	case "", "interface":
		raw := firstNonEmpty(valueFromStatusRef(c.Store, spec.LocalIPv6Source), valueFromStatusRef(c.Store, spec.LocalAddress), spec.LocalAddress)
		if spec.LocalAddressSuffix != "" {
			if raw == "" {
				ifname := interfaceName(c.Router, spec.Interface)
				if ifname == "" {
					return "", "", fmt.Errorf("missing Interface %q", spec.Interface)
				}
				discovered, _, err := interfaceGlobalIPv6(ifname)
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
		return interfaceGlobalIPv6(ifname)
	default:
		return "", "", fmt.Errorf("unsupported localAddressSource %q", spec.LocalAddressSource)
	}
}

type ipJSONAddress struct {
	Family            string `json:"family"`
	Local             string `json:"local"`
	Scope             string `json:"scope"`
	Dynamic           bool   `json:"dynamic"`
	Temporary         bool   `json:"temporary"`
	Deprecated        bool   `json:"deprecated"`
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
	var fallback string
	for _, link := range links {
		for _, info := range link.AddrInfo {
			if info.Family != "inet6" || info.Local == "" || info.Scope != "global" || info.Deprecated || info.Temporary || info.PreferredLifeTime == 0 {
				continue
			}
			if fallback == "" {
				fallback = info.Local
			}
			if info.Dynamic {
				return info.Local
			}
		}
	}
	return fallback
}

func firstUsableIfconfigGlobalIPv6(data []byte) string {
	var fallback string
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
		if strings.Contains(text, "deprecated") || strings.Contains(text, "temporary") {
			continue
		}
		if fallback == "" {
			fallback = addr.String()
		}
		if strings.Contains(text, "autoconf") {
			return addr.String()
		}
	}
	return fallback
}

func (c DSLiteTunnelController) localDelegatedAddress(spec api.DSLiteTunnelSpec) (string, string, error) {
	if spec.LocalDelegatedAddress == "" {
		return "", "", fmt.Errorf("localDelegatedAddress is required")
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "IPv6DelegatedAddress" || resource.Metadata.Name != spec.LocalDelegatedAddress {
			continue
		}
		delegated, err := resource.IPv6DelegatedAddressSpec()
		if err != nil {
			return "", "", err
		}
		prefix := prefixFromStatusOrAddress(c.Store, "${IPv6DelegatedAddress/"+spec.LocalDelegatedAddress+".status.address}")
		if prefix == "" {
			return "", "", fmt.Errorf("delegated prefix is not ready")
		}
		subnetID := delegated.SubnetID
		if parsed, err := netip.ParsePrefix(prefix); err == nil && parsed.Bits() == 64 {
			subnetID = ""
		}
		local, err := deriveIPv6AddressFromPrefix(prefix, subnetID, firstNonEmpty(spec.LocalAddressSuffix, delegated.AddressSuffix, "::1"))
		if err != nil {
			return "", "", err
		}
		return local, interfaceName(c.Router, delegated.Interface), nil
	}
	return "", "", fmt.Errorf("missing IPv6DelegatedAddress %q", spec.LocalDelegatedAddress)
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

func prefixFromStatusOrAddress(store Store, ref string) string {
	value := valueFromStatusRef(store, ref)
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

func ensureIPv6LocalEndpoint(ctx context.Context, ifname, address string) error {
	if ifname == "" || address == "" {
		return nil
	}
	if platform.CurrentOS() == platform.OSFreeBSD {
		out, err := exec.CommandContext(ctx, "ifconfig", ifname).CombinedOutput()
		if err == nil && ifconfigHasIPv6Address(out, address) {
			return nil
		}
		if err := exec.CommandContext(ctx, "ifconfig", ifname, "inet6", address, "prefixlen", "128", "alias").Run(); err != nil {
			return fmt.Errorf("ifconfig %s inet6 %s prefixlen 128 alias: %w", ifname, address, err)
		}
		return nil
	}
	return exec.CommandContext(ctx, "ip", "-6", "addr", "replace", address+"/128", "dev", ifname).Run()
}

func ensureDSLiteTunnel(ctx context.Context, router *api.Router, spec api.DSLiteTunnelSpec, ifname, remote, local, innerLocal string) (string, error) {
	if platform.CurrentOS() == platform.OSFreeBSD {
		return ensureFreeBSDDSLiteTunnel(ctx, spec, ifname, remote, local, innerLocal)
	}
	show, showErr := exec.CommandContext(ctx, "ip", "-6", "tunnel", "show", ifname).CombinedOutput()
	if showErr == nil && strings.Contains(string(show), "remote "+remote) && strings.Contains(string(show), "local "+local) {
		if err := ensureLinuxDSLiteInnerIPv4(ctx, ifname, innerLocal); err != nil {
			return "", err
		}
		return ifname, nil
	}
	_ = exec.CommandContext(ctx, "ip", "-6", "tunnel", "del", ifname).Run()
	args := []string{"-6", "tunnel", "add", ifname, "mode", "ipip6", "remote", remote, "local", local}
	if underlay := interfaceName(router, spec.Interface); underlay != "" && underlay != ifname {
		args = append(args, "dev", underlay)
	}
	args = append(args, "encaplimit", firstNonEmpty(spec.EncapsulationLimit, "none"))
	out, err := exec.CommandContext(ctx, "ip", args...).CombinedOutput()
	if err != nil {
		if existing := existingDSLiteTunnel(ctx, remote, local); existing != "" {
			if err := ensureLinuxDSLiteInnerIPv4(ctx, existing, innerLocal); err != nil {
				return "", err
			}
			return existing, nil
		}
		return "", fmt.Errorf("ip %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	if err := ensureLinuxDSLiteInnerIPv4(ctx, ifname, innerLocal); err != nil {
		return "", err
	}
	return ifname, nil
}

func existingDSLiteTunnel(ctx context.Context, remote, local string) string {
	out, err := exec.CommandContext(ctx, "ip", "-6", "tunnel", "show").CombinedOutput()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "remote "+remote) || !strings.Contains(line, "local "+local) {
			continue
		}
		name, _, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(name) != "" && strings.TrimSpace(name) != "ip6tnl0" {
			return strings.TrimSpace(name)
		}
	}
	return ""
}

func setDSLiteTunnelLinkUp(ctx context.Context, ifname string, mtu int) error {
	if platform.CurrentOS() == platform.OSFreeBSD {
		args := []string{ifname}
		if mtu > 0 {
			args = append(args, "mtu", strconv.Itoa(mtu))
		}
		args = append(args, "up")
		out, err := exec.CommandContext(ctx, "ifconfig", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("ifconfig %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return exec.CommandContext(ctx, "ip", "link", "set", ifname, "mtu", fmt.Sprintf("%d", mtu), "up").Run()
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

func ensureFreeBSDDSLiteTunnel(ctx context.Context, spec api.DSLiteTunnelSpec, desiredName, remote, local, innerLocal string) (string, error) {
	name := freeBSDDSLiteRuntimeIfName(desiredName)
	show, showErr := exec.CommandContext(ctx, "ifconfig", name).CombinedOutput()
	mtuText := ""
	if spec.MTU != 0 {
		mtuText = "mtu " + strconv.Itoa(spec.MTU)
	}
	innerIPv4Text := "inet " + innerLocal + " --> " + dsliteInnerRemoteIPv4
	needsRecreate := showErr != nil ||
		!strings.Contains(string(show), "tunnel inet6 "+local+" --> "+remote) ||
		!strings.Contains(string(show), innerIPv4Text) ||
		(mtuText != "" && !strings.Contains(string(show), mtuText))
	if needsRecreate {
		_ = exec.CommandContext(ctx, "ifconfig", name, "destroy").Run()
		if out, err := exec.CommandContext(ctx, "ifconfig", name, "create").CombinedOutput(); err != nil {
			return "", fmt.Errorf("ifconfig %s create: %w: %s", name, err, strings.TrimSpace(string(out)))
		}
		if out, err := exec.CommandContext(ctx, "ifconfig", name, "inet6", "tunnel", local, remote).CombinedOutput(); err != nil {
			return "", fmt.Errorf("ifconfig %s inet6 tunnel %s %s: %w: %s", name, local, remote, err, strings.TrimSpace(string(out)))
		}
		if out, err := exec.CommandContext(ctx, "ifconfig", name, "inet", innerLocal, dsliteInnerRemoteIPv4, "netmask", "255.255.255.255").CombinedOutput(); err != nil {
			return "", fmt.Errorf("ifconfig %s inet %s %s: %w: %s", name, innerLocal, dsliteInnerRemoteIPv4, err, strings.TrimSpace(string(out)))
		}
	}
	if spec.DefaultRoute {
		routeOut, routeErr := exec.CommandContext(ctx, "route", "-n", "get", "default").CombinedOutput()
		routeMissing := routeErr != nil ||
			!strings.Contains(string(routeOut), "gateway: "+dsliteInnerRemoteIPv4) ||
			!strings.Contains(string(routeOut), "interface: "+name)
		if routeMissing {
			if out, err := exec.CommandContext(ctx, "route", "-n", "change", "default", dsliteInnerRemoteIPv4).CombinedOutput(); err != nil {
				if addOut, addErr := exec.CommandContext(ctx, "route", "-n", "add", "default", dsliteInnerRemoteIPv4).CombinedOutput(); addErr != nil {
					return "", fmt.Errorf("route default via %s: change: %w: %s; add: %w: %s", dsliteInnerRemoteIPv4, err, strings.TrimSpace(string(out)), addErr, strings.TrimSpace(string(addOut)))
				}
			}
		}
	}
	return name, nil
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
		value = statusAddressValue(resourcequery.Value(store, spec.LocalAddressFrom))
		if value == "" {
			value = statusAddressValue(addressFromRouterResource(router, spec.LocalAddressFrom))
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

func ensureLinuxDSLiteInnerIPv4(ctx context.Context, ifname, innerLocal string) error {
	out, err := exec.CommandContext(ctx, "ip", "-4", "addr", "show", "dev", ifname).CombinedOutput()
	if err == nil && strings.Contains(string(out), "inet "+innerLocal+" ") && strings.Contains(string(out), "peer "+dsliteInnerRemoteIPv4+" ") {
		return nil
	}
	args := []string{"-4", "addr", "replace", innerLocal + "/32", "peer", dsliteInnerRemoteIPv4 + "/32", "dev", ifname}
	if out, err := exec.CommandContext(ctx, "ip", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("ip %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

var freeBSDDSLiteRuntimeGIFNamePattern = regexp.MustCompile(`^gif[0-9]+$`)

func freeBSDDSLiteRuntimeIfName(name string) string {
	name = strings.TrimSpace(name)
	if freeBSDDSLiteRuntimeGIFNamePattern.MatchString(name) {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	index := 100 + int(binary.BigEndian.Uint16(sum[:2])%900)
	return "gif" + strconv.Itoa(index)
}

func writeDnsmasqDHCPv6Config(path, pidFile, ifname string, dnsServers []string, port int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "port=%d\nno-resolv\nno-hosts\nlisten-address=127.0.0.1\nbind-interfaces\npid-file=%s\n", port, pidFile)
	fmt.Fprintf(&b, "interface=%s\n", ifname)
	b.WriteString("enable-ra\n")
	fmt.Fprintf(&b, "dhcp-range=::,constructor:%s,ra-stateless,64,12h\n", ifname)
	for _, server := range dnsServers {
		fmt.Fprintf(&b, "dhcp-option=option6:dns-server,[%s]\n", server)
	}
	return os.WriteFile(path, []byte(b.String()), 0644)
}
