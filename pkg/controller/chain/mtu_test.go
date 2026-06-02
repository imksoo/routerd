// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
	routerstate "github.com/imksoo/routerd/pkg/state"
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

func TestPathMTUControllerRendersDynamicRemoteAddressClaimMSSClamp(t *testing.T) {
	dir := t.TempDir()
	startup := startupHybridContextRouter()
	startup.Spec.Resources = append(startup.Spec.Resources,
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
			Metadata: api.ObjectMeta{Name: "wg-sam"},
			Spec:     api.WireGuardInterfaceSpec{MTU: 1420},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile"},
			Metadata: api.ObjectMeta{Name: "lab-cloud"},
			Spec: api.CloudProviderProfileSpec{
				Provider:     "oci",
				Capabilities: []string{"vnic-secondary-ip"},
				Auth:         api.ProviderAuth{Mode: "external-command", Command: "/bin/true"},
			},
		},
	)
	claim := remoteAddressClaimResource("app", "10.0.1.123/32", "provider-secondary-ip", "ens3")
	claimSpec := claim.Spec.(api.RemoteAddressClaimSpec)
	claimSpec.Capture.ProviderRef = "lab-cloud"
	claimSpec.Capture.ProviderMode = "vnic-secondary-ip"
	claimSpec.Capture.NICRef = "ocid1.vnic.example"
	claim.Spec = claimSpec
	store := &dynamicRouteSAMStore{
		records: []routerstate.DynamicConfigPartRecord{dynamicPartRecord(t, "MobilityPool/cloudedge/node/cloud", []api.Resource{
			addressMobilityDomainResource(),
			claim,
		}, time.Now().Add(time.Hour))},
		objects: map[string]map[string]any{},
	}
	view, err := buildDynamicRouteSAMView(startup, store, time.Now().UTC(), platform.OSLinux)
	if err != nil {
		t.Fatalf("buildDynamicRouteSAMView: %v", err)
	}
	controller := PathMTUController{Router: view.EffectiveRouter, Store: store, DryRun: true, Path: filepath.Join(dir, "mss.nft")}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(controller.Path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"table inet routerd_mss",
		`iifname "ens3" oifname "wg-sam" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1300 tcp option maxseg size set 1300`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dynamic SAM MSS clamp missing %q:\n%s", want, got)
		}
	}
}

func TestPathMTUControllerRendersBGPMobilityMSSClamp(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
			Metadata: api.ObjectMeta{Name: "cloudedge"},
			Spec:     api.EventGroupSpec{NodeName: "oci-router"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
			Metadata: api.ObjectMeta{Name: "wg-hybrid"},
			Spec:     api.WireGuardInterfaceSpec{MTU: 1420},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"},
			Metadata: api.ObjectMeta{Name: "onprem-main"},
			Spec: api.OverlayPeerSpec{
				Role:     "onprem",
				NodeID:   "onprem-router",
				Underlay: api.OverlayUnderlay{Type: "wireguard", Interface: "wg-hybrid"},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
			Metadata: api.ObjectMeta{Name: "cloudedge"},
			Spec: api.MobilityPoolSpec{
				Prefix:         "10.77.60.0/24",
				GroupRef:       "cloudedge",
				DeliveryPolicy: api.MobilityDeliveryPolicy{Mode: "bgp"},
				Members: []api.MobilityPoolMember{
					{
						NodeRef: "onprem-router",
						Site:    "onprem",
						Role:    "onprem",
						Capture: api.MobilityMemberCapture{Type: "proxy-arp", Interface: "ens21"},
						DeliveryTo: []api.MobilityMemberDeliveryTarget{
							{NodeRef: "oci-router", PeerRef: "onprem-main", Mode: "route", TunnelInterface: "wg-hybrid"},
						},
					},
					{
						NodeRef:  "oci-router",
						Site:     "oci",
						Role:     "cloud",
						Capture:  api.MobilityMemberCapture{Type: "provider-secondary-ip", Interface: "ens3", ProviderRef: "oci-lab"},
						Delivery: api.MobilityMemberDelivery{PeerRef: "onprem-main", Mode: "route", TunnelInterface: "wg-hybrid"},
					},
				},
			},
		},
	}}}
	store := mapStore{}
	controller := PathMTUController{Router: router, Store: store, DryRun: true, Path: filepath.Join(dir, "mss.nft")}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(controller.Path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"table inet routerd_mss",
		`iifname "ens3" oifname "wg-hybrid" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1300 tcp option maxseg size set 1300`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("BGP mobility MSS clamp missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `iifname "ens21"`) || strings.Contains(got, "meta nfproto ipv6") {
		t.Fatalf("BGP mobility MSS clamp should be self-member IPv4-only:\n%s", got)
	}
}

func TestPathMTUEffectiveViewEmptyPartsMatchesRawRouter(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}, Spec: api.DSLiteTunnelSpec{TunnelName: "ds-lite-a", MTU: 1454}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"ds-lite-a"}}},
	}}}
	view, err := buildDynamicRouteSAMView(router, &dynamicRouteSAMStore{objects: map[string]map[string]any{}}, time.Now().UTC(), platform.OSLinux)
	if err != nil {
		t.Fatalf("buildDynamicRouteSAMView: %v", err)
	}
	raw, err := render.NftablesTCPMSSClamp(router)
	if err != nil {
		t.Fatalf("raw render: %v", err)
	}
	effective, err := render.NftablesTCPMSSClamp(view.EffectiveRouter)
	if err != nil {
		t.Fatalf("effective render: %v", err)
	}
	if string(effective) != string(raw) {
		t.Fatalf("effective MSS render differs from raw\nraw:\n%s\neffective:\n%s", raw, effective)
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
