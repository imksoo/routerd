//go:build linux

// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNotifyRouterdVRRPTransitionDoesNotSignalReplacement(t *testing.T) {
	start := func() (*exec.Cmd, <-chan error) {
		t.Helper()
		cmd := exec.Command("/bin/sleep", "30")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait(); close(done) }()
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("owned notification target did not exit")
			}
		})
		return cmd, done
	}
	original, originalDone := start()
	replacement, replacementDone := start()
	reads, kills := 0, 0
	err := notifyRouterdVRRPTransition(func(name string, args ...string) ([]byte, error) {
		if name != "systemctl" || len(args) == 0 {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		switch args[0] {
		case "show":
			reads++
			if reads == 1 {
				return []byte(fmt.Sprintf("MainPID=%d\nActiveState=active\nSubState=running\n", original.Process.Pid)), nil
			}
			return []byte(fmt.Sprintf("MainPID=%d\nActiveState=activating\nSubState=start\n", replacement.Process.Pid)), nil
		case "kill":
			// The existing systemctl boundary resolves the unit's current main PID;
			// a restart has already selected this new, not-yet-ready process.
			kills++
			return nil, replacement.Process.Signal(syscall.SIGUSR1)
		default:
			t.Fatalf("unexpected systemctl command %s", strings.Join(args, " "))
		}
		return nil, nil
	})
	if err == nil || reads != 2 || kills != 0 {
		var outcome error
		select {
		case outcome = <-replacementDone:
		case <-time.After(time.Second):
		}
		t.Fatalf("unit-name notification reached replacement: reads=%d kills=%d replacement=%v err=%v", reads, kills, outcome, err)
	}
	select {
	case err := <-originalDone:
		t.Fatalf("stale ready process was signaled after replacement: %v", err)
	case err := <-replacementDone:
		t.Fatalf("new unready process was signaled: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	// Empty first values are still duplicate fields, not a missing first read.
	ambiguousReads := 0
	err = notifyRouterdVRRPTransition(func(name string, args ...string) ([]byte, error) {
		ambiguousReads++
		if name != "systemctl" || len(args) == 0 || args[0] != "show" {
			t.Fatalf("ambiguous observation reached a mutation: %s %v", name, args)
		}
		return []byte(fmt.Sprintf("ActiveState=\nActiveState=active\nSubState=running\nMainPID=%d\n", replacement.Process.Pid)), nil
	})
	if err == nil || ambiguousReads != 1 {
		t.Fatalf("ambiguous readiness authorized notification: reads=%d err=%v", ambiguousReads, err)
	}
	select {
	case err := <-replacementDone:
		t.Fatalf("ambiguous readiness signaled private process: %v", err)
	default:
	}
	t.Log("ready PID changed during observation; neither owned process was signaled")
}

func TestNotifyRouterdVRRPTransitionSignalsMainProcess(t *testing.T) {
	if os.Getenv("ROUTERD_READY_NOTIFY_CHILD") == "1" {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGUSR1)
		defer signal.Stop(signals)
		fmt.Println("READY")
		select {
		case <-signals:
			fmt.Println("NOTIFIED")
			// Keep stdout open until the parent consumes the acknowledgement.
			if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("ready process did not receive notification")
		}
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestNotifyRouterdVRRPTransitionSignalsMainProcess$", "-test.timeout=10s")
	cmd.Env = append(os.Environ(), "ROUTERD_READY_NOTIFY_CHILD=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("owned ready child did not exit")
		}
	})
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "READY" {
		t.Fatalf("private child was not ready: %q %v", scanner.Text(), scanner.Err())
	}
	reads := 0
	err = notifyRouterdVRRPTransition(func(name string, args ...string) ([]byte, error) {
		want := "show --property=ActiveState --property=SubState --property=MainPID routerd.service"
		if name != "systemctl" || strings.Join(args, " ") != want {
			t.Fatalf("unexpected readiness command %s %v", name, args)
		}
		reads++
		return []byte(fmt.Sprintf("MainPID=%d\nActiveState=active\nSubState=running\n", cmd.Process.Pid)), nil
	})
	if err != nil || reads != 2 {
		t.Fatalf("ready notification: reads=%d err=%v", reads, err)
	}
	if !scanner.Scan() || scanner.Text() != "NOTIFIED" {
		t.Fatalf("pidfd signal not received: %q %v", scanner.Text(), scanner.Err())
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ready notification child: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ready child did not exit after notification")
	}
	t.Log("two ready observations -> real pidfd SIGUSR1 -> acknowledged child exit0")
}
