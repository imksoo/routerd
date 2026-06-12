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

func TestWireGuardControllerOpensHostFirewallForListenPort(t *testing.T) {
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
`)
	store := mapStore{}
	var calls []string
	controller := WireGuardController{
		Router: router,
		Store:  store,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := name + " " + strings.Join(args, " ")
			calls = append(calls, call)
			switch call {
			case "ip link show wg0":
				return nil, errors.New("missing")
			case "wg show wg0 dump":
				return []byte("priv\tifacepub\t51820\toff\n"), nil
			case "iptables -C INPUT -p udp --dport 51820 -j ACCEPT":
				return []byte("iptables: Bad rule (does a matching rule exist in that chain?).\n"), errors.New("exit status 1")
			default:
				return nil, nil
			}
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	gotCommands := strings.Join(calls, "\n")
	for _, want := range []string{
		"iptables -C INPUT -p udp --dport 51820 -j ACCEPT",
		"iptables -I INPUT 1 -p udp --dport 51820 -j ACCEPT",
	} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("commands missing %q:\n%s", want, gotCommands)
		}
	}
	iface := store.ObjectStatus(api.NetAPIVersion, "WireGuardInterface", "wg0")
	hostFirewall, ok := iface["hostFirewall"].(map[string]any)
	if !ok {
		t.Fatalf("missing hostFirewall status: %#v", iface)
	}
	if hostFirewall["phase"] != "Applied" || hostFirewall["chain"] != "INPUT" || hostFirewall["port"] != 51820 {
		t.Fatalf("hostFirewall = %#v", hostFirewall)
	}
}

func TestWireGuardControllerRemovesStaleHostFirewallListenPort(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: test}
spec:
  resources: []
`)
	store := mapStore{
		api.NetAPIVersion + "/WireGuardInterface/wg0": {
			"managedBy": "routerd",
			"interface": "wg0",
			"hostFirewall": map[string]any{
				"managedBy": "routerd",
				"protocol":  "udp",
				"port":      51820,
				"chain":     "INPUT",
				"phase":     "Applied",
			},
		},
	}
	var calls []string
	controller := WireGuardController{
		Router: router,
		Store:  store,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	gotCommands := strings.Join(calls, "\n")
	for _, want := range []string{
		"ip link delete dev wg0",
		"iptables -D INPUT -p udp --dport 51820 -j ACCEPT",
	} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("commands missing %q:\n%s", want, gotCommands)
		}
	}
	if _, ok := store[api.NetAPIVersion+"/WireGuardInterface/wg0"]; ok {
		t.Fatalf("stale status was not deleted: %#v", store)
	}
}

func TestWireGuardControllerUsesInterfaceIfName(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: test}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg-svnet1}
      spec:
        ifname: wg-transport0
        privateKey: priv
        listenPort: 51820
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardPeer
      metadata: {name: peer-a}
      spec:
        interface: wg-svnet1
        publicKey: peerpub
        allowedIPs: [10.99.0.2/32]
`)
	store := mapStore{}
	var calls []string
	controller := WireGuardController{
		Router: router,
		Store:  store,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := name + " " + strings.Join(args, " ")
			calls = append(calls, call)
			if call == "ip link show wg-transport0" {
				return nil, errors.New("missing")
			}
			if call == "wg show wg-transport0 dump" {
				return []byte("priv\tifacepub\t51820\toff\npeerpub\tpsk\t\t10.99.0.2/32\t0\t0\t0\t0\n"), nil
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ip link add dev wg-transport0 type wireguard", "wg setconf wg-transport0", "ip link set up dev wg-transport0"} {
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
	iface := store.ObjectStatus(api.NetAPIVersion, "WireGuardInterface", "wg-svnet1")
	if iface["interface"] != "wg-svnet1" || iface["ifname"] != "wg-transport0" {
		t.Fatalf("interface status = %+v", iface)
	}
	peer := store.ObjectStatus(api.NetAPIVersion, "WireGuardPeer", "peer-a")
	if peer["interface"] != "wg-svnet1" || peer["ifname"] != "wg-transport0" {
		t.Fatalf("peer status = %+v", peer)
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

func TestWireGuardControllerKeepsDeclaredAllowedIPsWhenBGPMobilityRoutesExist(t *testing.T) {
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
		"PublicKey = peerpub-a\nAllowedIPs = 10.99.0.2/32",
		"PublicKey = peerpub-b\nAllowedIPs = 10.99.0.5/32",
	} {
		if !strings.Contains(setconf, want) {
			t.Fatalf("setconf missing %q:\n%s", want, setconf)
		}
	}
	for _, unwanted := range []string{"10.77.60.11/32", "10.77.60.12/32"} {
		if strings.Contains(setconf, unwanted) {
			t.Fatalf("BGP mobility prefix %s must not be added to WireGuard allowedIPs:\n%s", unwanted, setconf)
		}
	}
}

func TestWireGuardControllerDerivesPeersFromSAMNodeSet(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: router-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg0}
      spec:
        selfNodeRef: router-a
        privateKey: priv
        peersFrom:
          - resource: SAMNodeSet/fabric
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMNodeSet
      metadata: {name: fabric}
      spec:
        nodes:
          - nodeRef: router-a
            wireGuard:
              publicKey: selfpub
              allowedIPs: [10.99.0.1/32]
          - nodeRef: router-b
            wireGuard:
              publicKey: peerpub-b
              endpoint: 198.51.100.2:51820
              allowedIPs: [10.99.0.2/32]
              persistentKeepalive: 25
          - nodeRef: router-c
`)
	store := mapStore{}
	var setconf string
	controller := WireGuardController{
		Router: router,
		Store:  store,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := name + " " + strings.Join(args, " ")
			switch call {
			case "ip link show wg0":
				return nil, errors.New("missing")
			case "wg show wg0 dump":
				return []byte("priv\tifacepub\t51820\toff\npeerpub-b\tpsk\t198.51.100.2:51820\t10.99.0.2/32\t0\t0\t0\t25\n"), nil
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
		"PublicKey = peerpub-b",
		"AllowedIPs = 10.99.0.2/32",
		"Endpoint = 198.51.100.2:51820",
		"PersistentKeepalive = 25",
	} {
		if !strings.Contains(setconf, want) {
			t.Fatalf("setconf missing %q:\n%s", want, setconf)
		}
	}
	if strings.Contains(setconf, "selfpub") {
		t.Fatalf("self node must not be rendered as peer:\n%s", setconf)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "WireGuardInterface", "wg0")
	if status["phase"] != "Up" || status["selfNodeRef"] != "router-a" {
		t.Fatalf("interface status = %+v", status)
	}
	peer := store.ObjectStatus(api.NetAPIVersion, "WireGuardPeer", "router-b")
	if peer["publicKey"] != "peerpub-b" || peer["persistentKeepalive"] != 25 {
		t.Fatalf("generated peer status = %+v", peer)
	}
}

func TestWireGuardControllerStaticPeerOverridesPeersFrom(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: router-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg0}
      spec:
        selfNodeRef: router-a
        privateKey: priv
        peersFrom:
          - resource: SAMNodeSet/fabric
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMNodeSet
      metadata: {name: fabric}
      spec:
        nodes:
          - nodeRef: router-b
            wireGuard:
              publicKey: generated
              allowedIPs: [10.99.0.2/32]
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardPeer
      metadata: {name: router-b}
      spec:
        interface: wg0
        publicKey: override
        allowedIPs: [10.99.0.200/32]
        endpoint: 203.0.113.2:51820
`)
	store := mapStore{}
	var setconf string
	controller := WireGuardController{
		Router: router,
		Store:  store,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := name + " " + strings.Join(args, " ")
			switch call {
			case "ip link show wg0":
				return nil, errors.New("missing")
			case "wg show wg0 dump":
				return []byte("priv\tifacepub\t51820\toff\noverride\tpsk\t203.0.113.2:51820\t10.99.0.200/32\t0\t0\t0\t0\n"), nil
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
	if !strings.Contains(setconf, "PublicKey = override") || !strings.Contains(setconf, "AllowedIPs = 10.99.0.200/32") {
		t.Fatalf("static override was not rendered:\n%s", setconf)
	}
	if strings.Contains(setconf, "PublicKey = generated") || strings.Contains(setconf, "10.99.0.2/32") {
		t.Fatalf("generated peer leaked despite static override:\n%s", setconf)
	}
}

func TestWireGuardControllerPeersFromMissingRequiredIsPending(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: router-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg0}
      spec:
        selfNodeRef: router-a
        privateKey: priv
        peersFrom:
          - resource: SAMNodeSet/missing
`)
	store := mapStore{}
	controller := WireGuardController{
		Router: router,
		Store:  store,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			t.Fatalf("wireguard apply must not run while peersFrom is pending: %s %v", name, args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "WireGuardInterface", "wg0")
	if status["phase"] != "Pending" || status["reason"] != "PeersFromPending" {
		t.Fatalf("interface status = %+v, want peersFrom Pending", status)
	}
	pending, ok := status["pendingSources"].([]string)
	if !ok || len(pending) != 1 || pending[0] != "SAMNodeSet/missing" {
		t.Fatalf("pendingSources = %#v", status["pendingSources"])
	}
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
