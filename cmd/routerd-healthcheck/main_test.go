// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/healthcheck"
	routerotel "github.com/imksoo/routerd/pkg/otel"
)

func TestBindingValidationResult(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts options
		spec api.HealthCheckSpec
		want bool
	}{
		{"normal", options{}, api.HealthCheckSpec{}, false}, {"missing-iface", options{requireDSLiteBinding: true}, api.HealthCheckSpec{FwMark: 1}, true}, {"missing-mark", options{requireDSLiteBinding: true}, api.HealthCheckSpec{SourceInterface: "dslite0"}, true}, {"complete", options{requireDSLiteBinding: true}, api.HealthCheckSpec{SourceInterface: "dslite0", FwMark: 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := bindingValidationResult(tc.opts, tc.spec) != nil; got != tc.want {
				t.Fatalf("invalid=%v", got)
			}
		})
	}
}

func TestProbeOnceRequiresDSLiteBindingImmediatelyUnhealthy(t *testing.T) {
	dir := t.TempDir()
	d := newDaemon(options{
		resource: "dslite", target: "2001:db8::1", protocol: "tcp", timeout: time.Second,
		requireDSLiteBinding: true, unhealthyThreshold: 5,
		stateFile: filepath.Join(dir, "state.json"), eventFile: filepath.Join(dir, "events.jsonl"),
	}, &routerotel.Runtime{})
	d.state.Phase = healthcheck.PhaseHealthy
	if err := d.probeOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if d.state.Phase != healthcheck.PhaseUnhealthy || !strings.Contains(d.state.LastMessage, "DSLiteProbeBindingInvalid") {
		t.Fatalf("state = %#v", d.state)
	}
	if d.state.LastEvidence.FailureKind != healthcheck.FailureKindOther {
		t.Fatalf("evidence = %#v", d.state.LastEvidence)
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
