// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"routerd/pkg/api"
	bgpstate "routerd/pkg/bgp"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/platform"
	"routerd/pkg/render"
	"routerd/pkg/servicemgr"
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
	DaemonsPath string
	VTYSH       string
	FRRReload   string
	Systemctl   string
	MaxPrefixes int
	Command     CommandFunc
	Logger      *slog.Logger
	lastState   bgpstate.State
	observed    bool
	truncated   bool
	peerEvents  map[string]time.Time
	applyMeta   map[string]any
}

func (c *Controller) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil || !hasBGP(c.Router) {
		return nil
	}
	c.applyMeta = nil
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
	if changed && !c.DryRun {
		if err := os.WriteFile(path, data, 0644); err != nil {
			return err
		}
	}
	daemonsChanged, daemonsPath, err := c.renderFRRDaemons()
	if err != nil {
		return err
	}
	reloadNeeded := changed
	if !reloadNeeded && !c.DryRun && !c.observed {
		matches, err := c.runningConfigMatches(ctx, data)
		if err != nil {
			if c.Logger != nil {
				c.Logger.Warn("BGP running config comparison failed; reloading FRR", "error", err)
			}
			reloadNeeded = true
		} else {
			reloadNeeded = !matches
		}
	}
	if daemonsChanged {
		reloadNeeded = true
	}
	if !c.DryRun && daemonsChanged {
		for _, command := range c.frrDaemonChangeCommands() {
			if command.Name == "" {
				continue
			}
			if out, err := c.run(ctx, command.Name, command.Args...); err != nil {
				saveErr := c.saveConfiguredStatuses("Error", path, true, map[string]any{"reason": "FRRServiceEnableRestartFailed", "error": strings.TrimSpace(string(out)), "daemonsPath": daemonsPath, "daemonsChanged": daemonsChanged})
				if saveErr != nil {
					return saveErr
				}
				return fmt.Errorf("%s %s: %w: %s", command.Name, strings.Join(command.Args, " "), err, strings.TrimSpace(string(out)))
			}
		}
		readyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		out, err := c.waitFRRReady(readyCtx)
		cancel()
		if err != nil {
			saveErr := c.saveConfiguredStatuses("Error", path, true, map[string]any{"reason": "FRRNotReady", "error": strings.TrimSpace(string(out)), "daemonsPath": daemonsPath, "daemonsChanged": daemonsChanged})
			if saveErr != nil {
				return saveErr
			}
			return fmt.Errorf("wait for bgpd readiness: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	if !c.DryRun && reloadNeeded {
		reloadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		out, err := c.validateFRRConfig(reloadCtx, path, daemonsChanged)
		if err != nil {
			vtysh := firstNonEmpty(c.VTYSH, "vtysh")
			saveErr := c.saveConfiguredStatuses("Error", path, reloadNeeded, map[string]any{"reason": "FRRSyntaxInvalid", "error": strings.TrimSpace(string(out))})
			if saveErr != nil {
				return saveErr
			}
			return fmt.Errorf("%s -C -f %s: %w: %s", vtysh, path, err, strings.TrimSpace(string(out)))
		}
		reload := firstNonEmpty(c.FRRReload, defaultFRRReload())
		if out, err := c.run(reloadCtx, reload, "--reload", path); err != nil {
			saveErr := c.saveConfiguredStatuses("Error", path, reloadNeeded, map[string]any{"reason": "FRRReloadFailed", "error": strings.TrimSpace(string(out))})
			if saveErr != nil {
				return saveErr
			}
			return fmt.Errorf("%s --reload %s: %w: %s", reload, path, err, strings.TrimSpace(string(out)))
		}
	}
	if !c.DryRun {
		if reloadNeeded || daemonsChanged {
			extra := map[string]any{"daemonsPath": daemonsPath, "daemonsChanged": daemonsChanged}
			if err := c.saveConfiguredStatuses("Applied", path, reloadNeeded || daemonsChanged, extra); err != nil {
				return err
			}
			c.applyMeta = map[string]any{
				"configPath":      path,
				"applyWith":       "frr-reload.py --reload",
				"changed":         reloadNeeded || daemonsChanged,
				"dryRun":          c.DryRun,
				"daemonsPath":     daemonsPath,
				"daemonsChanged":  daemonsChanged,
				"configuredPhase": "Applied",
			}
		}
		return c.observe(ctx)
	}
	if err := c.saveConfiguredStatuses("Applied", path, changed, nil); err != nil {
		return err
	}
	return nil
}

func (c *Controller) renderFRRDaemons() (bool, string, error) {
	path := firstNonEmpty(c.DaemonsPath, "/etc/frr/daemons")
	var existing []byte
	if current, err := os.ReadFile(path); err == nil {
		existing = current
	} else if err != nil && !os.IsNotExist(err) {
		return false, path, err
	}
	data, err := render.FRRDaemons(existing, c.Router)
	if err != nil || len(data) == 0 {
		return false, path, err
	}
	changed := !bytes.Equal(existing, data)
	if changed && !c.DryRun {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return false, path, err
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			return false, path, err
		}
	}
	return changed, path, nil
}

func (c *Controller) waitFRRReady(ctx context.Context) ([]byte, error) {
	vtysh := firstNonEmpty(c.VTYSH, "vtysh")
	var lastOut []byte
	for {
		out, err := c.run(ctx, vtysh, "-c", "show bgp summary json")
		if err == nil {
			return out, nil
		}
		lastOut = out
		select {
		case <-ctx.Done():
			return lastOut, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (c *Controller) validateFRRConfig(ctx context.Context, path string, retry bool) ([]byte, error) {
	vtysh := firstNonEmpty(c.VTYSH, "vtysh")
	attempts := 1
	if retry {
		attempts = 25
	}
	var lastOut []byte
	var lastErr error
	for i := 0; i < attempts; i++ {
		out, err := c.run(ctx, vtysh, "-C", "-f", path)
		if err == nil {
			return out, nil
		}
		lastOut, lastErr = out, err
		if !retry {
			break
		}
		select {
		case <-ctx.Done():
			return lastOut, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return lastOut, lastErr
}

func (c *Controller) frrDaemonChangeCommands() []servicemgr.Command {
	frr := servicemgr.Service{SystemdName: "frr.service", OpenRCName: "frr", RCDName: "frr", NixName: "frr"}
	manager := c.serviceManager()
	return []servicemgr.Command{
		manager.Command(servicemgr.OperationEnable, frr),
		manager.Command(servicemgr.OperationRestart, frr),
	}
}

func (c *Controller) serviceManager() servicemgr.Manager {
	switch strings.TrimSpace(c.Systemctl) {
	case "rc-service", "rc-update":
		return servicemgr.OpenRC{}
	case "service", "sysrc":
		return servicemgr.RCD{}
	case "nixos-rebuild":
		return servicemgr.NixOS{}
	case "systemctl":
		return servicemgr.Systemd{}
	}
	_, features := platform.Current()
	return servicemgr.ForPlatform(features)
}

func (c *Controller) runningConfigMatches(ctx context.Context, desired []byte) (bool, error) {
	vtysh := firstNonEmpty(c.VTYSH, "vtysh")
	out, err := c.run(ctx, vtysh, "-c", "show running-config")
	if err != nil {
		return false, fmt.Errorf("%s -c show running-config: %w: %s", vtysh, err, strings.TrimSpace(string(out)))
	}
	running := string(out)
	for _, line := range criticalFRRLines(desired) {
		if !strings.Contains(running, line) {
			return false, nil
		}
	}
	return true, nil
}

func criticalFRRLines(data []byte) []string {
	var lines []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "frr ") || strings.HasPrefix(line, "hostname ") || strings.HasPrefix(line, "service ") {
			continue
		}
		if line == "address-family ipv4 unicast" || line == "exit-address-family" {
			continue
		}
		if line == "bgp graceful-restart restart-time 120" || line == "bgp graceful-restart stalepath-time 360" {
			continue
		}
		if strings.HasSuffix(line, " activate") {
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		lines = append(lines, line)
	}
	return lines
}

func (c *Controller) observe(ctx context.Context) error {
	vtysh := firstNonEmpty(c.VTYSH, "vtysh")
	states, truncated, err := c.observeInstances(ctx, vtysh)
	if err != nil {
		return c.saveConfiguredStatuses("Pending", firstNonEmpty(c.ConfigPath, "/run/routerd/frr/routerd.conf"), false, map[string]any{"reason": "FRRStatusUnavailable", "error": err.Error()})
	}
	c.truncated = truncated
	state := aggregateBGPStates(states)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	state.Peers = c.applyPeerHistory(state.Peers, now)
	var events []bgpstate.Event
	if c.observed {
		events = bgpstate.Diff(c.lastState, state)
	}
	c.lastState = state
	c.observed = true
	if err := c.saveObservedStatuses(states, state); err != nil {
		return err
	}
	for _, event := range events {
		c.publishBGPEvent(ctx, event)
	}
	return nil
}

func (c *Controller) publishBGPEvent(ctx context.Context, event bgpstate.Event) {
	if c.throttleBGPEvent(event) || c.Bus == nil {
		return
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

func (c *Controller) throttleBGPEvent(event bgpstate.Event) bool {
	if event.Peer == "" || (event.Type != bgpstate.EventPeerUp && event.Type != bgpstate.EventPeerDown) {
		return false
	}
	window := c.peerStateChangeThrottle()
	if window <= 0 {
		return false
	}
	if c.peerEvents == nil {
		c.peerEvents = map[string]time.Time{}
	}
	key := event.Type + "|" + event.Peer
	now := time.Now()
	if previous, ok := c.peerEvents[key]; ok && now.Sub(previous) < window {
		return true
	}
	c.peerEvents[key] = now
	return false
}

func (c *Controller) observeInstances(ctx context.Context, vtysh string) (map[string]bgpstate.State, bool, error) {
	states := map[string]bgpstate.State{}
	truncatedAny := false
	for _, instance := range c.bgpInstances() {
		summaryCmd, routesCmd := bgpShowCommands(instance.VRFName)
		summary, summaryErr := c.run(ctx, vtysh, "-c", summaryCmd)
		routes, routesErr := c.run(ctx, vtysh, "-c", routesCmd)
		if summaryErr != nil || routesErr != nil {
			errText := strings.TrimSpace(fmt.Sprintf("%s: %v %v", instance.Name, summaryErr, routesErr))
			return nil, false, fmt.Errorf("%s", errText)
		}
		state, err := bgpstate.ParseFRRState(summary, routes)
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", instance.Name, err)
		}
		if instance.IPv6 {
			routesV6, err := c.run(ctx, vtysh, "-c", bgpShowIPv6RoutesCommand(instance.VRFName))
			if err != nil {
				return nil, false, fmt.Errorf("%s ipv6: %w", instance.Name, err)
			}
			prefixesV6, err := bgpstate.ParseFRRRoutesJSON(routesV6)
			if err != nil {
				return nil, false, fmt.Errorf("%s ipv6: %w", instance.Name, err)
			}
			state.Prefixes = append(state.Prefixes, prefixesV6...)
			state = bgpstate.Normalize(state)
		}
		if instance.BFD {
			bfd, err := c.observeBFD(ctx, vtysh, instance.VRFName)
			if err != nil {
				return nil, false, fmt.Errorf("%s bfd: %w", instance.Name, err)
			}
			state = bgpstate.AttachBFD(state, bfd)
		}
		limited, truncated := bgpstate.LimitPrefixes(state, c.maxPrefixesForRouter(instance.Name))
		if truncated {
			truncatedAny = true
		}
		states[instance.Name] = limited
	}
	return states, truncatedAny, nil
}

func (c *Controller) observeBFD(ctx context.Context, vtysh, vrfName string) (map[string]bgpstate.BFD, error) {
	cmd := "show bfd peers brief json"
	if strings.TrimSpace(vrfName) != "" {
		cmd = "show bfd vrf " + strings.TrimSpace(vrfName) + " peers brief json"
	}
	out, err := c.run(ctx, vtysh, "-c", cmd)
	if err != nil {
		return nil, err
	}
	return bgpstate.ParseFRRBFDPeersJSON(out)
}

type bgpInstance struct {
	Name    string
	VRFName string
	BFD     bool
	IPv6    bool
}

func (c *Controller) bgpInstances() []bgpInstance {
	vrfs := controllerBGPVRFIfNames(c.Router)
	var out []bgpInstance
	if c.Router == nil {
		return out
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" {
			continue
		}
		spec, err := resource.BGPRouterSpec()
		if err != nil {
			continue
		}
		ref := strings.TrimSpace(spec.VRF)
		out = append(out, bgpInstance{Name: resource.Metadata.Name, VRFName: vrfs[controllerBGPVRFRefName(ref)], BFD: c.bgpRouterUsesBFD(resource.Metadata.Name), IPv6: c.bgpRouterUsesIPv6(resource.Metadata.Name, spec)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (c *Controller) bgpRouterUsesIPv6(routerName string, spec api.BGPRouterSpec) bool {
	prefixes := append([]string{}, spec.ImportPolicy.AllowedPrefixes...)
	prefixes = append(prefixes, spec.ExportPolicy.AllowedPrefixes...)
	prefixes = append(prefixes, spec.Redistribute.Connected.AllowedPrefixes...)
	prefixes = append(prefixes, spec.Redistribute.Static.AllowedPrefixes...)
	for _, prefix := range prefixes {
		if parsed, err := netip.ParsePrefix(strings.TrimSpace(prefix)); err == nil && parsed.Addr().Is6() {
			return true
		}
	}
	if c.Router == nil {
		return false
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPPeer" {
			continue
		}
		peerSpec, err := resource.BGPPeerSpec()
		if err != nil {
			continue
		}
		_, name, ok := strings.Cut(strings.TrimSpace(peerSpec.RouterRef), "/")
		if !ok || name != routerName {
			continue
		}
		for _, prefix := range peerSpec.ExportPolicy.AllowedPrefixes {
			if parsed, err := netip.ParsePrefix(strings.TrimSpace(prefix)); err == nil && parsed.Addr().Is6() {
				return true
			}
		}
		for _, peer := range peerSpec.Peers {
			if addr, err := netip.ParseAddr(strings.TrimSpace(peer)); err == nil && addr.Is6() {
				return true
			}
		}
	}
	return false
}

func (c *Controller) bgpRouterUsesBFD(routerName string) bool {
	if c.Router == nil {
		return false
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPPeer" {
			continue
		}
		spec, err := resource.BGPPeerSpec()
		if err != nil || !(spec.BFD.Enabled != nil && *spec.BFD.Enabled) {
			continue
		}
		_, name, ok := strings.Cut(strings.TrimSpace(spec.RouterRef), "/")
		if ok && name == routerName {
			return true
		}
	}
	return false
}

func bgpShowCommands(vrfName string) (string, string) {
	if strings.TrimSpace(vrfName) == "" {
		return "show bgp summary json", "show bgp ipv4 unicast json"
	}
	vrf := strings.TrimSpace(vrfName)
	return "show bgp vrf " + vrf + " summary json", "show bgp vrf " + vrf + " ipv4 unicast json"
}

func bgpShowIPv6RoutesCommand(vrfName string) string {
	if strings.TrimSpace(vrfName) == "" {
		return "show bgp ipv6 unicast json"
	}
	return "show bgp vrf " + strings.TrimSpace(vrfName) + " ipv6 unicast json"
}

func aggregateBGPStates(states map[string]bgpstate.State) bgpstate.State {
	var aggregate bgpstate.State
	for _, state := range states {
		aggregate.Peers = append(aggregate.Peers, state.Peers...)
		aggregate.Prefixes = append(aggregate.Prefixes, state.Prefixes...)
	}
	return bgpstate.Normalize(aggregate)
}

func controllerBGPVRFIfNames(router *api.Router) map[string]string {
	out := map[string]string{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "VRF" {
			continue
		}
		spec, err := resource.VRFSpec()
		if err != nil {
			continue
		}
		out[resource.Metadata.Name] = firstNonEmpty(spec.IfName, resource.Metadata.Name)
	}
	return out
}

func controllerBGPVRFRefName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if kind, name, ok := strings.Cut(value, "/"); ok && kind == "VRF" {
		return strings.TrimSpace(name)
	}
	return value
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

func (c *Controller) saveObservedStatuses(states map[string]bgpstate.State, aggregate bgpstate.State) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	peersByResource := c.peersByResource(aggregate)
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion {
			continue
		}
		switch resource.Kind {
		case "BGPRouter":
			state := c.routerState(resource.Metadata.Name, states, peersByResource)
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
				"phase":               phase,
				"backend":             "frr",
				"peers":               state.Peers,
				"prefixes":            state.Prefixes,
				"vrf":                 c.bgpInstanceVRFName(resource.Metadata.Name),
				"observedCommunities": observedCommunities(state.Prefixes),
				"establishedPeers":    established,
				"acceptedPrefixes":    len(state.Prefixes),
				"prefixLimit":         c.maxPrefixesForRouter(resource.Metadata.Name),
				"prefixesTruncated":   c.truncated,
				"observedAt":          now,
				"conditions":          []map[string]any{{"type": "Observed", "status": "True", "reason": "FRRStatus"}},
			}
			for key, value := range c.applyMeta {
				status[key] = value
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
			for key, value := range c.applyMeta {
				status[key] = value
			}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "BGPPeer", resource.Metadata.Name, status); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) routerState(routerName string, states map[string]bgpstate.State, peersByResource map[string][]bgpstate.Peer) bgpstate.State {
	state := states[routerName]
	wanted := c.peerResourceNamesForRouter(routerName)
	var peers []bgpstate.Peer
	for _, peerResource := range wanted {
		peers = append(peers, peersByResource[peerResource]...)
	}
	if len(peers) > 0 {
		state.Peers = peers
	}
	return bgpstate.Normalize(state)
}

func (c *Controller) peerResourceNamesForRouter(routerName string) []string {
	var out []string
	if c.Router == nil {
		return out
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPPeer" {
			continue
		}
		spec, err := resource.BGPPeerSpec()
		if err != nil {
			continue
		}
		_, name, ok := strings.Cut(strings.TrimSpace(spec.RouterRef), "/")
		if ok && name == routerName {
			out = append(out, resource.Metadata.Name)
		}
	}
	sort.Strings(out)
	return out
}

func (c *Controller) bgpInstanceVRFName(routerName string) string {
	for _, instance := range c.bgpInstances() {
		if instance.Name == routerName {
			return instance.VRFName
		}
	}
	return ""
}

func (c *Controller) maxPrefixesForRouter(routerName string) int {
	if c.MaxPrefixes > 0 {
		return c.MaxPrefixes
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" || resource.Metadata.Name != routerName {
			continue
		}
		spec, err := resource.BGPRouterSpec()
		if err == nil && spec.Watcher.MaxPrefixes > 0 {
			return spec.Watcher.MaxPrefixes
		}
	}
	return bgpstate.DefaultMaxPrefixes
}

func (c *Controller) peerStateChangeThrottle() time.Duration {
	var out time.Duration
	if c.Router == nil {
		return 0
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" {
			continue
		}
		spec, err := resource.BGPRouterSpec()
		if err != nil || strings.TrimSpace(spec.Watcher.PeerStateChangeThrottle) == "" {
			continue
		}
		duration, err := time.ParseDuration(spec.Watcher.PeerStateChangeThrottle)
		if err != nil || duration <= 0 {
			continue
		}
		if out == 0 || duration < out {
			out = duration
		}
	}
	return out
}

func PollInterval(router *api.Router) time.Duration {
	out := 15 * time.Second
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" {
			continue
		}
		spec, err := resource.BGPRouterSpec()
		if err != nil || strings.TrimSpace(spec.Watcher.PollInterval) == "" {
			continue
		}
		duration, err := time.ParseDuration(spec.Watcher.PollInterval)
		if err != nil || duration < MinPollInterval {
			continue
		}
		if duration < out {
			out = duration
		}
	}
	return out
}

func observedCommunities(prefixes []bgpstate.Prefix) []string {
	seen := map[string]bool{}
	var out []string
	for _, prefix := range prefixes {
		for _, community := range prefix.Communities {
			community = strings.TrimSpace(community)
			if community == "" || seen[community] {
				continue
			}
			seen[community] = true
			out = append(out, community)
		}
	}
	sort.Strings(out)
	return out
}

func (c *Controller) applyPeerHistory(peers []bgpstate.Peer, now string) []bgpstate.Peer {
	previous := c.previousPeers()
	out := append([]bgpstate.Peer(nil), peers...)
	for i, peer := range out {
		prev := previous[peer.Address]
		if peer.Established {
			if peer.LastEstablishedAt == "" {
				if prev.Established && prev.LastEstablishedAt != "" {
					peer.LastEstablishedAt = prev.LastEstablishedAt
				} else {
					peer.LastEstablishedAt = now
				}
			}
			if peer.LastErrorAt == "" {
				peer.LastErrorAt = prev.LastErrorAt
			}
			if peer.LastErrorReason == "" {
				peer.LastErrorReason = prev.LastErrorReason
			}
		} else {
			if peer.LastEstablishedAt == "" {
				peer.LastEstablishedAt = prev.LastEstablishedAt
			}
			reason := firstNonEmpty(peer.LastErrorReason, peer.State, "NotEstablished")
			peer.LastErrorReason = reason
			if peer.LastErrorAt == "" {
				if prev.LastErrorReason == reason && prev.LastErrorAt != "" {
					peer.LastErrorAt = prev.LastErrorAt
				} else {
					peer.LastErrorAt = now
				}
			}
		}
		out[i] = peer
	}
	return out
}

func (c *Controller) previousPeers() map[string]bgpstate.Peer {
	out := map[string]bgpstate.Peer{}
	if c.Store == nil || c.Router == nil {
		return out
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || (resource.Kind != "BGPRouter" && resource.Kind != "BGPPeer") {
			continue
		}
		for _, peer := range peersFromStatus(c.Store.ObjectStatus(api.NetAPIVersion, resource.Kind, resource.Metadata.Name)["peers"]) {
			if peer.Address != "" {
				out[peer.Address] = peer
			}
		}
	}
	return out
}

func peersFromStatus(value any) []bgpstate.Peer {
	switch typed := value.(type) {
	case []bgpstate.Peer:
		return typed
	case []any:
		out := make([]bgpstate.Peer, 0, len(typed))
		for _, raw := range typed {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, bgpstate.Peer{
				Address:           statusString(item["address"]),
				ASN:               uint32(statusInt(item["asn"])),
				State:             statusString(item["state"]),
				Established:       statusBool(item["established"]),
				PrefixesReceived:  statusInt(item["prefixesReceived"]),
				LastEstablishedAt: statusString(item["lastEstablishedAt"]),
				LastErrorAt:       statusString(item["lastErrorAt"]),
				LastErrorReason:   statusString(item["lastErrorReason"]),
			})
		}
		return out
	default:
		return nil
	}
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
