// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/logstore"
)

// These local SQLite fixtures exercise the tuning summary's read-only and
// cancellation contracts through both the helper and HTTP route. They do not
// measure live router traffic or impose latency acceptance thresholds.
func TestSummaryTuningMissingDatabaseDoesNotCreateFiles(t *testing.T) {
	for _, nested := range []bool{false, true} {
		name := "missing database"
		if nested {
			name = "missing directory"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "firewall.db")
			if nested {
				path = filepath.Join(dir, "missing", "firewall.db")
			}
			h := New(Options{FirewallLogPath: path})
			snapshot := requestTuningSnapshot(t, h, context.Background())
			if len(snapshot.Errors) != 0 {
				t.Fatalf("missing optional database returned errors: %v", snapshot.Errors)
			}
			tuning := snapshot.ConntrackTuning
			if tuning == nil || len(tuning.Suggestions) != 0 || tuning.ApplyMode != "manual" || tuning.AutoApply {
				t.Fatalf("missing optional database summary = %+v, want empty manual summary", tuning)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("GET created files or directories: %v", entries)
			}
		})
	}
}

func TestSummaryTuningPropagatesRequestCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "firewall.db")
	h := New(Options{FirewallLogPath: path})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.conntrackTuningSummary(ctx, time.Now(), 24*time.Hour, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled tuning error = %v, want context.Canceled", err)
	}
	snapshot := requestTuningSnapshot(t, h, ctx)
	if snapshot.ConntrackTuning != nil || !slices.Contains(snapshot.Errors, context.Canceled.Error()) {
		t.Fatalf("cancelled summary: tuning=%+v errors=%v", snapshot.ConntrackTuning, snapshot.Errors)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("cancelled GET created database directory: %v", err)
	}
}

func TestSummaryTuningAnalyzesExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall.db")
	store, err := logstore.OpenFirewallLog(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close fixture database: %v", err)
		}
	})
	now := time.Now().UTC()
	ctx := context.Background()
	if err := store.RecordDPIFlow(ctx, logstore.DPIFlowEntry{
		FirstSeen: now.Add(-time.Minute), LastSeen: now,
		Protocol: "tcp", L3Proto: "ipv4", AppName: "tls",
		SrcAddress: "192.0.2.2", SrcPort: 12345,
		DstAddress: "198.51.100.1", DstPort: 443,
	}, time.Hour, 100); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(ctx, logstore.FirewallLogEntry{
		Timestamp: now, Action: "drop", Protocol: "tcp", L3Proto: "ipv4",
		SrcAddress: "198.51.100.1", SrcPort: 443,
		DstAddress: "192.0.2.2", DstPort: 12345,
		DPIApp: "tls", Correlation: "orphan_return",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordExpiredFlow(ctx, logstore.ExpiredFlowEntry{
		Timestamp: now, Protocol: "tcp", L3Proto: "ipv4",
		OrigSrc: "192.0.2.2", OrigSrcPort: 12345,
		OrigDst: "198.51.100.1", OrigDstPort: 443,
	}, time.Hour, 100); err != nil {
		t.Fatal(err)
	}
	h := New(Options{FirewallLogPath: path})
	snapshot := requestTuningSnapshot(t, h, ctx)
	if len(snapshot.Errors) != 0 {
		t.Fatalf("existing database returned errors: %v", snapshot.Errors)
	}
	tuning := snapshot.ConntrackTuning
	if tuning == nil || len(tuning.Suggestions) != 1 {
		t.Fatalf("existing database summary = %+v, want one suggestion", tuning)
	}
	suggestion := tuning.Suggestions[0]
	if suggestion.Application != "tls" || suggestion.Protocol != "tcp" || suggestion.ObservedFlows != 1 || suggestion.ExpiredFlows != 1 || suggestion.OrphanReturns != 1 || suggestion.DenyEvents != 1 {
		t.Fatalf("summary lost recorded observations: %+v", suggestion)
	}
}

func TestSummaryTuningReportsCorruptDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall.db")
	if err := os.WriteFile(path, []byte("not a SQLite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot := requestTuningSnapshot(t, New(Options{FirewallLogPath: path}), context.Background())
	if snapshot.ConntrackTuning != nil || len(snapshot.Errors) == 0 {
		t.Fatalf("corrupt database was treated as empty: tuning=%+v errors=%v", snapshot.ConntrackTuning, snapshot.Errors)
	}
}

func requestTuningSnapshot(t *testing.T, h Handler, ctx context.Context) Snapshot {
	t.Helper()
	// Disable unrelated optional observations so cancellation evidence belongs
	// to the tuning read, and no connection, DNS or VPN probes are required.
	path := "/api/v1/summary?tuning=1&connections=-1&events=-1&firewallLogs=-1&dnsQueries=-1&trafficFlows=-1&vpn=0&dhcpLeases=0&resources=0"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx))
	t.Logf("summary status=%d body=%s", rec.Code, rec.Body.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("summary status = %d", rec.Code)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	return snapshot
}
