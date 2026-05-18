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

func TestControllerRendersIngressServiceActiveBackendFromStatus(t *testing.T) {
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

func TestControllerRendersLocalServiceRedirectWithoutNAT44(t *testing.T) {
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
	if !strings.Contains(got, "list table ip routerd_nat") {
		t.Fatalf("nft command log missing table check:\n%s", got)
	}
	if strings.Contains(got, "-f "+path) {
		t.Fatalf("unchanged existing table should not be reapplied:\n%s", got)
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
