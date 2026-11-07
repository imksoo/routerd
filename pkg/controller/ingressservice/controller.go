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

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/ingressdrain"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

type Store interface {
	ingressdrain.Reader
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error)
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
	Name            string                    `json:"name"`
	Address         string                    `json:"address"`
	AddressFrom     api.StatusValueSourceSpec `json:"addressFrom,omitempty"`
	ResolvedAddress string                    `json:"resolvedAddress,omitempty"`
	Port            int                       `json:"port"`
	Healthy         bool                      `json:"healthy"`
	Reason          string                    `json:"reason,omitempty"`
	HealthyCount    int                       `json:"healthyCount,omitempty"`
	UnhealthyCount  int                       `json:"unhealthyCount,omitempty"`
	LastHealthyAt   string                    `json:"lastHealthyAt,omitempty"`
	LastUnhealthyAt string                    `json:"lastUnhealthyAt,omitempty"`
	Drained         bool                      `json:"drained,omitempty"`
	DrainedUntil    string                    `json:"drainedUntil,omitempty"`
}

type previousBackendStatus struct {
	Address         string
	AddressFrom     api.StatusValueSourceSpec
	Port            int
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
	previousStatus, err := c.Store.ReadObjectStatus(api.FirewallAPIVersion, "IngressService", name)
	if err != nil {
		return err
	}
	listenAddress := statusvalue.Address(spec.Listen.Address)
	if listenAddress == "" && strings.TrimSpace(spec.Listen.AddressFrom.Resource) != "" {
		value, err := resourcequery.ReadValue(c.Store, spec.Listen.AddressFrom)
		if err != nil {
			return err
		}
		listenAddress = statusvalue.Address(value)
	}
	previous := previousBackends(previousStatus)
	var backends []backendStatus
	for i, backend := range spec.Backends {
		status := backendStatus{
			Name:        defaultString(backend.Name, fmt.Sprintf("backend-%d", i)),
			Address:     strings.TrimSpace(backend.Address),
			AddressFrom: backend.AddressFrom,
			Port:        backend.Port,
		}
		prior := previous[status.Name]
		if prior.Address != status.Address || prior.AddressFrom != status.AddressFrom || prior.Port != status.Port {
			prior = previousBackendStatus{}
		}
		resolved, reason, err := c.resolveAddress(ctx, backend, prior)
		if err != nil {
			return err
		}
		if prior.ResolvedAddress != resolved {
			prior = previousBackendStatus{}
		}
		status.ResolvedAddress = resolved
		checkOK := resolved != ""
		status.Reason = reason
		drain, drained, err := ingressdrain.Current(c.Store, name, status.Name)
		if err != nil {
			return err
		}
		if drained {
			status.applyDrained(prior, drain, now)
		} else if checkOK && !c.DryRun {
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
			status.applyCheckResult(checkOK, prior, spec.HealthCheck, now)
		} else {
			status.applyCheckResult(checkOK, prior, spec.HealthCheck, now)
		}
		backends = append(backends, status)
	}
	active, activeBackends, effectiveSelection, healthy := selectActiveBackends(backends, spec.Policy.Selection)
	phase := "NoHealthyBackends"
	if active.Name != "" {
		phase = "Active"
		if healthy < len(backends) {
			phase = "Degraded"
		}
	}
	status := map[string]any{
		"phase":              phase,
		"hostname":           strings.TrimSpace(spec.Hostname),
		"listenAddress":      listenAddress,
		"activeBackend":      map[string]any{"name": active.Name, "address": active.ResolvedAddress, "port": active.Port},
		"activeBackends":     activeBackendStatusMaps(activeBackends),
		"healthyBackends":    healthy,
		"totalBackends":      len(backends),
		"drainedBackends":    drainedBackendCount(backends),
		"backends":           backends,
		"selection":          defaultString(spec.Policy.Selection, "failover"),
		"effectiveSelection": effectiveSelection,
		"dryRun":             c.DryRun,
		"observedAt":         now,
	}
	if sameActiveBackend(previousStatus["activeBackend"], active) {
		if previousTransition := statusvalue.Text(previousStatus["lastActiveBackendTransitionAt"]); previousTransition != "" {
			status["lastActiveBackendTransitionAt"] = previousTransition
		}
	} else {
		status["previousActiveBackend"] = previousStatus["activeBackend"]
		status["lastActiveBackendTransitionAt"] = now
	}
	if active.Name == "" {
		status["conditions"] = []map[string]any{{"type": "BackendsHealthy", "status": "False", "reason": defaultString(spec.Policy.OnNoHealthyBackends, "drop")}}
	} else {
		status["conditions"] = []map[string]any{{"type": "BackendsHealthy", "status": "True", "reason": "ActiveBackendSelected"}}
	}
	return c.Store.SaveObjectStatus(api.FirewallAPIVersion, "IngressService", name, status)
}

func (s *backendStatus) applyDrained(previous previousBackendStatus, drain ingressdrain.State, now string) {
	s.Healthy = false
	s.Reason = "Drained"
	s.Drained = true
	s.DrainedUntil = drain.DrainedUntil
	s.HealthyCount = 0
	s.UnhealthyCount = previous.UnhealthyCount + 1
	s.LastHealthyAt = previous.LastHealthyAt
	s.LastUnhealthyAt = now
}

func drainedBackendCount(backends []backendStatus) int {
	var count int
	for _, backend := range backends {
		if backend.Drained {
			count++
		}
	}
	return count
}

func sameActiveBackend(previous any, active backendStatus) bool {
	item, ok := previous.(map[string]any)
	if !ok {
		return active.Name == "" && active.ResolvedAddress == "" && active.Port == 0
	}
	return statusvalue.Text(item["name"]) == active.Name && statusvalue.Text(item["address"]) == active.ResolvedAddress && statusInt(item["port"]) == active.Port
}

func (c *Controller) resolveAddress(ctx context.Context, backend api.IngressBackendSpec, previous previousBackendStatus) (string, string, error) {
	address := strings.TrimSpace(backend.Address)
	if address == "" && strings.TrimSpace(backend.AddressFrom.Resource) != "" {
		value, err := resourcequery.ReadValue(c.Store, backend.AddressFrom)
		if err != nil {
			return "", "", err
		}
		address = statusvalue.Address(value)
	}
	if address == "" {
		return "", "AddressFromPending", nil
	}
	if addr, err := netip.ParseAddr(address); err == nil && addr.Is4() {
		return addr.String(), "", nil
	}
	resolver := c.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	ips, err := resolver.LookupIPAddr(ctx, address)
	if err != nil {
		if previous.ResolvedAddress != "" {
			return previous.ResolvedAddress, "DNSFailedUsingPrevious: " + err.Error(), nil
		}
		return "", "DNSFailed: " + err.Error(), nil
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
			return previous.ResolvedAddress, "DNSNoIPv4UsingPrevious", nil
		}
		return "", "DNSNoIPv4", nil
	}
	return values[0], "", nil
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

func selectActiveBackends(backends []backendStatus, selection string) (backendStatus, []backendStatus, string, int) {
	healthy := 0
	var first backendStatus
	var selected []backendStatus
	for _, backend := range backends {
		if !backend.Healthy {
			continue
		}
		healthy++
		if first.Name == "" {
			first = backend
		}
		selected = append(selected, backend)
	}
	if healthy == 0 {
		return backendStatus{}, nil, defaultString(selection, "failover"), 0
	}
	requested := defaultString(selection, "failover")
	switch requested {
	case "sourceHash", "random":
		if len(selected) > 1 {
			return first, selected, requested, healthy
		}
		return first, []backendStatus{first}, "failover", healthy
	default:
		return first, []backendStatus{first}, "failover", healthy
	}
}

func activeBackendStatusMaps(backends []backendStatus) []map[string]any {
	out := make([]map[string]any, 0, len(backends))
	for _, backend := range backends {
		if backend.Name == "" || backend.ResolvedAddress == "" || backend.Port == 0 {
			continue
		}
		out = append(out, map[string]any{"name": backend.Name, "address": backend.ResolvedAddress, "port": backend.Port})
	}
	return out
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
					Address:         statusvalue.Text(item["address"]),
					AddressFrom:     previousBackendAddressFrom(item["addressFrom"]),
					Port:            statusInt(item["port"]),
					ResolvedAddress: statusvalue.Text(item["resolvedAddress"]),
					Healthy:         statusvalue.BoolOrFalse(item["healthy"]),
					HealthyCount:    statusInt(item["healthyCount"]),
					UnhealthyCount:  statusInt(item["unhealthyCount"]),
					LastHealthyAt:   statusvalue.Text(item["lastHealthyAt"]),
					LastUnhealthyAt: statusvalue.Text(item["lastUnhealthyAt"]),
				}
			}
		}
	case []backendStatus:
		for _, item := range values {
			if item.Name != "" {
				out[item.Name] = previousBackendStatus{
					Address:         item.Address,
					AddressFrom:     item.AddressFrom,
					Port:            item.Port,
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

func previousBackendAddressFrom(value any) api.StatusValueSourceSpec {
	switch source := value.(type) {
	case api.StatusValueSourceSpec:
		return source
	case map[string]any:
		return api.StatusValueSourceSpec{Resource: statusvalue.Text(source["resource"]), Field: statusvalue.Text(source["field"]), Optional: statusvalue.BoolOrFalse(source["optional"])}
	default:
		return api.StatusValueSourceSpec{}
	}
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
