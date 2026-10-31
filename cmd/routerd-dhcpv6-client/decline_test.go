// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/pdclient"
)

func TestDeclineCommandBindsDADReportToCurrentLeaseAndWorker(t *testing.T) {
	for _, tc := range []struct {
		name, owner, server string
		stale               bool
		accepted            bool
	}{
		{"current", "owner", "02", false, true},
		{"old-owner", "old-owner", "02", false, false},
		{"old-server", "owner", "03", false, false},
		{"old-lease", "owner", "02", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			transport := &daemonMemoryTransport{}
			client, err := pdclient.New(pdclient.Config{Resource: "worker", Interface: "wan0", ClientDUID: []byte{1}, IAID: 7, Mode: pdclient.RequestAddresses, Now: func() time.Time { return now }}, transport)
			if err != nil {
				t.Fatal(err)
			}
			client.State = pdclient.StateBound
			client.Lease = pdclient.Lease{ServerDUID: []byte{2}, IAID: 7, AddressAcquiredAt: now, AddressT1: time.Minute, AddressT2: 2 * time.Minute, Addresses: []pdclient.AddressLease{{Address: netip.MustParseAddr("2001:db8::1"), Preferred: 3 * time.Minute, Valid: 4 * time.Minute, AcquiredAt: now}}}
			dir := t.TempDir()
			opts, err := parseOptions("daemon", []string{"--resource", "worker", "--interface", "wan0", "--request-mode", "address", "--supervisor-owner", "owner", "--lease-file", filepath.Join(dir, "lease.json"), "--event-file", filepath.Join(dir, "events.jsonl")})
			if err != nil {
				t.Fatal(err)
			}
			d := &dhcpv6Daemon{opts: opts, client: client, recorder: &packetRecorder{limit: 10}}
			d.cond = sync.NewCond(&d.mu)
			d.initTelemetry()
			acquired := now
			if tc.stale {
				acquired = acquired.Add(-time.Second)
			}
			body := fmt.Sprintf(`{"supervisorOwner":%q,"serverDUID":%q,"addresses":[{"address":"2001:db8::1","acquiredAt":%q}]}`, tc.owner, tc.server, acquired.Format(time.RFC3339Nano))
			request := httptest.NewRequest(http.MethodPost, "/v1/commands/decline", strings.NewReader(body))
			response := httptest.NewRecorder()
			d.handleCommand(response, request)
			var result daemonapi.CommandResult
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Accepted != tc.accepted {
				t.Fatalf("Decline acceptance=%t, want %t: %s", result.Accepted, tc.accepted, response.Body.String())
			}
			if tc.accepted && len(client.Snapshot().Addresses) != 0 {
				t.Fatal("accepted DAD failure retained address")
			}
			if !tc.accepted && len(client.Snapshot().Addresses) != 1 {
				t.Fatal("stale DAD report revoked current binding")
			}
			leaseResponse := httptest.NewRecorder()
			d.handleLease(leaseResponse, httptest.NewRequest(http.MethodGet, "/v1/lease", nil))
			if leaseResponse.Header().Get("X-Routerd-Supervisor-Owner") != "owner" {
				t.Fatal("lease read is not bound to its supervisor owner")
			}
		})
	}
}
