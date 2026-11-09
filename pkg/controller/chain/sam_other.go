// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux && !freebsd

package chain

import (
	"context"
	"fmt"

	"github.com/imksoo/routerd/pkg/sam"
)

type unsupportedSAMProxyNeighborApplier struct{}

func defaultSAMProxyNeighborApplier() unsupportedSAMProxyNeighborApplier {
	return unsupportedSAMProxyNeighborApplier{}
}

func defaultSAMGratuitousARPAnnouncer() unsupportedSAMGratuitousARPAnnouncer {
	return unsupportedSAMGratuitousARPAnnouncer{}
}

func (unsupportedSAMProxyNeighborApplier) SetProxyARP(context.Context, string, bool) error {
	return nil
}

func (unsupportedSAMProxyNeighborApplier) SetIPForwarding(context.Context, bool) error {
	return nil
}

func (unsupportedSAMProxyNeighborApplier) EnsureProxyNeighbor(context.Context, string, string) error {
	return fmt.Errorf("SAM capture not implemented on this OS")
}

func (unsupportedSAMProxyNeighborApplier) DeleteProxyNeighbor(context.Context, string, string) error {
	return nil
}

func (unsupportedSAMProxyNeighborApplier) EnsureOSAddressAbsent(context.Context, string) (bool, error) {
	return false, nil
}

func (unsupportedSAMProxyNeighborApplier) ReconcileForwardPaths(context.Context, []sam.CaptureAction) error {
	return nil
}

type unsupportedSAMGratuitousARPAnnouncer struct{}

func (unsupportedSAMGratuitousARPAnnouncer) SendGratuitousARP(context.Context, string, string) error {
	return fmt.Errorf("SAM capture not implemented on this OS")
}
