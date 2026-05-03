package dnsresolver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/dnsresolver"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type Controller struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	DryRun bool
	Binary string
}

type runningResolver struct {
	process *exec.Cmd
	spec    api.DNSResolverSpec
	config  string
}

var (
	runningMu        sync.Mutex
	runningResolvers = map[string]runningResolver{}
)

func (c Controller) Start(ctx context.Context) {
	_ = c.Reconcile(ctx)
	if c.Bus == nil {
		return
	}
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.**"}, Filter: c.eventRelevant}, 64)
	go func() {
		for event := range ch {
			if strings.HasPrefix(event.Type, "routerd.dhcp.lease.") {
				_ = c.forwardLeaseEvent(ctx, event)
				continue
			}
			_ = c.Reconcile(ctx)
		}
	}()
}

func (c Controller) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DNSResolver" {
			continue
		}
		spec, err := resource.DNSResolverSpec()
		if err != nil {
			return err
		}
		spec = dnsresolver.NormalizeSpec(spec)
		spec, pending, err := c.expandSpec(spec)
		if err != nil {
			return err
		}
		if pending != "" {
			if err := c.saveStatus(resource.Metadata.Name, spec, "Pending", pending); err != nil {
				return err
			}
			continue
		}
		config, err := c.runtimeConfig(resource.Metadata.Name, spec)
		if err != nil {
			return err
		}
		phase := "Applied"
		if !c.DryRun {
			if err := c.ensureRunning(ctx, resource.Metadata.Name, spec, config); err != nil {
				phase = "Pending"
				if err := c.saveStatus(resource.Metadata.Name, spec, phase, err.Error()); err != nil {
					return err
				}
				return err
			}
		}
		if err := c.saveStatus(resource.Metadata.Name, spec, phase, ""); err != nil {
			return err
		}
		if c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd"}, "routerd.dns.resolver.configured", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DNSResolver", Name: resource.Metadata.Name}
			_ = c.Bus.Publish(ctx, event)
		}
	}
	return nil
}

func (c Controller) runtimeConfig(name string, spec api.DNSResolverSpec) (dnsresolver.RuntimeConfig, error) {
	config := dnsresolver.RuntimeConfig{Resource: name, Spec: spec}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DNSZone" {
			continue
		}
		zoneSpec, err := resource.DNSZoneSpec()
		if err != nil {
			return config, err
		}
		config.Zones = append(config.Zones, dnsresolver.RuntimeZone{Name: resource.Metadata.Name, Spec: zoneSpec})
	}
	return config, nil
}

func (c Controller) expandSpec(spec api.DNSResolverSpec) (api.DNSResolverSpec, string, error) {
	for i := range spec.Listen {
		addresses, pending := expandListenAddresses(c.Store, spec.Listen[i])
		if pending != "" {
			return spec, pending, nil
		}
		spec.Listen[i].Addresses = addresses
		spec.Listen[i].AddressSources = nil
	}
	for i := range spec.Sources {
		spec.Sources[i].Upstreams = expandUpstreams(c.Store, spec.Sources[i].Upstreams)
		spec.Sources[i].BootstrapResolver = expandStrings(c.Store, spec.Sources[i].BootstrapResolver)
	}
	if err := dnsresolver.Validate(spec); err != nil {
		return spec, "", err
	}
	return spec, "", nil
}

func (c Controller) eventRelevant(event daemonapi.DaemonEvent) bool {
	if strings.HasPrefix(event.Type, "routerd.dhcp.lease.") {
		return true
	}
	if event.Resource == nil {
		return false
	}
	return dnsResolverDependsOn(c.Router, *event.Resource)
}

func (c Controller) ensureRunning(ctx context.Context, name string, spec api.DNSResolverSpec, config dnsresolver.RuntimeConfig) error {
	runningMu.Lock()
	defer runningMu.Unlock()
	configPath := filepath.Join("/var/lib/routerd/dns-resolver", name, "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, append(data, '\n'), 0644); err != nil {
		return err
	}
	if current, ok := runningResolvers[name]; ok && processAlive(current.process.Process) && sameSpec(current.spec, spec) && current.config == string(data) {
		return nil
	}
	if current, ok := runningResolvers[name]; ok && current.process.Process != nil {
		_ = current.process.Process.Signal(syscall.SIGTERM)
		delete(runningResolvers, name)
	}
	binary := c.Binary
	if binary == "" {
		binary = "/usr/local/sbin/routerd-dns-resolver"
	}
	args := []string{
		"daemon",
		"--resource", name,
		"--config-file", configPath,
		"--socket", filepath.Join("/run/routerd/dns-resolver", name+".sock"),
		"--state-file", filepath.Join("/var/lib/routerd/dns-resolver", name, "state.json"),
		"--event-file", filepath.Join("/var/lib/routerd/dns-resolver", name, "events.jsonl"),
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	runningResolvers[name] = runningResolver{process: cmd, spec: spec, config: string(data)}
	go func() {
		_ = cmd.Wait()
		runningMu.Lock()
		if current, ok := runningResolvers[name]; ok && current.process == cmd {
			delete(runningResolvers, name)
		}
		runningMu.Unlock()
	}()
	return nil
}

func (c Controller) saveStatus(name string, spec api.DNSResolverSpec, phase, message string) error {
	status := map[string]any{
		"phase":           phase,
		"listeners":       len(spec.Listen),
		"listenAddresses": resolvedListenAddresses(spec),
		"sources":         len(spec.Sources),
		"updatedAt":       time.Now().UTC().Format(time.RFC3339Nano),
	}
	if message != "" {
		status["message"] = message
	}
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "DNSResolver", name, status)
}

func resolvedListenAddresses(spec api.DNSResolverSpec) []string {
	var out []string
	for _, listen := range spec.Listen {
		out = append(out, listen.Addresses...)
	}
	return compactStrings(out)
}

func (c Controller) forwardLeaseEvent(ctx context.Context, event daemonapi.DaemonEvent) error {
	action := strings.TrimPrefix(event.Type, "routerd.dhcp.lease.")
	if action == event.Type {
		return nil
	}
	payload := map[string]string{
		"action":   action,
		"mac":      event.Attributes["mac"],
		"ip":       event.Attributes["ip"],
		"hostname": event.Attributes["hostname"],
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DNSResolver" {
			continue
		}
		socket := filepath.Join("/run/routerd/dns-resolver", resource.Metadata.Name+".sock")
		client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socket)
		}}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/leases", bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		_ = resp.Body.Close()
	}
	return nil
}

func processAlive(process *os.Process) bool {
	if process == nil {
		return false
	}
	err := process.Signal(syscall.Signal(0))
	return err == nil || err == syscall.EPERM
}

func sameSpec(a, b api.DNSResolverSpec) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

func joinPorts(listen []api.DNSResolverListenSpec) string {
	out := ""
	for _, l := range listen {
		if out != "" {
			out += ","
		}
		out += strconv.Itoa(l.Port)
	}
	return out
}

func expandStrings(store Store, values []string) []string {
	var out []string
	for _, value := range values {
		resolved := valueFromStatusRef(store, value)
		if list := decodeStringList(resolved); len(list) > 0 {
			out = append(out, list...)
			continue
		}
		if strings.TrimSpace(resolved) != "" {
			out = append(out, strings.TrimSpace(resolved))
		}
	}
	return out
}

func expandListenAddresses(store Store, listen api.DNSResolverListenSpec) ([]string, string) {
	var out []string
	for _, value := range listen.Addresses {
		trimmed := strings.TrimSpace(value)
		resolved := valueFromStatusRef(store, value)
		if isStatusRef(trimmed) && strings.TrimSpace(resolved) == "" {
			return nil, "AddressUnresolved: " + trimmed
		}
		if list := decodeStringList(resolved); len(list) > 0 {
			for _, item := range list {
				if strings.TrimSpace(item) != "" {
					out = append(out, strings.TrimSpace(item))
				}
			}
			continue
		}
		if strings.TrimSpace(resolved) != "" {
			out = append(out, strings.TrimSpace(resolved))
		}
	}
	for _, source := range listen.AddressSources {
		resolved := valueFromStatusRef(store, source.Field)
		if strings.TrimSpace(resolved) == "" {
			if source.Optional {
				continue
			}
			return nil, "AddressUnresolved: " + source.Field
		}
		if list := decodeStringList(resolved); len(list) > 0 {
			for _, item := range list {
				if strings.TrimSpace(item) != "" {
					out = append(out, strings.TrimSpace(item))
				}
			}
			continue
		}
		out = append(out, strings.TrimSpace(resolved))
	}
	return compactStrings(out), ""
}

func expandUpstreams(store Store, values []string) []string {
	var out []string
	for _, value := range values {
		resolved := valueFromStatusRef(store, value)
		if list := decodeStringList(resolved); len(list) > 0 {
			for _, item := range list {
				if strings.TrimSpace(item) != "" {
					out = append(out, dnsresolver.NormalizeUpstream(item))
				}
			}
			continue
		}
		if strings.TrimSpace(resolved) != "" {
			out = append(out, dnsresolver.NormalizeUpstream(resolved))
		}
	}
	return out
}

func isStatusRef(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") && strings.Contains(value, ".status.")
}

func compactStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func dnsResolverDependsOn(router *api.Router, ref daemonapi.ResourceRef) bool {
	if router == nil || ref.APIVersion != api.NetAPIVersion {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "DNSResolver" {
			continue
		}
		spec, err := resource.DNSResolverSpec()
		if err != nil {
			continue
		}
		for _, dep := range dnsResolverStatusRefs(spec) {
			if dep == ref {
				return true
			}
		}
	}
	return false
}

func dnsResolverStatusRefs(spec api.DNSResolverSpec) []daemonapi.ResourceRef {
	var refs []daemonapi.ResourceRef
	add := func(expr string) {
		kind, name, ok := statusRefResource(expr)
		if ok {
			refs = append(refs, daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: kind, Name: name})
		}
	}
	for _, listen := range spec.Listen {
		for _, source := range listen.AddressSources {
			add(source.Field)
		}
		for _, address := range listen.Addresses {
			add(address)
		}
	}
	for _, source := range spec.Sources {
		for _, upstream := range source.Upstreams {
			add(upstream)
		}
		for _, resolver := range source.BootstrapResolver {
			add(resolver)
		}
	}
	return refs
}

func statusRefResource(expr string) (string, string, bool) {
	expr = strings.TrimSpace(expr)
	if !isStatusRef(expr) {
		return "", "", false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(expr, "${"), "}")
	left, _, ok := strings.Cut(inner, ".status.")
	if !ok {
		return "", "", false
	}
	kind, name, ok := strings.Cut(left, "/")
	if !ok || kind == "" || name == "" {
		return "", "", false
	}
	return kind, name, true
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
	if ref == "" || !strings.Contains(ref, ".status.") || store == nil {
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
