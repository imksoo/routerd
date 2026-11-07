// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"github.com/miekg/dns"
)

func TestProbeHTTPDistinguishesDeclarationsUnknownCommunicationAndSQLFailure(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.DNSResolverSpec{}}}}}
	server := httptest.NewServer(controlapi.Handler{Probe: serveProbeHandler(func() *api.Router { return router }, store)})
	defer server.Close()
	client := controlapi.NewHTTPClient(server.URL)
	result, err := client.Probe(t.Context(), controlapi.ProbeRequest{Subject: "dns", Target: "lan"})
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range result.Checks {
		if check.Status == "pass" || !strings.Contains(check.Detail, "unobserved") {
			t.Fatalf("declaration became observed success: %+v", check)
		}
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DNSResolver", "lan", map[string]any{"phase": "Failed"}); err != nil {
		t.Fatal(err)
	}
	result, err = client.Probe(t.Context(), controlapi.ProbeRequest{Subject: "dns", Target: "lan"})
	if err != nil || result.Checks[0].Status != "fail" {
		t.Fatalf("failed phase probe=%+v %v", result, err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DNSResolver", "lan", map[string]any{"phase": "Error", "error": "native operation failed"}); err != nil {
		t.Fatal(err)
	}
	result, err = client.Probe(t.Context(), controlapi.ProbeRequest{Subject: "dns", Target: "lan"})
	if err != nil || result.Checks[0].Status != "fail" || result.Checks[0].Detail != "reported phase Error" {
		t.Fatalf("native Error phase was not failed: %+v %v", result, err)
	}
	for _, target := range []string{"invalid", "fe80::1"} {
		result, err = client.Probe(t.Context(), controlapi.ProbeRequest{Subject: "lan-client", Target: target})
		if err != nil {
			t.Fatal(err)
		}
		want := "warn"
		if target == "invalid" {
			want = "fail"
		}
		if len(result.Checks) != 1 || result.Checks[0].Status != want || result.Checks[0].Kind != "scope" || !result.Checks[0].ObservedAt.IsZero() {
			t.Fatalf("LAN target-only probe=%+v", result)
		}
		if target == "fe80::1" && !strings.Contains(result.Checks[0].Detail, "unobserved:") {
			t.Fatalf("missing link-local zone became a communication observation: %+v", result)
		}
	}
	// A real DNS error response is a measured communication failure, unlike
	// a declaration or a client target missing its native interface scope.
	_, port := daemonProbeDNSPeer(t, func(writer dns.ResponseWriter, request *dns.Msg) {
		response := new(dns.Msg)
		response.SetRcode(request, dns.RcodeServerFailure)
		_ = writer.WriteMsg(response)
	})
	router = daemonProbeDNSRouter(port)
	result, err = client.Probe(t.Context(), controlapi.ProbeRequest{Subject: "dns", Target: "lan"})
	if err != nil {
		t.Fatal(err)
	}
	observed := 0
	for _, check := range result.Checks {
		if check.Kind != "communication" {
			continue
		}
		observed++
		if check.Status != "fail" || check.FailureKind != "dns_rcode" || check.ObservedAt.IsZero() {
			t.Fatalf("DNS SERVFAIL was not a measured communication failure: %+v", check)
		}
	}
	if observed != 2 {
		t.Fatalf("missing actual UDP/TCP communication failures: %+v", result)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = client.Probe(t.Context(), controlapi.ProbeRequest{Subject: "dns", Target: "lan"})
	var apiError *controlapi.APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusInternalServerError {
		t.Fatalf("SQL failure became phase observation: %v", err)
	}
}
