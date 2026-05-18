// SPDX-License-Identifier: BSD-3-Clause

package ingressservice

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
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
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	DryRun bool
	Check  CheckFunc
	Logger *slog.Logger
}

type backendStatus struct {
	Name            string `json:"name"`
	Address         string `json:"address"`
	ResolvedAddress string `json:"resolvedAddress,omitempty"`
	Port            int    `json:"port"`
	Healthy         bool   `json:"healthy"`
	Reason          string `json:"reason,omitempty"`
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
		status.Healthy = resolved != ""
		status.Reason = reason
		if status.Healthy && defaultString(spec.HealthCheck.Protocol, "tcp") == "tcp" && !c.DryRun {
			if err := c.check(ctx, resolved, backend.Port, timeout); err != nil {
				status.Healthy = false
				status.Reason = "TCPCheckFailed: " + err.Error()
			}
		}
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
		"observedAt":      time.Now().UTC().Format(time.RFC3339Nano),
	}
	if active.Name == "" {
		status["conditions"] = []map[string]any{{"type": "BackendsHealthy", "status": "False", "reason": defaultString(spec.Policy.OnNoHealthyBackends, "drop")}}
	} else {
		status["conditions"] = []map[string]any{{"type": "BackendsHealthy", "status": "True", "reason": "ActiveBackendSelected"}}
	}
	return c.Store.SaveObjectStatus(api.FirewallAPIVersion, "IngressService", name, status)
}

func (c *Controller) resolveAddress(ctx context.Context, backend api.IngressBackendSpec, previous string) (string, string) {
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
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, address)
	if err != nil {
		if previous != "" {
			return previous, "DNSFailedUsingPrevious: " + err.Error()
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
		if previous != "" {
			return previous, "DNSNoIPv4UsingPrevious"
		}
		return "", "DNSNoIPv4"
	}
	return values[0], ""
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

func previousBackends(status map[string]any) map[string]string {
	out := map[string]string{}
	switch values := status["backends"].(type) {
	case []any:
		for _, value := range values {
			item, _ := value.(map[string]any)
			name := strings.TrimSpace(fmt.Sprint(item["name"]))
			address := strings.TrimSpace(fmt.Sprint(item["resolvedAddress"]))
			if name != "" && address != "" {
				out[name] = address
			}
		}
	case []backendStatus:
		for _, item := range values {
			if item.Name != "" && item.ResolvedAddress != "" {
				out[item.Name] = item.ResolvedAddress
			}
		}
	}
	return out
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
