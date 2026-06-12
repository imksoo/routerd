// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package chain

import (
	"context"
	"fmt"
)

type unsupportedSAMProxyNeighborApplier struct{}

func defaultSAMProxyNeighborApplier() samProxyNeighborApplier {
	return unsupportedSAMProxyNeighborApplier{}
}

func defaultSAMGratuitousARPAnnouncer() samGratuitousARPAnnouncer {
	return unsupportedSAMGratuitousARPAnnouncer{}
}

func (unsupportedSAMProxyNeighborApplier) SetProxyARP(context.Context, string, bool) error {
	return nil
}

func (unsupportedSAMProxyNeighborApplier) EnsureProxyNeighbor(context.Context, string, string) error {
	return fmt.Errorf("SAM capture not implemented on this OS")
}

func (unsupportedSAMProxyNeighborApplier) DeleteProxyNeighbor(context.Context, string, string) error {
	return nil
}

func (unsupportedSAMProxyNeighborApplier) EnsureOSAddressPresent(context.Context, string, string) (samOSAddressAssignResult, error) {
	return samOSAddressAssignResult{}, nil
}

func (unsupportedSAMProxyNeighborApplier) EnsureOSAddressAbsent(context.Context, string) (samOSAddressDeassignResult, error) {
	return samOSAddressDeassignResult{}, nil
}

func (unsupportedSAMProxyNeighborApplier) EnsureReturnPolicyRoute(context.Context, string, string, string, int, int, int) error {
	return nil
}

func (unsupportedSAMProxyNeighborApplier) DeleteReturnPolicyRoute(context.Context, string, string, int, int) error {
	return nil
}

type unsupportedSAMGratuitousARPAnnouncer struct{}

func (unsupportedSAMGratuitousARPAnnouncer) SendGratuitousARP(context.Context, string, string) error {
	return fmt.Errorf("SAM capture not implemented on this OS")
}
