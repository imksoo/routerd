// SPDX-License-Identifier: BSD-3-Clause
//go:build linux || freebsd

package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBGPProcessOwnerChild(t *testing.T) {
	role := os.Getenv("ROUTERD_BGP_OWNER_CHILD")
	if role == "" {
		return
	}
	runtimeDir := os.Getenv("ROUTERD_BGP_OWNER_RUNTIME")
	if role == "contender" {
		for _, prefix := range []string{"first", "alternate"} {
			base := filepath.Join(filepath.Dir(runtimeDir), prefix)
			err := runBGPDaemon(context.Background(), runtimeDir, filepath.Join(base, "rpc", "api.sock"), filepath.Join(base, "control", "api.sock"), filepath.Join(base, "state", "applied.json"))
			if err == nil || !strings.Contains(err.Error(), "another routerd-bgp") {
				t.Fatalf("contending daemon was not rejected at its real entrypoint: %v", err)
			}
		}
		return
	}
	owner, err := acquireBGPProcessOwner(context.Background(), runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("owned")
	input := bufio.NewScanner(os.Stdin)
	if !input.Scan() || input.Text() != "retain" {
		t.Fatal("missing retain command")
	}
	if err := owner.finish(false); err != nil {
		t.Fatal(err)
	}
	owner = nil
	runtime.GC()
	fmt.Println("retained")
	if !input.Scan() || input.Text() != "exit" {
		t.Fatal("missing exit command")
	}
}

func TestBGPProcessOwnerRejectsSecondDaemonBeforeCustomPathsAndSurvivesGC(t *testing.T) {
	root := t.TempDir()
	runtimeDir := filepath.Join(root, "runtime")
	journal := filepath.Join(root, "first", "state", "applied.json")
	rpc := filepath.Join(root, "first", "rpc", "api.sock")
	control := filepath.Join(root, "first", "control", "api.sock")
	for _, path := range []string{journal, rpc, control} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("first owner sentinel"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	env := append(os.Environ(), "ROUTERD_BGP_OWNER_RUNTIME="+runtimeDir)
	holder := exec.CommandContext(ctx, executable, "-test.run=^TestBGPProcessOwnerChild$")
	holder.Env = append(env, "ROUTERD_BGP_OWNER_CHILD=holder")
	var stderr bytes.Buffer
	holder.Stderr = &stderr
	stdin, err := holder.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := holder.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		if holder.ProcessState == nil {
			_ = holder.Process.Kill()
			_ = holder.Wait()
		}
	})
	output := bufio.NewScanner(stdout)
	await := func(want string) {
		t.Helper()
		if !output.Scan() || output.Text() != want {
			t.Fatalf("holder did not acknowledge %s: %q, %v", want, output.Text(), output.Err())
		}
	}
	contend := func() {
		t.Helper()
		second := exec.CommandContext(ctx, executable, "-test.run=^TestBGPProcessOwnerChild$")
		second.Env = append(env, "ROUTERD_BGP_OWNER_CHILD=contender")
		if result, err := second.CombinedOutput(); err != nil {
			t.Fatalf("second actual process: %v\n%s", err, result)
		}
	}
	await("owned")
	contend()
	if _, err := fmt.Fprintln(stdin, "retain"); err != nil {
		t.Fatal(err)
	}
	await("retained")
	contend()
	for _, path := range []string{journal, rpc, control} {
		if data, err := os.ReadFile(path); err != nil || string(data) != "first owner sentinel" {
			t.Fatalf("second daemon changed the first owner's path %s: %q %v", path, data, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "alternate")); !os.IsNotExist(err) {
		t.Fatalf("custom paths escaped ownership admission: %v", err)
	}
	if _, err := fmt.Fprintln(stdin, "exit"); err != nil {
		t.Fatal(err)
	}
	if err := holder.Wait(); err != nil {
		t.Fatalf("holder exit: %v\n%s", err, stderr.String())
	}
	owner, err := acquireBGPProcessOwner(context.Background(), runtimeDir)
	if err != nil {
		t.Fatalf("actual process exit did not release the retained lock: %v", err)
	}
	if err := owner.finish(true); err != nil {
		t.Fatal(err)
	}
}

func TestBGPProcessOwnerPreservesLockInodeAfterAcknowledgedStop(t *testing.T) {
	directory := t.TempDir()
	owner, err := acquireBGPProcessOwner(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(directory)
	if err != nil || len(files) != 1 {
		t.Fatalf("lock file: %v, %v", files, err)
	}
	path := filepath.Join(directory, files[0].Name())
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.finish(true); err != nil {
		t.Fatal(err)
	}
	owner, err = acquireBGPProcessOwner(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.finish(true)
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("lock inode was replaced after stop: %v", err)
	}
}

func TestBGPProcessOwnerRejectsUnprotectedRuntimeDirectory(t *testing.T) {
	for _, mode := range []os.FileMode{0777, 0775} {
		t.Run(fmt.Sprintf("%o", mode), func(t *testing.T) {
			directory := t.TempDir()
			if err := os.Chmod(directory, mode); err != nil {
				t.Fatal(err)
			}
			if owner, err := acquireBGPProcessOwner(context.Background(), directory); err == nil {
				_ = owner.finish(true)
				t.Fatal("writable runtime directory accepted")
			}
			info, err := os.Stat(directory)
			if err != nil || info.Mode().Perm() != mode {
				t.Fatalf("unprotected directory was adopted or changed: %v", err)
			}
		})
	}
}
