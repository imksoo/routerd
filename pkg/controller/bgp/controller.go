// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"routerd/pkg/api"
	bgpstate "routerd/pkg/bgp"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/render"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type CommandFunc func(context.Context, string, ...string) ([]byte, error)

const MinPollInterval = 3 * time.Second

type Controller struct {
	Router      *api.Router
	Bus         *bus.Bus
	Store       Store
	DryRun      bool
	ConfigPath  string
	VTYSH       string
	FRRReload   string
	MaxPrefixes int
	Command     CommandFunc
	Logger      *slog.Logger
	lastState   bgpstate.State
	observed    bool
	truncated   bool
}

func (c *Controller) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil || !hasBGP(c.Router) {
		return nil
	}
	data, err := render.FRRConfig(c.Router)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	path := firstNonEmpty(c.ConfigPath, "/run/routerd/frr/routerd.conf")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	changed := true
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		changed = false
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if changed {
		if err := os.WriteFile(path, data, 0644); err != nil {
			return err
		}
	}
	if !c.DryRun && changed {
		vtysh := firstNonEmpty(c.VTYSH, "vtysh")
		if out, err := c.run(ctx, vtysh, "-C", "-f", path); err != nil {
			saveErr := c.saveConfiguredStatuses("Error", path, changed, map[string]any{"reason": "FRRSyntaxInvalid", "error": strings.TrimSpace(string(out))})
			if saveErr != nil {
				return saveErr
			}
			return fmt.Errorf("%s -C -f %s: %w: %s", vtysh, path, err, strings.TrimSpace(string(out)))
		}
		reload := firstNonEmpty(c.FRRReload, defaultFRRReload())
		if out, err := c.run(ctx, reload, "--reload", path); err != nil {
			saveErr := c.saveConfiguredStatuses("Error", path, changed, map[string]any{"reason": "FRRReloadFailed", "error": strings.TrimSpace(string(out))})
			if saveErr != nil {
				return saveErr
			}
			return fmt.Errorf("%s --reload %s: %w: %s", reload, path, err, strings.TrimSpace(string(out)))
		}
	}
	if err := c.saveConfiguredStatuses("Applied", path, changed, nil); err != nil {
		return err
	}
	if !c.DryRun {
		return c.observe(ctx)
	}
	return nil
}

func (c *Controller) observe(ctx context.Context) error {
	vtysh := firstNonEmpty(c.VTYSH, "vtysh")
	summary, summaryErr := c.run(ctx, vtysh, "-c", "show bgp summary json")
	routes, routesErr := c.run(ctx, vtysh, "-c", "show bgp ipv4 unicast json")
	if summaryErr != nil || routesErr != nil {
		errText := strings.TrimSpace(fmt.Sprintf("%v %v", summaryErr, routesErr))
		return c.saveConfiguredStatuses("Pending", firstNonEmpty(c.ConfigPath, "/run/routerd/frr/routerd.conf"), false, map[string]any{"reason": "FRRStatusUnavailable", "error": errText})
	}
	state, err := bgpstate.ParseFRRState(summary, routes)
	if err != nil {
		return c.saveConfiguredStatuses("Pending", firstNonEmpty(c.ConfigPath, "/run/routerd/frr/routerd.conf"), false, map[string]any{"reason": "FRRStatusParseFailed", "error": err.Error()})
	}
	state, c.truncated = bgpstate.LimitPrefixes(state, defaultInt(c.MaxPrefixes, bgpstate.DefaultMaxPrefixes))
	var events []bgpstate.Event
	if c.observed {
		events = bgpstate.Diff(c.lastState, state)
	}
	c.lastState = state
	c.observed = true
	if err := c.saveObservedStatuses(state); err != nil {
		return err
	}
	for _, event := range events {
		if c.Bus == nil {
			continue
		}
		daemonEvent := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "frr", Kind: "frr", Instance: "bgp"}, "routerd.bgp."+strings.ReplaceAll(event.Type, " ", "."), daemonapi.SeverityInfo)
		daemonEvent.Attributes = map[string]string{
			"peer":     event.Peer,
			"prefix":   event.Prefix,
			"previous": event.Previous,
			"current":  event.Current,
		}
		_ = c.Bus.Publish(ctx, daemonEvent)
	}
	return nil
}

func (c *Controller) saveConfiguredStatuses(phase, path string, changed bool, extra map[string]any) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || (resource.Kind != "BGPRouter" && resource.Kind != "BGPPeer") {
			continue
		}
		status := map[string]any{
			"phase":      phase,
			"backend":    "frr",
			"configPath": path,
			"applyWith":  "frr-reload.py --reload",
			"changed":    changed,
			"dryRun":     c.DryRun,
			"observedAt": now,
			"conditions": []map[string]any{{"type": "Configured", "status": "True", "reason": "FRRRendered"}},
		}
		for key, value := range extra {
			status[key] = value
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, resource.Kind, resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) saveObservedStatuses(state bgpstate.State) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	peersByResource := c.peersByResource(state)
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion {
			continue
		}
		switch resource.Kind {
		case "BGPRouter":
			established := 0
			for _, peer := range state.Peers {
				if peer.Established {
					established++
				}
			}
			phase := "Degraded"
			if len(state.Peers) > 0 && established == len(state.Peers) {
				phase = "Established"
			}
			if len(state.Peers) == 0 {
				phase = "Pending"
			}
			status := map[string]any{
				"phase":             phase,
				"backend":           "frr",
				"peers":             state.Peers,
				"prefixes":          state.Prefixes,
				"establishedPeers":  established,
				"acceptedPrefixes":  len(state.Prefixes),
				"prefixLimit":       defaultInt(c.MaxPrefixes, bgpstate.DefaultMaxPrefixes),
				"prefixesTruncated": c.truncated,
				"observedAt":        now,
				"conditions":        []map[string]any{{"type": "Observed", "status": "True", "reason": "FRRStatus"}},
			}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", resource.Metadata.Name, status); err != nil {
				return err
			}
		case "BGPPeer":
			peers := peersByResource[resource.Metadata.Name]
			established := 0
			for _, peer := range peers {
				if peer.Established {
					established++
				}
			}
			phase := "Pending"
			if len(peers) > 0 && established == len(peers) {
				phase = "Established"
			} else if established > 0 {
				phase = "Degraded"
			} else if len(peers) > 0 {
				phase = "Down"
			}
			status := map[string]any{
				"phase":            phase,
				"backend":          "frr",
				"peers":            peers,
				"establishedPeers": established,
				"observedAt":       now,
			}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "BGPPeer", resource.Metadata.Name, status); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) peersByResource(state bgpstate.State) map[string][]bgpstate.Peer {
	byAddress := map[string]bgpstate.Peer{}
	for _, peer := range state.Peers {
		byAddress[peer.Address] = peer
	}
	out := map[string][]bgpstate.Peer{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPPeer" {
			continue
		}
		spec, err := resource.BGPPeerSpec()
		if err != nil {
			continue
		}
		for _, peerAddress := range spec.Peers {
			peer, ok := byAddress[peerAddress]
			if !ok {
				peer = bgpstate.Peer{Address: peerAddress, ASN: spec.PeerASN, State: "Missing"}
			}
			out[resource.Metadata.Name] = append(out[resource.Metadata.Name], peer)
		}
	}
	return out
}

func hasBGP(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.NetAPIVersion && (resource.Kind == "BGPRouter" || resource.Kind == "BGPPeer") {
			return true
		}
	}
	return false
}

func (c *Controller) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if c.Command != nil {
		return c.Command(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func defaultFRRReload() string {
	if _, err := exec.LookPath("frr-reload.py"); err == nil {
		return "frr-reload.py"
	}
	for _, path := range []string{"/usr/lib/frr/frr-reload.py", "/usr/libexec/frr/frr-reload.py"} {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path
		}
	}
	return "frr-reload.py"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}
