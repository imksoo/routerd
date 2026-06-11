// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/state"
)

func withDoctorStatusStub(t *testing.T, status *controlapi.Status, fetchErr error) {
	t.Helper()
	prev := doctorStatusFetcher
	doctorStatusFetcher = func(_ string, _ time.Duration) (*controlapi.Status, error) {
		if fetchErr != nil {
			return nil, fetchErr
		}
		return status, nil
	}
	t.Cleanup(func() { doctorStatusFetcher = prev })
}

func withDoctorIncidentCommandsStub(t *testing.T, commands []diagnoseCommandCheck) {
	t.Helper()
	prev := doctorRunDiagnosticCommand
	doctorRunDiagnosticCommand = func(_ context.Context, label, _ string, _ ...string) diagnoseCommandCheck {
		for _, command := range commands {
			if command.Name == label {
				return command
			}
		}
		return diagnoseCommandCheck{Name: label, OK: true, Stdout: "stubbed output", Output: "stubbed output"}
	}
	t.Cleanup(func() { doctorRunDiagnosticCommand = prev })
}

func TestDoctorIncidentDumpIncludesRuntimeStatusAndCommands(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	db := openDoctorState(t, statePath)
	now := time.Now().UTC()
	if _, err := db.RecordPluginRun(state.PluginRunRecord{
		Plugin:      "cloud-provider",
		TriggerType: "manual",
		StartedAt:   now,
		Status:      "running",
	}); err != nil {
		t.Fatalf("record plugin run: %v", err)
	}
	if err := db.SaveObjectStatus(api.NetAPIVersion, "Interface", "wan", map[string]any{"phase": "Applied"}); err != nil {
		t.Fatalf("save object status: %v", err)
	}
	if err := db.RecordEvent("net.routerd.net/v1alpha1", "Interface", "wan", "Normal", "StateObserved", "incident test"); err != nil {
		t.Fatalf("record event: %v", err)
	}
	closeDoctorState(t, db)

	status := &controlapi.Status{}
	status.Status.Phase = "Running"
	status.Status.ResourcePhaseIssues = []controlapi.ResourcePhaseIssue{
		{APIVersion: api.NetAPIVersion, Kind: "Interface", Name: "wan", Phase: "Applied"},
	}
	stats := controlapi.NewRuntimeStats()
	stats.OpenFDs = 17
	stats.MaxFDs = 1024
	stats.HeapAllocBytes = 4 * 1024 * 1024
	stats.NumGC = 13
	withDoctorStatusStub(t, status, nil)
	withRuntimeStub(t, &stats, nil)
	withDoctorIncidentCommandsStub(t, []diagnoseCommandCheck{
		{Name: "ip -4 route show table all", OK: true, Stdout: "10.0.0.0/24 dev eth0", Output: "10.0.0.0/24 dev eth0", Command: "ip -4 route show table all"},
		{Name: "ip -6 route show table all", OK: true, Stdout: "", Output: "", Command: "ip -6 route show table all"},
		{Name: "journalctl -u routerd", OK: false, Error: "journal not available"},
	})

	var out bytes.Buffer
	if err := run([]string{"doctor", "--incident", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "doctor found failing checks") {
		t.Fatalf("doctor --incident should preserve normal check exit status, got %v", err)
	}
	var got doctorReport
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if got.Incident == nil {
		t.Fatalf("incident dump is nil")
	}
	if got.Incident.Status == nil || got.Incident.Status.Status.Phase != "Running" {
		t.Fatalf("incident status not captured: %#v", got.Incident.Status)
	}
	if got.Incident.Runtime == nil {
		t.Fatalf("incident runtime not captured")
	}
	if len(got.Incident.ObjectStatuses) == 0 {
		t.Fatalf("object status missing")
	}
	if len(got.Incident.PluginRuns) != 1 {
		t.Fatalf("plugin runs = %d, want 1", len(got.Incident.PluginRuns))
	}
	if len(got.Incident.Events) == 0 {
		t.Fatalf("events missing")
	}
	if got.Incident.Error != "" {
		t.Fatalf("incident error = %q", got.Incident.Error)
	}
	if len(got.Checks) == 0 {
		t.Fatalf("doctor --incident should attach incident data to normal checks")
	}
}

func TestDoctorIncidentCanRunWithoutChecksWhenTargeted(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	withDoctorStatusStub(t, &controlapi.Status{Status: controlapi.StatusStatus{Phase: "Running"}}, nil)
	withRuntimeStub(t, &controlapi.RuntimeStats{}, nil)
	var out bytes.Buffer
	if err := run([]string{"doctor", "incident", "--incident", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor incident target: %v", err)
	}
	var got doctorReport
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Incident == nil {
		t.Fatalf("incident dump missing")
	}
	if got.Checks != nil {
		t.Fatalf("expected no checks for incident target, got %d", len(got.Checks))
	}
}

func TestDoctorIncidentSkipsHostCommandsWhenNoHost(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	withDoctorStatusStub(t, &controlapi.Status{Status: controlapi.StatusStatus{Phase: "Running"}}, nil)
	withRuntimeStub(t, &controlapi.RuntimeStats{}, nil)
	withDoctorIncidentCommandsStub(t, []diagnoseCommandCheck{})

	var out bytes.Buffer
	if err := run([]string{"doctor", "incident", "--incident", "--no-host", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor incident: %v", err)
	}
	var got doctorReport
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Incident == nil {
		t.Fatalf("incident dump missing")
	}
	if len(got.Incident.Commands) != 0 {
		t.Fatalf("expected no commands for --no-host, got %d", len(got.Incident.Commands))
	}
}

func TestDoctorIncidentCommandErrorsAreCaptured(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	withDoctorStatusStub(t, nil, errors.New("socket unavailable"))
	withRuntimeStub(t, nil, errors.New("runtime socket unavailable"))

	var out bytes.Buffer
	if err := run([]string{"doctor", "incident", "--incident", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor incident should still emit snapshot: %v", err)
	}
	var got doctorReport
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Incident == nil {
		t.Fatalf("incident dump missing")
	}
	if !strings.Contains(got.Incident.Error, "socket unavailable") {
		t.Fatalf("incident error missing fetch details: %q", got.Incident.Error)
	}
}
