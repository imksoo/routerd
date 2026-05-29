// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
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
	if status["phase"] != "Ready" || status["deliveryRouteName"] != lowerings[0].IPv4RouteName || status["captureType"] != "proxy-arp" {
		t.Fatalf("status = %#v", status)
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
	if len(applier.ensure) != 0 || len(applier.delete) != 0 {
		t.Fatalf("host actions = ensure %#v delete %#v, want none", applier.ensure, applier.delete)
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

type fakeSAMApplier struct {
	ensure []string
	delete []string
}

func (a *fakeSAMApplier) EnsureProxyNeighbor(_ context.Context, address, ifname string) error {
	a.ensure = append(a.ensure, address+"@"+ifname)
	return nil
}

func (a *fakeSAMApplier) DeleteProxyNeighbor(_ context.Context, address, ifname string) error {
	a.delete = append(a.delete, address+"@"+ifname)
	return nil
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
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"}, Metadata: api.ObjectMeta{Name: "cloud"}, Spec: api.OverlayPeerSpec{
			Role:     "cloud",
			NodeID:   "cloud",
			Underlay: api.OverlayUnderlay{Type: "wireguard", Interface: "wg-sam"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "RemoteAddressClaim"}, Metadata: api.ObjectMeta{Name: "app"}, Spec: api.RemoteAddressClaimSpec{
			DomainRef: "same-subnet",
			Address:   "10.0.1.123/32",
			OwnerSide: "onprem",
			Capture:   api.AddressCapture{Type: "proxy-arp", Interface: "lan0"},
			Delivery:  api.AddressDelivery{PeerRef: "cloud", Mode: "route", TunnelInterface: "wg-sam"},
		}},
	}}}
}
