//go:build linux

// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestServePipelineProcessRestartRetainsPendingEvents(t *testing.T) {
	if dir := os.Getenv("ROUTERD_LOGGING_PROCESS_DIR"); dir != "" {
		err := serveCommand([]string{"--sandbox", "--root", dir, "--config", filepath.Join(dir, "router.yaml"), "--state-file", filepath.Join(dir, "state.db"), "--ledger-file", filepath.Join(dir, "ledger.db"), "--socket", filepath.Join(dir, "control.sock"), "--status-socket", filepath.Join(dir, "status.sock"), "--http-listen", "127.0.0.1:0", "--controllers", "observability-pipeline", "--graceful-stop-timeout", "0"}, io.Discard, os.Stderr)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	// Keep the owned Unix socket paths below the kernel's pathname limit.
	dir, err := os.MkdirTemp("", "rdlp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	t.Cleanup(func() {
		if t.Failed() {
			for _, name := range []string{"first.log", "second.log"} {
				data, _ := os.ReadFile(filepath.Join(dir, name))
				t.Logf("%s:\n%s", name, data)
			}
		}
	})
	received := make(chan string, 8)
	var firstDeliveries, secondDeliveries atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var push struct {
			Streams []struct {
				Values [][]string `json:"values"`
			} `json:"streams"`
		}
		if err := json.NewDecoder(r.Body).Decode(&push); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		for _, stream := range push.Streams {
			for _, value := range stream.Values {
				if len(value) != 2 {
					t.Errorf("invalid Loki value %v", value)
					w.WriteHeader(400)
					return
				}
				var event struct {
					Type    string `json:"type"`
					Message string `json:"message"`
					Cursor  string `json:"cursor"`
				}
				if err := json.Unmarshal([]byte(value[1]), &event); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if event.Type != "routerd.logging.process" {
					continue
				}
				if event.Cursor == "" {
					t.Error("real journal event has no cursor")
				}
				switch event.Message {
				case "before-restart":
					firstDeliveries.Add(1)
				case "while-stopped":
					secondDeliveries.Add(1)
				default:
					t.Errorf("unexpected process event %q", event.Message)
				}
				received <- event.Message
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	config := fmt.Sprintf(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: logging-process
spec:
  resources:
    - apiVersion: system.routerd.net/v1alpha1
      kind: ObservabilityPipeline
      metadata:
        name: persisted
      spec:
        logs:
          sinks:
            - type: loki
              loki:
                url: %s/loki/api/v1/push
`, receiver.URL)
	if err := os.WriteFile(filepath.Join(dir, "router.yaml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	start := func(name string) (*exec.Cmd, func()) {
		t.Helper()
		log, err := os.Create(filepath.Join(dir, name+".log"))
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(executable, "-test.run=^TestServePipelineProcessRestartRetainsPendingEvents$", "-test.timeout=30s")
		cmd.Env = append(os.Environ(), "ROUTERD_LOGGING_PROCESS_DIR="+dir)
		cmd.Stdout, cmd.Stderr = log, log
		if err := cmd.Start(); err != nil {
			log.Close()
			t.Fatal(err)
		}
		done := make(chan struct{})
		var waitErr error
		go func() { waitErr = cmd.Wait(); log.Close(); close(done) }()
		t.Cleanup(func() {
			select {
			case <-done:
				return
			default:
			}
			_ = cmd.Process.Kill()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("owned logging child did not exit after kill")
			}
		})
		stop := func() {
			t.Helper()
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
				if waitErr != nil {
					t.Fatalf("logging child: %v", waitErr)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("logging child did not stop")
			}
		}
		// Running status is persisted before serve finishes opening its APIs.
		// This scenario stops an initialized daemon, so wait for the actual
		// private control endpoint rather than signaling during bootstrap.
		readyCtx, readyCancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer readyCancel()
		if _, err := controlapi.NewUnixClient(filepath.Join(dir, "control.sock")).Status(readyCtx); err != nil {
			t.Fatalf("logging child API was not ready: %v", err)
		}
		return cmd, stop
	}
	first, stopFirst := start("first")
	statePath := filepath.Join(dir, "state.db")
	waitEventGeneration(t, func() bool {
		reader, err := routerstate.OpenSQLiteReadOnly(statePath)
		if err != nil {
			return false
		}
		defer reader.Close()
		status, err := reader.ReadObjectStatus(api.SystemAPIVersion, "ObservabilityPipeline", "persisted")
		return err == nil && status["phase"] == "Running"
	})
	publish := func(message string) int64 {
		t.Helper()
		writer, err := routerstate.OpenSQLite(statePath)
		if err != nil {
			t.Fatal(err)
		}
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "independent-process-input"}, "routerd.logging.process", daemonapi.SeverityInfo)
		event.Message = message
		cursor, writeErr := writer.RecordBusEvent(t.Context(), event)
		closeErr := writer.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatalf("journal input: %v, close: %v", writeErr, closeErr)
		}
		id, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	await := func(want string) {
		t.Helper()
		select {
		case got := <-received:
			if got != want {
				t.Fatalf("delivered %s, want %s", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no HTTP delivery for %s", want)
		}
	}
	waitCursor := func(want int64) {
		t.Helper()
		reader, err := sql.Open("sqlite", "file:"+statePath+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		waitEventGeneration(t, func() bool {
			var cursor int64
			if err := reader.QueryRowContext(context.Background(), `SELECT cursor FROM event_consumer_cursors WHERE consumer=?`, "observabilitypipeline/persisted").Scan(&cursor); err != nil {
				t.Fatal(err)
			}
			return cursor >= want
		})
	}
	firstCursor := publish("before-restart")
	await("before-restart")
	waitCursor(firstCursor)
	stopFirst()
	secondCursor := publish("while-stopped")
	second, stopSecond := start("second")
	if first.Process.Pid == second.Process.Pid {
		t.Fatal("restart reused the same process")
	}
	await("while-stopped")
	waitCursor(secondCursor)
	stopSecond()
	t.Logf("serve PIDs %d -> %d; persisted cursors %d -> %d; HTTP deliveries first=%d pending=%d", first.Process.Pid, second.Process.Pid, firstCursor, secondCursor, firstDeliveries.Load(), secondDeliveries.Load())
	if firstDeliveries.Load() != 1 || secondDeliveries.Load() != 1 {
		t.Fatalf("committed event replayed or pending event lost: first=%d pending=%d", firstDeliveries.Load(), secondDeliveries.Load())
	}
	// Neither process can be an in-memory substitute: the consumer position was
	// read through independent, read-only SQLite connections in each lifecycle.
	data, err := os.ReadFile(filepath.Join(dir, "router.yaml"))
	if err != nil || !strings.Contains(string(data), "kind: ObservabilityPipeline") {
		t.Fatalf("configuration was not preserved: %v", err)
	}
}
