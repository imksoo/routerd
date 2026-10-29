// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux && !freebsd

package bgp

import (
	"context"
	"github.com/imksoo/routerd/pkg/platform"
)

type noopFIBSyncer struct{}

func defaultFIBSyncer() FIBSyncer {
	return noopFIBSyncer{}
}

func (noopFIBSyncer) KernelScope(ctx context.Context) (string, error) {
	return platform.KernelNetworkScope(ctx)
}

func (noopFIBSyncer) SyncBGP(_ context.Context, _ FIBStore, routes []FIBRoute, _ FIBIntentReader) (FIBSyncResult, error) {
	result := FIBSyncResult{Installed: map[string]bool{}, Unsupported: map[string]string{}}
	for _, route := range routes {
		prefix := normalizeRoutePrefix(route.Prefix)
		if prefix != "" {
			result.Unsupported[prefix] = "GoBGPFIBUnsupported"
		}
	}
	return result, nil
}

func (noopFIBSyncer) ClearBGP(context.Context, FIBStore) error { return nil }
