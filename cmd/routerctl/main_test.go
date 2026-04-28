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
		Owner: "net.routerd.net/v1alpha1/IPv6PrefixDelegation/wan-pd",
	}})
	if err := ledger.Save(ledgerPath); err != nil {
		t.Fatalf("save ledger: %v", err)
	}

	var out bytes.Buffer
	err := run([]string{"show", "ipv6pd", "--config", configPath, "--state-file", statePath, "--ledger-file", ledgerPath}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("show ipv6pd: %v", err)
	}
	got := out.String()
	for _, want := range []string{"KIND", "IPv6PrefixDelegation", "wan-pd", "1 artifacts", "current=2001:db8:1200:1220::/60"} {
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

func TestShowPDLegacySubcommandRemoved(t *testing.T) {
	configPath := writeShowConfig(t, t.TempDir())
	dir := t.TempDir()
	var out bytes.Buffer
	err := run([]string{"show", "pd", "--config", configPath, "--state-file", filepath.Join(dir, "state.json"), "--ledger-file", filepath.Join(dir, "artifacts.json")}, &out, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown resource kind") {
		t.Fatalf("show pd err = %v, want unknown kind", err)
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
      kind: IPv6PrefixDelegation
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
