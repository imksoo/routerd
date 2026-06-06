// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/hybrid"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestTunnelInterfaceControllerAddsIPIP(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "tun-ipip"},
		Spec: api.TunnelInterfaceSpec{
			Mode:            "ipip",
			Local:           "192.0.2.10",
			Remote:          "192.0.2.20",
			TrustedUnderlay: true,
		},
	}}}}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: router,
		Store:  mapStore{},
		OS:     platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			if reflect.DeepEqual(append([]string{name}, args...), []string{"ip", "-d", "-o", "link", "show", "dev", "tun-ipip"}) {
				return []byte("Cannot find device \"tun-ipip\""), errors.New("missing")
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"ip", "-d", "-o", "link", "show", "dev", "tun-ipip"},
		{"ip", "link", "add", "dev", "tun-ipip", "type", "ipip", "local", "192.0.2.10", "remote", "192.0.2.20", "ttl", "64"},
		{"ip", "link", "set", "dev", "tun-ipip", "mtu", "1480", "up"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	status := controller.Store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip")
	if status["phase"] != "Up" || status["mode"] != "ipip" || status["mtu"] != 1480 {
		t.Fatalf("status = %#v", status)
	}
}

func TestTunnelInterfaceControllerResolvesEndpointSources(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"},
			Metadata: api.ObjectMeta{Name: "underlay"},
			Spec:     api.DHCPv4ClientSpec{Interface: "eth0"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
			Metadata: api.ObjectMeta{Name: "tun-ipip"},
			Spec: api.TunnelInterfaceSpec{
				Mode:            "ipip",
				LocalFrom:       api.StatusValueSourceSpec{Resource: "DHCPv4Client/underlay", Field: "currentAddress"},
				Remote:          "192.0.2.20",
				TrustedUnderlay: true,
			},
		},
	}}}
	store := mapStore{}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "underlay", map[string]any{"currentAddress": "192.0.2.10/24"}); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: router,
		Store:  store,
		OS:     platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			if reflect.DeepEqual(append([]string{name}, args...), []string{"ip", "-d", "-o", "link", "show", "dev", "tun-ipip"}) {
				return []byte("Cannot find device \"tun-ipip\""), errors.New("missing")
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"ip", "-d", "-o", "link", "show", "dev", "tun-ipip"},
		{"ip", "link", "add", "dev", "tun-ipip", "type", "ipip", "local", "192.0.2.10", "remote", "192.0.2.20", "ttl", "64"},
		{"ip", "link", "set", "dev", "tun-ipip", "mtu", "1480", "up"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	status := controller.Store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip")
	if status["phase"] != "Up" || status["local"] != "192.0.2.10" {
		t.Fatalf("status = %#v", status)
	}
}

func TestTunnelInterfaceControllerWaitsForEndpointSources(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"},
			Metadata: api.ObjectMeta{Name: "underlay"},
			Spec:     api.DHCPv4ClientSpec{Interface: "eth0"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
			Metadata: api.ObjectMeta{Name: "tun-ipip"},
			Spec: api.TunnelInterfaceSpec{
				Mode:            "ipip",
				LocalFrom:       api.StatusValueSourceSpec{Resource: "DHCPv4Client/underlay", Field: "currentAddress"},
				Remote:          "192.0.2.20",
				TrustedUnderlay: true,
			},
		},
	}}}
	store := mapStore{}
	controller := TunnelInterfaceController{
		Router: router,
		Store:  store,
		OS:     platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			t.Fatalf("pending endpoint source must not run commands, got %s %v", name, args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := controller.Store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip")
	if status["phase"] != "Pending" || status["reason"] != "EndpointSourcePending" || status["pendingSource"] != "DHCPv4Client/underlay.currentAddress" {
		t.Fatalf("status = %#v", status)
	}
}

func TestTunnelInterfaceControllerDerivesIPIPMTUFromUnderlay(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
			Metadata: api.ObjectMeta{Name: "wg-underlay"},
			Spec:     api.WireGuardInterfaceSpec{MTU: 1420},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
			Metadata: api.ObjectMeta{Name: "tun-ipip"},
			Spec: api.TunnelInterfaceSpec{
				Mode:              "ipip",
				Local:             "10.99.0.1",
				Remote:            "10.99.0.2",
				UnderlayInterface: "wg-underlay",
				TrustedUnderlay:   true,
			},
		},
	}}}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: router,
		Store:  mapStore{},
		OS:     platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			if reflect.DeepEqual(append([]string{name}, args...), []string{"ip", "-d", "-o", "link", "show", "dev", "tun-ipip"}) {
				return []byte("Cannot find device \"tun-ipip\""), errors.New("missing")
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"ip", "-d", "-o", "link", "show", "dev", "tun-ipip"},
		{"ip", "link", "add", "dev", "tun-ipip", "type", "ipip", "local", "10.99.0.1", "remote", "10.99.0.2", "ttl", "64"},
		{"ip", "link", "set", "dev", "tun-ipip", "mtu", "1400", "up"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	status := controller.Store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip")
	if status["phase"] != "Up" || status["mtu"] != 1400 || status["underlayInterface"] != "wg-underlay" || status["underlayMTU"] != 1420 || status["tunnelOverhead"] != 20 {
		t.Fatalf("status = %#v", status)
	}
}

func TestTunnelInterfaceControllerAddsFOU(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "tun-fou"},
		Spec: api.TunnelInterfaceSpec{
			Mode:            "fou",
			Local:           "192.0.2.10",
			Remote:          "192.0.2.20",
			EncapSport:      5555,
			EncapDport:      5556,
			TrustedUnderlay: true,
		},
	}}}}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: router,
		Store:  mapStore{},
		OS:     platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			if reflect.DeepEqual(append([]string{name}, args...), []string{"ip", "-d", "-o", "link", "show", "dev", "tun-fou"}) {
				return []byte("Cannot find device \"tun-fou\""), errors.New("missing")
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"ip", "-d", "-o", "link", "show", "dev", "tun-fou"},
		{"ip", "fou", "add", "port", "5555", "ipproto", "4"},
		{"ip", "link", "add", "dev", "tun-fou", "type", "ipip", "local", "192.0.2.10", "remote", "192.0.2.20", "ttl", "64", "encap", "fou", "encap-sport", "5555", "encap-dport", "5556"},
		{"ip", "link", "set", "dev", "tun-fou", "mtu", "1472", "up"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	status := controller.Store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-fou")
	if status["phase"] != "Up" || status["mode"] != "fou" || status["mtu"] != 1472 || status["encapSport"] != 5555 || status["encapDport"] != 5556 {
		t.Fatalf("status = %#v", status)
	}
}

func TestTunnelInterfaceControllerSkipsExistingGUE(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "tun-gue"},
		Spec: api.TunnelInterfaceSpec{
			Mode:            "gue",
			Local:           "192.0.2.10",
			Remote:          "192.0.2.20",
			TTL:             64,
			EncapSport:      6080,
			EncapDport:      6081,
			TrustedUnderlay: true,
		},
	}}}}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: router,
		Store:  mapStore{},
		OS:     platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			if reflect.DeepEqual(append([]string{name}, args...), []string{"ip", "fou", "add", "port", "6080", "gue"}) {
				return []byte("RTNETLINK answers: File exists"), errors.New("exists")
			}
			return []byte(`7: tun-gue@NONE: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1468 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000 link/ipip 192.0.2.10 peer 192.0.2.20 ttl 64 encap gue encap-sport 6080 encap-dport 6081`), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"ip", "-d", "-o", "link", "show", "dev", "tun-gue"},
		{"ip", "fou", "add", "port", "6080", "gue"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want only observe/listener ensure", calls)
	}
	status := controller.Store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-gue")
	if status["phase"] != "Up" || status["reason"] != "AlreadyConfigured" {
		t.Fatalf("status = %#v", status)
	}
}

func TestTunnelInterfaceControllerSkipsExistingGRE(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "tun-gre"},
		Spec: api.TunnelInterfaceSpec{
			Mode:            "gre",
			Local:           "192.0.2.10",
			Remote:          "192.0.2.20",
			TTL:             64,
			Key:             42,
			MTU:             1472,
			TrustedUnderlay: true,
		},
	}}}}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: router,
		Store:  mapStore{},
		OS:     platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			return []byte(`7: tun-gre@NONE: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1472 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000 link/gre 192.0.2.10 peer 192.0.2.20 ttl 64 key 42`), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"ip", "-d", "-o", "link", "show", "dev", "tun-gre"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want only observe", calls)
	}
	status := controller.Store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-gre")
	if status["phase"] != "Up" || status["reason"] != "AlreadyConfigured" {
		t.Fatalf("status = %#v", status)
	}
}

func TestTunnelInterfaceControllerChangesExistingGREWithoutAdd(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "tun-gre"},
		Spec: api.TunnelInterfaceSpec{
			Mode:            "gre",
			Local:           "192.0.2.10",
			Remote:          "192.0.2.20",
			TTL:             64,
			Key:             42,
			MTU:             1472,
			TrustedUnderlay: true,
		},
	}}}}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: router,
		Store:  mapStore{},
		OS:     platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			return []byte(`7: tun-gre@NONE: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1472 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000 link/gre 192.0.2.10 peer 192.0.2.30 ttl 64 key 42`), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"ip", "-d", "-o", "link", "show", "dev", "tun-gre"},
		{"ip", "tunnel", "change", "tun-gre", "mode", "gre", "local", "192.0.2.10", "remote", "192.0.2.20", "ttl", "64", "key", "42"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	for _, call := range calls {
		if strings.Join(call, " ") == "ip link add dev tun-gre type gre local 192.0.2.10 remote 192.0.2.20 ttl 64 key 42" {
			t.Fatalf("existing tunnel must not be added again: %#v", calls)
		}
	}
}

func TestTunnelInterfaceControllerDeletesStaleManagedInterface(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: nil}}
	store := mapStore{}
	store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", "old", map[string]any{
		"phase":     "Up",
		"ifname":    "tun-old",
		"managedBy": "routerd",
	})
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: router,
		Store:  store,
		OS:     platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"ip", "link", "del", "dev", "tun-old"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	if _, ok := store[api.HybridAPIVersion+"/TunnelInterface/old"]; ok {
		t.Fatalf("stale status was not deleted: %#v", store)
	}
}

func TestTunnelUnderlayRemovalOrderFixture(t *testing.T) {
	startup := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
			Metadata: api.ObjectMeta{Name: "tun-ipip"},
			Spec: api.TunnelInterfaceSpec{
				Mode:            "ipip",
				Local:           "192.0.2.10",
				Remote:          "192.0.2.20",
				TrustedUnderlay: true,
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"},
			Metadata: api.ObjectMeta{Name: "edge-main"},
			Spec:     api.OverlayPeerSpec{Role: "cloud", NodeID: "edge-1", Underlay: api.OverlayUnderlay{Type: "ipip", Interface: "tun-ipip"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "HybridRoute"},
			Metadata: api.ObjectMeta{Name: "edge-lan"},
			Spec:     api.HybridRouteSpec{DestinationCIDRs: []string{"10.20.0.0/16"}, PeerRef: "edge-main"},
		},
	}}}
	expanded, lowerings, err := hybrid.ExpandHybridRoutes(*startup)
	if err != nil {
		t.Fatal(err)
	}
	if len(lowerings) != 1 || lowerings[0].Device != "tun-ipip" {
		t.Fatalf("lowerings = %#v", lowerings)
	}
	routeStore := &routeCleanupStore{statuses: []routerstate.ObjectStatus{{
		APIVersion: api.NetAPIVersion,
		Kind:       "IPv4Route",
		Name:       lowerings[0].IPv4RouteName,
		Status: map[string]any{
			"phase":       "Installed",
			"type":        "unicast",
			"destination": "10.20.0.0/16",
			"device":      "tun-ipip",
		},
	}}}
	var routeCommands [][]string
	routeController := IPv4RouteController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			expanded.Spec.Resources[0],
		}}},
		Store: routeStore,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			routeCommands = append(routeCommands, append([]string{name}, args...))
			return nil, nil
		},
	}
	if err := routeController.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(routeCommands) != 1 || !reflect.DeepEqual(routeCommands[0], []string{"ip", "route", "del", "10.20.0.0/16", "dev", "tun-ipip"}) {
		t.Fatalf("route commands = %#v", routeCommands)
	}
	if !routeStore.deleted[api.NetAPIVersion+"/IPv4Route/"+lowerings[0].IPv4RouteName] {
		t.Fatalf("removed route status was not deleted")
	}

	tunnelStore := mapStore{}
	tunnelStore.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip", map[string]any{
		"phase":     "Up",
		"ifname":    "tun-ipip",
		"managedBy": "routerd",
	})
	var tunnelCommands [][]string
	tunnelController := TunnelInterfaceController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: nil}},
		Store:  tunnelStore,
		OS:     platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			tunnelCommands = append(tunnelCommands, append([]string{name}, args...))
			return nil, nil
		},
	}
	if err := tunnelController.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(tunnelCommands) != 1 || !reflect.DeepEqual(tunnelCommands[0], []string{"ip", "link", "del", "dev", "tun-ipip"}) {
		t.Fatalf("tunnel commands = %#v", tunnelCommands)
	}
}

func TestTunnelInterfaceControllerUnsupportedPlatform(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "tun-ipip"},
		Spec: api.TunnelInterfaceSpec{
			Mode:            "ipip",
			Local:           "192.0.2.10",
			Remote:          "192.0.2.20",
			TrustedUnderlay: true,
		},
	}}}}
	store := mapStore{}
	controller := TunnelInterfaceController{
		Router: router,
		Store:  store,
		OS:     platform.OSFreeBSD,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			t.Fatalf("unsupported platform must not run commands, got %s %v", name, args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip")
	if status["phase"] != "Unsupported" || status["reason"] != "PlatformUnsupported" {
		t.Fatalf("status = %#v", status)
	}
}

var _ routerstate.ObjectStatusLister = mapStore{}
var _ routerstate.ObjectDeleteStore = mapStore{}
