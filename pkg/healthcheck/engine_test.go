// SPDX-License-Identifier: BSD-3-Clause

package healthcheck

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

type mapStore map[string]map[string]any

func (s mapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if status := s[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

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
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "em0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "em0"}, Spec: api.InterfaceSpec{IfName: "em1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite"}, Spec: api.DSLiteTunnelSpec{TunnelName: "ds-routerd"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg"}, Spec: api.WireGuardInterfaceSpec{IfName: "wg-native"}},
	}}}
	for _, tc := range []struct {
		spec api.HealthCheckSpec
		want string
	}{
		{api.HealthCheckSpec{Interface: "wan"}, "em0"},
		{api.HealthCheckSpec{SourceInterface: "wan"}, "em0"},
		{api.HealthCheckSpec{Interface: "ds-lite"}, "ds-routerd"},
		{api.HealthCheckSpec{Interface: "wg"}, "wg-native"},
		{api.HealthCheckSpec{SourceInterface: "eth-test"}, "eth-test"},
		{api.HealthCheckSpec{}, ""},
	} {
		got, err := ResolveSpecForResource(router, "check", tc.spec)
		if err != nil || got.SourceInterface != tc.want || got.FwMark != 0 {
			t.Fatalf("source binding %+v => %+v, %v; want %q", tc.spec, got, err, tc.want)
		}
	}
}

func TestResolveSpecDerivesFwMarkFromRoutingHealthCheckReferences(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "balanced"}, Spec: api.EgressRoutePolicySpec{Candidates: []api.EgressRoutePolicyCandidate{{Targets: []api.EgressRoutePolicyTarget{{Interface: "ds-lite-a", Mark: 0x110, HealthCheck: "internet-a"}}}}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "default"}, Spec: api.EgressRoutePolicySpec{Candidates: []api.EgressRoutePolicyCandidate{{Interface: "wan", Mark: 0x116, HealthCheck: "internet-hgw"}}}},
	}}}
	for _, tc := range []struct {
		name, source string
		mark         int
	}{
		{"internet-a", "ds-lite-a", 0x110},
		{"internet-hgw", "wan", 0x116},
	} {
		got, err := ResolveSpecForResource(router, tc.name, api.HealthCheckSpec{})
		if err != nil || got.FwMark != tc.mark || got.SourceInterface != tc.source {
			t.Fatalf("%s binding = %+v, %v", tc.name, got, err)
		}
	}
	if _, err := ResolveSpecForResource(router, "internet-a", api.HealthCheckSpec{FwMark: 0x999}); err == nil {
		t.Fatal("configured mark silently overrode actual referenced path")
	}
}

func TestResolveSpecRejectsConflictingReferencedProbePaths(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		mark         int
	}{
		{"different interfaces", "wan-b", 0x110},
		{"different marks", "wan-a", 0x111},
		{"marked and unmarked paths", "wan-a", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "paths"}, Spec: api.EgressRoutePolicySpec{Candidates: []api.EgressRoutePolicyCandidate{
					{Interface: "wan-a", Mark: 0x110, HealthCheck: "check"},
					{Interface: tc.source, Mark: tc.mark, HealthCheck: "check"},
				}}},
			}}}
			if _, err := ResolveSpecWithStoreForResource(router, nil, "check", api.HealthCheckSpec{}); err == nil {
				t.Fatal("incompatible paths produced an unbound management probe")
			}
		})
	}
}

func TestProbeDNSSuccessAndFailure(t *testing.T) {
	packet, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer packet.Close()
	go serveOneDNSAnswer(packet)
	host, port := splitHostPort(t, packet.LocalAddr().String())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if result := ProbeDNS(ctx, api.HealthCheckSpec{Target: host, Port: port}); !result.OK {
		t.Fatalf("dns success result = %#v", result)
	}
	closed, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, port = splitHostPort(t, closed.LocalAddr().String())
	_ = closed.Close()
	ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if result := ProbeDNS(ctx, api.HealthCheckSpec{Target: host, Port: port}); result.OK {
		t.Fatalf("dns failure result = %#v", result)
	}
}

func healthResource(name string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
		Metadata: api.ObjectMeta{Name: name},
	}
}

func fixedNow() func() time.Time {
	now := time.Date(2026, 5, 2, 13, 0, 0, 0, time.UTC)
	return func() time.Time {
		now = now.Add(time.Second)
		return now
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

func serveOneDNSAnswer(packet net.PacketConn) {
	buf := make([]byte, 512)
	n, addr, err := packet.ReadFrom(buf)
	if err != nil || n < 12 {
		return
	}
	query := buf[:n]
	questionEnd := 12
	for questionEnd < len(query) && query[questionEnd] != 0 {
		questionEnd += int(query[questionEnd]) + 1
	}
	questionEnd += 5
	if questionEnd > len(query) {
		return
	}
	response := make([]byte, 0, n+16)
	response = append(response, query[0], query[1], 0x81, 0x80, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00)
	response = append(response, query[12:questionEnd]...)
	response = append(response, 0xc0, 0x0c, 0x00, 0x01, 0x00, 0x01)
	response = append(response, 0x00, 0x00, 0x00, 0x3c, 0x00, 0x04, 127, 0, 0, 1)
	_, _ = packet.WriteTo(response, addr)
}
