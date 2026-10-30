// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

const tunnelNativeStatusName = "tunnel-native"

// This is the Linux TunnelInterface owner's recovery record. Display status
// grants no native authority. A pending listener without an ACK remains unknown.
type tunnelNativeReceipt struct {
	Resource        string        `json:"resource"`
	Scope           string        `json:"scope"`
	Token           string        `json:"token"`
	Index           int           `json:"index"`
	Pending         bool          `json:"pending"`
	Withdrawing     bool          `json:"withdrawing"`
	Plan            tunnelDesired `json:"plan"`
	PreviousAddress string        `json:"previousAddress,omitempty"`
	Listener        fouListener   `json:"listener"`
	ListenerOwned   bool          `json:"listenerOwned"`
	ListenerPending bool          `json:"listenerPending"`
}

type tunnelNativeLink struct {
	Exists             bool
	Scope, Token, Kind string
	Index              int
	Observed           tunnelObserved
	Addresses          []string
}

func tunnelNativeKind(mode string) string {
	if mode == "gre" {
		return "gre"
	}
	return "ipip"
}

func tunnelCreationIndex(token string) int {
	digest := sha256.Sum256([]byte(token))
	return int(binary.BigEndian.Uint32(digest[:4])&0x3fffffff) + 1024
}

func (c TunnelInterfaceController) tunnelKernelScope(ctx context.Context) (string, error) {
	read := c.NativeScope
	if read == nil {
		read = platform.KernelNetworkScope
	}
	scope, err := read(ctx)
	if err != nil {
		return "", err
	}
	parts := strings.Split(scope, ":")
	if len(parts) != 3 || parts[0] != "linux" || parts[1] == "" || parts[2] == "" {
		return "", errors.New("TunnelInterface kernel scope is unavailable")
	}
	return scope, ctx.Err()
}

func (c TunnelInterfaceController) tunnelNativeReceipts() ([]tunnelNativeReceipt, error) {
	status, err := c.Store.ReadObjectStatus(api.RouterAPIVersion, "Router", tunnelNativeStatusName)
	if err != nil {
		return nil, err
	}
	if status == nil {
		return nil, nil
	}
	var records []tunnelNativeReceipt
	if !decodeAppliedDataplaneStatus(status["interfaces"], &records) || records == nil {
		return nil, errors.New("invalid TunnelInterface native journal")
	}
	names := make(map[string]bool, len(records))
	for _, record := range records {
		token, tokenErr := hex.DecodeString(record.Token)
		local, localErr := netip.ParseAddr(record.Plan.Local)
		remote, remoteErr := netip.ParseAddr(record.Plan.Remote)
		parts := strings.Split(record.Scope, ":")
		if record.Resource == "" || record.Plan.Name != record.Resource || names[record.Resource] || tokenErr != nil || len(token) != 16 || len(parts) != 3 || parts[0] != "linux" || parts[1] == "" || parts[2] == "" || record.Index < 0 || !record.Pending && record.Index < 1 || localErr != nil || !local.Is4() || remoteErr != nil || !remote.Is4() {
			return nil, fmt.Errorf("invalid TunnelInterface native receipt for %q", record.Resource)
		}
		switch record.Plan.Mode {
		case "ipip", "gre", "fou", "gue":
		default:
			return nil, errors.New("invalid TunnelInterface native mode")
		}
		if record.ListenerOwned || record.ListenerPending {
			if record.Listener.Mode != "fou" && record.Listener.Mode != "gue" || record.Listener.Port < 1 || record.Listener.Port > 65535 {
				return nil, errors.New("invalid TunnelInterface listener receipt")
			}
		}
		names[record.Resource] = true
	}
	return records, nil
}

func (c TunnelInterfaceController) saveTunnelNative(previous *tunnelNativeReceipt, next tunnelNativeReceipt, remove bool) error {
	records, err := c.tunnelNativeReceipts()
	if err != nil {
		return err
	}
	found := false
	updated := make([]tunnelNativeReceipt, 0, len(records)+1)
	for _, current := range records {
		if current.Resource != next.Resource {
			updated = append(updated, current)
			continue
		}
		if previous == nil || current != *previous {
			return errors.New("TunnelInterface native receipt changed before persistence")
		}
		found = true
	}
	if previous != nil && (!found || previous.Resource != next.Resource) {
		return errors.New("TunnelInterface native receipt disappeared before persistence")
	}
	if !remove {
		updated = append(updated, next)
	}
	sort.Slice(updated, func(i, j int) bool { return updated[i].Resource < updated[j].Resource })
	return c.Store.SaveObjectStatus(api.RouterAPIVersion, "Router", tunnelNativeStatusName, map[string]any{"interfaces": updated})
}

func (c TunnelInterfaceController) checkTunnelNativeReceipt(ctx context.Context, record tunnelNativeReceipt) error {
	scope, err := c.tunnelKernelScope(ctx)
	if err != nil {
		return err
	}
	if scope != record.Scope {
		return errors.New("TunnelInterface belongs to another kernel scope")
	}
	records, err := c.tunnelNativeReceipts()
	if err != nil {
		return err
	}
	for _, current := range records {
		if current == record {
			return ctx.Err()
		}
	}
	return errors.New("TunnelInterface native receipt changed while observing")
}

func verifyTunnelNative(record tunnelNativeReceipt, link tunnelNativeLink) error {
	if link.Scope != record.Scope {
		return errors.New("TunnelInterface native observation belongs to another scope")
	}
	if !link.Exists {
		return nil
	}
	index := record.Index
	if index == 0 {
		index = tunnelCreationIndex(record.Token)
	}
	if link.Index != index || link.Kind != tunnelNativeKind(record.Plan.Mode) || link.Token != record.Token {
		return errors.New("TunnelInterface native link is foreign or replaced")
	}
	return nil
}

func (c TunnelInterfaceController) observeOwnedTunnel(ctx context.Context, name string) (tunnelNativeLink, error) {
	read := c.ObserveNative
	if read == nil {
		read = observeTunnelNativeLink
	}
	return read(ctx, name)
}

func (c TunnelInterfaceController) checkedOwnedTunnel(ctx context.Context, record tunnelNativeReceipt) (tunnelNativeLink, error) {
	link, err := c.observeOwnedTunnel(ctx, record.Plan.Name)
	if err != nil {
		return link, err
	}
	if err := verifyTunnelNative(record, link); err != nil {
		return link, err
	}
	return link, c.checkTunnelNativeReceipt(ctx, record)
}
