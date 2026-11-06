//go:build linux

// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/controlapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestServeDerivedEventFeedsRuleAcrossApplyAndRestart(t *testing.T) {
	const env = "ROUTERD_EVENTS_PROCESS_DIR"
	if dir := os.Getenv(env); dir != "" {
		if err := serveCommand([]string{"--sandbox", "--root", dir, "--config", filepath.Join(dir, "router.yaml"), "--state-file", filepath.Join(dir, "state.db"), "--ledger-file", filepath.Join(dir, "ledger.db"), "--socket", filepath.Join(dir, "control.sock"), "--status-socket", filepath.Join(dir, "status.sock"), "--http-listen", "127.0.0.1:0", "--controllers", "event-rule,derived-event", "--graceful-stop-timeout", "0"}, io.Discard, os.Stderr); err != nil {
			t.Fatal(err)
		}
		return
	}
	dir, err := os.MkdirTemp("", "rdev-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	candidate := func(input string) string {
		return fmt.Sprintf(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: events-process}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: DerivedEvent
      metadata: {name: source}
      spec:
        topic: routerd.process.source
        emitInitial: true
        inputs: [{field: %q, equals: enabled}]
    - apiVersion: net.routerd.net/v1alpha1
      kind: EventRule
      metadata: {name: consumer}
      spec:
        pattern:
          operator: any_of
          topics: [routerd.process.source.asserted, routerd.process.source.retracted]
        emit: {topic: routerd.process.effect}
`, input)
	}
	if err := os.WriteFile(filepath.Join(dir, "router.yaml"), []byte(candidate("enabled")), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	client := controlapi.NewUnixClient(filepath.Join(dir, "control.sock"))
	start := func(name string) func() {
		t.Helper()
		log, err := os.Create(filepath.Join(dir, name+".log"))
		if err != nil {
			t.Fatal(err)
		}
		child := exec.Command(executable, "-test.run=^TestServeDerivedEventFeedsRuleAcrossApplyAndRestart$", "-test.timeout=30s")
		child.Env = append(os.Environ(), env+"="+dir)
		child.Stdout, child.Stderr = log, log
		if err := child.Start(); err != nil {
			_ = log.Close()
			t.Fatal(err)
		}
		done := make(chan struct{})
		var waitErr error
		go func() { waitErr = child.Wait(); _ = log.Close(); close(done) }()
		t.Cleanup(func() {
			select {
			case <-done:
			default:
				_ = child.Process.Kill()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Error("event process did not exit after kill")
				}
			}
			if t.Failed() {
				data, _ := os.ReadFile(filepath.Join(dir, name+".log"))
				t.Log(string(data))
			}
		})
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		if _, err := client.Status(ctx); err != nil {
			t.Fatal(err)
		}
		return func() {
			t.Helper()
			if err := child.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
				if waitErr != nil {
					t.Fatal(waitErr)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("event daemon did not stop")
			}
		}
	}
	assertCount := func(want int) {
		t.Helper()
		waitEventGeneration(t, func() bool {
			reader, err := routerstate.OpenSQLiteReadOnly(filepath.Join(dir, "state.db"))
			if err != nil {
				return false
			}
			defer reader.Close()
			cp, err := reader.ReadEventRule(t.Context(), "consumer")
			rows, listErr := reader.ListEvents(routerstate.EventQuery{Topic: "routerd.process.effect", Limit: 20})
			return err == nil && listErr == nil && cp.FireCount == want && len(rows) == want
		})
	}
	stop := start("first")
	assertCount(1)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err := client.Apply(ctx, controlapi.ApplyRequest{CandidateYAML: candidate("disabled"), Replace: true}); err != nil {
		t.Fatal(err)
	}
	assertCount(2)
	stop()
	stop = start("second")
	assertCount(2)
	if _, err := client.Apply(ctx, controlapi.ApplyRequest{CandidateYAML: candidate("enabled"), Replace: true}); err != nil {
		t.Fatal(err)
	}
	assertCount(3)
	stop()
}
