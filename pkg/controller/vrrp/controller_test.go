// SPDX-License-Identifier: BSD-3-Clause

package vrrp

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"routerd/pkg/api"
)

type mapStore map[string]map[string]any

func (s mapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if status := s[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func TestReconcileLowersVRRPPriorityAfterTrackHysteresis(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/BGPRouter/lan": {"phase": "Degraded"},
	}
	var calls []string
	controller := Controller{
		Router:     vrrpRouter("vrrp"),
		Store:      store,
		DryRun:     false,
		ConfigPath: t.TempDir() + "/keepalived.conf",
		Systemctl:  "systemctl",
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualIPv4Address", "vip")
	if status["priority"] != 150 {
		t.Fatalf("priority should not drop before confirm threshold: %#v", status)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("third reconcile: %v", err)
	}
	if len(calls) == 0 {
		t.Fatal("expected keepalived reload calls")
	}
	status = store.ObjectStatus(api.NetAPIVersion, "VirtualIPv4Address", "vip")
	if status["priority"] != 100 {
		t.Fatalf("priority status = %#v", status)
	}
}

func TestReconcileRestoresVRRPPriorityAfterHealthyHysteresis(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/BGPRouter/lan": {"phase": "Degraded"},
	}
	controller := Controller{Router: vrrpRouter("vrrp"), Store: store, DryRun: true, ConfigPath: t.TempDir() + "/keepalived.conf"}
	for i := 0; i < 3; i++ {
		if err := controller.Reconcile(context.Background()); err != nil {
			t.Fatalf("unhealthy reconcile %d: %v", i, err)
		}
	}
	store[api.NetAPIVersion+"/BGPRouter/lan"] = map[string]any{"phase": "Established"}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first healthy reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualIPv4Address", "vip")
	if status["priority"] != 100 {
		t.Fatalf("priority should remain penalized before healthy threshold: %#v", status)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second healthy reconcile: %v", err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "VirtualIPv4Address", "vip")
	if status["priority"] != 150 {
		t.Fatalf("priority should restore after healthy threshold: %#v", status)
	}
}

func TestReconcileRestoresTrackHysteresisFromStore(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/BGPRouter/lan": {"phase": "Degraded"},
		api.NetAPIVersion + "/VirtualIPv4Address/vip": {
			"track": []map[string]any{{
				"resource":       "BGPRouter/lan",
				"penalized":      true,
				"healthyCount":   0,
				"unhealthyCount": 3,
			}},
		},
	}
	controller := Controller{Router: vrrpRouter("vrrp"), Store: store, DryRun: true, ConfigPath: t.TempDir() + "/keepalived.conf"}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualIPv4Address", "vip")
	if status["priority"] != 100 {
		t.Fatalf("priority should stay penalized after restart restore: %#v", status)
	}
	track, ok := status["track"].([]map[string]any)
	if !ok || len(track) != 1 || track[0]["unhealthyCount"] != 4 {
		t.Fatalf("track state was not restored and advanced: %#v", status["track"])
	}
}

func TestReconcileAppliesStaticVirtualIPv4Address(t *testing.T) {
	store := mapStore{}
	var calls []string
	controller := Controller{
		Router: vrrpRouter("static"),
		Store:  store,
		IP:     "ip",
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []string{"ip addr replace 10.240.70.10/32 dev ens18"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func vrrpRouter(mode string) *api.Router {
	track := []api.ResourceTrackSpec(nil)
	vrrpSpec := api.VirtualIPv4VRRPSpec{}
	if mode == "vrrp" {
		track = []api.ResourceTrackSpec{{Resource: "BGPRouter/lan", UnhealthyPenalty: 50}}
		vrrpSpec = api.VirtualIPv4VRRPSpec{VirtualRouterID: 50, Priority: 150, Peers: []string{"10.240.70.3"}}
	}
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "ens18"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv4Address"},
			Metadata: api.ObjectMeta{Name: "vip"},
			Spec: api.VirtualIPv4AddressSpec{
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      mode,
				VRRP:      vrrpSpec,
				Track:     track,
			},
		},
	}}}
}
