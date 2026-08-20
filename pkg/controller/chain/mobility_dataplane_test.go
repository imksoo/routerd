// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
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

func TestIPv4RouteControllerAppliesTypedMobilityRouteWithoutGenericResource(t *testing.T) {
	store := &mobilityEffectStore{}
	var commands []string
	controller := IPv4RouteController{
		Router: &api.Router{}, Store: store,
		MobilityDataplane: dynamicconfig.MobilityDataplanePlan{Routes: []dynamicconfig.MobilityIPv4RouteIntent{{
			ID: "cloudedge/capture-prefix", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Purpose: dynamicconfig.MobilityIPv4RoutePurposeCapturePrefix,
			Destination: "10.77.60.0/24", Device: "lan0", Metric: 90,
		}}},
		DevicePresent: func(context.Context, string) bool { return true },
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return nil, nil
		},
	}
	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(controller.Router.Spec.Resources) != 0 {
		t.Fatalf("typed mobility route must not become a generic resource: %#v", controller.Router.Spec.Resources)
	}
	if !containsMobilityDataplaneCommand(commands, "ip route replace 10.77.60.0/24 dev lan0 metric 90") {
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
				ID: "cloudedge/capture-prefix", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Purpose: string(dynamicconfig.MobilityIPv4RoutePurposeCapturePrefix),
				Destination: "10.77.60.0/24", Device: "lan0", Metric: 90,
			}},
		},
	}}
	var commands []string
	controller := IPv4RouteController{
		Router: &api.Router{}, Store: store,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			command := name + " " + strings.Join(args, " ")
			commands = append(commands, command)
			if command == "ip route show 10.77.60.0/24" {
				return []byte("10.77.60.0/24 dev lan0 proto bgp metric 90\n"), nil
			}
			return nil, nil
		},
	}
	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, command := range commands {
		if strings.Contains(command, " route del ") {
			t.Fatalf("BGP route must not be deleted: commands = %#v", commands)
		}
	}
	if routes, err := controller.appliedMobilityRoutes(); err != nil || len(routes) != 0 {
		t.Fatalf("withdrawn BGP-protected mobility ledger = %#v, want empty", routes)
	}
}

func TestIPv4RouteControllerWithdrawsTypedMobilityRouteFromItsAppliedLedger(t *testing.T) {
	store := &mobilityEffectStore{statuses: map[string]map[string]any{
		api.RouterAPIVersion + "/Router/" + samDataplaneStatusName: {
			"appliedMobilityRoutes": []mobilityAppliedIPv4Route{{
				ID: "cloudedge/local-10.77.60.20", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Purpose: string(dynamicconfig.MobilityIPv4RoutePurposeLocalInventory),
				Destination: "10.77.60.20/32", Device: "lan0", PreferredSource: "10.77.60.1", Metric: 90,
			}},
		},
	}}
	var commands []string
	controller := IPv4RouteController{
		Router: &api.Router{}, Store: store,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			command := name + " " + strings.Join(args, " ")
			commands = append(commands, command)
			if command == "ip route show 10.77.60.20/32" {
				return []byte("10.77.60.20 dev lan0 proto static src 10.77.60.1 metric 90\\n"), nil
			}
			return nil, nil
		},
	}
	if err := controller.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !containsMobilityDataplaneCommand(commands, "ip route del 10.77.60.20/32 dev lan0 metric 90") {
		t.Fatalf("typed mobility ledger route was not withdrawn: commands = %#v", commands)
	}
	if routes, err := controller.appliedMobilityRoutes(); err != nil || len(routes) != 0 {
		t.Fatalf("withdrawn mobility ledger = %#v, want empty", routes)
	}
}

func TestIPv4StaticAddressControllerAppliesAndWithdrawsTypedMobilityAddress(t *testing.T) {
	store := &mobilityEffectStore{}
	var commands []string
	controller := IPv4StaticAddressController{
		Router: &api.Router{}, Store: store,
		MobilityDataplane: dynamicconfig.MobilityDataplanePlan{StaticAddresses: []dynamicconfig.MobilityIPv4AddressIntent{{
			ID: "cloudedge/capture-source", PoolRef: "cloudedge", PoolPrefix: "10.77.60.0/24", Purpose: dynamicconfig.MobilityIPv4AddressPurposeCaptureSource,
			Interface: "lan0", Address: "10.77.60.1/32",
		}}},
		DevicePresent:  func(context.Context, string) bool { return true },
		AddressPresent: func(context.Context, string, string) bool { return false },
		Command: func(_ context.Context, name string, args ...string) error {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if addresses, err := controller.appliedMobilityStaticAddresses(); err != nil || len(addresses) != 1 {
		t.Fatalf("applied mobility addresses = %#v, err=%v", addresses, err)
	}
	if len(commands) != 1 {
		t.Fatalf("apply commands = %#v", commands)
	}

	controller.MobilityDataplane = dynamicconfig.MobilityDataplanePlan{}
	controller.AddressPresent = func(context.Context, string, string) bool { return true }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if addresses, err := controller.appliedMobilityStaticAddresses(); err != nil || len(addresses) != 0 {
		t.Fatalf("withdrawn mobility addresses = %#v, err=%v", addresses, err)
	}
	if len(commands) != 2 {
		t.Fatalf("apply/withdraw commands = %#v", commands)
	}
}

func TestLinuxIPv4AddressPresentRequiresExactPrefix(t *testing.T) {
	output := []byte("7: ens19    inet 10.77.60.34/24 brd 10.77.60.255 scope global ens19\\n")
	if linuxIPv4AddressPresent(output, "10.77.60.34/32") {
		t.Fatal("a wider external address must not satisfy a managed /32")
	}
	if !linuxIPv4AddressPresent(output, "10.77.60.34/24") {
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
