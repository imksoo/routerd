// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

// This mandatory positive test uses the actual Unix endpoint and daemon owner.
// A sandbox denial must be reported as missing IPC evidence, not skipped.
func TestObserverActualControlSocketStopJoinsWorkers(t *testing.T) {
	dir := t.TempDir()
	socket, events := filepath.Join(dir, "observer.sock"), filepath.Join(dir, "events.jsonl")
	d := newDaemon(options{ifname: "routerd-no-link", socketPath: socket, eventFile: events})
	d.publishLocked("fixture", "info", "QueuedBeforeStop", "", nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.run(ctx) }()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	for deadline := time.Now().Add(2 * time.Second); ; {
		select {
		case err := <-done:
			t.Fatalf("actual daemon failed before control endpoint became ready: %v", err)
		default:
		}
		response, err := client.Get("http://unix/v1/status")
		if err == nil {
			response.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("actual Unix control endpoint did not start: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	response, err := client.Post("http://unix/v1/commands", "application/json", strings.NewReader(`{"command":"stop"}`))
	if err != nil {
		t.Fatal(err)
	}
	var result daemonapi.CommandResult
	err = json.NewDecoder(response.Body).Decode(&result)
	response.Body.Close()
	if err != nil || !result.Accepted {
		t.Fatalf("actual stop command rejected: %+v %v", result, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("actual orderly shutdown failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stop was accepted but daemon workers did not join")
	}
	data, err := os.ReadFile(events)
	if err != nil || !strings.Contains(string(data), "QueuedBeforeStop") {
		t.Fatalf("joined shutdown lost pending event: %s %v", data, err)
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("joined shutdown left its endpoint: %v", err)
	}
}
