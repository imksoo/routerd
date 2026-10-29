// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"fmt"
	"sort"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/sam"
)

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
	neighbors := make([]sam.PendingProxyNeighbor, 0, len(state.pendingProxyNeighbors))
	for _, row := range state.pendingProxyNeighbors {
		neighbors = append(neighbors, row)
	}
	sort.Slice(neighbors, func(i, j int) bool {
		return sam.ProxyNeighborKey(neighbors[i].ID, neighbors[i].Address, neighbors[i].Interface) < sam.ProxyNeighborKey(neighbors[j].ID, neighbors[j].Address, neighbors[j].Interface)
	})
	paths := make([]sam.AppliedForwardPath, 0, len(state.pendingForwardPaths))
	for _, row := range state.pendingForwardPaths {
		paths = append(paths, row)
	}
	sort.Slice(paths, func(i, j int) bool { return sam.ForwardPathKey(paths[i]) < sam.ForwardPathKey(paths[j]) })
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
		applied[sam.ProxyNeighborKey(row.ID, row.Address, row.Interface)] = true
	}
	for _, action := range actions {
		if action.Kind != "proxy-neighbor" {
			continue
		}
		row := sam.AppliedProxyNeighbor{ID: action.IntentID, PoolRef: action.PoolRef, PoolPrefix: action.PoolPrefix, Address: action.Address, Interface: action.Interface}
		key := sam.ProxyNeighborKey(row.ID, row.Address, row.Interface)
		pending, exists := state.pendingProxyNeighbors[key]
		if !exists {
			pending = sam.PendingProxyNeighbor{AppliedProxyNeighbor: row, ProxyEntryOwned: applied[key], GratuitousARP: !applied[key] && action.GratuitousARP}
		}
		state.pendingProxyNeighbors[key] = pending
	}
	for _, path := range state.forwardPaths {
		row := sam.AppliedForwardPath{ID: path.IntentID, PoolRef: path.PoolRef, PoolPrefix: path.PoolPrefix, Kind: path.Kind, Address: path.Address, Interface: path.Interface, PeerInterface: path.PeerInterface}
		state.pendingForwardPaths[sam.ForwardPathKey(row)] = row
	}
	return c.savePendingAcquisitions(*state)
}

func (c SAMController) samWithdrawalAcks(neighbors []sam.AppliedProxyNeighbor, paths []sam.AppliedForwardPath) []dynamicconfig.MobilityWithdrawalRequest {
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
