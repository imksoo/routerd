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
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/platform"
)

const (
	EventApplied = "routerd.dhcpv4.client.applied"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type outputCommandFunc func(context.Context, string, ...string) ([]byte, error)

type Controller struct {
	Router         *api.Router
	Bus            *bus.Bus
	Store          Store
	DaemonSockets  map[string]string
	DryRun         bool
	Command        outputCommandFunc
	Logger         *slog.Logger
	ResolvConfPath string
}

func (c Controller) Start(ctx context.Context) {
	if c.Router == nil || c.Bus == nil || c.Store == nil {
		return
	}
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.dhcpv4.client.*"}}, 32)
	go func() {
		for event := range ch {
			if event.Resource == nil || event.Resource.Kind != "DHCPv4Client" {
				continue
			}
			if err := c.reconcile(ctx, event.Resource.Name); err != nil && c.Logger != nil {
				c.Logger.Warn("DHCPv4 lease reconcile failed", "resource", event.Resource.Name, "error", err)
			}
		}
	}()
}

func (c Controller) Reconcile(ctx context.Context, name string) error {
	return c.reconcile(ctx, name)
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
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (c Controller) reconcile(ctx context.Context, name string) error {
	socket := c.socketFor(name)
	status, err := daemonStatus(ctx, socket)
	if err != nil {
		if c.DryRun {
			return c.savePendingDaemonStatus(name, socket, err)
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
		if mtu, err := strconv.Atoi(observed["mtu"]); err == nil && mtu > 0 {
			next["mtu"] = mtu
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
			"currentAddress": observed["currentAddress"],
			"defaultGateway": observed["defaultGateway"],
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
	for _, key := range []string{"phase", "currentAddress", "prefixLength", "defaultGateway", "mtu"} {
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
	if phase == daemonapi.ResourcePhaseExpired || phase == daemonapi.ResourcePhaseReleased {
		if err := c.removeLeaseRoute(ctx, name, current, next); err != nil {
			return err
		}
		if err := c.removeExpiredAddress(ctx, name, current, next); err != nil {
			return err
		}
		return c.restoreStaticDNS(ctx, current, next)
	}
	if phase != daemonapi.ResourcePhaseBound {
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
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
	}
	previousAddress := mapString(current, "appliedAddress")
	if previousAddress != "" && previousAddress != wantedAddress {
		present, err := dhcpv4LeaseAddressPresent(ctx, platform.CurrentOS(), command, ifname, previousAddress)
		if err != nil {
			return err
		}
		if present {
			if err := removeIPv4Address(ctx, platform.CurrentOS(), command, ifname, previousAddress); err != nil {
				return fmt.Errorf("remove previous DHCPv4 address %s: %w", previousAddress, err)
			}
		}
	}
	addressPresent, err := dhcpv4LeaseAddressPresent(ctx, platform.CurrentOS(), command, ifname, wantedAddress)
	if err != nil {
		return err
	}
	next["addressPresent"] = addressPresent
	if previousAddress != wantedAddress || !addressPresent {
		if err := replaceIPv4Address(ctx, platform.CurrentOS(), command, ifname, wantedAddress); err != nil {
			return err
		}
		next["addressPresent"] = true
	}
	next["appliedAddress"] = wantedAddress
	next["ifname"] = ifname
	previousGateway := mapString(current, "appliedDefaultGateway")
	previousMetric := mapString(current, "appliedRouteMetric")
	if previousGateway != "" && (!api.BoolDefault(spec.UseRoutes, true) || previousGateway != mapString(next, "defaultGateway") || mapString(current, "ifname") != ifname || previousMetric != strconv.Itoa(spec.RouteMetric)) {
		if err := c.removeLeaseRoute(ctx, name, current, next); err != nil {
			return err
		}
	}
	if api.BoolDefault(spec.UseRoutes, true) {
		gateway := strings.TrimSpace(fmt.Sprint(next["defaultGateway"]))
		if gateway != "" {
			routePresent, err := dhcpv4DefaultRoutePresent(ctx, platform.CurrentOS(), command, ifname, gateway, spec.RouteMetric)
			if err != nil {
				return err
			}
			next["defaultRoutePresent"] = routePresent
			if !routePresent {
				if err := replaceDefaultRoute(ctx, platform.CurrentOS(), command, ifname, gateway, spec.RouteMetric); err != nil {
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
			static, err := c.interfaceDNS(ifname)
			if err != nil {
				return err
			}
			var domains []string
			if static != nil {
				combined := append([]string(nil), static.Servers...)
				for _, server := range servers {
					if !slices.Contains(combined, server) {
						combined = append(combined, server)
					}
				}
				servers = combined
				domains = append(domains, static.Domains...)
			}
			if !slices.Contains(domains, "~.") {
				domains = append(domains, "~.")
			}
			next["appliedDNSServers"] = strings.Join(servers, ",")
			if err := applyDNS(ctx, platform.CurrentOS(), command, ifname, defaultString(c.ResolvConfPath, "/etc/resolv.conf"), name, servers, domains, c.DryRun); err != nil {
				return err
			}
		} else if err := c.restoreStaticDNS(ctx, current, next); err != nil {
			return err
		}
	} else if err := c.restoreStaticDNS(ctx, current, next); err != nil {
		return err
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
	spec, _, ok, err := c.leaseSpecAndIfName(name)
	if err != nil || !ok {
		return err
	}
	metric := spec.RouteMetric
	if text := mapString(current, "appliedRouteMetric"); text != "" {
		metric, err = strconv.Atoi(text)
		if err != nil {
			return fmt.Errorf("invalid applied DHCPv4 route metric: %w", err)
		}
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
			if !api.BoolDefault(other.UseRoutes, true) || other.RouteMetric != metric || (other.Interface != spec.Interface && other.Interface != ifname) {
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
			if (route.Destination == "0.0.0.0/0" || route.Destination == "default") && route.Gateway == gateway && route.Metric == metric && (route.Device == spec.Interface || route.Device == ifname) {
				delete(next, "appliedDefaultGateway")
				delete(next, "appliedRouteMetric")
				return nil
			}
		case "IPv4StaticRoute":
			route, err := resource.IPv4StaticRouteSpec()
			if err != nil {
				return err
			}
			if (route.Destination == "0.0.0.0/0" || route.Destination == "default") && route.Via == gateway && route.Metric == metric && (route.Interface == spec.Interface || route.Interface == ifname) {
				delete(next, "appliedDefaultGateway")
				delete(next, "appliedRouteMetric")
				return nil
			}
		}
	}
	if c.DryRun {
		return nil
	}
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
	}
	present, err := dhcpv4DefaultRoutePresent(ctx, platform.CurrentOS(), command, ifname, gateway, metric)
	if err != nil {
		return err
	}
	if present {
		if platform.CurrentOS() == platform.OSFreeBSD {
			if _, err := command(ctx, "route", "-n", "delete", "default", gateway); err != nil {
				return fmt.Errorf("remove expired DHCPv4 route: %w", err)
			}
		} else if _, err := command(ctx, "ip", "-4", "route", "del", "default", "via", gateway, "dev", ifname, "metric", strconv.Itoa(metric)); err != nil {
			return fmt.Errorf("remove expired DHCPv4 route: %w", err)
		}
	}
	delete(next, "appliedDefaultGateway")
	delete(next, "appliedRouteMetric")
	return nil
}

// removeExpiredAddress removes the expired dynamic address while retaining an
// identical address that is explicitly configured as static.
func (c Controller) removeExpiredAddress(ctx context.Context, name string, current, next map[string]any) error {
	address := mapString(current, "appliedAddress")
	ifname := mapString(current, "ifname")
	if address == "" || ifname == "" {
		return nil
	}
	spec, _, ok, err := c.leaseSpecAndIfName(name)
	if err != nil || !ok {
		return err
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "IPv4StaticAddress" {
			continue
		}
		static, err := resource.IPv4StaticAddressSpec()
		if err != nil {
			return err
		}
		if static.Address == address && (static.Interface == spec.Interface || static.Interface == ifname) {
			delete(next, "appliedAddress")
			return nil
		}
	}
	if c.DryRun {
		return nil
	}
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
	}
	present, err := dhcpv4LeaseAddressPresent(ctx, platform.CurrentOS(), command, ifname, address)
	if err != nil {
		return err
	}
	if present {
		if err := removeIPv4Address(ctx, platform.CurrentOS(), command, ifname, address); err != nil {
			return fmt.Errorf("remove expired DHCPv4 address %s: %w", address, err)
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

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func (c Controller) interfaceDNS(ifname string) (*api.InterfaceDNSSpec, error) {
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "Interface" {
			continue
		}
		spec, err := resource.InterfaceSpec()
		if err != nil {
			return nil, err
		}
		if spec.IfName == ifname {
			return spec.DNS, nil
		}
	}
	return nil, nil
}

func (c Controller) restoreStaticDNS(ctx context.Context, current, next map[string]any) error {
	ifname := mapString(current, "ifname")
	if ifname == "" || mapString(current, "appliedDNSServers") == "" || c.DryRun {
		return nil
	}
	static, err := c.interfaceDNS(ifname)
	if err != nil {
		return err
	}
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
	}
	path := defaultString(c.ResolvConfPath, "/etc/resolv.conf")
	if static == nil && platform.CurrentOS() == platform.OSLinux && systemdResolvedResolvConf(path) {
		if _, err := command(ctx, "resolvectl", "revert", ifname); err != nil {
			return fmt.Errorf("restore DNS for %s: %w", ifname, err)
		}
	} else {
		var servers, domains []string
		if static != nil {
			servers, domains = static.Servers, static.Domains
		}
		if err := applyDNS(ctx, platform.CurrentOS(), command, ifname, path, "static", servers, domains, false); err != nil {
			return err
		}
	}
	delete(next, "appliedDNSServers")
	return nil
}

func applyDNS(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, resolvConfPath, resource string, servers, domains []string, dryRun bool) error {
	if osName == platform.OSLinux && systemdResolvedResolvConf(resolvConfPath) {
		if dryRun {
			return nil
		}
		args := append([]string{"dns", ifname}, servers...)
		if len(servers) == 0 {
			args = append(args, "")
		}
		if _, err := command(ctx, "resolvectl", args...); err != nil {
			return fmt.Errorf("set systemd-resolved DNS for %s: %w", ifname, err)
		}
		domainArgs := append([]string{"domain", ifname}, domains...)
		if len(domains) == 0 {
			domainArgs = append(domainArgs, "")
		}
		if _, err := command(ctx, "resolvectl", domainArgs...); err != nil {
			return fmt.Errorf("set systemd-resolved default DNS domain for %s: %w", ifname, err)
		}
		return nil
	}
	return writeResolvConf(resolvConfPath, resource, servers, domains, dryRun)
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

func writeResolvConf(path, resource string, servers, domains []string, dryRun bool) error {
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
	var search []string
	for _, domain := range domains {
		if !strings.HasPrefix(domain, "~") {
			search = append(search, domain)
		}
	}
	if len(search) > 0 {
		b.WriteString("search " + strings.Join(search, " ") + "\n")
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

func replaceIPv4Address(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, address string) error {
	if osName == platform.OSFreeBSD {
		_, err := command(ctx, "ifconfig", ifname, "inet", address, "alias")
		return err
	}
	_, err := command(ctx, "ip", "-4", "addr", "replace", address, "dev", ifname)
	return err
}

func dhcpv4LeaseAddressPresent(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, address string) (bool, error) {
	if strings.TrimSpace(address) == "" {
		return false, nil
	}
	if osName == platform.OSFreeBSD {
		out, err := command(ctx, "ifconfig", ifname)
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
	out, err := command(ctx, "ip", "-4", "addr", "show", "dev", ifname)
	if err != nil {
		return false, fmt.Errorf("observe IPv4 addresses on %s: %w", ifname, err)
	}
	return strings.Contains(string(out), "inet "+address+" "), nil
}

func removeIPv4Address(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, address string) error {
	if osName == platform.OSFreeBSD {
		host := address
		if value, _, ok := strings.Cut(host, "/"); ok {
			host = value
		}
		_, err := command(ctx, "ifconfig", ifname, "inet", host, "-alias")
		return err
	}
	_, err := command(ctx, "ip", "-4", "addr", "del", address, "dev", ifname)
	return err
}

func dhcpv4DefaultRoutePresent(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, gateway string, metric int) (bool, error) {
	if osName == platform.OSFreeBSD {
		out, err := command(ctx, "route", "-n", "get", "default")
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
	out, err := command(ctx, "ip", "-4", "route", "show", "default")
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

func replaceDefaultRoute(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, gateway string, metric int) error {
	if osName == platform.OSFreeBSD {
		if _, err := command(ctx, "route", "-n", "change", "default", gateway); err == nil {
			return nil
		}
		_, err := command(ctx, "route", "-n", "add", "default", gateway)
		return err
	}
	args := []string{"-4", "route", "replace", "default", "via", gateway, "dev", ifname}
	if metric != 0 {
		args = append(args, "metric", strconv.Itoa(metric))
	}
	_, err := command(ctx, "ip", args...)
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
