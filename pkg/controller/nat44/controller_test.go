// SPDX-License-Identifier: BSD-3-Clause

package nat44

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/nftstate"
	"github.com/imksoo/routerd/pkg/platform"
)

func requireLinuxRuntimeFixture(t *testing.T) {
	t.Helper()
	if platform.CurrentOS() != platform.OSLinux {
		t.Skip("Linux nftables fixture")
	}
}

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
	requireLinuxRuntimeFixture(t)
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

func TestControllerRendersIngressServiceActiveBackendFromStatus(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "api"}, Spec: api.IngressServiceSpec{
			Listen:   api.IngressListenSpec{Interface: "lan", Protocol: "tcp", Port: 6443},
			Backends: []api.IngressBackendSpec{{Name: "cp-01", Address: "10.0.0.11", Port: 6443}, {Name: "cp-02", Address: "10.0.0.12", Port: 6443}},
		}},
	}}}
	store := &testStore{}
	if err := store.SaveObjectStatus(api.FirewallAPIVersion, "IngressService", "api", map[string]any{
		"activeBackend": map[string]any{"name": "cp-02", "address": "10.0.0.12", "port": 6443},
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "nat44.nft")
	controller := Controller{Router: router, Store: store, DryRun: true, NftablesPath: path}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ruleset: %v", err)
	}
	if !strings.Contains(string(data), `dnat to 10.0.0.12:6443`) || strings.Contains(string(data), `dnat to 10.0.0.11:6443`) {
		t.Fatalf("ruleset did not use active backend:\n%s", string(data))
	}
}

func TestControllerRendersIngressServiceDistributedBackendsFromStatus(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "api"}, Spec: api.IngressServiceSpec{
			Listen:   api.IngressListenSpec{Interface: "lan", Protocol: "tcp", Port: 6443},
			Backends: []api.IngressBackendSpec{{Name: "cp-01", Address: "10.0.0.11", Port: 6443}, {Name: "cp-02", Address: "10.0.0.12", Port: 6443}},
			Policy:   api.IngressServicePolicySpec{Selection: "sourceHash"},
		}},
	}}}
	store := &testStore{}
	if err := store.SaveObjectStatus(api.FirewallAPIVersion, "IngressService", "api", map[string]any{
		"effectiveSelection": "sourceHash",
		"activeBackends": []map[string]any{
			{"name": "cp-01", "address": "10.0.0.11", "port": 6443},
			{"name": "cp-02", "address": "10.0.0.12", "port": 6443},
		},
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "nat44.nft")
	controller := Controller{Router: router, Store: store, DryRun: true, NftablesPath: path}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ruleset: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `jhash ip saddr mod 2 vmap`) ||
		!strings.Contains(got, `jump ingress_ingressservice_api_0`) ||
		!strings.Contains(got, `dnat to 10.0.0.12:6443`) {
		t.Fatalf("ruleset did not use distributed active backends:\n%s", got)
	}
}

func TestControllerAppliesIngressRulesWhenNAT44IsDryRun(t *testing.T) {
	requireLinuxRuntimeFixture(t)
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
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-to-wan"}, Spec: api.NAT44RuleSpec{
			Type:            "masquerade",
			EgressInterface: "wan",
			SourceRanges:    []string{"192.168.0.0/16"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "api"}, Spec: api.IngressServiceSpec{
			Listen:   api.IngressListenSpec{Interface: "lan", Protocol: "tcp", Port: 6443},
			Backends: []api.IngressBackendSpec{{Name: "cp-01", Address: "10.0.0.11", Port: 6443}},
		}},
	}}}
	store := &testStore{}
	if err := store.SaveObjectStatus(api.FirewallAPIVersion, "IngressService", "api", map[string]any{
		"activeBackend": map[string]any{"name": "cp-01", "address": "10.0.0.11", "port": 6443},
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "nat44.nft")
	controller := Controller{Router: router, Store: store, DryRun: true, IngressLive: true, NftablesPath: path, NftCommand: nftPath}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ruleset: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `dnat to 10.0.0.11:6443`) {
		t.Fatalf("ruleset missing ingress DNAT:\n%s", got)
	}
	if strings.Contains(got, "masquerade") {
		t.Fatalf("NAT44Rule must stay out of ingress-live ruleset while NAT is dry-run:\n%s", got)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "-f "+path) {
		t.Fatalf("ingress-live reconcile did not apply nftables:\n%s", logData)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "NAT44Rule", "lan-to-wan")
	if status["dryRun"] != true {
		t.Fatalf("NAT44Rule status should remain dry-run: %#v", status)
	}
}

func TestControllerOmitsIngressRulesWhenIngressIsDryRun(t *testing.T) {
	requireLinuxRuntimeFixture(t)
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
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-to-wan"}, Spec: api.NAT44RuleSpec{
			Type:            "masquerade",
			EgressInterface: "wan",
			SourceRanges:    []string{"192.168.0.0/16"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "api"}, Spec: api.IngressServiceSpec{
			Listen:   api.IngressListenSpec{Interface: "lan", Protocol: "tcp", Port: 6443},
			Backends: []api.IngressBackendSpec{{Name: "cp-01", Address: "10.0.0.11", Port: 6443}},
		}},
	}}}
	store := &testStore{}
	if err := store.SaveObjectStatus(api.FirewallAPIVersion, "IngressService", "api", map[string]any{
		"activeBackend": map[string]any{"name": "cp-01", "address": "10.0.0.11", "port": 6443},
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "nat44.nft")
	controller := Controller{Router: router, Store: store, DryRun: false, NftablesPath: path, NftCommand: nftPath}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ruleset: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "masquerade") {
		t.Fatalf("ruleset missing NAT44Rule masquerade:\n%s", got)
	}
	if strings.Contains(got, "dnat to") {
		t.Fatalf("IngressService DNAT must stay out of NAT-live ruleset while ingress is dry-run:\n%s", got)
	}
}

func TestControllerResolvesSNATAddressFromStaticAddress(t *testing.T) {
	requireLinuxRuntimeFixture(t)
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

func TestControllerRendersLocalServiceRedirectWithoutNAT44(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "dns-google"}, Spec: api.IPAddressSetSpec{
			Addresses: []string{"8.8.8.8", "8.8.4.4"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "LocalServiceRedirect"}, Metadata: api.ObjectMeta{Name: "lan-local-services"}, Spec: api.LocalServiceRedirectSpec{
			Interface: "lan",
			Rules: []api.LocalServiceRedirectRuleSpec{{
				Name:              "dns-google",
				Protocols:         []string{"udp", "tcp"},
				DestinationSetRef: "IPAddressSet/dns-google",
				DestinationPort:   53,
				RedirectPort:      53,
			}},
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
	got := string(data)
	for _, want := range []string{
		`set ip_address_set_dns_google { type ipv4_addr; elements = { 8.8.4.4, 8.8.8.8 }; }`,
		`type nat hook prerouting priority dstnat; policy accept;`,
		`iifname "ens19" ip daddr @ip_address_set_dns_google tcp dport 53 counter redirect to :53 comment "routerd LocalServiceRedirect lan-local-services dns-google"`,
		`iifname "ens19" ip daddr @ip_address_set_dns_google udp dport 53 counter redirect to :53 comment "routerd LocalServiceRedirect lan-local-services dns-google"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ruleset missing %q:\n%s", want, got)
		}
	}
}

func TestControllerRendersFQDNAddressSetsWithoutResolvingNames(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "dns-google"}, Spec: api.IPAddressSetSpec{
			Names: []string{"dns.google"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "LocalServiceRedirect"}, Metadata: api.ObjectMeta{Name: "lan-local-services"}, Spec: api.LocalServiceRedirectSpec{
			Interface: "lan",
			Rules: []api.LocalServiceRedirectRuleSpec{{
				Name:              "dns-google",
				Protocols:         []string{"udp", "tcp"},
				DestinationSetRef: "IPAddressSet/dns-google",
				DestinationPort:   53,
				RedirectPort:      53,
			}},
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
	got := string(data)
	for _, want := range []string{
		`table ip routerd_nat`,
		`set ip_address_set_dns_google { type ipv4_addr; }`,
		`iifname "ens19" ip daddr @ip_address_set_dns_google tcp dport 53 counter redirect to :53 comment "routerd LocalServiceRedirect lan-local-services dns-google"`,
		`table ip6 routerd_nat`,
		`set ip_address_set_dns_google { type ipv6_addr; }`,
		`iifname "ens19" ip6 daddr @ip_address_set_dns_google tcp dport 53 counter redirect to :53 comment "routerd LocalServiceRedirect lan-local-services dns-google"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ruleset missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "dns.google") {
		t.Fatalf("runtime ruleset should not embed resolved FQDN names:\n%s", got)
	}
}

func TestControllerRendersNAT44DestinationAddressSet(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "cloud-service"}, Spec: api.IPAddressSetSpec{
			Names: []string{"service.example.test"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-to-cloud"}, Spec: api.NAT44RuleSpec{
			Type:               "masquerade",
			EgressInterface:    "wan",
			SourceRanges:       []string{"172.18.0.0/16"},
			DestinationSetRefs: []string{"IPAddressSet/cloud-service"},
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
	got := string(data)
	for _, want := range []string{
		`set ip_address_set_cloud_service { type ipv4_addr; }`,
		`oifname "wan" ip saddr 172.18.0.0/16 ip daddr @ip_address_set_cloud_service masquerade`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ruleset missing %q:\n%s", want, got)
		}
	}
}

func TestControllerSkipsUnchangedExistingNftablesTable(t *testing.T) {
	requireLinuxRuntimeFixture(t)
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
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-to-wan"}, Spec: api.NAT44RuleSpec{
			Type:            "masquerade",
			EgressInterface: "wan",
			SourceRanges:    []string{"192.168.0.0/16"},
		}},
	}}}
	store := &testStore{}
	path := filepath.Join(dir, "nat44.nft")
	controller := Controller{Router: router, Bus: bus.New(), Store: store, NftablesPath: path, NftCommand: nftPath}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if err := os.WriteFile(logPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(logData)
	if strings.Contains(got, "list table ip routerd_nat") {
		t.Fatalf("recently verified unchanged table should not be checked:\n%s", got)
	}
	if strings.Contains(got, "-f "+path) {
		t.Fatalf("unchanged existing table should not be reapplied:\n%s", got)
	}
}

func TestControllerRetriesFailedNftablesChange(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	for _, stage := range []string{"-c", "-f"} {
		t.Run(stage, func(t *testing.T) {
			nft, logPath, failPath := nftRecoveryFixture(t)
			router := natRecoveryRouter("192.168.0.0/16")
			store := &testStore{}
			path := filepath.Join(t.TempDir(), "nat44.nft")
			controller := Controller{Router: router, Store: store, NftablesPath: path, NftCommand: nft}
			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			controller.Router = natRecoveryRouter("10.0.0.0/8")
			if err := os.WriteFile(failPath, []byte(stage), 0644); err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				if err := os.WriteFile(logPath, nil, 0644); err != nil {
					t.Fatal(err)
				}
				if err := controller.Reconcile(context.Background()); err == nil {
					t.Fatal("failed nft transaction was acknowledged")
				}
				calls, err := os.ReadFile(logPath)
				expected := "-f " + path
				if stage == "-c" {
					expected = "-c " + expected
				}
				if err != nil || !strings.Contains("\n"+string(calls), "\n"+expected+"\n") {
					t.Fatalf("attempt %d skipped nft retry: %s (%v)", attempt, calls, err)
				}
				if nftstate.RecentlyVerified(path, time.Now()) {
					t.Fatal("failed effect retained acknowledgement")
				}
			}
			if err := os.Remove(failPath); err != nil {
				t.Fatal(err)
			}
			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			status := store.ObjectStatus(api.NetAPIVersion, "NAT44Rule", "lan-to-wan")
			if !nftstate.RecentlyVerified(path, time.Now()) || status["phase"] != "Active" {
				t.Fatalf("successful retry not acknowledged: %#v", status)
			}
		})
	}
}

func TestControllerAppliesRulesPreviouslyWrittenByDryRun(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	nft, logPath, _ := nftRecoveryFixture(t)
	path := filepath.Join(t.TempDir(), "nat44.nft")
	controller := Controller{Router: natRecoveryRouter("192.168.0.0/16"), Store: &testStore{}, NftablesPath: path, NftCommand: nft}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	controller.DryRun = true
	controller.Router = natRecoveryRouter("10.0.0.0/8")
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	controller.DryRun = false
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(calls), "\n-f "+path) {
		t.Fatalf("live reconcile skipped dry-run file: %s (%v)", calls, err)
	}
}

func TestControllerWithdrawsIngressWhenNoBackendIsActive(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	nft, logPath, failPath := nftRecoveryFixture(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "api"}, Spec: api.IngressServiceSpec{
			Listen: api.IngressListenSpec{Interface: "lan", Protocol: "tcp", Port: 6443}, Backends: []api.IngressBackendSpec{{Name: "cp-01", Address: "10.0.0.11", Port: 6443}},
		}},
	}}}
	store := &testStore{}
	if err := store.SaveObjectStatus(api.FirewallAPIVersion, "IngressService", "api", map[string]any{"activeBackend": map[string]any{"name": "cp-01", "address": "10.0.0.11", "port": 6443}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "nat44.nft")
	controller := Controller{Router: router, Store: store, DryRun: true, IngressLive: true, NftablesPath: path, NftCommand: nft}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.FirewallAPIVersion, "IngressService", "api", map[string]any{"phase": "NoHealthyBackends", "activeBackend": map[string]any{"name": "", "address": "", "port": 0}}); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	blocked, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(blocked), "dnat to") || !strings.Contains(string(blocked), "counter drop") {
		t.Fatalf("unhealthy declared backend was not blocked: %s (%v)", blocked, err)
	}
	controller.Router = &api.Router{}
	if err := os.WriteFile(failPath, []byte("delete"), 0644); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := controller.Reconcile(context.Background()); err == nil {
			t.Fatal("failed table withdrawal was acknowledged")
		}
		if nftstate.RecentlyVerified(path, time.Now()) {
			t.Fatal("failed withdrawal retained acknowledgement")
		}
	}
	if err := os.Remove(failPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) != 0 {
		t.Fatalf("unhealthy declared backend retained forwarding: %q (%v)", data, err)
	}
	calls, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(calls), "delete table ip routerd_nat") || !strings.Contains(string(calls), "delete table ip6 routerd_nat") {
		t.Fatalf("empty NAT did not withdraw both tables: %s (%v)", calls, err)
	}
	if !nftstate.RecentlyVerified(path, time.Now()) {
		t.Fatal("successful withdrawal was not acknowledged")
	}
}

func TestControllerAcknowledgesAlreadyAbsentNATTables(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	dir := t.TempDir()
	nft := filepath.Join(dir, "nft")
	if err := os.WriteFile(nft, []byte("#!/bin/sh\necho 'Error: No such file or directory' >&2\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "nat44.nft")
	controller := Controller{Router: &api.Router{}, Store: &testStore{}, NftablesPath: path, NftCommand: nft}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !nftstate.RecentlyVerified(path, time.Now()) {
		t.Fatal("absent NAT tables were not acknowledged")
	}
}

func TestControllerPFRetriesFailedAnchorWithdrawal(t *testing.T) {
	for _, stage := range []string{"-n", "-a"} {
		t.Run(stage, func(t *testing.T) {
			pfctl, logPath, failPath := nftRecoveryFixture(t)
			path := filepath.Join(t.TempDir(), "nat44.pf")
			controller := Controller{Router: &api.Router{}, Store: &testStore{}, NftablesPath: path, NftCommand: pfctl, PFDevicePath: pfctl}
			if err := os.WriteFile(failPath, []byte(stage), 0644); err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				if err := controller.reconcilePF(context.Background(), controller.Router, nil, nil); err == nil {
					t.Fatal("failed PF withdrawal was acknowledged")
				}
			}
			if err := os.Remove(failPath); err != nil {
				t.Fatal(err)
			}
			if err := controller.reconcilePF(context.Background(), controller.Router, nil, nil); err != nil {
				t.Fatal(err)
			}
			calls, err := os.ReadFile(logPath)
			if err != nil || strings.Count(string(calls), stage+" ") < 3 || !strings.Contains(string(calls), "-a routerd_nat -f "+path) {
				t.Fatalf("PF did not retry validation/load of empty anchor: %s (%v)", calls, err)
			}
		})
	}
}

func TestControllerPFAppliesBackendHealthAndEmptyAnchor(t *testing.T) {
	pfctl, logPath, _ := nftRecoveryFixture(t)
	path := filepath.Join(t.TempDir(), "nat44.pf")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "em0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "api"}, Spec: api.IngressServiceSpec{
			Listen:   api.IngressListenSpec{Interface: "wan", Address: "192.0.2.10", Protocol: "tcp", Port: 6443},
			Backends: []api.IngressBackendSpec{{Name: "declared", Address: "10.0.0.11", Port: 6443}},
			Policy:   api.IngressServicePolicySpec{OnNoHealthyBackends: "reject"},
		}},
	}}}
	store := &testStore{}
	if err := store.SaveObjectStatus(api.FirewallAPIVersion, "IngressService", "api", map[string]any{"activeBackend": map[string]any{"name": "selected", "address": "10.0.0.12", "port": 6443}}); err != nil {
		t.Fatal(err)
	}
	controller := Controller{Router: router, Store: store, DryRun: true, IngressLive: true, NftCommand: pfctl, NftablesPath: path, PFDevicePath: pfctl}
	if err := controller.reconcilePF(context.Background(), router, nil, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(data), "10.0.0.11") || !strings.Contains(string(data), "-> 10.0.0.12 port 6443") {
		t.Fatalf("PF ignored selected backend: %s (%v)", data, err)
	}
	if err := store.SaveObjectStatus(api.FirewallAPIVersion, "IngressService", "api", map[string]any{"phase": "NoHealthyBackends"}); err != nil {
		t.Fatal(err)
	}
	if err := controller.reconcilePF(context.Background(), router, nil, nil); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || strings.Contains(string(data), "rdr pass") || !strings.Contains(string(data), `block return-rst in quick on "em0" inet proto tcp from any to 192.0.2.10 port 6443`) {
		t.Fatalf("PF ignored unavailable backend policy: %s (%v)", data, err)
	}
	if err := controller.reconcilePF(context.Background(), &api.Router{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || strings.Contains(string(data), "rdr ") || strings.Contains(string(data), "block ") {
		t.Fatalf("empty PF anchor retained old rules: %s (%v)", data, err)
	}
	calls, err := os.ReadFile(logPath)
	if err != nil || strings.Count(string(calls), "\n-a routerd_nat -f "+path) != 3 {
		t.Fatalf("PF did not load all three desired transitions: %s (%v)", calls, err)
	}
}

func natRecoveryRouter(source string) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-to-wan"}, Spec: api.NAT44RuleSpec{
			Type: "masquerade", EgressInterface: "wan", SourceRanges: []string{source},
		}},
	}}}
}

func nftRecoveryFixture(t *testing.T) (nft, logPath, failPath string) {
	t.Helper()
	dir := t.TempDir()
	nft, logPath, failPath = filepath.Join(dir, "nft"), filepath.Join(dir, "nft.log"), filepath.Join(dir, "fail")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + testShellQuote(logPath) + "\n" +
		"if [ -f " + testShellQuote(failPath) + " ] && [ \"$1\" = \"$(cat " + testShellQuote(failPath) + ")\" ]; then echo 'transaction rejected' >&2; exit 1; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(nft, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return nft, logPath, failPath
}

func TestControllerResolvesPPPoEEgressInterface(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"}, Metadata: api.ObjectMeta{Name: "pppoe-flets"}, Spec: api.PPPoESessionSpec{Interface: "wan", IfName: "ppp-flets", Username: "open@open.ad.jp"}},
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
	requireLinuxRuntimeFixture(t)
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
	controller := Controller{Router: router, Store: store, NftCommand: nftPath, NftablesPath: filepath.Join(dir, "nat44.nft")}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(logData), "delete table ip routerd_nat") || !strings.Contains(string(logData), "delete table ip6 routerd_nat") {
		t.Fatalf("unresolved egress retained stale NAT rules: %s (%v)", logData, err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "NAT44Rule", "lan-to-missing")
	if status["phase"] != "Pending" {
		t.Fatalf("status = %#v", status)
	}
}

func testShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// A clean FreeBSD boot has no /dev/pf until the module is loaded. Empty
// desired state must work without loading PF, while an existing device still
// requires withdrawal and a nonempty desired anchor must report apply errors.
func TestControllerPFDeviceAbsence(t *testing.T) {
	for _, tc := range []struct {
		name      string
		device    bool
		desired   bool
		wantError bool
	}{
		{name: "empty absent"},
		{name: "empty present", device: true, wantError: true},
		{name: "desired absent", desired: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			device := filepath.Join(dir, "pf")
			if tc.device {
				if err := os.WriteFile(device, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			pfctl, logPath, failPath := nftRecoveryFixture(t)
			if err := os.WriteFile(failPath, []byte("-a"), 0600); err != nil {
				t.Fatal(err)
			}
			router := &api.Router{}
			if tc.desired {
				router = natRecoveryRouter("192.0.2.0/24")
			}
			c := Controller{OS: platform.OSFreeBSD, Router: router, Store: &testStore{}, NftCommand: pfctl, NftablesPath: filepath.Join(dir, "out", "nat44.pf"), PFDevicePath: device}
			err := c.Reconcile(t.Context())
			if (err != nil) != tc.wantError {
				t.Fatalf("reconcile error=%v, wantError=%v", err, tc.wantError)
			}
			calls, _ := os.ReadFile(logPath)
			if !tc.device && !tc.desired {
				if len(calls) != 0 {
					t.Fatalf("empty absent PF invoked command: %s", calls)
				}
				if _, err := os.Stat(filepath.Dir(c.NftablesPath)); !os.IsNotExist(err) {
					t.Fatalf("empty absent PF wrote artifacts: %v", err)
				}
			} else if !strings.Contains(string(calls), "-a routerd_nat -f ") {
				t.Fatalf("required PF apply/withdrawal not attempted: %s", calls)
			}
		})
	}
}

func TestControllerPFUnknownDeviceIsNotAbsence(t *testing.T) {
	pfctl, logPath, _ := nftRecoveryFixture(t)
	c := Controller{OS: platform.OSFreeBSD, Router: &api.Router{}, Store: &testStore{}, NftCommand: pfctl, NftablesPath: filepath.Join(t.TempDir(), "nat44.pf"), PFDevicePath: filepath.Join(pfctl, "not-a-directory")}
	if err := c.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "observe PF device") {
		t.Fatalf("unknown device state accepted: %v", err)
	}
	if calls, _ := os.ReadFile(logPath); len(calls) != 0 {
		t.Fatalf("unknown device state invoked PF: %s", calls)
	}
}
