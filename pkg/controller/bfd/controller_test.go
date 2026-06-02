// SPDX-License-Identifier: BSD-3-Clause

package bfd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

type testStore map[string]map[string]any

func (s testStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s testStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	return s[apiVersion+"/"+kind+"/"+name]
}

func TestRenderFRRConfigUsesBGPPeerAddresses(t *testing.T) {
	router := bfdRouter()
	controller := Controller{Router: router, Store: testStore{}}
	sessions, _, err := controller.sessions()
	if err != nil {
		t.Fatalf("sessions: %v", err)
	}
	config := RenderFRRConfig(sessions)
	for _, want := range []string{
		"peer 10.99.0.2 interface wg-hybrid",
		"peer 10.99.0.3 interface wg-hybrid",
		"receive-interval 300",
		"transmit-interval 300",
		"detect-multiplier 3",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, config)
		}
	}
}

func TestParseFRRBFDPeersJSON(t *testing.T) {
	got := ParseFRRBFDPeersJSON([]byte(`{
		"10.99.0.2": {"status": "up"},
		"peers": [
			{"peer": "10.99.0.3", "state": "down"},
			{"peerAddress": "10.99.0.4", "sessionState": "admin_down"}
		]
	}`))
	want := map[string]string{
		"10.99.0.2": "Up",
		"10.99.0.3": "Down",
		"10.99.0.4": "Down",
	}
	for address, state := range want {
		if got[address] != state {
			t.Fatalf("state[%s] = %q, want %q; all=%#v", address, got[address], state, got)
		}
	}
}

func TestReconcileAppliesFRRAndSavesPeerStates(t *testing.T) {
	store := testStore{}
	var commands []string
	controller := Controller{
		Router:     bfdRouter(),
		Store:      store,
		OS:         platform.OSLinux,
		RuntimeDir: t.TempDir(),
		Now:        func() time.Time { return time.Unix(100, 0) },
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			if len(args) == 2 && args[0] == "-c" && args[1] == "show bfd peers json" {
				return []byte(`{"10.99.0.2":{"state":"up"},"10.99.0.3":{"state":"down"}}`), nil
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(commands) != 2 || !strings.Contains(commands[0], "-f ") || !strings.Contains(commands[1], "show bfd peers json") {
		t.Fatalf("commands = %#v", commands)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "BFD", "fabric")
	if status["phase"] != "Degraded" {
		t.Fatalf("phase = %v, want Degraded; status=%#v", status["phase"], status)
	}
	states := status["peerStates"].(map[string]string)
	if states["10.99.0.2"] != "Up" || states["10.99.0.3"] != "Down" {
		t.Fatalf("peerStates = %#v", states)
	}
}

func TestReconcileLinuxOnlyStatus(t *testing.T) {
	store := testStore{}
	controller := Controller{Router: bfdRouter(), Store: store, OS: platform.OSFreeBSD}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "BFD", "fabric")
	if status["phase"] != "Unsupported" || status["reason"] != "BFDLinuxOnly" {
		t.Fatalf("status = %#v", status)
	}
}

func bfdRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
			Metadata: api.ObjectMeta{Name: "fabric"},
			Spec: api.BGPRouterSpec{
				ASN:      64512,
				RouterID: "10.99.0.1",
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: "rr"},
			Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/fabric",
				PeerASN:   64512,
				Peers:     []string{"10.99.0.2", "10.99.0.3"},
				BFD:       "BFD/fabric",
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BFD"},
			Metadata: api.ObjectMeta{Name: "fabric"},
			Spec: api.BFDSpec{
				Peer:      "BGPPeer/rr",
				Interface: "Interface/wg-hybrid",
				Profile:   "fast",
			},
		},
	}}}
}
