// SPDX-License-Identifier: BSD-3-Clause

package healthcheck

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestProbeTCPSuccessAndFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	host, port := splitHostPort(t, listener.Addr().String())
	if result := ProbeTCP(context.Background(), api.HealthCheckSpec{Target: host, Port: port}); !result.OK {
		t.Fatalf("tcp success result = %#v", result)
	}
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, port = splitHostPort(t, closed.Addr().String())
	_ = closed.Close()
	if result := ProbeTCP(context.Background(), api.HealthCheckSpec{Target: host, Port: port}); result.OK {
		t.Fatalf("tcp failure result = %#v", result)
	}
}

func TestProbeTCPSourceAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()
	host, port := splitHostPort(t, listener.Addr().String())
	result := ProbeTCP(context.Background(), api.HealthCheckSpec{Target: host, Port: port, SourceAddress: "127.0.0.1"})
	if !result.OK {
		t.Fatalf("tcp sourceAddress result = %#v", result)
	}
}

func TestProbeTCPInvalidSourceAddress(t *testing.T) {
	result := ProbeTCP(context.Background(), api.HealthCheckSpec{Target: "127.0.0.1", Port: 9, SourceAddress: "not-an-ip"})
	if result.OK || !strings.Contains(result.Message, "sourceAddress") {
		t.Fatalf("invalid sourceAddress result = %#v", result)
	}
}

func TestResolveSpecSourceInterfaceResourceNames(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "ens19"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
			Metadata: api.ObjectMeta{Name: "ds-lite"},
			Spec:     api.DSLiteTunnelSpec{TunnelName: "ds-routerd"},
		},
	}}}
	if got := ResolveSpec(router, api.HealthCheckSpec{SourceInterface: "lan"}).SourceInterface; got != "ens19" {
		t.Fatalf("Interface sourceInterface resolved to %q", got)
	}
	if got := ResolveSpec(router, api.HealthCheckSpec{SourceInterface: "ds-lite"}).SourceInterface; got != "ds-routerd" {
		t.Fatalf("DSLite sourceInterface resolved to %q", got)
	}
	if got := ResolveSpec(router, api.HealthCheckSpec{SourceInterface: "eth-test"}).SourceInterface; got != "eth-test" {
		t.Fatalf("raw sourceInterface resolved to %q", got)
	}
}

func TestResolveSpecDerivesFwMarkFromRoutingHealthCheckReferences(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
			Metadata: api.ObjectMeta{Name: "balanced"},
			Spec: api.EgressRoutePolicySpec{Mode: "hash", Candidates: []api.EgressRoutePolicyCandidate{{
				Name: "balanced",
				Targets: []api.EgressRoutePolicyTarget{
					{Name: "a", Interface: "ds-lite-a", Mark: 0x110, HealthCheck: "internet-a"},
				},
			}}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
			Metadata: api.ObjectMeta{Name: "default"},
			Spec: api.EgressRoutePolicySpec{Mode: "priority", Candidates: []api.EgressRoutePolicyCandidate{
				{Name: "hgw", Interface: "wan", Mark: 0x116, HealthCheck: "internet-hgw"},
			}},
		},
	}}}
	if got := ResolveSpecForResource(router, "internet-a", api.HealthCheckSpec{}).FwMark; got != 0x110 {
		t.Fatalf("route set target fwmark = 0x%x, want 0x110", got)
	}
	if got := ResolveSpecForResource(router, "internet-hgw", api.HealthCheckSpec{}).FwMark; got != 0x116 {
		t.Fatalf("default route candidate fwmark = 0x%x, want 0x116", got)
	}
	if got := ResolveSpecForResource(router, "internet-a", api.HealthCheckSpec{}).SourceInterface; got != "ds-lite-a" {
		t.Fatalf("route set target sourceInterface = %q, want ds-lite-a", got)
	}
	if got := ResolveSpecForResource(router, "internet-hgw", api.HealthCheckSpec{}).SourceInterface; got != "wan" {
		t.Fatalf("default route candidate sourceInterface = %q, want wan", got)
	}
	if got := ResolveSpecForResource(router, "internet-a", api.HealthCheckSpec{FwMark: 0x999}).FwMark; got != 0x999 {
		t.Fatalf("explicit fwmark = 0x%x, want 0x999", got)
	}
}

func healthResource(name string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
		Metadata: api.ObjectMeta{Name: name},
	}
}

func splitHostPort(t *testing.T, value string) (string, int) {
	t.Helper()
	host, portValue, err := net.SplitHostPort(value)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(portValue, "%d", &port); err != nil {
		t.Fatal(err)
	}
	return host, port
}
