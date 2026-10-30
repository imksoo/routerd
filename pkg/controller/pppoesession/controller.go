// SPDX-License-Identifier: BSD-3-Clause

package pppoesession

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/pppoeclient"
)

const EventApplied = "routerd.pppoe.session.applied"

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

// Source binds a status response to the currently eligible native worker.
// ConfigHash stays internal; only the opaque supervisor token crosses IPC.
type Source struct {
	Interface       string
	Device          string
	ConfigHash      string
	SupervisorOwner string
	Identity        pppoeclient.SessionIdentity
}

type SourceReader func(name string) (Source, bool, error)

type Controller struct {
	Router        *api.Router
	Bus           *bus.Bus
	Store         Store
	DaemonSockets map[string]string
	DryRun        bool
	Logger        *slog.Logger
	Source        SourceReader
}

func (c Controller) Start(ctx context.Context) {
	if c.Router == nil || c.Bus == nil || c.Store == nil {
		return
	}
	ch, _ := c.Bus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.pppoe.client.*"}}, 32)
	go func() {
		for event := range ch {
			if event.Resource == nil || event.Resource.Kind != "PPPoESession" {
				continue
			}
			if err := c.Reconcile(ctx, event.Resource.Name); err != nil && c.Logger != nil {
				c.Logger.Warn("pppoe session reconcile failed", "resource", event.Resource.Name, "error", err)
			}
		}
	}()
}

func (c Controller) Reconcile(ctx context.Context, name string) error {
	if c.Source == nil {
		return fmt.Errorf("PPPoE source observation is required")
	}
	source, ready, err := c.Source(name)
	if err != nil {
		return err
	}
	if !ready {
		return c.saveSourceUnavailable(name)
	}
	status, err := daemonStatus(ctx, c.socketFor(name))
	if err != nil {
		return err
	}
	return c.applyDaemonStatus(ctx, name, source, status)
}

func (c Controller) applyDaemonStatus(ctx context.Context, name string, source Source, status daemonapi.DaemonStatus) error {
	for _, resource := range status.Resources {
		if resource.Resource.APIVersion != api.NetAPIVersion || resource.Resource.Kind != "PPPoESession" || resource.Resource.Name != name {
			continue
		}
		observed := resource.Observed
		currentSource, ready, err := c.Source(name)
		if err != nil {
			return err
		}
		if !ready || currentSource != source || resource.PPPoEIdentity == nil || *resource.PPPoEIdentity != source.Identity || observed["interface"] != source.Interface || observed["ifname"] != source.Device || source.SupervisorOwner != "" && observed["supervisorOwner"] != source.SupervisorOwner {
			return c.saveSourceUnavailable(name)
		}
		next := map[string]any{
			"sourceIdentity": source.Identity,
			"phase":          resource.Phase,
			"health":         resource.Health,
			"conditions":     resource.Conditions,
			"observed":       observed,
			"interface":      observed["interface"],
			"device":         observed["ifname"],
			"currentAddress": observed["currentAddress"],
			"peerAddress":    observed["peerAddress"],
			"gateway":        observed["peerAddress"],
			"connectedAt":    observed["connectedAt"],
			"dryRun":         c.DryRun,
		}
		if servers := parseJSONStringList(observed["dnsServers"]); len(servers) > 0 {
			next["dnsServers"] = servers
		}
		if bytesIn, err := strconv.ParseUint(observed["bytesIn"], 10, 64); err == nil {
			next["bytesIn"] = bytesIn
		}
		if bytesOut, err := strconv.ParseUint(observed["bytesOut"], 10, 64); err == nil {
			next["bytesOut"] = bytesOut
		}
		current := c.Store.ObjectStatus(resource.Resource.APIVersion, resource.Resource.Kind, resource.Resource.Name)
		changed := sessionEventChanged(current, next)
		if err := c.Store.SaveObjectStatus(resource.Resource.APIVersion, resource.Resource.Kind, resource.Resource.Name, next); err != nil {
			return err
		}
		if !changed || c.Bus == nil {
			return nil
		}
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, EventApplied, daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "PPPoESession", Name: name}
		event.Attributes = map[string]string{
			"device":         observed["ifname"],
			"currentAddress": observed["currentAddress"],
			"peerAddress":    observed["peerAddress"],
			"dryRun":         fmt.Sprintf("%t", c.DryRun),
		}
		return c.Bus.Publish(ctx, event)
	}
	return fmt.Errorf("daemon status did not include PPPoESession/%s", name)
}

func (c Controller) saveSourceUnavailable(name string) error {
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "PPPoESession", name, map[string]any{
		"phase": "Pending", "reason": "DaemonSourceUnavailable", "health": daemonapi.HealthUnknown,
		"currentAddress": "", "peerAddress": "", "gateway": "", "dnsServers": []string{}, "device": "", "observed": map[string]string{}, "dryRun": c.DryRun,
	})
}

func sessionEventChanged(current, next map[string]any) bool {
	for _, key := range []string{"phase", "device", "currentAddress", "peerAddress", "gateway", "connectedAt"} {
		if fmt.Sprint(current[key]) != fmt.Sprint(next[key]) {
			return true
		}
	}
	if fmt.Sprint(current["dnsServers"]) != fmt.Sprint(next["dnsServers"]) {
		return true
	}
	return false
}

func (c Controller) socketFor(resource string) string {
	if socket := c.DaemonSockets[resource]; socket != "" {
		return socket
	}
	defaults, _ := platform.Current()
	return filepath.Join(defaults.RuntimeDir, "pppoe-client", resource+".sock")
}

func daemonStatus(ctx context.Context, socketPath string) (daemonapi.DaemonStatus, error) {
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
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
	return decodeDaemonStatus(resp)
}

func decodeDaemonStatus(resp *http.Response) (daemonapi.DaemonStatus, error) {
	if resp.StatusCode != http.StatusOK {
		return daemonapi.DaemonStatus{}, fmt.Errorf("PPPoE status endpoint HTTP %d", resp.StatusCode)
	}
	var status daemonapi.DaemonStatus
	err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&status)
	return status, err
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
