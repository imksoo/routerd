// SPDX-License-Identifier: BSD-3-Clause

//go:build linux || freebsd

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPPPoEDaemonDeathSignalsNativeChild(t *testing.T) {
	if os.Getenv("ROUTERD_PPP_PARENT_FIXTURE") == "1" {
		child := exec.Command("sleep", "30")
		child.Stdout = os.Stdout
		if err := bindPPPoEChildLifetime(child); err != nil {
			os.Exit(2)
		}
		if err := child.Start(); err != nil {
			os.Exit(3)
		}
		fmt.Fprintln(os.Stdout, child.Process.Pid)
		_ = child.Wait()
		os.Exit(0)
	}
	parent := exec.Command(os.Args[0], "-test.run=^TestPPPoEDaemonDeathSignalsNativeChild$")
	parent.Env = append(os.Environ(), "ROUTERD_PPP_PARENT_FIXTURE=1")
	stdout, err := parent.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil {
		parent.Process.Kill()
		parent.Wait()
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		parent.Process.Kill()
		parent.Wait()
		t.Fatal(err)
	}
	needsCleanup := true
	t.Cleanup(func() {
		if !needsCleanup {
			return
		}
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
	})
	if err := parent.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	// The child retains stdout. EOF proves it exited too, not merely its daemon.
	if file, ok := stdout.(*os.File); ok {
		if err := file.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	_, readErr := io.ReadAll(reader)
	_ = parent.Wait()
	if readErr == nil {
		needsCleanup = false
	}
	if readErr != nil {
		t.Fatalf("native child survived daemon death: %v", readErr)
	}
}
