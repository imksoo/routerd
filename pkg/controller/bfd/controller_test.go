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
	for _, forbidden := range []string{"configure terminal", "\nend\n", "local-address"} {
		if strings.Contains(config, forbidden) {
			t.Fatalf("rendered single-hop config contains forbidden %q:\n%s", forbidden, config)
		}
	}
	for _, want := range []string{
		"peer 10.99.0.2 interface wg0",
		"peer 10.99.0.3 interface wg0",
		"receive-interval 300",
		"transmit-interval 300",
		"detect-multiplier 3",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, config)
		}
	}
}

func TestRenderFRRConfigUsesFRRFileSyntaxAndMultihopLocalAddress(t *testing.T) {
	router := bfdMultihopRouter()
	controller := Controller{Router: router, Store: testStore{}}
	sessions, _, err := controller.sessions()
	if err != nil {
		t.Fatalf("sessions: %v", err)
	}
	config := RenderFRRConfig(sessions)
	if strings.Contains(config, "configure terminal") || strings.Contains(config, "\nend\n") {
		t.Fatalf("rendered config must be vtysh -f/frr.conf syntax, got:\n%s", config)
	}
	for _, want := range []string{
		"bfd\n",
		" peer 10.99.0.2 multihop local-address 10.99.0.1\n",
		" peer 10.99.0.3 multihop local-address 10.99.0.1\n",
		"  receive-interval 300\n",
		"  transmit-interval 300\n",
		"  detect-multiplier 3\n",
		" exit\n",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("rendered multihop config missing %q:\n%s", want, config)
		}
	}
}

func TestRenderFRRConfigCloudRRMultihopLocalAddress(t *testing.T) {
	router := cloudRRRouter("10.99.0.3")
	controller := Controller{Router: router, Store: testStore{}}
	sessions, byBFD, err := controller.sessions()
	if err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1; sessions=%#v", len(sessions), sessions)
	}
	if len(byBFD["onprem-rr"]) != 1 {
		t.Fatalf("byBFD[onprem-rr] = %#v, want one session", byBFD["onprem-rr"])
	}
	config := RenderFRRConfig(sessions)
	want := " peer 10.99.0.1 multihop local-address 10.99.0.3\n"
	if !strings.Contains(config, want) {
		t.Fatalf("rendered cloud RR config missing %q:\n%s", want, config)
	}
}

func TestSessionsRecoverFromBGPPeerBFDReferenceWithoutBFDResource(t *testing.T) {
	router := cloudRRRouter("10.99.0.4")
	router.Spec.Resources = router.Spec.Resources[:2]
	controller := Controller{Router: router, Store: testStore{}}
	sessions, byBFD, err := controller.sessions()
	if err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].BFDName != "onprem-rr" || sessions[0].Address != "10.99.0.1" || sessions[0].LocalAddr != "10.99.0.4" {
		t.Fatalf("sessions = %#v, want synthesized onprem-rr session", sessions)
	}
	if len(byBFD["onprem-rr"]) != 1 {
		t.Fatalf("byBFD = %#v, want synthesized BFD bucket", byBFD)
	}
}

func TestSessionsRejectMissingBGPPeerReference(t *testing.T) {
	router := cloudRRRouter("10.99.0.3")
	bfdRes := router.Spec.Resources[2]
	spec := bfdRes.Spec.(api.BFDSpec)
	spec.Peer = "BGPPeer/missing"
	bfdRes.Spec = spec
	router.Spec.Resources[2] = bfdRes
	controller := Controller{Router: router, Store: testStore{}}
	_, _, err := controller.sessions()
	if err == nil || !strings.Contains(err.Error(), "missing BGPPeer") {
		t.Fatalf("sessions err = %v, want missing BGPPeer error", err)
	}
}

func TestSessionsResolveInterfaceResourceToKernelIfName(t *testing.T) {
	router := bfdRouter()
	controller := Controller{Router: router, Store: testStore{}}
	sessions, _, err := controller.sessions()
	if err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if len(sessions) != 2 || sessions[0].Interface != "wg0" || sessions[1].Interface != "wg0" {
		t.Fatalf("sessions = %#v, want Interface/wg-hybrid resolved to wg0", sessions)
	}
}

func TestSessionsRejectUnknownInterfaceResource(t *testing.T) {
	router := bfdRouter()
	iface := router.Spec.Resources[3]
	spec := iface.Spec.(api.InterfaceSpec)
	spec.IfName = ""
	iface.Spec = spec
	router.Spec.Resources[3] = iface
	controller := Controller{Router: router, Store: testStore{}}
	if _, _, err := controller.sessions(); err == nil || !strings.Contains(err.Error(), "missing or empty Interface/wg-hybrid") {
		t.Fatalf("sessions error = %v, want missing Interface resource", err)
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

func TestReconcileFreeBSDAppliesFRRWithFreeBSDVtysh(t *testing.T) {
	store := testStore{}
	var commands []string
	controller := Controller{
		Router:     bfdRouter(),
		Store:      store,
		OS:         platform.OSFreeBSD,
		RuntimeDir: t.TempDir(),
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			if len(args) == 2 && args[0] == "-c" && args[1] == "show bfd peers json" {
				return []byte(`{"10.99.0.2":{"state":"up"},"10.99.0.3":{"state":"up"}}`), nil
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(commands) != 2 || !strings.HasPrefix(commands[0], "/usr/local/bin/vtysh -f ") || commands[1] != "/usr/local/bin/vtysh -c show bfd peers json" {
		t.Fatalf("commands = %#v", commands)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "BFD", "fabric")
	if status["phase"] != "Up" {
		t.Fatalf("status = %#v, want Up", status)
	}
}

func TestReconcileUnsupportedOSStatus(t *testing.T) {
	store := testStore{}
	controller := Controller{Router: bfdRouter(), Store: store, OS: platform.OSOther}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "BFD", "fabric")
	if status["phase"] != "Unsupported" || status["reason"] != "BFDUnsupportedOS" {
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
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wg-hybrid"},
			Spec:     api.InterfaceSpec{IfName: "wg0", Managed: false, Owner: "external"},
		},
	}}}
}

func bfdMultihopRouter() *api.Router {
	router := bfdRouter()
	bfdRes := router.Spec.Resources[2]
	spec := bfdRes.Spec.(api.BFDSpec)
	spec.Interface = ""
	bfdRes.Spec = spec
	router.Spec.Resources[2] = bfdRes
	return router
}

func cloudRRRouter(routerID string) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
			Metadata: api.ObjectMeta{Name: "mobility-bgp"},
			Spec: api.BGPRouterSpec{
				ASN:      64577,
				RouterID: routerID,
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: "onprem-rr"},
			Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/mobility-bgp",
				PeerASN:   64577,
				Peers:     []string{"10.99.0.1"},
				BFD:       "BFD/onprem-rr",
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BFD"},
			Metadata: api.ObjectMeta{Name: "onprem-rr"},
			Spec: api.BFDSpec{
				Peer:    "BGPPeer/onprem-rr",
				Profile: "fast",
			},
		},
	}}}
}
