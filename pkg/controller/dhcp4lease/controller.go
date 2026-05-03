package dhcp4lease

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
)

const (
	EventApplied = "routerd.dhcp4.lease.applied"
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

func (c Controller) Start(ctx context.Context) {
	if c.Router == nil || c.Bus == nil || c.Store == nil {
		return
	}
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.dhcp4.client.*"}}, 32)
	go func() {
		for event := range ch {
			if event.Resource == nil || event.Resource.Kind != "DHCPv4Lease" {
				continue
			}
			if err := c.reconcile(ctx, event.Resource.Name); err != nil && c.Logger != nil {
				c.Logger.Warn("dhcp4 lease reconcile failed", "resource", event.Resource.Name, "error", err)
			}
		}
	}()
}

func (c Controller) Reconcile(ctx context.Context, name string) error {
	return c.reconcile(ctx, name)
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
		if servers := parseJSONStringList(observed["dnsServers"]); len(servers) > 0 {
			next["dnsServers"] = servers
		}
		if err := c.Store.SaveObjectStatus(resource.Resource.APIVersion, resource.Resource.Kind, resource.Resource.Name, next); err != nil {
			return err
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

func (c Controller) socketFor(resource string) string {
	if socket := c.DaemonSockets[resource]; socket != "" {
		return socket
	}
	return filepath.Join("/run/routerd/dhcp4-client", resource+".sock")
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
	return status, json.NewDecoder(resp.Body).Decode(&status)
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
