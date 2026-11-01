// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	resolverruntime "github.com/imksoo/routerd/pkg/dnsresolver"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

// The hosts sidecar describes intended DHCP assignments. It carries neither a
// named source nor a lease deadline, so it cannot itself become a live lease.
// Join it to current configured reservations or still-held sticky DB rows.
func (c Controller) namedDHCPAssignments(ctx context.Context, parsed []resolverruntime.RuntimeDHCPHostRecord, scopes []resolverruntime.DHCPAddressScope) ([]resolverruntime.RuntimeDHCPHostRecord, error) {
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
	configured := make(map[string]struct{})
	// Runtime selection normally supplies every active reservation through Router.
	// Retain a declaration without a when condition from DeclaredRouter as well:
	// a lease callback may arrive while the effective view is being refreshed, but
	// it must not turn an unconditional fixed assignment into a removable name.
	// The selected DHCP source remains the authority, so a withdrawn server never
	// receives a record from this fallback.
	for index, router := range []*api.Router{c.Router, c.DeclaredRouter} {
		if router == nil || index == 1 && router == c.Router {
			continue
		}
		for _, resource := range router.Spec.Resources {
			if resource.APIVersion != api.NetAPIVersion || resource.Kind != "DHCPv4Reservation" {
				continue
			}
			spec, err := resource.DHCPv4ReservationSpec()
			if err != nil {
				return nil, err
			}
			if index == 1 && resourcequery.ResourceWhenPresent(spec.When) {
				continue
			}
			server := spec.Server
			if server == "" {
				server = spec.Scope
			}
			source := "DHCPv4Server/" + server
			if server == "" || spec.Hostname == "" || spec.MACAddress == "" || spec.IPAddress == "" || !hasDHCPSource(scopes, source) {
				continue
			}
			record := resolverruntime.RuntimeDHCPHostRecord{Hostname: spec.Hostname, IP: spec.IPAddress, MAC: spec.MACAddress, Source: source, Origin: resolverruntime.DHCPAssignmentReservation}
			key := dhcpAssignmentKey(record)
			if _, found := configured[key]; found {
				continue
			}
			configured[key] = struct{}{}
			out = append(out, record)
		}
	}
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
			if _, found := configured[dhcpAssignmentKey(record)]; !found {
				out = append(out, record)
			}
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
	sort.Slice(out, func(i, j int) bool { return dhcpAssignmentKey(out[i]) < dhcpAssignmentKey(out[j]) })
	return out, nil
}

func hasDHCPSource(scopes []resolverruntime.DHCPAddressScope, source string) bool {
	return slices.ContainsFunc(scopes, func(scope resolverruntime.DHCPAddressScope) bool { return scope.Source == source })
}

func dhcpAssignmentKey(record resolverruntime.RuntimeDHCPHostRecord) string {
	return strings.ToLower(strings.TrimSpace(record.Source)) + "\x00" + strings.ToLower(strings.TrimSpace(record.Hostname)) + "\x00" + strings.TrimSpace(record.IP) + "\x00" + strings.ToLower(strings.TrimSpace(record.MAC))
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

// declaredReservationAssignments puts fixed intent into the runtime zone at
// the point where its DNS source is selected. It deliberately does not depend
// on the dnsmasq hosts sidecar: that file tracks renderer output and may change
// when a client releases its current lease. An unconditional declaration from
// DeclaredRouter is retained only while its named DHCP source remains selected.
func (c Controller) declaredReservationAssignments(scopes []resolverruntime.DHCPAddressScope, sources []string) []resolverruntime.RuntimeDHCPHostRecord {
	var out []resolverruntime.RuntimeDHCPHostRecord
	seen := map[string]struct{}{}
	for index, router := range []*api.Router{c.Router, c.DeclaredRouter} {
		if router == nil || index == 1 && router == c.Router {
			continue
		}
		for _, resource := range router.Spec.Resources {
			if resource.Kind != "DHCPv4Reservation" {
				continue
			}
			spec, err := resource.DHCPv4ReservationSpec()
			if err != nil || index == 1 && resourcequery.ResourceWhenPresent(spec.When) {
				continue
			}
			server := spec.Server
			if server == "" {
				server = spec.Scope
			}
			source := "DHCPv4Server/" + server
			if server == "" || spec.Hostname == "" || spec.MACAddress == "" || spec.IPAddress == "" || !slices.Contains(sources, source) || !hasDHCPSource(scopes, source) {
				continue
			}
			record := resolverruntime.RuntimeDHCPHostRecord{Hostname: spec.Hostname, IP: spec.IPAddress, MAC: spec.MACAddress, Source: source, Origin: resolverruntime.DHCPAssignmentReservation}
			key := dhcpAssignmentKey(record)
			if _, found := seen[key]; found {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, record)
		}
	}
	return out
}

func mergeDHCPAssignments(groups ...[]resolverruntime.RuntimeDHCPHostRecord) []resolverruntime.RuntimeDHCPHostRecord {
	seen := map[string]struct{}{}
	var out []resolverruntime.RuntimeDHCPHostRecord
	for _, records := range groups {
		for _, record := range records {
			key := dhcpAssignmentKey(record)
			if _, found := seen[key]; found {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool { return dhcpAssignmentKey(out[i]) < dhcpAssignmentKey(out[j]) })
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
