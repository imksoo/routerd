// SPDX-License-Identifier: BSD-3-Clause

package bfd

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/subprocessio"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error)
}

type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type Controller struct {
	Router       *api.Router
	Store        Store
	DryRun       bool
	OS           platform.OS
	RuntimeDir   string
	VtyshCommand string
	Command      CommandRunner
	Now          func() time.Time
}

type session struct {
	BFDName    string `json:"bfdName"`
	Address    string `json:"address"`
	LocalAddr  string `json:"localAddress"`
	Interface  string `json:"interface"`
	MinRxMS    int    `json:"minRx"`
	MinTxMS    int    `json:"minTx"`
	Multiplier int    `json:"detectMultiplier"`
}

func (c Controller) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	sessions, byBFD, err := c.sessions()
	if err != nil {
		return err
	}
	if c.OS == "" {
		c.OS = platform.CurrentOS()
	}
	if !supportsFRRBFD(c.OS) {
		for name, sessions := range byBFD {
			if err := c.saveStatus(name, "Unsupported", sessions, nil, map[string]any{
				"reason": "BFDUnsupportedOS",
				"error":  "FRR bfdd bridge is supported only on Linux and FreeBSD",
			}); err != nil {
				return err
			}
		}
		return nil
	}
	configPath := c.configPath()
	if c.DryRun {
		for name, sessions := range byBFD {
			if err := c.saveStatus(name, "Planned", sessions, nil, map[string]any{
				"backend":    "frr-bfdd",
				"applyWith":  "vtysh",
				"configPath": configPath,
				"dryRun":     true,
			}); err != nil {
				return err
			}
		}
		return nil
	}
	observed, observeErr := c.reconcileNative(ctx, sessions)
	for name, sessions := range byBFD {
		phase := phaseForSessions(sessions, observed, observeErr)
		extra := map[string]any{
			"backend":    "frr-bfdd",
			"applyWith":  "vtysh",
			"configPath": configPath,
			"dryRun":     false,
		}
		if observeErr != nil {
			extra["error"] = observeErr.Error()
		}
		if err := c.saveStatus(name, phase, sessions, observed, extra); err != nil {
			return errors.Join(observeErr, err)
		}
	}
	return observeErr
}

func (c Controller) sessions() ([]session, map[string][]session, error) {
	bgpPeers := map[string]api.BGPPeerSpec{}
	bgpRouters := map[string]api.BGPRouterSpec{}
	bfdSpecs := map[string]api.BFDSpec{}
	interfaces := map[string]string{}
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.NetAPIVersion {
			continue
		}
		switch res.Kind {
		case "Interface":
			spec, err := res.InterfaceSpec()
			if err != nil {
				return nil, nil, err
			}
			interfaces[res.Metadata.Name] = strings.TrimSpace(spec.IfName)
		case "BGPRouter":
			spec, err := res.BGPRouterSpec()
			if err != nil {
				return nil, nil, err
			}
			bgpRouters[res.Metadata.Name] = spec
		case "BGPPeer":
			spec, err := res.BGPPeerSpec()
			if err != nil {
				return nil, nil, err
			}
			bgpPeers[res.Metadata.Name] = spec
		case "BFD":
			spec, err := res.BFDSpec()
			if err != nil {
				return nil, nil, err
			}
			bfdSpecs[res.Metadata.Name] = spec
		}
	}
	var out []session
	byBFD := map[string][]session{}
	addSessions := func(name string, spec api.BFDSpec) error {
		endpoints, err := bfdPeerEndpoints(spec, bgpPeers, bgpRouters)
		if err != nil {
			return err
		}
		if len(endpoints) == 0 {
			return fmt.Errorf("BFD/%s resolved no peer endpoints from spec.peer %q", name, spec.Peer)
		}
		ifName, err := resolveBFDInterface(spec.Interface, interfaces)
		if err != nil {
			return fmt.Errorf("BFD/%s interface: %w", name, err)
		}
		for _, endpoint := range endpoints {
			s := session{
				BFDName:    name,
				Address:    endpoint.Address,
				LocalAddr:  endpoint.LocalAddr,
				Interface:  ifName,
				MinRxMS:    bfdDurationMS(spec.MinRx, spec.Profile, true),
				MinTxMS:    bfdDurationMS(spec.MinTx, spec.Profile, false),
				Multiplier: bfdMultiplier(spec),
			}
			out = append(out, s)
			byBFD[name] = append(byBFD[name], s)
		}
		return nil
	}
	for name, spec := range bfdSpecs {
		if err := addSessions(name, spec); err != nil {
			return nil, nil, err
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].BFDName == out[j].BFDName {
			return out[i].Address < out[j].Address
		}
		return out[i].BFDName < out[j].BFDName
	})
	for name := range byBFD {
		sort.SliceStable(byBFD[name], func(i, j int) bool { return byBFD[name][i].Address < byBFD[name][j].Address })
	}
	return out, byBFD, nil
}

func resolveBFDInterface(ref string, interfaces map[string]string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	kind, name, isRef := strings.Cut(ref, "/")
	if !isRef {
		return ref, nil
	}
	if kind != "Interface" || strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("must be an interface name or Interface/<name>, got %q", ref)
	}
	ifName := strings.TrimSpace(interfaces[name])
	if ifName == "" {
		return "", fmt.Errorf("references missing or empty Interface/%s", name)
	}
	return ifName, nil
}

type bfdPeerEndpoint struct {
	Address   string
	LocalAddr string
}

func bfdPeerEndpoints(spec api.BFDSpec, peers map[string]api.BGPPeerSpec, routers map[string]api.BGPRouterSpec) ([]bfdPeerEndpoint, error) {
	peer := strings.TrimSpace(spec.Peer)
	if kind, name, ok := strings.Cut(peer, "/"); ok && kind == "BGPPeer" {
		peerSpec, ok := peers[name]
		if !ok {
			return nil, fmt.Errorf("BFD spec.peer references missing BGPPeer %q", peer)
		}
		localAddr, err := bgpPeerLocalAddress(peerSpec, routers)
		if err != nil {
			return nil, fmt.Errorf("BFD spec.peer %q: %w", peer, err)
		}
		return cleanEndpoints(peerSpec.Peers, localAddr), nil
	}
	localAddr := ""
	if strings.TrimSpace(spec.Interface) == "" {
		var err error
		localAddr, err = soleBGPRouterID(routers)
		if err != nil {
			return nil, err
		}
	}
	return cleanEndpoints([]string{peer}, localAddr), nil
}

func bgpPeerLocalAddress(peer api.BGPPeerSpec, routers map[string]api.BGPRouterSpec) (string, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(peer.RouterRef), "/")
	if !ok || kind != "BGPRouter" {
		return "", fmt.Errorf("BGPPeer spec.routerRef must reference BGPRouter/<name>, got %q", peer.RouterRef)
	}
	router, ok := routers[name]
	if !ok {
		return "", fmt.Errorf("BGPPeer spec.routerRef references missing BGPRouter %q", peer.RouterRef)
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(router.RouterID))
	if err != nil {
		return "", fmt.Errorf("%s spec.routerID must be a valid address: %w", peer.RouterRef, err)
	}
	return addr.String(), nil
}

func soleBGPRouterID(routers map[string]api.BGPRouterSpec) (string, error) {
	if len(routers) != 1 {
		return "", fmt.Errorf("direct BFD peer requires exactly one BGPRouter for local-address resolution, got %d", len(routers))
	}
	for name, router := range routers {
		addr, err := netip.ParseAddr(strings.TrimSpace(router.RouterID))
		if err != nil {
			return "", fmt.Errorf("BGPRouter/%s spec.routerID must be a valid address: %w", name, err)
		}
		return addr.String(), nil
	}
	return "", fmt.Errorf("direct BFD peer requires a BGPRouter")
}

func cleanEndpoints(values []string, localAddr string) []bfdPeerEndpoint {
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if addr, err := netip.ParseAddr(value); err == nil {
			value = addr.String()
		}
		seen[value] = true
	}
	out := make([]bfdPeerEndpoint, 0, len(seen))
	for value := range seen {
		out = append(out, bfdPeerEndpoint{Address: value, LocalAddr: localAddr})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

func bfdDurationMS(value, profile string, rx bool) int {
	value = strings.TrimSpace(value)
	if value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return int(d / time.Millisecond)
		}
	}
	switch strings.TrimSpace(profile) {
	case "slow":
		return 1000
	case "normal":
		return 500
	default:
		_ = rx
		return 300
	}
}

func bfdMultiplier(spec api.BFDSpec) int {
	if spec.DetectMultiplier > 0 {
		return spec.DetectMultiplier
	}
	switch strings.TrimSpace(spec.Profile) {
	case "slow":
		return 5
	default:
		return 3
	}
}

func RenderFRRConfig(sessions []session) string {
	var b strings.Builder
	b.WriteString("bfd\n")
	for _, s := range sessions {
		b.WriteString(" " + peerCommand(s) + "\n")
		b.WriteString(fmt.Sprintf("  receive-interval %d\n", s.MinRxMS))
		b.WriteString(fmt.Sprintf("  transmit-interval %d\n", s.MinTxMS))
		b.WriteString(fmt.Sprintf("  detect-multiplier %d\n", s.Multiplier))
		b.WriteString(" exit\n")
	}
	return b.String()
}

func (c Controller) saveStatus(name, phase string, sessions []session, observed map[string]string, extra map[string]any) error {
	now := c.now().UTC().Format(time.RFC3339Nano)
	peers := make([]map[string]any, 0, len(sessions))
	peerStates := map[string]string{}
	for _, s := range sessions {
		state := "Unknown"
		if observed != nil {
			if value := strings.TrimSpace(observed[sessionKey(s)]); value != "" {
				state = value
			}
		}
		peerStates[s.Address] = state
		peers = append(peers, map[string]any{
			"address":          s.Address,
			"localAddress":     s.LocalAddr,
			"state":            state,
			"interface":        s.Interface,
			"minRx":            s.MinRxMS,
			"minTx":            s.MinTxMS,
			"detectMultiplier": s.Multiplier,
		})
	}
	status := map[string]any{
		"phase":      phase,
		"peerStates": peerStates,
		"peers":      peers,
		"observedAt": now,
	}
	for key, value := range extra {
		status[key] = value
	}
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "BFD", name, status)
}

func phaseForSessions(sessions []session, observed map[string]string, err error) string {
	if err != nil {
		return "Degraded"
	}
	if len(sessions) == 0 {
		return "Pending"
	}
	up, down := 0, 0
	for _, s := range sessions {
		switch strings.ToLower(strings.TrimSpace(observed[sessionKey(s)])) {
		case "up":
			up++
		case "down":
			down++
		}
	}
	switch {
	case down > 0 && up > 0:
		return "Degraded"
	case down > 0:
		return "Down"
	case up == len(sessions):
		return "Up"
	default:
		return "Pending"
	}
}

func normalizeBFDState(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "up", "established":
		return "Up"
	case "down", "shutdown", "admin-down", "admin_down", "admindown":
		return "Down"
	default:
		return "Unknown"
	}
}

func (c Controller) configPath() string {
	if c.RuntimeDir != "" {
		return filepath.Join(c.RuntimeDir, "bfd", "bfdd.conf")
	}
	defaults, _ := platform.Current()
	return filepath.Join(defaults.RuntimeDir, "bfd", "bfdd.conf")
}

func (c Controller) vtysh() string {
	if strings.TrimSpace(c.VtyshCommand) != "" {
		return strings.TrimSpace(c.VtyshCommand)
	}
	if c.OS == platform.OSFreeBSD {
		return "/usr/local/bin/vtysh"
	}
	return "vtysh"
}

func supportsFRRBFD(osName platform.OS) bool {
	return osName == platform.OSLinux || osName == platform.OSFreeBSD
}

func (c Controller) runner() CommandRunner {
	if c.Command != nil {
		return c.Command
	}
	return defaultCommandRunner
}

func (c Controller) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func defaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, name, args...)
	cmd.WaitDelay = time.Second
	stdout := subprocessio.NewCapture(subprocessio.StdoutLimit)
	stderr := subprocessio.NewCapture(subprocessio.StderrLimit)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if commandCtx.Err() != nil {
		return nil, commandCtx.Err()
	}
	if limitErr := subprocessio.LimitError(stdout, stderr); limitErr != nil {
		return nil, limitErr
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
