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

func (unsupportedSAMProxyNeighborApplier) EnsureProxyNeighbor(context.Context, string, string) error {
	return fmt.Errorf("SAM capture not implemented on this OS")
}

func (unsupportedSAMProxyNeighborApplier) DeleteProxyNeighbor(context.Context, string, string) error {
	return nil
}

func (unsupportedSAMProxyNeighborApplier) EnsureOSAddressAbsent(context.Context, string) (samOSAddressDeassignResult, error) {
	return samOSAddressDeassignResult{}, nil
}
