package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"routerd/pkg/resource"
	routerstate "routerd/pkg/state"
)

func TestShowIPv6PDTableIncludesSpecStateLedger(t *testing.T) {
	dir := t.TempDir()
	configPath := writeShowConfig(t, dir)
	statePath := filepath.Join(dir, "state.json")
	ledgerPath := filepath.Join(dir, "artifacts.json")
	store := routerstate.New()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{
		CurrentPrefix:  "2001:db8:1200:1220::/60",
		LastPrefix:     "2001:db8:1200:1220::/60",
		LastObservedAt: "2026-04-28T01:02:03Z",
		DUIDText:       "00:03:00:01:02:00:5e:10:20:30",
		IAID:           "0",
	}), "test")
	if err := store.Save(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}
	ledger := resource.NewLedger()
	ledger.Remember([]resource.Artifact{{
		Kind:  "dhcp.ipv6.prefixDelegation",
		Name:  "ens18",
		Owner: "net.routerd.net/v1alpha1/DHCPv6PrefixDelegation/wan-pd",
	}})
	if err := ledger.Save(ledgerPath); err != nil {
		t.Fatalf("save ledger: %v", err)
	}

	var out bytes.Buffer
	err := run([]string{"show", "dhcpv6pd", "--config", configPath, "--state-file", statePath, "--ledger-file", ledgerPath}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("show ipv6pd: %v", err)
	}
	got := out.String()
	for _, want := range []string{"KIND", "DHCPv6PrefixDelegation", "wan-pd", "1 artifacts", "current=2001:db8:1200:1220::/60"} {
		if !strings.Contains(got, want) {
			t.Fatalf("show output missing %q:\n%s", want, got)
		}
	}
}

func TestShowKindNameYAML(t *testing.T) {
	dir := t.TempDir()
	configPath := writeShowConfig(t, dir)
	statePath := filepath.Join(dir, "state.json")
	ledgerPath := filepath.Join(dir, "artifacts.json")
	if err := routerstate.New().Save(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}
	if err := (resource.NewLedger()).Save(ledgerPath); err != nil {
		t.Fatalf("save ledger: %v", err)
	}

	var out bytes.Buffer
	err := run([]string{"show", "if/wan", "-o", "yaml", "--config", configPath, "--state-file", statePath, "--ledger-file", ledgerPath}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("show if/wan yaml: %v", err)
	}
	got := out.String()
	for _, want := range []string{"kind: Interface", "name: wan", "ifname: ens18"} {
		if !strings.Contains(got, want) {
			t.Fatalf("yaml output missing %q:\n%s", want, got)
		}
	}
}

func TestShowDiffAndLedgerModes(t *testing.T) {
	dir := t.TempDir()
	configPath := writeShowConfig(t, dir)
	statePath := filepath.Join(dir, "state.json")
	ledgerPath := filepath.Join(dir, "artifacts.json")
	if err := routerstate.New().Save(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}
	ledger := resource.NewLedger()
	ledger.Remember([]resource.Artifact{{
		Kind:  "net.link",
		Name:  "ens18",
		Owner: "net.routerd.net/v1alpha1/Interface/wan",
	}})
	if err := ledger.Save(ledgerPath); err != nil {
		t.Fatalf("save ledger: %v", err)
	}

	var diffOut bytes.Buffer
	if err := run([]string{"show", "interface/wan", "--diff", "--config", configPath, "--state-file", statePath, "--ledger-file", ledgerPath}, &diffOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("show diff: %v", err)
	}
	if got := diffOut.String(); !strings.Contains(got, "DIFF") || !strings.Contains(got, "fields") {
		t.Fatalf("diff output = %s", got)
	}

	var ledgerOut bytes.Buffer
	if err := run([]string{"show", "interface/wan", "--ledger", "--config", configPath, "--state-file", statePath, "--ledger-file", ledgerPath}, &ledgerOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("show ledger: %v", err)
	}
	if got := ledgerOut.String(); !strings.Contains(got, "1 artifacts") {
		t.Fatalf("ledger output = %s", got)
	}
}

func TestDescribeOrphans(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	if err := os.WriteFile(configPath, []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources: []
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	statePath := filepath.Join(dir, "state.json")
	ledgerPath := filepath.Join(dir, "artifacts.json")
	if err := routerstate.New().Save(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}
	ledger := resource.NewLedger()
	ledger.Remember([]resource.Artifact{{
		Kind:  "systemd.service",
		Name:  "routerd-stale.service",
		Owner: "net.routerd.net/v1alpha1/DSLiteTunnel/stale",
	}})
	if err := ledger.Save(ledgerPath); err != nil {
		t.Fatalf("save ledger: %v", err)
	}
	var out bytes.Buffer
	if err := run([]string{"describe", "orphans", "--config", configPath, "--state-file", statePath, "--ledger-file", ledgerPath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("describe orphans: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "routerd-stale.service") || !strings.Contains(got, "disable and stop systemd service") {
		t.Fatalf("orphan output = %s", got)
	}
}

func TestShowPDLegacySubcommandRemoved(t *testing.T) {
	configPath := writeShowConfig(t, t.TempDir())
	dir := t.TempDir()
	var out bytes.Buffer
	err := run([]string{"show", "pd", "--config", configPath, "--state-file", filepath.Join(dir, "state.json"), "--ledger-file", filepath.Join(dir, "artifacts.json")}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("show pd alias: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "DHCPv6PrefixDelegation") {
		t.Fatalf("show pd output = %s", got)
	}
}

func TestGetKindAndListKinds(t *testing.T) {
	configPath := writeShowConfig(t, t.TempDir())
	var out bytes.Buffer
	if err := run([]string{"get", "pd", "--config", configPath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("get pd: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "DHCPv6PrefixDelegation") || !strings.Contains(got, "wan-pd") || strings.Contains(got, "STATE") {
		t.Fatalf("get output = %s", got)
	}

	out.Reset()
	if err := run([]string{"get", "--list-kinds", "--config", configPath}, &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("get --list-kinds: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "Interface") || !strings.Contains(got, "IPv4SourceNAT") {
		t.Fatalf("list kinds output = %s", got)
	}
}

func TestDescribeIPv6PDIncludesStatusLedgerEvents(t *testing.T) {
	dir := t.TempDir()
	configPath := writeShowConfig(t, dir)
	dbPath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite state: %v", err)
	}
	generation, err := store.BeginGeneration("test")
	if err != nil {
		t.Fatalf("begin generation: %v", err)
	}
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{
		CurrentPrefix:  "2001:db8:1200:1220::/60",
		LastPrefix:     "2001:db8:1200:1220::/60",
		T1:             "7200",
		T2:             "12600",
		PLTime:         "14400",
		VLTime:         "14400",
		LastObservedAt: "2026-04-28T01:02:03Z",
		LastReplyAt:    "2026-04-28T01:02:04Z",
		LastRequestAt:  "2026-04-28T01:02:02Z",
		LastRenewAt:    "2026-04-28T03:02:04Z",
		DUIDText:       "00:03:00:01:02:00:00:00:00:02",
		IAID:           "1",
	}), "test")
	if err := store.RecordEvent("net.routerd.net/v1alpha1", "DHCPv6PrefixDelegation", "wan-pd", "Normal", "PrefixObserved", "observed delegated prefix"); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.FinishGeneration(generation, "Healthy", nil); err != nil {
		t.Fatalf("finish generation: %v", err)
	}
	ledger, err := resource.OpenSQLiteLedger(dbPath)
	if err != nil {
		t.Fatalf("open sqlite ledger: %v", err)
	}
	ledger.Remember([]resource.Artifact{{Kind: "dhcp.ipv6.prefixDelegation", Name: "ens18", Owner: "net.routerd.net/v1alpha1/DHCPv6PrefixDelegation/wan-pd"}})

	var out bytes.Buffer
	err = run([]string{"describe", "pd/wan-pd", "--config", configPath, "--state-file", dbPath, "--ledger-file", dbPath}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("describe pd: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"Currently observable:",
		"Current delegated prefix:",
		"Last delegated prefix:",
		"Client DUID:",
		"IAID:",
		"Last Reply at:",
		"Last Request at:",
		"Last Renew at:",
		"T1:",
		"7200s",
		"Next T1 at:",
		"2026-04-28T03:02:04Z",
		"Valid lifetime expires at:",
		"2026-04-28T05:02:04Z",
		"Last Apply Generation:",
		"PrefixObserved",
		"dhcp.ipv6.prefixDelegation/ens18",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("describe output missing %q:\n%s", want, got)
		}
	}
}

func TestDescribeInventoryHost(t *testing.T) {
	dir := t.TempDir()
	configPath := writeShowConfig(t, dir)
	dbPath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite state: %v", err)
	}
	if _, err := store.BeginGeneration("test"); err != nil {
		t.Fatalf("begin generation: %v", err)
	}
	status := map[string]any{
		"os": map[string]any{
			"goos":          "linux",
			"kernelName":    "Linux",
			"kernelRelease": "6.8.0-test",
		},
		"virtualization": map[string]any{"type": "kvm"},
		"serviceManager": "systemd",
		"commands":       map[string]any{"nft": true, "pf": false},
	}
	if err := store.SaveObjectStatus("routerd.net/v1alpha1", "Inventory", "host", status); err != nil {
		t.Fatalf("save inventory: %v", err)
	}
	if err := store.RecordEvent("routerd.net/v1alpha1", "Inventory", "host", "Normal", "InventoryObserved", "host inventory changed"); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.FinishGeneration(0, "Healthy", nil); err != nil {
		t.Fatalf("finish generation: %v", err)
	}

	var out bytes.Buffer
	err = run([]string{"describe", "inventory/host", "--config", configPath, "--state-file", dbPath, "--ledger-file", dbPath}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("describe inventory: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Kind:", "Inventory", "Currently observable:", "OS:", "linux", "Virtualization:", "kvm", "Service Manager:", "systemd", "InventoryObserved"} {
		if !strings.Contains(got, want) {
			t.Fatalf("describe inventory output missing %q:\n%s", want, got)
		}
	}
}

func TestDefaultStatePathUsesPlatformStateDir(t *testing.T) {
	if got := defaultStatePath(); got == "" || filepath.Base(got) != "routerd.db" {
		t.Fatalf("default state path = %q", got)
	}
}

func writeShowConfig(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "router.yaml")
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
        ifname: ens18
        managed: true
        owner: routerd
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv6PrefixDelegation
      metadata:
        name: wan-pd
      spec:
        interface: wan
        client: networkd
        prefixLength: 60
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4SourceNAT
      metadata:
        name: lan-nat
      spec:
        outboundInterface: wan
        sourceCIDRs:
          - 192.0.2.0/24
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
