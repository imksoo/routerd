// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"errors"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/sam"
)

func TestSAMControllerHoldRetainsPreviouslyAppliedDataplane(t *testing.T) {
	const (
		intentID = "cloudedge/10.77.60.9"
		address  = "10.77.60.9/32"
	)
	store := &mergeTrackingMapStore{mapStore: mapStore{
		api.RouterAPIVersion + "/Router/" + samDataplaneStatusName: {
			"appliedProxyNeighbors": []samAppliedProxyNeighbor{{ID: intentID, PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Address: address, Interface: "ens3"}},
			"appliedForwardPaths":   []samAppliedForwardPath{{ID: intentID, PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Kind: "forward-path", Address: address, Interface: "ens3", PeerInterface: "wg-cloud"}},
		},
	}}
	applier := &fakeSAMApplier{}
	controller := SAMController{
		Router: &api.Router{},
		Store:  store,
		LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{{
			ID: intentID, PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Address: address, Disposition: dynamicconfig.CaptureHold,
			CaptureType: "provider-secondary-ip", CaptureInterface: "ens3", TunnelInterfaces: []string{"wg-cloud"},
		}},
		OS:      platform.OSLinux,
		Applier: applier,
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(applier.delete) != 0 || len(applier.ensure) != 0 || len(applier.deassign) != 0 {
		t.Fatalf("hold changed neighbor state: delete=%#v ensure=%#v deassign=%#v", applier.delete, applier.ensure, applier.deassign)
	}
	if len(applier.ipForwarding) != 1 || applier.ipForwarding[0] != "1" {
		t.Fatalf("hold forwarding = %#v, want retained enabled forwarding", applier.ipForwarding)
	}
	if len(applier.forwardSets) != 1 || len(applier.forwardSets[0]) != 1 {
		t.Fatalf("held forward paths = %#v, want one retained path", applier.forwardSets)
	}
	path := applier.forwardSets[0][0]
	if path.Kind != "forward-path" || path.IntentID != intentID || path.Address != address || path.Interface != "ens3" || path.PeerInterface != "wg-cloud" {
		t.Fatalf("held forward path = %#v", path)
	}
	status := store.ObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName)
	if got, err := samAppliedProxyNeighbors(status["appliedProxyNeighbors"], true); err != nil || len(got) != 1 || got[0].ID != intentID {
		t.Fatalf("retained proxy-neighbor status = %#v, err=%v", got, err)
	}
	if got, err := samAppliedForwardPaths(status["appliedForwardPaths"], true); err != nil || len(got) != 1 || got[0].ID != intentID {
		t.Fatalf("retained forward-path status = %#v, err=%v", got, err)
	}
}

func TestSAMControllerAppliesProxyARPTunnelForwardPaths(t *testing.T) {
	const (
		intentID = "mobility-svnet1-192-168-123-111"
		address  = "192.168.123.111/32"
	)
	store := &mergeTrackingMapStore{mapStore: mapStore{}}
	applier := &fakeSAMApplier{}
	controller := SAMController{
		Router: &api.Router{}, Store: store, OS: platform.OSLinux, Applier: applier,
		LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{{
			ID: intentID, PoolRef: "svnet1", PoolPrefix: "192.168.123.0/24", Address: address,
			Disposition: dynamicconfig.CaptureDesired, CaptureType: "proxy-arp", CaptureInterface: "ens19",
			TunnelInterfaces: []string{"samt-rr-a", "samt-rr-b"},
		}},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(applier.ensure) != 1 || applier.ensure[0] != address+"@ens19" || len(applier.ipForwarding) != 1 || applier.ipForwarding[0] != "1" {
		t.Fatalf("proxy capture effects = %#v", applier.calls)
	}
	if len(applier.forwardSets) != 1 || len(applier.forwardSets[0]) != 2 {
		t.Fatalf("forward paths = %#v", applier.forwardSets)
	}
	for index, tunnel := range []string{"samt-rr-a", "samt-rr-b"} {
		path := applier.forwardSets[0][index]
		if path.Kind != "forward-local-path" || path.IntentID != intentID || path.Address != address || path.Interface != "ens19" || path.PeerInterface != tunnel {
			t.Fatalf("forward path[%d] = %#v", index, path)
		}
	}
	status := store.ObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName)
	paths, err := samAppliedForwardPaths(status["appliedForwardPaths"], true)
	if err != nil || len(paths) != 2 || paths[0].Kind != "forward-local-path" || paths[1].Kind != "forward-local-path" {
		t.Fatalf("persisted proxy forward paths = %#v, err=%v", paths, err)
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
}

func TestSAMControllerRejectsMalformedAppliedLedgerBeforeEffects(t *testing.T) {
	store := &mergeTrackingMapStore{mapStore: mapStore{
		api.RouterAPIVersion + "/Router/" + samDataplaneStatusName: {
			// PoolRef/PoolPrefix are deliberately missing: this old or corrupt
			// row must not authorize a host deletion or a new capture.
			"appliedProxyNeighbors": []samAppliedProxyNeighbor{{ID: "capture-a", Address: "10.77.60.9/32", Interface: "ens3"}},
		},
	}}
	applier := &fakeSAMApplier{}
	controller := SAMController{
		Router: &api.Router{}, Store: store, OS: platform.OSLinux, Applier: applier,
		LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{{
			ID: "capture-a", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Address: "10.77.60.9/32",
			Disposition: dynamicconfig.CaptureDesired, CaptureType: "provider-secondary-ip", CaptureInterface: "ens3", TunnelInterfaces: []string{"wg-cloud"},
		}},
	}
	if err := controller.Reconcile(t.Context()); err == nil {
		t.Fatal("malformed applied ledger was accepted")
	}
	if len(applier.calls) != 0 {
		t.Fatalf("malformed ledger reached host effector: %#v", applier.calls)
	}
}

func TestSAMControllerHoldRetainsOnlyExactPreviouslyAppliedEffects(t *testing.T) {
	store := &mergeTrackingMapStore{mapStore: mapStore{
		api.RouterAPIVersion + "/Router/" + samDataplaneStatusName: {
			"appliedProxyNeighbors": []samAppliedProxyNeighbor{{
				ID: "capture-a", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Address: "10.77.60.10/32", Interface: "ens3",
			}},
		},
	}}
	applier := &fakeSAMApplier{}
	controller := SAMController{
		Router: &api.Router{}, Store: store, OS: platform.OSLinux, Applier: applier,
		LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{{
			ID: "capture-a", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Address: "10.77.60.9/32",
			Disposition: dynamicconfig.CaptureHold, CaptureType: "provider-secondary-ip", CaptureInterface: "ens3", TunnelInterfaces: []string{"wg-cloud"},
		}},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(applier.ensure) != 0 || len(applier.forwardSets) != 1 || len(applier.forwardSets[0]) != 0 {
		t.Fatalf("mismatched hold recreated an effect: ensure=%#v forward=%#v", applier.ensure, applier.forwardSets)
	}
	if len(applier.delete) != 1 || applier.delete[0] != "10.77.60.10/32@ens3" {
		t.Fatalf("mismatched held ledger was not safely released: %#v", applier.delete)
	}
}

func TestSAMControllerDoesNotAdoptForeignProxyARP(t *testing.T) {
	foreign := samProxyARPApplyResult{}
	applier := &fakeSAMApplier{proxyARPResult: &foreign}
	store := &mergeTrackingMapStore{mapStore: mapStore{}}
	controller := SAMController{
		Router: &api.Router{}, Store: store, OS: platform.OSLinux, Applier: applier,
		LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{{
			ID: "capture-a", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Address: "10.77.60.9/32",
			Disposition: dynamicconfig.CaptureDesired, CaptureType: "proxy-arp", CaptureInterface: "ens3",
		}},
	}
	err := controller.Reconcile(t.Context())
	if err == nil || !strings.Contains(err.Error(), "ownership proof") {
		t.Fatalf("foreign proxy_arp adoption error = %v", err)
	}
	if len(applier.ensure) != 0 || len(applier.forwardSets) != 0 || len(applier.ipForwarding) != 0 {
		t.Fatalf("foreign proxy_arp proceeded to capture: %#v", applier.calls)
	}
}

func TestSAMControllerDoesNotForwardAfterDeassignFailure(t *testing.T) {
	applier := &fakeSAMApplier{deassignErr: errors.New("address busy")}
	store := &mergeTrackingMapStore{mapStore: mapStore{}}
	controller := SAMController{
		Router: &api.Router{}, Store: store, OS: platform.OSLinux, Applier: applier,
		LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{{
			ID: "capture-a", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Address: "10.77.60.9/32",
			Disposition: dynamicconfig.CaptureDesired, CaptureType: "provider-secondary-ip", CaptureInterface: "ens3", TunnelInterfaces: []string{"wg-cloud"},
		}},
	}
	if err := controller.Reconcile(t.Context()); err == nil {
		t.Fatal("deassign failure was accepted")
	}
	if len(applier.proxyARP) != 0 || len(applier.ipForwarding) != 0 || len(applier.forwardSets) != 0 || len(applier.ensure) != 0 {
		t.Fatalf("deassign failure reached later effects: %#v", applier.calls)
	}
}

type failingSAMStatusStore struct {
	mapStore
	err error
}

func (s failingSAMStatusStore) MergeObjectStatus(string, string, string, map[string]any) error {
	return s.err
}

func TestSAMControllerReturnsAppliedLedgerWriteFailure(t *testing.T) {
	store := failingSAMStatusStore{mapStore: mapStore{}, err: errors.New("sqlite unavailable")}
	controller := SAMController{
		Router: &api.Router{}, Store: store, OS: platform.OSLinux, Applier: &fakeSAMApplier{},
		LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{{
			ID: "capture-a", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Address: "10.77.60.9/32",
			Disposition: dynamicconfig.CaptureDesired, CaptureType: "proxy-arp", CaptureInterface: "ens3",
		}},
	}
	err := controller.Reconcile(t.Context())
	if err == nil || !strings.Contains(err.Error(), "sqlite unavailable") {
		t.Fatalf("applied ledger write failure = %v", err)
	}
}

func TestSAMControllerFreeBSDForwardingUsesResolvedTargetOS(t *testing.T) {
	applier := &fakeSAMApplier{}
	controller := SAMController{Store: mapStore{}, Applier: applier}
	actions := []sam.CaptureAction{
		{Kind: "sysctl", Key: "net.inet.ip.forwarding", Value: "1"},
		{Kind: "forward-path", IntentID: "cloudedge/10.77.60.9", Address: "10.77.60.9/32", Interface: "em0", PeerInterface: "gif0"},
	}
	state, err := controller.reconcileState(actions, platform.OSFreeBSD)
	if err != nil {
		t.Fatalf("reconcile state: %v", err)
	}
	if err := controller.reconcileSAMIPForwarding(t.Context(), platform.OSFreeBSD, actions, state); err != nil {
		t.Fatalf("reconcileSAMIPForwarding: %v", err)
	}
	if len(applier.ipForwarding) != 1 || applier.ipForwarding[0] != "1" {
		t.Fatalf("FreeBSD forwarding calls = %#v, want enabled", applier.ipForwarding)
	}
}

type countingSAMStatusStore struct {
	mapStore
	reads int
}

func (s *countingSAMStatusStore) MergeObjectStatus(apiVersion, kind, name string, updates map[string]any) error {
	current := copyStatusMap(s.mapStore.ObjectStatus(apiVersion, kind, name))
	for key, value := range updates {
		current[key] = value
	}
	return s.mapStore.SaveObjectStatus(apiVersion, kind, name, current)
}

func (s *countingSAMStatusStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if apiVersion == api.RouterAPIVersion && kind == "Router" && name == samDataplaneStatusName {
		s.reads++
	}
	return s.mapStore.ObjectStatus(apiVersion, kind, name)
}

func TestSAMControllerReadsAppliedDataplaneOncePerReconcile(t *testing.T) {
	const (
		intentID = "cloudedge/10.77.60.9"
		address  = "10.77.60.9/32"
	)
	store := &countingSAMStatusStore{mapStore: mapStore{
		api.RouterAPIVersion + "/Router/" + samDataplaneStatusName: {
			"appliedProxyNeighbors": []samAppliedProxyNeighbor{{ID: intentID, PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Address: address, Interface: "ens3"}},
			"appliedForwardPaths":   []samAppliedForwardPath{{ID: intentID, PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Kind: "forward-path", Address: address, Interface: "ens3", PeerInterface: "wg-cloud"}},
		},
	}}
	controller := SAMController{
		Router: &api.Router{}, Store: store, OS: platform.OSLinux, Applier: &fakeSAMApplier{},
		LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{{
			ID: intentID, PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Address: address, Disposition: dynamicconfig.CaptureHold,
			CaptureType: "provider-secondary-ip", CaptureInterface: "ens3", TunnelInterfaces: []string{"wg-cloud"},
		}},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if store.reads != 1 {
		t.Fatalf("sam-dataplane status reads = %d, want one snapshot per reconcile", store.reads)
	}
}
