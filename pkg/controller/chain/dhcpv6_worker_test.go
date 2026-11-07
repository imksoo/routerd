// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"slices"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

func dhcpv6WorkerRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "wan0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "alias"}, Spec: api.InterfaceSpec{IfName: "wan0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Address"}, Metadata: api.ObjectMeta{Name: "address"}, Spec: api.DHCPv6AddressSpec{Interface: "alias"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan", IAID: "7", ClientDUID: "00030001020000000001"}},
	}}}
}

func TestDHCPv6ObservedWorkerFollowsResolvedPDCondition(t *testing.T) {
	router := dhcpv6WorkerRouter()
	pd, _ := router.Spec.Resources[3].DHCPv6PrefixDelegationSpec()
	pd.When = api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/gw.role": {Equals: "master"}}}
	router.Spec.Resources[3].Spec = pd
	for _, role := range []string{"master", "backup"} {
		store := mapStore{api.NetAPIVersion + "/VirtualAddress/gw": {"role": role}}
		effective, err := resourcequery.FilterRouterByResolvedWhen(router, store)
		if err != nil {
			t.Fatal(err)
		}
		runner := &Runner{Router: router, Store: store}
		specs := runner.clientDaemonSpecs(effective)
		if len(specs) != 1 {
			t.Fatalf("role %s workers=%v", role, specs)
		}
		want := defaultClientSocket(specs[0].Binary, specs[0].ResourceName)
		sources, sourceErr := runner.daemonEventSourceSpecs(router, nil)
		if sourceErr != nil {
			t.Fatal(sourceErr)
		}
		sockets, err := (DaemonStatusController{Router: router, Store: store}).daemonSockets()
		if err != nil || !slices.Contains(sockets, want) || len(sources) != 1 {
			t.Fatalf("role %s observes a different worker: want=%s sockets=%v sources=%v err=%v", role, want, sockets, sources, err)
		}
		if !slices.Contains(sockets, defaultClientSocket("routerd-dhcpv6-client", "pd")) {
			t.Fatal("declared PD snapshot recovery must remain reachable while its worker is disabled")
		}
		for _, source := range sources {
			if source.Socket != want {
				t.Fatalf("role %s events use inactive worker %s", role, source.Socket)
			}
		}
	}
}

func TestDHCPv6DeclarationsShareOneWorkerAndEventSource(t *testing.T) {
	router := dhcpv6WorkerRouter()
	runner := &Runner{Router: router}
	specs := runner.clientDaemonSpecs(router)
	if len(specs) != 1 {
		t.Fatalf("same kernel interface must have one UDP/546 owner: %+v", specs)
	}
	args := strings.Join(specs[0].Args, " ")
	for _, want := range []string{"--resource pd", "--interface wan0", "--request-mode pd-address", "--address-resource address", "--iaid 7", "--client-duid 00030001020000000001"} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q in %s", want, args)
		}
	}
	sources, sourceErr := runner.daemonEventSourceSpecs(router, nil)
	if sourceErr != nil {
		t.Fatal(sourceErr)
	}
	sockets, err := (DaemonStatusController{Router: router, Store: mapStore{}}).daemonSockets()
	if err != nil || len(sockets) != 1 || len(sources) != 1 {
		t.Fatalf("one worker needs one source and socket: %v %v %v", sources, sockets, err)
	}
	for _, source := range sources {
		if source.Socket != sockets[0] {
			t.Fatalf("source/status sockets disagree: %+v %v", source, sockets)
		}
	}
}

func TestDHCPv6AddressWorkerIdentityDoesNotCollideWithPDName(t *testing.T) {
	router := dhcpv6WorkerRouter()
	router.Spec.Resources[1].Spec = api.InterfaceSpec{IfName: "wan1"}
	router.Spec.Resources[2].Metadata.Name = "pd"
	runner := &Runner{Router: router}
	specs := runner.clientDaemonSpecs(router)
	if len(specs) != 2 {
		t.Fatalf("independent interfaces need two workers: %+v", specs)
	}
	if supervisedDaemonKey(specs[0].Binary, specs[0].ResourceName) == supervisedDaemonKey(specs[1].Binary, specs[1].ResourceName) {
		t.Fatal("different resource kinds alias the same supervisor identity")
	}
	for _, spec := range specs {
		if strings.Contains(strings.Join(spec.Args, " "), "--request-mode address") {
			if !strings.Contains(strings.Join(spec.Args, " "), "--address-resource pd") {
				t.Fatal("worker identity must not replace the address resource name")
			}
			if defaultClientSocket(spec.Binary, spec.ResourceName) == defaultClientSocket("routerd-dhcpv6-client", "pd") {
				t.Fatal("address and PD share a socket")
			}
		}
	}
	sources, sourceErr := runner.daemonEventSourceSpecs(router, nil)
	if sourceErr != nil {
		t.Fatal(sourceErr)
	}
	sockets, err := (DaemonStatusController{Router: router, Store: mapStore{}}).daemonSockets()
	if err != nil || len(sockets) != 2 || sockets[0] == sockets[1] || len(sources) != 2 {
		t.Fatalf("independent workers lost: sources=%v sockets=%v err=%v", sources, sockets, err)
	}
}

func TestDHCPv6AddressProtocolStatusDoesNotClaimNativeApplied(t *testing.T) {
	if !daemonStatusObservedOnlyKind("DHCPv6Address") || daemonObservedPromotesTopLevel("DHCPv6Address") {
		t.Fatal("a protocol lease cannot establish native address ownership")
	}
}
