// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

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

func TestDoctorDSLiteSelectedHealthyPolicyIgnoresAFTRProbeFailure(t *testing.T) {
	configPath, statePath := writeDoctorDSLiteFixture(t)
	saveDoctorDSLiteState(t, statePath, map[string]any{"phase": "Applied", "health": "HealthOK"}, "ds-lite", map[string]any{"phase": "Applied", "selectedCandidate": "ds-lite", "selectedDevice": "ds-routerd"}, map[string]any{"phase": "Healthy"})
	installDoctorDSLiteHostCommands(t, false, true)

	report := runDoctorDSLiteJSON(t, configPath, statePath)
	if report.Summary.Warn != 0 || report.Summary.Fail != 0 {
		t.Fatalf("summary = %#v checks = %#v", report.Summary, report.Checks)
	}
	check := findDoctorCheck(t, report, "dig AFTR aftr.example.net")
	if check.Status != doctorPass || !strings.Contains(check.Detail, "selected via EgressRoutePolicy/ipv4-default") {
		t.Fatalf("AFTR check = %#v", check)
	}
}

func TestDoctorDSLiteUpSelectedSourceAggregateCandidatePasses(t *testing.T) {
	configPath, statePath := writeDoctorDSLiteFixture(t)
	saveDoctorDSLiteState(t, statePath, map[string]any{"phase": "Up"}, "dslite-pd-balanced", map[string]any{"phase": "Applied", "selectedSource": "DSLiteTunnel/ds-lite", "selectedTargets": 3}, map[string]any{"phase": "Healthy"})
	installDoctorDSLiteHostCommands(t, false, true)

	report := runDoctorDSLiteJSON(t, configPath, statePath)
	if report.Summary.Overall != doctorPass || report.Summary.Warn != 0 || report.Summary.Fail != 0 {
		t.Fatalf("summary = %#v checks = %#v", report.Summary, report.Checks)
	}
	resourceCheck := findDoctorCheck(t, report, "DSLiteTunnel/ds-lite")
	if resourceCheck.Status != doctorPass {
		t.Fatalf("DSLiteTunnel check = %#v", resourceCheck)
	}
	check := findDoctorCheck(t, report, "dig AFTR aftr.example.net")
	if check.Status != doctorPass || !strings.Contains(check.Detail, "selected via EgressRoutePolicy/ipv4-default, gatewayHealth-aligned") {
		t.Fatalf("AFTR check = %#v", check)
	}
}

func TestDoctorDSLiteSelectedHealthyPolicyWithAFTRProbeSuccessPasses(t *testing.T) {
	configPath, statePath := writeDoctorDSLiteFixture(t)
	saveDoctorDSLiteState(t, statePath, map[string]any{"phase": "Applied", "health": "HealthOK"}, "ds-lite", map[string]any{"phase": "Applied", "selectedCandidate": "ds-lite", "selectedDevice": "ds-routerd"}, map[string]any{"phase": "Healthy"})
	installDoctorDSLiteHostCommands(t, true, true)

	report := runDoctorDSLiteJSON(t, configPath, statePath)
	if report.Summary.Warn != 0 || report.Summary.Fail != 0 {
		t.Fatalf("summary = %#v checks = %#v", report.Summary, report.Checks)
	}
	check := findDoctorCheck(t, report, "dig AFTR aftr.example.net")
	if check.Status != doctorPass {
		t.Fatalf("AFTR check = %#v", check)
	}
}

func TestDoctorDSLiteNotSelectedWarnsOnAFTRProbeFailure(t *testing.T) {
	configPath, statePath := writeDoctorDSLiteFixture(t)
	saveDoctorDSLiteState(t, statePath, map[string]any{"phase": "Applied", "health": "HealthOK"}, "pppoe", map[string]any{"phase": "Applied", "selectedCandidate": "pppoe", "selectedDevice": "ppp0"}, map[string]any{"phase": "Healthy"})
	installDoctorDSLiteHostCommands(t, false, true)

	report := runDoctorDSLiteJSON(t, configPath, statePath)
	check := findDoctorCheck(t, report, "dig AFTR aftr.example.net")
	if check.Status != doctorWarn || strings.Contains(check.Detail, "selected via") {
		t.Fatalf("AFTR check = %#v", check)
	}
}

func TestDoctorDSLiteUpSelectedSourceMismatchWarnsOnAFTRProbeFailure(t *testing.T) {
	configPath, statePath := writeDoctorDSLiteFixture(t)
	saveDoctorDSLiteState(t, statePath, map[string]any{"phase": "Up"}, "dslite-pd-balanced", map[string]any{"phase": "Applied", "selectedSource": "DSLiteTunnel/other", "selectedDevice": "other-device"}, map[string]any{"phase": "Healthy"})
	installDoctorDSLiteHostCommands(t, false, true)

	report := runDoctorDSLiteJSON(t, configPath, statePath)
	check := findDoctorCheck(t, report, "dig AFTR aftr.example.net")
	if check.Status != doctorWarn || strings.Contains(check.Detail, "selected via") {
		t.Fatalf("AFTR check = %#v", check)
	}
}

func TestDoctorDSLiteUpSelectedSourceUnhealthyPolicyWarnsOnAFTRProbeFailure(t *testing.T) {
	configPath, statePath := writeDoctorDSLiteFixture(t)
	saveDoctorDSLiteState(t, statePath, map[string]any{"phase": "Up"}, "dslite-pd-balanced", map[string]any{"phase": "Pending", "selectedSource": "DSLiteTunnel/ds-lite"}, map[string]any{"phase": "Healthy"})
	installDoctorDSLiteHostCommands(t, false, true)

	report := runDoctorDSLiteJSON(t, configPath, statePath)
	check := findDoctorCheck(t, report, "dig AFTR aftr.example.net")
	if check.Status != doctorWarn || strings.Contains(check.Detail, "selected via") {
		t.Fatalf("AFTR check = %#v", check)
	}
}

func TestDoctorDSLiteDownStatusIsNotOverriddenBySelectedPolicy(t *testing.T) {
	configPath, statePath := writeDoctorDSLiteFixture(t)
	saveDoctorDSLiteState(t, statePath, map[string]any{"phase": "Error", "reason": "TunnelApplyFailed"}, "ds-lite", map[string]any{"phase": "Applied", "selectedCandidate": "ds-lite", "selectedDevice": "ds-routerd"}, map[string]any{"phase": "Healthy"})
	installDoctorDSLiteHostCommands(t, false, true)

	var out bytes.Buffer
	err := run([]string{"doctor", "dslite", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("doctor dslite succeeded with down tunnel:\n%s", out.String())
	}
	var report doctorReport
	if unmarshalErr := json.Unmarshal(out.Bytes(), &report); unmarshalErr != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", unmarshalErr, out.String())
	}
	check := findDoctorCheck(t, report, "DSLiteTunnel/ds-lite")
	if check.Status != doctorFail {
		t.Fatalf("DSLiteTunnel check = %#v", check)
	}
	aftrCheck := findDoctorCheck(t, report, "dig AFTR aftr.example.net")
	if aftrCheck.Status != doctorWarn || strings.Contains(aftrCheck.Detail, "selected via") {
		t.Fatalf("AFTR check = %#v", aftrCheck)
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

func writeDoctorDSLiteFixture(t *testing.T) (string, string) {
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
      kind: DSLiteTunnel
      metadata:
        name: ds-lite
      spec:
        interface: wan
        tunnelName: ds-routerd
        aftrFQDN: aftr.example.net
    - apiVersion: net.routerd.net/v1alpha1
      kind: EgressRoutePolicy
      metadata:
        name: ipv4-default
      spec:
        family: ipv4
        selection: highest-weight-ready
        candidates:
          - name: ds-lite
            source: DSLiteTunnel/ds-lite
            device: ds-routerd
            gatewaySource: none
            weight: 80
            healthCheck: internet
          - name: pppoe
            interface: ppp0
            gatewaySource: none
            weight: 20
            healthCheck: internet
    - apiVersion: net.routerd.net/v1alpha1
      kind: HealthCheck
      metadata:
        name: internet
      spec:
        target: 1.1.1.1
        protocol: icmp
`)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath, filepath.Join(dir, "routerd.db")
}

func saveDoctorDSLiteState(t *testing.T, statePath string, tunnelStatus map[string]any, selectedCandidate string, policyStatus map[string]any, healthStatus map[string]any) {
	t.Helper()
	store := openDoctorState(t, statePath)
	tunnelStatus["device"] = "ds-routerd"
	tunnelStatus["aftrFQDN"] = "aftr.example.net"
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", "ds-lite", tunnelStatus); err != nil {
		t.Fatalf("save dslite status: %v", err)
	}
	if selectedCandidate != "" {
		policyStatus["selectedCandidate"] = selectedCandidate
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default", policyStatus); err != nil {
		t.Fatalf("save egress status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "HealthCheck", "internet", healthStatus); err != nil {
		t.Fatalf("save health status: %v", err)
	}
	closeDoctorState(t, store)
}

func installDoctorDSLiteHostCommands(t *testing.T, digOK, ipOK bool) {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	digScript := "#!/bin/sh\necho 'dig failed' >&2\nexit 1\n"
	if digOK {
		digScript = "#!/bin/sh\necho '2001:db8::1'\n"
	}
	ipScript := "#!/bin/sh\necho 'ip link failed' >&2\nexit 1\n"
	if ipOK {
		ipScript = "#!/bin/sh\necho '7: ds-routerd: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1460'\n"
	}
	writeTestCommand(t, filepath.Join(binDir, "dig"), digScript)
	writeTestCommand(t, filepath.Join(binDir, "ip"), ipScript)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func runDoctorDSLiteJSON(t *testing.T, configPath, statePath string) doctorReport {
	t.Helper()
	var out bytes.Buffer
	if err := run([]string{"doctor", "dslite", "--config", configPath, "--state-file", statePath, "-o", "json"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor dslite: %v\n%s", err, out.String())
	}
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal doctor report: %v\n%s", err, out.String())
	}
	return report
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
