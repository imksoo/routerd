// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestInspectionReturnsStoreFailureThroughActualHTTPHandler(t *testing.T) {
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "wan0"},
	}}}}
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "wan", map[string]any{"phase": "Ready"}); err != nil {
		t.Fatal(err)
	}
	current := func() *api.Router { return router }
	server := httptest.NewServer(controlapi.Handler{Get: serveGetHandler(current, store, nil, nil, nil), Describe: serveDescribeHandler(current, store)})
	defer server.Close()
	client := controlapi.NewHTTPClient(server.URL)
	if result, err := client.Describe(t.Context(), controlapi.DescribeRequest{Target: "Interface/wan", EventsLimit: 10}); err != nil || result.Resource.Status["phase"] != "Ready" {
		t.Fatalf("initial inspection: result=%+v err=%v", result, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = client.Get(t.Context(), controlapi.GetRequest{Subject: "Interface/wan"})
	var apiErr *controlapi.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("closed store became successful empty status: %v", err)
	}
}
