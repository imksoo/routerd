// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dhcpeventqueue"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/state"
)

func TestHistoricalCallbacksRequireCurrentAuthoritativeLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.leases")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{Kind: "DHCPv4Server"}, Spec: api.DHCPv4ServerSpec{LeaseFile: path}}}}}
	now := time.Date(2026, 10, 21, 1, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name, action, mac, ip, hostname, data, want string
		bad                                         bool
	}{
		{"anonymous current", "renewed", "02:00:00:00:00:42", "192.0.2.42", "", "0 02:00:00:00:00:42 192.0.2.42 * *\n", "", false},
		{"current infinite", "renewed", "02:00:00:00:00:42", "192.0.2.42", "fixture", "0 02:00:00:00:00:42 192.0.2.42 fixture *\n", "", false},
		{"removed while current", "removed", "02:00:00:00:00:42", "192.0.2.42", "fixture", "0 02:00:00:00:00:42 192.0.2.42 fixture *\n", "history-only-lease-state-changed", false},
		{"different MAC same IP", "removed", "02:00:00:00:00:42", "192.0.2.42", "fixture", "0 02:00:00:00:00:43 192.0.2.42 newer *\n", "history-only-lease-state-changed", false},
		{"renewed old hostname", "renewed", "02:00:00:00:00:42", "192.0.2.42", "old", "0 02:00:00:00:00:42 192.0.2.42 new *\n", "history-only-lease-state-changed", false},
		{"expired renewal", "renewed", "02:00:00:00:00:42", "192.0.2.42", "fixture", "1 02:00:00:00:00:42 192.0.2.42 fixture *\n", "history-only-lease-state-changed", false},
		{"absent removal", "removed", "02:00:00:00:00:42", "192.0.2.42", "fixture", "", "", false},
		{"IPv6 identity not inferred", "renewed", "00:01:fixture", "2001:db8::42", "fixture", "duid 00:01:00:01\n0 42 2001:db8::42 fixture 00:01:fixture\n", "history-only-unverified-dhcpv6-identity", false},
		{"malformed", "removed", "02:00:00:00:00:42", "192.0.2.42", "fixture", "not a lease\n", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tt.data), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := dhcpReplayDisposition(router, "/unselected/fallback", controlapi.DHCPLeaseEventRequest{Action: tt.action, MAC: tt.mac, IP: tt.ip, Hostname: tt.hostname}, now)
			if (err != nil) != tt.bad || got != tt.want {
				t.Fatalf("got %q %v, want %q bad=%v", got, err, tt.want, tt.bad)
			}
		})
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := dhcpReplayDisposition(router, path, controlapi.DHCPLeaseEventRequest{Action: "removed", IP: "192.0.2.42"}, now); err == nil {
		t.Fatal("missing authority treated as empty lease list")
	}
}

func TestOriginalObservationTimePersistsInActualJournalAndZeroHoldDoesNotReserve(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.db")
	stickyPath := filepath.Join(root, "sticky.db")
	store, err := state.OpenSQLite(statePath)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 1, 1, 2, 3, 456789, time.UTC)
	e := dhcpeventqueue.Event{ObservedAt: at, Lease: controlapi.DHCPLeaseEventRequest{Action: "removed", MAC: "02:00:00:00:00:42", IP: "192.0.2.42"}}
	if err := processDHCPLeaseEvent(context.Background(), e, stickyPath, 0, bus.NewWithStore(store)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = state.OpenSQLite(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	events, err := store.ListEvents(state.EventQuery{Topic: daemonapi.EventDHCPLeaseRemoved})
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	if events[0].Attributes["observedAt"] != at.Format(time.RFC3339Nano) {
		t.Fatalf("lost original timestamp: %+v", events[0])
	}
	sticky, err := logstore.OpenDHCPStickyLog(stickyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sticky.Close()
	rows, err := sticky.List(context.Background(), logstore.DHCPStickyFilter{HeldOnly: true})
	if err != nil || len(rows) != 0 {
		t.Fatal("zero hold reserved address", rows, err)
	}
	rows, err = sticky.List(context.Background(), logstore.DHCPStickyFilter{})
	if err != nil || len(rows) != 1 || !rows[0].ReleasedAt.Equal(at) {
		t.Fatal(rows, err)
	}
}
