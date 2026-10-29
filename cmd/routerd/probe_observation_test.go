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
	for _, target := range []string{"invalid", "192.0.2.4"} {
		result, err = client.Probe(t.Context(), controlapi.ProbeRequest{Subject: "lan-client", Target: target})
		if err != nil {
			t.Fatal(err)
		}
		want := "warn"
		if target == "invalid" {
			want = "fail"
		}
		if len(result.Checks) != 1 || result.Checks[0].Status != want {
			t.Fatalf("LAN target-only probe=%+v", result)
		}
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
