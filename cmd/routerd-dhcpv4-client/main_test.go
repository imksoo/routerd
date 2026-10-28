// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRestoreLeaseIgnoresEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease.json")
	if err := os.WriteFile(path, []byte(" \n\t"), 0644); err != nil {
		t.Fatalf("write lease: %v", err)
	}
	daemon := &dhcpv4Daemon{opts: options{leaseFile: path}}
	if err := daemon.restoreLease(); err != nil {
		t.Fatalf("restore empty lease: %v", err)
	}
}

func TestStopCommandCancelsTheDaemonOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	d := &dhcpv4Daemon{opts: options{leaseFile: filepath.Join(dir, "lease.json"), eventFile: filepath.Join(dir, "events.jsonl")}, cancel: cancel}
	d.cond = sync.NewCond(&d.mu)
	response := httptest.NewRecorder()
	d.handleCommand(response, httptest.NewRequest(http.MethodPost, "/v1/commands/stop", strings.NewReader("")))
	var result daemonapi.CommandResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !result.Accepted || ctx.Err() != context.Canceled {
		t.Fatalf("stop did not cancel the worker: %+v", result)
	}
}
