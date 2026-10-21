// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// These tests require durable diagnostic content across recovery. They use
// temporary files and a child test process; no host networking or SSH is used.
func TestLeaseSyncExitHelper(t *testing.T) {
	if os.Getenv("ROUTERD_LEASE_SYNC_TEST_HELPER") != "1" {
		return
	}
	fmt.Fprintln(os.Stderr, "rsync: source vanished; code 23")
	os.Exit(23)
}

func leaseSyncDiagnosticJob(t *testing.T, kind string) fileSyncJob {
	t.Helper()
	file := filepath.Join(t.TempDir(), "leases")
	if err := os.WriteFile(file, []byte("private lease content"), 0600); err != nil {
		t.Fatal(err)
	}
	return fileSyncJob{APIVersion: api.NetAPIVersion, Kind: kind, Name: "lan-leases",
		Sources: []fileSyncSource{{Name: "leaseFile", Path: file, Required: true}},
		Targets: []fileSyncTarget{{Host: "standby.example", SSHOptions: []string{"-o", "ProxyCommand=secret-option"}}}}
}

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

func TestLeaseSyncFailureSurvivesRecovery(t *testing.T) {
	for _, kind := range []string{"DHCPv4ServerLeaseSync", "DHCPv6ServerLeaseSync", "DHCPv6PrefixDelegationLeaseSync"} {
		t.Run(kind, func(t *testing.T) {
			job := leaseSyncDiagnosticJob(t, kind)
			var logs bytes.Buffer
			controller := FileSyncController{Store: mapStore{}, Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			controller.Command = func(context.Context, string, ...string) ([]byte, error) {
				cmd := exec.Command(os.Args[0], "-test.run=^TestLeaseSyncExitHelper$")
				cmd.Env = append(os.Environ(), "ROUTERD_LEASE_SYNC_TEST_HELPER=1")
				out, err := cmd.CombinedOutput()
				return out, fmt.Errorf("sync subprocess: %w", err)
			}
			for i := 0; i < 2; i++ {
				if err := controller.reconcileJob(context.Background(), job); err != nil {
					t.Fatal(err)
				}
			}
			records := leaseSyncDiagnosticRecords(t, logs.String())
			if len(records) != 1 {
				t.Fatalf("identical failure logs = %d, want 1", len(records))
			}
			failure := records[0]
			for key, want := range map[string]any{"level": "WARN", "kind": kind, "name": job.Name,
				"reason": "SyncFailed", "command": "rsync", "source": job.Sources[0].Path,
				"target": "standby.example", "exitCode": float64(23), "timedOut": false, "canceled": false} {
				if failure[key] != want {
					t.Fatalf("%s = %#v, want %#v", key, failure[key], want)
				}
			}
			if !strings.Contains(failure["output"].(string), "source vanished") || !strings.Contains(failure["error"].(string), "exit status 23") {
				t.Fatal(failure)
			}
			controller.Command = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
			if err := controller.reconcileJob(context.Background(), job); err != nil {
				t.Fatal(err)
			}
			status := controller.Store.ObjectStatus(job.APIVersion, job.Kind, job.Name)
			if status["phase"] != "Synced" || status["error"] != nil {
				t.Fatal(status)
			}
			records = leaseSyncDiagnosticRecords(t, logs.String())
			if len(records) != 2 || records[1]["msg"] != "DHCP lease synchronization recovered" || records[0]["exitCode"] != float64(23) {
				t.Fatal(records)
			}
			if strings.Contains(logs.String(), "secret-option") || strings.Contains(logs.String(), "private lease content") {
				t.Fatal("logged command options or lease contents")
			}
			controller.Command = func(context.Context, string, ...string) ([]byte, error) {
				return []byte("new failure"), errors.New("SSH connection closed")
			}
			if err := controller.reconcileJob(context.Background(), job); err != nil {
				t.Fatal(err)
			}
			if len(leaseSyncDiagnosticRecords(t, logs.String())) != 3 {
				t.Fatal("failure after recovery was suppressed")
			}
			job.Targets[0].Host = "another-standby.example"
			if err := controller.reconcileJob(context.Background(), job); err != nil {
				t.Fatal(err)
			}
			if len(leaseSyncDiagnosticRecords(t, logs.String())) != 4 {
				t.Fatal("changed failure target was suppressed")
			}
		})
	}
}

func TestLeaseSyncFailureContextCause(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint(deadline), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if deadline {
				ctx, cancel = context.WithDeadline(context.Background(), time.Unix(0, 0))
				defer cancel()
			}
			var logs bytes.Buffer
			c := FileSyncController{Store: mapStore{}, Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Command: func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("signal: killed") }}
			if err := c.reconcileJob(ctx, leaseSyncDiagnosticJob(t, "DHCPv4ServerLeaseSync")); err != nil {
				t.Fatal(err)
			}
			r := leaseSyncDiagnosticRecords(t, logs.String())[0]
			if r["timedOut"] != deadline || r["canceled"] == deadline || r["exitCode"] != nil {
				t.Fatal(r)
			}
		})
	}
}

type leaseSyncFailingStore struct{ mapStore }

func (s leaseSyncFailingStore) SaveObjectStatus(string, string, string, map[string]any) error {
	return errors.New("state storage unavailable")
}

func TestLeaseSyncLogsBeforeStateSaveFailure(t *testing.T) {
	var logs bytes.Buffer
	c := FileSyncController{Store: leaseSyncFailingStore{mapStore{}}, Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	err := c.save(api.NetAPIVersion, "DHCPv4ServerLeaseSync", "lan", map[string]any{"phase": "Error", "reason": "SourceStatFailed", "error": "cannot stat leases"})
	if err == nil || !strings.Contains(logs.String(), "cannot stat leases") {
		t.Fatalf("error=%v log=%s", err, logs.String())
	}
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

func TestLeaseSyncMissingAndDryRunDoNotLogFailures(t *testing.T) {
	var logs bytes.Buffer
	c := FileSyncController{Store: mapStore{}, Logger: slog.New(slog.NewJSONHandler(&logs, nil)), DryRun: true, Command: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("unexpected subprocess")
		return nil, nil
	}}
	job := leaseSyncDiagnosticJob(t, "DHCPv4ServerLeaseSync")
	if err := c.reconcileJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	c.DryRun = false
	job.Sources[0].Path = filepath.Join(t.TempDir(), "missing")
	if err := c.reconcileJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if logs.Len() != 0 {
		t.Fatal(logs.String())
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
