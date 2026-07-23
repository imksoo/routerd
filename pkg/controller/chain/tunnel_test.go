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
	store := mapStore{}
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
	if status["phase"] != "Up" || status["mode"] != "ipip" || status["mtu"] != 1480 || !statusBoolOrFalse(status["interfaceOwned"]) {
		t.Fatalf("status = %#v", status)
	}
}

func TestTunnelInterfaceControllerRefusesForeignExistingInterfaceOnBothOS(t *testing.T) {
	for name, tt := range map[string]struct {
		os      platform.OS
		ifname  string
		observe []byte
		command []string
	}{
		"linux": {
			os:      platform.OSLinux,
			ifname:  "tun-foreign",
			observe: []byte("7: tun-foreign@NONE: <POINTOPOINT,UP> mtu 1480 link/ipip 192.0.2.10 peer 192.0.2.20 ttl 64"),
			command: []string{"ip", "-d", "-o", "link", "show", "dev", "tun-foreign"},
		},
		"freebsd": {
			os:      platform.OSFreeBSD,
			ifname:  "gif0",
			observe: []byte("gif0: flags=8011<UP,POINTOPOINT,MULTICAST> metric 0 mtu 1480\n\ttunnel inet 192.0.2.10 --> 192.0.2.20\n"),
			command: []string{"ifconfig", "gif0"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
				TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
				Metadata: api.ObjectMeta{Name: tt.ifname},
				Spec:     api.TunnelInterfaceSpec{Mode: "ipip", Local: "192.0.2.10", Remote: "192.0.2.20", TrustedUnderlay: true},
			}}}}
			var calls [][]string
			store := mapStore{}
			controller := TunnelInterfaceController{
				Router: router, Store: store, OS: tt.os,
				Command: func(_ context.Context, command string, args ...string) ([]byte, error) {
					calls = append(calls, append([]string{command}, args...))
					return tt.observe, nil
				},
			}
			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile error = %v, want persisted foreign-interface status", err)
			}
			if !reflect.DeepEqual(calls, [][]string{tt.command}) {
				t.Fatalf("foreign interface mutation calls = %#v, want only observe %#v", calls, tt.command)
			}
			status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", tt.ifname)
			if status["reason"] != "ForeignInterface" || statusBoolOrFalse(status["interfaceOwned"]) {
				t.Fatalf("foreign status = %#v", status)
			}
		})
	}
}

func TestTunnelInterfaceControllerChangesExistingIPIPRemoteWithoutAdd(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "tun-ipip"},
		Spec: api.TunnelInterfaceSpec{
			Mode:            "ipip",
			Local:           "192.0.2.10",
			Remote:          "192.0.2.30",
			TTL:             64,
			TrustedUnderlay: true,
		},
	}}}}
	store := mapStore{api.HybridAPIVersion + "/TunnelInterface/tun-ipip": {"interfaceOwned": true}}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: router,
		Store:  store,
		OS:     platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			return []byte(`7: tun-ipip@NONE: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1480 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000 link/ipip 192.0.2.10 peer 192.0.2.20 ttl 64`), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"ip", "-d", "-o", "link", "show", "dev", "tun-ipip"},
		{"ip", "link", "set", "dev", "tun-ipip", "type", "ipip", "local", "192.0.2.10", "remote", "192.0.2.30", "ttl", "64"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	for _, call := range calls {
		if strings.Join(call, " ") == "ip link add dev tun-ipip type ipip local 192.0.2.10 remote 192.0.2.30 ttl 64" {
			t.Fatalf("existing tunnel must not be added again: %#v", calls)
		}
	}
	status := controller.Store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip")
	if status["phase"] != "Up" || status["remote"] != "192.0.2.30" {
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
	store := mapStore{api.HybridAPIVersion + "/TunnelInterface/tun-ipip": {"interfaceOwned": true}}
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

func TestTunnelInterfaceControllerAppliesTunnelAddress(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "tun-ipip"},
		Spec: api.TunnelInterfaceSpec{
			Mode:            "ipip",
			Local:           "192.0.2.10",
			Remote:          "192.0.2.20",
			Address:         "10.255.1.0/31",
			TrustedUnderlay: true,
		},
	}}}}
	store := mapStore{api.HybridAPIVersion + "/TunnelInterface/tun-ipip": {"interfaceOwned": true}}
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
		{"ip", "-o", "-4", "addr", "show", "dev", "tun-ipip"},
		{"ip", "addr", "replace", "10.255.1.0/31", "dev", "tun-ipip"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	status := controller.Store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip")
	if status["phase"] != "Up" || status["address"] != "10.255.1.0/31" {
		t.Fatalf("status = %#v", status)
	}
}

func TestTunnelInterfaceControllerChangesTunnelAddress(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "tun-ipip"},
		Spec: api.TunnelInterfaceSpec{
			Mode:            "ipip",
			Local:           "192.0.2.10",
			Remote:          "192.0.2.20",
			Address:         "10.255.1.2/31",
			TrustedUnderlay: true,
		},
	}}}}
	store := mapStore{api.HybridAPIVersion + "/TunnelInterface/tun-ipip": {"interfaceOwned": true}}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: router,
		Store:  store,
		OS:     platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := append([]string{name}, args...)
			calls = append(calls, call)
			switch strings.Join(call, " ") {
			case "ip -d -o link show dev tun-ipip":
				return []byte(`7: tun-ipip@NONE: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1480 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000 link/ipip 192.0.2.10 peer 192.0.2.20 ttl 64`), nil
			case "ip -o -4 addr show dev tun-ipip":
				return []byte("7: tun-ipip    inet 10.255.1.0/31 scope global tun-ipip\n"), nil
			default:
				return nil, nil
			}
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"ip", "-d", "-o", "link", "show", "dev", "tun-ipip"},
		{"ip", "-o", "-4", "addr", "show", "dev", "tun-ipip"},
		{"ip", "addr", "del", "10.255.1.0/31", "dev", "tun-ipip"},
		{"ip", "addr", "replace", "10.255.1.2/31", "dev", "tun-ipip"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	status := controller.Store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip")
	if status["phase"] != "Up" || status["address"] != "10.255.1.2/31" {
		t.Fatalf("status = %#v", status)
	}
}

func TestTunnelInterfaceControllerRemovesStaleTunnelAddress(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "tun-ipip"},
		Spec: api.TunnelInterfaceSpec{
			Mode:            "ipip",
			Local:           "192.0.2.10",
			Remote:          "192.0.2.20",
			Address:         "10.255.1.2/31",
			TrustedUnderlay: true,
		},
	}}}}
	store := mapStore{api.HybridAPIVersion + "/TunnelInterface/tun-ipip": {"interfaceOwned": true}}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: router,
		Store:  store,
		OS:     platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := append([]string{name}, args...)
			calls = append(calls, call)
			switch strings.Join(call, " ") {
			case "ip -d -o link show dev tun-ipip":
				return []byte(`7: tun-ipip@NONE: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1480 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000 link/ipip 192.0.2.10 peer 192.0.2.20 ttl 64`), nil
			case "ip -o -4 addr show dev tun-ipip":
				return []byte("7: tun-ipip    inet 10.255.1.0/31 scope global tun-ipip\n7: tun-ipip    inet 10.255.1.2/31 scope global tun-ipip\n"), nil
			default:
				return nil, nil
			}
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"ip", "-d", "-o", "link", "show", "dev", "tun-ipip"},
		{"ip", "-o", "-4", "addr", "show", "dev", "tun-ipip"},
		{"ip", "addr", "del", "10.255.1.0/31", "dev", "tun-ipip"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
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
	store := mapStore{}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: router,
		Store:  store,
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

func TestTunnelInterfaceControllerReusesExistingFOUListener(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "tun-fou"},
		Spec: api.TunnelInterfaceSpec{
			Mode:            "fou",
			Local:           "192.0.2.10",
			Remote:          "192.0.2.20",
			EncapSport:      5555,
			EncapDport:      5555,
			TrustedUnderlay: true,
		},
	}}}}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: router,
		Store:  mapStore{},
		OS:     platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := append([]string{name}, args...)
			calls = append(calls, call)
			switch strings.Join(call, " ") {
			case "ip -d -o link show dev tun-fou":
				return []byte("Cannot find device \"tun-fou\""), errors.New("missing")
			case "ip fou add port 5555 ipproto 4":
				return []byte("RTNETLINK answers: Address already in use"), errors.New("exit status 2")
			case "ip fou show":
				return []byte("port 5555 ipproto 4\n"), nil
			default:
				return nil, nil
			}
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := controller.Store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-fou"); status["phase"] != "Up" {
		t.Fatalf("status = %#v, want Up", status)
	}
	if got, want := strings.Join(calls[3], " "), "ip link add dev tun-fou type ipip local 192.0.2.10 remote 192.0.2.20 ttl 64 encap fou encap-sport 5555 encap-dport 5555"; got != want {
		t.Fatalf("fourth call = %q, want %q", got, want)
	}
}

func TestTunnelInterfaceControllerRejectsWrongExistingFOUListenerShape(t *testing.T) {
	resource := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "tun-fou"},
		Spec:     api.TunnelInterfaceSpec{Mode: "fou", Local: "192.0.2.10", Remote: "192.0.2.20", EncapSport: 5555, EncapDport: 5555, TrustedUnderlay: true},
	}
	var calls [][]string
	store := mapStore{}
	controller := TunnelInterfaceController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}, Store: store, OS: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := append([]string{name}, args...)
			calls = append(calls, call)
			switch strings.Join(call, " ") {
			case "ip -d -o link show dev tun-fou":
				return []byte("Cannot find device \"tun-fou\""), errors.New("missing")
			case "ip fou add port 5555 ipproto 4":
				return []byte("RTNETLINK answers: File exists"), errors.New("exists")
			case "ip fou show":
				return []byte("port 5555 gue\n"), nil
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-fou")
	if status["phase"] != "Error" || status["reason"] != "ApplyFailed" || !strings.Contains(statusString(status, "error"), "does not match") {
		t.Fatalf("status = %#v, want fail-closed listener-shape error", status)
	}
	for _, call := range calls {
		if strings.Join(call, " ") == "ip link add dev tun-fou type ipip local 192.0.2.10 remote 192.0.2.20 ttl 64 encap fou encap-sport 5555 encap-dport 5555" {
			t.Fatalf("wrong listener shape must not create tunnel: %#v", calls)
		}
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
	store := mapStore{api.HybridAPIVersion + "/TunnelInterface/tun-gue": {"interfaceOwned": true}}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: router,
		Store:  store,
		OS:     platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			if reflect.DeepEqual(append([]string{name}, args...), []string{"ip", "fou", "add", "port", "6080", "gue"}) {
				return []byte("RTNETLINK answers: File exists"), errors.New("exists")
			}
			if reflect.DeepEqual(append([]string{name}, args...), []string{"ip", "fou", "show"}) {
				return []byte("port 6080 gue\n"), nil
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
		{"ip", "fou", "show"},
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
	store := mapStore{api.HybridAPIVersion + "/TunnelInterface/tun-gre": {"interfaceOwned": true}}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: router,
		Store:  store,
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
	store := mapStore{api.HybridAPIVersion + "/TunnelInterface/tun-gre": {"interfaceOwned": true}}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: router,
		Store:  store,
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
		{"ip", "link", "set", "dev", "tun-gre", "type", "gre", "local", "192.0.2.10", "remote", "192.0.2.20", "ttl", "64", "key", "42"},
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

func TestTunnelInterfaceControllerClearsLinuxGREKeyWithNoKey(t *testing.T) {
	resource := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "tun-gre"},
		Spec: api.TunnelInterfaceSpec{
			Mode: "gre", Local: "192.0.2.10", Remote: "192.0.2.20", TTL: 64, TrustedUnderlay: true,
		},
	}
	var calls [][]string
	store := mapStore{api.HybridAPIVersion + "/TunnelInterface/tun-gre": {"interfaceOwned": true}}
	controller := TunnelInterfaceController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}, Store: store, OS: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := append([]string{name}, args...)
			calls = append(calls, call)
			if strings.Join(call, " ") == "ip -d -o link show dev tun-gre" {
				return []byte(`7: tun-gre@NONE: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1476 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000 link/gre 192.0.2.10 peer 192.0.2.20 ttl 64 key 42`), nil
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"ip", "link", "set", "dev", "tun-gre", "type", "gre", "local", "192.0.2.10", "remote", "192.0.2.20", "ttl", "64", "nokey"}
	if len(calls) != 2 || !reflect.DeepEqual(calls[1], want) {
		t.Fatalf("calls = %#v, want GRE change %#v", calls, want)
	}
}

func TestTunnelInterfaceControllerClearsFreeBSDGREKeyWithOwnedProvenance(t *testing.T) {
	owned := mapStore{api.HybridAPIVersion + "/TunnelInterface/gre0": {"interfaceOwned": true, "key": 42}}
	zeroKey := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "gre0"},
		Spec:     api.TunnelInterfaceSpec{Mode: "gre", Local: "192.0.2.10", Remote: "192.0.2.20", MTU: 1472, TrustedUnderlay: true},
	}
	// This is the native FreeBSD ifconfig shape captured by the VNET smoke:
	// it omits grekey even though routerd's owned prior status recorded key 42.
	observed := []byte("gre0: flags=8011<UP,POINTOPOINT,MULTICAST> metric 0 mtu 1472\n\ttunnel inet 192.0.2.10 --> 192.0.2.20\n")
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{zeroKey}}}, Store: owned, OS: platform.OSFreeBSD,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			return observed, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("clear reconcile error = %v", err)
	}
	status := owned.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "gre0")
	if status["phase"] != "Up" || !statusBoolOrFalse(status["interfaceOwned"]) {
		t.Fatalf("clear status = %#v", status)
	}
	want := []string{"ifconfig", "gre0", "grekey", "0"}
	if len(calls) < 3 || !reflect.DeepEqual(calls[2], want) {
		t.Fatalf("calls = %#v, want clear %#v", calls, want)
	}
}

func TestTunnelInterfaceControllerUsesOwnedFreeBSDGREKeyForNoOp(t *testing.T) {
	resource := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "gre0"},
		Spec:     api.TunnelInterfaceSpec{Mode: "gre", Local: "192.0.2.10", Remote: "192.0.2.20", MTU: 1472, Key: 42, TrustedUnderlay: true},
	}
	owned := mapStore{api.HybridAPIVersion + "/TunnelInterface/gre0": {"interfaceOwned": true, "key": 42}}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}, Store: owned, OS: platform.OSFreeBSD,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			return []byte("gre0: flags=8011<UP,POINTOPOINT,MULTICAST> metric 0 mtu 1472\n\ttunnel inet 192.0.2.10 --> 192.0.2.20\n"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("same-key reconcile error = %v", err)
	}
	status := owned.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "gre0")
	if status["phase"] != "Up" || status["reason"] != "AlreadyConfigured" || !statusBoolOrFalse(status["interfaceOwned"]) {
		t.Fatalf("same-key status = %#v", status)
	}
	if len(calls) != 1 {
		t.Fatalf("same-key no-op calls = %#v", calls)
	}
}

func TestTunnelInterfaceControllerClearsParseableFreeBSDGREKey(t *testing.T) {
	zeroKey := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "gre0"},
		Spec:     api.TunnelInterfaceSpec{Mode: "gre", Local: "192.0.2.10", Remote: "192.0.2.20", MTU: 1472, TrustedUnderlay: true},
	}
	owned := mapStore{api.HybridAPIVersion + "/TunnelInterface/gre0": {"interfaceOwned": true}}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{zeroKey}}}, Store: owned, OS: platform.OSFreeBSD,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			return []byte("gre0: flags=8011<UP,POINTOPOINT,MULTICAST> metric 0 mtu 1472\n\ttunnel inet 192.0.2.10 --> 192.0.2.20\n\tgrekey: 42\n"), nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("clear reconcile error = %v", err)
	}
	status := owned.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "gre0")
	if status["phase"] != "Up" || !statusBoolOrFalse(status["interfaceOwned"]) {
		t.Fatalf("parseable key clear status = %#v", status)
	}
	want := []string{"ifconfig", "gre0", "grekey", "0"}
	if len(calls) < 3 || !reflect.DeepEqual(calls[2], want) {
		t.Fatalf("calls = %#v, want clear %#v", calls, want)
	}
}

func TestParseFreeBSDTunnelStatusParsesHexGREKey(t *testing.T) {
	observed := parseFreeBSDTunnelStatus("gre0", []byte("gre0: flags=8011<UP,POINTOPOINT,MULTICAST> metric 0 mtu 1472\n\ttunnel inet 192.0.2.10 --> 192.0.2.20\n\tgrekey: 0x2a (42)\n"))
	if observed.Key != 42 {
		t.Fatalf("GRE key = %d, want 42", observed.Key)
	}
}

func TestTunnelInterfaceControllerRemovesAddressWhenSpecCleared(t *testing.T) {
	resource := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "tun-ipip"},
		Spec: api.TunnelInterfaceSpec{
			Mode: "ipip", Local: "192.0.2.10", Remote: "192.0.2.20", TrustedUnderlay: true,
		},
	}
	store := mapStore{}
	if err := store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip", map[string]any{
		"phase": "Up", "managedBy": "routerd", "address": "10.255.1.1/30", "interfaceOwned": true,
	}); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}, Store: store, OS: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := append([]string{name}, args...)
			calls = append(calls, call)
			switch strings.Join(call, " ") {
			case "ip -d -o link show dev tun-ipip":
				return []byte(`7: tun-ipip@NONE: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1480 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000 link/ipip 192.0.2.10 peer 192.0.2.20 ttl 64`), nil
			case "ip -o -4 addr show dev tun-ipip":
				return []byte("7: tun-ipip    inet 10.255.1.1/30 scope global tun-ipip\n"), nil
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"ip", "addr", "del", "10.255.1.1/30", "dev", "tun-ipip"}
	if len(calls) != 3 || !reflect.DeepEqual(calls[2], want) {
		t.Fatalf("calls = %#v, want address removal %#v", calls, want)
	}
}

func TestTunnelInterfaceControllerTransfersSharedFOUListenerOwnership(t *testing.T) {
	resource := func(name string) api.Resource {
		return api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
			Metadata: api.ObjectMeta{Name: name},
			Spec:     api.TunnelInterfaceSpec{Mode: "fou", Local: "192.0.2.10", Remote: "192.0.2.20", EncapSport: 5555, EncapDport: 5555, TrustedUnderlay: true},
		}
	}
	store := mapStore{}
	var calls [][]string
	created := map[string]bool{}
	controller := TunnelInterfaceController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource("tun-a"), resource("tun-b")}}}, Store: store, OS: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := append([]string{name}, args...)
			calls = append(calls, call)
			switch strings.Join(call, " ") {
			case "ip -d -o link show dev tun-a":
				if created["tun-a"] {
					return []byte(`7: tun-a@NONE: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1472 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000 link/ipip 192.0.2.10 peer 192.0.2.20 ttl 64 encap fou encap-sport 5555 encap-dport 5555`), nil
				}
				return []byte("Cannot find device"), errors.New("missing")
			case "ip -d -o link show dev tun-b":
				if created["tun-b"] {
					return []byte(`7: tun-b@NONE: <POINTOPOINT,NOARP,UP,LOWER_UP> mtu 1472 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000 link/ipip 192.0.2.10 peer 192.0.2.20 ttl 64 encap fou encap-sport 5555 encap-dport 5555`), nil
				}
				return []byte("Cannot find device"), errors.New("missing")
			case "ip fou add port 5555 ipproto 4":
				for _, existing := range calls[:len(calls)-1] {
					if strings.Join(existing, " ") == "ip fou add port 5555 ipproto 4" {
						return []byte("RTNETLINK answers: File exists"), errors.New("exists")
					}
				}
			case "ip fou show":
				return []byte("port 5555 ipproto 4\n"), nil
			}
			if len(call) >= 5 && call[0] == "ip" && call[1] == "link" && call[2] == "add" && call[3] == "dev" {
				created[call[4]] = true
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tun-a", "tun-b"} {
		status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", name)
		if owned, _ := statusBool(status["fouListenerOwned"]); !owned {
			t.Fatalf("%s did not inherit routerd FOU ownership: %#v", name, status)
		}
	}
	controller.Router = &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource("tun-b")}}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, call := range calls {
		if strings.Join(call, " ") == "ip fou del port 5555" {
			t.Fatalf("shared listener was deleted while tun-b still desired: %#v", calls)
		}
	}
	if status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-b"); !statusBoolOrFalse(status["fouListenerOwned"]) {
		t.Fatalf("tun-b lost transferred ownership: %#v", status)
	}
	controller.Router = &api.Router{}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	deletes := 0
	for _, call := range calls {
		if strings.Join(call, " ") == "ip fou del port 5555" {
			deletes++
		}
	}
	if deletes != 1 {
		t.Fatalf("listener delete calls = %d, want exactly one; calls=%#v", deletes, calls)
	}
}

func statusBoolOrFalse(value any) bool {
	ok, _ := statusBool(value)
	return ok
}

func TestTunnelInterfaceControllerDeletesStaleManagedInterface(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: nil}}
	store := mapStore{}
	store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", "old", map[string]any{
		"phase":          "Up",
		"ifname":         "tun-old",
		"managedBy":      "routerd",
		"interfaceOwned": true,
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

func TestTunnelInterfaceControllerPreservesStaleUnownedStatus(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: nil}}
	store := mapStore{}
	store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", "old", map[string]any{
		"phase": "Pending", "ifname": "tun-foreign", "managedBy": "routerd",
	})
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: router, Store: store, OS: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("unowned stale interface was mutated: %#v", calls)
	}
	if status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "old"); status["ifname"] != "tun-foreign" {
		t.Fatalf("unowned stale status was removed: %#v", status)
	}
}

func TestTunnelUnderlayRemovalOrderFixture(t *testing.T) {
	requireLinuxRuntimeFixture(t)
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
	wantRouteCommands := [][]string{
		{"ip", "route", "show", "10.20.0.0/16"},
		{"ip", "route", "del", "10.20.0.0/16", "dev", "tun-ipip"},
	}
	if !reflect.DeepEqual(routeCommands, wantRouteCommands) {
		t.Fatalf("route commands = %#v", routeCommands)
	}
	if !routeStore.deleted[api.NetAPIVersion+"/IPv4Route/"+lowerings[0].IPv4RouteName] {
		t.Fatalf("removed route status was not deleted")
	}

	tunnelStore := mapStore{}
	tunnelStore.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip", map[string]any{
		"phase":          "Up",
		"ifname":         "tun-ipip",
		"managedBy":      "routerd",
		"interfaceOwned": true,
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
		OS:     platform.OSOther,
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

func TestTunnelInterfaceControllerFreeBSDGIFLifecycle(t *testing.T) {
	resource := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "gif0"},
		Spec: api.TunnelInterfaceSpec{
			Mode: "ipip", Local: "192.0.2.10", Remote: "192.0.2.20", Address: "10.99.0.1/30", PeerAddress: "10.99.0.2", MTU: 1400, TrustedUnderlay: true,
		},
	}
	var calls [][]string
	lookups := 0
	controller := TunnelInterfaceController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}},
		Store:  mapStore{},
		OS:     platform.OSFreeBSD,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			if reflect.DeepEqual(append([]string{name}, args...), []string{"ifconfig", "gif0"}) {
				lookups++
				if lookups == 1 {
					return []byte("ifconfig: interface gif0 does not exist"), errors.New("exit status 1")
				}
				if lookups == 2 {
					return []byte("gif0: flags=8843<UP,RUNNING> metric 0 mtu 1400\n\ttunnel inet 192.0.2.10 --> 192.0.2.20\n"), nil
				}
				return []byte("gif0: flags=8843<UP,RUNNING> metric 0 mtu 1400\n\ttunnel inet 192.0.2.10 --> 192.0.2.20\n\tinet 10.99.0.1 --> 10.99.0.2 netmask 0xfffffffc\n"), nil
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"ifconfig", "gif0"},
		{"ifconfig", "gif0", "create"},
		{"ifconfig", "gif0", "tunnel", "192.0.2.10", "192.0.2.20"},
		{"ifconfig", "gif0", "mtu", "1400", "up"},
		{"ifconfig", "gif0"},
		{"ifconfig", "gif0", "inet", "10.99.0.1/30", "10.99.0.2"},
		{"ifconfig", "gif0"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestFreeBSDTunnelPeerAddress(t *testing.T) {
	out := []byte("gif0: flags=8843<UP,RUNNING> metric 0 mtu 1400\n\tinet 10.99.0.1 --> 10.99.0.2 netmask 0xfffffffc\n")
	if got, want := freeBSDTunnelPeerAddress(out, "10.99.0.1/30"), "10.99.0.2"; got != want {
		t.Fatalf("peer = %q, want %q", got, want)
	}
	if got := freeBSDTunnelPeerAddress(out, "10.99.0.5/30"); got != "" {
		t.Fatalf("unexpected peer = %q", got)
	}
}

func TestTunnelInterfaceControllerFreeBSDReconfiguresChangedPeerAddress(t *testing.T) {
	resource := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "gif0"},
		Spec:     api.TunnelInterfaceSpec{Mode: "ipip", Local: "192.0.2.10", Remote: "192.0.2.20", Address: "10.99.0.1/30", PeerAddress: "10.99.0.2", TrustedUnderlay: true},
	}
	store := mapStore{}
	if err := store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", "gif0", map[string]any{"phase": "Up", "interfaceOwned": true}); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	configured := false
	controller := TunnelInterfaceController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}, Store: store, OS: platform.OSFreeBSD,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := append([]string{name}, args...)
			calls = append(calls, call)
			if reflect.DeepEqual(call, []string{"ifconfig", "gif0", "inet", "10.99.0.1/30", "10.99.0.2"}) {
				configured = true
			}
			if reflect.DeepEqual(call, []string{"ifconfig", "gif0"}) {
				peer := "10.99.0.9"
				if configured {
					peer = "10.99.0.2"
				}
				return []byte("gif0: flags=8843<UP,RUNNING> metric 0 mtu 1280\n\ttunnel inet 192.0.2.10 --> 192.0.2.20\n\tinet 10.99.0.1 --> " + peer + " netmask 0xfffffffc\n"), nil
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"ifconfig", "gif0", "inet", "10.99.0.1/30", "10.99.0.2"}
	found := false
	for _, call := range calls {
		if reflect.DeepEqual(call, want) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("calls = %#v, want peer reconfiguration %v", calls, want)
	}
	status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "gif0")
	if status["observedPeerAddress"] != "10.99.0.2" || status["observedAddress"] != "10.99.0.1/30" {
		t.Fatalf("status = %#v", status)
	}
}

func TestTunnelInterfaceControllerFreeBSDStaleCleanup(t *testing.T) {
	store := mapStore{}
	if err := store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", "old-gif", map[string]any{
		"phase": "Up", "managedBy": "routerd", "ifname": "gif3", "interfaceOwned": true,
	}); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	controller := TunnelInterfaceController{
		Router: &api.Router{}, Store: store, OS: platform.OSFreeBSD,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, [][]string{{"ifconfig", "gif3", "destroy"}}) {
		t.Fatalf("calls = %#v", calls)
	}
	if _, ok := store[api.HybridAPIVersion+"/TunnelInterface/old-gif"]; ok {
		t.Fatal("stale FreeBSD tunnel status was not removed")
	}
}

var _ routerstate.ObjectStatusLister = mapStore{}
var _ routerstate.ObjectDeleteStore = mapStore{}
