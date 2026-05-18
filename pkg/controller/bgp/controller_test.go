// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"routerd/pkg/api"
	bgpstate "routerd/pkg/bgp"
	"routerd/pkg/bus"
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

func TestReconcileValidatesAndReloadsFRR(t *testing.T) {
	var calls []string
	controller := Controller{
		Router:     bgpRouter(),
		Store:      mapStore{},
		DryRun:     false,
		ConfigPath: t.TempDir() + "/routerd.conf",
		VTYSH:      "vtysh",
		FRRReload:  "frr-reload.py",
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			switch {
			case name == "vtysh" && reflect.DeepEqual(args[:2], []string{"-C", "-f"}):
				return []byte("ok"), nil
			case name == "frr-reload.py":
				return []byte("reloaded"), nil
			case name == "vtysh" && strings.Join(args, " ") == "-c show bgp summary json":
				return []byte(`{"ipv4Unicast":{"peers":{"10.0.0.21":{"remoteAs":64513,"state":"Established","pfxRcd":1,"lastConnectionEstablished":"2026-05-18T10:00:00Z"}}}}`), nil
			case name == "vtysh" && strings.Join(args, " ") == "-c show bgp ipv4 unicast json":
				return []byte(`{"routes":{"10.0.0.200/32":[{"valid":true,"bestpath":true}]}}`), nil
			default:
				t.Fatalf("unexpected command: %s %v", name, args)
				return nil, nil
			}
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []string{
		"vtysh -C -f " + controller.ConfigPath,
		"frr-reload.py --reload " + controller.ConfigPath,
		"vtysh -c show bgp summary json",
		"vtysh -c show bgp ipv4 unicast json",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	status := controller.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "lan")
	if status["phase"] != "Established" {
		t.Fatalf("status = %#v", status)
	}
	peers, ok := status["peers"].([]bgpstate.Peer)
	if !ok || len(peers) != 1 || peers[0].LastEstablishedAt != "2026-05-18T10:00:00Z" {
		t.Fatalf("peer history = %#v", status["peers"])
	}
}

func TestReconcileSkipsInitialBGPDiffEvents(t *testing.T) {
	eventBus := bus.New()
	controller := Controller{
		Router:     bgpRouter(),
		Bus:        eventBus,
		Store:      mapStore{},
		ConfigPath: t.TempDir() + "/routerd.conf",
		VTYSH:      "vtysh",
		FRRReload:  "frr-reload.py",
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			switch {
			case name == "vtysh" && reflect.DeepEqual(args[:2], []string{"-C", "-f"}):
				return []byte("ok"), nil
			case name == "frr-reload.py":
				return []byte("reloaded"), nil
			case name == "vtysh" && strings.Join(args, " ") == "-c show bgp summary json":
				return []byte(`{"ipv4Unicast":{"peers":{"10.0.0.21":{"remoteAs":64513,"state":"Established","pfxRcd":1}}}}`), nil
			case name == "vtysh" && strings.Join(args, " ") == "-c show bgp ipv4 unicast json":
				return []byte(`{"routes":{"10.0.0.200/32":[{"valid":true,"bestpath":true}]}}`), nil
			default:
				t.Fatalf("unexpected command: %s %v", name, args)
				return nil, nil
			}
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := eventBus.Recent("routerd.bgp.peer.up"); len(got) != 0 {
		t.Fatalf("initial observe emitted peer-up events: %#v", got)
	}
	if got := eventBus.Recent("routerd.bgp.prefix.accepted"); len(got) != 0 {
		t.Fatalf("initial observe emitted prefix events: %#v", got)
	}
}

func bgpRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec: api.BGPRouterSpec{
				ASN:          64512,
				RouterID:     "10.0.0.1",
				ImportPolicy: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.0.0.200/29"}},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: "k8s"},
			Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/lan",
				PeerASN:   64513,
				Peers:     []string{"10.0.0.21"},
			},
		},
	}}}
}
