// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"routerd/pkg/api"
)

func mustWireGuardRouter(t *testing.T, body string) *api.Router {
	t.Helper()
	var router api.Router
	if err := yaml.Unmarshal([]byte(body), &router); err != nil {
		t.Fatal(err)
	}
	return &router
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
