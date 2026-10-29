// SPDX-License-Identifier: BSD-3-Clause

//go:build linux || freebsd

package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func childClient(address string) *http.Client {
	return &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", address)
		},
	}}
}

func startChildFixture(t *testing.T, args ...string) (*http.Client, string, <-chan error) {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "control.sock")
	if err := writeIntent(socket+".intent.json", args); err != nil {
		t.Fatal(err)
	}
	// The production listener is a private Unix socket. This executor denies
	// AF_UNIX bind, so use a private loopback listener with the same actual
	// HTTP child owner; process creation, signals and Wait are real.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- serveChildControl(ctx, listener, args) }()
	client := childClient(listener.Addr().String())
	t.Cleanup(func() {
		cancel()
		client.CloseIdleConnections()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("actual child owner cleanup: %v", err)
			}
		case <-time.After(12 * time.Second):
			t.Error("actual child owner did not join")
		}
	})
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		response, err := client.Get("http://child/status")
		if err == nil {
			response.Body.Close()
			return client, socket, done
		}
	}
	t.Fatal("child owner did not start")
	return nil, "", nil
}

func childRequest(t *testing.T, client *http.Client, path string) (int, childStatus) {
	t.Helper()
	response, err := client.Post("http://child"+path, "application/json", strings.NewReader(`{"pid":1}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var status childStatus
	if response.StatusCode == http.StatusOK {
		if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
			t.Fatal(err)
		}
	} else {
		_, _ = io.Copy(io.Discard, response.Body)
	}
	return response.StatusCode, status
}

func TestChildOwnerStopsAndJoinsActualForegroundChild(t *testing.T) {
	client, socket, _ := startChildFixture(t, "/bin/sleep", "60")
	data, err := os.ReadFile(socket + ".intent.json")
	if err != nil || !strings.Contains(string(data), `"/bin/sleep","60"`) {
		t.Fatalf("actual start lost durable intent: %s %v", data, err)
	}
	if code, _ := childRequest(t, client, "/close"); code != http.StatusConflict {
		t.Fatalf("live child owner closed: %d", code)
	}
	code, status := childRequest(t, client, "/term")
	if code != http.StatusOK || status.PID < 1 || status.Running || status.Error != "" {
		t.Fatalf("stop did not acknowledge actual Wait: code=%d %+v", code, status)
	}
	if code, status := childRequest(t, client, "/term"); code != http.StatusOK || status.Running {
		t.Fatalf("known stopped child is not idempotent: %d %+v", code, status)
	}
	if code, _ := childRequest(t, client, "/close"); code != http.StatusOK {
		t.Fatalf("acknowledged child owner could not close: %d", code)
	}
}

func TestChildOwnerNeverSignalsPIDFromDiskOrRequest(t *testing.T) {
	foreign := exec.Command("/bin/sleep", "60")
	if err := foreign.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = foreign.Process.Kill(); _ = foreign.Wait() })
	client, socket, _ := startChildFixture(t, "/bin/sleep", "60")
	// Diagnostic files and request bodies cannot replace the actual direct
	// child handle, even when they contain another currently live process PID.
	if err := os.WriteFile(socket+".intent.json", []byte(`{"pid":`+jsonNumber(foreign.Process.Pid)+`}`), 0600); err != nil {
		t.Fatal(err)
	}
	response, err := client.Post("http://child/kill", "application/json", strings.NewReader(`{"pid":`+jsonNumber(foreign.Process.Pid)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var stopped childStatus
	if err := json.NewDecoder(response.Body).Decode(&stopped); err != nil || response.StatusCode != http.StatusOK || stopped.Running || stopped.PID == foreign.Process.Pid {
		t.Fatalf("actual child stop failed: %+v %v", stopped, err)
	}
	if err := foreign.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("saved/request PID was signaled: %v", err)
	}
}

func jsonNumber(value int) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func TestChildIntentCollisionNeverExecutes(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "control.sock")
	if err := os.WriteFile(socket+".intent.json", []byte("uncertain prior start\n"), 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(filepath.Dir(socket), "executed")
	if err := serveChild(t.Context(), socket, []string{"/bin/sh", "-c", `printf ran > "$1"`, "child", marker}); err == nil {
		t.Fatal("uncertain intent was replaced")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("child executed before exclusive durable intent: %v", err)
	}
}
