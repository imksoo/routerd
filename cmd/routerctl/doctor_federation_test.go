// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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

func writeDoctorFederationFixture(t *testing.T, selfNode string, peers []struct{ name, endpoint string }) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")

	var peerYAML string
	for _, p := range peers {
		peerYAML += `    - apiVersion: federation.routerd.net/v1alpha1
      kind: EventPeer
      metadata:
        name: peer-` + p.name + `
      spec:
        groupRef: cloudedge
        nodeName: ` + p.name + `
        endpoint: ` + p.endpoint + "\n"
	}

	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: eth0
        managed: false
        owner: external
    - apiVersion: federation.routerd.net/v1alpha1
      kind: EventGroup
      metadata:
        name: cloudedge
      spec:
        nodeName: ` + selfNode + `
` + peerYAML)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func TestDoctorFederationExpectedPeerPassAllDelivered(t *testing.T) {
	configPath, statePath := writeDoctorFederationFixture(t, "self-node",
		[]struct{ name, endpoint string }{
			{"peer-a", "http://10.0.0.1:8787"},
			{"peer-b", "http://10.0.0.2:8787"},
		})
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-10 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := store.RecordFederationEvent(ev); err != nil {
		t.Fatalf("record event: %v", err)
	}
	for _, peer := range []string{"peer-a", "peer-b"} {
		if err := store.RecordDelivery("evt-1", peer); err != nil {
			t.Fatalf("record delivery %s: %v", peer, err)
		}
		if err := store.UpdateDeliveryStatus("evt-1", peer, routerstate.DeliveryDelivered, 1, "", now, ev.ExpiresAt); err != nil {
			t.Fatalf("update delivery %s: %v", peer, err)
		}
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "expected delivery") || !strings.Contains(got, "PASS") {
		t.Fatalf("expected PASS for expected delivery checks:\n%s", got)
	}
}

func TestDoctorFederationExpectedPeerMissingDelivery(t *testing.T) {
	configPath, statePath := writeDoctorFederationFixture(t, "self-node",
		[]struct{ name, endpoint string }{
			{"peer-a", "http://10.0.0.1:8787"},
			{"peer-b", "http://10.0.0.2:8787"},
		})
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-10 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := store.RecordFederationEvent(ev); err != nil {
		t.Fatalf("record event: %v", err)
	}
	// Only deliver to peer-a; peer-b has no delivery row.
	if err := store.RecordDelivery("evt-1", "peer-a"); err != nil {
		t.Fatalf("record delivery: %v", err)
	}
	if err := store.UpdateDeliveryStatus("evt-1", "peer-a", routerstate.DeliveryDelivered, 1, "", now, ev.ExpiresAt); err != nil {
		t.Fatalf("update delivery: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("expected error from doctor with missing delivery row:\n%s", out.String())
	}
	got := out.String()
	if !strings.Contains(got, "FAIL") || !strings.Contains(got, "peer-b") || !strings.Contains(got, "no delivery row") {
		t.Fatalf("expected FAIL for peer-b missing delivery:\n%s", got)
	}
}

func TestDoctorFederationExpectedPeerSelfExcluded(t *testing.T) {
	// Self-node appears as an EventPeer but should be excluded.
	configPath, statePath := writeDoctorFederationFixture(t, "self-node",
		[]struct{ name, endpoint string }{
			{"self-node", "http://10.0.0.1:8787"},
			{"peer-a", "http://10.0.0.2:8787"},
		})
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "self-node",
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
		t.Fatalf("doctor: %v\n%s", err, out.String())
	}
	got := out.String()
	// self-node should not appear in expected delivery checks.
	if strings.Contains(got, "self-node") && strings.Contains(got, "expected delivery") && strings.Contains(got, "FAIL") {
		t.Fatalf("self-node should be excluded from expected peer checks:\n%s", got)
	}
}

func TestDoctorFederationExpectedPeerEmptyEndpoint(t *testing.T) {
	configPath, statePath := writeDoctorFederationFixture(t, "self-node",
		[]struct{ name, endpoint string }{
			{"peer-a", ""},
		})
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-10 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := store.RecordFederationEvent(ev); err != nil {
		t.Fatalf("record event: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("expected error from doctor with empty endpoint:\n%s", out.String())
	}
	got := out.String()
	if !strings.Contains(got, "FAIL") || !strings.Contains(got, "endpoint is empty") {
		t.Fatalf("expected FAIL for empty endpoint:\n%s", got)
	}
}

func TestDoctorFederationExpectedPeerSkipNoSelfEvents(t *testing.T) {
	configPath, statePath := writeDoctorFederationFixture(t, "self-node",
		[]struct{ name, endpoint string }{
			{"peer-a", "http://10.0.0.1:8787"},
		})
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	// Event from a different source node, not self-emitted.
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "other-node",
		ObservedAt: now.Add(-10 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := store.RecordFederationEvent(ev); err != nil {
		t.Fatalf("record event: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "no self-emitted") {
		t.Fatalf("expected skip for no self-emitted events:\n%s", got)
	}
}
