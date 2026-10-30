// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/pdclient"
)

func TestAddressRequestModeOptions(t *testing.T) {
	for _, mode := range []string{"pd", "address", "pd-address"} {
		if _, err := parseOptions("daemon", []string{"--interface", "wan0", "--request-mode", mode}); err != nil {
			t.Fatalf("valid request mode %s rejected: %v", mode, err)
		}
	}
	if _, err := parseOptions("daemon", []string{"--interface", "wan0", "--request-mode", "unknown"}); err == nil {
		t.Fatal("unknown association mode accepted")
	}
}

func TestAddressExpiryPersistsWithoutPrefixOrExchangeChange(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	client, err := pdclient.New(pdclient.Config{Resource: "wan-address", Interface: "wan0", ClientDUID: []byte{1}, IAID: 7, Mode: pdclient.RequestAddresses, Now: func() time.Time { return now }, Random: func() float64 { return .5 }}, &daemonMemoryTransport{})
	if err != nil {
		t.Fatal(err)
	}
	// A server can assign multiple addresses with different valid lifetimes.
	// The short address expires before either IA timer or any packet exchange.
	client.State = pdclient.StateBound
	client.Lease = pdclient.Lease{ServerDUID: []byte{2}, IAID: 7, AddressAcquiredAt: now, AddressT1: 60 * time.Second, AddressT2: 90 * time.Second,
		Addresses: []pdclient.AddressLease{
			{Address: netip.MustParseAddr("2001:db8::1"), Preferred: 5 * time.Second, Valid: 10 * time.Second, AcquiredAt: now},
			{Address: netip.MustParseAddr("2001:db8::2"), Preferred: 90 * time.Second, Valid: 120 * time.Second, AcquiredAt: now},
		}}
	dir := t.TempDir()
	d := &dhcpv6Daemon{opts: options{resource: "wan-address", ifname: "wan0", leaseFile: filepath.Join(dir, "lease.json"), eventFile: filepath.Join(dir, "events.jsonl")}, client: client, recorder: &packetRecorder{limit: 10}}
	d.cond = sync.NewCond(&d.mu)
	d.initTelemetry()
	if err := d.saveLeaseLocked(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Second)
	if err := d.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(d.opts.leaseFile)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot pdclient.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Addresses) != 1 || snapshot.Addresses[0].Address != "2001:db8::2" {
		t.Fatalf("expired secondary address remains on disk: %+v", snapshot.Addresses)
	}
	status := d.statusLocked().Resources[0]
	if status.Resource.Kind != "DHCPv6Address" || status.Phase != daemonapi.ResourcePhaseBound || status.Conditions[0].Status != "True" {
		t.Fatalf("address-only status mislabeled: %+v", status)
	}
	changed := false
	for _, event := range d.events {
		if event.Type == daemonapi.EventDHCPv6PrefixBound {
			t.Fatal("address-only acquisition emitted prefix-bound event")
		}
		if event.Type == daemonapi.EventDHCPv6AddressesChanged && event.Attributes["addressCount"] == "1" {
			changed = true
		}
	}
	if !changed {
		t.Fatal("secondary expiry was not observable through address event")
	}
}

func TestPartialNoBindingTickDoesNotRewriteUnchangedLease(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	client, err := pdclient.New(pdclient.Config{Resource: "wan-address", Interface: "wan0", ClientDUID: []byte{1}, IAID: 7, Mode: pdclient.RequestPrefixAndAddress, Now: func() time.Time { return now }}, &daemonMemoryTransport{})
	if err != nil {
		t.Fatal(err)
	}
	// NoBinding recovery is requesting internally while the still-valid sibling
	// association remains publicly Bound. A tick with no reply changes neither.
	client.State = pdclient.StateRequesting
	client.Lease = pdclient.Lease{ServerDUID: []byte{2}, IAID: 7, AddressAcquiredAt: now, AddressT1: time.Minute, AddressT2: 2 * time.Minute,
		Addresses: []pdclient.AddressLease{{Address: netip.MustParseAddr("2001:db8::1"), Preferred: 3 * time.Minute, Valid: 4 * time.Minute, AcquiredAt: now}}}
	dir := t.TempDir()
	d := &dhcpv6Daemon{opts: options{resource: "wan-address", leaseFile: filepath.Join(dir, "lease.json"), eventFile: filepath.Join(dir, "events.jsonl")}, client: client, recorder: &packetRecorder{limit: 10}}
	d.cond = sync.NewCond(&d.mu)
	d.initTelemetry()
	if err := d.saveLeaseLocked(context.Background()); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1000, 0)
	if err := os.Chtimes(d.opts.leaseFile, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := d.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(d.opts.leaseFile)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(stamp) || len(d.events) != 0 {
		t.Fatal("unchanged partial lease rewrote storage or fabricated a binding event")
	}
}
