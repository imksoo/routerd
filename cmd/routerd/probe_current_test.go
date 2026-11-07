// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/state"
	"github.com/miekg/dns"
)

func TestDaemonDNSProbeRechecksSQLSourceAfterUDPBeforeTCP(t *testing.T) {
	store, err := state.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Set("ha.role", "master", "current role"); err != nil {
		t.Fatal(err)
	}
	observed := make(chan string, 4)
	faults := make(chan error, 4)
	_, port := daemonProbeDNSPeer(t, func(w dns.ResponseWriter, q *dns.Msg) {
		observed <- w.RemoteAddr().Network() + " " + q.Question[0].Name
		// This actual SQL change occurs while the UDP exchange is outstanding.
		// The resolver's next source selection would forward this private name.
		_, err := store.Set("ha.role", "backup", "source revoked during exchange")
		if err != nil {
			faults <- err
		}
		a := new(dns.Msg)
		a.SetReply(q)
		a.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: q.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 1}, A: net.ParseIP("192.0.2.55")}}
		_ = w.WriteMsg(a)
	})
	router := daemonProbeDNSRouter(port)
	private, _ := router.Spec.Resources[1].DNSForwarderSpec()
	private.When = api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master"}}}
	router.Spec.Resources[1].Spec = private
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSForwarder"}, Metadata: api.ObjectMeta{Name: "public"}, Spec: api.DNSForwarderSpec{Resolver: "lan", Match: []string{"."}, Upstreams: []string{"DNSUpstream/public"}}})
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSUpstream"}, Metadata: api.ObjectMeta{Name: "public"}, Spec: api.DNSUpstreamSpec{Address: "192.0.2.53", Protocol: "udp"}})
	server := httptest.NewServer(controlapi.Handler{Probe: serveProbeHandler(func() *api.Router { return router }, store)})
	defer server.Close()
	result, err := controlapi.NewHTTPClient(server.URL).Probe(t.Context(), controlapi.ProbeRequest{Subject: "dns", Target: "lan"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-faults:
		t.Fatal(err)
	default:
	}
	if len(observed) != 1 {
		t.Fatalf("source revocation still sent another private question: %d exchanges", len(observed))
	}
	if question := <-observed; !strings.HasPrefix(question, "udp ") {
		t.Fatalf("first exchange was not UDP: %s", question)
	}
	stale := false
	for _, check := range result.Checks {
		if check.Kind == "communication" && check.Status == "pass" {
			t.Fatalf("revoked source still published PASS: %+v", check)
		}
		stale = stale || check.FailureKind == "stale_source"
	}
	if !stale {
		t.Fatalf("source revocation was not explicit: %+v", result)
	}
}

func TestDaemonEgressProbeRechecksCandidateSQLAfterTCPResponse(t *testing.T) {
	store, err := state.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Set("ha.role", "master", "current role"); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	faults := make(chan error, 1)
	// HTTP supplies a deterministic wait on the actual TCP connection: the
	// role changes before the reply can make this configured check succeed.
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if _, err := store.Set("ha.role", "backup", "candidate revoked during TCP response"); err != nil {
			faults <- err
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer peer.Close()
	host, rawPort, err := net.SplitHostPort(strings.TrimPrefix(peer.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(rawPort)
	when := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master"}}}
	policy := api.EgressRoutePolicySpec{Family: "ipv4", Candidates: []api.EgressRoutePolicyCandidate{{Name: "first", HealthCheck: "actual", When: when}, {Name: "second", HealthCheck: "actual", When: when}}}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: policy},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "actual"}, Spec: api.HealthCheckSpec{Protocol: "http", Target: host, TargetSource: "static", Port: port}},
	}}}
	server := httptest.NewServer(controlapi.Handler{Probe: serveProbeHandler(func() *api.Router { return router }, store)})
	defer server.Close()
	result, err := controlapi.NewHTTPClient(server.URL).Probe(t.Context(), controlapi.ProbeRequest{Subject: "egress", Target: "wan"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-faults:
		t.Fatal(err)
	default:
	}
	if count := requests.Load(); count != 1 {
		t.Fatalf("revoked candidate still sent another TCP request: %d", count)
	}
	stale := false
	for _, check := range result.Checks {
		if check.Kind == "communication" && check.Status == "pass" {
			t.Fatalf("revoked candidate published PASS: %+v", check)
		}
		stale = stale || check.FailureKind == "stale_source"
	}
	if !stale {
		t.Fatalf("candidate revocation was not explicit: %+v", result)
	}
}
