// SPDX-License-Identifier: BSD-3-Clause

package sam

import (
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

// AppliedProxyNeighbor and PendingProxyNeighbor are the native capture owner's
// durable receipts. Pending rows are persisted before effects and remain
// protective until the same owner confirms cleanup or commits applied state.
type AppliedProxyNeighbor struct {
	ID         string `json:"id"`
	PoolRef    string `json:"poolRef"`
	PoolPrefix string `json:"poolPrefix"`
	Address    string `json:"address"`
	Interface  string `json:"interface"`
}

type PendingProxyNeighbor struct {
	AppliedProxyNeighbor
	ProxyEntryOwned bool `json:"proxyEntryOwned"`
	GratuitousARP   bool `json:"gratuitousARP,omitempty"`
}

type AppliedForwardPath struct {
	ID            string `json:"id"`
	PoolRef       string `json:"poolRef"`
	PoolPrefix    string `json:"poolPrefix"`
	Kind          string `json:"kind"`
	Address       string `json:"address"`
	Interface     string `json:"interface"`
	PeerInterface string `json:"peerInterface"`
}

func DecodeAppliedProxyNeighbors(value any, present bool) ([]AppliedProxyNeighbor, error) {
	if !present {
		return nil, nil
	}
	var decoded []AppliedProxyNeighbor
	if !decodeNativeLedger(value, &decoded) || decoded == nil {
		return nil, fmt.Errorf("must be a non-null JSON array")
	}
	byKey := map[string]AppliedProxyNeighbor{}
	for _, neighbor := range decoded {
		if err := validateAppliedProxyNeighbor(neighbor); err != nil {
			return nil, err
		}
		key := ProxyNeighborKey(neighbor.ID, neighbor.Address, neighbor.Interface)
		if previous, found := byKey[key]; found {
			if previous != neighbor {
				return nil, fmt.Errorf("conflicting proxy-neighbor ledger entries for %q", key)
			}
			continue
		}
		byKey[key] = neighbor
	}
	out := make([]AppliedProxyNeighbor, 0, len(byKey))
	for _, neighbor := range byKey {
		out = append(out, neighbor)
	}
	sort.Slice(out, func(i, j int) bool {
		return ProxyNeighborKey(out[i].ID, out[i].Address, out[i].Interface) < ProxyNeighborKey(out[j].ID, out[j].Address, out[j].Interface)
	})
	return out, nil
}

func DecodeAppliedForwardPaths(value any, present bool) ([]AppliedForwardPath, error) {
	if !present {
		return nil, nil
	}
	var decoded []AppliedForwardPath
	if !decodeNativeLedger(value, &decoded) || decoded == nil {
		return nil, fmt.Errorf("must be a non-null JSON array")
	}
	byKey := map[string]AppliedForwardPath{}
	for _, path := range decoded {
		if err := validateAppliedForwardPath(path); err != nil {
			return nil, err
		}
		key := ForwardPathKey(path)
		if previous, found := byKey[key]; found {
			if previous != path {
				return nil, fmt.Errorf("conflicting forward-path ledger entries for %q", key)
			}
			continue
		}
		byKey[key] = path
	}
	out := make([]AppliedForwardPath, 0, len(byKey))
	for _, path := range byKey {
		out = append(out, path)
	}
	sort.Slice(out, func(i, j int) bool { return ForwardPathKey(out[i]) < ForwardPathKey(out[j]) })
	return out, nil
}

func validateAppliedProxyNeighbor(neighbor AppliedProxyNeighbor) error {
	if err := validateAppliedLedgerScope(neighbor.ID, neighbor.PoolRef, neighbor.PoolPrefix, neighbor.Address); err != nil {
		return err
	}
	if err := ValidateCaptureInterface(neighbor.Interface); err != nil {
		return err
	}
	return nil
}

func validateAppliedForwardPath(path AppliedForwardPath) error {
	if path.Kind != "forward-path" && path.Kind != "forward-local-path" {
		return fmt.Errorf("unsupported applied forward-path kind %q", path.Kind)
	}
	if err := validateAppliedLedgerScope(path.ID, path.PoolRef, path.PoolPrefix, path.Address); err != nil {
		return err
	}
	if err := ValidateCaptureInterface(path.Interface); err != nil {
		return err
	}
	return ValidateCaptureInterface(path.PeerInterface)
}

func validateAppliedLedgerScope(id, poolRef, poolPrefix, address string) error {
	if id == "" || strings.TrimSpace(id) != id || strings.ContainsRune(id, '\x00') ||
		poolRef == "" || strings.TrimSpace(poolRef) != poolRef || strings.ContainsRune(poolRef, '\x00') {
		return fmt.Errorf("id and poolRef must be non-empty canonical tokens")
	}
	scope, err := dynamicconfig.ParseCanonicalIPv4Prefix(poolPrefix)
	if err != nil {
		return fmt.Errorf("poolPrefix: %w", err)
	}
	host, err := canonicalNativeHost(address)
	if err != nil || !scope.Contains(host.Addr()) {
		return fmt.Errorf("address %q must be a canonical IPv4 host within %q", address, poolPrefix)
	}
	return nil
}

func canonicalNativeHost(value string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(value)
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 || prefix.Masked().String() != value {
		return netip.Prefix{}, fmt.Errorf("%q must be a canonical IPv4 host prefix", value)
	}
	return prefix, nil
}

func ProxyNeighborKey(id, address, ifname string) string {
	return strings.TrimSpace(id) + "\x00" + strings.TrimSpace(address) + "\x00" + strings.TrimSpace(ifname)
}

func ForwardPathKey(path AppliedForwardPath) string {
	return strings.TrimSpace(path.ID) + "\x00" + strings.TrimSpace(path.PoolRef) + "\x00" + strings.TrimSpace(path.PoolPrefix) + "\x00" + strings.TrimSpace(path.Kind) + "\x00" + strings.TrimSpace(path.Address) + "\x00" + strings.TrimSpace(path.Interface) + "\x00" + strings.TrimSpace(path.PeerInterface)
}

func decodeNativeLedger(value any, target any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}

func DecodePendingProxyNeighbors(value any, present bool) (map[string]PendingProxyNeighbor, error) {
	rows := []PendingProxyNeighbor{}
	if present && (!decodeNativeLedger(value, &rows) || rows == nil) {
		return nil, fmt.Errorf("must be a non-null JSON array")
	}
	result := map[string]PendingProxyNeighbor{}
	for _, row := range rows {
		if err := validateAppliedProxyNeighbor(row.AppliedProxyNeighbor); err != nil {
			return nil, err
		}
		key := ProxyNeighborKey(row.ID, row.Address, row.Interface)
		if previous, ok := result[key]; ok && previous != row {
			return nil, fmt.Errorf("conflicting pending proxy-neighbor %q", key)
		}
		result[key] = row
	}
	return result, nil
}
