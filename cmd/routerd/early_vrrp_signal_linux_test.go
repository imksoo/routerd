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
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestServeQueuesVRRPTransitionBeforeInitialConfig(t *testing.T) {
	if dir := os.Getenv("ROUTERD_EARLY_VRRP_DIR"); dir != "" {
		err := serveCommand([]string{"--sandbox", "--root", dir,
			"--config", filepath.Join(dir, "router.yaml"), "--state-file", filepath.Join(dir, "state.db"),
			"--ledger-file", filepath.Join(dir, "ledger.db"), "--socket", filepath.Join(dir, "control.sock"),
			"--status-socket", filepath.Join(dir, "status.sock"), "--http-listen", "127.0.0.1:0",
			"--controllers", "observability-pipeline", "--graceful-stop-timeout", "0"}, io.Discard, os.Stderr)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	dir, err := os.MkdirTemp("", "rdusr1-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	fifo := filepath.Join(dir, "router.yaml")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(dir, "child.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestServeQueuesVRRPTransitionBeforeInitialConfig$", "-test.timeout=20s")
	cmd.Env = append(os.Environ(), "ROUTERD_EARLY_VRRP_DIR="+dir)
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
		default:
			_ = cmd.Process.Kill()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("owned serve child did not exit")
			}
		}
		if t.Failed() {
			data, _ := os.ReadFile(filepath.Join(dir, "child.log"))
			t.Logf("child output:\n%s", data)
		}
	})
	// A write-only nonblocking open succeeds only after the production config
	// reader has opened this FIFO. Keep it empty so initialization cannot advance.
	var writer *os.File
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		writer, err = os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			break
		}
		select {
		case <-done:
			t.Fatalf("serve exited before config read: %v", waitErr)
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("serve did not reach initial config read: %v", err)
	}
	defer writer.Close()
	if err := cmd.Process.Signal(syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	// The reader remains blocked throughout delivery. Surviving this interval
	// alone is insufficient: the persisted role event below must also appear.
	select {
	case <-done:
		t.Fatalf("VRRP notification killed serve before initialization: %v", waitErr)
	case <-time.After(250 * time.Millisecond):
	}
	config := []byte(testRouterYAML("early-vrrp"))
	replacement := filepath.Join(dir, "ready.yaml")
	if err := os.WriteFile(replacement, config, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, fifo); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(config); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client := controlapi.NewUnixClient(filepath.Join(dir, "status.sock"))
	ready := false
	for ctx.Err() == nil {
		if _, err := client.Status(ctx); err == nil {
			ready = true
			break
		}
		select {
		case <-done:
			t.Fatalf("serve exited after config release: %v", waitErr)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("private status API did not become ready: %v", ctx.Err())
	}
	reader, err := routerstate.OpenSQLiteReadOnly(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	delivered := false
	for ctx.Err() == nil {
		events, err := reader.ListEvents(routerstate.EventQuery{Topic: daemonapi.EventVRRPRoleTransition})
		if err != nil {
			t.Fatal(err)
		}
		if len(events) == 1 {
			delivered = true
			break
		}
		if len(events) > 1 {
			t.Fatalf("one notification produced %d events", len(events))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !delivered {
		t.Fatal("early VRRP notification was discarded instead of published")
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		if waitErr != nil {
			t.Fatalf("serve shutdown: %v", waitErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owned serve child did not acknowledge shutdown")
	}
	t.Log("initial config FIFO barrier -> real SIGUSR1 -> status API -> durable VRRP event -> child Wait exit0")
}
