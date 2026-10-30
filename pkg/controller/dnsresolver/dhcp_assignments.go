// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	resolverruntime "github.com/imksoo/routerd/pkg/dnsresolver"
	"github.com/imksoo/routerd/pkg/logstore"
)

// The hosts sidecar describes intended DHCP assignments. It carries neither a
// named source nor a lease deadline, so it cannot itself become a live lease.
// Join it to current configured reservations or still-held sticky DB rows.
func (c Controller) namedDHCPAssignments(ctx context.Context, parsed []resolverruntime.RuntimeDHCPHostRecord, scopes []resolverruntime.DHCPAddressScope) ([]resolverruntime.RuntimeDHCPHostRecord, error) {
	if len(parsed) == 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	var held []logstore.DHCPStickyLease
	_, stateDir := c.dirs()
	path := filepath.Join(stateDir, "dhcp-sticky.db")
	if _, err := os.Stat(path); err == nil {
		store, err := logstore.OpenDHCPStickyLogReadOnly(path)
		if err != nil {
			return nil, fmt.Errorf("read held DHCP assignments: %w", err)
		}
		held, err = store.List(ctx, logstore.DHCPStickyFilter{HeldOnly: true, Now: now, Limit: 10000})
		closeErr := store.Close()
		if err != nil {
			return nil, err
		}
		if len(held) == 10000 {
			return nil, fmt.Errorf("held DHCP assignment limit reached; snapshot would be incomplete")
		}
		if closeErr != nil {
			return nil, closeErr
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	var out []resolverruntime.RuntimeDHCPHostRecord
	for _, record := range parsed {
		source := resolverruntime.DHCPSourceForAddress(scopes, record.IP)
		if source == "" {
			continue
		}
		record.Source = source
		reservation := false
		for _, resource := range c.Router.Spec.Resources {
			if resource.APIVersion != api.NetAPIVersion || resource.Kind != "DHCPv4Reservation" {
				continue
			}
			spec, err := resource.DHCPv4ReservationSpec()
			if err != nil {
				return nil, err
			}
			server := spec.Server
			if server == "" {
				server = spec.Scope
			}
			if server != "" && "DHCPv4Server/"+server != source || !strings.EqualFold(spec.MACAddress, record.MAC) || spec.IPAddress != record.IP || !strings.EqualFold(spec.Hostname, record.Hostname) {
				continue
			}
			reservation = true
			break
		}
		if reservation {
			record.Origin = resolverruntime.DHCPAssignmentReservation
			out = append(out, record)
			continue
		}
		for _, row := range held {
			if strings.EqualFold(row.MAC, record.MAC) && row.IP == record.IP && strings.EqualFold(row.Hostname, record.Hostname) && now.Before(row.StickyUntil) {
				record.Origin = resolverruntime.DHCPAssignmentHeld
				record.ExpiresAt = row.StickyUntil
				out = append(out, record)
				break
			}
		}
	}
	return out, nil
}

func assignmentsForSources(records []resolverruntime.RuntimeDHCPHostRecord, sources []string) []resolverruntime.RuntimeDHCPHostRecord {
	var out []resolverruntime.RuntimeDHCPHostRecord
	for _, record := range records {
		if slices.Contains(sources, record.Source) {
			out = append(out, record)
		}
	}
	return out
}

func readDHCPHosts(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	const maxDHCPHosts = 8 << 20
	data, err := io.ReadAll(io.LimitReader(file, maxDHCPHosts+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDHCPHosts {
		return nil, fmt.Errorf("DHCP assignment sidecar exceeds %d bytes", maxDHCPHosts)
	}
	return data, nil
}
