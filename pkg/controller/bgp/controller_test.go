// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"routerd/pkg/api"
	bgpstate "routerd/pkg/bgp"
	"routerd/pkg/bus"
	"routerd/pkg/render"
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
				return []byte(`{"routes":{"10.0.0.200/32":[{"valid":true,"bestpath":true,"community":{"string":"64513:100 no-export"}}]}}`), nil
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
	communities, ok := status["observedCommunities"].([]string)
	if !ok || !reflect.DeepEqual(communities, []string{"64513:100", "no-export"}) {
		t.Fatalf("observed communities = %#v", status["observedCommunities"])
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

func TestReconcileDryRunDoesNotWriteFRRConfig(t *testing.T) {
	path := t.TempDir() + "/routerd.conf"
	if err := os.WriteFile(path, []byte("old config\n"), 0644); err != nil {
		t.Fatal(err)
	}
	controller := Controller{
		Router:     bgpRouter(),
		Store:      mapStore{},
		DryRun:     true,
		ConfigPath: path,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old config\n" {
		t.Fatalf("dry-run rewrote config: %q", string(data))
	}
	status := controller.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "lan")
	if status["changed"] != true || status["dryRun"] != true {
		t.Fatalf("dry-run status = %#v", status)
	}
}

func TestReconcileReloadsFRROnFirstLiveObserveWhenRunningConfigDiffers(t *testing.T) {
	path := t.TempDir() + "/routerd.conf"
	data, err := render.FRRConfig(bgpRouter())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	controller := Controller{
		Router:     bgpRouter(),
		Store:      mapStore{},
		ConfigPath: path,
		VTYSH:      "vtysh",
		FRRReload:  "frr-reload.py",
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			switch {
			case name == "vtysh" && strings.Join(args, " ") == "-c show running-config":
				return []byte("router bgp 64599\n neighbor 10.0.0.21 remote-as 64599\n"), nil
			case name == "vtysh" && reflect.DeepEqual(args[:2], []string{"-C", "-f"}):
				return []byte("ok"), nil
			case name == "frr-reload.py":
				return []byte("reloaded"), nil
			case name == "vtysh" && strings.Join(args, " ") == "-c show bgp summary json":
				return []byte(`{"ipv4Unicast":{"peers":{"10.0.0.21":{"remoteAs":64513,"state":"Established","pfxRcd":1}}}}`), nil
			case name == "vtysh" && strings.Join(args, " ") == "-c show bgp ipv4 unicast json":
				return []byte(`{"routes":{}}`), nil
			default:
				t.Fatalf("unexpected command: %s %v", name, args)
				return nil, nil
			}
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	wantFirst := []string{
		"vtysh -c show running-config",
		"vtysh -C -f " + path,
		"frr-reload.py --reload " + path,
		"vtysh -c show bgp summary json",
		"vtysh -c show bgp ipv4 unicast json",
	}
	if !reflect.DeepEqual(calls, wantFirst) {
		t.Fatalf("first calls = %#v, want %#v", calls, wantFirst)
	}
	calls = nil
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	wantSecond := []string{
		"vtysh -c show bgp summary json",
		"vtysh -c show bgp ipv4 unicast json",
	}
	if !reflect.DeepEqual(calls, wantSecond) {
		t.Fatalf("second calls = %#v, want %#v", calls, wantSecond)
	}
}

func TestReconcileSkipsInitialReloadWhenRunningConfigMatches(t *testing.T) {
	path := t.TempDir() + "/routerd.conf"
	data, err := render.FRRConfig(bgpRouter())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	controller := Controller{
		Router:     bgpRouter(),
		Store:      mapStore{},
		ConfigPath: path,
		VTYSH:      "vtysh",
		FRRReload:  "frr-reload.py",
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			switch {
			case name == "vtysh" && strings.Join(args, " ") == "-c show running-config":
				return data, nil
			case name == "vtysh" && strings.Join(args, " ") == "-c show bgp summary json":
				return []byte(`{"ipv4Unicast":{"peers":{"10.0.0.21":{"remoteAs":64513,"state":"Established","pfxRcd":1}}}}`), nil
			case name == "vtysh" && strings.Join(args, " ") == "-c show bgp ipv4 unicast json":
				return []byte(`{"routes":{}}`), nil
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
		"vtysh -c show running-config",
		"vtysh -c show bgp summary json",
		"vtysh -c show bgp ipv4 unicast json",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestReconcileSeparatesMultiInstanceRouterStatus(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VRF"}, Metadata: api.ObjectMeta{Name: "wan-peering"}, Spec: api.VRFSpec{IfName: "vrf-wan", RouteTable: 65001}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{ASN: 64512, RouterID: "10.0.0.1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.BGPRouterSpec{ASN: 65001, RouterID: "192.0.2.1", VRF: "wan-peering"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "lan-speaker"}, Spec: api.BGPPeerSpec{RouterRef: "BGPRouter/lan", PeerASN: 64513, Peers: []string{"10.0.0.21"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "wan-upstream"}, Spec: api.BGPPeerSpec{RouterRef: "BGPRouter/wan", PeerASN: 65002, Peers: []string{"192.0.2.254"}}},
	}}}
	path := t.TempDir() + "/routerd.conf"
	data, err := render.FRRConfig(router)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	controller := Controller{
		Router:     router,
		Store:      mapStore{},
		ConfigPath: path,
		VTYSH:      "vtysh",
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "vtysh" {
				t.Fatalf("unexpected command: %s %v", name, args)
			}
			switch strings.Join(args, " ") {
			case "-c show running-config":
				return data, nil
			case "-c show bgp summary json":
				return []byte(`{"ipv4Unicast":{"peers":{"10.0.0.21":{"remoteAs":64513,"state":"Established","pfxRcd":1}}}}`), nil
			case "-c show bgp ipv4 unicast json":
				return []byte(`{"routes":{"10.0.0.200/32":[{"valid":true,"bestpath":true}]}}`), nil
			case "-c show bgp vrf vrf-wan summary json":
				return []byte(`{"ipv4Unicast":{"peers":{"192.0.2.254":{"remoteAs":65002,"state":"Established","pfxRcd":1}}}}`), nil
			case "-c show bgp vrf vrf-wan ipv4 unicast json":
				return []byte(`{"routes":{"198.51.100.0/24":[{"valid":true,"bestpath":true}]}}`), nil
			default:
				t.Fatalf("unexpected command: %s %v", name, args)
				return nil, nil
			}
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	lanStatus := controller.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "lan")
	wanStatus := controller.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "wan")
	lanPeers := lanStatus["peers"].([]bgpstate.Peer)
	wanPeers := wanStatus["peers"].([]bgpstate.Peer)
	if len(lanPeers) != 1 || lanPeers[0].Address != "10.0.0.21" {
		t.Fatalf("lan peers = %#v", lanPeers)
	}
	if len(wanPeers) != 1 || wanPeers[0].Address != "192.0.2.254" || wanStatus["vrf"] != "vrf-wan" {
		t.Fatalf("wan status = %#v", wanStatus)
	}
}

func TestReconcileObservesBFDAndWritesFRRDaemons(t *testing.T) {
	router := bgpBFDRouter()
	configPath := t.TempDir() + "/routerd.conf"
	daemonsPath := t.TempDir() + "/daemons"
	var calls []string
	controller := Controller{
		Router:      router,
		Store:       mapStore{},
		ConfigPath:  configPath,
		DaemonsPath: daemonsPath,
		VTYSH:       "vtysh",
		FRRReload:   "frr-reload.py",
		Systemctl:   "systemctl",
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			switch {
			case name == "vtysh" && reflect.DeepEqual(args[:2], []string{"-C", "-f"}):
				return []byte("ok"), nil
			case name == "frr-reload.py":
				return []byte("reloaded"), nil
			case name == "systemctl" && strings.Join(args, " ") == "restart frr.service":
				return []byte("restarted"), nil
			case name == "vtysh" && strings.Join(args, " ") == "-c show bgp summary json":
				return []byte(`{"ipv4Unicast":{"peers":{"10.0.0.21":{"remoteAs":64513,"state":"Established","pfxRcd":1}}}}`), nil
			case name == "vtysh" && strings.Join(args, " ") == "-c show bgp ipv4 unicast json":
				return []byte(`{"routes":{}}`), nil
			case name == "vtysh" && strings.Join(args, " ") == "-c show bfd peers brief json":
				return []byte(`{"peers":{"10.0.0.21":{"status":"up","lastUp":"2026-05-19T00:00:00Z"}}}`), nil
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
		"vtysh -C -f " + configPath,
		"frr-reload.py --reload " + configPath,
		"systemctl restart frr.service",
		"vtysh -c show bgp summary json",
		"vtysh -c show bgp ipv4 unicast json",
		"vtysh -c show bfd peers brief json",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	daemons, err := os.ReadFile(daemonsPath)
	if err != nil {
		t.Fatalf("read daemons: %v", err)
	}
	if !strings.Contains(string(daemons), "bgpd=yes") || !strings.Contains(string(daemons), "bfdd=yes") {
		t.Fatalf("daemons = %s", daemons)
	}
	status := controller.Store.ObjectStatus(api.NetAPIVersion, "BGPPeer", "k8s")
	peers, ok := status["peers"].([]bgpstate.Peer)
	if !ok || len(peers) != 1 || peers[0].BFD == nil || peers[0].BFD.State != "up" {
		t.Fatalf("BFD peer status = %#v", status["peers"])
	}
}

func TestPollIntervalUsesBGPRouterWatcher(t *testing.T) {
	router := bgpRouter()
	spec := router.Spec.Resources[0].Spec.(api.BGPRouterSpec)
	spec.Watcher.PollInterval = "5s"
	router.Spec.Resources[0].Spec = spec
	if got := PollInterval(router); got != 5*time.Second {
		t.Fatalf("poll interval = %v", got)
	}
	spec.Watcher.PollInterval = "2s"
	router.Spec.Resources[0].Spec = spec
	if got := PollInterval(router); got != 15*time.Second {
		t.Fatalf("poll interval with too-low value = %v", got)
	}
}

func TestCriticalFRRLinesIgnoresDefaultGracefulRestartTimers(t *testing.T) {
	lines := criticalFRRLines([]byte(`
! Generated by routerd. Do not edit by hand.
router bgp 64512
 bgp graceful-restart
 bgp graceful-restart restart-time 120
 bgp graceful-restart stalepath-time 360
 neighbor 10.0.0.21 remote-as 64513
`))
	for _, line := range lines {
		if strings.Contains(line, "restart-time 120") || strings.Contains(line, "stalepath-time 360") {
			t.Fatalf("default graceful restart timer line was critical: %#v", lines)
		}
	}
	if !reflect.DeepEqual(lines, []string{
		"router bgp 64512",
		"bgp graceful-restart",
		"neighbor 10.0.0.21 remote-as 64513",
	}) {
		t.Fatalf("lines = %#v", lines)
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

func bgpBFDRouter() *api.Router {
	enabled := true
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec: api.BGPRouterSpec{
				ASN:      64512,
				RouterID: "10.0.0.1",
				Watcher:  api.BGPWatcherSpec{MaxPrefixes: 2, PeerStateChangeThrottle: "5s"},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: "k8s"},
			Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/lan",
				PeerASN:   64513,
				Peers:     []string{"10.0.0.21"},
				BFD: api.BGPBFDSpec{
					Enabled:          &enabled,
					MinRxInterval:    "300ms",
					MinTxInterval:    "300ms",
					DetectMultiplier: 3,
				},
			},
		},
	}}}
}
