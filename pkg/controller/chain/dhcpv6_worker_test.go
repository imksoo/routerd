// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func dhcpv6WorkerRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "wan0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "alias"}, Spec: api.InterfaceSpec{IfName: "wan0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Address"}, Metadata: api.ObjectMeta{Name: "address"}, Spec: api.DHCPv6AddressSpec{Interface: "alias"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan", IAID: "7", ClientDUID: "00030001020000000001"}},
	}}}
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
	sources := runner.daemonEventSourceSpecs(router, nil)
	sockets, err := (DaemonStatusController{Router: router}).daemonSockets()
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
	sources := runner.daemonEventSourceSpecs(router, nil)
	sockets, err := (DaemonStatusController{Router: router}).daemonSockets()
	if err != nil || len(sockets) != 2 || sockets[0] == sockets[1] || len(sources) != 2 {
		t.Fatalf("independent workers lost: sources=%v sockets=%v err=%v", sources, sockets, err)
	}
}

func TestDHCPv6AddressProtocolStatusDoesNotClaimNativeApplied(t *testing.T) {
	if !daemonStatusObservedOnlyKind("DHCPv6Address") || daemonObservedPromotesTopLevel("DHCPv6Address") {
		t.Fatal("a protocol lease cannot establish native address ownership")
	}
}
