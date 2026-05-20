// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"routerd/pkg/api"
	routerstate "routerd/pkg/state"
	"routerd/pkg/wireguard"
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
			switch call {
			case "wg show wg0 dump":
				return []byte("priv\tifacepub\t51820\toff\npeerpub\tpsk\t198.51.100.2:51820\t10.99.0.2/32\t1710000000\t100\t200\t25\n"), nil
			case "ip -o link show dev wg0":
				return []byte("7: wg0: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1420 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000\n"), nil
			default:
				return nil, nil
			}
		},
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
