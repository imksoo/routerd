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

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func requireLinuxDoctorFixture(t *testing.T) {
	t.Helper()
	if platform.CurrentOS() == platform.OSFreeBSD {
		t.Skip("Linux iproute2/nft diagnostic fixture")
	}
}

func TestDoctorDNSPassNoHost(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver", map[string]any{"phase": "Applied", "health": "HealthOK"}); err != nil {
		t.Fatalf("save dns status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "dns", "--config", configPath, "--state-file", statePath, "--no-host"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor dns: %v", err)
	}
	got := out.String()
	for _, want := range []string{"DOCTOR", "PASS", "DNSResolver/lan-resolver"} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, got)
		}
	}
}

func TestDoctorFirewallLoggerRuntimeHelpers(t *testing.T) {
	active, pid := parseSystemctlActiveStateMainPID("ActiveState=active\nMainPID=1234\n")
	if active != "active" || pid != 1234 {
		t.Fatalf("parsed active=%q pid=%d", active, pid)
	}
	active, pid = parseSystemctlActiveStateMainPID("MainPID=0\nActiveState=inactive\n")
	if active != "inactive" || pid != 0 {
		t.Fatalf("parsed inactive active=%q pid=%d", active, pid)
	}

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallEventLog"},
		Metadata: api.ObjectMeta{Name: "default"},
		Spec:     api.FirewallEventLogSpec{Enabled: true, Path: "/var/lib/routerd/custom-firewall.db"},
	}}}}
	if got := doctorFirewallLogPath(router); got != "/var/lib/routerd/custom-firewall.db" {
		t.Fatalf("firewall log path = %q", got)
	}

	path := filepath.Join(t.TempDir(), "firewall-logs.db")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := doctorFileSize(path); got != 5 {
		t.Fatalf("doctorFileSize = %d, want 5", got)
	}
}

func TestDoctorDHCPv6PDWarnDoesNotFail(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6PrefixDelegation", "wan-pd", map[string]any{"phase": "Pending", "reason": "NoLease"}); err != nil {
		t.Fatalf("save pd status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "dhcpv6-pd", "--config", configPath, "--state-file", statePath, "--no-host"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor dhcpv6-pd returned error for warning: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "WARN") || !strings.Contains(got, "DHCPv6PrefixDelegation/wan-pd") {
		t.Fatalf("doctor output = %s", got)
	}
}

func TestDoctorFailReturnsError(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver", map[string]any{"phase": "Error", "reason": "RenderFailed"}); err != nil {
		t.Fatalf("save dns status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "dns", "--config", configPath, "--state-file", statePath, "--no-host"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor dns succeeded with failing check:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "FAIL") {
		t.Fatalf("doctor output = %s", out.String())
	}
}

func TestDoctorJSONOutputIncludesSummaryAndChecks(t *testing.T) {
	configPath, statePath := writeDoctorFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver", map[string]any{"phase": "Applied", "health": "HealthOK"}); err != nil {
		t.Fatalf("save dns status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "dns", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor dns json: %v", err)
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	if report.Summary.Overall != doctorPass || report.Summary.Pass == 0 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	if len(report.Checks) == 0 || report.Checks[0].Area != "dns" {
		t.Fatalf("checks = %#v", report.Checks)
	}
}

func TestDoctorSAMOwnershipConflictFailsNoHost(t *testing.T) {
	configPath, statePath := writeDoctorSAMFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase":  "Degraded",
		"reason": "remote home owner overlaps local provider inventory",
		"ownershipResolverControlPlaneOwnerTable": []map[string]any{{
			"address":           "10.77.60.7/32",
			"state":             "Conflict",
			"ownerNode":         "oci-router",
			"localEvidenceNode": "azure-router",
			"conflictReason":    "remote-home-owner-overlaps-local-inventory",
		}},
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor sam succeeded with ownership conflict:\n%s", out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge ownership conflicts")
	if check.Status != doctorFail || !strings.Contains(check.Detail, "10.77.60.7/32") {
		t.Fatalf("check = %#v, want failing ownership conflict detail", check)
	}
}

func TestDoctorSAMBGPDeliveryFailsWhenBGPRouterDown(t *testing.T) {
	configPath, statePath := writeDoctorSAMBGPDeliveryFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase": "Ready",
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{
		"phase":            "Down",
		"establishedPeers": 0,
		"fibMissingRoutes": 0,
	}); err != nil {
		t.Fatalf("save bgp status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor sam should fail when BGP-delivery BGPRouter is down:\n%s", out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge BGP delivery BGPRouter/mobility-bgp")
	if check.Status != doctorFail || !strings.Contains(check.Detail, "phase=Down") {
		t.Fatalf("BGP delivery check = %#v", check)
	}
}

func TestDoctorSAMBGPDeliveryFailsWhenFIBMissing(t *testing.T) {
	configPath, statePath := writeDoctorSAMBGPDeliveryFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase": "Ready",
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{
		"phase":                "Established",
		"establishedPeers":     1,
		"fibMissingRoutes":     2,
		"fibUnsupportedRoutes": 0,
	}); err != nil {
		t.Fatalf("save bgp status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor sam should fail when BGP-delivery FIB routes are missing:\n%s", out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge BGP delivery BGPRouter/mobility-bgp")
	if check.Status != doctorFail || !strings.Contains(check.Detail, "fibMissingRoutes=2") {
		t.Fatalf("BGP delivery check = %#v", check)
	}
}

func TestDoctorSAMEnrollmentClientPassesWithFetchedRRSet(t *testing.T) {
	configPath, statePath := writeDoctorSAMEnrollmentClientFixture(t)
	store := openDoctorState(t, statePath)
	now := time.Date(2026, 6, 29, 4, 0, 0, 0, time.UTC)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "leaf-a", map[string]any{
		"phase":                   "Ready",
		"claimRef":                "SAMEnrollmentClaim/leaf-a",
		"observedRRSet":           "SAMRRSet/rrs",
		"observedDirectPeerGroup": "SAMPeerGroup/direct-leaves",
		"lastSuccess":             now.Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("save client status: %v", err)
	}
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:        "SAMRRSet/rrs",
		Generation:    1,
		ObservedAt:    now,
		ExpiresAt:     time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
		Digest:        "test",
		ResourcesJSON: `[]`,
		Status:        "active",
	}); err != nil {
		t.Fatalf("upsert rrset dynamic part: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "sam-enrollment-client", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor sam-enrollment-client: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	if check := findDoctorCheck(t, report, "SAMEnrollmentClient/leaf-a status"); check.Status != doctorPass || !strings.Contains(check.Detail, "observedDirectPeerGroup=SAMPeerGroup/direct-leaves") {
		t.Fatalf("status check = %#v", check)
	}
	if check := findDoctorCheck(t, report, "SAMEnrollmentClient/leaf-a fetched RRSet"); check.Status != doctorPass || !strings.Contains(check.Detail, "active=1") {
		t.Fatalf("rrset check = %#v", check)
	}
}

func TestDoctorSAMEnrollmentClientFailsDegradedStatus(t *testing.T) {
	configPath, statePath := writeDoctorSAMEnrollmentClientFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentClient", "leaf-a", map[string]any{
		"phase":    "Degraded",
		"reason":   "unauthorized",
		"claimRef": "SAMEnrollmentClaim/leaf-a",
	}); err != nil {
		t.Fatalf("save client status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "sam-enrollment-client", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor sam-enrollment-client succeeded with degraded status:\n%s", out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "SAMEnrollmentClient/leaf-a status")
	if check.Status != doctorFail || !strings.Contains(check.Detail, "unauthorized") {
		t.Fatalf("status check = %#v", check)
	}
}

func TestDoctorBGPDynamicPeerWarnsWhenNoPeersDiscovered(t *testing.T) {
	configPath, statePath := writeDoctorBGPDynamicPeerFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPDynamicPeer", "samred-leaves", map[string]any{
		"phase":               "Ready",
		"discoveredPeerCount": 0,
		"acceptedRouteCount":  0,
		"rejectedRouteCount":  0,
	}); err != nil {
		t.Fatalf("save dynamic peer status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "bgp-dynamic-peer", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor bgp-dynamic-peer should warn, not fail: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "BGPDynamicPeer/samred-leaves status")
	if check.Status != doctorWarn || !strings.Contains(check.Remedy, "sourcePrefixes") {
		t.Fatalf("status check = %#v", check)
	}
}

func TestDoctorBGPDynamicPeerFailsWhenAllRoutesRejected(t *testing.T) {
	configPath, statePath := writeDoctorBGPDynamicPeerFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPDynamicPeer", "samred-leaves", map[string]any{
		"phase":               "Ready",
		"discoveredPeerCount": 1,
		"acceptedRouteCount":  0,
		"rejectedRouteCount":  2,
		"rejectedRouteSummary": map[string]any{
			"not-claim-owned": 2,
		},
	}); err != nil {
		t.Fatalf("save dynamic peer status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "bgp-dynamic-peer", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor bgp-dynamic-peer succeeded with all routes rejected:\n%s", out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "BGPDynamicPeer/samred-leaves route admission")
	if check.Status != doctorFail || !strings.Contains(check.Detail, "not-claim-owned=2") {
		t.Fatalf("admission check = %#v", check)
	}
}

func TestDoctorSAMBGPDeliveryWarnsWhenDegradedButFIBInstalled(t *testing.T) {
	configPath, statePath := writeDoctorSAMBGPDeliveryFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase": "Ready",
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{
		"phase":                "Degraded",
		"establishedPeers":     8,
		"fibMissingRoutes":     0,
		"fibUnsupportedRoutes": 0,
	}); err != nil {
		t.Fatalf("save bgp status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor sam should warn, not fail, when BGP delivery FIB is installed: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge BGP delivery BGPRouter/mobility-bgp")
	if check.Status != doctorWarn || !strings.Contains(check.Detail, "phase=Degraded") {
		t.Fatalf("BGP delivery check = %#v, want warn for degraded peer establishment with installed FIB", check)
	}
}

func TestDoctorSAMBGPDeliveryPassesWhenBGPRouterEstablished(t *testing.T) {
	configPath, statePath := writeDoctorSAMBGPDeliveryFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase": "Ready",
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{
		"phase":                "Established",
		"establishedPeers":     1,
		"fibMissingRoutes":     0,
		"fibUnsupportedRoutes": 0,
	}); err != nil {
		t.Fatalf("save bgp status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor sam should pass when BGP delivery is established: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge BGP delivery BGPRouter/mobility-bgp")
	if check.Status != doctorPass || !strings.Contains(check.Detail, "establishedPeers=1") {
		t.Fatalf("BGP delivery check = %#v", check)
	}
}

func TestDoctorSAMStaleCaptureEvidenceWarns(t *testing.T) {
	configPath, statePath := writeDoctorSAMFederationFixture(t)
	store := openDoctorState(t, statePath)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"phase":          "Ready",
		"bgpRIBObserved": true,
		"ownershipResolverControlPlaneOwnerTable": []map[string]any{{
			"address":           "10.77.60.11/32",
			"state":             "Stale",
			"class":             "StaleCapture",
			"suppressionReason": "capture-not-desired",
		}},
	}); err != nil {
		t.Fatalf("save mobility status: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "sam", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor sam: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "MobilityPool/cloudedge stale capture evidence")
	if check.Status != doctorWarn || !strings.Contains(check.Detail, "10.77.60.11/32") || !strings.Contains(check.Remedy, "dataplane") {
		t.Fatalf("stale capture evidence check = %#v, want diagnostic warning with dataplane remedy", check)
	}
}

func TestDoctorDynamicHealthyMaskPolicyPasses(t *testing.T) {
	configPath, statePath := writeDoctorDynamicFixture(t, true)
	now := time.Now().UTC()
	store := openDoctorState(t, statePath)
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:         "Plugin/cloud",
		Generation:     1,
		ObservedAt:     now,
		ExpiresAt:      now.Add(time.Hour),
		Digest:         "sha256:dynamic",
		ResourcesJSON:  `[{"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv4Route","metadata":{"name":"cloud-route"},"spec":{"destination":"10.20.0.0/24","gateway":"192.0.2.1"}}]`,
		DirectivesJSON: `[{"op":"mask","target":{"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv4Route","name":"static-cloud-route"},"reason":"cloud route observed"}]`,
		Status:         "active",
	}); err != nil {
		t.Fatalf("upsert dynamic part: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	if err := run([]string{"doctor", "dynamic", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor dynamic: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	for _, name := range []string{"dynamic parts decode", "expired parts ignored", "effective config builds", "override policies present for masks"} {
		check := findDoctorCheck(t, report, name)
		if check.Status != doctorPass {
			t.Fatalf("%s check = %#v", name, check)
		}
	}
	if check := findDoctorCheck(t, report, "effective config builds"); !strings.Contains(check.Detail, "1 suppressed, 1 dynamic resources added") {
		t.Fatalf("effective detail = %q", check.Detail)
	}
}

func TestDoctorDynamicMaskWithoutPolicyFailsEffectiveBuild(t *testing.T) {
	configPath, statePath := writeDoctorDynamicFixture(t, false)
	now := time.Now().UTC()
	store := openDoctorState(t, statePath)
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:         "Plugin/cloud",
		Generation:     1,
		ObservedAt:     now,
		ExpiresAt:      now.Add(time.Hour),
		Digest:         "sha256:dynamic",
		ResourcesJSON:  `[]`,
		DirectivesJSON: `[{"op":"mask","target":{"apiVersion":"net.routerd.net/v1alpha1","kind":"IPv4Route","name":"static-cloud-route"},"reason":"cloud route observed"}]`,
		Status:         "active",
	}); err != nil {
		t.Fatalf("upsert dynamic part: %v", err)
	}
	closeDoctorState(t, store)

	var out bytes.Buffer
	err := run([]string{"doctor", "dynamic", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor dynamic succeeded with disallowed mask:\n%s", out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	check := findDoctorCheck(t, report, "effective config builds")
	if check.Status != doctorFail || !strings.Contains(check.Detail, "dynamic mask not allowed") {
		t.Fatalf("effective check = %#v", check)
	}
}

func TestDoctorHybridHealthyNoHost(t *testing.T) {
	configPath, statePath := writeDoctorHybridFixture(t, false)
	var out bytes.Buffer
	if err := run([]string{"doctor", "hybrid", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor hybrid: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	if report.Summary.Fail != 0 || report.Summary.Pass == 0 {
		t.Fatalf("summary = %#v checks=%#v", report.Summary, report.Checks)
	}
	if check := findDoctorCheck(t, report, "HybridRoute/cloud-private peerRef"); check.Status != doctorPass {
		t.Fatalf("peerRef check = %#v", check)
	}
	if check := findDoctorCheck(t, report, "HybridRoute/cloud-private default route untouched"); check.Status != doctorPass {
		t.Fatalf("default route check = %#v", check)
	}
}

func TestDoctorHybridFailsUnresolvedPeerRef(t *testing.T) {
	configPath, statePath := writeDoctorHybridFixture(t, true)
	var out bytes.Buffer
	err := run([]string{"doctor", "hybrid", "--config", configPath, "--state-file", statePath, "--no-host", "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor hybrid succeeded with unresolved peer:\n%s", out.String())
	}
	var report doctorReport
	if unmarshalErr := json.Unmarshal(out.Bytes(), &report); unmarshalErr != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", unmarshalErr, out.String())
	}
	check := findDoctorCheck(t, report, "HybridRoute/cloud-private peerRef")
	if check.Status != doctorFail || !strings.Contains(check.Detail, "missing OverlayPeer") {
		t.Fatalf("peerRef check = %#v", check)
	}
}

func writeDoctorFixture(t *testing.T) (string, string) {
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
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSResolver
      metadata:
        name: lan-resolver
      spec:
        listen:
          - addresses: ["127.0.0.1"]
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv6PrefixDelegation
      metadata:
        name: wan-pd
      spec:
        interface: wan
        prefixLength: 60
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func writeDoctorSAMFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: federation.routerd.net/v1alpha1
      kind: EventGroup
      metadata:
        name: cloudedge
      spec:
        nodeName: onprem-router
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMNodeSet
      metadata:
        name: cloudedge-nodes
      spec:
        nodes:
          - nodeRef: azure-router
            site: azure
            role: cloud
          - nodeRef: oci-router
            site: oci
            role: cloud
          - nodeRef: onprem-router
            site: onprem
            role: onprem
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: MobilityPool
      metadata:
        name: cloudedge
      spec:
        prefix: 10.77.60.0/24
        groupRef: cloudedge
        membersFrom:
          - resource: SAMNodeSet/cloudedge-nodes
        members:
          - nodeRef: onprem-router
            capture:
              type: proxy-arp
              interface: lan0
              activeWhen:
                type: single-router
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func writeDoctorSAMEnrollmentClientFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: leaf-a
spec:
  resources:
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMEnrollmentClaim
      metadata:
        name: leaf-a
      spec:
        policyRef: SAMEnrollmentPolicy/samred-leaves
        rrSetRef: SAMRRSet/rrs
        leafID: leaf-a
        tunnelAddress: 10.255.10.21/32
        bgp:
          asn: 64577
          routerID: 10.255.10.21
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMEnrollmentClient
      metadata:
        name: leaf-a
      spec:
        claimRef: SAMEnrollmentClaim/leaf-a
        bootstrapEndpoints:
          - http://10.30.0.10:65432
        controlAPITokenFrom:
          env: ROUTERD_TEST_CONTROL_TOKEN
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func writeDoctorBGPDynamicPeerFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: rr-1
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPRouter
      metadata:
        name: rr
      spec:
        asn: 64577
        routerID: 10.255.10.1
        listen:
          address: 10.255.10.1
    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPDynamicPeer
      metadata:
        name: samred-leaves
      spec:
        routerRef: BGPRouter/rr
        peerASN: 64577
        listen:
          sourcePrefixes:
            - 10.255.10.0/24
        importPolicy:
          allowedPrefixes:
            - 10.99.0.0/16
          allowedPrefixLengthMin: 32
          allowedPrefixLengthMax: 32
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func writeDoctorSAMBGPDeliveryFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: federation.routerd.net/v1alpha1
      kind: EventGroup
      metadata:
        name: cloudedge
      spec:
        nodeName: node-a
    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPRouter
      metadata:
        name: mobility-bgp
      spec:
        asn: 64577
        routerID: 10.99.0.1
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMTransportProfile
      metadata:
        name: cloudedge-transport
      spec:
        selfNodeRef: node-a
        mode: ipip
        innerPrefix: 10.255.0.0/20
        underlayInterface: wg-hybrid
        localEndpoint: 10.99.0.1
        bgp:
          routerRef: BGPRouter/mobility-bgp
          peerASN: 64577
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMNodeSet
      metadata:
        name: cloudedge-nodes
      spec:
        nodes:
          - nodeRef: node-a
            site: aws
            role: cloud
          - nodeRef: node-b
            site: azure
            role: cloud
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: MobilityPool
      metadata:
        name: cloudedge
      spec:
        prefix: 10.77.60.0/24
        groupRef: cloudedge
        membersFrom:
          - resource: SAMNodeSet/cloudedge-nodes
        members:
          - nodeRef: node-a
            capture:
              type: provider-secondary-ip
              providerRef: aws-test
              nicRef: eni-test
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func writeDoctorSAMFederationFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	data := fmt.Sprintf(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: federation.routerd.net/v1alpha1
      kind: EventGroup
      metadata: { name: cloudedge }
      spec:
        nodeName: azure-router
        retention:
          maxEvents: 1000
          maxAge: 24h
        listen:
          address: 10.99.0.3
          port: 9443
        replayWindow: 5m
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMNodeSet
      metadata:
        name: cloudedge-nodes
      spec:
        nodes:
          - nodeRef: azure-router
            site: azure
            role: cloud
          - nodeRef: aws-router-a
            site: aws
            role: cloud
          - nodeRef: oci-router
            site: oci
            role: cloud
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: MobilityPool
      metadata:
        name: cloudedge
      spec:
        prefix: 10.77.60.0/24
        groupRef: cloudedge
        membersFrom:
          - resource: SAMNodeSet/cloudedge-nodes
        members:
          - nodeRef: azure-router
            capture:
              type: provider-secondary-ip
              providerRef: azure-test
              nicRef: nic-test
`)
	if err := os.WriteFile(configPath, []byte(data), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func recordFederationEvent(t *testing.T, store *routerstate.SQLiteStore, rec routerstate.EventRecord) {
	t.Helper()
	if err := store.RecordFederationEvent(rec); err != nil {
		t.Fatalf("record federation event: %v", err)
	}
}

func writeDoctorHybridFixture(t *testing.T, missingPeer bool) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	peerRef := "cloud-main"
	if missingPeer {
		peerRef = "missing"
	}
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata:
        name: wg-hybrid
      spec:
        listenPort: 51820
        mtu: 1420
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: OverlayPeer
      metadata:
        name: cloud-main
      spec:
        role: cloud
        nodeID: cloud-main
        underlay:
          type: wireguard
          interface: wg-hybrid
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: HybridRoute
      metadata:
        name: cloud-private
      spec:
        destinationCIDRs: [10.20.0.0/16]
        peerRef: ` + peerRef + `
        install:
          table: main
          metric: 120
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func writeDoctorDynamicFixture(t *testing.T, includePolicy bool) (string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	policy := ""
	if includePolicy {
		policy = `
    - apiVersion: config.routerd.net/v1alpha1
      kind: DynamicOverridePolicy
      metadata:
        name: cloud-masks
      spec:
        allow:
          - source: Plugin/cloud
            operations: [mask]
            targets:
              - apiVersion: net.routerd.net/v1alpha1
                kind: IPv4Route
                name: static-cloud-route
`
	}
	data := []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4Route
      metadata:
        name: static-cloud-route
      spec:
        destination: 10.10.0.0/24
        gateway: 192.0.2.1
` + policy)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func findDoctorCheck(t *testing.T, report doctorReport, name string) doctorCheck {
	t.Helper()
	for _, check := range report.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("missing check %q in %#v", name, report.Checks)
	return doctorCheck{}
}

func assertDoctorCheckAbsent(t *testing.T, report doctorReport, name string) {
	t.Helper()
	for _, check := range report.Checks {
		if check.Name == name {
			t.Fatalf("unexpected check %q in %#v", name, report.Checks)
		}
	}
}

func openDoctorState(t *testing.T, path string) *routerstate.SQLiteStore {
	t.Helper()
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite state: %v", err)
	}
	return store
}

func closeDoctorState(t *testing.T, store *routerstate.SQLiteStore) {
	t.Helper()
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite state: %v", err)
	}
}

func TestDoctorNftCheckStatusWarnsWhenStdoutHasTableButExitNonZero(t *testing.T) {
	command := diagnoseCommandCheck{
		Name:     "nft list table ip routerd_nat",
		OK:       false,
		Command:  "nft list table ip routerd_nat",
		Stdout:   "table ip routerd_nat {\n\tchain postrouting {\n\t}\n}",
		Stderr:   "warning: ignoring unknown attribute",
		ExitCode: 1,
		Output:   "table ip routerd_nat {...}\n--- stderr ---\nwarning: ignoring unknown attribute",
	}
	check := doctorNftCheckStatus("nat", command, "ip", "routerd_nat", doctorFail, "remedy here", "NAT44Rule active=1 pending=0")
	if check.Status != doctorWarn {
		t.Fatalf("expected warn, got %#v", check)
	}
	if !strings.Contains(check.Detail, "table=ip/routerd_nat") {
		t.Fatalf("detail missing table label: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "exit=1") {
		t.Fatalf("detail missing exit code: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "stderr=warning: ignoring unknown attribute") {
		t.Fatalf("detail missing stderr excerpt: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "NAT44Rule active=1 pending=0") {
		t.Fatalf("detail missing resource count: %q", check.Detail)
	}
}

func TestDoctorNftCheckStatusFailsWhenStdoutEmpty(t *testing.T) {
	command := diagnoseCommandCheck{
		Name:     "nft list table ip routerd_nat",
		OK:       false,
		Command:  "nft list table ip routerd_nat",
		Stdout:   "",
		Stderr:   "Error: No such file or directory; did you mean table 'routerd' in family ip?",
		ExitCode: 1,
		Output:   "Error: No such file or directory",
	}
	check := doctorNftCheckStatus("nat", command, "ip", "routerd_nat", doctorFail, "apply NAT44Rule resources", "NAT44Rule active=0 pending=2")
	if check.Status != doctorFail {
		t.Fatalf("expected fail, got %#v", check)
	}
	if !strings.Contains(check.Detail, "table=ip/routerd_nat") {
		t.Fatalf("detail missing table label: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "cmd=nft list table ip routerd_nat") {
		t.Fatalf("detail missing cmd: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "exit=1") {
		t.Fatalf("detail missing exit code: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "stderr=Error: No such file or directory") {
		t.Fatalf("detail missing stderr excerpt: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "NAT44Rule active=0 pending=2") {
		t.Fatalf("detail missing resource counts: %q", check.Detail)
	}
	if check.Remedy != "apply NAT44Rule resources" {
		t.Fatalf("remedy = %q", check.Remedy)
	}
}

func TestDoctorNftCheckStatusPassesWithStdout(t *testing.T) {
	command := diagnoseCommandCheck{
		Name:     "nft list table ip routerd_nat",
		OK:       true,
		Command:  "nft list table ip routerd_nat",
		Stdout:   "table ip routerd_nat {\n}",
		ExitCode: 0,
		Output:   "table ip routerd_nat {\n}",
	}
	check := doctorNftCheckStatus("nat", command, "ip", "routerd_nat", doctorFail, "remedy", "NAT44Rule active=2 pending=0")
	if check.Status != doctorPass {
		t.Fatalf("expected pass, got %#v", check)
	}
	if !strings.Contains(check.Detail, "table=ip/routerd_nat") {
		t.Fatalf("detail missing table label: %q", check.Detail)
	}
	if !strings.Contains(check.Detail, "NAT44Rule active=2 pending=0") {
		t.Fatalf("detail missing resource counts: %q", check.Detail)
	}
}
