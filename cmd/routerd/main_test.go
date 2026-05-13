// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/apply"
	"routerd/pkg/controlapi"
	"routerd/pkg/eventlog"
	"routerd/pkg/platform"
	"routerd/pkg/render"
	"routerd/pkg/resource"
	routerstate "routerd/pkg/state"
)

func TestApplyFilesReportsCreatedAndChanged(t *testing.T) {
	dir := t.TempDir()
	netdevPath := filepath.Join(dir, "10-routerd-vxlan100.netdev")
	dropinPath := filepath.Join(dir, "ens18.network.d", "90-routerd.conf")
	netdevData := []byte("[NetDev]\nName=vxlan100\nKind=vxlan\n")
	dropinData := []byte("[Network]\nDHCP=yes\n")

	if err := os.MkdirAll(filepath.Dir(dropinPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dropinPath, dropinData, 0644); err != nil {
		t.Fatalf("seed dropin: %v", err)
	}

	changed, created, err := applyFiles([]render.File{
		{Path: netdevPath, Data: netdevData},
		{Path: dropinPath, Data: dropinData},
	})
	if err != nil {
		t.Fatalf("applyFiles: %v", err)
	}
	if len(changed) != 1 || changed[0] != netdevPath {
		t.Fatalf("changed = %v, want [%s]", changed, netdevPath)
	}
	if len(created) != 1 || created[0] != netdevPath {
		t.Fatalf("created = %v, want [%s]", created, netdevPath)
	}

	changed, created, err = applyFiles([]render.File{
		{Path: netdevPath, Data: append(netdevData, '\n')},
	})
	if err != nil {
		t.Fatalf("applyFiles second call: %v", err)
	}
	if len(changed) != 1 || changed[0] != netdevPath {
		t.Fatalf("second call changed = %v", changed)
	}
	if len(created) != 0 {
		t.Fatalf("second call created = %v, want none", created)
	}
}

func TestRunApplyOnceDryRunDoesNotCreateGeneration(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "routerd.db")
	statusPath := filepath.Join(dir, "status.json")
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{
			Name: "test-router",
		},
	}
	result, err := runApplyOnce(router, applyOptions{
		DryRun:     true,
		StatePath:  statePath,
		StatusFile: statusPath,
		ConfigPath: filepath.Join(dir, "router.yaml"),
	}, io.Discard, &eventlog.Logger{})
	if err != nil {
		t.Fatalf("dry-run apply: %v", err)
	}
	if result.Generation != 0 {
		t.Fatalf("dry-run generation = %d, want 0", result.Generation)
	}
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = store.Close() }()
	if got := store.LatestGeneration(); got != 0 {
		t.Fatalf("latest generation after dry-run = %d, want 0", got)
	}
}

func TestActiveControllerDryRunModes(t *testing.T) {
	got := activeControllerDryRunModes(map[string]bool{
		"route": false,
		"ra":    true,
		"nat":   false,
		"foo":   true,
	})
	want := []string{"foo", "ra"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("activeControllerDryRunModes = %v, want %v", got, want)
	}
}

func TestControllerStatusesFromDryRunModes(t *testing.T) {
	got := controllerStatusesFromDryRunModes(map[string]bool{
		"route": false,
		"nat":   true,
	})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "nat" || got[0].Mode != "dry-run" {
		t.Fatalf("first status = %+v, want nat dry-run", got[0])
	}
	if got[0].Reason != controlapi.ControllerModeReasonManual || got[0].Message == "" {
		t.Fatalf("first reason = %q message = %q, want manual reason with message", got[0].Reason, got[0].Message)
	}
	if got[1].Name != "route" || got[1].Mode != "live" {
		t.Fatalf("second status = %+v, want route live", got[1])
	}
	if got[1].Reason != controlapi.ControllerModeReasonLive || got[1].Message == "" {
		t.Fatalf("second reason = %q message = %q, want live reason with message", got[1].Reason, got[1].Message)
	}
	if len(got[0].ResourceKinds) == 0 {
		t.Fatalf("nat resource kinds should be populated")
	}
}

func TestControllerResourceKindsUseCanonicalNames(t *testing.T) {
	kinds := controllerResourceKinds("dhcpv6")
	for _, kind := range kinds {
		if kind == "IPv6DHCPv6Server" {
			t.Fatalf("controllerResourceKinds returned legacy kind %q in %v", kind, kinds)
		}
	}
	if !reflect.DeepEqual(kinds, []string{"DHCPv6Server", "DHCPv6Scope", "IPv6RouterAdvertisement"}) {
		t.Fatalf("dhcpv6 resource kinds = %v", kinds)
	}
}

func TestHasNewNetdevFiles(t *testing.T) {
	if !hasNewNetdevFiles([]string{"/etc/systemd/network/10-vxlan.netdev"}) {
		t.Fatal("expected new .netdev to be detected")
	}
	if hasNewNetdevFiles([]string{"/etc/systemd/network/10-vxlan.network"}) {
		t.Fatal("plain .network should not trigger new-netdev path")
	}
	if hasNewNetdevFiles(nil) {
		t.Fatal("nil should not trigger")
	}
}

func TestApplyNetworkConfigSkipsUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	netplanPath := filepath.Join(dir, "netplan", "90-routerd.yaml")
	dropinPath := filepath.Join(dir, "systemd", "10-netplan-ens18.network.d", "90-routerd-dhcpv6-pd.conf")
	netplanData := []byte("network:\n  version: 2\n")
	dropinData := []byte("[Network]\nDHCP=yes\n")

	if err := os.MkdirAll(filepath.Dir(netplanPath), 0755); err != nil {
		t.Fatalf("create netplan dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dropinPath), 0755); err != nil {
		t.Fatalf("create dropin dir: %v", err)
	}
	if err := os.WriteFile(netplanPath, netplanData, 0600); err != nil {
		t.Fatalf("write netplan fixture: %v", err)
	}
	if err := os.WriteFile(dropinPath, dropinData, 0644); err != nil {
		t.Fatalf("write dropin fixture: %v", err)
	}

	changed, err := applyNetworkConfig(netplanPath, netplanData, []render.File{
		{Path: dropinPath, Data: dropinData},
	})
	if err != nil {
		t.Fatalf("apply network config: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed files = %v, want none", changed)
	}
}

func TestDeleteCommandRemovesStateAndLedgerForResource(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	ledgerPath := filepath.Join(dir, "artifacts.json")
	store := routerstate.New()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{LastPrefix: "2001:db8::/60"}), "test")
	if err := store.Save(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}
	ledger := resource.NewLedger()
	ledger.Remember([]resource.Artifact{{
		Kind:  "file",
		Name:  "/tmp/routerd-test",
		Owner: "net.routerd.net/v1alpha1/DHCPv6PrefixDelegation/wan-pd",
	}})
	if err := ledger.Save(ledgerPath); err != nil {
		t.Fatalf("save ledger: %v", err)
	}

	var out strings.Builder
	if err := deleteCommand([]string{"--state-file", statePath, "--ledger-file", ledgerPath, "pd/wan-pd"}, &out); err != nil {
		t.Fatalf("delete command: %v", err)
	}
	if !strings.Contains(out.String(), "delete net.routerd.net/v1alpha1/DHCPv6PrefixDelegation/wan-pd") {
		t.Fatalf("delete output = %s", out.String())
	}
	loadedState, err := routerstate.Load(statePath)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if got := loadedState.Get("ipv6PrefixDelegation.wan-pd.lease"); got.Status != routerstate.StatusUnknown {
		t.Fatalf("state after delete = %+v, want unknown", got)
	}
	loadedLedger, err := resource.LoadLedger(ledgerPath)
	if err != nil {
		t.Fatalf("load ledger: %v", err)
	}
	if len(loadedLedger.All()) != 0 {
		t.Fatalf("ledger after delete = %+v, want empty", loadedLedger.All())
	}
}

func TestDeleteCommandFileTargetsRouterResources(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	if err := os.WriteFile(configPath, []byte(`apiVersion: routerd.net/v1alpha1
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
        ifName: ens18
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	targets, err := deleteTargets(nil, configPath)
	if err != nil {
		t.Fatalf("delete targets: %v", err)
	}
	if len(targets) != 1 || targets[0].Kind != "Interface" || targets[0].Name != "wan" {
		t.Fatalf("targets = %+v", targets)
	}
}

func TestWriteFileIfChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.conf")

	changed, err := writeFileIfChanged(path, []byte("one\n"), 0644)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	if !changed {
		t.Fatal("first write changed = false, want true")
	}

	changed, err = writeFileIfChanged(path, []byte("one\n"), 0644)
	if err != nil {
		t.Fatalf("same write: %v", err)
	}
	if changed {
		t.Fatal("same write changed = true, want false")
	}

	changed, err = writeFileIfChanged(path, []byte("two\n"), 0644)
	if err != nil {
		t.Fatalf("different write: %v", err)
	}
	if !changed {
		t.Fatal("different write changed = false, want true")
	}
}

func TestRouterWithIPv6PDClientOptionsResolvesFlavorDefaults(t *testing.T) {
	router := testRouterWithPrefixDelegation(api.DHCPv6PrefixDelegationSpec{
		Interface: "wan",
		Profile:   api.IPv6PDProfileNTTHGWLANPD,
	})

	got, warnings, err := routerWithIPv6PDClientOptions(router, applyOptions{}, "linux", false)
	if err != nil {
		t.Fatalf("resolve PD options: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	spec, err := got.Spec.Resources[1].DHCPv6PrefixDelegationSpec()
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if spec.Client != api.IPv6PDClientRouterd {
		t.Fatalf("client = %q, want routerd-dhcpv6-client", spec.Client)
	}
	if spec.Profile != api.IPv6PDProfileNTTHGWLANPD {
		t.Fatalf("profile = %q, want original profile", spec.Profile)
	}

	got, warnings, err = routerWithIPv6PDClientOptions(router, applyOptions{}, "linux", true)
	if err != nil {
		t.Fatalf("resolve NixOS PD options: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("nixos warnings = %v, want none", warnings)
	}
	spec, err = got.Spec.Resources[1].DHCPv6PrefixDelegationSpec()
	if err != nil {
		t.Fatalf("read nixos spec: %v", err)
	}
	if spec.Client != api.IPv6PDClientRouterd {
		t.Fatalf("nixos client = %q, want routerd-dhcpv6-client", spec.Client)
	}
}

func TestRouterWithIPv6PDClientOptionsOverridesAndWarns(t *testing.T) {
	router := testRouterWithPrefixDelegation(api.DHCPv6PrefixDelegationSpec{
		Interface: "wan",
		Client:    api.IPv6PDClientDHCPv6C,
		Profile:   api.IPv6PDProfileNTTHGWLANPD,
	})

	got, warnings, err := routerWithIPv6PDClientOptions(router, applyOptions{
		OverrideClient: api.IPv6PDClientDHCPCD,
	}, "freebsd", false)
	if err != nil {
		t.Fatalf("resolve overridden PD options: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one known-ng warning", warnings)
	}
	if !strings.Contains(warnings[0], "Known") && !strings.Contains(warnings[0], "known problematic") {
		t.Fatalf("warning does not describe known problem: %q", warnings[0])
	}
	spec, err := got.Spec.Resources[1].DHCPv6PrefixDelegationSpec()
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if spec.Client != api.IPv6PDClientDHCPCD {
		t.Fatalf("client = %q, want override dhcpcd", spec.Client)
	}

	original, err := router.Spec.Resources[1].DHCPv6PrefixDelegationSpec()
	if err != nil {
		t.Fatalf("read original spec: %v", err)
	}
	if original.Client != api.IPv6PDClientDHCPv6C {
		t.Fatalf("original router mutated: client = %q", original.Client)
	}
}

func TestRouterWithIPv6PDClientOptionsRejectsInvalidOverride(t *testing.T) {
	router := testRouterWithPrefixDelegation(api.DHCPv6PrefixDelegationSpec{Interface: "wan"})
	if _, _, err := routerWithIPv6PDClientOptions(router, applyOptions{OverrideClient: "bad"}, "linux", false); err == nil {
		t.Fatal("expected invalid override client to be rejected")
	}
	if _, _, err := routerWithIPv6PDClientOptions(router, applyOptions{OverrideProfile: "bad"}, "linux", false); err == nil {
		t.Fatal("expected invalid override profile to be rejected")
	}
}

func testRouterWithPrefixDelegation(spec api.DHCPv6PrefixDelegationSpec) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
				Metadata: api.ObjectMeta{Name: "wan-pd"},
				Spec:     spec,
			},
		}},
	}
}

func TestParseFreeBSDRCConf(t *testing.T) {
	got, err := parseFreeBSDRCConf([]byte(`# Generated
gateway_enable="YES"
ifconfig_vtnet2="DHCP"
ifconfig_vtnet0_ipv6="inet6 accept_rtadv"
`))
	if err != nil {
		t.Fatalf("parse rc.conf: %v", err)
	}
	for key, want := range map[string]string{
		"gateway_enable":       "YES",
		"ifconfig_vtnet2":      "DHCP",
		"ifconfig_vtnet0_ipv6": "inet6 accept_rtadv",
	} {
		if got[key] != want {
			t.Fatalf("%s = %q, want %q", key, got[key], want)
		}
	}
	if ifname := freeBSDIfconfigKeyInterface("ifconfig_vtnet0_ipv6"); ifname != "vtnet0" {
		t.Fatalf("ifconfig key interface = %q, want vtnet0", ifname)
	}
	ifnames := freeBSDDHCPClientIfnames([]byte("interface \"vtnet2\" {\n  ignore routers;\n};\n"))
	if len(ifnames) != 1 || ifnames[0] != "vtnet2" {
		t.Fatalf("dhclient ifnames = %v, want [vtnet2]", ifnames)
	}
}

func TestParseFreeBSDSysrcValue(t *testing.T) {
	tests := []struct {
		name string
		key  string
		out  string
		want string
	}{
		{name: "dash value", key: "dhcp6c_flags", out: "dhcp6c_flags: -n\n", want: "-n"},
		{name: "quoted style not emitted", key: "ifconfig_vtnet0", out: "ifconfig_vtnet0: DHCP\n", want: "DHCP"},
		{name: "fallback raw", key: "missing", out: "NO\n", want: "NO"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseFreeBSDSysrcValue(tt.key, []byte(tt.out)); got != tt.want {
				t.Fatalf("parseFreeBSDSysrcValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyFreeBSDConfigDoesNotReclaimStaleSysrcKeys(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	stateLog := filepath.Join(dir, "sysrc-calls.log")
	writeExecutable(t, filepath.Join(binDir, "sysrc"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
case "$1" in
  -x) exit 0 ;;
  *) echo "$1: NO"; exit 0 ;;
esac
`, stateLog))
	writeExecutable(t, filepath.Join(binDir, "service"), `#!/bin/sh
if [ "$1" = "dhcp6c" ] && [ "$2" = "status" ]; then exit 0; fi
exit 0
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "vtnet0", Managed: false, Owner: "external"}},
	}}}

	store := routerstate.New()
	store.Set(freebsdSysrcStateKey, "ifconfig_vxlan102,ifconfig_vxlan103,gateway_enable", "test seed")

	_, _, err := applyFreeBSDConfig(router, store, "", "", "", "")
	if err != nil {
		t.Fatalf("apply FreeBSD config: %v", err)
	}

	got, err := os.ReadFile(stateLog)
	if err != nil {
		t.Fatalf("read sysrc log: %v", err)
	}
	if strings.Contains(string(got), "-x ") {
		t.Fatalf("FreeBSD apply must not remove sysrc keys implicitly:\n%s", got)
	}
}

func TestApplyFreeBSDConfigAppliesPFAndRCDScripts(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	sysrcLog := filepath.Join(dir, "sysrc.log")
	serviceLog := filepath.Join(dir, "service.log")
	pfctlLog := filepath.Join(dir, "pfctl.log")
	writeExecutable(t, filepath.Join(binDir, "sysrc"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
case "$1" in
  -x) exit 0 ;;
  *) echo "$1: NO"; exit 0 ;;
esac
`, sysrcLog))
	writeExecutable(t, filepath.Join(binDir, "service"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
if [ "$1" = "-l" ]; then
  printf 'pf\npflog\nrouterd_healthcheck_internet\n'
  exit 0
fi
if [ "$2" = "status" ]; then
  exit 1
fi
exit 0
`, serviceLog))
	writeExecutable(t, filepath.Join(binDir, "pfctl"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
exit 0
`, pfctlLog))
	writeExecutable(t, filepath.Join(binDir, "kldstat"), `#!/bin/sh
exit 1
`)
	writeExecutable(t, filepath.Join(binDir, "kldload"), `#!/bin/sh
exit 0
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "vtnet0", Managed: false, Owner: "external"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "SystemdUnit"},
			Metadata: api.ObjectMeta{Name: "routerd-healthcheck@internet.service"},
			Spec: api.SystemdUnitSpec{
				ExecStart:        []string{"/usr/local/sbin/routerd-healthcheck", "daemon", "--resource", "internet"},
				RuntimeDirectory: []string{"routerd/healthcheck"},
				StateDirectory:   []string{"routerd/healthcheck"},
			},
		},
	}}}

	pfPath := filepath.Join(dir, "etc", "pf.conf")
	rcDir := filepath.Join(dir, "rc.d")
	changed, warnings, err := applyFreeBSDConfig(router, routerstate.New(), "", "", pfPath, rcDir)
	if err != nil {
		t.Fatalf("apply FreeBSD config: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	for _, path := range []string{pfPath, filepath.Join(rcDir, "routerd_healthcheck_internet")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to be written: %v", path, err)
		}
	}
	pfctlCalls, err := os.ReadFile(pfctlLog)
	if err != nil {
		t.Fatalf("read pfctl log: %v", err)
	}
	for _, want := range []string{"-nf " + pfPath, "-f " + pfPath} {
		if !strings.Contains(string(pfctlCalls), want) {
			t.Fatalf("pfctl calls missing %q:\n%s", want, pfctlCalls)
		}
	}
	if !strings.Contains(string(pfctlCalls), "-e") {
		t.Fatalf("pfctl calls missing enable:\n%s", pfctlCalls)
	}
	serviceCalls, err := os.ReadFile(serviceLog)
	if err != nil {
		t.Fatalf("read service log: %v", err)
	}
	for _, want := range []string{"pf onestart", "pflog onestart", "routerd_healthcheck_internet onestart"} {
		if !strings.Contains(string(serviceCalls), want) {
			t.Fatalf("service calls missing %q:\n%s", want, serviceCalls)
		}
	}
	gotChanged := strings.Join(changed, "\n")
	for _, want := range []string{pfPath, filepath.Join(rcDir, "routerd_healthcheck_internet"), "service:pf", "service:pflog", "service:routerd_healthcheck_internet"} {
		if !strings.Contains(gotChanged, want) {
			t.Fatalf("changed missing %q:\n%v", want, changed)
		}
	}
}

func TestApplyFreeBSDConfigContinuesAfterNTPStartFailure(t *testing.T) {
	dir := t.TempDir()
	oldDefaults := platformDefaults
	platformDefaults.SysconfDir = filepath.Join(dir, "usr", "local", "etc", "routerd")
	t.Cleanup(func() { platformDefaults = oldDefaults })
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	serviceLog := filepath.Join(dir, "service.log")
	writeExecutable(t, filepath.Join(binDir, "sysrc"), `#!/bin/sh
case "$1" in
  -x) exit 0 ;;
  *) echo "$1: NO"; exit 0 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "service"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
if [ "$1" = "-l" ]; then
  printf 'ntpd\nrouterd_healthcheck_internet\n'
  exit 0
fi
if [ "$1" = "ntpd" ] && [ "$2" = "start" ]; then
  echo "ntpd failed to start" >&2
  exit 1
fi
if [ "$2" = "status" ]; then
  exit 1
fi
exit 0
`, serviceLog))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPClient"},
			Metadata: api.ObjectMeta{Name: "time"},
			Spec: api.NTPClientSpec{
				Provider: "ntpd",
				Managed:  true,
				Source:   "static",
				Servers:  []string{"ntp.example.net"},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "SystemdUnit"},
			Metadata: api.ObjectMeta{Name: "routerd-healthcheck@internet.service"},
			Spec: api.SystemdUnitSpec{
				ExecStart:        []string{"/usr/local/sbin/routerd-healthcheck", "daemon", "--resource", "internet"},
				RuntimeDirectory: []string{"routerd/healthcheck"},
				StateDirectory:   []string{"routerd/healthcheck"},
			},
		},
	}}}

	rcDir := filepath.Join(dir, "rc.d")
	changed, warnings, err := applyFreeBSDConfig(router, routerstate.New(), "", "", "", rcDir)
	if err != nil {
		t.Fatalf("apply FreeBSD config should continue after ntpd failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rcDir, "routerd_healthcheck_internet")); err != nil {
		t.Fatalf("expected rc.d script to be written after ntpd warning: %v", err)
	}
	if !stringSliceContainsPrefix(warnings, "service ntpd start:") {
		t.Fatalf("warnings = %v, want ntpd warning", warnings)
	}
	if !stringSliceContains(changed, filepath.Join(rcDir, "routerd_healthcheck_internet")) {
		t.Fatalf("changed = %v, want rc.d script path", changed)
	}
}

func TestApplyFreeBSDRCDScriptsDoesNotRestartRouterdFromApply(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	serviceLog := filepath.Join(dir, "service.log")
	writeExecutable(t, filepath.Join(binDir, "service"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
if [ "$2" = "status" ]; then
  exit 0
fi
exit 0
`, serviceLog))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	changed, err := applyFreeBSDRCDScripts(map[string][]byte{
		"routerd": []byte("#!/bin/sh\n# PROVIDE: routerd\n"),
	}, filepath.Join(dir, "rc.d"))
	if err != nil {
		t.Fatalf("apply rc.d scripts: %v", err)
	}
	if !stringSliceContains(changed, "service:routerd:restart-required") {
		t.Fatalf("changed = %v, want restart-required marker", changed)
	}
	serviceCalls, err := os.ReadFile(serviceLog)
	if err == nil && strings.Contains(string(serviceCalls), "routerd") {
		t.Fatalf("routerd service must not be controlled from routerd apply:\n%s", serviceCalls)
	}
}

func TestApplyFreeBSDConfigContinuesAfterPackageFailure(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	writeExecutable(t, filepath.Join(binDir, "pkg"), `#!/bin/sh
exit 3
`)
	writeExecutable(t, filepath.Join(binDir, "sysrc"), `#!/bin/sh
if [ "$#" -eq 1 ]; then
  echo "$1: NO"
  exit 0
fi
exit 0
`)
	writeExecutable(t, filepath.Join(binDir, "service"), `#!/bin/sh
if [ "$2" = "status" ]; then
  exit 1
fi
exit 0
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Package"},
			Metadata: api.ObjectMeta{Name: "deps"},
			Spec: api.PackageSpec{Packages: []api.OSPackageSetSpec{{
				OS:      "freebsd",
				Manager: "pkg",
				Names:   []string{"dnsmasq"},
			}}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "SystemdUnit"},
			Metadata: api.ObjectMeta{Name: "routerd-healthcheck@internet.service"},
			Spec: api.SystemdUnitSpec{
				ExecStart: []string{"/usr/local/sbin/routerd-healthcheck", "daemon", "--resource", "internet"},
			},
		},
	}}}

	rcDir := filepath.Join(dir, "rc.d")
	changed, warnings, err := applyFreeBSDConfig(router, routerstate.New(), "", "", "", rcDir)
	if err != nil {
		t.Fatalf("apply FreeBSD config should continue after package failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rcDir, "routerd_healthcheck_internet")); err != nil {
		t.Fatalf("expected rc.d script to be written after package warning: %v", err)
	}
	if !stringSliceContainsPrefix(warnings, "pkg install:") {
		t.Fatalf("warnings = %v, want package warning", warnings)
	}
	if !stringSliceContains(changed, filepath.Join(rcDir, "routerd_healthcheck_internet")) {
		t.Fatalf("changed = %v, want rc.d script path", changed)
	}
}

func TestApplyFreeBSDRCDScriptsDisablesExecutableBackups(t *testing.T) {
	dir := t.TempDir()
	rcDir := filepath.Join(dir, "rc.d")
	if err := os.MkdirAll(rcDir, 0755); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(rcDir, "routerd.recovery.20260510T235157Z")
	if err := os.WriteFile(backup, []byte("#!/bin/sh\n# PROVIDE: routerd\n"), 0555); err != nil {
		t.Fatal(err)
	}
	changed, err := applyFreeBSDRCDScripts(map[string][]byte{
		"routerd": []byte("#!/bin/sh\n# PROVIDE: routerd\n"),
	}, rcDir)
	if err != nil {
		t.Fatal(err)
	}
	if !stringSliceContainsPrefix(changed, "rc.d:disable-stale:") {
		t.Fatalf("changed = %v, want stale rc.d backup disable marker", changed)
	}
	info, err := os.Stat(backup)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0111 != 0 {
		t.Fatalf("backup remained executable: %v", info.Mode())
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func stringSliceContainsPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func TestRunFreeBSDServiceTreatsAlreadyRunningAsIdempotentStart(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	writeExecutable(t, filepath.Join(binDir, "service"), `#!/bin/sh
echo "daemon: process already running, pid: 10220" >&2
exit 1
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := runFreeBSDService("routerd_dhcpv6_client_wan_pd", "onestart"); err != nil {
		t.Fatalf("onestart already running should be idempotent: %v", err)
	}
	if err := runFreeBSDService("routerd_dhcpv6_client_wan_pd", "onerestart"); err == nil {
		t.Fatal("onerestart failure should not be hidden")
	}
}

func TestApplyFreeBSDDnsmasqConfigValidatesAndUsesPersistentLeaseDirectory(t *testing.T) {
	dir := t.TempDir()
	oldDefaults := platformDefaults
	platformDefaults = platform.Defaults{
		OS:         platform.OSFreeBSD,
		RuntimeDir: filepath.Join(dir, "var", "run", "routerd"),
		StateDir:   filepath.Join(dir, "var", "db", "routerd"),
	}
	t.Cleanup(func() { platformDefaults = oldDefaults })

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	dnsmasqLog := filepath.Join(dir, "dnsmasq.log")
	serviceLog := filepath.Join(dir, "service.log")
	sysrcLog := filepath.Join(dir, "sysrc.log")
	writeExecutable(t, filepath.Join(binDir, "dnsmasq"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
exit 0
`, dnsmasqLog))
	writeExecutable(t, filepath.Join(binDir, "sysrc"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
exit 0
`, sysrcLog))
	writeExecutable(t, filepath.Join(binDir, "service"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
if [ "$1" = "-l" ]; then
  printf 'routerd_dnsmasq\n'
  exit 0
fi
if [ "$2" = "status" ]; then
  exit 1
fi
exit 0
`, serviceLog))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	configPath := filepath.Join(dir, "usr", "local", "etc", "routerd", "dnsmasq.conf")
	servicePath := filepath.Join(dir, "usr", "local", "etc", "rc.d", "routerd_dnsmasq")
	changed, err := applyFreeBSDDnsmasqConfig(configPath, servicePath, []byte("port=0\ndhcp-leasefile="+dnsmasqLeaseFileForPlatform()+"\n"), filepath.Join(binDir, "dnsmasq"))
	if err != nil {
		t.Fatalf("apply FreeBSD dnsmasq: %v", err)
	}
	for _, path := range []string{
		configPath,
		servicePath,
		filepath.Join(platformDefaults.StateDir, "dnsmasq"),
		platformDefaults.RuntimeDir,
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}
	if !containsString(changed, configPath) || !containsString(changed, servicePath) {
		t.Fatalf("changed = %v", changed)
	}
	dnsmasqCalls, err := os.ReadFile(dnsmasqLog)
	if err != nil {
		t.Fatalf("read dnsmasq log: %v", err)
	}
	if !strings.Contains(string(dnsmasqCalls), "--test --conf-file="+configPath) {
		t.Fatalf("dnsmasq --test not called:\n%s", dnsmasqCalls)
	}
	serviceData, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatalf("read rc.d script: %v", err)
	}
	for _, want := range []string{
		`mkdir -p "` + platformDefaults.RuntimeDir + `"`,
		`mkdir -p "` + filepath.Join(platformDefaults.StateDir, "dnsmasq") + `"`,
	} {
		if !strings.Contains(string(serviceData), want) {
			t.Fatalf("rc.d script missing %q:\n%s", want, serviceData)
		}
	}
}

func TestApplyFreeBSDDSLiteSupportsDynamicDelegatedAddress(t *testing.T) {
	dir := t.TempDir()
	oldDefaults := platformDefaults
	platformDefaults = platform.Defaults{OS: platform.OSFreeBSD}
	t.Cleanup(func() { platformDefaults = oldDefaults })

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	ifconfigLog := filepath.Join(dir, "ifconfig.log")
	routeLog := filepath.Join(dir, "route.log")
	writeExecutable(t, filepath.Join(binDir, "ifconfig"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
if [ "$1" = "vtnet1" ] && [ "$#" -eq 1 ]; then
  cat <<'OUT'
vtnet1: flags=1008843<UP,BROADCAST,RUNNING,SIMPLEX,MULTICAST> metric 0 mtu 1500
	inet6 fe80::1%%vtnet1 prefixlen 64 scopeid 0x2
	inet6 2001:db8:1234:5678::abcd prefixlen 64
OUT
  exit 0
fi
if [ "$#" -eq 1 ]; then
  exit 1
fi
exit 0
`, ifconfigLog))
	writeExecutable(t, filepath.Join(binDir, "route"), fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
if [ "$1" = "-n" ] && [ "$2" = "get" ]; then
  printf 'gateway: 192.0.2.1\ninterface: vtnet0\n'
  exit 0
fi
exit 0
`, routeLog))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "vtnet0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "vtnet1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-ipv6"}, Spec: api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", SubnetID: "0", AddressSuffix: "::1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite"}, Spec: api.DSLiteTunnelSpec{
			Interface:             "wan",
			TunnelName:            "ds-lite",
			LocalAddressSource:    "delegatedAddress",
			LocalDelegatedAddress: "lan-ipv6",
			LocalAddressSuffix:    "::11",
			AFTRIPv6:              "2001:db8::feed",
			MTU:                   1454,
			DefaultRoute:          true,
		}},
	}}}

	store := routerstate.New()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{CurrentPrefix: "2001:db8:1234:5678::/64"}), "test")
	changed, err := applyDSLiteTunnelsWithState(router, store)
	if err != nil {
		t.Fatalf("apply DS-Lite tunnels: %v", err)
	}
	if !containsString(changed, "ds-lite") {
		t.Fatalf("changed = %v, want ds-lite", changed)
	}
	gif := freeBSDDSLiteRuntimeIfName("ds-lite")
	ifconfigCalls, err := os.ReadFile(ifconfigLog)
	if err != nil {
		t.Fatalf("read ifconfig log: %v", err)
	}
	for _, want := range []string{
		"vtnet1 inet6 2001:db8:1234:5678::11 prefixlen 64 alias",
		gif + " create",
		gif + " inet6 tunnel 2001:db8:1234:5678::11 2001:db8::feed",
		gif + " inet 192.0.0.2 192.0.0.1 netmask 255.255.255.255",
		gif + " mtu 1454",
		gif + " up",
	} {
		if !strings.Contains(string(ifconfigCalls), want) {
			t.Fatalf("ifconfig calls missing %q:\n%s", want, ifconfigCalls)
		}
	}
	routeCalls, err := os.ReadFile(routeLog)
	if err != nil {
		t.Fatalf("read route log: %v", err)
	}
	if !strings.Contains(string(routeCalls), "-n change default 192.0.0.1") {
		t.Fatalf("route change not called for %s:\n%s", gif, routeCalls)
	}
}

func TestCleanupLegacyFreeBSDStateDirMovesVarLibRouterd(t *testing.T) {
	dir := t.TempDir()
	oldDefaults := platformDefaults
	oldLegacy := legacyFreeBSDStateDir
	platformDefaults = platform.Defaults{
		OS:       platform.OSFreeBSD,
		StateDir: filepath.Join(dir, "var", "db", "routerd"),
	}
	legacyFreeBSDStateDir = filepath.Join(dir, "var", "lib", "routerd")
	t.Cleanup(func() {
		platformDefaults = oldDefaults
		legacyFreeBSDStateDir = oldLegacy
	})

	staleLease := filepath.Join(legacyFreeBSDStateDir, "dhcpv6-client", "wan-pd", "lease.json")
	if err := os.MkdirAll(filepath.Dir(staleLease), 0755); err != nil {
		t.Fatalf("create legacy state: %v", err)
	}
	if err := os.WriteFile(staleLease, []byte(`{"currentPrefix":"2001:db8::/60"}`), 0644); err != nil {
		t.Fatalf("write stale lease: %v", err)
	}

	moved, err := cleanupLegacyFreeBSDStateDir()
	if err != nil {
		t.Fatalf("cleanup legacy state: %v", err)
	}
	if len(moved) != 1 || !strings.Contains(moved[0], "legacy-var-lib-routerd-") {
		t.Fatalf("moved = %v", moved)
	}
	if _, err := os.Stat(legacyFreeBSDStateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy state dir still exists or unexpected stat error: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(platformDefaults.StateDir, "legacy-var-lib-routerd-*", "dhcpv6-client", "wan-pd", "lease.json"))
	if err != nil {
		t.Fatalf("glob moved lease: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("moved stale lease matches = %v", matches)
	}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestFreeBSDProtectedIfnames(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{
		Apply: api.ApplyPolicySpec{ProtectedInterfaces: []string{"mgmt"}},
		Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "vtnet0", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "mgmt"},
				Spec:     api.InterfaceSpec{IfName: "vtnet2", Managed: true},
			},
		},
	}}
	got := freeBSDProtectedIfnames(router)
	if !got["vtnet2"] {
		t.Fatalf("protected ifnames = %v, want vtnet2", got)
	}
	if got["vtnet0"] {
		t.Fatalf("protected ifnames = %v, did not want vtnet0", got)
	}
}

func TestReplaceManagedPPPoEBlocks(t *testing.T) {
	current := "# existing\nold * value *\n# BEGIN routerd pppoe old\n\"u\" * \"old\" *\n# END routerd pppoe old\n"
	got := replaceManagedPPPoEBlocks(current, []render.PPPoESecretEntry{
		{Name: "wan", Username: "user@example.jp", Password: "secret"},
	})
	for _, want := range []string{
		"# existing\nold * value *\n",
		"# BEGIN routerd pppoe wan\n",
		"\"user@example.jp\" * \"secret\" *\n",
		"# END routerd pppoe wan\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("managed secrets missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\"u\" * \"old\"") {
		t.Fatalf("old managed block was not removed:\n%s", got)
	}
}

func TestRenderIPv4DefaultRoutePolicyMarks(t *testing.T) {
	data, err := renderIPv4DefaultRoutePolicyMarks(
		"test/default",
		api.IPv4DefaultRoutePolicySpec{
			SourceCIDRs:      []string{"192.168.10.0/24"},
			DestinationCIDRs: []string{"0.0.0.0/0"},
		},
		api.IPv4DefaultRoutePolicyCandidate{Name: "pppoe", Mark: 273},
		[]api.IPv4DefaultRoutePolicyCandidate{
			{Name: "dslite", Mark: 272},
			{Name: "pppoe", Mark: 273},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("render default route policy marks: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"table ip routerd_default_route",
		"ip saddr 192.168.10.0/24 ip daddr 0.0.0.0/0 ct mark { 0x110, 0x111 } meta mark set ct mark",
		"ct mark != 0x0 ct mark != { 0x110, 0x111 } meta mark set 0x111 ct mark set meta mark",
		"ct mark 0x0 meta mark set 0x111 ct mark set meta mark",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestRenderIPv4DefaultRoutePolicyMarksRouteSetActive(t *testing.T) {
	data, err := renderIPv4DefaultRoutePolicyMarks(
		"test/default",
		api.IPv4DefaultRoutePolicySpec{
			SourceCIDRs:      []string{"192.168.10.0/24"},
			DestinationCIDRs: []string{"0.0.0.0/0"},
		},
		api.IPv4DefaultRoutePolicyCandidate{Name: "dslite", RouteSet: "lan-dslite-balance"},
		[]api.IPv4DefaultRoutePolicyCandidate{
			{Name: "dslite", RouteSet: "lan-dslite-balance"},
		},
		map[string]api.IPv4PolicyRouteSetSpec{
			"lan-dslite-balance": {
				Targets: []api.IPv4PolicyRouteTarget{
					{Name: "transix-a", Mark: 256},
					{Name: "transix-b", Mark: 257},
					{Name: "transix-c", Mark: 258},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("render default route policy marks: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"ip saddr 192.168.10.0/24 ip daddr 0.0.0.0/0 ct mark { 0x100, 0x101, 0x102 } meta mark set ct mark",
		"ct mark != 0x0 ct mark != { 0x100, 0x101, 0x102 } meta mark set 0x0 ct mark set meta mark",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ct mark 0x0 meta mark set 0x") {
		t.Fatalf("routeSet active candidate should leave new flows unmarked for IPv4PolicyRouteSet hashing:\n%s", got)
	}
}

func TestSelectIPv4DefaultRouteCandidateTreatsMissingHealthCheckAsUp(t *testing.T) {
	candidate, ok := selectIPv4DefaultRouteCandidate([]api.IPv4DefaultRoutePolicyCandidate{
		{Name: "preferred", Priority: 10, HealthCheck: "preferred-check"},
		{Name: "fallback", Priority: 20},
	}, map[string]bool{"preferred-check": false})
	if !ok {
		t.Fatal("candidate not selected")
	}
	if candidate.Name != "fallback" {
		t.Fatalf("candidate = %s, want fallback", candidate.Name)
	}
}

func TestAvailableIPv4DefaultRouteCandidatesSkipsMissingRouteSetDevices(t *testing.T) {
	candidates := []api.IPv4DefaultRoutePolicyCandidate{
		{Name: "dslite", Priority: 10, RouteSet: "dslite-set"},
		{Name: "wan", Priority: 20, Interface: "wan", HealthCheck: "wan-check"},
	}
	routeSets := map[string]api.IPv4PolicyRouteSetSpec{
		"dslite-set": {
			Targets: []api.IPv4PolicyRouteTarget{
				{OutboundInterface: "ds-lite-a"},
				{OutboundInterface: "ds-lite-b"},
			},
		},
	}
	aliases := map[string]string{
		"wan":       "ens18",
		"ds-lite-a": "ds-lite-a",
		"ds-lite-b": "ds-lite-b",
	}
	available := availableIPv4DefaultRouteCandidates(effectiveRouterAvailability{
		Router:     &api.Router{},
		Aliases:    aliases,
		RouteSets:  routeSets,
		Health:     map[string]bool{"wan-check": true},
		LinkExists: func(ifname string) bool { return ifname == "ens18" },
	}, candidates)
	if len(available) != 1 || available[0].Name != "wan" {
		t.Fatalf("available candidates = %+v, want wan only", available)
	}
}

func TestAvailableIPv4DefaultRouteCandidatesKeepsRouteSetWithAnyDevice(t *testing.T) {
	candidates := []api.IPv4DefaultRoutePolicyCandidate{
		{Name: "dslite", Priority: 10, RouteSet: "dslite-set"},
		{Name: "wan", Priority: 20, Interface: "wan"},
	}
	routeSets := map[string]api.IPv4PolicyRouteSetSpec{
		"dslite-set": {
			Targets: []api.IPv4PolicyRouteTarget{
				{OutboundInterface: "ds-lite-a"},
				{OutboundInterface: "ds-lite-b"},
			},
		},
	}
	aliases := map[string]string{
		"wan":       "ens18",
		"ds-lite-a": "ds-lite-a",
		"ds-lite-b": "ds-lite-b",
	}
	available := availableIPv4DefaultRouteCandidates(effectiveRouterAvailability{
		Router:     &api.Router{},
		Aliases:    aliases,
		RouteSets:  routeSets,
		LinkExists: func(ifname string) bool { return ifname == "ens18" || ifname == "ds-lite-b" },
	}, candidates)
	if len(available) != 2 || available[0].Name != "dslite" {
		t.Fatalf("available candidates = %+v, want dslite first", available)
	}
}

func TestAvailableIPv4DefaultRouteCandidatesSkipsDSLiteWithoutLocalAddress(t *testing.T) {
	candidates := []api.IPv4DefaultRoutePolicyCandidate{
		{Name: "dslite", Priority: 10, RouteSet: "dslite-set"},
		{Name: "wan", Priority: 20, Interface: "wan"},
	}
	routeSets := map[string]api.IPv4PolicyRouteSetSpec{
		"dslite-set": {
			Targets: []api.IPv4PolicyRouteTarget{{OutboundInterface: "ds-lite-a"}},
		},
	}
	aliases := map[string]string{
		"wan":       "ens18",
		"lan":       "ens19",
		"ds-lite-a": "ds-lite-a",
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
			Metadata: api.ObjectMeta{Name: "ds-lite-a"},
			Spec: api.DSLiteTunnelSpec{
				Interface:             "wan",
				LocalAddressSource:    "delegatedAddress",
				LocalDelegatedAddress: "lan-ipv6",
				LocalAddressSuffix:    "::3",
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"},
			Metadata: api.ObjectMeta{Name: "lan-ipv6"},
			Spec: api.IPv6DelegatedAddressSpec{
				PrefixDelegation: "wan-pd",
				Interface:        "lan",
				AddressSuffix:    "::3",
			},
		},
	}}}
	available := availableIPv4DefaultRouteCandidates(effectiveRouterAvailability{
		Router:     router,
		Aliases:    aliases,
		RouteSets:  routeSets,
		LinkExists: func(ifname string) bool { return ifname == "ens18" || ifname == "ds-lite-a" },
	}, candidates)
	if len(available) != 1 || available[0].Name != "wan" {
		t.Fatalf("available candidates = %+v, want wan only", available)
	}
}

func TestApplyDSLiteSkipsAFTRResolutionWithoutDelegatedPrefix(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "ens18"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "ens19"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"},
			Metadata: api.ObjectMeta{Name: "lan-ipv6"},
			Spec: api.IPv6DelegatedAddressSpec{
				PrefixDelegation: "wan-pd",
				Interface:        "lan",
				AddressSuffix:    "::3",
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
			Metadata: api.ObjectMeta{Name: "transix-a"},
			Spec: api.DSLiteTunnelSpec{
				Interface:             "wan",
				TunnelName:            "ds-transix-a",
				AFTRFQDN:              "invalid.invalid",
				AFTRDNSServers:        []string{"2001:db8::53"},
				AFTRAddressOrdinal:    1,
				LocalAddressSource:    "delegatedAddress",
				LocalDelegatedAddress: "lan-ipv6",
				LocalAddressSuffix:    "::100",
			},
		},
	}}}
	applied, err := applyDSLiteTunnelsWithState(router, routerstate.New())
	if err != nil {
		t.Fatalf("apply DS-Lite without delegated prefix: %v", err)
	}
	if len(applied) != 1 || applied[0] != "removed-unusable:ds-transix-a" {
		t.Fatalf("applied = %v, want removed-unusable tunnel", applied)
	}
}

func TestDSLiteAFTRDNSServersIncludeAFTRSourceStatus(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6Information", "wan-info", map[string]any{
		"phase":      "Ready",
		"dnsServers": []string{"2404:1a8:7f01:a::3", "2404:1a8:7f01:b::3"},
	}); err != nil {
		t.Fatalf("save status: %v", err)
	}
	got := dsliteAFTRDNSServersWithState(api.DSLiteTunnelSpec{
		AFTRDNSServers: []string{"2001:db8::53"},
		AFTRFrom: api.StatusValueSourceSpec{
			Resource: "DHCPv6Information/wan-info",
			Field:    "aftrName",
		},
	}, store)
	want := []string{"2001:db8::53", "2404:1a8:7f01:a::3", "2404:1a8:7f01:b::3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dns servers = %#v, want %#v", got, want)
	}
}

func TestDNSServerAddressAddsDefaultPort(t *testing.T) {
	tests := map[string]string{
		"127.0.0.1":         "127.0.0.1:53",
		"127.0.0.1:1053":    "127.0.0.1:1053",
		"2001:db8::53":      "[2001:db8::53]:53",
		"[2001:db8::53]:53": "[2001:db8::53]:53",
	}
	for input, want := range tests {
		if got := dnsServerAddress(input); got != want {
			t.Fatalf("dnsServerAddress(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestStaleIPv4ManagedFwmarkRules(t *testing.T) {
	desired := map[ipv4FwmarkRule]bool{
		{Priority: 10, Mark: 0x111, Table: 111}: true,
		{Priority: 20, Mark: 0x112, Table: 112}: true,
	}
	current := []ipv4FwmarkRule{
		{Priority: 10, Mark: 0x111, Table: 111},
		{Priority: 20, Mark: 0x112, Table: 112},
		{Priority: 30, Mark: 0x112, Table: 112},
		{Priority: 10000, Mark: 0x100, Table: 100},
		{Priority: 10001, Mark: 0x101, Table: 101},
		{Priority: 500, Mark: 0x900, Table: 900},
	}
	got := staleIPv4ManagedFwmarkRules(desired, current)
	want := []ipv4FwmarkRule{
		{Priority: 30, Mark: 0x112, Table: 112},
		{Priority: 10000, Mark: 0x100, Table: 100},
		{Priority: 10001, Mark: 0x101, Table: 101},
	}
	if len(got) != len(want) {
		t.Fatalf("stale rules = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stale rules = %+v, want %+v", got, want)
		}
	}
}

func TestResolveHealthCheckTargetDSLiteRemoteAddress(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
				Metadata: api.ObjectMeta{Name: "transix"},
				Spec: api.DSLiteTunnelSpec{
					Interface:     "wan",
					TunnelName:    "ds-transix",
					RemoteAddress: "2404:8e00::feed:100",
				},
			},
		}},
	}
	target, family, err := resolveHealthCheckTarget(router, api.HealthCheckSpec{
		Interface:    "transix",
		TargetSource: "dsliteRemote",
	}, map[string]string{"transix": "ds-transix"})
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	if target != "2404:8e00::feed:100" || family != "ipv6" {
		t.Fatalf("target/family = %s/%s, want 2404:8e00::feed:100/ipv6", target, family)
	}
}

func TestHealthCheckPingSourceUsesDSLiteLocalAddress(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
				Metadata: api.ObjectMeta{Name: "transix"},
				Spec: api.DSLiteTunnelSpec{
					Interface:          "wan",
					TunnelName:         "ds-transix",
					LocalAddressSource: "static",
					LocalAddress:       "2001:db8::3",
					RemoteAddress:      "2001:db8::100",
				},
			},
		}},
	}
	source := healthCheckPingSource(router, api.HealthCheckSpec{
		Interface:    "transix",
		TargetSource: "dsliteRemote",
	}, map[string]string{"wan": "ens18", "transix": "ds-transix"})
	if source != "2001:db8::3" {
		t.Fatalf("ping source = %q, want 2001:db8::3", source)
	}
}

func TestChangedNetworkdInterfaces(t *testing.T) {
	got := changedNetworkdInterfaces([]string{
		"/etc/systemd/network/10-netplan-ens19.network.d/90-routerd-dhcpv6-pd.conf",
		"/etc/systemd/network/10-netplan-ens19.network.d/90-routerd-extra.conf",
		"/etc/systemd/network/10-netplan-ens18.network.d/90-routerd-dhcpv6-pd.conf",
	})
	want := []string{"ens19", "ens18"}
	if len(got) != len(want) {
		t.Fatalf("interfaces = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("interfaces = %v, want %v", got, want)
		}
	}
}

func TestManagedHostnames(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Hostname"},
				Metadata: api.ObjectMeta{Name: "system-hostname"},
				Spec:     api.HostnameSpec{Hostname: "router03.example.internal", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Hostname"},
				Metadata: api.ObjectMeta{Name: "observed-hostname"},
				Spec:     api.HostnameSpec{Hostname: "ignored.example", Managed: false},
			},
		}},
	}
	got, err := managedHostnames(router)
	if err != nil {
		t.Fatalf("managed hostnames: %v", err)
	}
	if len(got) != 1 || got[0] != "router03.example.internal" {
		t.Fatalf("managed hostnames = %v, want router03.example.internal", got)
	}
}

func TestDriftedAdoptionCandidates(t *testing.T) {
	candidates := []apply.AdoptionCandidate{
		{
			Kind:     "host.hostname",
			Name:     "system",
			Desired:  map[string]string{"hostname": "router03.example.internal"},
			Observed: map[string]string{"hostname": "router03"},
		},
		{
			Kind:     "linux.ipv4.routeTable",
			Name:     "table=111",
			Desired:  map[string]string{"table": "111", "ifname": "ppp0"},
			Observed: map[string]string{"table": "111", "ifname": "ppp0"},
		},
	}
	got := driftedAdoptionCandidates(candidates)
	if len(got) != 1 || got[0].Kind != "host.hostname" {
		t.Fatalf("drifted candidates = %+v, want hostname only", got)
	}
}

func TestAdoptedArtifactsForResultDeduplicates(t *testing.T) {
	artifacts := []resource.Artifact{
		{Kind: "nft.table", Name: "routerd_nat", Owner: "one"},
		{Kind: "nft.table", Name: "routerd_nat", Owner: "one"},
		{Kind: "host.hostname", Name: "system", Owner: "host"},
	}
	got := adoptedArtifactsForResult(artifacts)
	if len(got) != 2 {
		t.Fatalf("adopted artifacts = %+v, want two", got)
	}
}

func TestDeriveIPv6Address(t *testing.T) {
	got, err := deriveIPv6Address([]string{"2001:db8:3d60:1220::/64"}, "::100")
	if err != nil {
		t.Fatalf("derive IPv6 address: %v", err)
	}
	if got != "2001:db8:3d60:1220::100" {
		t.Fatalf("address = %s, want 2001:db8:3d60:1220::100", got)
	}
}

func TestDeriveIPv6AddressFromGlobalAddress(t *testing.T) {
	got, err := deriveIPv6AddressFromGlobalAddress([]string{
		"fe80::3",
		"2001:db8:3d60:1220::3",
	}, "::100")
	if err != nil {
		t.Fatalf("derive IPv6 address from global address: %v", err)
	}
	if got != "2001:db8:3d60:1220::100" {
		t.Fatalf("address = %s, want 2001:db8:3d60:1220::100", got)
	}
}

func TestDeriveIPv6AddressFromDelegatedPrefix(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		subnetID string
		suffix   string
		want     string
	}{
		{
			name:     "documentation /60 first subnet",
			prefix:   "2001:db8:3d60:1220::/60",
			subnetID: "0",
			suffix:   "::1",
			want:     "2001:db8:3d60:1220::1",
		},
		{
			name:     "documentation /60 hex subnet",
			prefix:   "2001:db8:3d60:1220::/60",
			subnetID: "a",
			suffix:   "::3",
			want:     "2001:db8:3d60:122a::3",
		},
		{
			name:     "documentation /56 decimal subnet",
			prefix:   "2001:db8:3d60:1200::/56",
			subnetID: "16",
			suffix:   "::100",
			want:     "2001:db8:3d60:1210::100",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := deriveIPv6AddressFromDelegatedPrefix(tt.prefix, tt.subnetID, tt.suffix)
			if err != nil {
				t.Fatalf("derive IPv6 delegated address: %v", err)
			}
			if got != tt.want {
				t.Fatalf("address = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDelegatedPrefixFromObservedUsesKernelPrefix(t *testing.T) {
	got, ok := delegatedPrefixFromObserved([]string{
		"fe80::/64",
		"2001:db8:3d60:1240::/64",
	}, nil, 60)
	if !ok {
		t.Fatal("delegated prefix not found")
	}
	if got != "2001:db8:3d60:1240::/60" {
		t.Fatalf("prefix = %s, want 2001:db8:3d60:1240::/60", got)
	}
}

func TestDelegatedPrefixFromObservedIgnoresStandaloneAddress(t *testing.T) {
	if got, ok := delegatedPrefixFromObserved(nil, []string{
		"fe80::3",
		"2001:db8:3d60:1240::2",
	}, 60); ok {
		t.Fatalf("prefix = %s, want no prefix from standalone address", got)
	}
}

func TestDelegatedPrefixFromObservedIgnoresHostRoute(t *testing.T) {
	if got, ok := delegatedPrefixFromObserved([]string{
		"2001:db8:3d60:1240::2/128",
	}, nil, 60); ok {
		t.Fatalf("prefix = %s, want no prefix from host route", got)
	}
}

func TestDelegatedPrefixFromAddressEntriesIgnoresManagedSuffixWhenFreshClientAddressExists(t *testing.T) {
	got, ok := delegatedPrefixFromAddressEntries([]ipv6AddressEntry{
		{Address: "fe80::1", PrefixLen: 64},
		{Address: "2001:db8:3d60:1240::1", PrefixLen: 64},
		{Address: "2001:db8:3d60:1220:be24:11ff:fea3:c1f4", PrefixLen: 64},
	}, 60, map[uint64]bool{ipv6HostSuffix64(netip.MustParseAddr("::1")): true})
	if !ok {
		t.Fatal("delegated prefix not found")
	}
	if got != "2001:db8:3d60:1220::/60" {
		t.Fatalf("prefix = %s, want 2001:db8:3d60:1220::/60", got)
	}
}

func TestDelegatedPrefixFromAddressEntriesFallsBackWhenOnlyManagedSuffixExists(t *testing.T) {
	if got, ok := delegatedPrefixFromAddressEntries([]ipv6AddressEntry{
		{Address: "2001:db8:3d60:1230::3", PrefixLen: 64},
	}, 60, map[uint64]bool{ipv6HostSuffix64(netip.MustParseAddr("::3")): true}); ok {
		t.Fatalf("prefix = %s, want no prefix from filtered managed suffix", got)
	}
}

func TestConflictingManagedIPv6Addresses(t *testing.T) {
	got := conflictingManagedIPv6Addresses([]ipv6AddressEntry{
		{Address: "fe80::1", PrefixLen: 64},
		{Address: "2001:db8:3d60:1240::1", PrefixLen: 64},
		{Address: "2001:db8:3d60:1220::1", PrefixLen: 64},
		{Address: "2001:db8:3d60:1220::100", PrefixLen: 128},
	}, "2001:db8:3d60:1220::1", ipv6HostSuffix64(netip.MustParseAddr("::1")))
	if len(got) != 1 || got[0].Address != "2001:db8:3d60:1240::1" {
		t.Fatalf("conflicts = %#v, want stale ::1 only", got)
	}
}

func TestManagedDelegatedIPv6TargetsIncludeDelegatedAddressAndDSLiteSources(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-ipv6"}, Spec: api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", SubnetID: "0", AddressSuffix: "::3"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}, Spec: api.DSLiteTunnelSpec{Interface: "wan", LocalAddressSource: "delegatedAddress", LocalDelegatedAddress: "lan-ipv6", LocalAddressSuffix: "::100"}},
	}}}
	store := routerstate.New()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{CurrentPrefix: "2001:db8:3d60:1220::/60"}), "test")

	targets, err := managedDelegatedIPv6Targets(router, store)
	if err != nil {
		t.Fatalf("managed targets: %v", err)
	}
	for _, want := range []string{"2001:db8:3d60:1220::3", "2001:db8:3d60:1220::100"} {
		if !targets.DesiredByInterface["ens19"][want] {
			t.Fatalf("desired targets = %#v, missing %s", targets.DesiredByInterface, want)
		}
	}
	for _, suffix := range []string{"::3", "::100"} {
		addr := netip.MustParseAddr(suffix)
		if !targets.SuffixesByInterface["ens19"][ipv6HostSuffix64(addr)] {
			t.Fatalf("suffix targets = %#v, missing %s", targets.SuffixesByInterface, suffix)
		}
	}
}

func TestManagedDelegatedIPv6TargetsTrackSuffixWithoutCurrentPrefix(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-ipv6"}, Spec: api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", AddressSuffix: "::3"}},
	}}}
	store := routerstate.New()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{LastPrefix: "2001:db8:3d60:1220::/60"}), "test")

	targets, err := managedDelegatedIPv6Targets(router, store)
	if err != nil {
		t.Fatalf("managed targets: %v", err)
	}
	if len(targets.DesiredByInterface["ens19"]) != 0 {
		t.Fatalf("desired targets = %#v, want none without current prefix", targets.DesiredByInterface)
	}
	if !targets.SuffixesByInterface["ens19"][ipv6HostSuffix64(netip.MustParseAddr("::3"))] {
		t.Fatalf("suffix targets = %#v, want ::3 tracked for stale cleanup", targets.SuffixesByInterface)
	}
}

func TestParseFreeBSDIfconfigIPv6(t *testing.T) {
	prefixes, addrs := parseFreeBSDIfconfigIPv6(`vtnet1: flags=1008843<UP,BROADCAST,RUNNING,SIMPLEX,MULTICAST,LOWER_UP> metric 0 mtu 1500
	inet 192.0.2.1 netmask 0xffffff00 broadcast 192.0.2.255
	inet6 fe80::be24:11ff:fea3:c1f4%vtnet1 prefixlen 64 scopeid 0x2
	inet6 2001:db8:3d60:1240:be24:11ff:fea3:c1f4 prefixlen 64
`)
	if len(prefixes) != 1 || prefixes[0] != "2001:db8:3d60:1240::/64" {
		t.Fatalf("prefixes = %v, want delegated /64", prefixes)
	}
	wantAddrs := []string{"fe80::be24:11ff:fea3:c1f4", "2001:db8:3d60:1240:be24:11ff:fea3:c1f4"}
	if fmt.Sprint(addrs) != fmt.Sprint(wantAddrs) {
		t.Fatalf("addrs = %v, want %v", addrs, wantAddrs)
	}
	_, entries := parseFreeBSDIfconfigIPv6Entries(`vtnet1: flags=1008843<UP,BROADCAST,RUNNING,SIMPLEX,MULTICAST,LOWER_UP> metric 0 mtu 1500
	inet6 2001:db8:3d60:1240::1 prefixlen 64
`)
	if len(entries) != 1 || entries[0].Address != "2001:db8:3d60:1240::1" || entries[0].PrefixLen != 64 {
		t.Fatalf("entries = %#v, want address with prefixlen 64", entries)
	}
}

func TestParseDHCPCDDumpLeasePD(t *testing.T) {
	out := []byte(`reason=BOUND6
interface=ens18
protocol=dhcp6
dhcp6_client_id=00030001020000000103
dhcp6_server_id=00030001020000000001
dhcp6_ia_pd1_iaid=00000001
dhcp6_ia_pd1_t1=7200
dhcp6_ia_pd1_t2=12600
dhcp6_ia_pd1_prefix1_pltime=14400
dhcp6_ia_pd1_prefix1_vltime=14400
dhcp6_ia_pd1_prefix1_length=60
dhcp6_ia_pd1_prefix1=2001:db8:3d60:1220::
`)
	prefix, lease, ok := parseDHCPCDDumpLeasePD(out, 60)
	if !ok {
		t.Fatal("parseDHCPCDDumpLeasePD ok = false, want true")
	}
	if prefix != "2001:db8:3d60:1220::/60" {
		t.Fatalf("prefix = %q, want documentation /60", prefix)
	}
	if lease.T1 != "7200" || lease.T2 != "12600" || lease.PLTime != "14400" || lease.VLTime != "14400" {
		t.Fatalf("lease = %#v", lease)
	}
}

func TestLinuxPDClientUnitNameSanitizesResourceName(t *testing.T) {
	got := linuxPDClientUnitName("wan.pd/default", "dhcpcd")
	if got != "routerd-dhcpcd-wan-pd-default.service" {
		t.Fatalf("unit = %q", got)
	}
}

func TestObserveFreeBSDDHCPv6CIdentityPayload(t *testing.T) {
	payload := freeBSDDHCPv6CDUIDPayload([]byte{
		0x0e, 0x00,
		0x00, 0x01, 0x00, 0x01, 0x31, 0x82, 0x0f, 0x6f, 0x02, 0x00, 0x00, 0x00, 0x01, 0x01,
	})
	if got := colonHex(payload); got != "00:01:00:01:31:82:0f:6f:02:00:00:00:01:01" {
		t.Fatalf("DUID payload = %s", got)
	}
	if got := configuredOrDefaultDHCPv6CIAID("00000001"); got != "1" {
		t.Fatalf("IAID = %s, want decimal conversion", got)
	}
}

func TestAppendPrefixDelegationStateWarnings(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
		Metadata: api.ObjectMeta{Name: "wan-pd"},
		Spec:     api.DHCPv6PrefixDelegationSpec{Interface: "wan"},
	}}}}
	store := routerstate.New()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{
		LastPrefix: "2001:db8:3d60:1240::/60",
	}), "test")
	result := &apply.Result{}
	appendPrefixDelegationStateWarnings(result, router, store)
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "2001:db8:3d60:1240::/60") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestAppendPrefixDelegationStateWarningsWithoutLastPrefix(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
		Metadata: api.ObjectMeta{Name: "wan-pd"},
		Spec:     api.DHCPv6PrefixDelegationSpec{Interface: "wan"},
	}}}}
	store := routerstate.New()
	store.Unset("ipv6PrefixDelegation.wan-pd.currentPrefix", "test")
	result := &apply.Result{}
	appendPrefixDelegationStateWarnings(result, router, store)
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "no delegated prefix has been recorded") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestRecordPrefixDelegationStateUsesManagedDaemonLease(t *testing.T) {
	dir := t.TempDir()
	oldDir := pdClientLeaseDir
	pdClientLeaseDir = dir
	t.Cleanup(func() { pdClientLeaseDir = oldDir })
	leaseDir := filepath.Join(dir, "wan-pd")
	if err := os.MkdirAll(leaseDir, 0755); err != nil {
		t.Fatal(err)
	}
	acquiredAt := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	updatedAt := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339)
	data := []byte(fmt.Sprintf(`{
  "resource": "wan-pd",
  "interface": "ens18",
  "state": "bound",
  "currentPrefix": "2001:db8:1230::/60",
  "serverDUID": "00030001020000000001",
  "iaid": 1,
  "t1Seconds": 7200,
  "t2Seconds": 12600,
  "preferredSeconds": 14400,
  "validSeconds": 14400,
  "acquiredAt": %q,
  "updatedAt": %q
}`, acquiredAt, updatedAt))
	if err := os.WriteFile(filepath.Join(leaseDir, "lease.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "ens18"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
			Metadata: api.ObjectMeta{Name: "wan-pd"},
			Spec:     api.DHCPv6PrefixDelegationSpec{Interface: "wan"},
		},
	}}}
	store := routerstate.New()
	if _, err := recordObservedPrefixDelegationState(router, store); err != nil {
		t.Fatalf("record PD state: %v", err)
	}
	result := &apply.Result{}
	appendPrefixDelegationStateWarnings(result, router, store)
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	lease, ok := routerstate.PDLeaseFromStore(store, "ipv6PrefixDelegation.wan-pd")
	if !ok {
		t.Fatal("lease missing")
	}
	if lease.CurrentPrefix != "2001:db8:1230::/60" || lease.VLTime != "14400" || lease.LastReplyAt != acquiredAt {
		t.Fatalf("lease = %+v", lease)
	}
}

func TestRecordPrefixDelegationStateClearsIdentityWhenClientChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = store.Close() }()
	if _, err := store.BeginGeneration("test"); err != nil {
		t.Fatalf("begin generation: %v", err)
	}
	store.Set("ipv6PrefixDelegation.wan-pd.client", "networkd", "old client")
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{
		LastPrefix: "2001:db8:3d60:1240::/60",
		DUID:       "00030001020000000001",
		DUIDText:   "00:03:00:01:02:00:00:00:00:01",
		IAID:       "3394439514",
	}), "old lease")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "ens18"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
			Metadata: api.ObjectMeta{Name: "wan-pd"},
			Spec: api.DHCPv6PrefixDelegationSpec{
				Interface: "wan",
				Client:    "dhcpcd",
				Profile:   "ntt-hgw-lan-pd",
			},
		},
	}}}
	if _, err := recordObservedPrefixDelegationState(router, store); err != nil {
		t.Fatalf("record PD state: %v", err)
	}
	lease, ok := routerstate.PDLeaseFromStore(store, "ipv6PrefixDelegation.wan-pd")
	if !ok {
		t.Fatal("lease missing")
	}
	if lease.DUID != "" || lease.DUIDText != "" || lease.IAID != "" {
		t.Fatalf("identity was not cleared: %+v", lease)
	}
	if lease.LastPrefix != "2001:db8:3d60:1240::/60" {
		t.Fatalf("last prefix = %q, want preserved", lease.LastPrefix)
	}
	if got := store.Get("ipv6PrefixDelegation.wan-pd.client").Value; got != "dhcpcd" {
		t.Fatalf("client = %q, want dhcpcd", got)
	}
	events := store.Events(api.NetAPIVersion, "DHCPv6PrefixDelegation", "wan-pd", 10)
	found := false
	for _, event := range events {
		if event.Reason == "PDClientChanged" {
			found = true
		}
	}
	if !found {
		t.Fatalf("PDClientChanged event missing: %+v", events)
	}
}

func TestRecordHostInventoryState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := store.BeginGeneration("test"); err != nil {
		t.Fatalf("begin generation: %v", err)
	}
	if err := recordHostInventoryState(store); err != nil {
		t.Fatalf("record inventory: %v", err)
	}
	status := store.ObjectStatus(api.RouterAPIVersion, "Inventory", "host")
	if status == nil || status["os"] == nil || status["commands"] == nil {
		t.Fatalf("inventory status = %#v", status)
	}
	events := store.Events(api.RouterAPIVersion, "Inventory", "host", 10)
	if len(events) != 1 || events[0].Reason != "InventoryObserved" {
		t.Fatalf("events = %#v", events)
	}
	if err := recordHostInventoryState(store); err != nil {
		t.Fatalf("record inventory again: %v", err)
	}
	events = store.Events(api.RouterAPIVersion, "Inventory", "host", 10)
	if len(events) != 1 {
		t.Fatalf("unchanged inventory should not add event: %#v", events)
	}
}

func TestParseRFC4361ClientID(t *testing.T) {
	identity := parseRFC4361ClientID("ff000000010003000102005e102030")
	if identity.IAID != "00000001" {
		t.Fatalf("IAID = %q, want 00000001", identity.IAID)
	}
	if identity.DUID != "0003000102005e102030" {
		t.Fatalf("DUID = %q, want link-layer DUID", identity.DUID)
	}
}

func TestLinkLayerDUIDFromMAC(t *testing.T) {
	got := linkLayerDUIDFromMAC("02:00:5e:10:20:30")
	if got != "0003000102005e102030" {
		t.Fatalf("DUID = %q, want 0003000102005e102030", got)
	}
}

func TestStateWhenRequiresSetAndEqual(t *testing.T) {
	store := routerstate.New()
	when := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
		"wan.ipv6.mode": {Equals: "pd-ready"},
	}}
	if resourceWhenMatches(when, store) {
		t.Fatal("unknown state matched equals")
	}
	store.Unset("wan.ipv6.mode", "observed absent")
	if resourceWhenMatches(when, store) {
		t.Fatal("unset state matched equals")
	}
	store.Set("wan.ipv6.mode", "address-only", "observed fallback")
	if resourceWhenMatches(when, store) {
		t.Fatal("different set value matched equals")
	}
	store.Set("wan.ipv6.mode", "pd-ready", "observed pd")
	if !resourceWhenMatches(when, store) {
		t.Fatal("matching set value did not match equals")
	}
}

func TestFilterDefaultRouteCandidatesByWhen(t *testing.T) {
	store := routerstate.New()
	store.Set("wan.ipv6.mode", "address-only", "test")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{Kind: "IPv4DefaultRoutePolicy"}, Metadata: api.ObjectMeta{Name: "default-v4"}, Spec: api.IPv4DefaultRoutePolicySpec{Candidates: []api.IPv4DefaultRoutePolicyCandidate{
			{Name: "dslite", RouteSet: "dslite", Priority: 10, When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"wan.ipv6.mode": {In: []string{"pd-ready", "address-only"}}}}},
			{Name: "pppoe", Interface: "wan-pppoe", Priority: 20, When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"wan.ipv6.mode": {Equals: "ipv4-only"}}}},
		}}},
	}}}
	filtered := filterRouterByWhen(router, store)
	spec, err := filtered.Spec.Resources[0].IPv4DefaultRoutePolicySpec()
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Candidates) != 1 || spec.Candidates[0].Name != "dslite" {
		t.Fatalf("candidates = %+v, want only dslite", spec.Candidates)
	}
}

func TestSelectAAAAByOrdinal(t *testing.T) {
	values := []string{
		"2404:8e00::feed:100",
		"2404:8e00::feed:101",
		"2404:8e00::feed:102",
	}
	got, err := selectAAAA(values, 2, "ordinal")
	if err != nil {
		t.Fatalf("select AAAA: %v", err)
	}
	if got != "2404:8e00::feed:101" {
		t.Fatalf("AAAA = %s, want 2404:8e00::feed:101", got)
	}
}

func TestSelectAAAAModulo(t *testing.T) {
	values := []string{
		"2404:8e00::feed:100",
		"2404:8e00::feed:101",
	}
	got, err := selectAAAA(values, 3, "ordinalModulo")
	if err != nil {
		t.Fatalf("select AAAA: %v", err)
	}
	if got != "2404:8e00::feed:100" {
		t.Fatalf("AAAA = %s, want 2404:8e00::feed:100", got)
	}
}

func TestWebConsoleResolvesListenAddressFromResourceStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer store.Close()
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "mgmt", map[string]any{
		"phase":         "Up",
		"ipv4Addresses": []string{"192.168.123.129/24"},
	}); err != nil {
		t.Fatalf("save status: %v", err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "WebConsole"},
			Metadata: api.ObjectMeta{Name: "mgmt"},
			Spec: api.WebConsoleSpec{
				ListenAddressFrom: api.StatusValueSourceSpec{Resource: "Interface/mgmt", Field: "ipv4Addresses"},
				Port:              8080,
			},
		},
	}}}
	spec, ok, err := webConsoleFromRouter(router, store)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("web console disabled")
	}
	if spec.ListenAddress != "192.168.123.129" {
		t.Fatalf("listen address = %q", spec.ListenAddress)
	}
}
