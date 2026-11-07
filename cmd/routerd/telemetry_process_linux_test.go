// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package main

import (
	"context"
	logs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/grpc"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type processLogReceiver struct {
	logs.UnimplementedLogsServiceServer
	received chan string
}

func (r *processLogReceiver) Export(_ context.Context, req *logs.ExportLogsServiceRequest) (*logs.ExportLogsServiceResponse, error) {
	for _, rs := range req.ResourceLogs {
		for _, ss := range rs.ScopeLogs {
			for _, record := range ss.LogRecords {
				select {
				case r.received <- record.Body.GetStringValue():
				default:
				}
			}
		}
	}
	return &logs.ExportLogsServiceResponse{}, nil
}
func TestServeTelemetryDeliversReconcileLogs(t *testing.T) {
	if dir := os.Getenv("ROUTERD_TELEMETRY_PROCESS_DIR"); dir != "" {
		if err := run([]string{"serve", "--sandbox", "--root", dir, "--config", filepath.Join(dir, "router.yaml"), "--state-file", filepath.Join(dir, "state.db"), "--ledger-file", filepath.Join(dir, "ledger.db"), "--socket", filepath.Join(dir, "control.sock"), "--status-socket", filepath.Join(dir, "status.sock"), "--http-listen", "127.0.0.1:0", "--controllers", "observability-pipeline", "--graceful-stop-timeout", "0"}, io.Discard, os.Stderr); err != nil {
			t.Fatal(err)
		}
		return
	}
	dir, err := os.MkdirTemp("", "rdotel-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "router.yaml"), []byte("apiVersion: routerd.net/v1alpha1\nkind: Router\nmetadata:\n  name: telemetry\nspec:\n  resources: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	defer server.Stop()
	receiver := &processLogReceiver{received: make(chan string, 64)}
	logs.RegisterLogsServiceServer(server, receiver)
	go func() { _ = server.Serve(listener) }()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestServeTelemetryDeliversReconcileLogs$", "-test.timeout=20s")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "OTEL_") && !strings.HasPrefix(entry, "ROUTERD_OTLP_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "ROUTERD_TELEMETRY_PROCESS_DIR="+dir, "OTEL_EXPORTER_OTLP_ENDPOINT=http://"+listener.Addr().String(), "OTEL_EXPORTER_OTLP_INSECURE=true", "OTEL_LOGS_EXPORTER=otlp", "OTEL_METRICS_EXPORTER=none", "OTEL_TRACES_EXPORTER=none", "OTEL_BLRP_SCHEDULE_DELAY=10")
	output, err := os.Create(filepath.Join(dir, "process.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() { _ = cmd.Process.Kill() }()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case message := <-receiver.received:
			if message == "controller reconcile completed" {
				if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("daemon did not stop")
				}
				data, _ := os.ReadFile(filepath.Join(dir, "process.log"))
				if !strings.Contains(string(data), message) {
					t.Fatal("OTLP delivery suppressed local controller logs")
				}
				return
			}
		case err := <-done:
			data, _ := os.ReadFile(filepath.Join(dir, "process.log"))
			t.Fatalf("daemon exited before delivery: %v: %s", err, data)
		case <-timer.C:
			data, _ := os.ReadFile(filepath.Join(dir, "process.log"))
			t.Fatalf("daemon reconcile log not exported: %s", data)
		}
	}
}
