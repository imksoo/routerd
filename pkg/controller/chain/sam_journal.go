// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"fmt"
	"sort"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/sam"
)

func samPendingProxyNeighbors(value any, present bool) (map[string]samPendingProxyNeighbor, error) {
	rows := []samPendingProxyNeighbor{}
	if present && (!decodeAppliedDataplaneStatus(value, &rows) || rows == nil) {
		return nil, fmt.Errorf("must be a non-null JSON array")
	}
	result := map[string]samPendingProxyNeighbor{}
	for _, row := range rows {
		if err := validateSAMAppliedProxyNeighbor(row.samAppliedProxyNeighbor); err != nil {
			return nil, err
		}
		key := samProxyNeighborKey(row.ID, row.Address, row.Interface)
		if previous, ok := result[key]; ok && previous != row {
			return nil, fmt.Errorf("conflicting pending proxy-neighbor %q", key)
		}
		result[key] = row
	}
	return result, nil
}

func samProxyARPSettingRows(settings map[string]samAppliedProxyARP) []samAppliedProxyARP {
	rows := make([]samAppliedProxyARP, 0, len(settings))
	for _, row := range settings {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Interface < rows[j].Interface })
	return rows
}

func (c SAMController) savePendingAcquisitions(state samReconcileState) error {
	if c.DryRun {
		return nil
	}
	merger, ok := c.Store.(objectStatusMerger)
	if !ok {
		return fmt.Errorf("SAM effects require durable pending acquisition recording")
	}
	neighbors := make([]samPendingProxyNeighbor, 0, len(state.pendingProxyNeighbors))
	for _, row := range state.pendingProxyNeighbors {
		neighbors = append(neighbors, row)
	}
	sort.Slice(neighbors, func(i, j int) bool {
		return samProxyNeighborKey(neighbors[i].ID, neighbors[i].Address, neighbors[i].Interface) < samProxyNeighborKey(neighbors[j].ID, neighbors[j].Address, neighbors[j].Interface)
	})
	paths := make([]samAppliedForwardPath, 0, len(state.pendingForwardPaths))
	for _, row := range state.pendingForwardPaths {
		paths = append(paths, row)
	}
	sort.Slice(paths, func(i, j int) bool { return samForwardPathKey(paths[i]) < samForwardPathKey(paths[j]) })
	return merger.MergeObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName, map[string]any{
		"pendingProxyNeighbors":                 neighbors,
		"pendingForwardPaths":                   paths,
		"pendingProxyARP":                       samProxyARPSettingRows(state.pendingProxyARP),
		"pendingAcceptLocal":                    samProxyARPSettingRows(state.pendingAcceptLocal),
		dynamicconfig.MobilitySAMWithdrawalAcks: []dynamicconfig.MobilityWithdrawalRequest{},
	})
}

func (c SAMController) beginPendingAcquisitions(actions []sam.CaptureAction, state *samReconcileState) error {
	applied := map[string]bool{}
	for _, row := range state.previousProxyNeighbors {
		applied[samProxyNeighborKey(row.ID, row.Address, row.Interface)] = true
	}
	for _, action := range actions {
		if action.Kind != "proxy-neighbor" {
			continue
		}
		row := samAppliedProxyNeighbor{ID: action.IntentID, PoolRef: action.PoolRef, PoolPrefix: action.PoolPrefix, Address: action.Address, Interface: action.Interface}
		key := samProxyNeighborKey(row.ID, row.Address, row.Interface)
		pending, exists := state.pendingProxyNeighbors[key]
		if !exists {
			pending = samPendingProxyNeighbor{samAppliedProxyNeighbor: row, ProxyEntryOwned: applied[key], GratuitousARP: !applied[key] && action.GratuitousARP}
		}
		state.pendingProxyNeighbors[key] = pending
	}
	for _, path := range state.forwardPaths {
		row := samAppliedForwardPath{ID: path.IntentID, PoolRef: path.PoolRef, PoolPrefix: path.PoolPrefix, Kind: path.Kind, Address: path.Address, Interface: path.Interface, PeerInterface: path.PeerInterface}
		state.pendingForwardPaths[samForwardPathKey(row)] = row
	}
	return c.savePendingAcquisitions(*state)
}

func (c SAMController) samWithdrawalAcks(neighbors []samAppliedProxyNeighbor, paths []samAppliedForwardPath) []dynamicconfig.MobilityWithdrawalRequest {
	return matchingMobilityWithdrawalAcks(c.Withdrawals, func(request dynamicconfig.MobilityWithdrawalRequest) bool {
		for _, intent := range c.LocalCaptureIntents {
			if intent.PoolRef == request.PoolRef && intent.Disposition != dynamicconfig.CaptureRelease && dynamicconfig.MobilityEffectContainsAddress(intent.Address, request.Address) {
				return false
			}
		}
		for _, row := range neighbors {
			if row.PoolRef == request.PoolRef && dynamicconfig.MobilityEffectContainsAddress(row.Address, request.Address) {
				return false
			}
		}
		for _, row := range paths {
			if row.PoolRef == request.PoolRef && dynamicconfig.MobilityEffectContainsAddress(row.Address, request.Address) {
				return false
			}
		}
		return true
	})
}
