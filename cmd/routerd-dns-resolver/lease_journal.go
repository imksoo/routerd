// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/imksoo/routerd/pkg/dnsresolver"
	"github.com/imksoo/routerd/pkg/state"
)

// Runtime config may be reused by systemd/daemon supervision after a crash.
// Its last observation snapshot cannot prove that no later lease was revoked.
// Read the current journal owner before constructing any serving zone table.
func currentLeaseZones(ctx context.Context, config dnsresolver.RuntimeConfig) ([]dnsresolver.RuntimeZone, error) {
	zones := slices.Clone(config.Zones)
	needed := false
	for _, zone := range zones {
		needed = needed || len(zone.Spec.DHCPDerived.Sources) > 0
	}
	if !needed {
		return zones, nil
	}
	if !filepath.IsAbs(config.LeaseJournalPath) {
		return nil, fmt.Errorf("DHCP-derived DNS requires an absolute current lease journal path")
	}
	store, err := state.OpenSQLiteReadOnly(config.LeaseJournalPath)
	if err != nil {
		return nil, fmt.Errorf("open current DHCP lease journal: %w", err)
	}
	defer store.Close()
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	observations, err := store.ReadDHCPLeaseObservations(readCtx)
	if err != nil {
		return nil, fmt.Errorf("read current DHCP lease journal: %w", err)
	}
	for i := range zones {
		zones[i].LeaseObservations = nil
		for _, observation := range observations {
			if slices.Contains(zones[i].Spec.DHCPDerived.Sources, observation.Source) {
				zones[i].LeaseObservations = append(zones[i].LeaseObservations, observation)
			}
		}
	}
	return zones, nil
}
