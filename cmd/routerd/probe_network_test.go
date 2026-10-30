// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/state"
	"github.com/miekg/dns"
)

func daemonProbeDNSPeer(t *testing.T, handler dns.HandlerFunc) (string, int) {
	t.Helper()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	udp, err := net.ListenPacket("udp", tcp.Addr().String())
	if err != nil {
		tcp.Close()
		t.Fatal(err)
	}
	servers := []*dns.Server{{Listener: tcp, Net: "tcp", Handler: handler}, {PacketConn: udp, Net: "udp", Handler: handler}}
	for _, server := range servers {
		ready := make(chan struct{})
		server.NotifyStartedFunc = func() { close(ready) }
		go func() { _ = server.ActivateAndServe() }()
		select {
		case <-ready:
		case <-time.After(time.Second):
			t.Fatal("actual DNS fixture did not start")
		}
		t.Cleanup(func() { _ = server.Shutdown() })
	}
	_, port, _ := net.SplitHostPort(tcp.Addr().String())
	number, _ := strconv.Atoi(port)
	return tcp.Addr().String(), number
}

func daemonProbeDNSRouter(port int) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.DNSResolverSpec{Listen: []api.DNSResolverListenSpec{{Name: "lan", Addresses: []string{"127.0.0.1"}, Port: port}}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSForwarder"}, Metadata: api.ObjectMeta{Name: "local"}, Spec: api.DNSForwarderSpec{Resolver: "lan", Match: []string{"test.example"}, ZoneRefs: []string{"DNSZone/local"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSZone"}, Metadata: api.ObjectMeta{Name: "local"}, Spec: api.DNSZoneSpec{Zone: "test.example", Records: []api.DNSZoneRecordSpec{{Hostname: "router", IPv4: "192.0.2.55"}}}},
	}}}
}

func TestDaemonDNSProbeActualUDPAndTCPExpectedAnswers(t *testing.T) {
	_, port := daemonProbeDNSPeer(t, func(writer dns.ResponseWriter, request *dns.Msg) {
		response := new(dns.Msg)
		response.SetReply(request)
		response.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: request.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.ParseIP("192.0.2.55")}}
		_ = writer.WriteMsg(response)
	})
	server := httptest.NewServer(controlapi.Handler{Probe: serveProbeHandler(func() *api.Router { return daemonProbeDNSRouter(port) }, nil)})
	defer server.Close()
	result, err := controlapi.NewHTTPClient(server.URL).Probe(t.Context(), controlapi.ProbeRequest{Subject: "dns", Target: "lan"})
	var checks []controlapi.ProbeCheck
	if result != nil {
		checks = result.Checks
	}
	if err != nil {
		t.Fatal(err)
	}
	observed := 0
	for _, check := range checks {
		if check.Kind != "communication" {
			continue
		}
		observed++
		if check.Status != "pass" || check.ObservedAt.IsZero() || !strings.Contains(check.Detail, "router.test.example.") {
			t.Fatalf("wire answer lost its actual question: %+v", check)
		}
	}
	if observed != 2 {
		t.Fatalf("both actual transports not probed: %+v", checks)
	}
}

func TestDaemonDNSProbeTransportLivenessIsNotSuccessfulResolution(t *testing.T) {
	_, port := daemonProbeDNSPeer(t, func(writer dns.ResponseWriter, request *dns.Msg) {
		response := new(dns.Msg)
		response.SetRcode(request, dns.RcodeServerFailure)
		_ = writer.WriteMsg(response)
	})
	checks, err := serveProbeChecks(t.Context(), daemonProbeDNSRouter(port), nil, "dns", "lan")
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range checks {
		if check.Kind == "communication" && (check.Status != "fail" || check.FailureKind != "dns_rcode" || !strings.Contains(check.Detail, "transport replied")) {
			t.Fatalf("SERVFAIL became successful resolution: %+v", check)
		}
	}
}

func TestDaemonDNSProbeCancelsActualBlockedExchange(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	endpoint, _ := daemonProbeDNSPeer(t, func(writer dns.ResponseWriter, request *dns.Msg) { once.Do(func() { close(started) }) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request := new(dns.Msg)
	request.SetQuestion("blocked.test.", dns.TypeA)
	done := make(chan error, 1)
	go func() { _, err := exchangeDaemonDNSProbe(ctx, "udp", endpoint, request); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("actual query never reached peer")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("cancelled request retained DNS socket until ordinary timeout")
	}
}

func TestDaemonEgressProbeUsesActualConfiguredTCPAndBoundsAttempts(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			connection.Close()
		}
	}()
	_, rawPort, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(rawPort)
	policy := api.EgressRoutePolicySpec{Family: "ipv4"}
	for i := 0; i < 10; i++ {
		policy.Candidates = append(policy.Candidates, api.EgressRoutePolicyCandidate{Name: fmt.Sprint(i), HealthCheck: "actual"})
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: policy},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "actual"}, Spec: api.HealthCheckSpec{Protocol: "tcp", Target: "127.0.0.1", TargetSource: "static", Port: port}},
	}}}
	checks, err := serveProbeChecks(t.Context(), router, nil, "egress", "wan")
	if err != nil {
		t.Fatal(err)
	}
	attempted, unprobed := 0, 0
	for _, check := range checks {
		if check.Kind == "communication" {
			attempted++
			if check.Status != "pass" || check.Protocol != "tcp" || check.ObservedAt.IsZero() {
				t.Fatalf("actual TCP result missing: %+v", check)
			}
		}
		if strings.Contains(check.Detail, "remaining configured targets are unprobed") {
			unprobed++
		}
	}
	if attempted != 8 || unprobed != 1 {
		t.Fatalf("attempt cap silently qualified unprobed candidates: attempted=%d unprobed=%d", attempted, unprobed)
	}
}

func TestDaemonProbeCurrentSQLFailureAndReportedPhaseCannotReplaceWire(t *testing.T) {
	store, err := state.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	router := daemonProbeDNSRouter(65534)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DNSResolver", "lan", map[string]any{"phase": "Applied"}); err != nil {
		t.Fatal(err)
	}
	checks, err := serveProbeChecks(t.Context(), router, store, "dns", "lan")
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range checks {
		if check.Kind == "communication" && check.Status == "pass" {
			t.Fatalf("phase Applied became current DNS traffic success: %+v", check)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(controlapi.Handler{Probe: serveProbeHandler(func() *api.Router { return router }, store)})
	defer server.Close()
	_, err = controlapi.NewHTTPClient(server.URL).Probe(t.Context(), controlapi.ProbeRequest{Subject: "dns", Target: "lan"})
	var failure *controlapi.APIError
	if !errors.As(err, &failure) || failure.StatusCode != http.StatusInternalServerError {
		t.Fatalf("SQL failure became missing or successful observation: %v", err)
	}
}

func TestDaemonDNSProbeDoesNotSendAnUnselectedPrivateQuestion(t *testing.T) {
	observed := make(chan string, 1)
	_, port := daemonProbeDNSPeer(t, func(writer dns.ResponseWriter, request *dns.Msg) {
		observed <- request.Question[0].Name
		response := new(dns.Msg)
		response.SetRcode(request, dns.RcodeNameError)
		_ = writer.WriteMsg(response)
	})
	router := daemonProbeDNSRouter(port)
	forwarder, _ := router.Spec.Resources[1].DNSForwarderSpec()
	forwarder.Match = []string{"other-private.example"}
	router.Spec.Resources[1].Spec = forwarder
	checks, err := daemonDNSProbe(t.Context(), router, nil, "lan")
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range checks {
		if check.Kind == "communication" {
			t.Fatalf("unmatched private question was selected: %+v", check)
		}
	}
	select {
	case question := <-observed:
		t.Fatalf("probe leaked private question outside configured forwarding match: %s", question)
	default:
	}
}

func TestDaemonProbeAdmissionRejectsFifthActualBlockedRequest(t *testing.T) {
	entered := make(chan struct{}, 4)
	_, port := daemonProbeDNSPeer(t, func(writer dns.ResponseWriter, request *dns.Msg) { entered <- struct{}{} })
	router := daemonProbeDNSRouter(port)
	server := httptest.NewServer(controlapi.Handler{Probe: serveProbeHandler(func() *api.Router { return router }, nil)})
	defer server.Close()
	client := controlapi.NewHTTPClient(server.URL)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{}, 4)
	for i := 0; i < 4; i++ {
		go func() {
			_, _ = client.Probe(ctx, controlapi.ProbeRequest{Subject: "dns", Target: "lan"})
			done <- struct{}{}
		}()
	}
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("four actual requests did not enter their owned DNS exchanges")
		}
	}
	started := time.Now()
	result, err := client.Probe(t.Context(), controlapi.ProbeRequest{Subject: "dns", Target: "lan"})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 300*time.Millisecond || len(result.Checks) != 1 || result.Checks[0].Status != "warn" || !strings.Contains(result.Checks[0].Detail, "active probe requests") {
		t.Fatalf("admission did not reject saturated live probes: %+v", result)
	}
	cancel()
	for i := 0; i < 4; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("cancelled probe request retained admission")
		}
	}
}

func TestDaemonProbePlanningStopsWithoutAllocatingOneCheckPerUnknownTarget(t *testing.T) {
	policy := api.EgressRoutePolicySpec{Family: "ipv4"}
	for i := 0; i < 10000; i++ {
		policy.Candidates = append(policy.Candidates, api.EgressRoutePolicyCandidate{Name: strconv.Itoa(i), HealthCheck: "unavailable"})
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: policy}}}}
	checks, err := daemonEgressProbe(t.Context(), router, nil, "wan")
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) > 26 {
		t.Fatalf("bounded network probes still allocated unbounded planning output: %d", len(checks))
	}
	if !strings.Contains(checks[len(checks)-2].Detail, "remaining configured targets are unprobed") {
		t.Fatalf("unvisited targets silently disappeared: %+v", checks)
	}
}

func TestDaemonProbeUsesCurrentConditionalGraphBeforeChoosingPrivateQuestions(t *testing.T) {
	observed := make(chan string, 2)
	_, port := daemonProbeDNSPeer(t, func(writer dns.ResponseWriter, request *dns.Msg) {
		observed <- request.Question[0].Name
		response := new(dns.Msg)
		response.SetReply(request)
		response.Answer = []dns.RR{&dns.NS{Hdr: dns.RR_Header{Name: ".", Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 30}, Ns: "root.example."}}
		_ = writer.WriteMsg(response)
	})
	router := daemonProbeDNSRouter(port)
	private, _ := router.Spec.Resources[1].DNSForwarderSpec()
	private.When = api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master"}}}
	router.Spec.Resources[1].Spec = private
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSUpstream"}, Metadata: api.ObjectMeta{Name: "public"}, Spec: api.DNSUpstreamSpec{Protocol: "udp", Address: "127.0.0.1", Port: port}}, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSForwarder"}, Metadata: api.ObjectMeta{Name: "public"}, Spec: api.DNSForwarderSpec{Resolver: "lan", Match: []string{"."}, Upstreams: []string{"DNSUpstream/public"}}})
	store, err := state.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Set("ha.role", "backup", "observed role"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(controlapi.Handler{Probe: serveProbeHandler(func() *api.Router { return router }, store)})
	defer server.Close()
	result, err := controlapi.NewHTTPClient(server.URL).Probe(t.Context(), controlapi.ProbeRequest{Subject: "dns", Target: "lan"})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, check := range result.Checks {
		if check.Kind == "communication" {
			count++
			if check.Status != "pass" {
				t.Fatalf("current eligible root question failed: %+v", check)
			}
		}
	}
	if count != 2 {
		t.Fatalf("expected actual UDP and TCP root checks: %+v", result)
	}
	for i := 0; i < 2; i++ {
		select {
		case question := <-observed:
			if question != "." {
				t.Fatalf("inactive private source leaked a question: %s", question)
			}
		default:
			t.Fatal("actual current graph query missing")
		}
	}
}
