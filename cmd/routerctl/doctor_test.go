// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"routerd/pkg/api"
	routerstate "routerd/pkg/state"
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
