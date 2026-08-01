// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"log/slog"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/ha"
)

func TestStartDefersClientDaemonSupervisionToControllerBootstrap(t *testing.T) {
	useSupervisedDaemonMarkerTestRoot(t)
	router := vrrpGatedPDTestRouter()
	store := mapStore{
		api.NetAPIVersion + "/VirtualAddress/lan-gw-v4": {"role": "master"},
	}
	runner := &Runner{
		Router: router,
		Bus:    bus.New(),
		Store:  store,
		Opts: Options{
			DaemonSockets:          map[string]string{"wan-pd": t.TempDir() + "/wan-pd.sock"},
			SuperviseClientDaemons: true,
			EnabledControllers:     []string{"daemon-supervisor"},
		},
	}
	runner.generationBuilder = func(context.Context, *slog.Logger, eventedStore, bool, ha.Decision) ([]framework.Controller, DaemonStatusController, error) {
		return nil, DaemonStatusController{}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := runner.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	runner.supervisedMu.Lock()
	defer runner.supervisedMu.Unlock()
	if len(runner.clientDaemonStates) != 0 {
		t.Fatalf("startup used persisted VRRP role before controller bootstrap: %#v", runner.clientDaemonStates)
	}
}

func TestControllerBootstrapObservesVRRPBeforeClientDaemonSupervisor(t *testing.T) {
	router := vrrpGatedPDTestRouter()
	runner := &Runner{
		Router: router,
		Bus:    bus.New(),
		Store:  mapStore{},
		Opts: Options{
			DryRunVRRP:             true,
			SuperviseClientDaemons: true,
			EnabledControllers:     []string{"vrrp", "daemon-supervisor"},
		},
	}
	store := eventedStore{Store: runner.Store, Bus: runner.Bus, Router: router}
	controllers, _, err := runner.frameworkControllers(context.Background(), slog.Default(), store, false, ha.Decision{Leader: true})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, controller := range controllers {
		names = append(names, controller.Name())
	}
	want := []string{"vrrp", "daemon-supervisor"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] {
		t.Fatalf("bootstrap controller order = %v, want %v", names, want)
	}
}

func TestDaemonSupervisionRouterRequiresObservedMasterRole(t *testing.T) {
	router := vrrpGatedPDTestRouter()
	store := mapStore{}
	runner := &Runner{Router: router}
	evented := eventedStore{Store: store, Router: router}

	if got := runner.clientDaemonSpecs(runner.daemonSupervisionRouter(evented)); len(got) != 0 {
		t.Fatalf("unknown VRRP role selected client daemons: %#v", got)
	}
	store[api.NetAPIVersion+"/VirtualAddress/lan-gw-v4"] = map[string]any{"role": "backup"}
	if got := runner.clientDaemonSpecs(runner.daemonSupervisionRouter(evented)); len(got) != 0 {
		t.Fatalf("backup VRRP role selected client daemons: %#v", got)
	}
	store[api.NetAPIVersion+"/VirtualAddress/lan-gw-v4"] = map[string]any{"role": "master"}
	got := runner.clientDaemonSpecs(runner.daemonSupervisionRouter(evented))
	if len(got) != 1 || got[0].Binary != "routerd-dhcpv6-client" || got[0].ResourceName != "wan-pd" {
		t.Fatalf("master VRRP role client daemons = %#v", got)
	}
}

func vrrpGatedPDTestRouter() *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "startup-order"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan-vmac"},
				Spec:     api.InterfaceSpec{IfName: "lo"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
				Metadata: api.ObjectMeta{Name: "lan-gw-v4"},
				Spec: api.VirtualAddressSpec{
					Family:    "ipv4",
					Interface: "lan",
					Address:   "192.0.2.1/24",
					Mode:      "vrrp",
					VRRP:      api.VirtualAddressVRRPSpec{VirtualRouterID: 1, Priority: 100},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
				Metadata: api.ObjectMeta{Name: "wan-pd"},
				Spec: api.DHCPv6PrefixDelegationSpec{
					Interface:  "wan-vmac",
					ClientDUID: "00030001020000000113",
					When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
						"VirtualAddress/lan-gw-v4.role": {Equals: "master"},
					}},
				},
			},
		}},
	}
}
