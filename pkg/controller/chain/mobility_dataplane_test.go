// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"net/netip"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// mobilityEffectStore is deliberately in-memory. The direct dataplane
// adapters require an applied-effect ledger, but tests must not consult or
// mutate host networking to exercise it.
type mobilityEffectStore struct {
	statuses map[string]map[string]any
}

func (s *mobilityEffectStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	if s.statuses == nil {
		s.statuses = map[string]map[string]any{}
	}
	s.statuses[apiVersion+"/"+kind+"/"+name] = copyStatusMap(status)
	return nil
}

func (s *mobilityEffectStore) MergeObjectStatus(apiVersion, kind, name string, updates map[string]any) error {
	current := copyStatusMap(s.ObjectStatus(apiVersion, kind, name))
	for key, value := range updates {
		current[key] = value
	}
	return s.SaveObjectStatus(apiVersion, kind, name, current)
}

func (s *mobilityEffectStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if s.statuses == nil {
		return map[string]any{}
	}
	return s.statuses[apiVersion+"/"+kind+"/"+name]
}

func (s *mobilityEffectStore) ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error) {
	return s.ObjectStatus(apiVersion, kind, name), nil
}

func (s *mobilityEffectStore) ListObjectStatuses() ([]routerstate.ObjectStatus, error) {
	var out []routerstate.ObjectStatus
	for key, status := range s.statuses {
		parts := strings.Split(key, "/")
		last := len(parts) - 1
		out = append(out, routerstate.ObjectStatus{APIVersion: strings.Join(parts[:last-1], "/"), Kind: parts[last-1], Name: parts[last], Status: copyStatusMap(status)})
	}
	return out, nil
}

func TestIPv4RouteControllerAppliesTypedMobilityRouteWithoutGenericResource(t *testing.T) {
	store := &mobilityEffectStore{}
	var commands []string
	native := &nativeRouteFixture{}
	controller := IPv4RouteController{
		NativeScope: ordinaryFixtureScope, DeviceLookup: func(context.Context, string) (int, bool, error) { return 17, true, nil },
		Router: &api.Router{}, Store: store,
		MobilityDataplane: dynamicconfig.MobilityDataplanePlan{Routes: []dynamicconfig.MobilityIPv4RouteIntent{{
			ID: "cloudedge/capture-prefix", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Purpose: dynamicconfig.MobilityIPv4RoutePurposeCapturePrefix,
			Destination: "10.77.60.0/24", Device: "lan0", Metric: 90,
		}}},
		DevicePresent: func(context.Context, string) bool { return true },
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return native.command(ctx, name, args...)
		},
	}
	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(controller.Router.Spec.Resources) != 0 {
		t.Fatalf("typed mobility route must not become a generic resource: %#v", controller.Router.Spec.Resources)
	}
	want := "ip route add 10.77.60.0/24 dev lan0 metric 90 proto boot"
	if platform.CurrentOS() == platform.OSFreeBSD {
		want = "route -n add -net 10.77.60.0/24 -interface lan0"
	}
	if !containsMobilityDataplaneCommand(commands, want) {
		t.Fatalf("commands = %#v", commands)
	}
	routes, err := controller.appliedMobilityRoutes()
	if err != nil {
		t.Fatalf("read applied routes: %v", err)
	}
	if len(routes) != 1 || routes[0].Destination != "10.77.60.0/24" || routes[0].Device != "lan0" {
		t.Fatalf("applied mobility routes = %#v", routes)
	}
}

func TestIPv4RouteControllerDoesNotDeleteBGPRouteWhenTypedMobilityEffectWithdraws(t *testing.T) {
	store := &mobilityEffectStore{statuses: map[string]map[string]any{
		api.RouterAPIVersion + "/Router/" + samDataplaneStatusName: {
			"appliedMobilityRoutes": []mobilityAppliedIPv4Route{{
				ID: "cloudedge/capture-prefix", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Purpose: string(dynamicconfig.MobilityIPv4RoutePurposeCapturePrefix), Scope: "linux:fixture:1", IfIndex: 17,
				Destination: "10.77.60.0/24", Device: "lan0", Metric: 90,
			}},
		},
	}}
	var commands []string
	native := &nativeRouteFixture{route: &ordinaryIPv4RouteObservation{Type: "unicast", Destination: "10.77.60.0/24", Device: "lan0", Protocol: "bgp", Metric: 90}}
	controller := IPv4RouteController{
		NativeScope: ordinaryFixtureScope, DeviceLookup: func(context.Context, string) (int, bool, error) { return 17, true, nil },
		Router: &api.Router{}, Store: store,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			command := name + " " + strings.Join(args, " ")
			commands = append(commands, command)
			return native.command(ctx, name, args...)
		},
	}
	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, command := range commands {
		if strings.Contains(command, " route del ") || strings.Contains(command, "route -n delete ") {
			t.Fatalf("BGP route must not be deleted: commands = %#v", commands)
		}
	}
	if native.writes != 0 || native.route == nil || native.route.Protocol != "bgp" {
		t.Fatalf("foreign BGP route was changed: writes=%d route=%+v", native.writes, native.route)
	}
	if routes, err := controller.appliedMobilityRoutes(); err != nil || len(routes) != 0 {
		t.Fatalf("withdrawn BGP-protected mobility ledger = %#v, want empty", routes)
	}
}

func TestIPv4RouteControllerWithdrawsTypedMobilityRouteFromItsAppliedLedger(t *testing.T) {
	store := &mobilityEffectStore{statuses: map[string]map[string]any{
		api.RouterAPIVersion + "/Router/" + samDataplaneStatusName: {
			"appliedMobilityRoutes": []mobilityAppliedIPv4Route{{
				ID: "cloudedge/local-10.77.60.20", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Purpose: string(dynamicconfig.MobilityIPv4RoutePurposeLocalInventory), Scope: "linux:fixture:1", IfIndex: 17,
				Destination: "10.77.60.20/32", Device: "lan0", PreferredSource: "10.77.60.1", Metric: 90,
			}},
		},
	}}
	var commands []string
	native := &nativeRouteFixture{route: &ordinaryIPv4RouteObservation{Type: "unicast", Destination: "10.77.60.20", Device: "lan0", Protocol: "boot", PreferredSource: "10.77.60.1", Metric: 90}}
	controller := IPv4RouteController{
		NativeScope: ordinaryFixtureScope, DeviceLookup: func(context.Context, string) (int, bool, error) { return 17, true, nil },
		Router: &api.Router{}, Store: store,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			command := name + " " + strings.Join(args, " ")
			commands = append(commands, command)
			return native.command(ctx, name, args...)
		},
	}
	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := "ip route del 10.77.60.20/32 dev lan0 metric 90 proto boot"
	if platform.CurrentOS() == platform.OSFreeBSD {
		want = "route -n delete -host 10.77.60.20 -interface lan0 -ifp lan0"
	}
	if !containsMobilityDataplaneCommand(commands, want) {
		t.Fatalf("typed mobility ledger route was not withdrawn: commands = %#v", commands)
	}
	if routes, err := controller.appliedMobilityRoutes(); err != nil || len(routes) != 0 {
		t.Fatalf("withdrawn mobility ledger = %#v, want empty", routes)
	}
}

func TestIPv4StaticAddressControllerAppliesAndWithdrawsTypedMobilityAddress(t *testing.T) {
	store := &mobilityEffectStore{}
	var commands []string
	present := false
	controller := IPv4StaticAddressController{
		Router: &api.Router{}, Store: store,
		MobilityDataplane: dynamicconfig.MobilityDataplanePlan{StaticAddresses: []dynamicconfig.MobilityIPv4AddressIntent{{
			ID: "cloudedge/capture-source", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Purpose: dynamicconfig.MobilityIPv4AddressPurposeCaptureSource,
			Interface: "lan0", Address: "10.77.60.1/32",
		}}},
		DevicePresent: func(context.Context, string) bool { return true },
		ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
			var addresses []netip.Prefix
			if present {
				addresses = []netip.Prefix{netip.MustParsePrefix("10.77.60.1/32")}
			}
			return staticAddressSnapshot{Index: 7, Scope: "address-fixture", Addresses: addresses}, nil
		},
		Command: func(_ context.Context, name string, args ...string) error {
			commands = append(commands, name+" "+strings.Join(args, " "))
			switch name {
			case "ip":
				present = args[2] == "add"
			case "ifconfig":
				present = args[3] == "alias"
			default:
				t.Fatalf("unexpected address command: %s %v", name, args)
			}
			return nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if addresses, err := controller.appliedMobilityStaticAddresses(); err != nil || len(addresses) != 1 {
		t.Fatalf("applied mobility addresses = %#v, err=%v", addresses, err)
	}
	want := "ip -4 addr add 10.77.60.1/32 dev lan0"
	if platform.CurrentOS() == platform.OSFreeBSD {
		want = "ifconfig lan0 inet 10.77.60.1/32 alias"
	}
	if len(commands) != 1 || commands[0] != want {
		t.Fatalf("apply commands = %#v", commands)
	}

	controller.MobilityDataplane = dynamicconfig.MobilityDataplanePlan{}

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if addresses, err := controller.appliedMobilityStaticAddresses(); err != nil || len(addresses) != 0 {
		t.Fatalf("withdrawn mobility addresses = %#v, err=%v", addresses, err)
	}
	want = "ip -4 addr del 10.77.60.1/32 dev lan0"
	if platform.CurrentOS() == platform.OSFreeBSD {
		want = "ifconfig lan0 inet 10.77.60.1/32 -alias"
	}
	if len(commands) != 2 || commands[1] != want {
		t.Fatalf("apply/withdraw commands = %#v", commands)
	}
}

func TestStaticAddressObservationRequiresExactPrefix(t *testing.T) {
	observed := staticAddressSnapshot{Index: 7, Addresses: []netip.Prefix{netip.MustParsePrefix("10.77.60.34/24")}}
	if observed.has("10.77.60.34/32") {
		t.Fatal("a wider external address must not satisfy a managed /32")
	}
	if !observed.has("10.77.60.34/24") {
		t.Fatal("exactly matching address was not detected")
	}
}

func TestMobilityRouteLedgerRejectsInvalidOrConflictingRowsBeforeCommands(t *testing.T) {
	for _, rows := range [][]mobilityAppliedIPv4Route{
		{{ID: "route-a", PoolRef: "cloudedge", Purpose: string(dynamicconfig.MobilityIPv4RoutePurposeCapturePrefix), Destination: "10.77.60.0/24", Device: "lan0"}}, // no poolPrefix
		{
			{ID: "route-a", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Purpose: string(dynamicconfig.MobilityIPv4RoutePurposeCapturePrefix), Destination: "10.77.60.0/24", Device: "lan0"},
			{ID: "route-a", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Purpose: string(dynamicconfig.MobilityIPv4RoutePurposeCapturePrefix), Destination: "10.77.60.1/32", Device: "lan0"},
		},
	} {
		store := &mobilityEffectStore{statuses: map[string]map[string]any{
			api.RouterAPIVersion + "/Router/" + samDataplaneStatusName: {"appliedMobilityRoutes": rows},
		}}
		var commands []string
		controller := IPv4RouteController{Router: &api.Router{}, Store: store, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return nil, nil
		}}
		if err := controller.reconcile(context.Background()); err == nil {
			t.Fatalf("invalid mobility route ledger %#v was accepted", rows)
		}
		if len(commands) != 0 {
			t.Fatalf("invalid mobility route ledger reached commands: %#v", commands)
		}
	}
}

func TestMobilityAddressLedgerRejectsInvalidOrConflictingRowsBeforeCommands(t *testing.T) {
	for _, rows := range [][]mobilityAppliedIPv4Address{
		{{ID: "address-a", PoolRef: "cloudedge", Purpose: string(dynamicconfig.MobilityIPv4AddressPurposeCaptureSource), Interface: "lan0", Address: "10.77.60.9/32"}}, // no poolPrefix
		{
			{ID: "address-a", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Purpose: string(dynamicconfig.MobilityIPv4AddressPurposeCaptureSource), Interface: "lan0", Address: "10.77.60.9/32"},
			{ID: "address-a", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Purpose: string(dynamicconfig.MobilityIPv4AddressPurposeCaptureSource), Interface: "lan0", Address: "10.77.60.10/32"},
		},
	} {
		store := &mobilityEffectStore{statuses: map[string]map[string]any{
			api.RouterAPIVersion + "/Router/" + samDataplaneStatusName: {"appliedMobilityStaticAddresses": rows},
		}}
		var commands []string
		controller := IPv4StaticAddressController{Router: &api.Router{}, Store: store, Command: func(_ context.Context, name string, args ...string) error {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return nil
		}}
		if err := controller.Reconcile(context.Background()); err == nil {
			t.Fatalf("invalid mobility address ledger %#v was accepted", rows)
		}
		if len(commands) != 0 {
			t.Fatalf("invalid mobility address ledger reached commands: %#v", commands)
		}
	}
}

func containsMobilityDataplaneCommand(commands []string, want string) bool {
	for _, command := range commands {
		if command == want {
			return true
		}
	}
	return false
}
