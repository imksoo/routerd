// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	EventApplied = "routerd.dhcpv4.client.applied"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type Controller struct {
	Router        *api.Router
	Bus           *bus.Bus
	Store         Store
	DaemonSockets map[string]string
	DryRun        bool
	Logger        *slog.Logger
}

func (c Controller) ReconcileAll(ctx context.Context) error {
	if c.Router == nil {
		return nil
	}
	var errs []string
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DHCPv4Client" {
			continue
		}
		name := resource.Metadata.Name
		if err := c.reconcile(ctx, name); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
			if c.Logger != nil {
				c.Logger.Warn("DHCPv4 lease reconcile failed", "resource", name, "error", err)
			}
		}
	}
	if err := c.cleanupRemoved(ctx); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (c Controller) cleanupRemoved(ctx context.Context) error {
	if c.DryRun || c.Store == nil {
		return nil
	}
	lister, ok := c.Store.(routerstate.ObjectStatusLister)
	if !ok {
		return nil
	}
	deleter, ok := c.Store.(routerstate.ObjectDeleteStore)
	if !ok {
		return nil
	}
	desired := map[string]bool{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind == "DHCPv4Client" {
			desired[resource.Metadata.Name] = true
		}
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return err
	}
	var failures []error
	for _, status := range statuses {
		if status.APIVersion != api.NetAPIVersion || status.Kind != "DHCPv4Client" || desired[status.Name] {
			continue
		}
		err := func() error {
			next := make(map[string]any, len(status.Status))
			for key, value := range status.Status {
				next[key] = value
			}
			if err := c.removeLeaseRoute(ctx, status.Name, status.Status, next); err != nil {
				return fmt.Errorf("remove DHCPv4Client/%s route: %w", status.Name, err)
			}
			sharedAddress, sharedDNS := false, false
			for _, other := range statuses {
				if other.APIVersion != api.NetAPIVersion || other.Kind != "DHCPv4Client" || !desired[other.Name] || mapString(other.Status, "ifname") != mapString(status.Status, "ifname") {
					continue
				}
				expires, err := time.Parse(time.RFC3339Nano, mapString(other.Status, "expiresAt"))
				phase := mapString(other.Status, "phase")
				if err != nil || !time.Now().Before(expires) || (phase != daemonapi.ResourcePhaseBound && phase != daemonapi.ResourcePhaseRefreshing && phase != daemonapi.ResourcePhaseRebinding) {
					continue
				}
				sharedAddress = sharedAddress || (mapString(other.Status, "appliedAddress") != "" && mapString(other.Status, "appliedAddress") == mapString(status.Status, "appliedAddress"))
				sharedDNS = sharedDNS || mapString(other.Status, "appliedDNSServers") != ""
			}
			if !sharedAddress {
				if err := c.removeLeaseAddress(ctx, status.Status, next); err != nil {
					return fmt.Errorf("remove DHCPv4Client/%s address: %w", status.Name, err)
				}
			}
			if !sharedDNS {
				if err := c.clearDHCPDNS(ctx, status.Status, next); err != nil {
					return fmt.Errorf("remove DHCPv4Client/%s DNS: %w", status.Name, err)
				}
			}
			return deleter.DeleteObject(status.APIVersion, status.Kind, status.Name)
		}()
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (c Controller) reconcile(ctx context.Context, name string) error {
	socket := c.socketFor(name)
	status, err := daemonStatus(ctx, socket)
	if err != nil {
		if c.DryRun {
			return c.savePendingDaemonStatus(name, socket, err)
		}
		if c.Store != nil {
			current := c.Store.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", name)
			expires, expiryErr := time.Parse(time.RFC3339Nano, mapString(current, "expiresAt"))
			if expiryErr == nil && !time.Now().Before(expires) && (mapString(current, "phase") != daemonapi.ResourcePhaseExpired || mapString(current, "appliedAddress") != "" || mapString(current, "appliedDefaultGateway") != "" || mapString(current, "appliedDNSServers") != "") {
				next := make(map[string]any, len(current))
				for key, value := range current {
					next[key] = value
				}
				next["phase"] = daemonapi.ResourcePhaseExpired
				next["reason"] = "DaemonUnavailable"
				next["error"] = err.Error()
				if cleanupErr := c.applyLease(ctx, name, current, next); cleanupErr != nil {
					next["phase"] = "Error"
					next["reason"] = "ExpiryCleanupFailed"
					next["error"] = cleanupErr.Error()
					if saveErr := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", name, next); saveErr != nil {
						return errors.Join(err, cleanupErr, saveErr)
					}
					return errors.Join(err, cleanupErr)
				}
				if saveErr := c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", name, next); saveErr != nil {
					return errors.Join(err, saveErr)
				}
				if c.Bus != nil {
					event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, EventApplied, daemonapi.SeverityInfo)
					event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client", Name: name}
					if publishErr := c.Bus.Publish(ctx, event); publishErr != nil {
						return errors.Join(err, publishErr)
					}
				}
			}
		}
		return err
	}
	for _, resource := range status.Resources {
		if resource.Resource.Kind != "DHCPv4Client" || resource.Resource.Name != name {
			continue
		}
		observed := resource.Observed
		next := map[string]any{
			"phase":          resource.Phase,
			"health":         resource.Health,
			"conditions":     resource.Conditions,
			"observed":       observed,
			"interface":      observed["interface"],
			"currentAddress": observed["currentAddress"],
			"prefixLength":   observed["prefixLength"],
			"defaultGateway": observed["defaultGateway"],
			"device":         observed["interface"],
			"gateway":        observed["defaultGateway"],
			"domain":         observed["domain"],
			"renewAt":        observed["renewAt"],
			"rebindAt":       observed["rebindAt"],
			"expiresAt":      observed["expiresAt"],
			"dryRun":         c.DryRun,
		}
		if leaseTime, err := strconv.ParseInt(observed["leaseTime"], 10, 64); err == nil {
			next["leaseTime"] = leaseTime
		}
		if prefixLength, err := strconv.Atoi(observed["prefixLength"]); err == nil && prefixLength > 0 {
			next["prefixLength"] = prefixLength
		}
		if servers := parseJSONStringList(observed["dnsServers"]); len(servers) > 0 {
			next["dnsServers"] = servers
		}
		if servers := parseJSONStringList(observed["ntpServers"]); len(servers) > 0 {
			next["ntpServers"] = servers
		}
		current := c.Store.ObjectStatus(resource.Resource.APIVersion, resource.Resource.Kind, resource.Resource.Name)
		for _, key := range []string{"appliedAddress", "ifname", "appliedDefaultGateway", "appliedRouteMetric", "appliedDNSServers"} {
			if value, ok := current[key]; ok {
				next[key] = value
			}
		}
		if err := c.applyLease(ctx, name, current, next); err != nil {
			next["phase"] = "Error"
			next["reason"] = "ApplyFailed"
			next["error"] = err.Error()
			if saveErr := c.Store.SaveObjectStatus(resource.Resource.APIVersion, resource.Resource.Kind, resource.Resource.Name, next); saveErr != nil {
				return saveErr
			}
			return err
		}
		changed := leaseEventChanged(current, next)
		if err := c.Store.SaveObjectStatus(resource.Resource.APIVersion, resource.Resource.Kind, resource.Resource.Name, next); err != nil {
			return err
		}
		if !changed || c.Bus == nil {
			return nil
		}
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, EventApplied, daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client", Name: name}
		event.Attributes = map[string]string{
			"currentAddress": mapString(next, "currentAddress"),
			"defaultGateway": mapString(next, "defaultGateway"),
			"dryRun":         fmt.Sprintf("%t", c.DryRun),
		}
		return c.Bus.Publish(ctx, event)
	}
	return fmt.Errorf("daemon status did not include DHCPv4Client/%s", name)
}

func (c Controller) savePendingDaemonStatus(name, socket string, cause error) error {
	if c.Store == nil {
		return nil
	}
	status := map[string]any{
		"phase":     daemonapi.ResourcePhasePending,
		"reason":    "DaemonUnavailable",
		"socket":    socket,
		"message":   fmt.Sprintf("waiting for DHCPv4 client socket %s", socket),
		"error":     cause.Error(),
		"dryRun":    true,
		"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
	}
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", name, status)
}

func leaseEventChanged(current, next map[string]any) bool {
	for _, key := range []string{"phase", "currentAddress", "prefixLength", "defaultGateway"} {
		if leaseFieldString(current[key]) != leaseFieldString(next[key]) {
			return true
		}
	}
	return false
}

func leaseFieldString(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func (c Controller) applyLease(ctx context.Context, name string, current, next map[string]any) error {
	phase := fmt.Sprint(next["phase"])
	staleLease := false
	if phase == daemonapi.ResourcePhaseAcquiring && mapString(current, "appliedAddress") != "" {
		oldIfname := mapString(current, "ifname")
		newIfname := mapString(next, "interface")
		if oldIfname != "" && newIfname != "" && oldIfname != newIfname {
			staleLease = true
		} else if expires, err := time.Parse(time.RFC3339Nano, mapString(next, "expiresAt")); err == nil && !time.Now().Before(expires) {
			staleLease = true
		} else if expires, err := time.Parse(time.RFC3339Nano, mapString(current, "expiresAt")); err == nil && !time.Now().Before(expires) {
			staleLease = true
		}
		if !staleLease && oldIfname != "" && oldIfname == newIfname && mapString(next, "expiresAt") == "" {
			if expires, err := time.Parse(time.RFC3339Nano, mapString(current, "expiresAt")); err == nil && time.Now().Before(expires) {
				next["expiresAt"] = mapString(current, "expiresAt")
			}
		}
	}
	if phase == daemonapi.ResourcePhaseBound || phase == daemonapi.ResourcePhaseRefreshing || phase == daemonapi.ResourcePhaseRebinding {
		expires, err := time.Parse(time.RFC3339Nano, mapString(next, "expiresAt"))
		if err != nil {
			return fmt.Errorf("DHCPv4 lease %s has invalid expiration: %w", name, err)
		}
		if !time.Now().Before(expires) {
			phase = daemonapi.ResourcePhaseExpired
			next["phase"] = phase
		}
	}
	if phase == daemonapi.ResourcePhaseExpired || phase == daemonapi.ResourcePhaseReleased || staleLease {
		for _, key := range []string{"currentAddress", "prefixLength", "defaultGateway", "gateway", "dnsServers", "ntpServers", "domain", "leaseTime"} {
			delete(next, key)
		}
		if err := c.removeLeaseRoute(ctx, name, current, next); err != nil {
			return err
		}
		if err := c.removeLeaseAddress(ctx, current, next); err != nil {
			return err
		}
		if err := c.clearDHCPDNS(ctx, current, next); err != nil {
			return err
		}
		return nil
	}
	if phase != daemonapi.ResourcePhaseBound && phase != daemonapi.ResourcePhaseRefreshing && phase != daemonapi.ResourcePhaseRebinding {
		return nil
	}
	spec, ifname, ok, err := c.leaseSpecAndIfName(name)
	if err != nil || !ok {
		return err
	}
	address := strings.TrimSpace(fmt.Sprint(next["currentAddress"]))
	prefixLength, _ := strconv.Atoi(fmt.Sprint(next["prefixLength"]))
	if address == "" || prefixLength <= 0 {
		return fmt.Errorf("DHCPv4 lease %s is bound without prefixLength", name)
	}
	wantedAddress := fmt.Sprintf("%s/%d", address, prefixLength)
	if c.DryRun {
		next["appliedAddress"] = wantedAddress
		next["ifname"] = ifname
		next["applyMode"] = "dry-run"
		return nil
	}
	previousGateway := mapString(current, "appliedDefaultGateway")
	previousMetric := mapString(current, "appliedRouteMetric")
	if previousGateway != "" && (!api.BoolDefault(spec.UseRoutes, true) || previousGateway != mapString(next, "defaultGateway") || mapString(current, "ifname") != ifname || previousMetric != strconv.Itoa(spec.RouteMetric)) {
		if err := c.removeLeaseRoute(ctx, name, current, next); err != nil {
			return err
		}
	}
	oldIfname := mapString(current, "ifname")
	if oldIfname != "" && oldIfname != ifname {
		if err := c.clearDHCPDNS(ctx, current, next); err != nil {
			return err
		}
	}
	previousAddress := mapString(current, "appliedAddress")
	if previousAddress != "" && (previousAddress != wantedAddress || mapString(current, "ifname") != ifname) {
		if err := c.removeLeaseAddress(ctx, current, next); err != nil {
			return err
		}
	}
	addressPresent, err := dhcpv4LeaseAddressMatches(ctx, platform.CurrentOS(), ifname, wantedAddress, spec.RouteMetric)
	if err != nil {
		return err
	}
	next["addressPresent"] = addressPresent
	if !addressPresent {
		if err := replaceIPv4Address(ctx, platform.CurrentOS(), ifname, wantedAddress, spec.RouteMetric); err != nil {
			return err
		}
		next["addressPresent"] = true
	}
	next["appliedAddress"] = wantedAddress
	next["ifname"] = ifname
	if api.BoolDefault(spec.UseRoutes, true) {
		gateway := strings.TrimSpace(fmt.Sprint(next["defaultGateway"]))
		if gateway != "" {
			routePresent, err := dhcpv4DefaultRoutePresent(ctx, platform.CurrentOS(), ifname, gateway, spec.RouteMetric)
			if err != nil {
				return err
			}
			next["defaultRoutePresent"] = routePresent
			if !routePresent {
				if err := replaceDefaultRoute(ctx, platform.CurrentOS(), ifname, gateway, spec.RouteMetric); err != nil {
					return err
				}
				next["defaultRoutePresent"] = true
			}
			next["appliedDefaultGateway"] = gateway
			next["appliedRouteMetric"] = spec.RouteMetric
		}
	}
	if api.BoolDefault(spec.UseDNS, true) {
		servers := stringSlice(next["dnsServers"])
		if len(servers) > 0 {
			if err := applyDNS(ctx, platform.CurrentOS(), ifname, "/etc/resolv.conf", name, servers, c.DryRun); err != nil {
				return err
			}
			next["appliedDNSServers"] = strings.Join(servers, ",")
		} else if oldIfname == ifname {
			if err := c.clearDHCPDNS(ctx, current, next); err != nil {
				return err
			}
		}
	} else if oldIfname == ifname {
		if err := c.clearDHCPDNS(ctx, current, next); err != nil {
			return err
		}
	}
	next["applyMode"] = "active"
	return nil
}

// removeLeaseRoute removes an obsolete lease route unless it is still configured.
func (c Controller) removeLeaseRoute(ctx context.Context, name string, current, next map[string]any) error {
	gateway, ifname := mapString(current, "appliedDefaultGateway"), mapString(current, "ifname")
	if gateway == "" || ifname == "" {
		return nil
	}
	spec, _, _, err := c.leaseSpecAndIfName(name)
	if err != nil {
		return err
	}
	metric := spec.RouteMetric
	if text := mapString(current, "appliedRouteMetric"); text != "" {
		metric, err = strconv.Atoi(text)
		if err != nil {
			return fmt.Errorf("invalid applied DHCPv4 route metric: %w", err)
		}
	}
	aliases := map[string]string{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind == "Interface" {
			iface, err := resource.InterfaceSpec()
			if err != nil {
				return err
			}
			aliases[resource.Metadata.Name] = iface.IfName
		}
	}
	resolveInterface := func(name string) string {
		if aliases[name] != "" {
			return aliases[name]
		}
		return name
	}
	for _, resource := range c.Router.Spec.Resources {
		switch resource.Kind {
		case "DHCPv4Client":
			if resource.Metadata.Name == name {
				continue
			}
			other, err := resource.DHCPv4ClientSpec()
			if err != nil {
				return err
			}
			if !api.BoolDefault(other.UseRoutes, true) || other.RouteMetric != metric || resolveInterface(other.Interface) != ifname {
				continue
			}
			status := c.Store.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", resource.Metadata.Name)
			expires, err := time.Parse(time.RFC3339Nano, mapString(status, "expiresAt"))
			phase := mapString(status, "phase")
			if err == nil && time.Now().Before(expires) && (phase == daemonapi.ResourcePhaseBound || phase == daemonapi.ResourcePhaseRefreshing || phase == daemonapi.ResourcePhaseRebinding) && mapString(status, "defaultGateway") == gateway {
				delete(next, "appliedDefaultGateway")
				delete(next, "appliedRouteMetric")
				return nil
			}
		case "IPv4Route":
			route, err := resource.IPv4RouteSpec()
			if err != nil {
				return err
			}
			if (route.Destination == "0.0.0.0/0" || route.Destination == "default") && route.Gateway == gateway && route.Metric == metric && resolveInterface(route.Device) == ifname {
				delete(next, "appliedDefaultGateway")
				delete(next, "appliedRouteMetric")
				return nil
			}
		case "IPv4StaticRoute":
			route, err := resource.IPv4StaticRouteSpec()
			if err != nil {
				return err
			}
			if (route.Destination == "0.0.0.0/0" || route.Destination == "default") && route.Via == gateway && route.Metric == metric && resolveInterface(route.Interface) == ifname {
				delete(next, "appliedDefaultGateway")
				delete(next, "appliedRouteMetric")
				return nil
			}
		}
	}
	if c.DryRun {
		return nil
	}
	present, err := dhcpv4DefaultRoutePresent(ctx, platform.CurrentOS(), ifname, gateway, metric)
	if err != nil {
		return err
	}
	if present {
		if platform.CurrentOS() == platform.OSFreeBSD {
			if _, err := runOutputCommandContext(ctx, "route", "-n", "delete", "default", gateway); err != nil {
				return fmt.Errorf("remove expired DHCPv4 route: %w", err)
			}
		} else if _, err := runOutputCommandContext(ctx, "ip", "-4", "route", "del", "default", "via", gateway, "dev", ifname, "metric", strconv.Itoa(metric)); err != nil {
			return fmt.Errorf("remove expired DHCPv4 route: %w", err)
		}
	}
	delete(next, "appliedDefaultGateway")
	delete(next, "appliedRouteMetric")
	return nil
}

// removeLeaseAddress retains an identical address configured as static.
func (c Controller) removeLeaseAddress(ctx context.Context, current, next map[string]any) error {
	address := mapString(current, "appliedAddress")
	ifname := mapString(current, "ifname")
	if address == "" || ifname == "" {
		return nil
	}
	aliases := map[string]string{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind == "Interface" {
			spec, err := resource.InterfaceSpec()
			if err != nil {
				return err
			}
			aliases[resource.Metadata.Name] = spec.IfName
		}
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "IPv4StaticAddress" {
			continue
		}
		static, err := resource.IPv4StaticAddressSpec()
		if err != nil {
			return err
		}
		staticIfname := static.Interface
		if aliases[staticIfname] != "" {
			staticIfname = aliases[staticIfname]
		}
		if static.Address == address && staticIfname == ifname {
			delete(next, "appliedAddress")
			return nil
		}
	}
	if c.DryRun {
		return nil
	}
	present, err := dhcpv4LeaseAddressPresent(ctx, platform.CurrentOS(), ifname, address)
	if err != nil {
		return err
	}
	if present {
		if err := removeIPv4Address(ctx, platform.CurrentOS(), ifname, address); err != nil {
			return fmt.Errorf("remove DHCPv4 address %s: %w", address, err)
		}
	}
	delete(next, "appliedAddress")
	return nil
}

func mapString(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func stringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		var out []string
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return parseJSONStringList(typed)
	default:
		return nil
	}
}

func (c Controller) clearDHCPDNS(ctx context.Context, current, next map[string]any) error {
	ifname := mapString(current, "ifname")
	if ifname == "" || mapString(current, "appliedDNSServers") == "" || c.DryRun {
		return nil
	}
	if platform.CurrentOS() == platform.OSLinux && systemdResolvedResolvConf("/etc/resolv.conf") {
		if _, err := runOutputCommandContext(ctx, "resolvectl", "revert", ifname); err != nil {
			return fmt.Errorf("clear DHCP DNS for %s: %w", ifname, err)
		}
	} else if err := applyDNS(ctx, platform.CurrentOS(), ifname, "/etc/resolv.conf", "static", nil, false); err != nil {
		return err
	}
	delete(next, "appliedDNSServers")
	return nil
}

func applyDNS(ctx context.Context, osName platform.OS, ifname, resolvConfPath, resource string, servers []string, dryRun bool) error {
	if osName == platform.OSLinux && systemdResolvedResolvConf(resolvConfPath) {
		if dryRun {
			return nil
		}
		if _, err := runOutputCommandContext(ctx, "resolvectl", append([]string{"dns", ifname}, servers...)...); err != nil {
			return fmt.Errorf("set systemd-resolved DNS for %s: %w", ifname, err)
		}
		if _, err := runOutputCommandContext(ctx, "resolvectl", "domain", ifname, "~."); err != nil {
			return fmt.Errorf("set systemd-resolved default DNS domain for %s: %w", ifname, err)
		}
		return nil
	}
	return writeResolvConf(resolvConfPath, resource, servers, dryRun)
}

func systemdResolvedResolvConf(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	target, err := os.Readlink(path)
	if err != nil {
		return false
	}
	return strings.Contains(target, "systemd/resolve") || strings.Contains(target, "stub-resolv.conf")
}

func writeResolvConf(path, resource string, servers []string, dryRun bool) error {
	var b strings.Builder
	b.WriteString("# Managed by routerd. Do not edit by hand.\n")
	b.WriteString("# Source: DHCPv4Client/")
	b.WriteString(resource)
	b.WriteByte('\n')
	for _, server := range servers {
		server = strings.TrimSpace(server)
		if server == "" {
			continue
		}
		b.WriteString("nameserver ")
		b.WriteString(server)
		b.WriteByte('\n')
	}
	data := []byte(b.String())
	if dryRun {
		return nil
	}
	if current, err := os.ReadFile(path); err == nil && string(current) == string(data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (c Controller) leaseSpecAndIfName(name string) (api.DHCPv4ClientSpec, string, bool, error) {
	if c.Router == nil {
		return api.DHCPv4ClientSpec{}, "", false, nil
	}
	aliases := map[string]string{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "Interface" {
			continue
		}
		spec, err := resource.InterfaceSpec()
		if err != nil {
			return api.DHCPv4ClientSpec{}, "", false, err
		}
		aliases[resource.Metadata.Name] = spec.IfName
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DHCPv4Client" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.DHCPv4ClientSpec()
		if err != nil {
			return api.DHCPv4ClientSpec{}, "", false, err
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			ifname = spec.Interface
		}
		if ifname == "" {
			return api.DHCPv4ClientSpec{}, "", false, fmt.Errorf("DHCPv4Client/%s needs spec.interface", name)
		}
		return spec, ifname, true, nil
	}
	return api.DHCPv4ClientSpec{}, "", false, nil
}

func replaceIPv4Address(ctx context.Context, osName platform.OS, ifname, address string, metric int) error {
	if osName == platform.OSFreeBSD {
		_, err := runOutputCommandContext(ctx, "ifconfig", ifname, "inet", address, "alias")
		return err
	}
	args := []string{"-4", "addr", "replace", address, "dev", ifname}
	if metric > 0 {
		args = append(args, "metric", strconv.Itoa(metric))
	}
	_, err := runOutputCommandContext(ctx, "ip", args...)
	return err
}

func dhcpv4LeaseAddressMatches(ctx context.Context, osName platform.OS, ifname, address string, metric int) (bool, error) {
	if osName == platform.OSFreeBSD {
		return dhcpv4LeaseAddressPresent(ctx, osName, ifname, address)
	}
	out, err := runOutputCommandContext(ctx, "ip", "-j", "-4", "addr", "show", "dev", ifname)
	if err != nil {
		return false, fmt.Errorf("observe IPv4 addresses on %s: %w", ifname, err)
	}
	var links []struct {
		Addresses []struct {
			Local     string `json:"local"`
			PrefixLen int    `json:"prefixlen"`
			Metric    int    `json:"metric"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal(out, &links); err != nil {
		return false, fmt.Errorf("decode IPv4 addresses on %s: %w", ifname, err)
	}
	for _, link := range links {
		for _, item := range link.Addresses {
			if fmt.Sprintf("%s/%d", item.Local, item.PrefixLen) == address && item.Metric == metric {
				return true, nil
			}
		}
	}
	return false, nil
}

func dhcpv4LeaseAddressPresent(ctx context.Context, osName platform.OS, ifname, address string) (bool, error) {
	if strings.TrimSpace(address) == "" {
		return false, nil
	}
	if osName == platform.OSFreeBSD {
		out, err := runOutputCommandContext(ctx, "ifconfig", ifname)
		if err != nil {
			return false, fmt.Errorf("observe IPv4 addresses on %s: %w", ifname, err)
		}
		addr := strings.TrimSpace(strings.Split(address, "/")[0])
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			for i, field := range fields {
				if field == "inet" && i+1 < len(fields) && fields[i+1] == addr {
					return true, nil
				}
			}
		}
		return false, nil
	}
	out, err := runOutputCommandContext(ctx, "ip", "-4", "addr", "show", "dev", ifname)
	if err != nil {
		return false, fmt.Errorf("observe IPv4 addresses on %s: %w", ifname, err)
	}
	return strings.Contains(string(out), "inet "+address+" "), nil
}

func removeIPv4Address(ctx context.Context, osName platform.OS, ifname, address string) error {
	if osName == platform.OSFreeBSD {
		host := address
		if value, _, ok := strings.Cut(host, "/"); ok {
			host = value
		}
		_, err := runOutputCommandContext(ctx, "ifconfig", ifname, "inet", host, "-alias")
		return err
	}
	_, err := runOutputCommandContext(ctx, "ip", "-4", "addr", "del", address, "dev", ifname)
	return err
}

func dhcpv4DefaultRoutePresent(ctx context.Context, osName platform.OS, ifname, gateway string, metric int) (bool, error) {
	if osName == platform.OSFreeBSD {
		out, err := runOutputCommandContext(ctx, "route", "-n", "get", "default")
		if err != nil {
			message := strings.ToLower(string(out) + " " + err.Error())
			if strings.Contains(message, "not in table") || strings.Contains(message, "not found") {
				return false, nil
			}
			return false, fmt.Errorf("observe default route: %w", err)
		}
		values := map[string]string{}
		for _, line := range strings.Split(string(out), "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
			if ok {
				values[key] = strings.TrimSpace(value)
			}
		}
		return values["interface"] == ifname && values["gateway"] == gateway, nil
	}
	out, err := runOutputCommandContext(ctx, "ip", "-4", "route", "show", "default")
	if err != nil {
		return false, fmt.Errorf("observe default routes: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if dhcpv4DefaultRouteLineMatches(line, ifname, gateway, metric) {
			return true, nil
		}
	}
	return false, nil
}

func dhcpv4DefaultRouteLineMatches(line, ifname, gateway string, metric int) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "default" || !dhcpv4RouteFieldsContainPair(fields, "dev", ifname) || !dhcpv4RouteFieldsContainPair(fields, "via", gateway) {
		return false
	}
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "metric" {
			return fields[i+1] == strconv.Itoa(metric)
		}
	}
	return metric == 0
}

func dhcpv4RouteFieldsContainPair(fields []string, key, value string) bool {
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == key && fields[i+1] == value {
			return true
		}
	}
	return false
}

func replaceDefaultRoute(ctx context.Context, osName platform.OS, ifname, gateway string, metric int) error {
	if osName == platform.OSFreeBSD {
		if _, err := runOutputCommandContext(ctx, "route", "-n", "change", "default", gateway); err == nil {
			return nil
		}
		_, err := runOutputCommandContext(ctx, "route", "-n", "add", "default", gateway)
		return err
	}
	args := []string{"-4", "route", "replace", "default", "via", gateway, "dev", ifname}
	if metric != 0 {
		args = append(args, "metric", strconv.Itoa(metric))
	}
	_, err := runOutputCommandContext(ctx, "ip", args...)
	return err
}

func runOutputCommandContext(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (c Controller) socketFor(resource string) string {
	if socket := c.DaemonSockets[resource]; socket != "" {
		return socket
	}
	defaults, _ := platform.Current()
	return filepath.Join(defaults.RuntimeDir, "dhcpv4-client", resource+".sock")
}

func daemonStatus(ctx context.Context, socketPath string) (daemonapi.DaemonStatus, error) {
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socketPath)
	}}}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1/status", nil)
	if err != nil {
		return daemonapi.DaemonStatus{}, err
	}
	req.Close = true
	resp, err := client.Do(req)
	if err != nil {
		return daemonapi.DaemonStatus{}, err
	}
	defer resp.Body.Close()
	var status daemonapi.DaemonStatus
	return status, json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&status)
}

func parseJSONStringList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return out
	}
	return strings.Split(raw, ",")
}
