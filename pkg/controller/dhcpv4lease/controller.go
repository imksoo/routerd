// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4lease

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

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/platform"
)

const (
	EventApplied = "routerd.dhcpv4.lease.applied"
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
			if event.Resource == nil || event.Resource.Kind != "DHCPv4Lease" {
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
		if resource.Kind != "DHCPv4Lease" {
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
	status, err := daemonStatus(ctx, c.socketFor(name))
	if err != nil {
		return err
	}
	for _, resource := range status.Resources {
		if resource.Resource.Kind != "DHCPv4Lease" || resource.Resource.Name != name {
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
		current := c.Store.ObjectStatus(resource.Resource.APIVersion, resource.Resource.Kind, resource.Resource.Name)
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
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Lease", Name: name}
		event.Attributes = map[string]string{
			"currentAddress": observed["currentAddress"],
			"defaultGateway": observed["defaultGateway"],
			"dryRun":         fmt.Sprintf("%t", c.DryRun),
		}
		return c.Bus.Publish(ctx, event)
	}
	return fmt.Errorf("daemon status did not include DHCPv4Lease/%s", name)
}

func leaseEventChanged(current, next map[string]any) bool {
	for _, key := range []string{"phase", "currentAddress", "prefixLength", "defaultGateway", "domain", "leaseTime", "appliedAddress"} {
		if fmt.Sprint(current[key]) != fmt.Sprint(next[key]) {
			return true
		}
	}
	if fmt.Sprint(current["dnsServers"]) != fmt.Sprint(next["dnsServers"]) {
		return true
	}
	return false
}

func (c Controller) applyLease(ctx context.Context, name string, current, next map[string]any) error {
	if fmt.Sprint(next["phase"]) != daemonapi.ResourcePhaseBound {
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
	next["appliedAddress"] = wantedAddress
	next["ifname"] = ifname
	if c.DryRun {
		next["applyMode"] = "dry-run"
		return nil
	}
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
	}
	previousAddress := mapString(current, "appliedAddress")
	if previousAddress != "" && previousAddress != wantedAddress {
		_ = removeIPv4Address(ctx, platform.CurrentOS(), command, ifname, previousAddress)
	}
	addressPresent := dhcpv4LeaseAddressPresent(ctx, platform.CurrentOS(), command, ifname, wantedAddress)
	next["addressPresent"] = addressPresent
	if previousAddress != wantedAddress || !addressPresent {
		if err := replaceIPv4Address(ctx, platform.CurrentOS(), command, ifname, wantedAddress); err != nil {
			return err
		}
		next["addressPresent"] = true
	}
	if api.BoolDefault(spec.UseRoutes, true) {
		gateway := strings.TrimSpace(fmt.Sprint(next["defaultGateway"]))
		if gateway != "" {
			if err := replaceDefaultRoute(ctx, platform.CurrentOS(), command, ifname, gateway, spec.RouteMetric); err != nil {
				return err
			}
			next["appliedDefaultGateway"] = gateway
		}
	}
	if api.BoolDefault(spec.UseDNS, true) {
		servers := stringSlice(next["dnsServers"])
		if len(servers) > 0 {
			next["appliedDNSServers"] = strings.Join(servers, ",")
			if err := applyDNS(ctx, platform.CurrentOS(), command, ifname, defaultString(c.ResolvConfPath, "/etc/resolv.conf"), name, servers, c.DryRun); err != nil {
				return err
			}
		}
	}
	next["applyMode"] = "active"
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

func applyDNS(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, resolvConfPath, resource string, servers []string, dryRun bool) error {
	if osName == platform.OSLinux && systemdResolvedResolvConf(resolvConfPath) {
		if dryRun {
			return nil
		}
		args := append([]string{"dns", ifname}, servers...)
		if _, err := command(ctx, "resolvectl", args...); err != nil {
			return fmt.Errorf("set systemd-resolved DNS for %s: %w", ifname, err)
		}
		if _, err := command(ctx, "resolvectl", "domain", ifname, "~."); err != nil {
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
	b.WriteString("# Source: DHCPv4Lease/")
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

func (c Controller) leaseSpecAndIfName(name string) (api.DHCPv4LeaseSpec, string, bool, error) {
	if c.Router == nil {
		return api.DHCPv4LeaseSpec{}, "", false, nil
	}
	aliases := map[string]string{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "Interface" {
			continue
		}
		spec, err := resource.InterfaceSpec()
		if err != nil {
			return api.DHCPv4LeaseSpec{}, "", false, err
		}
		aliases[resource.Metadata.Name] = spec.IfName
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DHCPv4Lease" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.DHCPv4LeaseSpec()
		if err != nil {
			return api.DHCPv4LeaseSpec{}, "", false, err
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			ifname = spec.Interface
		}
		if ifname == "" {
			return api.DHCPv4LeaseSpec{}, "", false, fmt.Errorf("DHCPv4Lease/%s needs spec.interface", name)
		}
		return spec, ifname, true, nil
	}
	return api.DHCPv4LeaseSpec{}, "", false, nil
}

func replaceIPv4Address(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, address string) error {
	if osName == platform.OSFreeBSD {
		_, err := command(ctx, "ifconfig", ifname, "inet", address, "alias")
		return err
	}
	_, err := command(ctx, "ip", "-4", "addr", "replace", address, "dev", ifname)
	return err
}

func dhcpv4LeaseAddressPresent(ctx context.Context, osName platform.OS, command outputCommandFunc, ifname, address string) bool {
	if strings.TrimSpace(address) == "" {
		return false
	}
	if osName == platform.OSFreeBSD {
		out, err := command(ctx, "ifconfig", ifname)
		if err != nil {
			return false
		}
		addr := strings.TrimSpace(strings.Split(address, "/")[0])
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			for i, field := range fields {
				if field == "inet" && i+1 < len(fields) && fields[i+1] == addr {
					return true
				}
			}
		}
		return false
	}
	out, err := command(ctx, "ip", "-4", "addr", "show", "dev", ifname)
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "inet "+address+" ")
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
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socketPath)
	}}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1/status", nil)
	if err != nil {
		return daemonapi.DaemonStatus{}, err
	}
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
