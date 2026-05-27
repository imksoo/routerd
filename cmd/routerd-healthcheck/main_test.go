// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/healthcheck"
	routerotel "github.com/imksoo/routerd/pkg/otel"
)

func TestSelftestTCP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()
	host, portValue, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	dir := t.TempDir()
	if err := run([]string{"selftest", "--target", host, "--port", portValue, "--protocol", "tcp", "--state-file", dir + "/state.json", "--event-file", dir + "/events.jsonl"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		State struct {
			Phase string `json:"phase"`
		} `json:"state"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.State.Phase != "Healthy" {
		t.Fatalf("phase = %q, output:\n%s", decoded.State.Phase, stdout.String())
	}
}

func TestRestoreStateIgnoresEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("\n"), 0644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	daemon := &daemon{opts: options{stateFile: path}}
	if err := daemon.restoreState(); err != nil {
		t.Fatalf("restore empty state: %v", err)
	}
}

func TestProbeOnceUpdatesStateHistory(t *testing.T) {
	// Stub the route lookup so the daemon path is hermetic.
	orig := healthcheck.RouteLookup
	healthcheck.RouteLookup = func(ctx context.Context, target, family string) (healthcheck.RouteInfo, error) {
		return healthcheck.RouteInfo{NextHop: "192.0.2.1", OutInterface: "wan0", Source: "192.0.2.42"}, nil
	}
	defer func() { healthcheck.RouteLookup = orig }()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	host, portValue, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port := 0
	if _, err := fmtSscanInt(portValue, &port); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	opts := options{
		resource:           "internet",
		target:             host,
		protocol:           "tcp",
		port:               port,
		interval:           time.Second,
		timeout:            time.Second,
		healthyThreshold:   1,
		unhealthyThreshold: 1,
		sourceOrigin:       "static",
		tunnelLocal:        "192.0.2.42",
		tunnelRemote:       "203.0.113.10",
		stateFile:          dir + "/state.json",
		eventFile:          dir + "/events.jsonl",
		socketPath:         dir + "/sock",
	}
	d := newDaemon(opts, &routerotel.Runtime{})
	d.cancel = func() {}
	if err := d.probeOnce(context.Background()); err != nil {
		t.Fatalf("probeOnce: %v", err)
	}
	if len(d.state.History) != 1 {
		t.Fatalf("history len = %d", len(d.state.History))
	}
	if d.state.History[0].ProbeEvidence.NextHop != "192.0.2.1" {
		t.Errorf("nextHop = %q", d.state.History[0].NextHop)
	}
	if d.state.LastEvidence.TunnelLocal != "192.0.2.42" {
		t.Errorf("tunnelLocal hint not applied: %q", d.state.LastEvidence.TunnelLocal)
	}
	if d.state.LastEvidence.TunnelRemote != "203.0.113.10" {
		t.Errorf("tunnelRemote hint not applied: %q", d.state.LastEvidence.TunnelRemote)
	}
	if d.state.LastEvidence.SourceOrigin != "static" {
		t.Errorf("sourceOrigin hint not applied: %q", d.state.LastEvidence.SourceOrigin)
	}
	// A second probe extends the history.
	if err := d.probeOnce(context.Background()); err != nil {
		t.Fatalf("probeOnce 2: %v", err)
	}
	if len(d.state.History) != 2 {
		t.Fatalf("history len after 2 probes = %d", len(d.state.History))
	}
	// Round-trip the state file: restoring should see both records.
	d2 := &daemon{opts: opts}
	if err := d2.restoreState(); err != nil {
		t.Fatalf("restoreState: %v", err)
	}
	if len(d2.state.History) != 2 {
		t.Fatalf("restored history len = %d", len(d2.state.History))
	}
	if d2.state.LastEvidence.NextHop != "192.0.2.1" {
		t.Errorf("restored nextHop = %q", d2.state.LastEvidence.NextHop)
	}
}

// fmtSscanInt is a tiny shim so we can keep the imports list short.
func fmtSscanInt(s string, dst *int) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	*dst = n
	return n, nil
}
