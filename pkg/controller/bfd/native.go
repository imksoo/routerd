// SPDX-License-Identifier: BSD-3-Clause

package bfd

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

const nativeStatusName = "bfd-native"

// FRR exposes the configured tuple and timers in `show bfd peers json`.
// This is the native observation boundary, not a persisted application format.
type frrPeer struct {
	Multihop   bool   `json:"multihop"`
	Address    string `json:"peer"`
	LocalAddr  string `json:"local"`
	VRF        string `json:"vrf"`
	Interface  string `json:"interface"`
	Status     string `json:"status"`
	Type       string `json:"-"`
	MinRxMS    int    `json:"receive-interval"`
	MinTxMS    int    `json:"transmit-interval"`
	Multiplier int    `json:"detect-multiplier"`
}

type nativeReceipt struct {
	Held    []session
	Pending []session
}

func sessionKey(s session) string {
	if s.Interface != "" {
		return s.Address + "\x00interface\x00" + s.Interface
	}
	return s.Address + "\x00multihop\x00" + s.LocalAddr
}

func (p frrPeer) key() string {
	var key string
	if p.Multihop {
		key = sessionKey(session{Address: p.Address, LocalAddr: p.LocalAddr})
	} else {
		key = sessionKey(session{Address: p.Address, Interface: p.Interface})
	}
	if p.VRF != "default" {
		return "vrf\x00" + p.VRF + "\x00" + key
	}
	return key
}

func (p frrPeer) matches(s session) bool {
	return p.Type == "configured" && p.key() == sessionKey(s) &&
		p.MinRxMS == s.MinRxMS && p.MinTxMS == s.MinTxMS && p.Multiplier == s.Multiplier
}

func parseFRRBFDPeers(data []byte) (map[string]frrPeer, error) {
	var peers []frrPeer
	if err := json.Unmarshal(data, &peers); err != nil {
		return nil, fmt.Errorf("decode FRR BFD peers: %w", err)
	}
	if peers == nil {
		return nil, fmt.Errorf("FRR BFD peers is not an array")
	}
	out := make(map[string]frrPeer, len(peers))
	for _, peer := range peers {
		address, err := netip.ParseAddr(peer.Address)
		if err != nil || peer.Status == "" || peer.VRF == "" {
			return nil, fmt.Errorf("incomplete FRR BFD peer %q", peer.Address)
		}
		peer.Address = address.String()
		if peer.Multihop {
			local, err := netip.ParseAddr(peer.LocalAddr)
			if err != nil {
				return nil, fmt.Errorf("invalid FRR multihop BFD local address %q", peer.LocalAddr)
			}
			peer.LocalAddr = local.String()
		}
		if _, exists := out[peer.key()]; exists {
			return nil, fmt.Errorf("duplicate FRR BFD peer tuple %q", peer.Address)
		}
		out[peer.key()] = peer
	}
	return out, nil
}

func observeFRRBFD(ctx context.Context, run CommandRunner, vtysh string) (map[string]frrPeer, error) {
	out, err := run(ctx, vtysh, "-c", "show bfd peers json")
	if err != nil {
		return nil, fmt.Errorf("observe FRR BFD peers: %w", err)
	}
	peers, err := parseFRRBFDPeers(out)
	if err != nil || len(peers) == 0 {
		return peers, err
	}
	// FRR 8.4 exposes configured/dynamic only in its text display, not JSON.
	// Match the complete native header; never infer ownership from Up or timers.
	out, err = run(ctx, vtysh, "-c", "show bfd peers")
	if err != nil {
		return nil, fmt.Errorf("observe FRR BFD peer types: %w", err)
	}
	headers := make(map[string]string, len(peers))
	for key, peer := range peers {
		header := "peer " + peer.Address
		if peer.Multihop {
			header += " multihop"
		}
		if peer.LocalAddr != "" {
			header += " local-address " + peer.LocalAddr
		}
		header += " vrf " + peer.VRF
		if peer.Interface != "" {
			header += " interface " + peer.Interface
		}
		headers[header] = key
	}
	key := ""
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "peer ") {
			key = headers[line]
		}
		if key == "" || !strings.HasPrefix(line, "Peer Type:") {
			continue
		}
		peer := peers[key]
		if peer.Type != "" {
			return nil, fmt.Errorf("duplicate FRR BFD peer type for %s", peer.Address)
		}
		peer.Type = strings.TrimSpace(strings.TrimPrefix(line, "Peer Type:"))
		peers[key] = peer
	}
	for _, peer := range peers {
		if peer.Type != "configured" && peer.Type != "dynamic" {
			return nil, fmt.Errorf("incomplete FRR BFD peer type for %s", peer.Address)
		}
	}
	return peers, nil
}

func nativeStates(peers map[string]frrPeer) map[string]string {
	out := make(map[string]string, len(peers))
	for key, peer := range peers {
		out[key] = normalizeBFDState(peer.Status)
	}
	return out
}

func (c Controller) reconcileNative(ctx context.Context, desired []session) (map[string]string, error) {
	receipt, err := c.readNativeReceipt()
	if err != nil {
		return nil, err
	}
	if len(desired) == 0 && len(receipt.Held) == 0 && len(receipt.Pending) == 0 {
		return nil, nil
	}
	run := c.runner()
	observed, err := observeFRRBFD(ctx, run, c.vtysh())
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]session, len(desired))
	for _, s := range desired {
		if err := validateNativeSession(s); err != nil {
			return nativeStates(observed), err
		}
		if previous, exists := wanted[sessionKey(s)]; exists && !sameSessionConfig(previous, s) {
			return nativeStates(observed), fmt.Errorf("BFD resources disagree on native peer %s", s.Address)
		}
		wanted[sessionKey(s)] = s
	}
	owned := map[string][]session{}
	for _, sessions := range [][]session{receipt.Held, receipt.Pending} {
		for _, s := range sessions {
			owned[sessionKey(s)] = append(owned[sessionKey(s)], s)
		}
	}
	// Refuse adoption before journaling or changing any peer. Exact tuple and
	// timer evidence also prevents deleting a peer changed by another owner.
	for key, peer := range observed {
		candidates := owned[key]
		if len(candidates) == 0 {
			if _, desired := wanted[key]; desired {
				return nativeStates(observed), fmt.Errorf("BFD peer %s already exists without routerd ownership", peer.Address)
			}
			continue
		}
		matched := false
		for _, previous := range candidates {
			matched = matched || peer.matches(previous)
		}
		if !matched {
			return nativeStates(observed), fmt.Errorf("owned BFD peer %s changed outside routerd", peer.Address)
		}
	}
	var removals []session
	for key, candidates := range owned {
		if _, keep := wanted[key]; keep {
			continue
		}
		if _, exists := observed[key]; exists {
			removals = append(removals, candidates[0])
		}
	}
	needsApply := len(removals) != 0
	for key, s := range wanted {
		peer, exists := observed[key]
		needsApply = needsApply || !exists || !peer.matches(s)
	}
	if needsApply {
		// Keep both the previous and attempted configuration until the native
		// operation and its final receipt are acknowledged. A failed/partial
		// vtysh operation can then be retried or withdrawn after restart.
		// Retain only the currently observed owned configuration and the
		// next attempt, rather than accumulating every failed config version.
		held := []session{}
		for key, candidates := range owned {
			if peer, exists := observed[key]; exists {
				for _, previous := range candidates {
					if peer.matches(previous) {
						held = append(held, previous)
						break
					}
				}
			}
		}
		pending := nativeReceipt{Held: uniqueSessions(held), Pending: uniqueSessions(desired)}
		if err := c.checkNativeReceipt(receipt); err != nil {
			return nativeStates(observed), err
		}
		if err := c.saveNativeReceipt(pending); err != nil {
			return nativeStates(observed), err
		}
		receipt = pending
		// Recheck the actual tuples after journaling and before configuration.
		// A missing/changed observation is not permission to update or remove it.
		current, err := observeFRRBFD(ctx, run, c.vtysh())
		if err != nil {
			return nil, err
		}
		for key := range wanted {
			if !sameObservedSession(observed, current, key) {
				return nativeStates(current), fmt.Errorf("FRR BFD peer changed before apply")
			}
		}
		for _, s := range removals {
			if !sameObservedSession(observed, current, sessionKey(s)) {
				return nativeStates(current), fmt.Errorf("FRR BFD peer changed before withdrawal")
			}
		}
		if err := c.checkNativeReceipt(receipt); err != nil {
			return nativeStates(current), err
		}
		if err := c.applyNativeConfig(ctx, desired, removals); err != nil {
			return nativeStates(observed), err
		}
		observed, err = observeFRRBFD(ctx, run, c.vtysh())
		if err != nil {
			return nil, err
		}
		for _, s := range removals {
			if _, exists := observed[sessionKey(s)]; exists {
				return nativeStates(observed), fmt.Errorf("FRR BFD peer %s remains after withdrawal", s.Address)
			}
		}
		for key, s := range wanted {
			if peer, exists := observed[key]; !exists || !peer.matches(s) {
				return nativeStates(observed), fmt.Errorf("FRR BFD peer %s does not match acknowledged configuration", s.Address)
			}
		}
	}
	if err := c.checkNativeReceipt(receipt); err != nil {
		return nativeStates(observed), err
	}
	if err := c.saveNativeReceipt(nativeReceipt{Held: uniqueSessions(desired), Pending: []session{}}); err != nil {
		return nativeStates(observed), err
	}
	return nativeStates(observed), nil
}

func sameObservedSession(a, b map[string]frrPeer, key string) bool {
	left, leftOK := a[key]
	right, rightOK := b[key]
	return leftOK == rightOK && (!leftOK || left.Type == right.Type && left.Multihop == right.Multihop &&
		left.Address == right.Address && left.LocalAddr == right.LocalAddr && left.VRF == right.VRF && left.Interface == right.Interface &&
		left.MinRxMS == right.MinRxMS && left.MinTxMS == right.MinTxMS && left.Multiplier == right.Multiplier)
}

func (c Controller) checkNativeReceipt(want nativeReceipt) error {
	actual, err := c.readNativeReceipt()
	if err != nil {
		return err
	}
	if !slices.Equal(actual.Held, want.Held) || !slices.Equal(actual.Pending, want.Pending) {
		return fmt.Errorf("BFD native receipt changed during observation")
	}
	return nil
}

func sameSessionConfig(a, b session) bool {
	return sessionKey(a) == sessionKey(b) && a.MinRxMS == b.MinRxMS && a.MinTxMS == b.MinTxMS && a.Multiplier == b.Multiplier
}

func uniqueSessions(sessions []session) []session {
	out := make([]session, 0, len(sessions))
	for _, s := range sessions {
		found := false
		for _, previous := range out {
			found = found || sameSessionConfig(s, previous)
		}
		if !found {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return sessionKey(out[i]) < sessionKey(out[j]) })
	return out
}

func (c Controller) applyNativeConfig(ctx context.Context, desired, removals []session) error {
	var config strings.Builder
	config.WriteString("bfd\n")
	for _, s := range uniqueSessions(removals) {
		config.WriteString(" no " + peerCommand(s) + "\n")
	}
	config.WriteString(strings.TrimPrefix(RenderFRRConfig(uniqueSessions(desired)), "bfd\n"))
	path := c.configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(config.String()), 0644); err != nil {
		return err
	}
	if out, err := c.runner()(ctx, c.vtysh(), "-f", path); err != nil {
		return fmt.Errorf("apply FRR bfdd config: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func peerCommand(s session) string {
	if s.Interface != "" {
		return "peer " + s.Address + " interface " + s.Interface
	}
	return "peer " + s.Address + " multihop local-address " + s.LocalAddr
}

func (c Controller) saveNativeReceipt(receipt nativeReceipt) error {
	if receipt.Held == nil {
		receipt.Held = []session{}
	}
	if receipt.Pending == nil {
		receipt.Pending = []session{}
	}
	return c.Store.SaveObjectStatus(api.RouterAPIVersion, "Router", nativeStatusName, map[string]any{"held": receipt.Held, "pending": receipt.Pending})
}

func (c Controller) readNativeReceipt() (nativeReceipt, error) {
	status, err := c.Store.ReadObjectStatus(api.RouterAPIVersion, "Router", nativeStatusName)
	if err != nil {
		return nativeReceipt{}, fmt.Errorf("read BFD native ownership: %w", err)
	}
	if status == nil {
		return nativeReceipt{}, nil
	}
	held, err := decodeNativeSessions(status["held"])
	if err != nil {
		return nativeReceipt{}, err
	}
	pending, err := decodeNativeSessions(status["pending"])
	return nativeReceipt{Held: held, Pending: pending}, err
}

func decodeNativeSessions(value any) ([]session, error) {
	var sessions []session
	switch value := value.(type) {
	case []session:
		sessions = value
	case []any:
		for _, item := range value {
			fields, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid BFD native receipt session")
			}
			s := session{}
			for key, target := range map[string]*string{"bfdName": &s.BFDName, "address": &s.Address, "localAddress": &s.LocalAddr, "interface": &s.Interface} {
				text, ok := fields[key].(string)
				if !ok {
					return nil, fmt.Errorf("invalid BFD native receipt %s", key)
				}
				*target = text
			}
			for key, target := range map[string]*int{"minRx": &s.MinRxMS, "minTx": &s.MinTxMS, "detectMultiplier": &s.Multiplier} {
				number, ok := fields[key].(float64)
				if !ok || number != math.Trunc(number) || number < 1 || number > math.MaxInt32 {
					return nil, fmt.Errorf("invalid BFD native receipt %s", key)
				}
				*target = int(number)
			}
			sessions = append(sessions, s)
		}
	default:
		return nil, fmt.Errorf("invalid BFD native receipt sessions")
	}
	for _, s := range sessions {
		if err := validateNativeSession(s); err != nil {
			return nil, fmt.Errorf("invalid BFD native receipt: %w", err)
		}
	}
	return sessions, nil
}

func validateNativeSession(s session) error {
	if _, err := netip.ParseAddr(s.Address); err != nil {
		return fmt.Errorf("BFD peer must have a valid address: %q", s.Address)
	}
	if s.Interface == "" {
		if _, err := netip.ParseAddr(s.LocalAddr); err != nil {
			return fmt.Errorf("multihop BFD peer must have a local address: %q", s.LocalAddr)
		}
	} else if len(strings.Fields(s.Interface)) != 1 || strings.ContainsAny(s.Interface, "\r\n\x00") {
		return fmt.Errorf("BFD peer has invalid interface %q", s.Interface)
	}
	if s.BFDName == "" || s.MinRxMS < 1 || s.MinTxMS < 1 || s.Multiplier < 1 || s.Multiplier > 255 {
		return fmt.Errorf("BFD peer %s has invalid configuration", s.Address)
	}
	return nil
}
