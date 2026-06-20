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

// sloGroupByName returns the per-group SLO status for the given group name, or
// nil if no matching group is found.
func sloGroupByName(slo *doctorFederationSLOStatus, group string) *doctorFederationSLOGroupStatus {
	if slo == nil {
		return nil
	}
	for i := range slo.Groups {
		if slo.Groups[i].Group == group {
			return &slo.Groups[i]
		}
	}
	return nil
}

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
	if len(sloStatus.Groups) == 0 {
		t.Fatal("expected at least one SLO group")
	}
	grp := sloStatus.Groups[0]
	if !grp.Defined {
		t.Error("expected group.defined=true")
	}
	if grp.Thresholds.Delivery.LagWarnSeconds != 10 {
		t.Errorf("group.thresholds.delivery.lagWarnSeconds = %d, want 10", grp.Thresholds.Delivery.LagWarnSeconds)
	}
	if grp.Thresholds.Delivery.LagFailSeconds != 30 {
		t.Errorf("group.thresholds.delivery.lagFailSeconds = %d, want 30", grp.Thresholds.Delivery.LagFailSeconds)
	}
	if grp.Thresholds.Delivery.ExpiresSoonSeconds != 60 {
		t.Errorf("group.thresholds.delivery.expiresSoonSeconds = %d, want 60", grp.Thresholds.Delivery.ExpiresSoonSeconds)
	}
	if len(grp.Violations) == 0 {
		t.Fatal("expected at least one SLO violation (lag warn)")
	}
	found := false
	for _, v := range grp.Violations {
		if strings.Contains(v.Check, "delivery lag") && v.Severity == "warn" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected delivery lag warn violation; got: %+v", grp.Violations)
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
	if len(report.Federation.SLO.Groups) == 0 {
		t.Fatal("expected at least one SLO group")
	}
	grp := report.Federation.SLO.Groups[0]
	if grp.Defined {
		t.Error("expected group.defined=false when no FederationSLO resource")
	}
	if grp.Thresholds.Delivery.LagWarnSeconds != defaultFederationWarnLag {
		t.Errorf("default lagWarnSeconds = %d, want %d", grp.Thresholds.Delivery.LagWarnSeconds, defaultFederationWarnLag)
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
	for _, grp := range report.Federation.SLO.Groups {
		for _, v := range grp.Violations {
			if strings.Contains(v.Check, "delivery lag") && v.Severity == "fail" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected delivery lag fail violation at 200s; got groups: %+v", report.Federation.SLO.Groups)
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
	for _, grp := range report.Federation.SLO.Groups {
		for _, v := range grp.Violations {
			if strings.Contains(v.Check, "failed deliveries") && v.Severity == "fail" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected failed deliveries violation; got groups: %+v", report.Federation.SLO.Groups)
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
	for _, grp := range report.Federation.SLO.Groups {
		for _, v := range grp.Violations {
			if strings.Contains(v.Check, "pending expiring-soon") && v.Severity == "fail" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected pending expiring-soon violation; got groups: %+v", report.Federation.SLO.Groups)
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
	for _, grp := range report.Federation.SLO.Groups {
		for _, v := range grp.Violations {
			if strings.Contains(v.Check, "delivery lag") {
				t.Errorf("unexpected lag violation with custom SLO allowing 100s warn: %+v", v)
			}
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

	var results [][]string
	for i := 0; i < 3; i++ {
		var out bytes.Buffer
		_ = run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json", "--remediation-plan"}, &out, &bytes.Buffer{})
		var report doctorReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatalf("run %d decode: %v\n%s", i, err, out.String())
		}
		if report.RemediationPlan == nil {
			t.Fatalf("run %d remediation plan is nil", i)
		}
		var actions []string
		for _, a := range report.RemediationPlan.Actions {
			actions = append(actions, a.Action+"/"+a.TargetGroup+"/"+a.TargetPeer)
		}
		results = append(results, actions)
	}
	for i := 1; i < len(results); i++ {
		if strings.Join(results[i], "\n") != strings.Join(results[0], "\n") {
			t.Errorf("run %d actions differ from run 0:\nrun 0: %v\nrun %d: %v", i, results[0], i, results[i])
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
	if len(report.Federation.SLO.Groups) == 0 {
		t.Fatal("expected at least one SLO group")
	}
	if report.Federation.SLO.Groups[0].Defined {
		t.Error("SLO group.defined should be false without FederationSLO resource")
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

// --- Multi-group test: 2 groups with different SLO thresholds ---

func writeMultiGroupFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
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
        name: alpha
      spec:
        nodeName: self-node
    - apiVersion: federation.routerd.net/v1alpha1
      kind: EventGroup
      metadata:
        name: beta
      spec:
        nodeName: self-node
    - apiVersion: federation.routerd.net/v1alpha1
      kind: EventPeer
      metadata:
        name: peer-alpha
      spec:
        groupRef: alpha
        nodeName: peer-a
        endpoint: http://10.0.0.1:8787
    - apiVersion: federation.routerd.net/v1alpha1
      kind: EventPeer
      metadata:
        name: peer-beta
      spec:
        groupRef: beta
        nodeName: peer-b
        endpoint: http://10.0.0.2:8787
    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: alpha-slo
      spec:
        groupRef: alpha
        delivery:
          lagWarnSeconds: 10
          lagFailSeconds: 30
    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: beta-slo
      spec:
        groupRef: beta
        delivery:
          lagWarnSeconds: 100
          lagFailSeconds: 300
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func TestDoctorFederationMultiGroupSLOJSON(t *testing.T) {
	configPath, statePath := writeMultiGroupFixture(t)
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()
	// alpha group: lag 15s → above alpha warn(10), below alpha fail(30) → WARN
	evA := routerstate.EventRecord{
		ID: "evt-a1", Group: "alpha", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-15 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := store.RecordFederationEvent(evA); err != nil {
		t.Fatalf("record event alpha: %v", err)
	}
	if err := store.RecordDelivery("evt-a1", "peer-a"); err != nil {
		t.Fatalf("record delivery alpha: %v", err)
	}
	if err := store.UpdateDeliveryStatus("evt-a1", "peer-a", routerstate.DeliveryDelivered, 1, "", now, evA.ExpiresAt); err != nil {
		t.Fatalf("update delivery alpha: %v", err)
	}

	// beta group: lag 15s → below beta warn(100) → PASS
	evB := routerstate.EventRecord{
		ID: "evt-b1", Group: "beta", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-15 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := store.RecordFederationEvent(evB); err != nil {
		t.Fatalf("record event beta: %v", err)
	}
	if err := store.RecordDelivery("evt-b1", "peer-b"); err != nil {
		t.Fatalf("record delivery beta: %v", err)
	}
	if err := store.UpdateDeliveryStatus("evt-b1", "peer-b", routerstate.DeliveryDelivered, 1, "", now, evB.ExpiresAt); err != nil {
		t.Fatalf("update delivery beta: %v", err)
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
		t.Fatal("missing SLO status")
	}
	if len(report.Federation.SLO.Groups) != 2 {
		t.Fatalf("expected 2 SLO groups; got %d", len(report.Federation.SLO.Groups))
	}

	groupMap := map[string]doctorFederationSLOGroupStatus{}
	for _, g := range report.Federation.SLO.Groups {
		groupMap[g.Group] = g
	}

	// alpha: custom SLO (10/30), should have lag warn violation
	alphaG, ok := groupMap["alpha"]
	if !ok {
		t.Fatal("alpha group not found in SLO groups")
	}
	if !alphaG.Defined {
		t.Error("alpha group should have defined=true")
	}
	if alphaG.Thresholds.Delivery.LagWarnSeconds != 10 {
		t.Errorf("alpha lagWarnSeconds = %d, want 10", alphaG.Thresholds.Delivery.LagWarnSeconds)
	}
	if alphaG.Thresholds.Delivery.LagFailSeconds != 30 {
		t.Errorf("alpha lagFailSeconds = %d, want 30", alphaG.Thresholds.Delivery.LagFailSeconds)
	}
	alphaHasLagViolation := false
	for _, v := range alphaG.Violations {
		if strings.Contains(v.Check, "delivery lag") && v.Severity == "warn" {
			alphaHasLagViolation = true
		}
	}
	if !alphaHasLagViolation {
		t.Errorf("alpha should have lag warn violation at 15s (warn=10s); violations: %+v", alphaG.Violations)
	}

	// beta: custom SLO (100/300), lag 15s is well below warn → no violation
	betaG, ok := groupMap["beta"]
	if !ok {
		t.Fatal("beta group not found in SLO groups")
	}
	if !betaG.Defined {
		t.Error("beta group should have defined=true")
	}
	if betaG.Thresholds.Delivery.LagWarnSeconds != 100 {
		t.Errorf("beta lagWarnSeconds = %d, want 100", betaG.Thresholds.Delivery.LagWarnSeconds)
	}
	for _, v := range betaG.Violations {
		if strings.Contains(v.Check, "delivery lag") {
			t.Errorf("beta should NOT have lag violation at 15s (warn=100s); got: %+v", v)
		}
	}
}

// --- Zero-value effective validation tests ---

func TestFederationSLOValidationZeroValueDefaultFallback(t *testing.T) {
	// lagWarnSeconds=0 (effective=60), lagFailSeconds=0 (effective=180): valid
	slo := `    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: cloudedge-slo
      spec:
        groupRef: cloudedge
`
	configPath, _ := writeDoctorFederationSLOFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}}, slo)
	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if err := config.Validate(router); err != nil {
		t.Fatalf("zero-value SLO (all defaults) should pass validation: %v", err)
	}
}

func TestFederationSLOValidationZeroWarnExplicitFail(t *testing.T) {
	// lagWarnSeconds=0 (effective=60), lagFailSeconds=50 → effective warn(60) >= fail(50) → error
	slo := `    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: cloudedge-slo
      spec:
        groupRef: cloudedge
        delivery:
          lagFailSeconds: 50
`
	configPath, _ := writeDoctorFederationSLOFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}}, slo)
	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	err = config.Validate(router)
	if err == nil {
		t.Fatal("expected validation error: effective lagWarnSeconds(60) >= lagFailSeconds(50)")
	}
	if !strings.Contains(err.Error(), "effective") {
		t.Fatalf("error should mention 'effective'; got: %v", err)
	}
}

func TestFederationSLOValidationExplicitWarnZeroFail(t *testing.T) {
	// lagWarnSeconds=200, lagFailSeconds=0 (effective=180) → warn(200) >= fail(180) → error
	slo := `    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: cloudedge-slo
      spec:
        groupRef: cloudedge
        delivery:
          lagWarnSeconds: 200
`
	configPath, _ := writeDoctorFederationSLOFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}}, slo)
	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	err = config.Validate(router)
	if err == nil {
		t.Fatal("expected validation error: lagWarnSeconds(200) >= effective lagFailSeconds(180)")
	}
	if !strings.Contains(err.Error(), "effective") {
		t.Fatalf("error should mention 'effective'; got: %v", err)
	}
}

// --- Named zero-value validation tests matching spec ---

func TestFederationSLOValidationWarnExceedsDefaultFail(t *testing.T) {
	// warn=200, fail=0 -> effective warn=200, effective fail=180 -> reject (200 >= 180)
	slo := `    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: bad-slo
      spec:
        groupRef: cloudedge
        delivery:
          lagWarnSeconds: 200
          lagFailSeconds: 0
`
	configPath, _ := writeDoctorFederationSLOFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}}, slo)
	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	err = config.Validate(router)
	if err == nil {
		t.Fatal("expected validation error: effective warn=200 >= effective fail=180")
	}
	if !strings.Contains(err.Error(), "lagWarnSeconds") {
		t.Fatalf("expected lagWarnSeconds in error; got: %v", err)
	}
}

func TestFederationSLOValidationDefaultWarnExceedsFail(t *testing.T) {
	// warn=0, fail=30 -> effective warn=60, effective fail=30 -> reject (60 >= 30)
	slo := `    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: bad-slo
      spec:
        groupRef: cloudedge
        delivery:
          lagWarnSeconds: 0
          lagFailSeconds: 30
`
	configPath, _ := writeDoctorFederationSLOFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}}, slo)
	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	err = config.Validate(router)
	if err == nil {
		t.Fatal("expected validation error: effective warn=60 >= effective fail=30")
	}
	if !strings.Contains(err.Error(), "lagWarnSeconds") {
		t.Fatalf("expected lagWarnSeconds in error; got: %v", err)
	}
}

func TestFederationSLOValidationAllZeroValid(t *testing.T) {
	// warn=0, fail=0 -> effective warn=60, effective fail=180 -> valid (60 < 180)
	slo := `    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: ok-slo
      spec:
        groupRef: cloudedge
        delivery:
          lagWarnSeconds: 0
          lagFailSeconds: 0
`
	configPath, _ := writeDoctorFederationSLOFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}}, slo)
	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if err := config.Validate(router); err != nil {
		t.Fatalf("all-zero SLO should pass validation (effective warn=60 < effective fail=180): %v", err)
	}
}

// --- Multi-group SLO test with FAIL-level violation and remediation ---

// writeDoctorFederationMultiGroupSLOFixture creates a config with multiple
// EventGroups, each with its own EventPeer and FederationSLO resource.
func writeDoctorFederationMultiGroupSLOFixture(t *testing.T, selfNode string, groups []struct {
	group    string
	peer     string
	endpoint string
	sloWarn  int
	sloFail  int
}) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")

	var resourcesYAML string
	for _, g := range groups {
		resourcesYAML += `    - apiVersion: federation.routerd.net/v1alpha1
      kind: EventGroup
      metadata:
        name: ` + g.group + `
      spec:
        nodeName: ` + selfNode + `
    - apiVersion: federation.routerd.net/v1alpha1
      kind: EventPeer
      metadata:
        name: peer-` + g.peer + `-` + g.group + `
      spec:
        groupRef: ` + g.group + `
        nodeName: ` + g.peer + `
        endpoint: ` + g.endpoint + `
    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: slo-` + g.group + `
      spec:
        groupRef: ` + g.group + `
        delivery:
          lagWarnSeconds: ` + fmt.Sprintf("%d", g.sloWarn) + `
          lagFailSeconds: ` + fmt.Sprintf("%d", g.sloFail) + `
`
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
` + resourcesYAML)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func TestDoctorFederationMultiGroupSLO(t *testing.T) {
	// group-a SLO: warn=10, fail=30 -- both peers have lag=50 -> FAIL (50 >= 30)
	// group-b SLO: warn=100, fail=300 -- both peers have lag=50 -> PASS (50 < 100)
	configPath, statePath := writeDoctorFederationMultiGroupSLOFixture(t, "self-node", []struct {
		group    string
		peer     string
		endpoint string
		sloWarn  int
		sloFail  int
	}{
		{group: "group-a", peer: "peer-a", endpoint: "http://10.0.0.1:8787", sloWarn: 10, sloFail: 30},
		{group: "group-b", peer: "peer-b", endpoint: "http://10.0.0.2:8787", sloWarn: 100, sloFail: 300},
	})
	store := openDoctorState(t, statePath)

	now := time.Now().UTC()

	// Group-a: event with lag=50s
	evA := routerstate.EventRecord{
		ID: "evt-a-1", Group: "group-a", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-50 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := store.RecordFederationEvent(evA); err != nil {
		t.Fatalf("record event a: %v", err)
	}
	if err := store.RecordDelivery("evt-a-1", "peer-a"); err != nil {
		t.Fatalf("record delivery a: %v", err)
	}
	if err := store.UpdateDeliveryStatus("evt-a-1", "peer-a", routerstate.DeliveryDelivered, 1, "", now, evA.ExpiresAt); err != nil {
		t.Fatalf("update delivery a: %v", err)
	}

	// Group-b: event with lag=50s
	evB := routerstate.EventRecord{
		ID: "evt-b-1", Group: "group-b", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-50 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := store.RecordFederationEvent(evB); err != nil {
		t.Fatalf("record event b: %v", err)
	}
	if err := store.RecordDelivery("evt-b-1", "peer-b"); err != nil {
		t.Fatalf("record delivery b: %v", err)
	}
	if err := store.UpdateDeliveryStatus("evt-b-1", "peer-b", routerstate.DeliveryDelivered, 1, "", now, evB.ExpiresAt); err != nil {
		t.Fatalf("update delivery b: %v", err)
	}

	closeDoctorState(t, store)

	// Run doctor JSON with remediation plan
	var out bytes.Buffer
	_ = run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json", "--remediation-plan"}, &out, &bytes.Buffer{})

	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if report.Federation == nil || report.Federation.SLO == nil {
		t.Fatal("missing SLO status")
	}

	// Verify per-group SLO status
	groupA := sloGroupByName(report.Federation.SLO, "group-a")
	groupB := sloGroupByName(report.Federation.SLO, "group-b")
	if groupA == nil {
		t.Fatalf("no SLO group for group-a; groups: %+v", report.Federation.SLO.Groups)
	}
	if groupB == nil {
		t.Fatalf("no SLO group for group-b; groups: %+v", report.Federation.SLO.Groups)
	}

	// group-a: lag=50 >= fail=30 -> should have a FAIL violation
	if !groupA.Defined {
		t.Error("group-a: expected defined=true")
	}
	if groupA.Thresholds.Delivery.LagWarnSeconds != 10 {
		t.Errorf("group-a lagWarnSeconds = %d, want 10", groupA.Thresholds.Delivery.LagWarnSeconds)
	}
	if groupA.Thresholds.Delivery.LagFailSeconds != 30 {
		t.Errorf("group-a lagFailSeconds = %d, want 30", groupA.Thresholds.Delivery.LagFailSeconds)
	}
	foundAFail := false
	for _, v := range groupA.Violations {
		if strings.Contains(v.Check, "delivery lag") && v.Severity == "fail" {
			foundAFail = true
		}
	}
	if !foundAFail {
		t.Errorf("group-a: expected delivery lag fail violation (50s >= fail=30s); got: %+v", groupA.Violations)
	}

	// group-b: lag=50 < warn=100 -> no lag violations
	if !groupB.Defined {
		t.Error("group-b: expected defined=true")
	}
	if groupB.Thresholds.Delivery.LagWarnSeconds != 100 {
		t.Errorf("group-b lagWarnSeconds = %d, want 100", groupB.Thresholds.Delivery.LagWarnSeconds)
	}
	if groupB.Thresholds.Delivery.LagFailSeconds != 300 {
		t.Errorf("group-b lagFailSeconds = %d, want 300", groupB.Thresholds.Delivery.LagFailSeconds)
	}
	for _, v := range groupB.Violations {
		if strings.Contains(v.Check, "delivery lag") {
			t.Errorf("group-b: unexpected delivery lag violation (50s < warn=100s): %+v", v)
		}
	}

	// Remediation: should only have lag-related actions for group-a, not group-b
	if report.RemediationPlan == nil {
		t.Fatal("remediation plan is nil")
	}
	for _, a := range report.RemediationPlan.Actions {
		if a.TargetGroup == "group-b" && a.Action == "check-peer-connectivity" {
			t.Errorf("unexpected remediation for group-b (should be healthy): %+v", a)
		}
	}
}

// --- Doctor-level pipeline SLO recovery test ---

func TestDoctorFederationPipelineSLORecovery(t *testing.T) {
	// This test simulates a state transition at the doctor level:
	// Phase 1: failed delivery -> doctor shows violation
	// Phase 2: delivery recovered -> doctor shows no violation
	slo := `    - apiVersion: federation.routerd.net/v1alpha1
      kind: FederationSLO
      metadata:
        name: cloudedge-slo
      spec:
        groupRef: cloudedge
        delivery:
          lagWarnSeconds: 30
          lagFailSeconds: 90
`
	configPath, statePath := writeDoctorFederationSLOFixture(t, "self-node",
		[]struct{ name, endpoint string }{{"peer-a", "http://10.0.0.1:8787"}}, slo)

	now := time.Now().UTC()

	// Phase 1: write a failed delivery with high lag
	store := openDoctorState(t, statePath)
	ev := routerstate.EventRecord{
		ID: "evt-1", Group: "cloudedge", Type: "t", SourceNode: "self-node",
		ObservedAt: now.Add(-120 * time.Second), ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := store.RecordFederationEvent(ev); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.RecordDelivery("evt-1", "peer-a"); err != nil {
		t.Fatalf("record delivery: %v", err)
	}
	if err := store.UpdateDeliveryStatus("evt-1", "peer-a", routerstate.DeliveryFailed, 3, "connection refused", time.Time{}, ev.ExpiresAt); err != nil {
		t.Fatalf("update delivery failed: %v", err)
	}
	closeDoctorState(t, store)

	// Phase 1: run doctor JSON -> verify SLO violation
	var out1 bytes.Buffer
	_ = run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out1, &bytes.Buffer{})
	var report1 doctorReport
	if err := json.Unmarshal(out1.Bytes(), &report1); err != nil {
		t.Fatalf("phase 1 decode: %v\n%s", err, out1.String())
	}
	if report1.Federation == nil || report1.Federation.SLO == nil {
		t.Fatal("phase 1: missing SLO status")
	}
	grp1 := sloGroupByName(report1.Federation.SLO, "cloudedge")
	if grp1 == nil {
		t.Fatalf("phase 1: no SLO group for cloudedge; groups: %+v", report1.Federation.SLO.Groups)
	}
	if len(grp1.Violations) == 0 {
		t.Fatal("phase 1: expected SLO violations for failed delivery")
	}
	foundFailedViolation := false
	for _, v := range grp1.Violations {
		if strings.Contains(v.Check, "failed deliveries") && v.Severity == "fail" {
			foundFailedViolation = true
		}
	}
	if !foundFailedViolation {
		t.Errorf("phase 1: expected failed deliveries violation; got: %+v", grp1.Violations)
	}

	// Phase 2: update delivery to delivered with small lag (5s < warn=30s).
	// Use deliveredAt = observedAt+5s so MaxLagSeconds reflects the short lag.
	store2 := openDoctorState(t, statePath)
	deliveredAt := ev.ObservedAt.Add(5 * time.Second)
	if err := store2.UpdateDeliveryStatus("evt-1", "peer-a", routerstate.DeliveryDelivered, 4, "", deliveredAt, ev.ExpiresAt); err != nil {
		t.Fatalf("update delivery recovered: %v", err)
	}
	closeDoctorState(t, store2)

	// Phase 2: run doctor JSON -> verify no failed-delivery violation, thresholds match config
	var out2 bytes.Buffer
	if err := run([]string{"doctor", "federation", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out2, &bytes.Buffer{}); err != nil {
		t.Fatalf("phase 2 doctor: %v\n%s", err, out2.String())
	}
	var report2 doctorReport
	if err := json.Unmarshal(out2.Bytes(), &report2); err != nil {
		t.Fatalf("phase 2 decode: %v\n%s", err, out2.String())
	}
	if report2.Federation == nil || report2.Federation.SLO == nil {
		t.Fatal("phase 2: missing SLO status")
	}
	grp2 := sloGroupByName(report2.Federation.SLO, "cloudedge")
	if grp2 == nil {
		t.Fatalf("phase 2: no SLO group for cloudedge; groups: %+v", report2.Federation.SLO.Groups)
	}

	// No failed-delivery violations after recovery
	for _, v := range grp2.Violations {
		if strings.Contains(v.Check, "failed deliveries") {
			t.Errorf("phase 2: unexpected failed deliveries violation after recovery: %+v", v)
		}
	}

	// Verify thresholds match the SLO config
	if grp2.Thresholds.Delivery.LagWarnSeconds != 30 {
		t.Errorf("phase 2: lagWarnSeconds = %d, want 30", grp2.Thresholds.Delivery.LagWarnSeconds)
	}
	if grp2.Thresholds.Delivery.LagFailSeconds != 90 {
		t.Errorf("phase 2: lagFailSeconds = %d, want 90", grp2.Thresholds.Delivery.LagFailSeconds)
	}
	if !grp2.Defined {
		t.Error("phase 2: expected slo.defined=true")
	}
}
