// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestDynamicRouteSAMViewEmptyPartsMatchesStaticExpansion(t *testing.T) {
	startup := staticSAMRouter("10.0.1.123/32", "proxy-arp", "lan0")
	store := &dynamicRouteSAMStore{objects: map[string]map[string]any{}}
	view, err := buildDynamicRouteSAMView(startup, store, time.Now().UTC(), platform.OSLinux)
	if err != nil {
		t.Fatalf("buildDynamicRouteSAMView: %v", err)
	}
	if countResources(view.EffectiveRouter, api.HybridAPIVersion, "RemoteAddressClaim") != 1 {
		t.Fatalf("effective claims = %d, want 1", countResources(view.EffectiveRouter, api.HybridAPIVersion, "RemoteAddressClaim"))
	}
	if countResources(view.RouteRouter, api.NetAPIVersion, "IPv4Route") != 1 {
		t.Fatalf("route IPv4Routes = %d, want static SAM delivery route", countResources(view.RouteRouter, api.NetAPIVersion, "IPv4Route"))
	}
	if len(view.SAMLowerings) != 1 || view.SAMLowerings[0].AddressCIDR != "10.0.1.123/32" {
		t.Fatalf("SAM lowerings = %+v, want one static lowering", view.SAMLowerings)
	}
}

func TestDynamicRouteSAMViewIncludesDynamicRemoteAddressClaim(t *testing.T) {
	startup := startupHybridContextRouter()
	claim := remoteAddressClaimResource("app", "10.0.1.123/32", "proxy-arp", "lan0")
	store := &dynamicRouteSAMStore{
		records: []routerstate.DynamicConfigPartRecord{dynamicPartRecord(t, "MobilityPool/cloudedge/node/onprem", []api.Resource{
			addressMobilityDomainResource(),
			claim,
		}, time.Now().Add(time.Hour))},
		objects: map[string]map[string]any{},
	}
	view, err := buildDynamicRouteSAMView(startup, store, time.Now().UTC(), platform.OSLinux)
	if err != nil {
		t.Fatalf("buildDynamicRouteSAMView: %v", err)
	}
	if countResources(view.EffectiveRouter, api.HybridAPIVersion, "RemoteAddressClaim") != 1 {
		t.Fatalf("effective claims = %d, want dynamic claim", countResources(view.EffectiveRouter, api.HybridAPIVersion, "RemoteAddressClaim"))
	}
	if countResources(view.RouteRouter, api.NetAPIVersion, "IPv4Route") != 1 {
		t.Fatalf("route IPv4Routes = %d, want dynamic SAM delivery route", countResources(view.RouteRouter, api.NetAPIVersion, "IPv4Route"))
	}
	if len(view.SAMLowerings) != 1 || view.SAMLowerings[0].ClaimName != "app" {
		t.Fatalf("SAM lowerings = %+v, want dynamic claim lowering", view.SAMLowerings)
	}
}

func TestDynamicControllerRouterIncludesDynamicTunnelAndBGPPeer(t *testing.T) {
	now := time.Now().UTC()
	startup := startupHybridContextRouter()
	startup.Spec.Resources = append(startup.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
		Metadata: api.ObjectMeta{Name: "sam"},
		Spec:     api.BGPRouterSpec{ASN: 64512, RouterID: "10.255.0.1"},
	})
	dynamicResources := []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
			Metadata: api.ObjectMeta{Name: "sam-core-a"},
			Spec: api.TunnelInterfaceSpec{
				Mode:              "ipip",
				Local:             "10.252.0.1",
				Remote:            "10.252.0.2",
				Address:           "10.255.1.0/31",
				UnderlayInterface: "wg-svnet1",
				TrustedUnderlay:   true,
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: "sam-core-a"},
			Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/sam",
				PeerASN:   64512,
				Peers:     []string{"10.255.1.1"},
			},
		},
	}
	store := &dynamicRouteSAMStore{
		records: []routerstate.DynamicConfigPartRecord{
			dynamicPartRecord(t, "SAMTransportProfile/svnet1-core/node/pve-rt01", dynamicResources, now.Add(time.Hour)),
		},
		objects: map[string]map[string]any{},
	}
	runner := &Runner{Router: startup, Store: store}
	staticRouter, err := runner.effectiveRouterForReconcile(eventedStore{Store: store})
	if err != nil {
		t.Fatalf("effectiveRouterForReconcile: %v", err)
	}
	if countResources(staticRouter, api.HybridAPIVersion, "TunnelInterface") != 0 || countResources(staticRouter, api.NetAPIVersion, "BGPPeer") != 0 {
		t.Fatalf("static effective router unexpectedly contains dynamic resources")
	}

	dynamicRouter, err := runner.effectiveDynamicRouterForReconcile(eventedStore{Store: store}, now, platform.OSLinux)
	if err != nil {
		t.Fatalf("effectiveDynamicRouterForReconcile: %v", err)
	}
	tunnel := resourceByName(t, dynamicRouter, api.HybridAPIVersion, "TunnelInterface", "sam-core-a")
	tunnelSpec, err := tunnel.TunnelInterfaceSpec()
	if err != nil {
		t.Fatalf("TunnelInterface spec: %v", err)
	}
	if tunnelSpec.Mode != "ipip" || tunnelSpec.Address != "10.255.1.0/31" || tunnelSpec.UnderlayInterface != "wg-svnet1" {
		t.Fatalf("dynamic TunnelInterface spec = %#v", tunnelSpec)
	}
	peer := resourceByName(t, dynamicRouter, api.NetAPIVersion, "BGPPeer", "sam-core-a")
	peerSpec, err := peer.BGPPeerSpec()
	if err != nil {
		t.Fatalf("BGPPeer spec: %v", err)
	}
	if peerSpec.RouterRef != "BGPRouter/sam" || !reflect.DeepEqual(peerSpec.Peers, []string{"10.255.1.1"}) {
		t.Fatalf("dynamic BGPPeer spec = %#v", peerSpec)
	}
}

func TestDynamicRouteSAMViewSuppressesMobilityClaimsWhenPoolUsesBGPDelivery(t *testing.T) {
	startup := startupHybridContextRouter()
	startup.Spec.Resources = append(startup.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
		Metadata: api.ObjectMeta{Name: "cloudedge"},
		Spec: api.MobilityPoolSpec{
			Prefix:         "10.0.1.0/24",
			GroupRef:       "cloudedge",
			DeliveryPolicy: api.MobilityDeliveryPolicy{Mode: "bgp"},
			Members: []api.MobilityPoolMember{
				{NodeRef: "onprem-router", Site: "onprem", Role: "onprem"},
				{NodeRef: "cloud-router", Site: "cloud", Role: "cloud"},
			},
		},
	})
	claim := remoteAddressClaimResource("app", "10.0.1.123/32", "proxy-arp", "lan0")
	claim.Metadata.Annotations = map[string]string{"mobility.routerd.net/pool": "cloudedge"}
	store := &dynamicRouteSAMStore{
		records: []routerstate.DynamicConfigPartRecord{dynamicPartRecord(t, "MobilityPool/cloudedge/node/onprem", []api.Resource{
			addressMobilityDomainResource(),
			claim,
		}, time.Now().Add(time.Hour))},
		objects: map[string]map[string]any{},
	}
	view, err := buildDynamicRouteSAMView(startup, store, time.Now().UTC(), platform.OSLinux)
	if err != nil {
		t.Fatalf("buildDynamicRouteSAMView: %v", err)
	}
	if countResources(view.EffectiveRouter, api.HybridAPIVersion, "RemoteAddressClaim") != 1 {
		t.Fatalf("effective claims = %d, want generated claim visible in effective config", countResources(view.EffectiveRouter, api.HybridAPIVersion, "RemoteAddressClaim"))
	}
	if countResources(view.RouteRouter, api.NetAPIVersion, "IPv4Route") != 0 || len(view.SAMLowerings) != 0 {
		t.Fatalf("route IPv4Routes/lowerings = %d/%d, want BGP delivery suppressing SAM lowering", countResources(view.RouteRouter, api.NetAPIVersion, "IPv4Route"), len(view.SAMLowerings))
	}
}

func TestDynamicRouteSAMViewDerivesBGPProxyARPClaimsWithoutRouteLowering(t *testing.T) {
	startup := startupHybridContextRouter()
	startup.Spec.Resources = append(startup.Spec.Resources,
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
			Metadata: api.ObjectMeta{Name: "cloudedge"},
			Spec:     api.EventGroupSpec{NodeName: "onprem-router"},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
			Metadata: api.ObjectMeta{Name: "mobility-bgp"},
			Spec:     api.BGPRouterSpec{ASN: 64577, RouterID: "10.99.0.1"},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "onprem-vip"},
			Spec:     api.VirtualAddressSpec{Family: "ipv4", Interface: "lan0", Address: "10.0.1.1/32", Mode: "vrrp", VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 40, Peers: []string{"10.0.1.2"}}},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
			Metadata: api.ObjectMeta{Name: "cloudedge"},
			Spec: api.MobilityPoolSpec{
				Prefix:         "10.0.1.0/24",
				GroupRef:       "cloudedge",
				DeliveryPolicy: api.MobilityDeliveryPolicy{Mode: "bgp"},
				Members: []api.MobilityPoolMember{
					{
						NodeRef:              "onprem-router",
						Site:                 "onprem",
						Role:                 "onprem",
						StaticOwnedAddresses: []string{"10.0.1.10/32"},
						Capture: api.MobilityMemberCapture{
							Type:          "proxy-arp",
							Interface:     "lan0",
							GratuitousARP: false,
							ActiveWhen:    api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"},
						},
					},
					{NodeRef: "aws-router", Site: "aws", Role: "cloud", Capture: api.MobilityMemberCapture{Type: "provider-secondary-ip", Interface: "ens5"}},
				},
			},
		},
	)
	store := mapStore{
		api.NetAPIVersion + "/BGPRouter/mobility-bgp": {
			"installedNextHops": map[string]any{
				"10.0.1.10/32": []any{"10.99.0.1"},
				"10.0.1.11/32": []any{"10.99.0.2"},
				"10.0.1.12/32": []any{"10.99.0.3"},
			},
		},
		api.NetAPIVersion + "/VirtualAddress/onprem-vip": {"role": "master"},
	}
	view, err := buildDynamicRouteSAMView(startup, store, time.Now().UTC(), platform.OSLinux)
	if err != nil {
		t.Fatalf("buildDynamicRouteSAMView: %v", err)
	}
	if got := countResources(view.EffectiveRouter, api.HybridAPIVersion, "RemoteAddressClaim"); got != 2 {
		t.Fatalf("effective BGP proxy claims = %d, want 2", got)
	}
	if got := countResources(view.RouteRouter, api.NetAPIVersion, "IPv4Route"); got != 1 {
		t.Fatalf("BGP proxy claims route IPv4Routes = %d, want capture prefix route only", got)
	}
	if len(view.SAMLowerings) != 0 {
		t.Fatalf("BGP proxy claims produced SAM lowerings: %#v", view.SAMLowerings)
	}
	route := ipv4RouteSpecByName(t, view.RouteRouter, "sam-cloudedge-capture-prefix")
	if route.Destination != "10.0.1.0/24" || route.Device != "lan0" || route.Metric != 90 {
		t.Fatalf("capture prefix route = %#v", route)
	}
	applier := &fakeSAMApplier{}
	garp := &fakeSAMGARP{}
	controller := SAMController{Router: view.EffectiveRouter, Store: store, Lowerings: view.SAMLowerings, OS: platform.OSLinux, Applier: applier, GARP: garp}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("SAM reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{
		"proxyarp:lan0=1",
		"ensure:10.0.1.11/32@lan0",
		"ensure:10.0.1.12/32@lan0",
	})
}

func TestBGPMobilityClaimPreservesProviderSecondaryOSCaptureIntent(t *testing.T) {
	claim := bgpMobilityProxyARPClaim("cloudedge", api.MobilityPoolMember{
		NodeRef: "aws-router",
		Site:    "aws",
		Role:    "cloud",
		Capture: api.MobilityMemberCapture{
			Type:               "provider-secondary-ip",
			ProviderRef:        "aws-lab",
			ProviderMode:       "eni-secondary-ip",
			CaptureStrategy:    "provider-secondary-ip",
			NICRef:             "eni-a",
			ConfigureOSAddress: true,
			Interface:          "ens5",
		},
	}, "10.0.1.11/32")
	spec, err := claim.RemoteAddressClaimSpec()
	if err != nil {
		t.Fatalf("RemoteAddressClaimSpec: %v", err)
	}
	if spec.Capture.Type != "provider-secondary-ip" || !spec.Capture.ConfigureOSAddress || spec.Capture.Interface != "ens5" {
		t.Fatalf("capture = %#v, want provider-secondary-ip with configureOSAddress on ens5", spec.Capture)
	}
	if spec.Capture.ProviderRef != "aws-lab" || spec.Capture.ProviderMode != "eni-secondary-ip" || spec.Capture.NICRef != "eni-a" {
		t.Fatalf("provider capture fields = %#v", spec.Capture)
	}
}

func TestDynamicRouteSAMViewDerivesProviderSecondaryBGPClaimForOSCapture(t *testing.T) {
	startup := bgpProxyARPStartup(false)
	for i, resource := range startup.Spec.Resources {
		switch {
		case resource.APIVersion == api.FederationAPIVersion && resource.Kind == "EventGroup":
			spec := resource.Spec.(api.EventGroupSpec)
			spec.NodeName = "aws-router"
			startup.Spec.Resources[i].Spec = spec
		case resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "MobilityPool":
			spec := resource.Spec.(api.MobilityPoolSpec)
			spec.Members[1].Capture.ConfigureOSAddress = true
			spec.Members[1].Capture.ProviderRef = "aws-lab"
			spec.Members[1].Capture.NICRef = "eni-a"
			startup.Spec.Resources[i].Spec = spec
		}
	}
	store := actionMapStore{
		mapStore: mapStore{
			api.NetAPIVersion + "/BGPRouter/mobility-bgp": {
				"installedNextHops": map[string]any{"10.0.1.11/32": []any{"10.99.0.2"}},
			},
		},
		actions: []routerstate.ActionExecutionRecord{
			samSucceededAssignAction("aws-lab", "eni-a", "10.0.1.11/32"),
		},
	}
	view, err := buildDynamicRouteSAMView(startup, store, time.Now().UTC(), platform.OSLinux)
	if err != nil {
		t.Fatalf("buildDynamicRouteSAMView: %v", err)
	}
	claims := remoteAddressClaimSpecs(view.EffectiveRouter)
	if len(claims) != 1 {
		t.Fatalf("effective claims = %#v, want one provider-secondary-ip claim", claims)
	}
	if claims[0].Address != "10.0.1.11/32" || claims[0].Capture.Type != "provider-secondary-ip" || !claims[0].Capture.ConfigureOSAddress || claims[0].Capture.Interface != "ens5" {
		t.Fatalf("claim = %#v, want provider-secondary-ip OS capture on ens5", claims[0])
	}
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: view.EffectiveRouter, Store: store, Lowerings: view.SAMLowerings, OS: platform.OSLinux, Applier: applier}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("SAM reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{"assign:10.0.1.11/32@ens5"})
}

func TestDynamicRouteSAMViewProviderSecondaryBGPClaimInstallsSAMForwardPath(t *testing.T) {
	startup := bgpProxyARPStartup(false)
	startup.Spec.Resources = append(startup.Spec.Resources,
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
			Metadata: api.ObjectMeta{
				Name: "samt-onprem-aws",
				OwnerRefs: []api.OwnerRef{{
					APIVersion: api.MobilityAPIVersion,
					Kind:       "SAMTransportProfile",
					Name:       "cloudedge-transport",
				}},
			},
			Spec: api.TunnelInterfaceSpec{Mode: "ipip", Local: "10.255.0.1", Remote: "10.255.0.2", Address: "10.255.1.0/31"},
		},
	)
	for i, resource := range startup.Spec.Resources {
		switch {
		case resource.APIVersion == api.FederationAPIVersion && resource.Kind == "EventGroup":
			spec := resource.Spec.(api.EventGroupSpec)
			spec.NodeName = "aws-router"
			startup.Spec.Resources[i].Spec = spec
		case resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "MobilityPool":
			spec := resource.Spec.(api.MobilityPoolSpec)
			spec.Members[1].Capture.ConfigureOSAddress = true
			spec.Members[1].Capture.ProviderRef = "aws-lab"
			spec.Members[1].Capture.ProviderMode = "eni-secondary-ip"
			spec.Members[1].Capture.NICRef = "eni-a"
			startup.Spec.Resources[i].Spec = spec
		}
	}
	store := actionMapStore{
		mapStore: mapStore{
			api.MobilityAPIVersion + "/MobilityPool/cloudedge": {
				"ownershipResolverDecisions": []any{
					map[string]any{
						"address":           "10.0.1.44/32",
						"class":             "ConfirmedCapture",
						"captureHolderNode": "aws-router",
						"captureState":      "Confirmed",
					},
				},
			},
		},
		actions: []routerstate.ActionExecutionRecord{
			samSucceededAssignAction("aws-lab", "eni-a", "10.0.1.44/32"),
		},
	}
	view, err := buildDynamicRouteSAMView(startup, store, time.Now().UTC(), platform.OSLinux)
	if err != nil {
		t.Fatalf("buildDynamicRouteSAMView: %v", err)
	}
	claims := remoteAddressClaimSpecs(view.EffectiveRouter)
	if len(claims) != 1 || claims[0].Address != "10.0.1.44/32" || claims[0].Delivery.Mode != "bgp" {
		t.Fatalf("effective BGP provider claim = %#v, want one BGP claim for confirmed capture", claims)
	}
	if len(view.SAMLowerings) != 0 {
		t.Fatalf("BGP delivery must not produce route lowerings: %#v", view.SAMLowerings)
	}
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: view.EffectiveRouter, Store: store, Lowerings: view.SAMLowerings, OS: platform.OSLinux, Applier: applier}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("SAM reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{
		"assign:10.0.1.44/32@ens5",
		"forward-path:ens5<->samt-onprem-aws",
	})
	status := store.ObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", "bgp-cloudedge-10-0-1-44")
	paths, ok := status["captureForwardingPaths"].([]map[string]any)
	if !ok || len(paths) != 1 {
		t.Fatalf("captureForwardingPaths = %#v, want one SAM tunnel path", status["captureForwardingPaths"])
	}
	if paths[0]["captureInterface"] != "ens5" || paths[0]["tunnelInterface"] != "samt-onprem-aws" || paths[0]["enforced"] != true {
		t.Fatalf("forward path status = %#v", paths[0])
	}
}

func TestDynamicRouteSAMViewKeepsProviderSecondaryClaimFromConfirmedCaptureStatus(t *testing.T) {
	startup := bgpProxyARPStartup(false)
	for i, resource := range startup.Spec.Resources {
		switch {
		case resource.APIVersion == api.FederationAPIVersion && resource.Kind == "EventGroup":
			spec := resource.Spec.(api.EventGroupSpec)
			spec.NodeName = "aws-router"
			startup.Spec.Resources[i].Spec = spec
		case resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "MobilityPool":
			spec := resource.Spec.(api.MobilityPoolSpec)
			spec.Members[1].Capture.ConfigureOSAddress = true
			spec.Members[1].Capture.ProviderRef = "aws-lab"
			spec.Members[1].Capture.NICRef = "eni-a"
			startup.Spec.Resources[i].Spec = spec
		}
	}
	store := actionMapStore{
		mapStore: mapStore{
			api.MobilityAPIVersion + "/MobilityPool/cloudedge": {
				"ownershipResolverDecisions": []any{
					map[string]any{
						"address":           "10.0.1.44/32",
						"class":             "ConfirmedCapture",
						"captureHolderNode": "aws-router",
						"captureState":      "Confirmed",
					},
				},
			},
		},
		actions: []routerstate.ActionExecutionRecord{
			samSucceededAssignAction("aws-lab", "eni-a", "10.0.1.44/32"),
		},
	}
	view, err := buildDynamicRouteSAMView(startup, store, time.Now().UTC(), platform.OSLinux)
	if err != nil {
		t.Fatalf("buildDynamicRouteSAMView: %v", err)
	}
	claims := remoteAddressClaimSpecs(view.EffectiveRouter)
	if len(claims) != 1 {
		t.Fatalf("effective claims = %#v, want confirmed provider capture claim", claims)
	}
	if claims[0].Address != "10.0.1.44/32" || claims[0].Capture.Type != "provider-secondary-ip" || !claims[0].Capture.ConfigureOSAddress {
		t.Fatalf("claim = %#v, want confirmed provider-secondary-ip OS capture", claims[0])
	}
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: view.EffectiveRouter, Store: store, Lowerings: view.SAMLowerings, OS: platform.OSLinux, Applier: applier}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("SAM reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{"assign:10.0.1.44/32@ens5"})
}

func TestDynamicRouteSAMViewBGPProxyARPExcludesLocalAddresses(t *testing.T) {
	startup := bgpProxyARPStartup(false)
	for i, resource := range startup.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "MobilityPool" {
			continue
		}
		spec := resource.Spec.(api.MobilityPoolSpec)
		spec.Members[0].Capture.ExcludeAddresses = []string{"10.0.1.1/32"}
		startup.Spec.Resources[i].Spec = spec
	}
	store := mapStore{
		api.NetAPIVersion + "/BGPRouter/mobility-bgp": {
			"installedNextHops": map[string]any{
				"10.0.1.1/32":  []any{"10.99.0.1"},
				"10.0.1.10/32": []any{"10.99.0.1"},
				"10.0.1.11/32": []any{"10.99.0.2"},
			},
		},
		api.NetAPIVersion + "/VirtualAddress/onprem-vip": {"role": "master"},
	}
	view, err := buildDynamicRouteSAMView(startup, store, time.Now().UTC(), platform.OSLinux)
	if err != nil {
		t.Fatalf("buildDynamicRouteSAMView: %v", err)
	}
	claims := remoteAddressClaimSpecs(view.EffectiveRouter)
	if len(claims) != 1 || claims[0].Address != "10.0.1.11/32" {
		t.Fatalf("effective BGP proxy claims = %#v, want only 10.0.1.11/32", claims)
	}
	routeDestinations := ipv4RouteDestinations(t, view.RouteRouter)
	wantDestinations := []string{
		"10.0.1.0/32",
		"10.0.1.2/31",
		"10.0.1.4/30",
		"10.0.1.8/29",
		"10.0.1.16/28",
		"10.0.1.32/27",
		"10.0.1.64/26",
		"10.0.1.128/25",
	}
	if !reflect.DeepEqual(routeDestinations, wantDestinations) {
		t.Fatalf("capture prefix route destinations = %#v, want %#v", routeDestinations, wantDestinations)
	}
	if route := ipv4RouteSpecByName(t, view.RouteRouter, "sam-cloudedge-capture-10-0-1-2-31"); route.Destination != "10.0.1.2/31" || route.Device != "lan0" {
		t.Fatalf("split capture prefix route = %#v", route)
	}
}

func TestDynamicRouteSAMViewBGPProxyARPIdentityOnlyRemoteMatchesInline(t *testing.T) {
	now := time.Now().UTC()
	store := mapStore{
		api.NetAPIVersion + "/BGPRouter/mobility-bgp": {
			"installedNextHops": map[string]any{
				"10.0.1.10/32": []any{"10.99.0.1"},
				"10.0.1.11/32": []any{"10.99.0.2"},
				"10.0.1.12/32": []any{"10.99.0.3"},
			},
		},
		api.NetAPIVersion + "/VirtualAddress/onprem-vip": {"role": "master"},
	}
	inlineView, err := buildDynamicRouteSAMView(bgpProxyARPStartup(false), store, now, platform.OSLinux)
	if err != nil {
		t.Fatalf("inline view: %v", err)
	}
	identityView, err := buildDynamicRouteSAMView(bgpProxyARPStartup(true), store, now, platform.OSLinux)
	if err != nil {
		t.Fatalf("identity-only view: %v", err)
	}
	if got, want := remoteAddressClaimSpecs(identityView.EffectiveRouter), remoteAddressClaimSpecs(inlineView.EffectiveRouter); !reflect.DeepEqual(got, want) {
		t.Fatalf("identity-only proxy-ARP claims = %#v, want inline %#v", got, want)
	}
	if len(identityView.SAMLowerings) != 0 {
		t.Fatalf("identity-only BGP proxy claims produced SAM lowerings: %#v", identityView.SAMLowerings)
	}
}

func TestDynamicRouteSAMViewBGPProxyARPCapturePrefixRouteUsesInterfaceIfName(t *testing.T) {
	startup := bgpProxyARPStartup(false)
	startup.Spec.Resources = append(startup.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
		Metadata: api.ObjectMeta{Name: "svnet1"},
		Spec:     api.InterfaceSpec{IfName: "eth1", Managed: true},
	})
	for i, resource := range startup.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "MobilityPool" {
			continue
		}
		spec := resource.Spec.(api.MobilityPoolSpec)
		spec.Members[0].Capture.Interface = "svnet1"
		startup.Spec.Resources[i].Spec = spec
	}
	store := mapStore{
		api.NetAPIVersion + "/BGPRouter/mobility-bgp": {
			"installedNextHops": map[string]any{"10.0.1.11/32": []any{"10.99.0.2"}},
		},
		api.NetAPIVersion + "/Interface/svnet1":          {"ipv4Addresses": []any{"192.0.2.10/32", "10.0.1.254/32"}},
		api.NetAPIVersion + "/VirtualAddress/onprem-vip": {"role": "master"},
	}
	view, err := buildDynamicRouteSAMView(startup, store, time.Now().UTC(), platform.OSLinux)
	if err != nil {
		t.Fatalf("buildDynamicRouteSAMView: %v", err)
	}
	route := ipv4RouteSpecByName(t, view.RouteRouter, "sam-cloudedge-capture-prefix")
	if route.Destination != "10.0.1.0/24" || route.Device != "eth1" || route.PreferredSource != "10.0.1.254" || route.Metric != 90 {
		t.Fatalf("capture prefix route = %#v", route)
	}
}

func TestDynamicRouteSAMViewBGPProxyARPCaptureSourceAddressLowersStaticAddress(t *testing.T) {
	startup := bgpProxyARPStartup(false)
	startup.Spec.Resources = append(startup.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
		Metadata: api.ObjectMeta{Name: "svnet1"},
		Spec:     api.InterfaceSpec{IfName: "eth1", Managed: true},
	})
	for i, resource := range startup.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "MobilityPool" {
			continue
		}
		spec := resource.Spec.(api.MobilityPoolSpec)
		spec.Members[0].Capture.Interface = "svnet1"
		spec.Members[0].Capture.SourceAddress = "10.0.1.254"
		startup.Spec.Resources[i].Spec = spec
	}
	store := mapStore{
		api.NetAPIVersion + "/BGPRouter/mobility-bgp": {
			"installedNextHops": map[string]any{"10.0.1.11/32": []any{"10.99.0.2"}},
		},
		api.NetAPIVersion + "/VirtualAddress/onprem-vip": {"role": "master"},
	}
	view, err := buildDynamicRouteSAMView(startup, store, time.Now().UTC(), platform.OSLinux)
	if err != nil {
		t.Fatalf("buildDynamicRouteSAMView: %v", err)
	}
	address := ipv4StaticAddressSpecByName(t, view.RouteRouter, "sam-cloudedge-capture-source")
	if address.Interface != "svnet1" || address.Address != "10.0.1.254/32" {
		t.Fatalf("capture source address = %#v", address)
	}
	route := ipv4RouteSpecByName(t, view.RouteRouter, "sam-cloudedge-capture-prefix")
	if route.Destination != "10.0.1.0/24" || route.Device != "eth1" || route.PreferredSource != "10.0.1.254" || route.Metric != 90 {
		t.Fatalf("capture prefix route = %#v", route)
	}
}

func TestDynamicRouteSAMViewBGPProxyARPCaptureSourceAddressFromDHCPDoesNotLowerStaticAddress(t *testing.T) {
	startup := bgpProxyARPStartup(false)
	startup.Spec.Resources = append(startup.Spec.Resources,
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "svnet1"},
			Spec:     api.InterfaceSpec{IfName: "eth1", Managed: true},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"},
			Metadata: api.ObjectMeta{Name: "svnet1-source"},
			Spec:     api.DHCPv4ClientSpec{Interface: "svnet1"},
		},
	)
	for i, resource := range startup.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "MobilityPool" {
			continue
		}
		spec := resource.Spec.(api.MobilityPoolSpec)
		spec.Members[0].Capture.Interface = "svnet1"
		spec.Members[0].Capture.SourceAddressFrom = api.StatusValueSourceSpec{Resource: "DHCPv4Client/svnet1-source", Field: "currentAddress"}
		startup.Spec.Resources[i].Spec = spec
	}
	store := mapStore{
		api.NetAPIVersion + "/BGPRouter/mobility-bgp": {
			"installedNextHops": map[string]any{"10.0.1.11/32": []any{"10.99.0.2"}},
		},
		api.NetAPIVersion + "/DHCPv4Client/svnet1-source": {"currentAddress": "10.0.1.240/24"},
		api.NetAPIVersion + "/VirtualAddress/onprem-vip":  {"role": "master"},
	}
	view, err := buildDynamicRouteSAMView(startup, store, time.Now().UTC(), platform.OSLinux)
	if err != nil {
		t.Fatalf("buildDynamicRouteSAMView: %v", err)
	}
	if got := countResources(view.RouteRouter, api.NetAPIVersion, "IPv4StaticAddress"); got != 0 {
		t.Fatalf("IPv4StaticAddress resources = %d, want none for DHCP-owned source address", got)
	}
	route := ipv4RouteSpecByName(t, view.RouteRouter, "sam-cloudedge-capture-prefix")
	if route.Destination != "10.0.1.0/24" || route.Device != "eth1" || route.PreferredSource != "10.0.1.240" || route.Metric != 90 {
		t.Fatalf("capture prefix route = %#v", route)
	}
}

func TestDynamicRouteSAMViewBGPProxyARPCapturePrefixRouteHonorsGate(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/BGPRouter/mobility-bgp": {
			"installedNextHops": map[string]any{"10.0.1.11/32": []any{"10.99.0.2"}},
		},
		api.NetAPIVersion + "/VirtualAddress/onprem-vip": {"role": "backup"},
	}
	view, err := buildDynamicRouteSAMView(bgpProxyARPStartup(false), store, time.Now().UTC(), platform.OSLinux)
	if err != nil {
		t.Fatalf("buildDynamicRouteSAMView: %v", err)
	}
	if got := countResources(view.RouteRouter, api.NetAPIVersion, "IPv4Route"); got != 0 {
		t.Fatalf("backup route IPv4Routes = %d, want none", got)
	}
}

func TestDynamicRouteSAMClaimRemovalTriggersRouteAndSAMCleanup(t *testing.T) {
	startup := startupHybridContextRouter()
	now := time.Now().UTC()
	activeStore := &dynamicRouteSAMStore{
		records: []routerstate.DynamicConfigPartRecord{dynamicPartRecord(t, "MobilityPool/cloudedge/node/onprem", []api.Resource{
			addressMobilityDomainResource(),
			remoteAddressClaimResource("app", "10.0.1.123/32", "proxy-arp", "lan0"),
		}, now.Add(time.Hour))},
		objects: map[string]map[string]any{},
	}
	activeView, err := buildDynamicRouteSAMView(startup, activeStore, now, platform.OSLinux)
	if err != nil {
		t.Fatalf("active view: %v", err)
	}
	if len(activeView.SAMLowerings) != 1 {
		t.Fatalf("active lowerings = %+v, want one", activeView.SAMLowerings)
	}
	routeName := activeView.SAMLowerings[0].IPv4RouteName

	removedStore := &dynamicRouteSAMStore{
		records: []routerstate.DynamicConfigPartRecord{dynamicPartRecord(t, "MobilityPool/cloudedge/node/onprem", []api.Resource{
			addressMobilityDomainResource(),
			remoteAddressClaimResource("app", "10.0.1.123/32", "proxy-arp", "lan0"),
		}, now.Add(-time.Minute))},
		objects: map[string]map[string]any{},
		statuses: []routerstate.ObjectStatus{
			{
				APIVersion: api.NetAPIVersion,
				Kind:       "IPv4Route",
				Name:       routeName,
				Status: map[string]any{
					"phase":       "Installed",
					"type":        "unicast",
					"destination": "10.0.1.123/32",
					"device":      "wg-sam",
					"metric":      200,
				},
			},
			samRemoteAddressClaimStatus("app", "10.0.1.123/32", "lan0"),
		},
	}
	removedView, err := buildDynamicRouteSAMView(startup, removedStore, now, platform.OSLinux)
	if err != nil {
		t.Fatalf("removed view: %v", err)
	}
	var commands [][]string
	routeController := IPv4RouteController{
		Router: removedView.RouteRouter,
		Store:  removedStore,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			return nil, nil
		},
	}
	if err := routeController.reconcile(context.Background()); err != nil {
		t.Fatalf("route reconcile: %v", err)
	}
	wantRouteDelete := []string{"ip", "route", "del", "10.0.1.123/32", "dev", "wg-sam", "metric", "200"}
	if len(commands) != 1 || !reflect.DeepEqual(commands[0], wantRouteDelete) {
		t.Fatalf("route cleanup commands = %#v, want %#v", commands, wantRouteDelete)
	}

	applier := &fakeSAMApplier{}
	samController := SAMController{Router: removedView.EffectiveRouter, Store: removedStore, Lowerings: removedView.SAMLowerings, OS: platform.OSLinux, Applier: applier}
	if err := samController.Reconcile(context.Background()); err != nil {
		t.Fatalf("SAM reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{"delete:10.0.1.123/32@lan0"})
	if !removedStore.deleted[api.NetAPIVersion+"/IPv4Route/"+routeName] {
		t.Fatalf("route status was not deleted: %#v", removedStore.deleted)
	}
	if !removedStore.deleted[api.HybridAPIVersion+"/RemoteAddressClaim/app"] {
		t.Fatalf("claim status was not deleted: %#v", removedStore.deleted)
	}
}

func TestDynamicRouteSAMViewGatesRouteAndSAMCleanupOnVRRPBackup(t *testing.T) {
	startup := startupHybridContextRouter()
	startup.Spec.Resources = append(startup.Spec.Resources,
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan0"},
			Spec:     api.InterfaceSpec{IfName: "lan0", Managed: true},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "onprem-vip"},
			Spec:     api.VirtualAddressSpec{Family: "ipv4", Interface: "lan0", Address: "10.0.1.1/32", Mode: "vrrp", VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 40, Peers: []string{"10.0.1.2"}}},
		},
	)
	now := time.Now().UTC()
	claim := remoteAddressClaimResource("app", "10.0.1.123/32", "proxy-arp", "lan0")
	claimSpec := claim.Spec.(api.RemoteAddressClaimSpec)
	claimSpec.Capture.ActiveWhen = api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"}
	claim.Spec = claimSpec

	activeStore := &dynamicRouteSAMStore{
		records: []routerstate.DynamicConfigPartRecord{dynamicPartRecord(t, "MobilityPool/cloudedge/node/onprem-a", []api.Resource{
			addressMobilityDomainResource(),
			claim,
		}, now.Add(time.Hour))},
		objects: map[string]map[string]any{
			api.NetAPIVersion + "/VirtualAddress/onprem-vip": {"role": "master"},
		},
	}
	activeView, err := buildDynamicRouteSAMView(startup, activeStore, now, platform.OSLinux)
	if err != nil {
		t.Fatalf("active view: %v", err)
	}
	if countResources(activeView.RouteRouter, api.NetAPIVersion, "IPv4Route") != 1 || len(activeView.SAMLowerings) != 1 {
		t.Fatalf("active view route/lowerings = %d/%d", countResources(activeView.RouteRouter, api.NetAPIVersion, "IPv4Route"), len(activeView.SAMLowerings))
	}
	routeName := activeView.SAMLowerings[0].IPv4RouteName

	backupStore := &dynamicRouteSAMStore{
		records: activeStore.records,
		objects: map[string]map[string]any{
			api.NetAPIVersion + "/VirtualAddress/onprem-vip": {"role": "backup"},
		},
		statuses: []routerstate.ObjectStatus{
			{
				APIVersion: api.NetAPIVersion,
				Kind:       "IPv4Route",
				Name:       routeName,
				Status: map[string]any{
					"phase":       "Installed",
					"type":        "unicast",
					"destination": "10.0.1.123/32",
					"device":      "wg-sam",
					"metric":      120,
				},
			},
			samRemoteAddressClaimStatus("app", "10.0.1.123/32", "lan0"),
		},
	}
	backupView, err := buildDynamicRouteSAMView(startup, backupStore, now, platform.OSLinux)
	if err != nil {
		t.Fatalf("backup view: %v", err)
	}
	if countResources(backupView.RouteRouter, api.NetAPIVersion, "IPv4Route") != 0 || len(backupView.SAMLowerings) != 0 {
		t.Fatalf("backup view route/lowerings = %d/%d, want none", countResources(backupView.RouteRouter, api.NetAPIVersion, "IPv4Route"), len(backupView.SAMLowerings))
	}

	var commands [][]string
	routeController := IPv4RouteController{
		Router: backupView.RouteRouter,
		Store:  backupStore,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			return nil, nil
		},
	}
	if err := routeController.reconcile(context.Background()); err != nil {
		t.Fatalf("route reconcile: %v", err)
	}
	wantRouteDelete := []string{"ip", "route", "del", "10.0.1.123/32", "dev", "wg-sam", "metric", "120"}
	if len(commands) != 1 || !reflect.DeepEqual(commands[0], wantRouteDelete) {
		t.Fatalf("route cleanup commands = %#v, want %#v", commands, wantRouteDelete)
	}

	applier := &fakeSAMApplier{}
	samController := SAMController{Router: backupView.EffectiveRouter, Store: backupStore, Lowerings: backupView.SAMLowerings, OS: platform.OSLinux, Applier: applier, GARP: &fakeSAMGARP{}}
	if err := samController.Reconcile(context.Background()); err != nil {
		t.Fatalf("SAM reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{"delete:10.0.1.123/32@lan0", "proxyarp:lan0=0"})
	status := backupStore.ObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", "app")
	if status["phase"] != "Gated" {
		t.Fatalf("backup claim status = %#v", status)
	}

	rejoinedStore := &dynamicRouteSAMStore{
		records: activeStore.records,
		objects: map[string]map[string]any{
			api.NetAPIVersion + "/VirtualAddress/onprem-vip": {"role": "master"},
		},
		statuses: []routerstate.ObjectStatus{
			{
				APIVersion: api.HybridAPIVersion,
				Kind:       "RemoteAddressClaim",
				Name:       "app",
				Status: map[string]any{
					"phase":         "Gated",
					"captureStatus": "gated",
				},
			},
		},
	}
	rejoinedView, err := buildDynamicRouteSAMView(startup, rejoinedStore, now, platform.OSLinux)
	if err != nil {
		t.Fatalf("rejoined view: %v", err)
	}
	if countResources(rejoinedView.RouteRouter, api.NetAPIVersion, "IPv4Route") != 1 || len(rejoinedView.SAMLowerings) != 1 {
		t.Fatalf("rejoined view route/lowerings = %d/%d, want one each", countResources(rejoinedView.RouteRouter, api.NetAPIVersion, "IPv4Route"), len(rejoinedView.SAMLowerings))
	}

	rejoinedApplier := &fakeSAMApplier{}
	rejoinedGARP := &fakeSAMGARP{}
	rejoinedSAM := SAMController{Router: rejoinedView.EffectiveRouter, Store: rejoinedStore, Lowerings: rejoinedView.SAMLowerings, OS: platform.OSLinux, Applier: rejoinedApplier, GARP: rejoinedGARP}
	if err := rejoinedSAM.Reconcile(context.Background()); err != nil {
		t.Fatalf("rejoined SAM reconcile: %v", err)
	}
	assertSAMCalls(t, rejoinedApplier.calls, []string{"proxyarp:lan0=1", "ensure:10.0.1.123/32@lan0"})
	if !reflect.DeepEqual(rejoinedGARP.calls, []string{"10.0.1.123/32@lan0"}) {
		t.Fatalf("rejoined GARP calls = %#v, want one gratuitous ARP", rejoinedGARP.calls)
	}
}

type dynamicRouteSAMStore struct {
	records  []routerstate.DynamicConfigPartRecord
	objects  map[string]map[string]any
	statuses []routerstate.ObjectStatus
	deleted  map[string]bool
}

func (s *dynamicRouteSAMStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	if s.objects != nil {
		s.objects[apiVersion+"/"+kind+"/"+name] = status
	}
	return nil
}

func (s *dynamicRouteSAMStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if s.objects != nil {
		if status := s.objects[apiVersion+"/"+kind+"/"+name]; status != nil {
			return status
		}
	}
	return map[string]any{}
}

func (s *dynamicRouteSAMStore) ListObjectStatuses() ([]routerstate.ObjectStatus, error) {
	return s.statuses, nil
}

func (s *dynamicRouteSAMStore) DeleteObject(apiVersion, kind, name string) error {
	if s.deleted == nil {
		s.deleted = map[string]bool{}
	}
	s.deleted[apiVersion+"/"+kind+"/"+name] = true
	return nil
}

func (s *dynamicRouteSAMStore) ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error) {
	return s.records, nil
}

func dynamicPartRecord(t *testing.T, source string, resources []api.Resource, expiresAt time.Time) routerstate.DynamicConfigPartRecord {
	t.Helper()
	raw, err := json.Marshal(resources)
	if err != nil {
		t.Fatalf("marshal resources: %v", err)
	}
	return routerstate.DynamicConfigPartRecord{
		Source:        source,
		Generation:    1,
		ObservedAt:    time.Now().UTC(),
		ExpiresAt:     expiresAt,
		Digest:        source + "-digest",
		ResourcesJSON: string(raw),
		Status:        "active",
	}
}

func staticSAMRouter(address, captureType, captureInterface string) *api.Router {
	router := startupHybridContextRouter()
	router.Spec.Resources = append(router.Spec.Resources,
		addressMobilityDomainResource(),
		remoteAddressClaimResource("app", address, captureType, captureInterface),
	)
	return router
}

func bgpProxyARPStartup(identityOnlyRemote bool) *api.Router {
	startup := startupHybridContextRouter()
	awsMember := api.MobilityPoolMember{NodeRef: "aws-router", Site: "aws", Role: "cloud", Capture: api.MobilityMemberCapture{Type: "provider-secondary-ip", Interface: "ens5"}}
	if identityOnlyRemote {
		awsMember = api.MobilityPoolMember{NodeRef: "aws-router", Site: "aws", Role: "cloud"}
	}
	startup.Spec.Resources = append(startup.Spec.Resources,
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
			Metadata: api.ObjectMeta{Name: "cloudedge"},
			Spec:     api.EventGroupSpec{NodeName: "onprem-router"},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
			Metadata: api.ObjectMeta{Name: "mobility-bgp"},
			Spec:     api.BGPRouterSpec{ASN: 64577, RouterID: "10.99.0.1"},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "onprem-vip"},
			Spec:     api.VirtualAddressSpec{Family: "ipv4", Interface: "lan0", Address: "10.0.1.1/32", Mode: "vrrp", VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 40, Peers: []string{"10.0.1.2"}}},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
			Metadata: api.ObjectMeta{Name: "cloudedge"},
			Spec: api.MobilityPoolSpec{
				Prefix:         "10.0.1.0/24",
				GroupRef:       "cloudedge",
				DeliveryPolicy: api.MobilityDeliveryPolicy{Mode: "bgp"},
				Members: []api.MobilityPoolMember{
					{
						NodeRef:              "onprem-router",
						Site:                 "onprem",
						Role:                 "onprem",
						StaticOwnedAddresses: []string{"10.0.1.10/32"},
						Capture: api.MobilityMemberCapture{
							Type:       "proxy-arp",
							Interface:  "lan0",
							ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"},
						},
					},
					awsMember,
				},
			},
		},
	)
	return startup
}

func remoteAddressClaimSpecs(router *api.Router) []api.RemoteAddressClaimSpec {
	if router == nil {
		return nil
	}
	var out []api.RemoteAddressClaimSpec
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.HybridAPIVersion || res.Kind != "RemoteAddressClaim" {
			continue
		}
		spec, err := res.RemoteAddressClaimSpec()
		if err == nil {
			out = append(out, spec)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

func startupHybridContextRouter() *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test-router"},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"},
			Metadata: api.ObjectMeta{Name: "cloud"},
			Spec: api.OverlayPeerSpec{
				Role:     "cloud",
				NodeID:   "cloud",
				Underlay: api.OverlayUnderlay{Type: "wireguard", Interface: "wg-sam"},
			},
		}}},
	}
}

func addressMobilityDomainResource() api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "AddressMobilityDomain"},
		Metadata: api.ObjectMeta{Name: "same-subnet"},
		Spec: api.AddressMobilityDomainSpec{
			Prefix:  "10.0.1.0/24",
			Mode:    "selective-address",
			PeerRef: "cloud",
		},
	}
}

func remoteAddressClaimResource(name, address, captureType, captureInterface string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "RemoteAddressClaim"},
		Metadata: api.ObjectMeta{Name: name},
		Spec: api.RemoteAddressClaimSpec{
			DomainRef: "same-subnet",
			Address:   address,
			OwnerSide: "onprem",
			Capture:   api.AddressCapture{Type: captureType, Interface: captureInterface},
			Delivery:  api.AddressDelivery{PeerRef: "cloud", Mode: "route", TunnelInterface: "wg-sam"},
		},
	}
}

func countResources(router *api.Router, apiVersion, kind string) int {
	if router == nil {
		return 0
	}
	count := 0
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == apiVersion && resource.Kind == kind {
			count++
		}
	}
	return count
}

func resourceByName(t *testing.T, router *api.Router, apiVersion, kind, name string) api.Resource {
	t.Helper()
	if router == nil {
		t.Fatalf("router is nil")
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == apiVersion && resource.Kind == kind && resource.Metadata.Name == name {
			return resource
		}
	}
	t.Fatalf("resource %s/%s/%s not found", apiVersion, kind, name)
	return api.Resource{}
}

func ipv4RouteSpecByName(t *testing.T, router *api.Router, name string) api.IPv4RouteSpec {
	t.Helper()
	if router == nil {
		t.Fatalf("router is nil")
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.NetAPIVersion && resource.Kind == "IPv4Route" && resource.Metadata.Name == name {
			spec, err := resource.IPv4RouteSpec()
			if err != nil {
				t.Fatalf("%s IPv4RouteSpec: %v", name, err)
			}
			return spec
		}
	}
	t.Fatalf("IPv4Route/%s not found", name)
	return api.IPv4RouteSpec{}
}

func ipv4RouteDestinations(t *testing.T, router *api.Router) []string {
	t.Helper()
	if router == nil {
		t.Fatalf("router is nil")
	}
	var out []string
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "IPv4Route" {
			continue
		}
		spec, err := resource.IPv4RouteSpec()
		if err != nil {
			t.Fatalf("%s IPv4RouteSpec: %v", resource.Metadata.Name, err)
		}
		out = append(out, spec.Destination)
	}
	return out
}

func ipv4StaticAddressSpecByName(t *testing.T, router *api.Router, name string) api.IPv4StaticAddressSpec {
	t.Helper()
	if router == nil {
		t.Fatalf("router is nil")
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.NetAPIVersion && resource.Kind == "IPv4StaticAddress" && resource.Metadata.Name == name {
			spec, err := resource.IPv4StaticAddressSpec()
			if err != nil {
				t.Fatalf("%s IPv4StaticAddressSpec: %v", name, err)
			}
			return spec
		}
	}
	t.Fatalf("IPv4StaticAddress/%s not found", name)
	return api.IPv4StaticAddressSpec{}
}
