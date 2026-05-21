// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package bgp

import "context"

type noopFIBSyncer struct{}

func defaultFIBSyncer() FIBSyncer {
	return noopFIBSyncer{}
}

func (noopFIBSyncer) SyncBGP(context.Context, []FIBRoute) error {
	return nil
}
