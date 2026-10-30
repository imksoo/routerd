// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/imksoo/routerd/internal/statusvalue"
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
	controller, native, store := newTunnelUnitController(t, router)
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.creates) != 1 || native.creates[0].Mode != "ipip" || native.creates[0].Local != "192.0.2.10" || native.creates[0].Remote != "192.0.2.20" || native.creates[0].TTL != 64 || native.aliases != 1 || len(native.applies) != 1 {
		t.Fatalf("native acquisition: creates=%+v aliases=%d applies=%+v", native.creates, native.aliases, native.applies)
	}
	records, err := controller.tunnelNativeReceipts()
	if err != nil || len(records) != 1 || records[0].Pending || records[0].Index < 1 || native.links["tun-ipip"].Token != records[0].Token {
		t.Fatalf("native receipt not acknowledged: %+v %v", records, err)
	}
	status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip")
	if status["phase"] != "Up" || status["mode"] != "ipip" || status["mtu"] != float64(1480) || !statusBoolOrFalse(status["interfaceOwned"]) {
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
			if tt.os == platform.OSLinux {
				controller, native, store := newTunnelUnitController(t, router)
				native.links[tt.ifname] = tunnelNativeLink{Exists: true, Scope: "linux:fixture:1", Index: 7, Kind: "ipip", Observed: tunnelObserved{Exists: true, Mode: "ipip", MTU: 1480, Local: "192.0.2.10", Remote: "192.0.2.20", TTL: 64, Up: true}}
				if err := controller.Reconcile(t.Context()); err == nil {
					t.Fatal("foreign native interface was accepted")
				}
				if len(native.creates) != 0 || len(native.applies) != 0 || native.aliases != 0 || len(native.deletes) != 0 {
					t.Fatalf("foreign native interface was mutated: %+v", native)
				}
				status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", tt.ifname)
				if status["phase"] != "Unknown" || statusBoolOrFalse(status["interfaceOwned"]) {
					t.Fatalf("foreign ownership status = %#v", status)
				}
				return
			}
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
	spec := router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
	spec.Remote = "192.0.2.20"
	router.Spec.Resources[0].Spec = spec
	controller, native, store := newTunnelUnitController(t, router)
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	index := native.links["tun-ipip"].Index
	spec.Remote = "192.0.2.30"
	router.Spec.Resources[0].Spec = spec
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.creates) != 1 || len(native.applies) != 2 || native.links["tun-ipip"].Index != index || native.applies[1].Plan.Remote != "192.0.2.30" || native.links["tun-ipip"].Observed.Remote != "192.0.2.30" {
		t.Fatalf("owned native endpoint update = %+v", native)
	}
	status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip")
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
	controller, native, store := newTunnelUnitController(t, router)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "underlay", map[string]any{"currentAddress": "192.0.2.10/24"}); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.creates) != 1 || native.creates[0].Local != "192.0.2.10" || native.links["tun-ipip"].Observed.Local != "192.0.2.10" {
		t.Fatalf("native source endpoint = %+v", native.creates)
	}
	status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip")
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
	controller, native, store := newTunnelUnitController(t, router)
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.applies) != 1 || native.applies[0].Plan.MTU != 1400 || native.links["tun-ipip"].Observed.MTU != 1400 {
		t.Fatalf("native MTU plan = %+v", native.applies)
	}
	status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip")
	if status["phase"] != "Up" || status["underlayInterface"] != "wg-underlay" || status["underlayMTU"] != float64(1420) || status["tunnelOverhead"] != float64(20) {
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
	controller, native, store := newTunnelUnitController(t, router)
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.applies) != 1 || native.applies[0].Plan.Address != "10.255.1.0/31" || !slices.Equal(native.links["tun-ipip"].Addresses, []string{"10.255.1.0/31"}) {
		t.Fatalf("native address application = %+v", native)
	}
	status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip")
	if status["phase"] != "Up" || status["address"] != "10.255.1.0/31" {
		t.Fatalf("status = %#v", status)
	}
}

func TestTunnelInterfaceControllerRepairsOwnedTunnelAfterAddressApplyFailure(t *testing.T) {
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
	controller, native, store := newTunnelUnitController(t, router)
	native.applyErr = errors.New("native address application failed")
	if err := controller.Reconcile(t.Context()); err == nil {
		t.Fatal("native address failure was accepted")
	}
	records, err := controller.tunnelNativeReceipts()
	if err != nil || len(records) != 1 || !records[0].Pending || records[0].Index < 1 || records[0].Token != native.links["tun-ipip"].Token {
		t.Fatalf("failed application lost native acquisition: %+v %v", records, err)
	}
	index, token := records[0].Index, records[0].Token
	native.applyErr = nil
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if len(native.creates) != 1 || len(native.applies) != 2 || native.links["tun-ipip"].Index != index || native.links["tun-ipip"].Token != token || !slices.Contains(native.links["tun-ipip"].Addresses, "10.255.1.0/31") {
		t.Fatalf("repair recreated or failed to repair the owned link: %+v", native)
	}
	status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip")
	if status["phase"] != "Up" || !statusBoolOrFalse(status["interfaceOwned"]) {
		t.Fatalf("repair status = %#v", status)
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
	spec := router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
	spec.Address = "10.255.1.0/31"
	router.Spec.Resources[0].Spec = spec
	controller, native, store := newTunnelUnitController(t, router)
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	index := native.links["tun-ipip"].Index
	spec.Address = "10.255.1.2/31"
	router.Spec.Resources[0].Spec = spec
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.creates) != 1 || len(native.applies) != 2 || native.applies[1].PreviousAddress != "10.255.1.0/31" || native.applies[1].Plan.Address != "10.255.1.2/31" || native.links["tun-ipip"].Index != index || !slices.Equal(native.links["tun-ipip"].Addresses, []string{"10.255.1.2/31"}) {
		t.Fatalf("native owned address transition = %+v", native)
	}
	if status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-ipip"); status["phase"] != "Up" || status["address"] != "10.255.1.2/31" {
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
	spec := router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
	spec.Address = "10.255.1.0/31"
	router.Spec.Resources[0].Spec = spec
	controller, native, _ := newTunnelUnitController(t, router)
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	spec.Address = "10.255.1.2/31"
	router.Spec.Resources[0].Spec = spec
	native.applyErr = errors.New("native address transition interrupted")
	if err := controller.Reconcile(t.Context()); err == nil {
		t.Fatal("interrupted address transition was accepted")
	}
	// The old address remains owned by the durable transition receipt.
	link := native.links["tun-ipip"]
	link.Addresses = append(link.Addresses, "10.255.1.2/31")
	native.links["tun-ipip"] = link
	native.applyErr = nil
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.creates) != 1 || len(native.applies) != 3 || native.applies[2].PreviousAddress != "10.255.1.0/31" || !slices.Equal(native.links["tun-ipip"].Addresses, []string{"10.255.1.2/31"}) {
		t.Fatalf("owned stale address survived repair: %+v", native)
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
	controller, native, store := newTunnelUnitController(t, router)
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.creates) != 1 || native.creates[0].Mode != "fou" || native.creates[0].EncapSport != 5555 || native.creates[0].EncapDport != 5556 || len(native.applies) != 1 || native.applies[0].Plan.MTU != 1472 || !slices.Equal(native.listenerAdds, []fouListener{{Mode: "fou", Port: 5555}}) {
		t.Fatalf("native FOU configuration = %+v", native)
	}
	records, err := controller.tunnelNativeReceipts()
	if err != nil || len(records) != 1 || !records[0].ListenerOwned || records[0].ListenerPending || records[0].Pending {
		t.Fatalf("FOU create ACK not held: %+v %v", records, err)
	}
	status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-fou")
	if status["phase"] != "Up" || status["mode"] != "fou" || status["mtu"] != float64(1472) || status["encapSport"] != float64(5555) || status["encapDport"] != float64(5556) {
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
	controller, native, store := newTunnelUnitController(t, router)
	listener := fouListener{Mode: "fou", Port: 5555}
	native.listeners[5555] = listener
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.creates) != 1 || len(native.applies) != 1 || len(native.listenerAdds) != 0 {
		t.Fatalf("matching external listener was not reused: %+v", native)
	}
	records, err := controller.tunnelNativeReceipts()
	if err != nil || len(records) != 1 || records[0].ListenerOwned || records[0].ListenerPending {
		t.Fatalf("external listener was adopted: %+v %v", records, err)
	}
	if status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-fou"); status["phase"] != "Up" {
		t.Fatalf("status = %#v", status)
	}
	controller.Router = &api.Router{}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if native.listeners[5555] != listener || len(native.listenerDeletes) != 0 {
		t.Fatalf("external listener was deleted: %+v", native)
	}
}

func TestTunnelInterfaceControllerRejectsWrongExistingFOUListenerShape(t *testing.T) {
	resource := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "tun-fou"},
		Spec:     api.TunnelInterfaceSpec{Mode: "fou", Local: "192.0.2.10", Remote: "192.0.2.20", EncapSport: 5555, EncapDport: 5555, TrustedUnderlay: true},
	}
	controller, native, store := newTunnelUnitController(t, &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}})
	native.listeners[5555] = fouListener{Mode: "gue", Port: 5555}
	if err := controller.Reconcile(t.Context()); err == nil {
		t.Fatal("wrong listener shape was accepted")
	}
	if len(native.creates) != 0 || len(native.applies) != 0 || len(native.listenerAdds) != 0 || len(native.listenerDeletes) != 0 || native.listeners[5555].Mode != "gue" {
		t.Fatalf("wrong listener shape caused native effects: %+v", native)
	}
	status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-fou")
	if status["phase"] != "Unknown" || statusBoolOrFalse(status["interfaceOwned"]) {
		t.Fatalf("wrong listener was published as acquired: %#v", status)
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
	controller, native, store := newTunnelUnitController(t, router)
	native.listeners[6080] = fouListener{Mode: "gue", Port: 6080}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	index, token := native.links["tun-gue"].Index, native.links["tun-gue"].Token
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.creates) != 1 || len(native.applies) != 1 || native.aliases != 1 || len(native.deletes) != 0 || len(native.listenerAdds) != 0 || len(native.listenerDeletes) != 0 || native.links["tun-gue"].Index != index || native.links["tun-gue"].Token != token {
		t.Fatalf("no-op caused native effects: %+v", native)
	}
	if status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-gue"); status["phase"] != "Up" {
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
	controller, native, store := newTunnelUnitController(t, router)
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	index, token := native.links["tun-gre"].Index, native.links["tun-gre"].Token
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.creates) != 1 || len(native.applies) != 1 || native.aliases != 1 || len(native.deletes) != 0 || len(native.listenerAdds) != 0 || len(native.listenerDeletes) != 0 || native.links["tun-gre"].Index != index || native.links["tun-gre"].Token != token {
		t.Fatalf("no-op caused native effects: %+v", native)
	}
	if status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-gre"); status["phase"] != "Up" {
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
	spec := router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
	spec.Remote = "192.0.2.30"
	router.Spec.Resources[0].Spec = spec
	controller, native, _ := newTunnelUnitController(t, router)
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	index := native.links["tun-gre"].Index
	spec.Remote = "192.0.2.20"
	router.Spec.Resources[0].Spec = spec
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.creates) != 1 || len(native.applies) != 2 || len(native.deletes) != 0 || native.applies[1].Plan.Key != 42 || native.applies[1].Plan.Remote != "192.0.2.20" || native.links["tun-gre"].Index != index || native.links["tun-gre"].Observed.Remote != "192.0.2.20" {
		t.Fatalf("owned GRE change recreated or changed wrong link: %+v", native)
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
	spec := resource.Spec.(api.TunnelInterfaceSpec)
	spec.Key = 42
	resource.Spec = spec
	controller, native, _ := newTunnelUnitController(t, &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}})
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	spec.Key = 0
	controller.Router.Spec.Resources[0].Spec = spec
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.creates) != 1 || len(native.applies) != 2 || native.applies[1].Plan.Key != 0 || native.links["tun-gre"].Observed.Key != 0 {
		t.Fatalf("native GRE key was not cleared: %+v", native)
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
	spec := resource.Spec.(api.TunnelInterfaceSpec)
	spec.Address = "10.255.1.1/30"
	resource.Spec = spec
	controller, native, _ := newTunnelUnitController(t, &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}})
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	spec.Address = ""
	controller.Router.Spec.Resources[0].Spec = spec
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.creates) != 1 || len(native.applies) != 2 || native.applies[1].PreviousAddress != "10.255.1.1/30" || native.applies[1].Plan.Address != "" || slices.Contains(native.links["tun-ipip"].Addresses, "10.255.1.1/30") || len(native.deletes) != 0 {
		t.Fatalf("owned address clearing failed: %+v", native)
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
	controller, native, _ := newTunnelUnitController(t, &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource("tun-a"), resource("tun-b")}}})
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	records, err := controller.tunnelNativeReceipts()
	if err != nil || len(records) != 2 || len(native.listenerAdds) != 1 || len(native.listenerDeletes) != 0 {
		t.Fatalf("shared listener acquisition: %+v %v", records, err)
	}
	owners := 0
	for _, record := range records {
		if record.ListenerOwned {
			owners++
		}
	}
	if owners != 1 {
		t.Fatalf("shared listener has %d authorities: %+v", owners, records)
	}
	controller.Router = &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource("tun-b")}}}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	records, err = controller.tunnelNativeReceipts()
	if err != nil || len(records) != 1 || records[0].Resource != "tun-b" || !records[0].ListenerOwned || len(native.listenerDeletes) != 0 {
		t.Fatalf("shared listener ownership was not transferred: %+v %v", records, err)
	}
	controller.Router = &api.Router{}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.listenerDeletes) != 1 || len(native.listeners) != 0 || len(native.deletes) != 2 {
		t.Fatalf("shared listener final withdrawal = %+v", native)
	}
}

func statusBoolOrFalse(value any) bool {
	ok, _ := statusvalue.ExtendedBool(value)
	return ok
}

func TestTunnelInterfaceControllerDeletesStaleManagedInterface(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"}, Metadata: api.ObjectMeta{Name: "tun-old"}, Spec: api.TunnelInterfaceSpec{Mode: "ipip", Local: "192.0.2.10", Remote: "192.0.2.20", TrustedUnderlay: true}}}}}
	controller, native, store := newTunnelUnitController(t, router)
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	index := native.links["tun-old"].Index
	controller.Router = &api.Router{}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	records, err := controller.tunnelNativeReceipts()
	if err != nil || len(records) != 0 || native.links["tun-old"].Exists || !slices.Equal(native.deletes, []int{index}) {
		t.Fatalf("stale native ownership not withdrawn: records=%+v io=%+v err=%v", records, native, err)
	}
	if status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tun-old"); status["phase"] != "Absent" {
		t.Fatalf("stale native was not reported absent: %#v", status)
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
	parentRouter := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{expanded.Spec.Resources[0]}}}
	parent, native, store := newTunnelUnitController(t, parentRouter)
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	records, err := parent.tunnelNativeReceipts()
	if err != nil || len(records) != 1 {
		t.Fatalf("missing acquired parent: %+v %v", records, err)
	}
	parentLink := native.links["tun-ipip"]
	routeStatus := ownedRouteStatusForTest(map[string]any{"phase": "Installed", "type": "unicast", "destination": "10.20.0.0/16", "device": "tun-ipip"})
	receipt := routeStatus["routeOwner"].(ipv4RouteReceipt)
	receipt.Scope, receipt.IfIndex, receipt.ParentToken = parentLink.Scope, parentLink.Index, "tunnel:"+records[0].Token
	routeStatus["routeOwner"] = receipt
	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", lowerings[0].IPv4RouteName, routeStatus); err != nil {
		t.Fatal(err)
	}
	routePresent := true
	var commands [][]string
	route := IPv4RouteController{Router: parentRouter, Store: store, Tunnels: &parent,
		NativeScope: func(context.Context) (string, error) { return parentLink.Scope, nil },
		DeviceLookup: func(context.Context, string) (int, bool, error) {
			link := native.links["tun-ipip"]
			return link.Index, link.Exists, nil
		},
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			if slices.Equal(args, []string{"-j", "-d", "-4", "route", "show", "10.20.0.0/16"}) {
				if routePresent {
					return []byte(`[{"dst":"10.20.0.0/16","dev":"tun-ipip","protocol":"boot"}]`), nil
				}
				return []byte(`[]`), nil
			}
			if len(args) >= 2 && args[0] == "route" && args[1] == "del" {
				routePresent = false
			}
			return nil, nil
		},
	}
	if err := route.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"ip", "-j", "-d", "-4", "route", "show", "10.20.0.0/16"}, {"ip", "-j", "-d", "-4", "route", "show", "10.20.0.0/16"}, {"ip", "route", "del", "10.20.0.0/16", "dev", "tun-ipip", "metric", "0", "proto", "boot"}, {"ip", "-j", "-d", "-4", "route", "show", "10.20.0.0/16"}}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("owned route removal commands = %#v", commands)
	}
	native.beforeDelete = func() {
		status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv4Route", lowerings[0].IPv4RouteName)
		if err != nil || status != nil || routePresent {
			t.Fatalf("parent deletion preceded owned route withdrawal: status=%+v present=%t err=%v", status, routePresent, err)
		}
	}
	parent.Router = &api.Router{}
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(native.deletes, []int{parentLink.Index}) || native.links["tun-ipip"].Exists {
		t.Fatalf("parent teardown did not follow route withdrawal: %+v", native)
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

func TestTunnelInterfaceControllerLinuxStaleCleanup(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"}, Metadata: api.ObjectMeta{Name: "gre7"}, Spec: api.TunnelInterfaceSpec{Mode: "gre", Local: "192.0.2.10", Remote: "192.0.2.20", TrustedUnderlay: true}}}}}
	controller, native, store := newTunnelUnitController(t, router)
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	index := native.links["gre7"].Index
	controller.Router = &api.Router{}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	records, err := controller.tunnelNativeReceipts()
	if err != nil || len(records) != 0 || native.links["gre7"].Exists || !slices.Equal(native.deletes, []int{index}) {
		t.Fatalf("stale native ownership not withdrawn: records=%+v io=%+v err=%v", records, native, err)
	}
	if status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "gre7"); status["phase"] != "Absent" {
		t.Fatalf("stale native was not reported absent: %#v", status)
	}
}

var _ routerstate.ObjectStatusLister = mapStore{}
var _ routerstate.ObjectDeleteStore = mapStore{}

// The fixture owns only Linux OS input/output. Reconcile owns plans, receipts,
// source checks and recovery; no host networking or global hook is involved.
type tunnelUnitIO struct {
	links                         map[string]tunnelNativeLink
	listeners                     map[int]fouListener
	creates                       []tunnelDesired
	applies                       []tunnelNativeReceipt
	aliases                       int
	deletes                       []int
	listenerAdds, listenerDeletes []fouListener
	applyErr                      error
	beforeDelete                  func()
}

func newTunnelUnitController(t *testing.T, router *api.Router) (TunnelInterfaceController, *tunnelUnitIO, *routerstate.SQLiteStore) {
	t.Helper()
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "tunnel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	native := &tunnelUnitIO{links: map[string]tunnelNativeLink{}, listeners: map[int]fouListener{}}
	controller := TunnelInterfaceController{Router: router, Store: store, OS: platform.OSLinux, NativeListeners: native}
	controller.NativeScope = func(ctx context.Context) (string, error) { return "linux:fixture:1", ctx.Err() }
	controller.ObserveNative = func(ctx context.Context, name string) (tunnelNativeLink, error) {
		link := native.links[name]
		link.Scope = "linux:fixture:1"
		link.Addresses = append([]string(nil), link.Addresses...)
		return link, ctx.Err()
	}
	controller.Command = func(_ context.Context, command string, args ...string) ([]byte, error) {
		if command != "ip" || len(args) < 6 || !slices.Equal(args[:3], []string{"link", "add", "dev"}) {
			return nil, fmt.Errorf("unexpected tunnel OS command %s %v", command, args)
		}
		value := func(flag string) string {
			at := slices.Index(args, flag)
			if at >= 0 && at+1 < len(args) {
				return args[at+1]
			}
			return ""
		}
		number := func(flag string) int {
			value := value(flag)
			if value == "" {
				return 0
			}
			n, err := strconv.Atoi(value)
			if err != nil {
				t.Fatalf("invalid numeric OS argument %s: %v", flag, err)
			}
			return n
		}
		name := args[3]
		if native.links[name].Exists {
			return nil, errors.New("native name already exists")
		}
		mode := value("encap")
		if mode == "" {
			mode = value("type")
		}
		created := tunnelDesired{Name: name, Mode: mode, Local: value("local"), Remote: value("remote"), TTL: number("ttl"), Key: number("key"), EncapSport: number("encap-sport"), EncapDport: number("encap-dport")}
		native.creates = append(native.creates, created)
		native.links[name] = tunnelNativeLink{Exists: true, Scope: "linux:fixture:1", Index: number("index"), Kind: value("type"), Observed: tunnelObserved{Exists: true, Mode: mode, Local: created.Local, Remote: created.Remote, TTL: created.TTL, Key: created.Key, EncapSport: created.EncapSport, EncapDport: created.EncapDport, MTU: 1480}}
		return nil, nil
	}
	controller.SetAliasByIndex = func(_ context.Context, index int, name, kind, proof, scope string) error {
		link := native.links[name]
		if !link.Exists || link.Index != index || link.Kind != kind || link.Token != "" || link.Scope != scope {
			return errors.New("native alias target changed")
		}
		var ok bool
		link.Token, ok = strings.CutPrefix(proof, "routerd-tunnel:")
		if !ok {
			return errors.New("invalid native alias")
		}
		native.aliases++
		native.links[name] = link
		return nil
	}
	controller.ApplyNative = func(_ context.Context, record tunnelNativeReceipt) error {
		link := native.links[record.Plan.Name]
		if !link.Exists || link.Index != record.Index || link.Kind != tunnelNativeKind(record.Plan.Mode) || link.Token != record.Token || link.Scope != record.Scope {
			return errors.New("native apply target changed")
		}
		native.applies = append(native.applies, record)
		if native.applyErr != nil {
			return native.applyErr
		}
		plan := record.Plan
		link.Observed = tunnelObserved{Exists: true, Mode: plan.Mode, Local: plan.Local, Remote: plan.Remote, MTU: plan.MTU, TTL: plan.TTL, Key: plan.Key, EncapSport: plan.EncapSport, EncapDport: plan.EncapDport, Up: true}
		if record.PreviousAddress != "" && record.PreviousAddress != plan.Address {
			link.Addresses = slices.DeleteFunc(link.Addresses, func(address string) bool { return address == record.PreviousAddress })
		}
		if plan.Address != "" && !slices.Contains(link.Addresses, plan.Address) {
			link.Addresses = append(link.Addresses, plan.Address)
		}
		native.links[plan.Name] = link
		return nil
	}
	controller.DeleteByIndex = func(_ context.Context, index int, name, kind, proof, scope string) error {
		link := native.links[name]
		if !link.Exists {
			return nil
		}
		alias := ""
		if link.Token != "" {
			alias = "routerd-tunnel:" + link.Token
		}
		if link.Index != index || link.Kind != kind || link.Scope != scope || alias != proof {
			return errors.New("native deletion target changed")
		}
		if native.beforeDelete != nil {
			native.beforeDelete()
		}
		native.deletes = append(native.deletes, index)
		delete(native.links, name)
		return nil
	}
	return controller, native, store
}

func (n *tunnelUnitIO) Observe(_ context.Context, listener fouListener) (bool, bool, error) {
	actual, exists := n.listeners[listener.Port]
	return exists, exists && actual == listener, nil
}
func (n *tunnelUnitIO) Add(_ context.Context, listener fouListener) error {
	if _, exists := n.listeners[listener.Port]; exists {
		return errors.New("native listener exists")
	}
	n.listenerAdds = append(n.listenerAdds, listener)
	n.listeners[listener.Port] = listener
	return nil
}
func (n *tunnelUnitIO) Delete(_ context.Context, listener fouListener) error {
	if actual, exists := n.listeners[listener.Port]; exists && actual != listener {
		return errors.New("native listener changed")
	}
	n.listenerDeletes = append(n.listenerDeletes, listener)
	delete(n.listeners, listener.Port)
	return nil
}
