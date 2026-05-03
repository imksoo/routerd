package healthcheck

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
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

func TestControllerProbeUsesResolvedSourceInterface(t *testing.T) {
	store := mapStore{}
	seen := make(chan api.HealthCheckSpec, 1)
	controller := &Controller{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19"},
			},
		}}},
		Bus:   bus.New(),
		Store: store,
		Now:   fixedNow(),
		Probe: func(ctx context.Context, spec api.HealthCheckSpec) ProbeResult {
			seen <- spec
			return ProbeResult{OK: true}
		},
	}
	resource := healthResource("internet")
	resource.Spec = api.HealthCheckSpec{Target: "1.1.1.1", SourceInterface: "lan"}
	spec, err := resource.HealthCheckSpec()
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.ProbeOnce(context.Background(), resource, spec); err != nil {
		t.Fatal(err)
	}
	got := <-seen
	if got.SourceInterface != "ens19" {
		t.Fatalf("probe sourceInterface = %q", got.SourceInterface)
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

func TestThresholdSuppressesFlap(t *testing.T) {
	store := mapStore{}
	b := bus.New()
	controller := &Controller{
		Bus:   b,
		Store: store,
		Now:   fixedNow(),
	}
	resource := healthResource("internet")
	spec := api.HealthCheckSpec{HealthyThreshold: 2, UnhealthyThreshold: 2}
	if err := controller.applyResult(context.Background(), resource, spec, ProbeResult{OK: true}); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "HealthCheck", "internet")
	if status["phase"] != PhasePassing {
		t.Fatalf("phase after one pass = %#v", status)
	}
	if err := controller.applyResult(context.Background(), resource, spec, ProbeResult{OK: true}); err != nil {
		t.Fatal(err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "HealthCheck", "internet")
	if status["phase"] != PhaseHealthy {
		t.Fatalf("phase after two passes = %#v", status)
	}
	if err := controller.applyResult(context.Background(), resource, spec, ProbeResult{}); err != nil {
		t.Fatal(err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "HealthCheck", "internet")
	if status["phase"] != PhaseFailing {
		t.Fatalf("phase after one failure = %#v", status)
	}
	if err := controller.applyResult(context.Background(), resource, spec, ProbeResult{}); err != nil {
		t.Fatal(err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "HealthCheck", "internet")
	if status["phase"] != PhaseUnhealthy {
		t.Fatalf("phase after two failures = %#v", status)
	}
	if got := b.Recent("routerd.healthcheck.internet.passed"); len(got) != 2 {
		t.Fatalf("passed events = %d", len(got))
	}
	if got := b.Recent("routerd.healthcheck.internet.failed"); len(got) != 2 {
		t.Fatalf("failed events = %d", len(got))
	}
}

func TestStateMachineDefaultThresholds(t *testing.T) {
	store := mapStore{}
	controller := &Controller{Bus: bus.New(), Store: store, Now: fixedNow()}
	resource := healthResource("internet")
	spec := api.HealthCheckSpec{}
	if err := controller.applyResult(context.Background(), resource, spec, ProbeResult{OK: true}); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "HealthCheck", "internet")
	if status["phase"] != PhaseHealthy || status["consecutivePassed"] != 1 {
		t.Fatalf("healthy status = %#v", status)
	}
	for i := 0; i < 2; i++ {
		if err := controller.applyResult(context.Background(), resource, spec, ProbeResult{}); err != nil {
			t.Fatal(err)
		}
	}
	status = store.ObjectStatus(api.NetAPIVersion, "HealthCheck", "internet")
	if status["phase"] != PhaseFailing {
		t.Fatalf("phase before default unhealthy threshold = %#v", status)
	}
	if err := controller.applyResult(context.Background(), resource, spec, ProbeResult{}); err != nil {
		t.Fatal(err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "HealthCheck", "internet")
	if status["phase"] != PhaseUnhealthy || status["consecutiveFailed"] != 3 {
		t.Fatalf("unhealthy status = %#v", status)
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
