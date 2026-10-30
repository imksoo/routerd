// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
)

func TestDoctorRollbackRequiresStoredConfig(t *testing.T) {
	for _, tc := range []struct {
		name          string
		saveConfig    bool
		newerAttempts int
		wantStatus    string
	}{
		{name: "failed attempt without config", newerAttempts: 1, wantStatus: doctorWarn},
		{name: "older saved config", saveConfig: true, newerAttempts: 2, wantStatus: doctorPass},
		{name: "config outside default history window", saveConfig: true, newerAttempts: 100, wantStatus: doctorWarn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configPath, statePath := writeDoctorFixture(t)
			store := openDoctorState(t, statePath)
			var savedGeneration int64
			if tc.saveConfig {
				var err error
				savedGeneration, err = store.BeginGeneration("saved")
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := store.RecordGenerationConfig(savedGeneration, string(data)); err != nil {
					t.Fatal(err)
				}
				if err := store.FinishGeneration(savedGeneration, "Applied", nil); err != nil {
					t.Fatal(err)
				}
			}
			var latest int64
			for range tc.newerAttempts {
				var err error
				latest, err = store.BeginGeneration("failed")
				if err != nil {
					t.Fatal(err)
				}
				if err := store.FinishGeneration(latest, "Failed", nil); err != nil {
					t.Fatal(err)
				}
			}
			closeDoctorState(t, store)
			if _, err := rollbackGenerationConfig(statePath, latest); err == nil || !strings.Contains(err.Error(), "has no saved config") {
				t.Fatalf("failed attempt unexpectedly provides a rollback config: %v", err)
			}
			var output bytes.Buffer
			if err := run([]string{"doctor", "rollback", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &output, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			var report doctorReport
			if err := json.Unmarshal(output.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if len(report.Checks) != 1 || report.Checks[0].Status != tc.wantStatus {
				t.Fatalf("rollback diagnostic = %+v, want %s", report.Checks, tc.wantStatus)
			}
			if tc.wantStatus == doctorPass && !strings.Contains(report.Checks[0].Detail, fmt.Sprintf("generation %d", savedGeneration)) {
				t.Fatalf("diagnostic did not identify the saved generation: %+v", report.Checks[0])
			}
			if tc.wantStatus == doctorWarn && !strings.Contains(report.Checks[0].Detail, "checked") {
				t.Fatalf("diagnostic did not bound its unsuccessful search: %+v", report.Checks[0])
			}
		})
	}
}

func TestDoctorRollbackRetainsHistoryReadFailure(t *testing.T) {
	_, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)
	closeDoctorState(t, store)
	checks := (doctorRunner{store: store}).doctorRollback()
	if len(checks) != 1 || checks[0].Status != doctorWarn || !strings.Contains(checks[0].Detail, "connection") {
		t.Fatalf("closed history became rollback availability: %+v", checks)
	}
}

func TestDoctorFailsOnCorruptStatusRatherThanReportingMissingObservation(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver", map[string]any{"phase": "Applied"}); err != nil {
		t.Fatal(err)
	}
	closeDoctorState(t, store)
	database, err := sql.Open("sqlite", statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE objects SET status='corrupt' WHERE kind='DNSResolver' AND name='lan-resolver'`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = run([]string{"doctor", "dns", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &output, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "read doctor object statuses") {
		t.Fatalf("corrupt SQL row became diagnostic success: err=%v output=%s", err, output.String())
	}
	if output.Len() != 0 {
		t.Fatalf("published status report despite failed authoritative read: %s", output.String())
	}
}

func TestDoctorProbeReportsFailureAndRejectsIgnoredOptionsBeforeTransport(t *testing.T) {
	result := controlapi.NewProbeResult("dns", "lan", []controlapi.ProbeCheck{{Name: "DNSResolver/lan status", Status: "fail", Detail: "reported phase Failed"}})
	for _, format := range []string{"table", "json", "yaml"} {
		var output bytes.Buffer
		if err := writeDoctorProbeResult(&output, &result, format); err == nil || !strings.Contains(err.Error(), "failing checks") || !strings.Contains(output.String(), "Failed") {
			t.Fatalf("probe %s hid failed outcome: %v %s", format, err, output.String())
		}
	}
	for _, flags := range [][]string{{"--no-host"}, {"--server", "127.0.0.1"}, {"--name", "example.com"}} {
		args := append([]string{"doctor", "--probe", "dns", "--socket", t.TempDir() + "/missing.sock"}, flags...)
		err := run(args, &bytes.Buffer{}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "do not implement") {
			t.Fatalf("ignored probe options %v reached transport: %v", flags, err)
		}
	}
}

func TestDoctorOptionalAreaDoesNotTurnUnreadableDatabaseIntoSkip(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	if err := os.WriteFile(statePath, []byte("not a SQLite database"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, area := range []string{"dynamic", "plugin"} {
		var output bytes.Buffer
		err := run([]string{"doctor", area, "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &output, &bytes.Buffer{})
		if err == nil || output.Len() != 0 {
			t.Fatalf("%s database failure became successful skip: %v %s", area, err, output.String())
		}
	}
}
