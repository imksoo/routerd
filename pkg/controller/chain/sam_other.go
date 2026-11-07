// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux && !freebsd

package chain

import (
	"context"
	"fmt"

	"github.com/imksoo/routerd/pkg/sam"
)

type unsupportedSAMProxyNeighborApplier struct{}

func defaultSAMProxyNeighborApplier() samProxyNeighborApplier {
	return unsupportedSAMProxyNeighborApplier{}
}

func defaultSAMGratuitousARPAnnouncer() samGratuitousARPAnnouncer {
	return unsupportedSAMGratuitousARPAnnouncer{}
}

func (unsupportedSAMProxyNeighborApplier) SetProxyARP(context.Context, string, bool, bool, func() error) (samProxyARPApplyResult, error) {
	return samProxyARPApplyResult{}, fmt.Errorf("SAM capture not implemented on this OS")
}

func (unsupportedSAMProxyNeighborApplier) SetAcceptLocal(context.Context, string, bool, bool, func() error) (samProxyARPApplyResult, error) {
	return samProxyARPApplyResult{}, fmt.Errorf("SAM capture not implemented on this OS")
}

func (unsupportedSAMProxyNeighborApplier) SetIPForwarding(context.Context, bool) error {
	return fmt.Errorf("SAM capture not implemented on this OS")
}

func (unsupportedSAMProxyNeighborApplier) EnsureProxyNeighbor(context.Context, string, string, bool, func() error) error {
	return fmt.Errorf("SAM capture not implemented on this OS")
}

func (unsupportedSAMProxyNeighborApplier) DeleteProxyNeighbor(context.Context, string, string) error {
	return fmt.Errorf("SAM capture not implemented on this OS")
}

func (unsupportedSAMProxyNeighborApplier) EnsureOSAddressAbsent(context.Context, string) error {
	return fmt.Errorf("SAM capture not implemented on this OS")
}

func (unsupportedSAMProxyNeighborApplier) ReconcileForwardPaths(context.Context, []sam.CaptureAction, func(context.Context, string, bool) error) error {
	return fmt.Errorf("SAM capture not implemented on this OS")
}

type unsupportedSAMGratuitousARPAnnouncer struct{}

func (unsupportedSAMGratuitousARPAnnouncer) SendGratuitousARP(context.Context, string, string) error {
	return fmt.Errorf("SAM capture not implemented on this OS")
}
