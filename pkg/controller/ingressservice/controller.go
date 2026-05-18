// SPDX-License-Identifier: BSD-3-Clause

package ingressservice

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/resourcequery"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type CheckFunc func(context.Context, string, int, time.Duration) error

type Controller struct {
	Router   *api.Router
	Bus      *bus.Bus
	Store    Store
	DryRun   bool
	Resolver *net.Resolver
	Check    CheckFunc
	Logger   *slog.Logger
}

type backendStatus struct {
	Name            string `json:"name"`
	Address         string `json:"address"`
	ResolvedAddress string `json:"resolvedAddress,omitempty"`
	Port            int    `json:"port"`
	Healthy         bool   `json:"healthy"`
	Reason          string `json:"reason,omitempty"`
	HealthyCount    int    `json:"healthyCount,omitempty"`
	UnhealthyCount  int    `json:"unhealthyCount,omitempty"`
	LastHealthyAt   string `json:"lastHealthyAt,omitempty"`
	LastUnhealthyAt string `json:"lastUnhealthyAt,omitempty"`
}

type previousBackendStatus struct {
	ResolvedAddress string
	Healthy         bool
	HealthyCount    int
	UnhealthyCount  int
	LastHealthyAt   string
	LastUnhealthyAt string
}

func (c *Controller) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.FirewallAPIVersion || resource.Kind != "IngressService" {
			continue
		}
		spec, err := resource.IngressServiceSpec()
		if err != nil {
			return err
		}
		if err := c.reconcileResource(ctx, resource.Metadata.Name, spec); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) reconcileResource(ctx context.Context, name string, spec api.IngressServiceSpec) error {
	timeout := time.Second
	if spec.HealthCheck.Timeout != "" {
		if parsed, err := time.ParseDuration(spec.HealthCheck.Timeout); err == nil && parsed > 0 {
			timeout = parsed
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	previous := previousBackends(c.Store.ObjectStatus(api.FirewallAPIVersion, "IngressService", name))
	var backends []backendStatus
	for i, backend := range spec.Backends {
		status := backendStatus{
			Name:    defaultString(backend.Name, fmt.Sprintf("backend-%d", i)),
			Address: strings.TrimSpace(backend.Address),
			Port:    backend.Port,
		}
		resolved, reason := c.resolveAddress(ctx, backend, previous[status.Name])
		status.ResolvedAddress = resolved
		checkOK := resolved != ""
		status.Reason = reason
		if checkOK && !c.DryRun {
			switch defaultString(spec.HealthCheck.Protocol, "tcp") {
			case "tcp":
				if err := c.check(ctx, resolved, backend.Port, timeout); err != nil {
					checkOK = false
					status.Reason = "TCPCheckFailed: " + err.Error()
				}
			case "http", "https":
				if err := c.checkHTTP(ctx, resolved, backend.Port, timeout, spec.HealthCheck); err != nil {
					checkOK = false
					status.Reason = strings.ToUpper(spec.HealthCheck.Protocol) + "CheckFailed: " + err.Error()
				}
			}
		}
		status.applyCheckResult(checkOK, previous[status.Name], spec.HealthCheck, now)
		backends = append(backends, status)
	}
	active, healthy := selectActiveBackend(backends, spec.Policy.Selection)
	phase := "NoHealthyBackends"
	if active.Name != "" {
		phase = "Active"
		if healthy < len(backends) {
			phase = "Degraded"
		}
	}
	status := map[string]any{
		"phase":           phase,
		"activeBackend":   map[string]any{"name": active.Name, "address": active.ResolvedAddress, "port": active.Port},
		"healthyBackends": healthy,
		"totalBackends":   len(backends),
		"backends":        backends,
		"selection":       defaultString(spec.Policy.Selection, "failover"),
		"dryRun":          c.DryRun,
		"observedAt":      now,
	}
	if active.Name == "" {
		status["conditions"] = []map[string]any{{"type": "BackendsHealthy", "status": "False", "reason": defaultString(spec.Policy.OnNoHealthyBackends, "drop")}}
	} else {
		status["conditions"] = []map[string]any{{"type": "BackendsHealthy", "status": "True", "reason": "ActiveBackendSelected"}}
	}
	return c.Store.SaveObjectStatus(api.FirewallAPIVersion, "IngressService", name, status)
}

func (c *Controller) resolveAddress(ctx context.Context, backend api.IngressBackendSpec, previous previousBackendStatus) (string, string) {
	address := strings.TrimSpace(backend.Address)
	if address == "" && strings.TrimSpace(backend.AddressFrom.Resource) != "" {
		address = statusAddressValue(resourcequery.Value(c.Store, backend.AddressFrom))
	}
	if address == "" {
		return "", "AddressFromPending"
	}
	if addr, err := netip.ParseAddr(address); err == nil && addr.Is4() {
		return addr.String(), ""
	}
	resolver := c.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	ips, err := resolver.LookupIPAddr(ctx, address)
	if err != nil {
		if previous.ResolvedAddress != "" {
			return previous.ResolvedAddress, "DNSFailedUsingPrevious: " + err.Error()
		}
		return "", "DNSFailed: " + err.Error()
	}
	var values []string
	for _, ip := range ips {
		addr := ip.IP
		if v4 := addr.To4(); v4 != nil {
			var raw [4]byte
			copy(raw[:], v4)
			values = append(values, netip.AddrFrom4(raw).String())
		}
	}
	sort.Strings(values)
	if len(values) == 0 {
		if previous.ResolvedAddress != "" {
			return previous.ResolvedAddress, "DNSNoIPv4UsingPrevious"
		}
		return "", "DNSNoIPv4"
	}
	return values[0], ""
}

func (s *backendStatus) applyCheckResult(ok bool, previous previousBackendStatus, spec api.IngressHealthCheckSpec, now string) {
	healthyThreshold := defaultInt(spec.HealthyThreshold, 1)
	unhealthyThreshold := defaultInt(spec.UnhealthyThreshold, 1)
	if ok {
		s.HealthyCount = previous.HealthyCount + 1
		s.UnhealthyCount = 0
		s.LastHealthyAt = now
		s.LastUnhealthyAt = previous.LastUnhealthyAt
		s.Healthy = previous.Healthy || s.HealthyCount >= healthyThreshold
		return
	}
	s.HealthyCount = 0
	s.UnhealthyCount = previous.UnhealthyCount + 1
	s.LastHealthyAt = previous.LastHealthyAt
	s.LastUnhealthyAt = now
	s.Healthy = previous.Healthy && s.UnhealthyCount < unhealthyThreshold
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

func (c *Controller) check(ctx context.Context, address string, port int, timeout time.Duration) error {
	if c.Check != nil {
		return c.Check(ctx, address, port, timeout)
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(address, fmt.Sprint(port)))
	if err != nil {
		return err
	}
	return conn.Close()
}

func (c *Controller) checkHTTP(ctx context.Context, address string, port int, timeout time.Duration, spec api.IngressHealthCheckSpec) error {
	scheme := defaultString(spec.Protocol, "http")
	path := defaultString(spec.Path, "/")
	target := net.JoinHostPort(address, fmt.Sprint(port))
	u := url.URL{Scheme: scheme, Host: target, Path: path}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	if host := strings.TrimSpace(spec.Host); host != "" {
		req.Host = host
	}
	transport := &http.Transport{
		DisableKeepAlives: true,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: spec.TLSSkipVerify, //nolint:gosec // explicitly user-controlled for self-signed backend health checks
			ServerName:         strings.TrimSpace(spec.Host),
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if !expectedHTTPStatus(resp.StatusCode, spec.ExpectedStatus) {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}
	if spec.ExpectedBody != "" {
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return err
		}
		if !strings.Contains(string(data), spec.ExpectedBody) {
			return fmt.Errorf("response body did not contain expected text")
		}
	}
	return nil
}

func expectedHTTPStatus(status int, expected []int) bool {
	if len(expected) == 0 {
		return status >= 200 && status < 400
	}
	for _, code := range expected {
		if status == code {
			return true
		}
	}
	return false
}

func selectActiveBackend(backends []backendStatus, selection string) (backendStatus, int) {
	healthy := 0
	var first backendStatus
	for _, backend := range backends {
		if !backend.Healthy {
			continue
		}
		healthy++
		if first.Name == "" {
			first = backend
		}
	}
	if healthy == 0 {
		return backendStatus{}, 0
	}
	return first, healthy
}

func previousBackends(status map[string]any) map[string]previousBackendStatus {
	out := map[string]previousBackendStatus{}
	switch values := status["backends"].(type) {
	case []any:
		for _, value := range values {
			item, _ := value.(map[string]any)
			name := strings.TrimSpace(fmt.Sprint(item["name"]))
			if name != "" {
				out[name] = previousBackendStatus{
					ResolvedAddress: statusString(item["resolvedAddress"]),
					Healthy:         statusBool(item["healthy"]),
					HealthyCount:    statusInt(item["healthyCount"]),
					UnhealthyCount:  statusInt(item["unhealthyCount"]),
					LastHealthyAt:   statusString(item["lastHealthyAt"]),
					LastUnhealthyAt: statusString(item["lastUnhealthyAt"]),
				}
			}
		}
	case []backendStatus:
		for _, item := range values {
			if item.Name != "" {
				out[item.Name] = previousBackendStatus{
					ResolvedAddress: item.ResolvedAddress,
					Healthy:         item.Healthy,
					HealthyCount:    item.HealthyCount,
					UnhealthyCount:  item.UnhealthyCount,
					LastHealthyAt:   item.LastHealthyAt,
					LastUnhealthyAt: item.LastUnhealthyAt,
				}
			}
		}
	}
	return out
}

func statusString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func statusInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		var out int
		_, _ = fmt.Sscanf(strings.TrimSpace(typed), "%d", &out)
		return out
	default:
		return 0
	}
}

func statusBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}
