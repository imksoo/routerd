//go:build linux

// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestServeStopsDuringInitialObservation(t *testing.T) {
	if os.Getenv("ROUTERD_INITIAL_OBSERVATION_CHILD") == "1" {
		dir := os.Getenv("ROUTERD_INITIAL_OBSERVATION_DIR")
		err := serveCommand([]string{"--config", filepath.Join(dir, "router.yaml"), "--state-file", filepath.Join(dir, "state.db"), "--ledger-file", filepath.Join(dir, "ledger.db"), "--status-file", filepath.Join(dir, "status.json"), "--controllers", "log-retention", "--graceful-stop-timeout", "0"}, io.Discard, os.Stderr)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "router.yaml"), []byte(testRouterYAML("initial-observe")), 0600); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(dir, "observing")
	ip := "#!/bin/sh\nif [ \"$*\" = 'link set lo up' ]; then exit 0; fi\n: > \"$ROUTERD_INITIAL_OBSERVATION_READY\"\nexec /bin/sleep 30\n"
	if err := os.WriteFile(filepath.Join(dir, "ip"), []byte(ip), 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestServeStopsDuringInitialObservation$")
	cmd.Env = append(os.Environ(), "ROUTERD_INITIAL_OBSERVATION_CHILD=1", "ROUTERD_INITIAL_OBSERVATION_DIR="+dir, "ROUTERD_INITIAL_OBSERVATION_READY="+ready, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("serve left before initial observation: %v\n%s", err, output.String())
		case <-deadline.C:
			t.Fatal("serve did not reach the test-owned initial observation")
		case <-tick.C:
		}
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("initial observation SIGTERM: %v\n%s", err, output.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SIGTERM remained queued behind initial observation")
	}
}
