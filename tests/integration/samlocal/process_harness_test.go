// SPDX-License-Identifier: BSD-3-Clause

package samlocal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

type nodeProcess struct {
	Name     string
	Command  *exec.Cmd
	Ready    string
	ExitCode int
}

func startHelperNode(t *testing.T, ctx context.Context, name string) nodeProcess {
	t.Helper()
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestSAMLocalHelperProcess", "--")
	cmd.Env = append(os.Environ(),
		"SAMLOCAL_HELPER_PROCESS=1",
		"SAMLOCAL_NODE_NAME="+name,
		"SAMLOCAL_READY_FILE="+ready,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper node %s: %v", name, err)
	}
	waitForFile(t, ready, 2*time.Second)
	return nodeProcess{Name: name, Command: cmd, Ready: ready}
}

func stopHelperNode(t *testing.T, proc nodeProcess) {
	t.Helper()
	if proc.Command == nil || proc.Command.Process == nil {
		return
	}
	_ = proc.Command.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- proc.Command.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return
		}
		var exitErr *exec.ExitError
		if ok := errors.As(err, &exitErr); ok && exitErr.ExitCode() == 0 {
			return
		}
	case <-time.After(2 * time.Second):
		_ = proc.Command.Process.Kill()
		<-done
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func TestProcessHarnessStartsAndStopsNNodes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var nodes []nodeProcess
	for i := 0; i < 3; i++ {
		nodes = append(nodes, startHelperNode(t, ctx, fmt.Sprintf("leaf-%d", i)))
	}
	seen := map[int]bool{}
	for _, node := range nodes {
		if node.Command.Process == nil || node.Command.Process.Pid <= 0 {
			t.Fatalf("node %s has invalid pid", node.Name)
		}
		if seen[node.Command.Process.Pid] {
			t.Fatalf("duplicate pid %d", node.Command.Process.Pid)
		}
		seen[node.Command.Process.Pid] = true
	}
	for _, node := range nodes {
		stopHelperNode(t, node)
	}
}

func TestSAMLocalHelperProcess(t *testing.T) {
	if os.Getenv("SAMLOCAL_HELPER_PROCESS") != "1" {
		return
	}
	ready := os.Getenv("SAMLOCAL_READY_FILE")
	if ready == "" {
		fmt.Fprintln(os.Stderr, "SAMLOCAL_READY_FILE is required")
		os.Exit(2)
	}
	if err := os.WriteFile(ready, []byte(os.Getenv("SAMLOCAL_NODE_NAME")), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write ready file: %v\n", err)
		os.Exit(2)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	select {
	case <-sig:
		os.Exit(0)
	case <-time.After(helperProcessTimeout()):
		os.Exit(0)
	}
}

func helperProcessTimeout() time.Duration {
	if raw := os.Getenv("SAMLOCAL_HELPER_TIMEOUT_MS"); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return 30 * time.Second
}
