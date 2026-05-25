// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/dnsresolver"
	"routerd/pkg/platform"
	"routerd/pkg/resourcequery"
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

	RuntimeDir string
	StateDir   string
}

func (c Controller) Start(ctx context.Context) {
	_ = c.Reconcile(ctx)
	if c.Bus == nil {
		return
	}
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.**"}, Filter: c.eventRelevant}, 64)
	go func() {
		for event := range ch {
			_ = c.HandleEvent(ctx, event)
		}
	}()
}

func (c Controller) HandleEvent(ctx context.Context, event daemonapi.DaemonEvent) error {
	if strings.HasPrefix(event.Type, "routerd.dhcp.lease.") {
		return c.forwardLeaseEvent(ctx, event)
	}
	return c.Reconcile(ctx)
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
		spec, err = c.attachForwarders(resource.Metadata.Name, spec)
		if err != nil {
			return err
		}
		spec = dnsresolver.NormalizeSpec(spec)
		spec, waiting, blockReason, err := c.expandSpec(spec)
		if err != nil {
			return err
		}
		if blockReason != "" {
			if err := c.saveStatus(resource.Metadata.Name, spec, "Pending", blockReason, waiting); err != nil {
				return err
			}
			continue
		}
		config, err := c.runtimeConfig(resource.Metadata.Name, spec)
		if err != nil {
			return err
		}
		phase := "Applied"
		message := ""
		if len(waiting) > 0 {
			phase = "Degraded"
			message = "WaitingForDependencies"
		}
		changed := c.DryRun
		if !c.DryRun {
			changed, err = c.ensureRunning(ctx, resource.Metadata.Name, config)
			if err != nil {
				var reloadErr resolverReloadError
				if errors.As(err, &reloadErr) {
					phase = "Degraded"
					message = err.Error()
				} else {
					phase = "Pending"
					if err := c.saveStatus(resource.Metadata.Name, spec, phase, err.Error(), waiting); err != nil {
						return err
					}
					return err
				}
			}
		}
		if err := c.saveStatus(resource.Metadata.Name, spec, phase, message, waiting); err != nil {
			return err
		}
		if changed && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd"}, "routerd.dns.resolver.configured", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DNSResolver", Name: resource.Metadata.Name}
			_ = c.Bus.Publish(ctx, event)
		}
	}
	return nil
}

func (c Controller) runtimeConfig(name string, spec api.DNSResolverSpec) (dnsresolver.RuntimeConfig, error) {
	config := dnsresolver.RuntimeConfig{Resource: name, Spec: spec}
	servedZones := dnsResolverZoneRefs(spec)
	autoRecords := c.hostnameRecordsForResolver(servedZones)
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DNSZone" {
			continue
		}
		zoneSpec, err := resource.DNSZoneSpec()
		if err != nil {
			return config, err
		}
		if servedZones[resource.Metadata.Name] {
			zoneSpec = appendHostnameRecords(resource.Metadata.Name, zoneSpec, autoRecords)
		}
		zoneSpec, pendingRecords, err := c.expandZoneSpec(zoneSpec)
		if err != nil {
			return config, err
		}
		if err := c.saveZoneStatus(resource.Metadata.Name, zoneSpec, pendingRecords); err != nil {
			return config, err
		}
		config.Zones = append(config.Zones, dnsresolver.RuntimeZone{Name: resource.Metadata.Name, Spec: zoneSpec})
	}
	return config, nil
}

type hostnameRecord struct {
	Hostname string
	Address  string
	Family   string
}

func (c Controller) hostnameRecordsForResolver(servedZones map[string]bool) map[string][]hostnameRecord {
	out := map[string][]hostnameRecord{}
	if c.Router == nil || c.Store == nil {
		return out
	}
	for _, resource := range c.Router.Spec.Resources {
		switch resource.Kind {
		case "VirtualAddress":
			if resource.APIVersion != api.NetAPIVersion {
				continue
			}
			spec, err := resource.VirtualAddressSpec()
			if err != nil {
				continue
			}
			if spec.ExternalDNS {
				continue
			}
			hostname := strings.TrimSpace(spec.Hostname)
			if hostname == "" {
				continue
			}
			address := statusAddressValue(statusString(c.Store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", resource.Metadata.Name)["address"]))
			if address == "" {
				address = statusAddressValue(spec.Address)
			}
			c.addHostnameRecord(out, servedZones, hostname, address)
		case "IngressService":
			if resource.APIVersion != api.FirewallAPIVersion {
				continue
			}
			spec, err := resource.IngressServiceSpec()
			if err != nil {
				continue
			}
			if spec.ExternalDNS {
				continue
			}
			hostname := strings.TrimSpace(spec.Hostname)
			if hostname == "" {
				continue
			}
			address := statusAddressValue(statusString(c.Store.ObjectStatus(api.FirewallAPIVersion, "IngressService", resource.Metadata.Name)["listenAddress"]))
			if address == "" {
				address = statusAddressValue(spec.Listen.Address)
			}
			if address == "" && strings.TrimSpace(spec.Listen.AddressFrom.Resource) != "" {
				address = statusAddressValue(resourcequery.Value(c.Store, spec.Listen.AddressFrom))
			}
			c.addHostnameRecord(out, servedZones, hostname, address)
		}
	}
	return out
}

func (c Controller) addHostnameRecord(out map[string][]hostnameRecord, servedZones map[string]bool, hostname, address string) {
	hostname = strings.Trim(strings.TrimSpace(hostname), ".")
	address = statusAddressValue(address)
	if hostname == "" || address == "" {
		return
	}
	ip := net.ParseIP(address)
	if ip == nil {
		return
	}
	family := "ipv6"
	if ip.To4() != nil {
		family = "ipv4"
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "DNSZone" || !servedZones[resource.Metadata.Name] {
			continue
		}
		zoneSpec, err := resource.DNSZoneSpec()
		if err != nil {
			continue
		}
		relative, ok := relativeHostname(hostname, zoneSpec.Zone)
		if !ok {
			continue
		}
		out[resource.Metadata.Name] = append(out[resource.Metadata.Name], hostnameRecord{Hostname: relative, Address: address, Family: family})
		return
	}
}

func appendHostnameRecords(zoneName string, spec api.DNSZoneSpec, records map[string][]hostnameRecord) api.DNSZoneSpec {
	seen := map[string]bool{}
	for _, record := range spec.Records {
		host := canonicalRecordHostname(record.Hostname, spec.Zone)
		if strings.TrimSpace(record.IPv4) != "" || strings.TrimSpace(record.IPv4From.Resource) != "" {
			seen[host+"|ipv4"] = true
		}
		if strings.TrimSpace(record.IPv6) != "" || strings.TrimSpace(record.IPv6From.Resource) != "" {
			seen[host+"|ipv6"] = true
		}
	}
	for _, value := range records[zoneName] {
		key := canonicalRecordHostname(value.Hostname, spec.Zone) + "|" + value.Family
		if seen[key] {
			continue
		}
		seen[key] = true
		record := api.DNSZoneRecordSpec{Hostname: value.Hostname}
		if value.Family == "ipv6" {
			record.IPv6 = value.Address
		} else {
			record.IPv4 = value.Address
		}
		spec.Records = append(spec.Records, record)
	}
	return spec
}

func (c Controller) attachForwarders(resolverName string, spec api.DNSResolverSpec) (api.DNSResolverSpec, error) {
	if c.Router == nil {
		return spec, nil
	}
	upstreams := map[string]api.DNSUpstreamSpec{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "DNSUpstream" {
			continue
		}
		upstreamSpec, err := resource.DNSUpstreamSpec()
		if err != nil {
			return spec, err
		}
		upstreams[resource.Metadata.Name] = upstreamSpec
	}
	var sources []api.DNSResolverSourceSpec
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "DNSForwarder" {
			continue
		}
		forwarder, err := resource.DNSForwarderSpec()
		if err != nil {
			return spec, err
		}
		if refName(forwarder.Resolver) != resolverName {
			continue
		}
		source := api.DNSResolverSourceSpec{
			Name:              resource.Metadata.Name,
			Match:             append([]string(nil), forwarder.Match...),
			DNSSECValidate:    forwarder.DNSSECValidate,
			Healthcheck:       forwarder.Healthcheck,
			ZoneRef:           append([]string(nil), forwarder.ZoneRefs...),
			BootstrapResolver: nil,
		}
		if len(forwarder.ZoneRefs) > 0 {
			source.Kind = "zone"
		} else {
			source.Kind = "forward"
			for _, match := range forwarder.Match {
				if strings.TrimSpace(match) == "." {
					source.Kind = "upstream"
					break
				}
			}
			for _, upstreamRef := range forwarder.Upstreams {
				name := refName(upstreamRef)
				upstream, ok := upstreams[name]
				if !ok {
					return spec, fmt.Errorf("DNSForwarder/%s references missing DNSUpstream %q", resource.Metadata.Name, upstreamRef)
				}
				if len(upstream.AddressFrom) > 0 {
					source.UpstreamFrom = append(source.UpstreamFrom, upstream.AddressFrom...)
				}
				if strings.TrimSpace(upstream.Address) != "" {
					raw, err := dnsUpstreamURL(upstream, upstream.Address)
					if err != nil {
						return spec, fmt.Errorf("DNSUpstream/%s: %w", name, err)
					}
					source.Upstreams = append(source.Upstreams, raw)
				}
				if len(upstream.Bootstrap) > 0 {
					source.BootstrapResolver = append(source.BootstrapResolver, upstream.Bootstrap...)
				}
				if source.ViaInterface == "" && strings.TrimSpace(upstream.SourceInterface) != "" {
					source.ViaInterface = upstream.SourceInterface
				}
			}
		}
		sources = append(sources, source)
	}
	if len(sources) == 0 && len(spec.Sources) > 0 {
		return spec, nil
	}
	spec.Sources = sources
	return spec, nil
}

func dnsUpstreamURL(spec api.DNSUpstreamSpec, address string) (string, error) {
	protocol := strings.ToLower(strings.TrimSpace(spec.Protocol))
	if protocol == "" {
		protocol = "udp"
	}
	address = strings.TrimSpace(address)
	if address == "" {
		return "", fmt.Errorf("address is required")
	}
	port := spec.Port
	switch protocol {
	case "udp", "tcp":
		if port == 0 {
			port = 53
		}
		return protocol + "://" + net.JoinHostPort(address, strconv.Itoa(port)), nil
	case "dot":
		if port == 0 {
			port = 853
		}
		values := url.Values{}
		if serverName := strings.TrimSpace(spec.TLSName); serverName != "" {
			values.Set("serverName", serverName)
		}
		raw := "tls://" + net.JoinHostPort(address, strconv.Itoa(port))
		if encoded := values.Encode(); encoded != "" {
			raw += "?" + encoded
		}
		return raw, nil
	case "doh":
		if port == 0 {
			port = 443
		}
		path := strings.TrimSpace(spec.Path)
		if path == "" {
			path = "/dns-query"
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		values := url.Values{}
		if serverName := strings.TrimSpace(spec.TLSName); serverName != "" {
			values.Set("serverName", serverName)
		}
		raw := "https://" + net.JoinHostPort(address, strconv.Itoa(port)) + path
		if encoded := values.Encode(); encoded != "" {
			raw += "?" + encoded
		}
		return raw, nil
	default:
		return "", fmt.Errorf("protocol must be udp, tcp, dot, or doh")
	}
}

func relativeHostname(hostname, zone string) (string, bool) {
	hostname = strings.Trim(strings.ToLower(strings.TrimSpace(hostname)), ".")
	zone = strings.Trim(strings.ToLower(strings.TrimSpace(zone)), ".")
	if hostname == "" || zone == "" {
		return "", false
	}
	if hostname == zone {
		return "@", true
	}
	suffix := "." + zone
	if !strings.HasSuffix(hostname, suffix) {
		return "", false
	}
	return strings.TrimSuffix(hostname, suffix), true
}

func canonicalRecordHostname(hostname, zone string) string {
	hostname = strings.Trim(strings.ToLower(strings.TrimSpace(hostname)), ".")
	zone = strings.Trim(strings.ToLower(strings.TrimSpace(zone)), ".")
	if hostname == "" || hostname == "@" {
		return zone
	}
	if strings.HasSuffix(hostname, "."+zone) || hostname == zone {
		return hostname
	}
	return hostname + "." + zone
}

func (c Controller) expandSpec(spec api.DNSResolverSpec) (api.DNSResolverSpec, []map[string]string, string, error) {
	var waiting []map[string]string
	var expandedListen []api.DNSResolverListenSpec
	for i := range spec.Listen {
		addresses, listenWaiting, blockReason := expandListenAddresses(c.Store, spec.Listen[i])
		if blockReason != "" {
			return spec, waiting, blockReason, nil
		}
		waiting = append(waiting, listenWaiting...)
		if len(addresses) == 0 {
			continue
		}
		listen := spec.Listen[i]
		listen.Addresses = addresses
		listen.AddressFrom = nil
		listen.AddressSources = nil
		expandedListen = append(expandedListen, listen)
	}
	spec.Listen = expandedListen
	if len(spec.Listen) == 0 {
		return spec, waiting, "AddressUnresolved: no resolved listen addresses", nil
	}
	var expandedSources []api.DNSResolverSourceSpec
	for i := range spec.Sources {
		source := spec.Sources[i]
		upstreams, upstreamWaiting := expandUpstreams(c.Store, source.Upstreams, source.UpstreamFrom)
		if len(upstreamWaiting) > 0 {
			waiting = append(waiting, waitingForSource(source, upstreamWaiting)...)
			continue
		}
		source.Upstreams = upstreams
		source.UpstreamFrom = nil
		source.BootstrapResolver = expandStrings(c.Store, source.BootstrapResolver)
		if (source.Kind == "forward" || source.Kind == "upstream") && len(source.Upstreams) == 0 && sourceHasDynamicUpstreams(spec.Sources[i]) {
			continue
		}
		expandedSources = append(expandedSources, source)
	}
	spec.Sources = expandedSources
	if len(spec.Sources) == 0 {
		return spec, waiting, "UpstreamUnresolved: no usable DNS sources", nil
	}
	if err := dnsresolver.Validate(spec); err != nil {
		return spec, waiting, "", err
	}
	return spec, waiting, "", nil
}

func (c Controller) expandZoneSpec(spec api.DNSZoneSpec) (api.DNSZoneSpec, []map[string]string, error) {
	var pending []map[string]string
	for i := range spec.Records {
		record := &spec.Records[i]
		if strings.TrimSpace(record.IPv4From.Resource) != "" {
			value, pendingReason, err := resolveRecordAddress(c.Store, record.IPv4From, true)
			if err != nil {
				return spec, pending, fmt.Errorf("DNSZone record %q ipv4From: %w", record.Hostname, err)
			}
			if pendingReason != "" {
				pending = append(pending, map[string]string{"hostname": record.Hostname, "field": "ipv4", "source": record.IPv4From.Resource, "reason": pendingReason})
			} else if value != "" {
				record.IPv4 = value
			}
			record.IPv4From = api.StatusValueSourceSpec{}
		}
		if strings.TrimSpace(record.IPv6From.Resource) != "" {
			value, pendingReason, err := resolveRecordAddress(c.Store, record.IPv6From, false)
			if err != nil {
				return spec, pending, fmt.Errorf("DNSZone record %q ipv6From: %w", record.Hostname, err)
			}
			if pendingReason != "" {
				pending = append(pending, map[string]string{"hostname": record.Hostname, "field": "ipv6", "source": record.IPv6From.Resource, "reason": pendingReason})
			} else if value != "" {
				record.IPv6 = value
			}
			record.IPv6From = api.StatusValueSourceSpec{}
		}
	}
	return spec, pending, nil
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

func (c Controller) ensureRunning(ctx context.Context, name string, config dnsresolver.RuntimeConfig) (bool, error) {
	runtimeDir, stateDir := c.dirs()
	configPath := filepath.Join(stateDir, "dns-resolver", name, "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return false, err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return false, err
	}
	changed := false
	currentConfig, readErr := os.ReadFile(configPath)
	if readErr != nil || string(bytes.TrimSpace(currentConfig)) != string(data) {
		if err := os.WriteFile(configPath, append(data, '\n'), 0644); err != nil {
			return false, err
		}
		changed = true
	}
	if !changed {
		return false, nil
	}
	if err := c.reloadResolver(ctx, filepath.Join(runtimeDir, "dns-resolver", name+".sock")); err != nil {
		return true, err
	}
	return true, nil
}

type resolverReloadError struct {
	status string
	body   string
}

func (e resolverReloadError) Error() string {
	if strings.TrimSpace(e.body) == "" {
		return "ReloadFailed: " + e.status
	}
	return "ReloadFailed: " + e.status + ": " + strings.TrimSpace(e.body)
}

func (c Controller) reloadResolver(ctx context.Context, socket string) error {
	client := newResolverHTTPClient(socket)
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/reload", nil)
	if err != nil {
		return err
	}
	req.Close = true
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return resolverReloadError{status: resp.Status, body: string(body)}
	}
	return nil
}

var newResolverHTTPClient = resolverUnixHTTPClient

func (c Controller) dirs() (runtimeDir, stateDir string) {
	defaults, _ := platform.Current()
	runtimeDir = strings.TrimRight(c.RuntimeDir, "/")
	if runtimeDir == "" {
		runtimeDir = defaults.RuntimeDir
	}
	stateDir = strings.TrimRight(c.StateDir, "/")
	if stateDir == "" {
		stateDir = defaults.StateDir
	}
	return runtimeDir, stateDir
}

func (c Controller) saveStatus(name string, spec api.DNSResolverSpec, phase, message string, waiting []map[string]string) error {
	status := map[string]any{
		"phase":           phase,
		"listeners":       len(spec.Listen),
		"listenAddresses": resolvedListenAddresses(spec),
		"sources":         len(spec.Sources),
		"updatedAt":       time.Now().UTC().Format(time.RFC3339Nano),
	}
	if message != "" {
		status["message"] = message
		status["reason"] = message
	}
	if len(waiting) > 0 {
		status["waiting"] = waiting
	}
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "DNSResolver", name, status)
}

func (c Controller) saveZoneStatus(name string, spec api.DNSZoneSpec, pendingRecords []map[string]string) error {
	phase := "Applied"
	if len(pendingRecords) > 0 {
		phase = "Pending"
	}
	status := map[string]any{
		"phase":          phase,
		"zone":           spec.Zone,
		"records":        len(spec.Records),
		"pendingRecords": pendingRecords,
		"updatedAt":      time.Now().UTC().Format(time.RFC3339Nano),
	}
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "DNSZone", name, status)
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
		runtimeDir, _ := c.dirs()
		socket := filepath.Join(runtimeDir, "dns-resolver", resource.Metadata.Name+".sock")
		client := resolverUnixHTTPClient(socket)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/leases", bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Close = true
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			client.CloseIdleConnections()
			continue
		}
		_ = resp.Body.Close()
		client.CloseIdleConnections()
	}
	return nil
}

func resolverUnixHTTPClient(socket string) *http.Client {
	return &http.Client{Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socket)
	}}}
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

func expandListenAddresses(store Store, listen api.DNSResolverListenSpec) ([]string, []map[string]string, string) {
	var out []string
	var waiting []map[string]string
	for _, value := range listen.Addresses {
		trimmed := strings.TrimSpace(value)
		if isStatusRef(trimmed) {
			return nil, waiting, "AddressUsesOldStatusExpression: " + trimmed
		}
		resolved := value
		if list := decodeStringList(resolved); len(list) > 0 {
			for _, item := range list {
				if address := statusAddressValue(item); address != "" {
					out = append(out, address)
				}
			}
			continue
		}
		if address := statusAddressValue(resolved); address != "" {
			out = append(out, address)
		}
	}
	for _, source := range listen.AddressFrom {
		resolved := strings.Join(resourcequery.Values(store, source), ",")
		if strings.TrimSpace(resolved) == "" {
			if source.Optional {
				continue
			}
			waiting = append(waiting, map[string]string{
				"kind":   "listen",
				"name":   listen.Name,
				"field":  "address",
				"source": source.Resource,
				"reason": "AddressUnresolved",
			})
			continue
		}
		if list := decodeStringList(resolved); len(list) > 0 {
			for _, item := range list {
				if address := statusAddressValue(item); address != "" {
					out = append(out, address)
				}
			}
			continue
		}
		if address := statusAddressValue(resolved); address != "" {
			out = append(out, address)
		}
	}
	return compactStrings(out), waiting, ""
}

func resolveRecordAddress(store Store, source api.StatusValueSourceSpec, wantIPv4 bool) (string, string, error) {
	resolved := strings.TrimSpace(resourcequery.Value(store, source))
	if resolved == "" {
		if source.Optional {
			return "", "", nil
		}
		return "", "AddressUnresolved", nil
	}
	values := decodeStringList(resolved)
	if len(values) == 0 {
		values = []string{resolved}
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(value); err == nil {
			value = prefix.Addr().String()
		}
		ip := net.ParseIP(value)
		if ip == nil {
			return "", "", fmt.Errorf("resolved value %q is not an IP address", value)
		}
		if wantIPv4 && ip.To4() == nil {
			return "", "", fmt.Errorf("resolved value %q is not an IPv4 address", value)
		}
		if !wantIPv4 && ip.To4() != nil {
			return "", "", fmt.Errorf("resolved value %q is not an IPv6 address", value)
		}
		return value, "", nil
	}
	if source.Optional {
		return "", "", nil
	}
	return "", "AddressUnresolved", nil
}

func statusAddressValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Addr().String()
	}
	return value
}

// expandUpstreams resolves a source's static upstreams plus any upstreamFrom
// references into concrete upstream values. It returns waiting entries when a
// required dynamic upstream has not published a value yet.
func expandUpstreams(store Store, values []string, sources []api.StatusValueSourceSpec) ([]string, []map[string]string) {
	var out []string
	var waiting []map[string]string
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if isStatusRef(trimmed) {
			resolved := valueFromStatusRef(store, trimmed)
			if strings.TrimSpace(resolved) == "" {
				waiting = append(waiting, map[string]string{"field": "upstream", "source": statusRefSource(trimmed), "reason": "UpstreamUnresolved"})
				continue
			}
			if list := decodeStringList(resolved); len(list) > 0 {
				for _, item := range list {
					if strings.TrimSpace(item) != "" {
						out = append(out, dnsresolver.NormalizeUpstream(item))
					}
				}
				continue
			}
			out = append(out, dnsresolver.NormalizeUpstream(resolved))
			continue
		}
		out = append(out, dnsresolver.NormalizeUpstream(trimmed))
	}
	for _, source := range sources {
		resolved := false
		for _, value := range resourcequery.Values(store, source) {
			if strings.TrimSpace(value) != "" {
				out = append(out, dnsresolver.NormalizeUpstream(value))
				resolved = true
			}
		}
		if !resolved && !source.Optional {
			waiting = append(waiting, map[string]string{
				"field":  "upstream",
				"source": source.Resource,
				"reason": "UpstreamUnresolved",
			})
		}
	}
	return out, waiting
}

func waitingForSource(source api.DNSResolverSourceSpec, waiting []map[string]string) []map[string]string {
	out := make([]map[string]string, 0, len(waiting))
	for _, item := range waiting {
		next := map[string]string{
			"kind":   "source",
			"name":   source.Name,
			"field":  item["field"],
			"source": item["source"],
			"reason": item["reason"],
		}
		out = append(out, next)
	}
	return out
}

func sourceHasDynamicUpstreams(source api.DNSResolverSourceSpec) bool {
	if len(source.UpstreamFrom) > 0 {
		return true
	}
	for _, upstream := range source.Upstreams {
		if isStatusRef(upstream) {
			return true
		}
	}
	return false
}

func statusRefSource(expr string) string {
	kind, name, ok := statusRefResource(expr)
	if !ok {
		return strings.TrimSpace(expr)
	}
	return kind + "/" + name
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
	if router == nil {
		return false
	}
	if ref.APIVersion == api.NetAPIVersion && ref.Kind == "VirtualAddress" && hostnameResourceExists(router, ref.Kind, ref.Name) {
		return true
	}
	if ref.APIVersion == api.FirewallAPIVersion && ref.Kind == "IngressService" && hostnameResourceExists(router, ref.Kind, ref.Name) {
		return true
	}
	if ref.APIVersion != api.NetAPIVersion {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "DNSResolver" {
			switch resource.Kind {
			case "DNSZone":
				zoneSpec, err := resource.DNSZoneSpec()
				if err != nil {
					continue
				}
				for _, dep := range dnsZoneStatusRefs(zoneSpec) {
					if dep == ref {
						return true
					}
				}
			case "DNSUpstream":
				upstreamSpec, err := resource.DNSUpstreamSpec()
				if err != nil {
					continue
				}
				for _, dep := range dnsUpstreamStatusRefs(upstreamSpec) {
					if dep == ref {
						return true
					}
				}
			}
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

func hostnameResourceExists(router *api.Router, kind, name string) bool {
	for _, resource := range router.Spec.Resources {
		if resource.Kind != kind || resource.Metadata.Name != name {
			continue
		}
		switch kind {
		case "VirtualAddress":
			spec, err := resource.VirtualAddressSpec()
			return err == nil && strings.TrimSpace(spec.Hostname) != "" && !spec.ExternalDNS
		case "IngressService":
			spec, err := resource.IngressServiceSpec()
			return err == nil && strings.TrimSpace(spec.Hostname) != "" && !spec.ExternalDNS
		}
	}
	return false
}

func dnsResolverZoneRefs(spec api.DNSResolverSpec) map[string]bool {
	out := map[string]bool{}
	for _, source := range spec.Sources {
		if source.Kind != "zone" {
			continue
		}
		for _, ref := range source.ZoneRef {
			name := refName(ref)
			if name != "" {
				out[name] = true
			}
		}
	}
	return out
}

func dnsZoneStatusRefs(spec api.DNSZoneSpec) []daemonapi.ResourceRef {
	var refs []daemonapi.ResourceRef
	for _, record := range spec.Records {
		if ref, ok := resourcequery.SourceRef(record.IPv4From); ok {
			refs = append(refs, daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: ref.Kind, Name: ref.Name})
		}
		if ref, ok := resourcequery.SourceRef(record.IPv6From); ok {
			refs = append(refs, daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: ref.Kind, Name: ref.Name})
		}
	}
	return refs
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
		for _, source := range listen.AddressFrom {
			if ref, ok := resourcequery.SourceRef(source); ok {
				refs = append(refs, daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: ref.Kind, Name: ref.Name})
			}
		}
		for _, address := range listen.Addresses {
			add(address)
		}
	}
	for _, source := range spec.Sources {
		for _, upstream := range source.Upstreams {
			add(upstream)
		}
		for _, upstream := range source.UpstreamFrom {
			if ref, ok := resourcequery.SourceRef(upstream); ok {
				refs = append(refs, daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: ref.Kind, Name: ref.Name})
			}
		}
		for _, resolver := range source.BootstrapResolver {
			add(resolver)
		}
	}
	return refs
}

func dnsUpstreamStatusRefs(spec api.DNSUpstreamSpec) []daemonapi.ResourceRef {
	var refs []daemonapi.ResourceRef
	for _, source := range spec.AddressFrom {
		if ref, ok := resourcequery.SourceRef(source); ok {
			refs = append(refs, daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: ref.Kind, Name: ref.Name})
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

func refName(ref string) string {
	if i := strings.LastIndex(strings.TrimSpace(ref), "/"); i >= 0 {
		return strings.TrimSpace(ref)[i+1:]
	}
	return strings.TrimSpace(ref)
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

func statusString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
