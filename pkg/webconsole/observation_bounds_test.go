// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/subprocessio"
)

func TestObservationCommandRejectsOversizedOutput(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			limit := subprocessio.StdoutLimit
			redirect := ""
			if stream == "stderr" {
				limit = subprocessio.StderrLimit
				redirect = " >&2"
			}
			payload := filepath.Join(t.TempDir(), "payload")
			if err := os.WriteFile(payload, []byte(strings.Repeat("s", limit+1)), 0o600); err != nil {
				t.Fatal(err)
			}
			command := filepath.Join(t.TempDir(), "status")
			if err := os.WriteFile(command, []byte("#!/bin/sh\ncat \"$1\""+redirect+"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			data, err := commandOutputTimeout(context.Background(), 2*time.Second, command, payload)
			var limitErr *subprocessio.OutputLimitError
			if data != nil || !errors.As(err, &limitErr) || limitErr.Stream != stream {
				t.Fatalf("oversized %s became a usable observation: bytes=%d error=%v", stream, len(data), err)
			}
		})
	}
}

func TestMalformedLeaseExpiryIsUnavailableRatherThanInfinite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dnsmasq.leases")
	h := New(Options{DHCPLeasePaths: []string{path}})
	for _, expiry := range []string{"broken", "-1", "9223372036854775808"} {
		if err := os.WriteFile(path, []byte(expiry+" 00:11:22:33:44:55 192.0.2.9 history *\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("GET", "/api/v1/summary?resources=0&events=-1&connections=-1&firewallLogs=-1&dnsQueries=-1&trafficFlows=-1&vpn=0", nil)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		var snapshot Snapshot
		if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
			t.Fatal(err)
		}
		if len(snapshot.DHCPLeases) != 0 || snapshot.collected(SectionDHCPLeases) || len(snapshot.Errors) == 0 || !strings.Contains(strings.Join(snapshot.Errors, ";"), "invalid lease expiry") {
			t.Fatalf("expiry %q became a valid current lease: %+v", expiry, snapshot)
		}
	}
	if err := os.WriteFile(path, []byte("0 00:11:22:33:44:55 192.0.2.9 infinite *\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	leases, err := h.dhcpLeaseList()
	if err != nil || len(leases) != 1 || !leases[0].ExpiresAt.IsZero() {
		t.Fatalf("native zero expiry must remain an indefinite lease: %+v, %v", leases, err)
	}
}
