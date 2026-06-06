// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"github.com/imksoo/routerd/pkg/wireguard"
)

func mustWireGuardRouter(t *testing.T, body string) *api.Router {
	t.Helper()
	var router api.Router
	if err := yaml.Unmarshal([]byte(body), &router); err != nil {
		t.Fatal(err)
	}
	return &router
}

func (s mapStore) ListObjectStatuses() ([]routerstate.ObjectStatus, error) {
	out := make([]routerstate.ObjectStatus, 0, len(s))
	for key, status := range s {
		parts := strings.Split(key, "/")
		if len(parts) != 4 {
			continue
		}
		out = append(out, routerstate.ObjectStatus{
			APIVersion: parts[0] + "/" + parts[1],
			Kind:       parts[2],
			Name:       parts[3],
			Status:     status,
		})
	}
	return out, nil
}

func (s mapStore) DeleteObject(apiVersion, kind, name string) error {
	delete(s, apiVersion+"/"+kind+"/"+name)
	return nil
}

func TestWireGuardControllerAppliesInterfaceAndPeers(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: test}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg0}
      spec:
        privateKey: priv
        listenPort: 51820
        mtu: 1420
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardPeer
      metadata: {name: peer-a}
      spec:
        interface: wg0
        publicKey: peerpub
        allowedIPs: [10.99.0.2/32]
        endpoint: 198.51.100.2:51820
        persistentKeepalive: 25
`)
	store := mapStore{}
	store.SaveObjectStatus(api.NetAPIVersion, "WireGuardInterface", "wg0", map[string]any{
		"configHash": wireGuardConfigHash(wireguard.InterfaceConfig{
			Name:       "wg0",
			PrivateKey: "priv",
			ListenPort: 51820,
			Peers: []wireguard.PeerConfig{{
				Name:                "peer-a",
				PublicKey:           "peerpub",
				AllowedIPs:          []string{"10.99.0.2/32"},
				Endpoint:            "198.51.100.2:51820",
				PersistentKeepalive: 25,
			}},
		}, false),
	})
	var calls []string
	controller := WireGuardController{
		Router: router,
		Store:  store,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := name + " " + strings.Join(args, " ")
			calls = append(calls, call)
			switch {
			case call == "ip link show wg0":
				return nil, errors.New("missing")
			case call == "wg show wg0 dump":
				return []byte("priv\tifacepub\t51820\toff\npeerpub\tpsk\t198.51.100.2:51820\t10.99.0.2/32\t1710000000\t100\t200\t25\n"), nil
			default:
				return nil, nil
			}
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	iface := store.ObjectStatus(api.NetAPIVersion, "WireGuardInterface", "wg0")
	if iface["phase"] != "Up" || iface["publicKey"] != "ifacepub" || iface["peerCount"] != 1 {
		t.Fatalf("interface status = %+v", iface)
	}
	peer := store.ObjectStatus(api.NetAPIVersion, "WireGuardPeer", "peer-a")
	if peer["phase"] != "Connected" || peer["latestEndpoint"] != "198.51.100.2:51820" {
		t.Fatalf("peer status = %+v", peer)
	}
	for _, want := range []string{"ip link add dev wg0 type wireguard", "wg setconf wg0", "ip link set up dev wg0"} {
		found := false
		for _, call := range calls {
			if strings.HasPrefix(call, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing command prefix %q in %#v", want, calls)
		}
	}
}

func TestWireGuardControllerDeletesStaleManagedInterfaceAndPeer(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: test}
spec:
  resources: []
`)
	store := mapStore{}
	store.SaveObjectStatus(api.NetAPIVersion, "WireGuardInterface", "old", map[string]any{
		"phase":     "Up",
		"ifname":    "wg-old",
		"managedBy": "routerd",
	})
	store.SaveObjectStatus(api.NetAPIVersion, "WireGuardPeer", "old-peer", map[string]any{
		"phase":     "Connected",
		"interface": "old",
		"managedBy": "routerd",
	})
	var calls []string
	controller := WireGuardController{
		Router: router,
		Store:  store,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "ip link delete dev wg-old" {
		t.Fatalf("calls = %#v, want stale link delete", calls)
	}
	if _, ok := store[api.NetAPIVersion+"/WireGuardInterface/old"]; ok {
		t.Fatalf("stale interface status was not deleted: %#v", store)
	}
	if _, ok := store[api.NetAPIVersion+"/WireGuardPeer/old-peer"]; ok {
		t.Fatalf("stale peer status was not deleted: %#v", store)
	}
}

func TestWireGuardControllerKeepsAdoptedStaleInterface(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: test}
spec:
  resources: []
`)
	store := mapStore{}
	store.SaveObjectStatus(api.NetAPIVersion, "WireGuardInterface", "external", map[string]any{
		"phase":     "Observed",
		"ifname":    "wg-ext",
		"managedBy": "external",
	})
	controller := WireGuardController{
		Router: router,
		Store:  store,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			t.Fatalf("adopted stale interface must not be deleted, got %s %v", name, args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := store[api.NetAPIVersion+"/WireGuardInterface/external"]; !ok {
		t.Fatalf("adopted interface status was deleted: %#v", store)
	}
}

func TestWireGuardControllerMarksEmptyPeerNotConfigured(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: test}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg0}
      spec:
        privateKey: priv
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardPeer
      metadata: {name: pending-peer}
      spec:
        interface: wg0
`)
	store := mapStore{}
	controller := WireGuardController{
		Router: router,
		Store:  store,
		DryRun: true,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			return nil, errors.New("not available in test")
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	peer := store.ObjectStatus(api.NetAPIVersion, "WireGuardPeer", "pending-peer")
	if peer["phase"] != "NotConfigured" || peer["reason"] != "PeerSpecEmpty" {
		t.Fatalf("peer status = %+v", peer)
	}
	iface := store.ObjectStatus(api.NetAPIVersion, "WireGuardInterface", "wg0")
	if iface["peerCount"] != 0 {
		t.Fatalf("interface peerCount = %v, want 0", iface["peerCount"])
	}
}

func TestWireGuardControllerSkipsApplyWhenInterfaceMatches(t *testing.T) {
	t.Run("literal endpoint", func(t *testing.T) {
		testWireGuardControllerSkipsApplyWhenInterfaceMatches(t, "198.51.100.2:51820", "198.51.100.2:51820", nil)
	})
	t.Run("dns endpoint resolved to latest endpoint", func(t *testing.T) {
		testWireGuardControllerSkipsApplyWhenInterfaceMatches(t, "peer-a.example.test:51820", "198.51.100.2:51820", func(_ context.Context, host string) ([]string, error) {
			if host != "peer-a.example.test" {
				t.Fatalf("lookup host = %q, want peer-a.example.test", host)
			}
			return []string{"198.51.100.2"}, nil
		})
	})
	t.Run("configured endpoint without latest handshake endpoint", func(t *testing.T) {
		testWireGuardControllerSkipsApplyWhenInterfaceMatches(t, "198.51.100.2:51820", "", nil)
	})
}

func testWireGuardControllerSkipsApplyWhenInterfaceMatches(t *testing.T, desiredEndpoint, observedEndpoint string, lookup func(context.Context, string) ([]string, error)) {
	t.Helper()
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: test}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg0}
      spec:
        privateKey: priv
        listenPort: 51820
        mtu: 1420
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardPeer
      metadata: {name: peer-a}
      spec:
        interface: wg0
        publicKey: peerpub
        allowedIPs: [10.99.0.2/32]
        endpoint: `+desiredEndpoint+`
        persistentKeepalive: 25
`)
	store := mapStore{}
	store.SaveObjectStatus(api.NetAPIVersion, "WireGuardInterface", "wg0", map[string]any{
		"configHash": wireGuardConfigHash(wireguard.InterfaceConfig{
			Name:       "wg0",
			PrivateKey: "priv",
			ListenPort: 51820,
			Peers: []wireguard.PeerConfig{{
				Name:                "peer-a",
				PublicKey:           "peerpub",
				AllowedIPs:          []string{"10.99.0.2/32"},
				Endpoint:            desiredEndpoint,
				PersistentKeepalive: 25,
			}},
		}, false),
	})
	var calls []string
	controller := WireGuardController{
		Router: router,
		Store:  store,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := name + " " + strings.Join(args, " ")
			calls = append(calls, call)
			switch call {
			case "wg show wg0 dump":
				return []byte("priv\tifacepub\t51820\toff\npeerpub\tpsk\t" + observedEndpoint + "\t10.99.0.2/32\t1710000000\t100\t200\t25\n"), nil
			case "ip -o link show dev wg0":
				return []byte("7: wg0: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1420 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000\n"), nil
			default:
				return nil, nil
			}
		},
		LookupHost: lookup,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "wg setconf wg0") || strings.HasPrefix(call, "ip link set") {
			t.Fatalf("matching interface should not be reapplied, calls = %#v", calls)
		}
	}
	status := store.ObjectStatus(api.NetAPIVersion, "WireGuardInterface", "wg0")
	if status["phase"] != "Up" || status["reason"] != "AlreadyConfigured" {
		t.Fatalf("status = %+v", status)
	}
}

func TestWireGuardControllerAddsBGPMobilityAllowedIPs(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: test}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPRouter
      metadata: {name: mobility-bgp}
      spec:
        asn: 64577
        routerID: 10.99.0.1
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg0}
      spec:
        privateKey: priv
        allowBGPMobilityAllowedIPs: true
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardPeer
      metadata: {name: peer-a}
      spec:
        interface: wg0
        publicKey: peerpub-a
        allowedIPs: [10.99.0.2/32]
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardPeer
      metadata: {name: peer-b}
      spec:
        interface: wg0
        publicKey: peerpub-b
        allowedIPs: [10.99.0.5/32]
`)
	store := mapStore{}
	store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{
		"installedNextHops": map[string][]string{
			"10.77.60.11/32": {"10.99.0.2"},
			"10.77.60.12/32": {"10.99.0.5"},
			"10.77.0.0/16":   {"10.99.0.2"},
		},
	})
	var setconf string
	controller := WireGuardController{
		Router: router,
		Store:  store,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := name + " " + strings.Join(args, " ")
			switch {
			case call == "ip link show wg0":
				return nil, errors.New("missing")
			case call == "wg show wg0 dump":
				return []byte("priv\tifacepub\t51820\toff\npeerpub-a\tpsk\t198.51.100.2:51820\t10.77.60.11/32,10.99.0.2/32\t1710000000\t100\t200\t0\npeerpub-b\tpsk\t198.51.100.5:51820\t10.77.60.12/32,10.99.0.5/32\t1710000000\t100\t200\t0\n"), nil
			default:
				return nil, nil
			}
		},
		CommandStdin: func(_ context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			if name == "wg" && strings.Join(args, " ") == "setconf wg0 /dev/stdin" {
				setconf = string(stdin)
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"PublicKey = peerpub-a\nAllowedIPs = 10.77.60.11/32, 10.99.0.2/32",
		"PublicKey = peerpub-b\nAllowedIPs = 10.77.60.12/32, 10.99.0.5/32",
	} {
		if !strings.Contains(setconf, want) {
			t.Fatalf("setconf missing %q:\n%s", want, setconf)
		}
	}
	if strings.Contains(setconf, "10.77.0.0/16") {
		t.Fatalf("non-/32 BGP prefix should not be added to WireGuard allowedIPs:\n%s", setconf)
	}
}

func TestWireGuardControllerDoesNotAddBGPMobilityAllowedIPsByDefault(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: test}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPRouter
      metadata: {name: mobility-bgp}
      spec:
        asn: 64577
        routerID: 10.99.0.1
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg0}
      spec:
        privateKey: priv
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardPeer
      metadata: {name: peer-a}
      spec:
        interface: wg0
        publicKey: peerpub-a
        allowedIPs: [10.99.0.2/32]
`)
	store := mapStore{}
	store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{
		"installedNextHops": map[string][]string{
			"10.77.60.11/32": {"10.99.0.2"},
		},
	})
	controller := WireGuardController{Router: router, Store: store}
	cfg, err := wireguard.BuildInterface(router.Spec.Resources[1], router.Spec.Resources)
	if err != nil {
		t.Fatal(err)
	}
	cfg = controller.withBGPMobilityAllowedIPs(cfg)
	if !stringSetEqual(cfg.Peers[0].AllowedIPs, []string{"10.99.0.2/32"}) {
		t.Fatalf("default allowedIPs = %#v, want only declared endpoint prefix", cfg.Peers[0].AllowedIPs)
	}
}

func TestWireGuardControllerMovesAndWithdrawsBGPMobilityAllowedIPs(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: test}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPRouter
      metadata: {name: mobility-bgp}
      spec:
        asn: 64577
        routerID: 10.99.0.1
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg0}
      spec:
        privateKey: priv
        allowBGPMobilityAllowedIPs: true
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardPeer
      metadata: {name: peer-a}
      spec:
        interface: wg0
        publicKey: peerpub-a
        allowedIPs: [10.99.0.2/32]
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardPeer
      metadata: {name: peer-b}
      spec:
        interface: wg0
        publicKey: peerpub-b
        allowedIPs: [10.99.0.5/32]
`)
	store := mapStore{}
	controller := WireGuardController{Router: router, Store: store}
	store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{
		"installedNextHops": map[string]any{"10.77.60.11/32": []any{"10.99.0.2"}},
	})
	cfg, err := wireguard.BuildInterface(router.Spec.Resources[1], router.Spec.Resources)
	if err != nil {
		t.Fatal(err)
	}
	cfg = controller.withBGPMobilityAllowedIPs(cfg)
	if !stringSetEqual(cfg.Peers[0].AllowedIPs, []string{"10.99.0.2/32", "10.77.60.11/32"}) || !stringSetEqual(cfg.Peers[1].AllowedIPs, []string{"10.99.0.5/32"}) {
		t.Fatalf("initial derived allowedIPs = %#v", cfg.Peers)
	}

	store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{
		"installedNextHops": map[string]any{"10.77.60.11/32": []any{"10.99.0.5"}},
	})
	cfg, err = wireguard.BuildInterface(router.Spec.Resources[1], router.Spec.Resources)
	if err != nil {
		t.Fatal(err)
	}
	cfg = controller.withBGPMobilityAllowedIPs(cfg)
	if !stringSetEqual(cfg.Peers[0].AllowedIPs, []string{"10.99.0.2/32"}) || !stringSetEqual(cfg.Peers[1].AllowedIPs, []string{"10.99.0.5/32", "10.77.60.11/32"}) {
		t.Fatalf("moved derived allowedIPs = %#v", cfg.Peers)
	}

	store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{"installedNextHops": map[string]any{}})
	cfg, err = wireguard.BuildInterface(router.Spec.Resources[1], router.Spec.Resources)
	if err != nil {
		t.Fatal(err)
	}
	cfg = controller.withBGPMobilityAllowedIPs(cfg)
	if !stringSetEqual(cfg.Peers[0].AllowedIPs, []string{"10.99.0.2/32"}) || !stringSetEqual(cfg.Peers[1].AllowedIPs, []string{"10.99.0.5/32"}) {
		t.Fatalf("withdrawn derived allowedIPs = %#v", cfg.Peers)
	}
}

func TestWireGuardControllerUpdatesBGPMobilityAllowedIPsWithoutSetconf(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: test}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: BGPRouter
      metadata: {name: mobility-bgp}
      spec:
        asn: 64577
        routerID: 10.99.0.1
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg0}
      spec:
        privateKey: priv
        allowBGPMobilityAllowedIPs: true
        listenPort: 51820
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardPeer
      metadata: {name: peer-a}
      spec:
        interface: wg0
        publicKey: peerpub-a
        allowedIPs: [10.99.0.2/32]
        persistentKeepalive: 25
`)
	store := mapStore{}
	store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{
		"installedNextHops": map[string]any{"10.77.60.11/32": []any{"10.99.0.2"}},
	})
	baseCfg, err := wireguard.BuildInterface(router.Spec.Resources[1], router.Spec.Resources)
	if err != nil {
		t.Fatal(err)
	}
	store.SaveObjectStatus(api.NetAPIVersion, "WireGuardInterface", "wg0", map[string]any{
		"baseConfigHash": wireGuardConfigHash(baseCfg, false),
		"configHash":     wireGuardConfigHash(baseCfg, false),
	})
	allowedIPs := "10.99.0.2/32"
	var calls []string
	controller := WireGuardController{
		Router: router,
		Store:  store,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := name + " " + strings.Join(args, " ")
			calls = append(calls, call)
			switch {
			case call == "wg show wg0 dump":
				return []byte("priv\tifacepub\t51820\toff\npeerpub-a\tpsk\t192.0.2.10:51820\t" + allowedIPs + "\t1710000000\t100\t200\t25\n"), nil
			case call == "wg set wg0 peer peerpub-a allowed-ips 10.77.60.11/32,10.99.0.2/32":
				allowedIPs = "10.77.60.11/32,10.99.0.2/32"
				return nil, nil
			default:
				return nil, nil
			}
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "wg setconf wg0") {
			t.Fatalf("allowedIPs-only BGP update must not reset interface with setconf: %#v", calls)
		}
	}
	assertWireGuardControllerCall(t, calls, "wg set wg0 peer peerpub-a allowed-ips 10.77.60.11/32,10.99.0.2/32")
	status := store.ObjectStatus(api.NetAPIVersion, "WireGuardInterface", "wg0")
	if status["configHash"] == wireGuardConfigHash(baseCfg, false) {
		t.Fatalf("configHash was not updated after allowedIPs update: %+v", status)
	}
}

func assertWireGuardControllerCall(t *testing.T, calls []string, want string) {
	t.Helper()
	for _, call := range calls {
		if call == want {
			return
		}
	}
	t.Fatalf("missing call %q in %#v", want, calls)
}

func TestWireGuardControllerMarksMissingKeyPending(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: test}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg0}
      spec:
        listenPort: 51820
`)
	store := mapStore{}
	controller := WireGuardController{Router: router, Store: store}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "WireGuardInterface", "wg0")
	if status["phase"] != "Pending" || status["reason"] != "PrivateKeyMissing" {
		t.Fatalf("status = %+v", status)
	}
}
