// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/controlapi"
)

func startStatusTestServer(t *testing.T, status controlapi.Status) string {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "routerd.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	server := &http.Server{Handler: controlapi.Handler{
		Status: func(r *http.Request) (*controlapi.Status, error) {
			out := status
			return &out, nil
		},
	}}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(listener) }()
	return socketPath
}

func sampleControllerStatus() controlapi.ControllerStatus {
	now := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	maxAt := now.Add(-time.Hour)
	return controlapi.ControllerStatus{
		Name:                "dns-resolver",
		Mode:                "live",
		ReconcileCount:      42,
		ReconcileErrorCount: 2,
		CurrentError:        false,
		MaxDuration:         "180ms",
		MaxDurationMillis:   180,
		MaxDurationAt:       &maxAt,
		LastSuccessTime:     &now,
		ReconcileErrorHistory: []controlapi.ReconcileErrorEntry{
			{
				StartedAt:    now.Add(-2 * time.Hour),
				CompletedAt:  now.Add(-2*time.Hour + 12*time.Millisecond),
				Duration:     "12ms",
				DurationMs:   12,
				Trigger:      "event",
				ResourceKind: "DNSResolver",
				ResourceName: "lan",
				Error:        "upstream timeout",
			},
			{
				StartedAt:    now.Add(-30 * time.Minute),
				CompletedAt:  now.Add(-30*time.Minute + 8*time.Millisecond),
				Duration:     "8ms",
				DurationMs:   8,
				Trigger:      "periodic",
				ResourceKind: "DNSResolver",
				ResourceName: "lan",
				Error:        "nxdomain",
			},
		},
	}
}

func TestStatusCommandJSONIncludesReconcileErrorHistory(t *testing.T) {
	status := controlapi.NewStatus(&apply.Result{Phase: "Healthy", Generation: 11})
	status.Status.Controllers = []controlapi.ControllerStatus{sampleControllerStatus()}
	socketPath := startStatusTestServer(t, status)

	var out bytes.Buffer
	if err := run([]string{"status", "--socket", socketPath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("status: %v", err)
	}
	var decoded controlapi.Status
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if len(decoded.Status.Controllers) != 1 {
		t.Fatalf("controllers = %d", len(decoded.Status.Controllers))
	}
	ctl := decoded.Status.Controllers[0]
	if len(ctl.ReconcileErrorHistory) != 2 {
		t.Fatalf("history len = %d, want 2:\n%s", len(ctl.ReconcileErrorHistory), out.String())
	}
	if ctl.ReconcileErrorHistory[0].Error != "upstream timeout" {
		t.Fatalf("history[0] = %+v", ctl.ReconcileErrorHistory[0])
	}
	if ctl.MaxDurationAt == nil {
		t.Fatalf("MaxDurationAt missing")
	}
}

func TestStatusCommandTableShowErrors(t *testing.T) {
	status := controlapi.NewStatus(&apply.Result{Phase: "Healthy", Generation: 11})
	status.Status.Controllers = []controlapi.ControllerStatus{sampleControllerStatus()}
	socketPath := startStatusTestServer(t, status)

	var out bytes.Buffer
	if err := run([]string{"status", "--socket", socketPath, "-o", "table", "--show-errors"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("status table: %v", err)
	}
	output := out.String()
	for _, want := range []string{
		"CONTROLLER",
		"dns-resolver",
		"ERRORS controller=dns-resolver",
		"upstream timeout",
		"nxdomain",
		"DNSResolver/lan",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("status table missing %q:\n%s", want, output)
		}
	}
}
