// SPDX-License-Identifier: BSD-3-Clause

package bfd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
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
	BFDName    string
	Address    string
	Interface  string
	MinRxMS    int
	MinTxMS    int
	Multiplier int
}

func (c Controller) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	sessions, byBFD, err := c.sessions()
	if err != nil {
		return err
	}
	if len(byBFD) == 0 {
		return nil
	}
	if c.OS == "" {
		c.OS = platform.CurrentOS()
	}
	if c.OS != platform.OSLinux {
		for name, sessions := range byBFD {
			if err := c.saveStatus(name, "Unsupported", sessions, nil, map[string]any{
				"reason": "BFDLinuxOnly",
				"error":  "FRR bfdd bridge is currently Linux-only",
			}); err != nil {
				return err
			}
		}
		return nil
	}
	config := RenderFRRConfig(sessions)
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
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		return err
	}
	run := c.runner()
	if _, err := run(ctx, c.vtysh(), "-f", configPath); err != nil {
		for name, sessions := range byBFD {
			_ = c.saveStatus(name, "Degraded", sessions, nil, map[string]any{
				"backend":    "frr-bfdd",
				"applyWith":  "vtysh",
				"configPath": configPath,
				"error":      err.Error(),
			})
		}
		return fmt.Errorf("apply FRR bfdd config: %w", err)
	}
	observed, observeErr := observeFRRBFD(ctx, run, c.vtysh())
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
			return err
		}
	}
	return nil
}

func (c Controller) sessions() ([]session, map[string][]session, error) {
	bgpPeers := map[string]api.BGPPeerSpec{}
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.NetAPIVersion || res.Kind != "BGPPeer" {
			continue
		}
		spec, err := res.BGPPeerSpec()
		if err != nil {
			return nil, nil, err
		}
		bgpPeers[res.Metadata.Name] = spec
	}
	var out []session
	byBFD := map[string][]session{}
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.NetAPIVersion || res.Kind != "BFD" {
			continue
		}
		spec, err := res.BFDSpec()
		if err != nil {
			return nil, nil, err
		}
		addresses := bfdPeerAddresses(spec, bgpPeers)
		for _, address := range addresses {
			s := session{
				BFDName:    res.Metadata.Name,
				Address:    address,
				Interface:  strings.TrimSpace(spec.Interface),
				MinRxMS:    bfdDurationMS(spec.MinRx, spec.Profile, true),
				MinTxMS:    bfdDurationMS(spec.MinTx, spec.Profile, false),
				Multiplier: bfdMultiplier(spec),
			}
			out = append(out, s)
			byBFD[res.Metadata.Name] = append(byBFD[res.Metadata.Name], s)
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

func bfdPeerAddresses(spec api.BFDSpec, peers map[string]api.BGPPeerSpec) []string {
	peer := strings.TrimSpace(spec.Peer)
	if kind, name, ok := strings.Cut(peer, "/"); ok && kind == "BGPPeer" {
		return cleanAddresses(peers[name].Peers)
	}
	return cleanAddresses([]string{peer})
}

func cleanAddresses(values []string) []string {
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
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
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
	b.WriteString("configure terminal\n")
	b.WriteString("bfd\n")
	for _, s := range sessions {
		b.WriteString(" peer " + s.Address)
		if s.Interface != "" {
			b.WriteString(" interface " + interfaceName(s.Interface))
		} else {
			b.WriteString(" multihop")
		}
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  receive-interval %d\n", s.MinRxMS))
		b.WriteString(fmt.Sprintf("  transmit-interval %d\n", s.MinTxMS))
		b.WriteString(fmt.Sprintf("  detect-multiplier %d\n", s.Multiplier))
		b.WriteString(" exit\n")
	}
	b.WriteString("end\n")
	return b.String()
}

func interfaceName(ref string) string {
	ref = strings.TrimSpace(ref)
	if _, name, ok := strings.Cut(ref, "/"); ok {
		return strings.TrimSpace(name)
	}
	return ref
}

func (c Controller) saveStatus(name, phase string, sessions []session, observed map[string]string, extra map[string]any) error {
	now := c.now().UTC().Format(time.RFC3339Nano)
	peers := make([]map[string]any, 0, len(sessions))
	peerStates := map[string]string{}
	for _, s := range sessions {
		state := "Unknown"
		if observed != nil {
			if value := strings.TrimSpace(observed[s.Address]); value != "" {
				state = value
			}
		}
		peerStates[s.Address] = state
		peers = append(peers, map[string]any{
			"address":          s.Address,
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
		switch strings.ToLower(strings.TrimSpace(observed[s.Address])) {
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

func observeFRRBFD(ctx context.Context, run CommandRunner, vtysh string) (map[string]string, error) {
	out, err := run(ctx, vtysh, "-c", "show bfd peers json")
	if err != nil {
		return nil, err
	}
	return ParseFRRBFDPeersJSON(out), nil
}

func ParseFRRBFDPeersJSON(data []byte) map[string]string {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil
	}
	out := map[string]string{}
	walkBFDJSON(value, out)
	return out
}

func walkBFDJSON(value any, out map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		address := firstStringField(typed, "peer", "peerAddress", "peerAddr", "remote", "remoteAddress", "dst")
		state := firstStringField(typed, "state", "status", "sessionState", "localState")
		if address != "" && state != "" {
			out[address] = normalizeBFDState(state)
		}
		for key, child := range typed {
			if addr, err := netip.ParseAddr(strings.TrimSpace(key)); err == nil {
				if childMap, ok := child.(map[string]any); ok {
					state := firstStringField(childMap, "state", "status", "sessionState", "localState")
					if state != "" {
						out[addr.String()] = normalizeBFDState(state)
					}
				}
			}
			walkBFDJSON(child, out)
		}
	case []any:
		for _, item := range typed {
			walkBFDJSON(item, out)
		}
	}
}

func firstStringField(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(fmt.Sprint(values[key])); value != "" && value != "<nil>" {
			if addr, err := netip.ParseAddr(value); err == nil {
				return addr.String()
			}
			return value
		}
	}
	return ""
}

func normalizeBFDState(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "up", "established":
		return "Up"
	case "down", "admin_down", "admindown":
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
	return "vtysh"
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
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
