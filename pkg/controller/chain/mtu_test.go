// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestPathMTUControllerRendersMSSClamp(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}, Spec: api.DSLiteTunnelSpec{TunnelName: "ds-lite-a", MTU: 1454}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"ds-lite-a"}}},
	}}}
	store := mapStore{}
	controller := PathMTUController{Router: router, Store: store, DryRun: true, Path: dir + "/mss.nft"}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(controller.Path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{`table inet routerd_mss`, `iifname "ens19" oifname "ds-lite-a" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1414 tcp option maxseg size set 1414`} {
		if !strings.Contains(got, want) {
			t.Fatalf("mss rules missing %q:\n%s", want, got)
		}
	}
	status := store.ObjectStatus(api.RouterAPIVersion, "Router", "derived-path-mtu")
	if status["phase"] != "Applied" {
		t.Fatalf("status = %#v", status)
	}
}

func TestPathMTUControllerSkipsUnchangedLiveReload(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "nft.log")
	statePath := filepath.Join(dir, "routerd_mss.present")
	nftPath := filepath.Join(dir, "nft")
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> " + shellQuote(logPath) + "\n" +
		"if [ \"$1\" = \"list\" ]; then [ -f " + shellQuote(statePath) + " ]; exit $?; fi\n" +
		"if [ \"$1\" = \"-f\" ]; then touch " + shellQuote(statePath) + "; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(nftPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}, Spec: api.DSLiteTunnelSpec{TunnelName: "ds-lite-a", MTU: 1454}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"ds-lite-a"}}},
	}}}
	controller := PathMTUController{Router: router, Store: mapStore{}, NftCommand: nftPath, Path: filepath.Join(dir, "mss.nft")}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(logData)
	if count := countLogLine(got, "-f "+controller.Path); count != 1 {
		t.Fatalf("nft -f count = %d, want 1:\n%s", count, got)
	}
	if count := countLogLine(got, "-c -f "+controller.Path); count != 2 {
		t.Fatalf("nft -c -f count = %d, want 2:\n%s", count, got)
	}
}

func countLogLine(logData, want string) int {
	count := 0
	for _, line := range strings.Split(logData, "\n") {
		if line == want {
			count++
		}
	}
	return count
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}
