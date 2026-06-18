// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/config"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// --- SLO schema: custom thresholds override defaults ---

func writeDoctorFederationSLOFixture(t *testing.T, selfNode string, peers []struct{ name, endpoint string }, sloYAML string) (string, string) {
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
` + peerYAML + sloYAML)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func TestDoctorFederationSLOCustomLagThresholds(t *testing.T) {
	slo := `    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: cloudedge-slo
      spec:
        groupRef: cloudedge
        delivery:
          lagWarnSeconds: 10
          lagFailSeconds: 30
          expiresSoonSeconds: 60
`
	configPath, statePath := writeDoctorFederationSLOFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}}, slo)
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	// Delivery lag of 15s: above custom warn(10s), below custom fail(30s).
	// With default thresholds (warn=60, fail=180) this would PASS.
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-15 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
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
	if !strings.Contains(got, "WARN") || !strings.Contains(got, "lag") {
		t.Fatalf("expected WARN with SLO-derived lag threshold (10s); output:\n%s", got)
	}
}

func TestDoctorFederationSLOCustomLagFail(t *testing.T) {
	slo := `    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: cloudedge-slo
      spec:
        groupRef: cloudedge
        delivery:
          lagWarnSeconds: 10
          lagFailSeconds: 30
`
	configPath, statePath := writeDoctorFederationSLOFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}}, slo)
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	// Delivery lag of 35s: above custom fail(30s).
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-35 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
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
	err := run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("expected error from doctor with lag exceeding SLO fail threshold:\n%s", out.String())
	}
	got := out.String()
	if !strings.Contains(got, "FAIL") || !strings.Contains(got, "lag") {
		t.Fatalf("expected FAIL with SLO-derived lag threshold (30s); output:\n%s", got)
	}
	if !strings.Contains(got, "SLO fail threshold 30") {
		t.Fatalf("expected SLO threshold in remedy text; output:\n%s", got)
	}
}

func TestDoctorFederationSLODefaultsMatchHardcoded(t *testing.T) {
	// No FederationSLO resource: defaults should match the original constants.
	configPath, statePath := writeDoctorFederationFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}})
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	// Lag of 70s: above original warn(60), below original fail(180).
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-70 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
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
	if !strings.Contains(got, "WARN") || !strings.Contains(got, "lag") {
		t.Fatalf("expected WARN at 70s with default threshold 60s:\n%s", got)
	}
}

// --- SLO-aware JSON output ---

func TestDoctorFederationSLOJSONOutput(t *testing.T) {
	slo := `    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: cloudedge-slo
      spec:
        groupRef: cloudedge
        delivery:
          lagWarnSeconds: 10
          lagFailSeconds: 30
          expiresSoonSeconds: 60
`
	configPath, statePath := writeDoctorFederationSLOFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}}, slo)
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-15 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
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
		t.Fatalf("doctor: %v\n%s", err, out.String())
	}

	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if report.Federation == nil {
		t.Fatal("federation summary is nil")
	}
	if report.Federation.SLO == nil {
		t.Fatal("federation SLO status is nil")
	}
	sloStatus := report.Federation.SLO
	if !sloStatus.Defined {
		t.Error("expected slo.defined=true")
	}
	if sloStatus.Thresholds.LagWarnSeconds != 10 {
		t.Errorf("slo.thresholds.lagWarnSeconds = %d, want 10", sloStatus.Thresholds.LagWarnSeconds)
	}
	if sloStatus.Thresholds.LagFailSeconds != 30 {
		t.Errorf("slo.thresholds.lagFailSeconds = %d, want 30", sloStatus.Thresholds.LagFailSeconds)
	}
	if sloStatus.Thresholds.ExpiresSoonSeconds != 60 {
		t.Errorf("slo.thresholds.expiresSoonSeconds = %d, want 60", sloStatus.Thresholds.ExpiresSoonSeconds)
	}
	if len(sloStatus.Violations) == 0 {
		t.Fatal("expected at least one SLO violation (lag warn)")
	}
	found := false
	for _, v := range sloStatus.Violations {
		if strings.Contains(v.Check, "delivery lag") && v.Severity == "warn" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected delivery lag warn violation; got: %+v", sloStatus.Violations)
	}
}

func TestDoctorFederationSLOJSONNoSLOResource(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "n",
		ObservedAt: now.Add(-5 * time.Second), ExpiresAt: now.Add(30 * time.Minute),
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
		t.Fatalf("doctor: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if report.Federation == nil || report.Federation.SLO == nil {
		t.Fatal("expected SLO status even without SLO resource")
	}
	if report.Federation.SLO.Defined {
		t.Error("expected slo.defined=false when no FederationSLO resource")
	}
	if report.Federation.SLO.Thresholds.LagWarnSeconds != defaultFederationWarnLag {
		t.Errorf("default lagWarnSeconds = %d, want %d", report.Federation.SLO.Thresholds.LagWarnSeconds, defaultFederationWarnLag)
	}
}

// --- Remediation plan ---

func TestDoctorFederationRemediationPlanFailedDelivery(t *testing.T) {
	configPath, statePath := writeDoctorFederationFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}})
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
	if err := store.UpdateDeliveryStatus("evt-1", "peer-a", routerstate.DeliveryFailed, 3, "timeout", time.Time{}, ev.ExpiresAt); err != nil {
		t.Fatalf("update delivery: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	_ = run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json", "--remediation-plan"}, &out, &bytes.Buffer{})
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if report.RemediationPlan == nil {
		t.Fatal("remediation plan is nil")
	}
	if len(report.RemediationPlan.Actions) == 0 {
		t.Fatal("expected at least one remediation action")
	}
	found := false
	for _, a := range report.RemediationPlan.Actions {
		if a.Action == "retry-failed-deliveries" {
			found = true
			if a.TargetPeer != "peer-a" {
				t.Errorf("targetPeer = %q, want peer-a", a.TargetPeer)
			}
			if a.TargetGroup != "cloudedge" {
				t.Errorf("targetGroup = %q, want cloudedge", a.TargetGroup)
			}
			if !a.Safe {
				t.Error("retry-failed-deliveries should be safe")
			}
			if a.RequiresOperatorApproval {
				t.Error("retry-failed-deliveries should not require operator approval")
			}
		}
	}
	if !found {
		t.Errorf("expected retry-failed-deliveries action; got: %+v", report.RemediationPlan.Actions)
	}
}

func TestDoctorFederationRemediationPlanEmptyEndpoint(t *testing.T) {
	configPath, statePath := writeDoctorFederationFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", ""}})
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
	_ = run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json", "--remediation-plan"}, &out, &bytes.Buffer{})
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if report.RemediationPlan == nil {
		t.Fatal("remediation plan is nil")
	}
	found := false
	for _, a := range report.RemediationPlan.Actions {
		if a.Action == "configure-peer-endpoint" {
			found = true
			if a.Safe {
				t.Error("configure-peer-endpoint should not be safe")
			}
			if !a.RequiresOperatorApproval {
				t.Error("configure-peer-endpoint should require operator approval")
			}
		}
	}
	if !found {
		t.Errorf("expected configure-peer-endpoint action; got: %+v", report.RemediationPlan.Actions)
	}
}

func TestDoctorFederationRemediationPlanHealthy(t *testing.T) {
	configPath, statePath := writeDoctorFederationFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}})
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-5 * time.Second), ExpiresAt: now.Add(30 * time.Minute),
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
	if err := run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json", "--remediation-plan"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if report.RemediationPlan == nil {
		t.Fatal("remediation plan is nil even when healthy")
	}
	if len(report.RemediationPlan.Actions) != 0 {
		t.Errorf("expected zero remediation actions when healthy; got %d: %+v", len(report.RemediationPlan.Actions), report.RemediationPlan.Actions)
	}
}

// --- Fault injection: SLO-based violation detection ---

func TestDoctorFederationFaultInjectionLagViolation(t *testing.T) {
	// Inject: delivery lag at 200s (above default SLO fail=180s).
	// Expect: SLO violation reported in JSON with severity=fail.
	configPath, statePath := writeDoctorFederationFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}})
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-200 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
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
	_ = run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{})
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if report.Federation == nil || report.Federation.SLO == nil {
		t.Fatal("missing SLO status")
	}
	found := false
	for _, v := range report.Federation.SLO.Violations {
		if strings.Contains(v.Check, "delivery lag") && v.Severity == "fail" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected delivery lag fail violation at 200s; got: %+v", report.Federation.SLO.Violations)
	}
}

func TestDoctorFederationFaultInjectionFailedDeliveryViolation(t *testing.T) {
	configPath, statePath := writeDoctorFederationFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}})
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-5 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := store.RecordFederationEvent(ev); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.RecordDelivery("evt-1", "peer-a"); err != nil {
		t.Fatalf("record delivery: %v", err)
	}
	if err := store.UpdateDeliveryStatus("evt-1", "peer-a", routerstate.DeliveryFailed, 3, "connection refused", time.Time{}, ev.ExpiresAt); err != nil {
		t.Fatalf("update delivery: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	_ = run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{})
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if report.Federation == nil || report.Federation.SLO == nil {
		t.Fatal("missing SLO status")
	}
	found := false
	for _, v := range report.Federation.SLO.Violations {
		if strings.Contains(v.Check, "failed deliveries") && v.Severity == "fail" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected failed deliveries violation; got: %+v", report.Federation.SLO.Violations)
	}
}

func TestDoctorFederationFaultInjectionPendingExpiringSoonViolation(t *testing.T) {
	configPath, statePath := writeDoctorFederationFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}})
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	// Event expires in 30s (below default SLO expiresSoonSeconds=120s),
	// with a pending delivery.
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-60 * time.Second), ExpiresAt: now.Add(30 * time.Second),
	}
	if err := store.RecordFederationEvent(ev); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.RecordDelivery("evt-1", "peer-a"); err != nil {
		t.Fatalf("record delivery: %v", err)
	}
	// Leave as pending (no UpdateDeliveryStatus to delivered/failed).
	closeDoctorState(t, store)

	var out bytes.Buffer
	_ = run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{})
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if report.Federation == nil || report.Federation.SLO == nil {
		t.Fatal("missing SLO status")
	}
	found := false
	for _, v := range report.Federation.SLO.Violations {
		if strings.Contains(v.Check, "pending expiring-soon") && v.Severity == "fail" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected pending expiring-soon violation; got: %+v", report.Federation.SLO.Violations)
	}
}

func TestDoctorFederationFaultInjectionCustomSLORecovery(t *testing.T) {
	// With a custom SLO (lagWarnSeconds=100), a delivery lag of 70s should PASS.
	slo := `    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: cloudedge-slo
      spec:
        groupRef: cloudedge
        delivery:
          lagWarnSeconds: 100
          lagFailSeconds: 300
`
	configPath, statePath := writeDoctorFederationSLOFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}}, slo)
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-70 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
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
		t.Fatalf("doctor should pass with custom SLO (lagWarn=100s, actual=70s): %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if report.Federation == nil || report.Federation.SLO == nil {
		t.Fatal("missing SLO status")
	}
	for _, v := range report.Federation.SLO.Violations {
		if strings.Contains(v.Check, "delivery lag") {
			t.Errorf("unexpected lag violation with custom SLO allowing 100s warn: %+v", v)
		}
	}
}

func TestDoctorFederationRemediationPlanStaleTTL(t *testing.T) {
	configPath, statePath := writeDoctorFederationFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}})
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-30 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := store.RecordFederationEvent(ev); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.RecordDelivery("evt-1", "peer-a"); err != nil {
		t.Fatalf("record delivery: %v", err)
	}
	// Stale TTL: delivery's eventExpiresAt is older than event's ExpiresAt.
	if err := store.UpdateDeliveryStatus("evt-1", "peer-a", routerstate.DeliveryDelivered, 1, "", now, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("update delivery: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	// Stale TTL when all delivered events are stale is a FAIL check, so run()
	// returns an error. The remediation plan is still generated.
	_ = run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json", "--remediation-plan"}, &out, &bytes.Buffer{})
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if report.RemediationPlan == nil {
		t.Fatal("remediation plan is nil")
	}
	found := false
	for _, a := range report.RemediationPlan.Actions {
		if a.Action == "force-repush-stale-ttl" {
			found = true
			if !a.Safe {
				t.Error("force-repush-stale-ttl should be safe")
			}
		}
	}
	if !found {
		t.Errorf("expected force-repush-stale-ttl action; got: %+v", report.RemediationPlan.Actions)
	}
}

// --- Validation tests for FederationSLO kind ---

func TestFederationSLOValidation(t *testing.T) {
	slo := `    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: cloudedge-slo
      spec:
        groupRef: cloudedge
        delivery:
          lagWarnSeconds: 30
          lagFailSeconds: 120
          expiresSoonSeconds: 60
`
	configPath, _ := writeDoctorFederationSLOFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}}, slo)
	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if err := config.Validate(router); err != nil {
		t.Fatalf("valid SLO should pass validation: %v", err)
	}
}

func TestFederationSLOValidationWarnGTEFail(t *testing.T) {
	slo := `    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: bad-slo
      spec:
        groupRef: cloudedge
        delivery:
          lagWarnSeconds: 120
          lagFailSeconds: 60
`
	configPath, _ := writeDoctorFederationSLOFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}}, slo)
	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	err = config.Validate(router)
	if err == nil {
		t.Fatal("expected validation error for lagWarnSeconds >= lagFailSeconds")
	}
	if !strings.Contains(err.Error(), "lagWarnSeconds") {
		t.Fatalf("expected lagWarnSeconds in error; got: %v", err)
	}
}

func TestFederationSLOValidationMissingGroupRef(t *testing.T) {
	slo := `    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: bad-slo
      spec:
        delivery:
          lagWarnSeconds: 30
          lagFailSeconds: 120
`
	configPath, _ := writeDoctorFederationSLOFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}}, slo)
	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	err = config.Validate(router)
	if err == nil {
		t.Fatal("expected validation error for missing groupRef")
	}
	if !strings.Contains(err.Error(), "groupRef") {
		t.Fatalf("expected groupRef in error; got: %v", err)
	}
}

// --- Cross-resource validation tests ---

func TestFederationSLOValidationDuplicateGroupRef(t *testing.T) {
	slo := `    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: slo-1
      spec:
        groupRef: cloudedge
        delivery:
          lagWarnSeconds: 30
          lagFailSeconds: 120
    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: slo-2
      spec:
        groupRef: cloudedge
        delivery:
          lagWarnSeconds: 60
          lagFailSeconds: 180
`
	configPath, _ := writeDoctorFederationSLOFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}}, slo)
	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	err = config.Validate(router)
	if err == nil {
		t.Fatal("expected validation error for duplicate groupRef")
	}
	if !strings.Contains(err.Error(), "conflicts") || !strings.Contains(err.Error(), "cloudedge") {
		t.Fatalf("expected conflict error for cloudedge; got: %v", err)
	}
}

func TestFederationSLOValidationGroupRefNotFound(t *testing.T) {
	slo := `    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: bad-slo
      spec:
        groupRef: nonexistent-group
        delivery:
          lagWarnSeconds: 30
          lagFailSeconds: 120
`
	configPath, _ := writeDoctorFederationSLOFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}}, slo)
	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	err = config.Validate(router)
	if err == nil {
		t.Fatal("expected validation error for non-existent groupRef")
	}
	if !strings.Contains(err.Error(), "nonexistent-group") || !strings.Contains(err.Error(), "does not reference") {
		t.Fatalf("expected 'does not reference' error; got: %v", err)
	}
}

// --- Remediation plan safety tests ---

func TestDoctorFederationRemediationPlanNoStateMutation(t *testing.T) {
	configPath, statePath := writeDoctorFederationFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}})
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
	if err := store.UpdateDeliveryStatus("evt-1", "peer-a", routerstate.DeliveryFailed, 3, "timeout", time.Time{}, ev.ExpiresAt); err != nil {
		t.Fatalf("update delivery: %v", err)
	}

	// Snapshot state before remediation plan
	deliveriesBefore, err := store.ListDeliveries("evt-1", "peer-a")
	if err != nil {
		t.Fatalf("list deliveries before: %v", err)
	}
	eventsBefore, err := store.ListFederationEvents("cloudedge", true, now.Unix())
	if err != nil {
		t.Fatalf("list events before: %v", err)
	}
	closeDoctorState(t, store)

	// Run doctor with --remediation-plan
	var out bytes.Buffer
	_ = run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json", "--remediation-plan"}, &out, &bytes.Buffer{})

	// Verify state is unchanged
	store2 := openDoctorState(t, statePath)
	deliveriesAfter, err := store2.ListDeliveries("evt-1", "peer-a")
	if err != nil {
		t.Fatalf("list deliveries after: %v", err)
	}
	eventsAfter, err := store2.ListFederationEvents("cloudedge", true, now.Unix())
	if err != nil {
		t.Fatalf("list events after: %v", err)
	}
	closeDoctorState(t, store2)

	if len(deliveriesBefore) != len(deliveriesAfter) {
		t.Fatalf("delivery count changed: %d → %d", len(deliveriesBefore), len(deliveriesAfter))
	}
	for i := range deliveriesBefore {
		if deliveriesBefore[i].Status != deliveriesAfter[i].Status {
			t.Errorf("delivery[%d] status changed: %q → %q", i, deliveriesBefore[i].Status, deliveriesAfter[i].Status)
		}
		if deliveriesBefore[i].Attempts != deliveriesAfter[i].Attempts {
			t.Errorf("delivery[%d] attempts changed: %d → %d", i, deliveriesBefore[i].Attempts, deliveriesAfter[i].Attempts)
		}
	}
	if len(eventsBefore) != len(eventsAfter) {
		t.Fatalf("event count changed: %d → %d", len(eventsBefore), len(eventsAfter))
	}
}

func TestDoctorFederationRemediationPlanNoDuplicateActions(t *testing.T) {
	configPath, statePath := writeDoctorFederationFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}})
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	// Create multiple failed events to the same peer to check for duplicate actions
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("evt-%d", i+1)
		ev := routerstate.EventRecord{
			ID: id, Group: "cloudedge", Type: "t", SourceNode: "self-node",
			ObservedAt: now.Add(-10 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
		}
		if err := store.RecordFederationEvent(ev); err != nil {
			t.Fatalf("record event %s: %v", id, err)
		}
		if err := store.RecordDelivery(id, "peer-a"); err != nil {
			t.Fatalf("record delivery %s: %v", id, err)
		}
		if err := store.UpdateDeliveryStatus(id, "peer-a", routerstate.DeliveryFailed, 3, "timeout", time.Time{}, ev.ExpiresAt); err != nil {
			t.Fatalf("update delivery %s: %v", id, err)
		}
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	_ = run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json", "--remediation-plan"}, &out, &bytes.Buffer{})
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if report.RemediationPlan == nil {
		t.Fatal("remediation plan is nil")
	}

	// Check for duplicate (action, targetGroup, targetPeer) triples
	type actionKey struct{ action, group, peer string }
	seen := map[actionKey]int{}
	for _, a := range report.RemediationPlan.Actions {
		k := actionKey{a.Action, a.TargetGroup, a.TargetPeer}
		seen[k]++
	}
	for k, count := range seen {
		if count > 1 {
			t.Errorf("duplicate remediation action: %+v appeared %d times", k, count)
		}
	}
}

func TestDoctorFederationRemediationPlanDeterministicOrdering(t *testing.T) {
	configPath, statePath := writeDoctorFederationFixture(t, "self-node",
		[]struct{ name, endpoint string }{
			{"peer-a", "http://10.0.0.1:8787"},
			{"peer-b", "http://10.0.0.2:8787"},
		})
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-200 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := store.RecordFederationEvent(ev); err != nil {
		t.Fatalf("record event: %v", err)
	}
	for _, peer := range []string{"peer-a", "peer-b"} {
		if err := store.RecordDelivery("evt-1", peer); err != nil {
			t.Fatalf("record delivery %s: %v", peer, err)
		}
		if err := store.UpdateDeliveryStatus("evt-1", peer, routerstate.DeliveryFailed, 3, "timeout", time.Time{}, ev.ExpiresAt); err != nil {
			t.Fatalf("update delivery %s: %v", peer, err)
		}
	}
	closeDoctorState(t, store)

	var results []string
	for i := 0; i < 3; i++ {
		var out bytes.Buffer
		_ = run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json", "--remediation-plan"}, &out, &bytes.Buffer{})
		results = append(results, out.String())
	}
	for i := 1; i < len(results); i++ {
		if results[i] != results[0] {
			t.Errorf("run %d output differs from run 0:\nrun 0: %s\nrun %d: %s", i, results[0], i, results[i])
		}
	}
}

// --- Backward compatibility test ---

func TestDoctorFederationBackwardCompatNoSLO(t *testing.T) {
	// No FederationSLO resource: old defaults, no schema changes required
	configPath, statePath := writeDoctorFederationFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}})
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-5 * time.Second), ExpiresAt: now.Add(30 * time.Minute),
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

	// Without --remediation-plan, JSON must not contain remediationPlan
	var out bytes.Buffer
	if err := run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	// RemediationPlan should be nil (not generated) without --remediation-plan flag
	if report.RemediationPlan != nil {
		t.Error("remediationPlan should not be present without --remediation-plan flag")
	}
	// Federation summary should use defaults
	if report.Federation == nil {
		t.Fatal("federation summary is nil")
	}
	if report.Federation.SLO == nil {
		t.Fatal("SLO status should still be present")
	}
	if report.Federation.SLO.Defined {
		t.Error("SLO.defined should be false without FederationSLO resource")
	}
	// Existing fields must be present (backward compat)
	if report.Federation.TotalEvents != 1 {
		t.Errorf("totalEvents = %d, want 1", report.Federation.TotalEvents)
	}
	if report.Federation.TotalDelivered != 1 {
		t.Errorf("totalDelivered = %d, want 1", report.Federation.TotalDelivered)
	}
}

// --- loadSLOThresholds order-independence test ---

func TestLoadSLOThresholdsOrderIndependent(t *testing.T) {
	// FederationSLO at beginning of resources
	sloFirst := `    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: cloudedge-slo
      spec:
        groupRef: cloudedge
        delivery:
          lagWarnSeconds: 25
          lagFailSeconds: 90
          expiresSoonSeconds: 45
`
	configPathFirst, _ := writeDoctorFederationSLOFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}}, sloFirst)
	routerFirst, err := config.Load(configPathFirst)
	if err != nil {
		t.Fatalf("config load first: %v", err)
	}
	thresholdsFirst := loadSLOThresholds(routerFirst, "cloudedge")

	// Verify the custom thresholds were loaded
	if thresholdsFirst.LagWarnSeconds != 25 {
		t.Errorf("LagWarnSeconds = %d, want 25", thresholdsFirst.LagWarnSeconds)
	}
	if thresholdsFirst.LagFailSeconds != 90 {
		t.Errorf("LagFailSeconds = %d, want 90", thresholdsFirst.LagFailSeconds)
	}
	if thresholdsFirst.ExpiresSoonSeconds != 45 {
		t.Errorf("ExpiresSoonSeconds = %d, want 45", thresholdsFirst.ExpiresSoonSeconds)
	}

	// Non-matching group should get defaults
	thresholdsOther := loadSLOThresholds(routerFirst, "other-group")
	if thresholdsOther.LagWarnSeconds != defaultFederationWarnLag {
		t.Errorf("non-matching group LagWarnSeconds = %d, want default %d", thresholdsOther.LagWarnSeconds, defaultFederationWarnLag)
	}

	// nil router should get defaults
	thresholdsNil := loadSLOThresholds(nil, "cloudedge")
	if thresholdsNil.LagWarnSeconds != defaultFederationWarnLag {
		t.Errorf("nil router LagWarnSeconds = %d, want default %d", thresholdsNil.LagWarnSeconds, defaultFederationWarnLag)
	}
}
