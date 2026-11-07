// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func leaseSyncDiagnosticRecords(t *testing.T, output string) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("invalid structured log: %v: %s", err, line)
		}
		records = append(records, record)
	}
	return records
}

func TestLeaseSyncDiagnosticBounds(t *testing.T) {
	var logs bytes.Buffer
	c := FileSyncController{Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	output := strings.Repeat("あ", 2000) + "\xff\nfinal failure"
	c.logFileSyncFailure(api.NetAPIVersion, "DHCPv4ServerLeaseSync", "lan", map[string]any{"reason": "SyncFailed", "output": output, "sshOptions": "secret", "error": "exit status 23"})
	r := leaseSyncDiagnosticRecords(t, logs.String())[0]
	text := r["output"].(string)
	if len(text) > fileSyncDiagnosticLimit || !utf8.ValidString(text) || !strings.HasSuffix(text, "final failure") || r["outputTruncated"] != true || r["sshOptions"] != nil {
		t.Fatal(r)
	}
}

func TestLeaseSyncDuplicateFailureWithSQLiteStatus(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var logs bytes.Buffer
	c := FileSyncController{Store: store, Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	for i := 0; i < 2; i++ {
		// The real store round-trip decodes an int exit code as float64.
		failure := map[string]any{"phase": "Error", "reason": "SyncFailed", "exitCode": 23, "error": "exit status 23", "output": "source vanished", "timedOut": false, "canceled": false}
		if err := c.save(api.NetAPIVersion, "DHCPv4ServerLeaseSync", "lan", failure); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(leaseSyncDiagnosticRecords(t, logs.String())); got != 1 {
		t.Fatalf("failure logs=%d want 1", got)
	}
}
