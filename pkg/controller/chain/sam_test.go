// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/sam"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestSAMControllerAppliesProxyNeighborAndStatus(t *testing.T) {
	router := samControllerRouter()
	expanded, lowerings, err := sam.ExpandRemoteAddressClaimRoutes(*router)
	if err != nil {
		t.Fatal(err)
	}
	store := &samStore{objects: map[string]map[string]any{}}
	store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", lowerings[0].IPv4RouteName, map[string]any{"phase": "Installed"})
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: router, Store: store, Lowerings: lowerings, OS: platform.OSLinux, Applier: applier}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	_ = expanded
	if len(applier.ensure) != 1 || applier.ensure[0] != "10.0.1.123/32@lan0" {
		t.Fatalf("ensure = %#v", applier.ensure)
	}
	status := store.ObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", "app")
	if status["phase"] != "Ready" || status["deliveryRouteName"] != lowerings[0].IPv4RouteName || status["captureType"] != "proxy-arp" || status["captureStatus"] != sam.CaptureStatusCaptured {
		t.Fatalf("status = %#v", status)
	}
}

func TestSAMControllerDeassignsProviderSecondaryOSAddressAndStatus(t *testing.T) {
	router := samControllerRouterWithClaim("10.0.1.122/32", "provider-secondary-ip", "")
	store := &samStore{objects: map[string]map[string]any{}}
	applier := &fakeSAMApplier{deassignResult: samOSAddressDeassignResult{
		address:              "10.0.1.122/32",
		ifname:               "eth0",
		removedThisReconcile: true,
	}}
	controller := SAMController{Router: router, Store: store, OS: platform.OSLinux, Applier: applier}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{"deassign:10.0.1.122/32"})
	status := store.ObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", "app")
	note, ok := status["captureOSAddressAbsence"].(map[string]any)
	if !ok {
		t.Fatalf("missing captureOSAddressAbsence in status %#v", status)
	}
	if note["address"] != "10.0.1.122/32" || note["interface"] != "eth0" || note["enforced"] != true || note["lastReconcileRemoved"] != true {
		t.Fatalf("deassign note = %#v", note)
	}
}

func TestSAMControllerDeassignAbsentAddressIsNoopButTracked(t *testing.T) {
	router := samControllerRouterWithClaim("10.0.1.122/32", "provider-secondary-ip", "")
	store := &samStore{objects: map[string]map[string]any{}}
	applier := &fakeSAMApplier{deassignResult: samOSAddressDeassignResult{address: "10.0.1.122/32"}}
	controller := SAMController{Router: router, Store: store, OS: platform.OSLinux, Applier: applier}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{"deassign:10.0.1.122/32"})
	status := store.ObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", "app")
	note, ok := status["captureOSAddressAbsence"].(map[string]any)
	if !ok {
		t.Fatalf("missing captureOSAddressAbsence in status %#v", status)
	}
	if note["address"] != "10.0.1.122/32" || note["enforced"] != true || note["lastReconcileRemoved"] != false {
		t.Fatalf("deassign note = %#v", note)
	}
	if _, ok := note["interface"]; ok {
		t.Fatalf("absent address should not record interface: %#v", note)
	}
}

func TestSAMControllerAssignsProviderSecondaryOSAddressAndStatus(t *testing.T) {
	router := samControllerRouterWithClaim("10.0.1.122/32", "provider-secondary-ip", "eth0")
	spec := router.Spec.Resources[1].Spec.(api.RemoteAddressClaimSpec)
	spec.Capture.ConfigureOSAddress = true
	router.Spec.Resources[1].Spec = spec
	store := &samStore{objects: map[string]map[string]any{}}
	applier := &fakeSAMApplier{assignResult: samOSAddressAssignResult{
		address:            "10.0.1.122/32",
		ifname:             "eth0",
		addedThisReconcile: true,
	}}
	controller := SAMController{Router: router, Store: store, OS: platform.OSLinux, Applier: applier}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{"assign:10.0.1.122/32@eth0"})
	status := store.ObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", "app")
	note, ok := status["captureOSAddressPresence"].(map[string]any)
	if !ok {
		t.Fatalf("missing captureOSAddressPresence in status %#v", status)
	}
	if note["address"] != "10.0.1.122/32" || note["interface"] != "eth0" || note["enforced"] != true || note["lastReconcileAdded"] != true {
		t.Fatalf("assign note = %#v", note)
	}
}

func TestSAMControllerNonLinuxNoHostActions(t *testing.T) {
	router := samControllerRouter()
	store := &samStore{objects: map[string]map[string]any{}}
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: router, Store: store, OS: platform.OSFreeBSD, Applier: applier}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(applier.ensure) != 0 || len(applier.delete) != 0 || len(applier.deassign) != 0 {
		t.Fatalf("host actions = ensure %#v delete %#v deassign %#v, want none", applier.ensure, applier.delete, applier.deassign)
	}
	status := store.ObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", "app")
	if status["phase"] != "Degraded" || status["reason"] != "CaptureUnsupported" {
		t.Fatalf("status = %#v", status)
	}
}

func TestSAMControllerCleansRemovedProxyNeighbor(t *testing.T) {
	router := &api.Router{}
	store := &samStore{
		objects: map[string]map[string]any{},
		statuses: []routerstate.ObjectStatus{{
			APIVersion: api.HybridAPIVersion,
			Kind:       "RemoteAddressClaim",
			Name:       "old",
			Status: map[string]any{
				"captureProxyNeighbor": map[string]any{"address": "10.0.1.123/32", "interface": "lan0"},
			},
		}},
	}
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: router, Store: store, OS: platform.OSLinux, Applier: applier}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(applier.delete) != 1 || applier.delete[0] != "10.0.1.123/32@lan0" {
		t.Fatalf("delete = %#v", applier.delete)
	}
	if !store.deleted[api.HybridAPIVersion+"/RemoteAddressClaim/old"] {
		t.Fatalf("deleted = %#v", store.deleted)
	}
}

func TestSAMControllerCleansChangedProxyNeighborInterface(t *testing.T) {
	router := samControllerRouterWithClaim("10.0.1.123/32", "proxy-arp", "br-new")
	store := &samStore{objects: map[string]map[string]any{}, statuses: []routerstate.ObjectStatus{
		samRemoteAddressClaimStatus("app", "10.0.1.123/32", "br-old"),
	}}
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: router, Store: store, OS: platform.OSLinux, Applier: applier}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{
		"delete:10.0.1.123/32@br-old",
		"proxyarp:br-new=1",
		"ensure:10.0.1.123/32@br-new",
	})
}

func TestSAMControllerCleansChangedProxyNeighborAddress(t *testing.T) {
	router := samControllerRouterWithClaim("10.0.1.124/32", "proxy-arp", "lan0")
	store := &samStore{objects: map[string]map[string]any{}, statuses: []routerstate.ObjectStatus{
		samRemoteAddressClaimStatus("app", "10.0.1.123/32", "lan0"),
	}}
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: router, Store: store, OS: platform.OSLinux, Applier: applier}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{
		"delete:10.0.1.123/32@lan0",
		"proxyarp:lan0=1",
		"ensure:10.0.1.124/32@lan0",
	})
}

func TestSAMControllerCleansProxyNeighborWhenCaptureTypeChanges(t *testing.T) {
	router := samControllerRouterWithClaim("10.0.1.123/32", "provider-secondary-ip", "")
	store := &samStore{objects: map[string]map[string]any{}, statuses: []routerstate.ObjectStatus{
		samRemoteAddressClaimStatus("app", "10.0.1.123/32", "lan0"),
	}}
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: router, Store: store, OS: platform.OSLinux, Applier: applier}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{"delete:10.0.1.123/32@lan0", "deassign:10.0.1.123/32"})
	if len(applier.ensure) != 0 {
		t.Fatalf("ensure = %#v, want none", applier.ensure)
	}
}

func TestSAMControllerLeavesUnchangedProxyNeighbor(t *testing.T) {
	router := samControllerRouter()
	store := &samStore{objects: map[string]map[string]any{}, statuses: []routerstate.ObjectStatus{
		samRemoteAddressClaimStatus("app", "10.0.1.123/32", "lan0"),
	}}
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: router, Store: store, OS: platform.OSLinux, Applier: applier}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(applier.delete) != 0 {
		t.Fatalf("delete = %#v, want none", applier.delete)
	}
	assertSAMCalls(t, applier.calls, []string{"proxyarp:lan0=1", "ensure:10.0.1.123/32@lan0"})
}

func TestSAMControllerLeavesUnchangedProxyNeighborWithStoredInterfaceResourceName(t *testing.T) {
	router := samControllerRouterWithClaim("10.0.1.123/32", "proxy-arp", "svnet1")
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
		Metadata: api.ObjectMeta{Name: "svnet1"},
		Spec:     api.InterfaceSpec{IfName: "eth1", Managed: true},
	})
	expanded, lowerings, err := sam.ExpandRemoteAddressClaimRoutes(*router)
	if err != nil {
		t.Fatalf("ExpandRemoteAddressClaimRoutes: %v", err)
	}
	router = &expanded
	store := &samStore{objects: map[string]map[string]any{}, statuses: []routerstate.ObjectStatus{
		samRemoteAddressClaimStatus("app", "10.0.1.123/32", "svnet1"),
	}}
	store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", lowerings[0].IPv4RouteName, map[string]any{"phase": "Installed"})
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: router, Store: store, Lowerings: lowerings, OS: platform.OSLinux, Applier: applier}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{"proxyarp:eth1=1", "ensure:10.0.1.123/32@eth1"})
	status := store.ObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", "app")
	note, ok := status["captureProxyNeighbor"].(map[string]any)
	if !ok || note["interface"] != "eth1" {
		t.Fatalf("captureProxyNeighbor = %#v", status["captureProxyNeighbor"])
	}
}

func TestSAMControllerGatedProxyNeighborSendsGARPOnlyOnInactiveToActive(t *testing.T) {
	router := samControllerRouterWithClaim("10.0.1.123/32", "proxy-arp", "lan0")
	spec := router.Spec.Resources[1].Spec.(api.RemoteAddressClaimSpec)
	spec.Capture.ActiveWhen = api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"}
	router.Spec.Resources[1].Spec = spec

	store := &samStore{objects: map[string]map[string]any{
		api.NetAPIVersion + "/VirtualAddress/onprem-vip": {"role": "master"},
	}}
	expanded, lowerings, err := sam.ExpandRemoteAddressClaimRoutesWithOptions(*router, sam.PlanOptions{StatusReader: store})
	if err != nil {
		t.Fatalf("ExpandRemoteAddressClaimRoutesWithOptions: %v", err)
	}
	router = &expanded
	applier := &fakeSAMApplier{}
	garp := &fakeSAMGARP{}
	controller := SAMController{Router: router, Store: store, Lowerings: lowerings, OS: platform.OSLinux, Applier: applier, GARP: garp}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{"proxyarp:lan0=1", "ensure:10.0.1.123/32@lan0"})
	if len(garp.calls) != 1 || garp.calls[0] != "10.0.1.123/32@lan0" {
		t.Fatalf("first GARP calls = %#v", garp.calls)
	}
	status := store.ObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", "app")
	if status["captureStatus"] != sam.CaptureStatusCaptured || status["lastGARPSent"] != true {
		t.Fatalf("first status = %#v", status)
	}

	store.statuses = []routerstate.ObjectStatus{samRemoteAddressClaimStatus("app", "10.0.1.123/32", "lan0")}
	applier.calls = nil
	applier.ensure = nil
	garp.calls = nil
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("steady Reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{"proxyarp:lan0=1", "ensure:10.0.1.123/32@lan0"})
	if len(garp.calls) != 0 {
		t.Fatalf("steady-state GARP calls = %#v, want none", garp.calls)
	}
	status = store.ObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", "app")
	if _, ok := status["lastGARPSent"]; ok {
		t.Fatalf("steady status should not mark GARP sent: %#v", status)
	}
}

func TestSAMControllerGARPFailureDoesNotFailCapture(t *testing.T) {
	router := samControllerRouterWithClaim("10.0.1.123/32", "proxy-arp", "lan0")
	spec := router.Spec.Resources[1].Spec.(api.RemoteAddressClaimSpec)
	spec.Capture.GratuitousARP = true
	router.Spec.Resources[1].Spec = spec

	expanded, lowerings, err := sam.ExpandRemoteAddressClaimRoutes(*router)
	if err != nil {
		t.Fatalf("ExpandRemoteAddressClaimRoutes: %v", err)
	}
	router = &expanded
	store := &samStore{objects: map[string]map[string]any{}}
	store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", lowerings[0].IPv4RouteName, map[string]any{"phase": "Installed"})
	applier := &fakeSAMApplier{}
	garp := &fakeSAMGARP{err: errors.New("arping failed")}
	controller := SAMController{Router: router, Store: store, Lowerings: lowerings, OS: platform.OSLinux, Applier: applier, GARP: garp}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{"proxyarp:lan0=1", "ensure:10.0.1.123/32@lan0"})
	status := store.ObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", "app")
	if status["phase"] != "Ready" || status["lastGARPError"] != "gratuitous ARP 10.0.1.123/32 dev lan0: arping failed" {
		t.Fatalf("status = %#v", status)
	}
}

func TestSAMControllerGatedProxyNeighborCleansOnMasterToBackupWithoutGARP(t *testing.T) {
	router := samControllerRouterWithClaim("10.0.1.123/32", "proxy-arp", "lan0")
	spec := router.Spec.Resources[1].Spec.(api.RemoteAddressClaimSpec)
	spec.Capture.ActiveWhen = api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"}
	router.Spec.Resources[1].Spec = spec

	store := &samStore{
		objects: map[string]map[string]any{
			api.NetAPIVersion + "/VirtualAddress/onprem-vip": {"role": "backup"},
		},
		statuses: []routerstate.ObjectStatus{samRemoteAddressClaimStatus("app", "10.0.1.123/32", "lan0")},
	}
	applier := &fakeSAMApplier{}
	garp := &fakeSAMGARP{}
	controller := SAMController{Router: router, Store: store, OS: platform.OSLinux, Applier: applier, GARP: garp}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{"delete:10.0.1.123/32@lan0", "proxyarp:lan0=0"})
	if len(garp.calls) != 0 {
		t.Fatalf("backup transition GARP calls = %#v, want none", garp.calls)
	}
	status := store.ObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", "app")
	if status["phase"] != "Gated" || status["reason"] != "CaptureGateInactive" || status["captureActive"] != false || status["captureStatus"] != sam.CaptureStatusStandby {
		t.Fatalf("gated status = %#v", status)
	}
	if _, ok := status["captureProxyNeighbor"]; ok {
		t.Fatalf("standby status must not retain captureProxyNeighbor: %#v", status)
	}
}

func TestSAMControllerGatedProxyARPDisableResolvesInterfaceResourceName(t *testing.T) {
	router := samControllerRouterWithClaim("10.0.1.123/32", "proxy-arp", "svnet1")
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
		Metadata: api.ObjectMeta{Name: "svnet1"},
		Spec:     api.InterfaceSpec{IfName: "eth1", Managed: true},
	})
	spec := router.Spec.Resources[1].Spec.(api.RemoteAddressClaimSpec)
	spec.Capture.ActiveWhen = api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"}
	router.Spec.Resources[1].Spec = spec

	store := &samStore{
		objects: map[string]map[string]any{
			api.NetAPIVersion + "/VirtualAddress/onprem-vip": {"role": "backup"},
		},
		statuses: []routerstate.ObjectStatus{samRemoteAddressClaimStatus("app", "10.0.1.123/32", "eth1")},
	}
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: router, Store: store, OS: platform.OSLinux, Applier: applier}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{"delete:10.0.1.123/32@eth1", "proxyarp:eth1=0"})
}

func TestSAMControllerGatedProxyNeighborUnknownStatusIsBlockedFailClosed(t *testing.T) {
	router := samControllerRouterWithClaim("10.0.1.123/32", "proxy-arp", "lan0")
	spec := router.Spec.Resources[1].Spec.(api.RemoteAddressClaimSpec)
	spec.Capture.ActiveWhen = api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"}
	router.Spec.Resources[1].Spec = spec

	store := &samStore{
		objects:  map[string]map[string]any{},
		statuses: []routerstate.ObjectStatus{samRemoteAddressClaimStatus("app", "10.0.1.123/32", "lan0")},
	}
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: router, Store: store, OS: platform.OSLinux, Applier: applier, GARP: &fakeSAMGARP{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	assertSAMCalls(t, applier.calls, []string{"delete:10.0.1.123/32@lan0", "proxyarp:lan0=0"})
	status := store.ObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", "app")
	if status["phase"] != "Gated" || status["captureStatus"] != sam.CaptureStatusBlocked {
		t.Fatalf("unknown gate status = %#v", status)
	}
	if _, ok := status["captureProxyNeighbor"]; ok {
		t.Fatalf("blocked status must not retain captureProxyNeighbor: %#v", status)
	}
}

func TestSAMControllerDryRunSkipsProxyNeighborActions(t *testing.T) {
	router := samControllerRouterWithClaim("10.0.1.123/32", "proxy-arp", "br-new")
	store := &samStore{objects: map[string]map[string]any{}, statuses: []routerstate.ObjectStatus{
		samRemoteAddressClaimStatus("app", "10.0.1.123/32", "br-old"),
	}}
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: router, Store: store, DryRun: true, OS: platform.OSLinux, Applier: applier}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(applier.ensure) != 0 || len(applier.delete) != 0 || len(applier.deassign) != 0 || len(applier.calls) != 0 {
		t.Fatalf("host actions = ensure %#v delete %#v deassign %#v calls %#v, want none", applier.ensure, applier.delete, applier.deassign, applier.calls)
	}
}

func TestSAMControllerNoClaimsNoProxyNeighborActions(t *testing.T) {
	router := &api.Router{}
	store := &samStore{objects: map[string]map[string]any{}}
	applier := &fakeSAMApplier{}
	controller := SAMController{Router: router, Store: store, OS: platform.OSLinux, Applier: applier}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(applier.ensure) != 0 || len(applier.delete) != 0 || len(applier.deassign) != 0 || len(applier.calls) != 0 {
		t.Fatalf("host actions = ensure %#v delete %#v deassign %#v calls %#v, want none", applier.ensure, applier.delete, applier.deassign, applier.calls)
	}
}

type fakeSAMApplier struct {
	ensure         []string
	delete         []string
	assign         []string
	deassign       []string
	proxyARP       []string
	calls          []string
	assignResult   samOSAddressAssignResult
	deassignResult samOSAddressDeassignResult
}

type fakeSAMGARP struct {
	calls []string
	err   error
}

func (g *fakeSAMGARP) SendGratuitousARP(_ context.Context, address, ifname string) error {
	g.calls = append(g.calls, address+"@"+ifname)
	return g.err
}

func (a *fakeSAMApplier) SetProxyARP(_ context.Context, ifname string, enabled bool) error {
	value := "0"
	if enabled {
		value = "1"
	}
	a.proxyARP = append(a.proxyARP, ifname+"="+value)
	a.calls = append(a.calls, "proxyarp:"+ifname+"="+value)
	return nil
}

func (a *fakeSAMApplier) EnsureProxyNeighbor(_ context.Context, address, ifname string) error {
	a.ensure = append(a.ensure, address+"@"+ifname)
	a.calls = append(a.calls, "ensure:"+address+"@"+ifname)
	return nil
}

func (a *fakeSAMApplier) DeleteProxyNeighbor(_ context.Context, address, ifname string) error {
	a.delete = append(a.delete, address+"@"+ifname)
	a.calls = append(a.calls, "delete:"+address+"@"+ifname)
	return nil
}

func (a *fakeSAMApplier) EnsureOSAddressPresent(_ context.Context, address, ifname string) (samOSAddressAssignResult, error) {
	a.assign = append(a.assign, address+"@"+ifname)
	a.calls = append(a.calls, "assign:"+address+"@"+ifname)
	result := a.assignResult
	if result.address == "" {
		result.address = address
	}
	if result.ifname == "" {
		result.ifname = ifname
	}
	return result, nil
}

func (a *fakeSAMApplier) EnsureOSAddressAbsent(_ context.Context, address string) (samOSAddressDeassignResult, error) {
	a.deassign = append(a.deassign, address)
	a.calls = append(a.calls, "deassign:"+address)
	result := a.deassignResult
	if result.address == "" {
		result.address = address
	}
	return result, nil
}

type samStore struct {
	objects  map[string]map[string]any
	statuses []routerstate.ObjectStatus
	deleted  map[string]bool
}

func (s *samStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	if s.objects != nil {
		s.objects[apiVersion+"/"+kind+"/"+name] = status
	}
	return nil
}

func (s *samStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if s.objects != nil {
		if status := s.objects[apiVersion+"/"+kind+"/"+name]; status != nil {
			return status
		}
	}
	return map[string]any{}
}

func (s *samStore) ListObjectStatuses() ([]routerstate.ObjectStatus, error) {
	return s.statuses, nil
}

func (s *samStore) DeleteObject(apiVersion, kind, name string) error {
	if s.deleted == nil {
		s.deleted = map[string]bool{}
	}
	s.deleted[apiVersion+"/"+kind+"/"+name] = true
	return nil
}

func samControllerRouter() *api.Router {
	return samControllerRouterWithClaim("10.0.1.123/32", "proxy-arp", "lan0")
}

func samControllerRouterWithClaim(address, captureType, captureInterface string) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"}, Metadata: api.ObjectMeta{Name: "cloud"}, Spec: api.OverlayPeerSpec{
			Role:     "cloud",
			NodeID:   "cloud",
			Underlay: api.OverlayUnderlay{Type: "wireguard", Interface: "wg-sam"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "RemoteAddressClaim"}, Metadata: api.ObjectMeta{Name: "app"}, Spec: api.RemoteAddressClaimSpec{
			DomainRef: "same-subnet",
			Address:   address,
			OwnerSide: "onprem",
			Capture:   api.AddressCapture{Type: captureType, Interface: captureInterface},
			Delivery:  api.AddressDelivery{PeerRef: "cloud", Mode: "route", TunnelInterface: "wg-sam"},
		}},
	}}}
}

func samRemoteAddressClaimStatus(name, address, ifname string) routerstate.ObjectStatus {
	return routerstate.ObjectStatus{
		APIVersion: api.HybridAPIVersion,
		Kind:       "RemoteAddressClaim",
		Name:       name,
		Status: map[string]any{
			"captureProxyNeighbor": map[string]any{"address": address, "interface": ifname},
		},
	}
}

func assertSAMCalls(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("calls = %#v, want %#v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("calls = %#v, want %#v", got, want)
		}
	}
}
