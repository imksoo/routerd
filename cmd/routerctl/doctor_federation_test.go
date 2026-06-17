// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestDoctorFederationPassAllDelivered(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "n",
		ObservedAt: now.Add(-10 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := store.RecordFederationEvent(ev); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.RecordDelivery("evt-1", "peer-a"); err != nil {
		t.Fatalf("record delivery: %v", err)
	}
	if err := store.UpdateDeliveryStatus("evt-1", "peer-a", routerstate.DeliveryDelivered, 1, "", now, ev.ExpiresAt); err != nil {
		t.Fatalf("update delivery: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor federation: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "PASS") {
		t.Fatalf("expected PASS in output:\n%s", got)
	}
	if strings.Contains(got, "FAIL") {
		t.Fatalf("unexpected FAIL in output:\n%s", got)
	}
}

func TestDoctorFederationFailOnFailedDelivery(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "n",
		ObservedAt: now.Add(-10 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := store.RecordFederationEvent(ev); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.RecordDelivery("evt-1", "peer-a"); err != nil {
		t.Fatalf("record delivery: %v", err)
	}
	if err := store.UpdateDeliveryStatus("evt-1", "peer-a", routerstate.DeliveryFailed, 3, "connection refused", time.Time{}, time.Time{}); err != nil {
		t.Fatalf("update delivery: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("expected error from doctor with failed deliveries:\n%s", out.String())
	}
	got := out.String()
	if !strings.Contains(got, "FAIL") || !strings.Contains(got, "failed") {
		t.Fatalf("expected FAIL with failed delivery detail:\n%s", got)
	}
}

func TestDoctorFederationWarnOnStaleTTL(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	// Two events: one delivered with fresh TTL, one with stale TTL.
	for _, ev := range []routerstate.EventRecord{
		{ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "n", ObservedAt: now.Add(-30 * time.Second), ExpiresAt: now.Add(10 * time.Minute)},
		{ID: "evt-2", Group: "cloudedge", Type: "t", SourceNode: "n", ObservedAt: now.Add(-20 * time.Second), ExpiresAt: now.Add(10 * time.Minute)},
	} {
		if err := store.RecordFederationEvent(ev); err != nil {
			t.Fatalf("record event %s: %v", ev.ID, err)
		}
	}
	for _, d := range []struct {
		eventID        string
		eventExpiresAt time.Time
	}{
		{"evt-1", now.Add(5 * time.Minute)},  // stale: older than event's 10m
		{"evt-2", now.Add(10 * time.Minute)},  // fresh: matches event
	} {
		if err := store.RecordDelivery(d.eventID, "peer-a"); err != nil {
			t.Fatalf("record delivery %s: %v", d.eventID, err)
		}
		if err := store.UpdateDeliveryStatus(d.eventID, "peer-a", routerstate.DeliveryDelivered, 1, "", now, d.eventExpiresAt); err != nil {
			t.Fatalf("update delivery %s: %v", d.eventID, err)
		}
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor federation: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "WARN") || !strings.Contains(got, "stale TTL") {
		t.Fatalf("expected WARN with stale TTL detail:\n%s", got)
	}
}

func TestDoctorFederationWarnOnHighLag(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	observedAt := now.Add(-90 * time.Second)
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "n",
		ObservedAt: observedAt, ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := store.RecordFederationEvent(ev); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.RecordDelivery("evt-1", "peer-a"); err != nil {
		t.Fatalf("record delivery: %v", err)
	}
	deliveredAt := now.Add(-5 * time.Second)
	if err := store.UpdateDeliveryStatus("evt-1", "peer-a", routerstate.DeliveryDelivered, 1, "", deliveredAt, ev.ExpiresAt); err != nil {
		t.Fatalf("update delivery: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor federation: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "WARN") || !strings.Contains(got, "lag") {
		t.Fatalf("expected WARN with lag detail:\n%s", got)
	}
}

func TestDoctorFederationSkipNoDeliveries(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)

	var out bytes.Buffer
	if err := run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor federation: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "SKIP") {
		t.Fatalf("expected SKIP when no deliveries exist:\n%s", got)
	}
}

func TestDoctorFederationJSONOutput(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "n",
		ObservedAt: now.Add(-10 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := store.RecordFederationEvent(ev); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.RecordDelivery("evt-1", "peer-a"); err != nil {
		t.Fatalf("record delivery: %v", err)
	}
	if err := store.UpdateDeliveryStatus("evt-1", "peer-a", routerstate.DeliveryDelivered, 1, "", now, ev.ExpiresAt); err != nil {
		t.Fatalf("update delivery: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor federation json: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if report.Summary.Overall != "pass" {
		t.Fatalf("overall = %q, want pass", report.Summary.Overall)
	}
	hasArea := false
	for _, c := range report.Checks {
		if c.Area == "federation" {
			hasArea = true
			break
		}
	}
	if !hasArea {
		t.Fatalf("no federation checks in report: %+v", report.Checks)
	}
}

func TestDoctorFederationPendingExpiringSoon(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "n",
		ObservedAt: now.Add(-60 * time.Second), ExpiresAt: now.Add(30 * time.Second),
	}
	if err := store.RecordFederationEvent(ev); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.RecordDelivery("evt-1", "peer-a"); err != nil {
		t.Fatalf("record delivery: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("expected error from doctor with pending expiring-soon event:\n%s", out.String())
	}
	got := out.String()
	if !strings.Contains(got, "FAIL") || !strings.Contains(got, "expire") {
		t.Fatalf("expected FAIL with expiring detail:\n%s", got)
	}
}
