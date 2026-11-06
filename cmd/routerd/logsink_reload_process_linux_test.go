//go:build linux

// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/eventlog"
)

func TestServeLogSinkFollowsCommittedConfiguration(t *testing.T) {
	if dir := os.Getenv("ROUTERD_LOGSINK_RELOAD_TEST"); dir != "" {
		if err := serveCommand([]string{"--sandbox", "--root", dir, "--config", filepath.Join(dir, "router.yaml"), "--state-file", filepath.Join(dir, "state.db"), "--ledger-file", filepath.Join(dir, "ledger.db"), "--socket", filepath.Join(dir, "control.sock"), "--status-socket", filepath.Join(dir, "status.sock"), "--http-listen", "127.0.0.1:0", "--controllers", "observability-pipeline", "--graceful-stop-timeout", "0"}, io.Discard, os.Stderr); err != nil {
			t.Fatal(err)
		}
		return
	}
	dir, err := os.MkdirTemp("", "rdlr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	var oldCount, newCount atomic.Int32
	receiver := func(count *atomic.Int32) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var event eventlog.Event
			if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			if event.Message == "log level override changed" {
				count.Add(1)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
	}
	oldReceiver, newReceiver := receiver(&oldCount), receiver(&newCount)
	defer oldReceiver.Close()
	defer newReceiver.Close()
	candidate := func(url string) string {
		return fmt.Sprintf(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: reload-logs}
spec:
  resources:
    - apiVersion: system.routerd.net/v1alpha1
      kind: LogSink
      metadata: {name: remote}
      spec:
        type: webhook
        webhook: {url: %q, timeout: 1s}
`, url)
	}
	if err := os.WriteFile(filepath.Join(dir, "router.yaml"), []byte(candidate(oldReceiver.URL)), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(dir, "process.log"))
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestServeLogSinkFollowsCommittedConfiguration$", "-test.timeout=30s")
	child.Env = append(os.Environ(), "ROUTERD_LOGSINK_RELOAD_TEST="+dir)
	child.Stdout, child.Stderr = log, log
	if err := child.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = child.Wait(); _ = log.Close(); close(done) }()
	t.Cleanup(func() {
		_ = child.Process.Kill()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("logging test process did not stop")
		}
		if t.Failed() {
			data, _ := os.ReadFile(filepath.Join(dir, "process.log"))
			t.Log(string(data))
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	client := controlapi.NewUnixClient(filepath.Join(dir, "control.sock"))
	if _, err := client.Status(ctx); err != nil {
		t.Fatal(err)
	}
	emit := func(level string) {
		t.Helper()
		if _, err := client.SetLogLevel(ctx, controlapi.LogLevelRequest{Level: level}); err != nil {
			t.Fatal(err)
		}
	}
	emit("debug")
	if oldCount.Load() != 1 || newCount.Load() != 0 {
		t.Fatalf("initial delivery: old=%d new=%d", oldCount.Load(), newCount.Load())
	}
	if _, err := client.Apply(ctx, controlapi.ApplyRequest{CandidateYAML: candidate(newReceiver.URL), Replace: true}); err != nil {
		t.Fatal(err)
	}
	emit("info")
	if oldCount.Load() != 1 || newCount.Load() != 1 {
		t.Fatalf("committed destination not activated: old=%d new=%d", oldCount.Load(), newCount.Load())
	}
	empty := "apiVersion: routerd.net/v1alpha1\nkind: Router\nmetadata: {name: reload-logs}\nspec: {resources: []}\n"
	if _, err := client.Apply(ctx, controlapi.ApplyRequest{CandidateYAML: empty, Replace: true}); err != nil {
		t.Fatal(err)
	}
	emit("default")
	if oldCount.Load() != 1 || newCount.Load() != 1 {
		t.Fatalf("removed sink still received events: old=%d new=%d", oldCount.Load(), newCount.Load())
	}
}
