// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/controlapi"
)

func withReconcileStub(t *testing.T, controllers []controlapi.ControllerStatus, fetchErr error) {
	t.Helper()
	prev := reconcileStatusFetcher
	reconcileStatusFetcher = func(socketPath string, timeout time.Duration) ([]controlapi.ControllerStatus, error) {
		if fetchErr != nil {
			return nil, fetchErr
		}
		return controllers, nil
	}
	t.Cleanup(func() { reconcileStatusFetcher = prev })
}

func TestDoctorReconcileAggregatesErrorsWithinSince(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	now := time.Now().UTC()
	controllers := []controlapi.ControllerStatus{
		{
			Name:                "dns",
			CurrentError:        false,
			ReconcileErrorCount: 3,
			ReconcileErrorHistory: []controlapi.ReconcileErrorEntry{
				{CompletedAt: now.Add(-3 * time.Hour), Trigger: "periodic", ResourceKind: "DNSResolver", ResourceName: "lan", Error: "old timeout"},
				{CompletedAt: now.Add(-30 * time.Minute), Trigger: "event", ResourceKind: "DNSResolver", ResourceName: "lan", Error: "recent timeout"},
				{CompletedAt: now.Add(-5 * time.Minute), Trigger: "event", ResourceKind: "DNSResolver", ResourceName: "lan", Error: "another recent"},
			},
		},
		{
			Name:                "vrrp",
			CurrentError:        true,
			ReconcileErrorCount: 1,
			ReconcileErrorHistory: []controlapi.ReconcileErrorEntry{
				{CompletedAt: now.Add(-10 * time.Minute), Trigger: "periodic", Error: "peer down"},
			},
		},
	}
	withReconcileStub(t, controllers, nil)

	var out bytes.Buffer
	if err := run([]string{"doctor", "reconcile", "--config", configPath, "--state-file", statePath, "--no-host", "--since", "1h", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor reconcile: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if len(report.Checks) != 1 {
		t.Fatalf("checks = %d:\n%s", len(report.Checks), out.String())
	}
	check := report.Checks[0]
	if check.Area != "reconcile" {
		t.Fatalf("area = %q", check.Area)
	}
	if !strings.Contains(check.Detail, "3 reconcile errors in last 1h0m0s across 2 controllers") {
		t.Fatalf("detail does not summarize 3 recent errors / 2 controllers: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "current failures=1") {
		t.Fatalf("detail missing current failure count: %q", check.Detail)
	}
	if check.Status != doctorWarn {
		t.Fatalf("status = %q, want warn", check.Status)
	}
	if !strings.Contains(check.Detail, "recent timeout") || !strings.Contains(check.Detail, "peer down") {
		t.Fatalf("sample errors missing: %q", check.Detail)
	}
	if strings.Contains(check.Detail, "old timeout") {
		t.Fatalf("expected --since to exclude old timeout: %q", check.Detail)
	}
}

func TestDoctorReconcilePassesWhenHistoryEmpty(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	controllers := []controlapi.ControllerStatus{
		{Name: "dns", ReconcileCount: 12, CurrentError: false},
	}
	withReconcileStub(t, controllers, nil)

	var out bytes.Buffer
	if err := run([]string{"doctor", "reconcile", "--config", configPath, "--state-file", statePath, "--no-host", "--since", "24h", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor reconcile: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if len(report.Checks) != 1 || report.Checks[0].Status != doctorPass {
		t.Fatalf("report = %#v", report)
	}
	if !strings.Contains(report.Checks[0].Detail, "0 reconcile errors") {
		t.Fatalf("detail = %q", report.Checks[0].Detail)
	}
}

func TestDoctorReconcileSkipsOnFetchError(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	withReconcileStub(t, nil, errors.New("connection refused"))

	var out bytes.Buffer
	if err := run([]string{"doctor", "reconcile", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor reconcile: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if len(report.Checks) != 1 || report.Checks[0].Status != doctorSkip {
		t.Fatalf("expected skip, got: %#v", report.Checks)
	}
	if !strings.Contains(report.Checks[0].Detail, "connection refused") {
		t.Fatalf("detail = %q", report.Checks[0].Detail)
	}
}

func TestDoctorReconcileFailsAboveThreshold(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	now := time.Now().UTC()
	history := make([]controlapi.ReconcileErrorEntry, 0, doctorReconcileFailThreshold)
	for i := 0; i < doctorReconcileFailThreshold; i++ {
		history = append(history, controlapi.ReconcileErrorEntry{CompletedAt: now.Add(-time.Duration(i) * time.Minute), Error: "boom"})
	}
	withReconcileStub(t, []controlapi.ControllerStatus{{Name: "dns", ReconcileErrorHistory: history}}, nil)

	var out, errOut bytes.Buffer
	err := run([]string{"doctor", "reconcile", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &errOut)
	if err == nil {
		t.Fatalf("expected doctor to error on fail, got nil; output=%s", out.String())
	}
	var report doctorReport
	if jsonErr := json.Unmarshal(out.Bytes(), &report); jsonErr != nil {
		t.Fatalf("decode: %v\n%s", jsonErr, out.String())
	}
	if len(report.Checks) != 1 || report.Checks[0].Status != doctorFail {
		t.Fatalf("expected fail, got %#v", report.Checks)
	}
}
