// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestDoctorSAMRouteQueryUsesCommandTimeout(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "ip"), []byte("#!/bin/sh\nsleep 30 &\nwait\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	runner := doctorRunner{opts: diagnoseOptions{Host: true, Timeout: 50 * time.Millisecond}}
	status := map[string]any{"ownershipResolverControlPlaneOwnerTable": []map[string]any{{"address": "192.0.2.1/32", "state": "OK", "class": "LocalHomeOwned", "localEvidenceNode": "node"}}}
	started := time.Now()
	checks := runner.doctorSAMOwnerTableRouteChecks("pool", api.MobilityPoolSpec{}, status)
	if time.Since(started) > 1500*time.Millisecond {
		t.Fatal("route diagnostic outlived its timeout/process group")
	}
	if len(checks) == 0 {
		t.Fatal("timed out query produced no unknown/failure observation")
	}
	for _, check := range checks {
		if check.Status == doctorPass {
			t.Fatalf("failed route query passed: %+v", check)
		}
	}
}

func TestDoctorRuntimeStorageFailureSurvivesHighFDWarning(t *testing.T) {
	stats := controlapi.NewRuntimeStats()
	stats.OpenFDs = 99
	stats.MaxFDs = 100
	alert := routerstate.NewStorageAlert("test-owned.db", time.Now())
	stats.EventJournal = &routerstate.EventJournalStats{StorageAlert: &alert}
	withRuntimeStub(t, &stats, nil)
	checks := (doctorRunner{}).doctorRuntime()
	if len(checks) != 1 || checks[0].Status != doctorFail || !strings.Contains(checks[0].Detail, "storageCritical") || !strings.Contains(checks[0].Remedy, "persistent disk") {
		t.Fatalf("critical storage failure overwritten: %+v", checks)
	}
}

func TestFederationReadCommandsDoNotCreateStateDatabase(t *testing.T) {
	for _, command := range [][]string{{"federation", "event", "list"}, {"federation", "event", "deliveries"}, {"federation", "deliveries", "summary"}, {"federation", "subscription", "runs", "--subscription", "test"}} {
		directory := t.TempDir()
		path := filepath.Join(directory, "state.db")
		args := append(append([]string{}, command...), "--state-file", path, "-o", "json")
		_ = run(args, &bytes.Buffer{}, &bytes.Buffer{})
		files, err := os.ReadDir(directory)
		if err != nil || len(files) != 0 {
			t.Fatalf("read command %v created state files: %+v %v", command, files, err)
		}
	}
}

func TestTrafficTableDistinguishesMissingAccountingFromObservedZero(t *testing.T) {
	var output bytes.Buffer
	rows := []logstore.TrafficFlow{{ClientAddress: "2001:db8::1", ClientPort: 443, PeerAddress: "2001:db8::2", PeerPort: 443, Protocol: "tcp"}, {ClientAddress: "192.0.2.1", PeerAddress: "192.0.2.2", Protocol: "udp", BytesOut: counterFixture(0), BytesIn: counterFixture(0)}}
	if err := writeTrafficFlowsTable(&output, rows); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	unknown := strings.Fields(lines[1])
	zero := strings.Fields(lines[2])
	if len(unknown) != 9 || unknown[6] != "unknown" || unknown[7] != "unknown" || unknown[3] != "[2001:db8::1]:443" {
		t.Fatalf("missing accounting/IPv6 endpoints: %s", output.String())
	}
	if zero[6] != "0" || zero[7] != "0" {
		t.Fatalf("observed zero was lost: %s", output.String())
	}
}

func TestConnectionTableDoesNotInventStatelessOrHidePortTranslation(t *testing.T) {
	var output bytes.Buffer
	entry := observe.ConnectionEntry{Family: "ipv6", Protocol: "udp", Original: observe.ConntrackTuple{Source: "2001:db8::1", SourcePort: "1234", Destination: "2001:db8::2", DestinationPort: "53"}, Reply: observe.ConntrackTuple{Source: "2001:db8::2", SourcePort: "53", Destination: "2001:db8::1", DestinationPort: "4321"}}
	if err := writeConnectionsTable(&output, observe.ConnectionTable{Entries: []observe.ConnectionEntry{entry}, Count: 1}); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "stateless") || !strings.Contains(got, "unknown") || !strings.Contains(got, "reply-dst=[2001:db8::1]:4321") {
		t.Fatalf("invented state or missing port NAT: %s", got)
	}
	entry.Reply.DestinationPort = ""
	if translation := connectionNATDelta(entry); translation != "" {
		t.Fatalf("missing reply port invented translation: %s", translation)
	}
}
