// SPDX-License-Identifier: BSD-3-Clause

package nat44

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/bus"
)

type testStore struct {
	status map[string]map[string]any
}

func (s *testStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	if s.status == nil {
		s.status = map[string]map[string]any{}
	}
	s.status[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s *testStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if s.status == nil {
		return map[string]any{}
	}
	if status := s.status[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func TestControllerRendersDryRunNAT44FromEgressRoutePolicy(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "ipv4-default"}, Spec: api.EgressRoutePolicySpec{}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-to-wan"}, Spec: api.NAT44RuleSpec{
			Type:            "masquerade",
			EgressPolicyRef: "ipv4-default",
			SourceRanges:    []string{"192.168.0.0/16"},
		}},
	}}}
	store := &testStore{}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default", map[string]any{"phase": "Applied", "selectedDevice": "ds-lite"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "nat44.nft")
	controller := Controller{Router: router, Bus: bus.New(), Store: store, DryRun: true, NftablesPath: path, NftCommand: "true"}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ruleset: %v", err)
	}
	if !strings.Contains(string(data), `oifname "ds-lite" ip saddr 192.168.0.0/16 masquerade`) {
		t.Fatalf("ruleset missing masquerade:\n%s", string(data))
	}
	status := store.ObjectStatus(api.NetAPIVersion, "NAT44Rule", "lan-to-wan")
	if status["phase"] != "Active" || status["activeEgressInterface"] != "ds-lite" {
		t.Fatalf("status = %#v", status)
	}
}

func TestControllerResolvesSNATAddressFromStaticAddress(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "ds-lite-source"}, Spec: api.IPv4StaticAddressSpec{Interface: "ds-lite", Address: "192.168.160.250/32"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-to-dslite"}, Spec: api.NAT44RuleSpec{
			Type:            "snat",
			EgressInterface: "gif41",
			SourceRanges:    []string{"192.168.160.0/24"},
			SNATAddressFrom: api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/ds-lite-source", Field: "address"},
		}},
	}}}
	store := &testStore{}
	path := filepath.Join(t.TempDir(), "nat44.nft")
	controller := Controller{Router: router, Bus: bus.New(), Store: store, DryRun: true, NftablesPath: path, NftCommand: "true"}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ruleset: %v", err)
	}
	if !strings.Contains(string(data), `oifname "gif41" ip saddr 192.168.160.0/24 snat to 192.168.160.250`) {
		t.Fatalf("ruleset missing resolved snat:\n%s", string(data))
	}
	status := store.ObjectStatus(api.NetAPIVersion, "NAT44Rule", "lan-to-dslite")
	if status["snatAddress"] != "192.168.160.250" {
		t.Fatalf("status = %#v", status)
	}
}

func TestControllerResolvesPPPoEEgressInterface(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoEInterface"}, Metadata: api.ObjectMeta{Name: "pppoe-flets"}, Spec: api.PPPoEInterfaceSpec{Interface: "wan", IfName: "ppp-flets", Username: "open@open.ad.jp"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-to-pppoe"}, Spec: api.NAT44RuleSpec{
			Type:            "masquerade",
			EgressInterface: "pppoe-flets",
			SourceRanges:    []string{"192.168.160.0/24"},
		}},
	}}}
	store := &testStore{}
	path := filepath.Join(t.TempDir(), "nat44.nft")
	controller := Controller{Router: router, Bus: bus.New(), Store: store, DryRun: true, NftablesPath: path, NftCommand: "true"}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ruleset: %v", err)
	}
	if !strings.Contains(string(data), `oifname "ppp-flets" ip saddr 192.168.160.0/24 masquerade`) {
		t.Fatalf("ruleset missing PPPoE masquerade:\n%s", string(data))
	}
	status := store.ObjectStatus(api.NetAPIVersion, "NAT44Rule", "lan-to-pppoe")
	if status["activeEgressInterface"] != "ppp-flets" {
		t.Fatalf("status = %#v", status)
	}
}

func TestControllerClearsNAT44TableWhenRuleHasNoActiveEgress(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "nft.log")
	nftPath := filepath.Join(dir, "nft")
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> " + testShellQuote(logPath) + "\n" +
		"exit 0\n"
	if err := os.WriteFile(nftPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-to-missing"}, Spec: api.NAT44RuleSpec{
			Type:            "masquerade",
			EgressPolicyRef: "missing-policy",
			SourceRanges:    []string{"192.168.0.0/16"},
		}},
	}}}
	store := &testStore{}
	controller := Controller{Router: router, Store: store, NftCommand: nftPath}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(logData)
	for _, want := range []string{
		"list table ip routerd_nat",
		"delete table ip routerd_nat",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nft command log missing %q:\n%s", want, got)
		}
	}
	status := store.ObjectStatus(api.NetAPIVersion, "NAT44Rule", "lan-to-missing")
	if status["phase"] != "Pending" {
		t.Fatalf("status = %#v", status)
	}
}

func testShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
