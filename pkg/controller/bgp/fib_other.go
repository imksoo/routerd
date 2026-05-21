// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package bgp

import "context"

type noopFIBSyncer struct{}

func defaultFIBSyncer() FIBSyncer {
	return noopFIBSyncer{}
}

func (noopFIBSyncer) SyncBGP(_ context.Context, routes []FIBRoute) (FIBSyncResult, error) {
	result := FIBSyncResult{Installed: map[string]bool{}, Unsupported: map[string]string{}}
	for _, route := range routes {
		prefix := normalizeRoutePrefix(route.Prefix)
		if prefix != "" {
			result.Unsupported[prefix] = "GoBGPFIBUnsupported"
		}
	}
	return result, nil
}
