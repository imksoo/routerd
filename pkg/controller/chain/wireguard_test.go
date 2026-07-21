// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/platform"
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

func assertStringSet(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %#v, want %#v", label, got, want)
	}
	seen := map[string]int{}
	for _, value := range got {
		seen[value]++
	}
	for _, value := range want {
		if seen[value] != 1 {
			t.Fatalf("%s = %#v, want %#v", label, got, want)
		}
	}
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

func TestWireGuardControllerDerivesPeersFromSAMRRSet(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: leaf-pve}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg-hybrid}
      spec:
        selfNodeRef: leaf-pve
        privateKey: priv
        peersFrom:
          - resource: SAMRRSet/cloudedge-rrs
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMRRSet
      metadata: {name: cloudedge-rrs}
      spec:
        enrollmentPolicyRef: SAMEnrollmentPolicy/cloudedge-leaves
        members:
          - nodeRef: aws-rr-a
            endpoint: 203.0.113.10
            tunnelAddress: 10.99.0.2/32
            wireGuard:
              publicKey: rrpub-a
              endpoint: 203.0.113.10:51820
          - nodeRef: aws-rr-b
            endpoint: 203.0.113.11
            tunnelAddress: 10.99.0.3/32
            wireGuard:
              publicKey: rrpub-b
              endpoint: 203.0.113.11:51820
`)
	var setconf string
	controller := WireGuardController{
		Router: router,
		Store:  mapStore{},
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			switch name + " " + strings.Join(args, " ") {
			case "ip link show wg-hybrid":
				return nil, errors.New("missing")
			case "wg show wg-hybrid dump":
				return []byte("priv\tifacepub\t51820\toff\n"), nil
			default:
				return nil, nil
			}
		},
		CommandStdin: func(_ context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			if name == "wg" && strings.Join(args, " ") == "setconf wg-hybrid /dev/stdin" {
				setconf = string(stdin)
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"PublicKey = rrpub-a",
		"AllowedIPs = 10.99.0.2/32",
		"Endpoint = 203.0.113.10:51820",
		"PublicKey = rrpub-b",
		"AllowedIPs = 10.99.0.3/32",
		"Endpoint = 203.0.113.11:51820",
	} {
		if !strings.Contains(setconf, want) {
			t.Fatalf("setconf missing %q:\n%s", want, setconf)
		}
	}
}

func TestResolveWireGuardSAMResourcesDerivesPeersFromSAMRRSet(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: leaf-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg-cloudedge}
      spec:
        selfNodeRef: leaf-a
        privateKey: priv
        peersFrom:
          - resource: SAMRRSet/cloudedge-rrs
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMRRSet
      metadata: {name: cloudedge-rrs}
      spec:
        enrollmentPolicyRef: SAMEnrollmentPolicy/cloudedge-leaves
        members:
          - nodeRef: rr-a
            endpoint: 10.10.0.2
            tunnelAddress: 10.99.0.2/32
            wireGuard:
              publicKey: rrpub-a
              endpoint: 203.0.113.10:51820
              allowedIPs: [10.10.0.2/32]
          - nodeRef: rr-b
            endpoint: 10.10.0.3
            tunnelAddress: 10.99.0.3/32
            wireGuard:
              publicKey: rrpub-b
              endpoint: 203.0.113.11:51820
              allowedIPs: [10.10.0.3/32]
`)
	effective, err := resolveWireGuardSAMResources(router)
	if err != nil {
		t.Fatal(err)
	}
	if got := countResources(effective, api.NetAPIVersion, "WireGuardPeer"); got != 2 {
		t.Fatalf("WireGuardPeer count = %d, want 2 resources=%#v", got, effective.Spec.Resources)
	}
	for _, want := range []struct {
		name       string
		allowedIPs []string
	}{
		{name: "rr-a", allowedIPs: []string{"10.99.0.2/32", "10.10.0.2/32"}},
		{name: "rr-b", allowedIPs: []string{"10.99.0.3/32", "10.10.0.3/32"}},
	} {
		peer := mustWireGuardPeer(t, effective, want.name)
		if strings.Join(peer.AllowedIPs, ",") != strings.Join(want.allowedIPs, ",") {
			t.Fatalf("WireGuardPeer/%s allowedIPs = %#v, want %#v", want.name, peer.AllowedIPs, want.allowedIPs)
		}
	}
}

func mustWireGuardPeer(t *testing.T, router *api.Router, name string) api.WireGuardPeerSpec {
	t.Helper()
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "WireGuardPeer" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.WireGuardPeerSpec()
		if err != nil {
			t.Fatalf("WireGuardPeer/%s spec: %v", name, err)
		}
		return spec
	}
	t.Fatalf("WireGuardPeer/%s not found in %#v", name, router.Spec.Resources)
	return api.WireGuardPeerSpec{}
}

func TestWireGuardControllerAppliesInterfaceAndPeers(t *testing.T) {
	requireLinuxRuntimeFixture(t)
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
	requireLinuxRuntimeFixture(t)
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
	requireLinuxRuntimeFixture(t)
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
	requireLinuxRuntimeFixture(t)
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
		testWireGuardControllerSkipsApplyWhenInterfaceMatches(t, "198.51.100.2:51820", "198.51.100.2:51820")
	})
	t.Run("dns endpoint resolved to latest endpoint", func(t *testing.T) {
		testWireGuardControllerSkipsApplyWhenInterfaceMatches(t, "peer-a.example.test:51820", "198.51.100.2:51820")
	})
	t.Run("configured endpoint without latest handshake endpoint", func(t *testing.T) {
		testWireGuardControllerSkipsApplyWhenInterfaceMatches(t, "198.51.100.2:51820", "")
	})
	t.Run("roamed endpoint differs from configured endpoint", func(t *testing.T) {
		testWireGuardControllerSkipsApplyWhenInterfaceMatches(t, "198.51.100.2:51820", "203.0.113.9:51820")
	})
}

func testWireGuardControllerSkipsApplyWhenInterfaceMatches(t *testing.T, desiredEndpoint, observedEndpoint string) {
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

func TestWireGuardControllerDerivesPeersFromSAMEnrollmentPolicy(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: aws-rr-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg-hybrid}
      spec:
        selfNodeRef: aws-rr-a
        privateKey: priv
        listenPort: 51820
        peersFrom:
          - resource: SAMEnrollmentPolicy/cloudedge-leaves
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMEnrollmentPolicy
      metadata: {name: cloudedge-leaves}
      spec:
        transportProfileRef: SAMTransportProfile/aws-rr-a
        tunnelAddressPrefixes: [10.255.0.0/20]
        ttl: 1h
        wireGuard:
          interface: wg-hybrid
          persistentKeepalive: 25
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMEnrollmentClaim
      metadata: {name: leaf-pve}
      spec:
        policyRef: SAMEnrollmentPolicy/cloudedge-leaves
        leafID: leaf-pve
        tunnelAddress: 10.255.0.21/32
        wireGuard:
          publicKey: leafpub-pve
          endpoint: 198.51.100.21:51820
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMEnrollmentClaim
      metadata: {name: leaf-revoked}
      spec:
        policyRef: SAMEnrollmentPolicy/cloudedge-leaves
        leafID: leaf-revoked
        tunnelAddress: 10.255.0.22/32
        revoked: true
        wireGuard:
          publicKey: revokedpub
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMEnrollmentClaim
      metadata: {name: leaf-ttl-expired}
      spec:
        policyRef: SAMEnrollmentPolicy/cloudedge-leaves
        leafID: leaf-ttl-expired
        joinTimestamp: "2026-06-28T00:00:00Z"
        tunnelAddress: 10.255.0.23/32
        wireGuard:
          publicKey: ttlpub
`)
	store := mapStore{}
	var setconf string
	controller := WireGuardController{
		Router: router,
		Store:  store,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			switch name + " " + strings.Join(args, " ") {
			case "ip link show wg-hybrid":
				return nil, errors.New("missing")
			case "wg show wg-hybrid dump":
				return []byte("priv\tifacepub\t51820\toff\nleafpub-pve\tpsk\t198.51.100.21:51820\t10.255.0.21/32\t0\t0\t0\t25\n"), nil
			default:
				return nil, nil
			}
		},
		CommandStdin: func(_ context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
			if name == "wg" && strings.Join(args, " ") == "setconf wg-hybrid /dev/stdin" {
				setconf = string(stdin)
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"PublicKey = leafpub-pve",
		"AllowedIPs = 10.255.0.21/32",
		"Endpoint = 198.51.100.21:51820",
		"PersistentKeepalive = 25",
	} {
		if !strings.Contains(setconf, want) {
			t.Fatalf("setconf missing %q:\n%s", want, setconf)
		}
	}
	if strings.Contains(setconf, "revokedpub") {
		t.Fatalf("revoked claim must not be materialized:\n%s", setconf)
	}
	if strings.Contains(setconf, "ttlpub") {
		t.Fatalf("TTL-expired claim must not be materialized:\n%s", setconf)
	}
	policy := store.ObjectStatus(api.MobilityAPIVersion, "SAMEnrollmentPolicy", "cloudedge-leaves")
	if policy["acceptedClaims"] != 1 || policy["skippedClaims"] != 2 {
		t.Fatalf("policy status = %+v", policy)
	}
}

func TestPVEWireGuardPeersFromSubmittedEnrollmentClaimState(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	router, err := config.Load(filepath.Join("..", "..", "..", "examples", "pve-minimal-rr.yaml"))
	if err != nil {
		t.Fatalf("load pve-minimal-rr.yaml: %v", err)
	}
	assertResourceCount(t, router.Spec.Resources, api.MobilityAPIVersion, "SAMEnrollmentClaim", 0)
	claim := claimWithJoinTimestamp(t, pveMinimalRRSeedClaim(t, "pve-leaf-a"), now)
	store := &dynamicRouteSAMStore{records: []routerstate.DynamicConfigPartRecord{
		dynamicPartRecord(t, "SAMEnrollmentClaim/pve-leaf-a", []api.Resource{claim}, now.Add(5*time.Minute)),
	}}
	effective, err := BuildDynamicRouteSAMEffectiveRouter(router, store, now, platform.OSLinux)
	if err != nil {
		t.Fatalf("BuildDynamicRouteSAMEffectiveRouter: %v", err)
	}
	assertResourceCount(t, effective.Spec.Resources, api.MobilityAPIVersion, "SAMEnrollmentClaim", 1)
	resolved, err := resolveWireGuardSAMResources(effective)
	if err != nil {
		t.Fatalf("resolveWireGuardSAMResources: %v", err)
	}
	peer := mustResource(t, resolved, api.NetAPIVersion, "WireGuardPeer", "pve-leaf-a")
	spec, err := peer.WireGuardPeerSpec()
	if err != nil {
		t.Fatalf("WireGuardPeer spec: %v", err)
	}
	if spec.Interface != "wg-pve" || spec.PublicKey != "PVE_LEAF_A_WIREGUARD_PUBLIC_KEY" || spec.Endpoint != "10.30.0.21:51820" {
		t.Fatalf("WireGuardPeer/pve-leaf-a = %#v", spec)
	}
	assertStringSet(t, "WireGuardPeer/pve-leaf-a allowedIPs", spec.AllowedIPs, []string{"10.255.10.21/32", "10.31.0.21/32"})
}

func TestCloudEdgeRRExamplesDeriveOnlyWGAdmissionPeers(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, example := range []string{
		"cloudedge-dynamic-rr-a-hub.yaml",
		"cloudedge-dynamic-rr-b-hub.yaml",
	} {
		t.Run(example, func(t *testing.T) {
			router, err := config.Load(filepath.Join("..", "..", "..", "examples", example))
			if err != nil {
				t.Fatalf("load %s: %v", example, err)
			}
			if err := config.Validate(router); err != nil {
				t.Fatalf("validate %s: %v", example, err)
			}
			assertResourceCount(t, router.Spec.Resources, api.MobilityAPIVersion, "SAMEnrollmentClaim", 0)
			claim := claimWithJoinTimestamp(t, seedClaimFromFixture(t, "cloudedge-rr-claims-seed.yaml", "leaf-a"), now)
			nonWGClaim := claimWithJoinTimestamp(t, seedClaimFromFixture(t, "cloudedge-rr-claims-seed.yaml", "leaf-b"), now)
			store := &dynamicRouteSAMStore{records: []routerstate.DynamicConfigPartRecord{
				dynamicPartRecord(t, "SAMEnrollmentClaim/leaf-a", []api.Resource{claim}, now.Add(5*time.Minute)),
				dynamicPartRecord(t, "SAMEnrollmentClaim/leaf-b", []api.Resource{nonWGClaim}, now.Add(5*time.Minute)),
			}}
			effective, err := BuildDynamicRouteSAMEffectiveRouter(router, store, now, platform.OSLinux)
			if err != nil {
				t.Fatalf("BuildDynamicRouteSAMEffectiveRouter: %v", err)
			}
			assertResourceCount(t, effective.Spec.Resources, api.MobilityAPIVersion, "SAMEnrollmentClaim", 2)
			resolved, err := resolveWireGuardSAMResources(effective)
			if err != nil {
				t.Fatalf("resolveWireGuardSAMResources: %v", err)
			}
			var peers []api.WireGuardPeerSpec
			var names []string
			for _, resource := range resolved.Spec.Resources {
				if resource.APIVersion != api.NetAPIVersion || resource.Kind != "WireGuardPeer" {
					continue
				}
				spec, err := resource.WireGuardPeerSpec()
				if err != nil {
					t.Fatalf("WireGuardPeer/%s spec: %v", resource.Metadata.Name, err)
				}
				names = append(names, resource.Metadata.Name)
				peers = append(peers, spec)
			}
			if len(peers) != 1 || names[0] != "leaf-a" {
				t.Fatalf("generated WireGuard peers = %v %#v, want only leaf-a", names, peers)
			}
			peer := peers[0]
			if peer.Interface != "wg-cloudedge" || peer.PublicKey != "LEAF_A_WIREGUARD_PUBLIC_KEY" || peer.Endpoint != "198.51.100.31:51820" {
				t.Fatalf("leaf-a WireGuard peer = %#v", peer)
			}
			assertStringSet(t, "leaf-a allowedIPs", peer.AllowedIPs, []string{"10.255.0.31/32", "10.20.0.31/32"})
		})
	}
}

func pveMinimalRRSeedClaim(t *testing.T, name string) api.Resource {
	t.Helper()
	return seedClaimFromFixture(t, "pve-minimal-rr-claims-seed.yaml", name)
}

func seedClaimFromFixture(t *testing.T, fixture, name string) api.Resource {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "fixtures", fixture))
	if err != nil {
		t.Fatalf("read claim seed %s: %v", fixture, err)
	}
	var seed api.Router
	if err := yaml.Unmarshal(data, &seed); err != nil {
		t.Fatalf("parse claim seed %s: %v", fixture, err)
	}
	for _, resource := range seed.Spec.Resources {
		if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "SAMEnrollmentClaim" && resource.Metadata.Name == name {
			return resource
		}
	}
	t.Fatalf("missing seed claim %s", name)
	return api.Resource{}
}

func claimWithJoinTimestamp(t *testing.T, resource api.Resource, now time.Time) api.Resource {
	t.Helper()
	spec, err := resource.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("SAMEnrollmentClaim/%s spec: %v", resource.Metadata.Name, err)
	}
	spec.JoinTimestamp = now.UTC().Format(time.RFC3339)
	resource.Spec = spec
	return resource
}

func mustResource(t *testing.T, router *api.Router, apiVersion, kind, name string) api.Resource {
	t.Helper()
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == apiVersion && resource.Kind == kind && resource.Metadata.Name == name {
			return resource
		}
	}
	t.Fatalf("missing %s/%s", kind, name)
	return api.Resource{}
}

func assertResourceCount(t *testing.T, resources []api.Resource, apiVersion, kind string, want int) {
	t.Helper()
	got := 0
	for _, resource := range resources {
		if resource.APIVersion == apiVersion && resource.Kind == kind {
			got++
		}
	}
	if got != want {
		t.Fatalf("%s/%s count = %d, want %d", apiVersion, kind, got, want)
	}
}

func TestWireGuardControllerGeneratesSAMEndpointRoutes(t *testing.T) {
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
            samEndpoint: 10.99.70.1
            wireGuard:
              publicKey: selfpub
              allowedIPs: [10.99.70.1/32]
          - nodeRef: router-b
            samEndpoint: 10.99.70.2
            wireGuard:
              publicKey: peerpub-b
              endpoint: 198.51.100.2:51820
              allowedIPs: [10.99.70.2/32]
          - nodeRef: router-c
            samEndpoint: 10.99.70.3
            wireGuard:
              publicKey: peerpub-c
              endpoint: 198.51.100.3:51820
              allowedIPs: [10.99.70.3/32]
`)
	ctrl := WireGuardController{Router: router, Store: mapStore{}}
	resolved, err := ctrl.resolvePeerResources()
	if err != nil {
		t.Fatal(err)
	}
	routes := map[string]string{}
	for _, r := range resolved.Router.Spec.Resources {
		if r.Kind == "IPv4Route" {
			spec, _ := r.IPv4RouteSpec()
			routes[r.Metadata.Name] = spec.Destination + " dev " + spec.Device
		}
	}
	if got, ok := routes["wg-sam-endpoint-router-b"]; !ok || got != "10.99.70.2/32 dev wg0" {
		t.Fatalf("expected route for router-b; routes=%v", routes)
	}
	if got, ok := routes["wg-sam-endpoint-router-c"]; !ok || got != "10.99.70.3/32 dev wg0" {
		t.Fatalf("expected route for router-c; routes=%v", routes)
	}
	if _, ok := routes["wg-sam-endpoint-router-a"]; ok {
		t.Fatal("self node route must not be generated")
	}
}

func TestWireGuardControllerStaticRouteOverridesGeneratedEndpointRoute(t *testing.T) {
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
            samEndpoint: 10.99.70.2
            wireGuard:
              publicKey: peerpub-b
              allowedIPs: [10.99.70.2/32]
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4Route
      metadata: {name: wg-sam-endpoint-router-b}
      spec:
        destination: 10.99.70.2/32
        device: custom0
        metric: 42
`)
	ctrl := WireGuardController{Router: router, Store: mapStore{}}
	resolved, err := ctrl.resolvePeerResources()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range resolved.Router.Spec.Resources {
		if r.Kind == "IPv4Route" && r.Metadata.Name == "wg-sam-endpoint-router-b" {
			spec, _ := r.IPv4RouteSpec()
			if spec.Device != "custom0" || spec.Metric != 42 {
				t.Fatalf("static route did not override generated: %+v", spec)
			}
			return
		}
	}
	t.Fatal("route not found in resolved resources")
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

func TestWireGuardControllerGeneratesMissingPrivateKeyFileAndPublishesPublicKey(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "secrets", "wg0.key")
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
        privateKeyFile: `+keyPath+`
        listenPort: 51820
`)
	store := mapStore{}
	controller := WireGuardController{
		Router: router,
		Store:  store,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := name + " " + strings.Join(args, " ")
			switch call {
			case "ip link show wg0":
				return nil, errors.New("missing")
			case "wg show wg0 dump":
				return nil, errors.New("status unavailable")
			default:
				return nil, nil
			}
		},
		CommandStdin: func(_ context.Context, _ []byte, _ string, _ ...string) ([]byte, error) {
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := wireguard.PublicKeyFromPrivateKey(strings.TrimSpace(string(key)))
	if err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "WireGuardInterface", "wg0")
	if status["phase"] != "Up" || status["publicKey"] != publicKey {
		t.Fatalf("status = %+v, want derived public key %q", status, publicKey)
	}
}

func TestResolveWireGuardSAMResourcesGeneratesSelfAddressAndPeerRoutes(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: router-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg-sam}
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
            samEndpoint: 10.99.70.1
            wireGuard:
              publicKey: selfpub
              allowedIPs: [10.99.70.1/32]
          - nodeRef: router-b
            samEndpoint: 10.99.70.2
            wireGuard:
              publicKey: peerpub-b
              endpoint: 198.51.100.2:51820
              allowedIPs: [10.99.70.2/32]
`)
	resolved, err := resolveWireGuardSAMResources(router)
	if err != nil {
		t.Fatal(err)
	}
	var selfAddr, peerRoute, peerPeer bool
	for _, r := range resolved.Spec.Resources {
		switch {
		case r.Kind == "IPv4StaticAddress" && r.Metadata.Name == "wg-sam-addr-wg-sam":
			spec, _ := r.IPv4StaticAddressSpec()
			if spec.Interface != "wg-sam" || spec.Address != "10.99.70.1/32" {
				t.Fatalf("self address spec mismatch: %+v", spec)
			}
			selfAddr = true
		case r.Kind == "IPv4Route" && r.Metadata.Name == "wg-sam-endpoint-router-b":
			spec, _ := r.IPv4RouteSpec()
			if spec.Destination != "10.99.70.2/32" || spec.Device != "wg-sam" {
				t.Fatalf("peer route spec mismatch: %+v", spec)
			}
			peerRoute = true
		case r.Kind == "WireGuardPeer" && r.Metadata.Name == "router-b":
			spec, _ := r.WireGuardPeerSpec()
			if spec.PublicKey != "peerpub-b" || spec.Interface != "wg-sam" {
				t.Fatalf("peer spec mismatch: %+v", spec)
			}
			peerPeer = true
		}
	}
	if !selfAddr {
		t.Fatal("self-node IPv4StaticAddress not generated")
	}
	if !peerRoute {
		t.Fatal("peer IPv4Route not generated")
	}
	if !peerPeer {
		t.Fatal("peer WireGuardPeer not generated")
	}
	for _, r := range resolved.Spec.Resources {
		if r.Kind == "IPv4Route" && r.Metadata.Name == "wg-sam-endpoint-router-a" {
			t.Fatal("self-node route must not be generated")
		}
	}
}

func TestResolveWireGuardSAMResourcesStaticAddressOverride(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: router-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg-sam}
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
            samEndpoint: 10.99.70.1
            wireGuard:
              publicKey: selfpub
              allowedIPs: [10.99.70.1/32]
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata: {name: wg-sam-addr-wg-sam}
      spec:
        interface: wg-sam
        address: 10.99.70.99/32
`)
	resolved, err := resolveWireGuardSAMResources(router)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range resolved.Spec.Resources {
		if r.Kind == "IPv4StaticAddress" && r.Metadata.Name == "wg-sam-addr-wg-sam" {
			spec, _ := r.IPv4StaticAddressSpec()
			if spec.Address != "10.99.70.99/32" {
				t.Fatalf("static override not respected: got %s", spec.Address)
			}
			return
		}
	}
	t.Fatal("IPv4StaticAddress not found")
}

func TestResolveWireGuardSAMResourcesIsIdempotent(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: router-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg-sam}
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
            samEndpoint: 10.99.70.1
            wireGuard:
              publicKey: selfpub
              allowedIPs: [10.99.70.1/32]
          - nodeRef: router-b
            samEndpoint: 10.99.70.2
            wireGuard:
              publicKey: peerpub-b
              endpoint: 198.51.100.2:51820
              allowedIPs: [10.99.70.2/32]
`)
	first, err := resolveWireGuardSAMResources(router)
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolveWireGuardSAMResources(first)
	if err != nil {
		t.Fatal(err)
	}
	count := func(kind, name string) int {
		total := 0
		for _, r := range second.Spec.Resources {
			if r.APIVersion == api.NetAPIVersion && r.Kind == kind && r.Metadata.Name == name {
				total++
			}
		}
		return total
	}
	for _, want := range []struct {
		kind string
		name string
	}{
		{"IPv4StaticAddress", "wg-sam-addr-wg-sam"},
		{"IPv4Route", "wg-sam-endpoint-router-b"},
		{"WireGuardPeer", "router-b"},
	} {
		if got := count(want.kind, want.name); got != 1 {
			t.Fatalf("%s/%s count = %d, want 1", want.kind, want.name, got)
		}
	}
}

func TestResolveWireGuardSAMResourcesNilRouter(t *testing.T) {
	resolved, err := resolveWireGuardSAMResources(nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != nil {
		t.Fatalf("expected nil router, got %+v", resolved)
	}
}

func TestResolveWireGuardSAMResourcesNoPeersFromIsNoOp(t *testing.T) {
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
        privateKey: priv
`)
	resolved, err := resolveWireGuardSAMResources(router)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Spec.Resources) != len(router.Spec.Resources) {
		t.Fatalf("expected no change; before=%d after=%d", len(router.Spec.Resources), len(resolved.Spec.Resources))
	}
}
