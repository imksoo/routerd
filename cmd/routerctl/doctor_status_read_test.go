// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
)

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
