//go:build linux

// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestHTTPFrameworkOwnerRebindsObservedAddressAndWithdrawsExpiredDynamicConsole(t *testing.T) {
	setMissingSAMForwardChainIPTables(t)
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "observed"}, Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "local"}, Spec: api.InterfaceSpec{IfName: "lo", Managed: false, Owner: "external"}}}}}
	port := unusedHTTPPort(t)
	now := time.Now().UTC()
	part := dynamicconfig.NewPart("console", "console", nil, 1, now, now.Add(time.Hour))
	part.Spec.Resources = []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "WebConsole"}, Metadata: api.ObjectMeta{Name: "dynamic"}, Spec: api.WebConsoleSpec{Port: port, Title: "observed dynamic console", ListenAddressFrom: api.StatusValueSourceSpec{Resource: "Interface/local", Field: "primaryIPv4"}}}}
	record, err := codec.Encode(part)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertDynamicConfigPart(record); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "local", map[string]any{"primaryIPv4": "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	servers := &serveHTTPServers{
		controlConfiguration: func(candidate *api.Router) (controlAPIHTTPConfig, error) {
			return resolveControlAPIHTTPConfig(candidate, "", nil, api.SecretValueSourceSpec{}, controlAPITLSConfig{}, nil)
		},
		webConfiguration: func(candidate *api.Router) (api.WebConsoleSpec, bool, error) {
			return webConsoleFromRouter(candidate, store)
		},
		newWebHandler: func(spec api.WebConsoleSpec, candidate *api.Router) http.Handler {
			return webConsoleHTTPHandler(spec, candidate, func() *api.Router { return router }, store, nil, &resultCache{}, "", nil, nil)
		}}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), controllerchain.RuntimeShutdownGrace)
		defer cancel()
		if err := servers.close(ctx); err != nil {
			t.Error(err)
		}
	}()
	runner := &controllerchain.Runner{Router: router, Store: store, Bus: bus.New(), Opts: controllerchain.Options{EnabledControllers: []string{"log-retention"}, ReconcileHTTPServers: func(_ context.Context, observation controllerchain.RuntimeRouterObservation) error {
		return servers.reconcile(observation)
	}}}
	request := func(address string) {
		t.Helper()
		client := &http.Client{Timeout: time.Second}
		response, err := client.Get(fmt.Sprintf("http://%s:%d/", address, port))
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || !strings.Contains(string(body), "observed dynamic console") {
			t.Fatalf("dynamic handler unavailable: %q %v", body, err)
		}
	}
	if err := runner.ReconcileOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	request("127.0.0.1")
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "local", map[string]any{"primaryIPv4": "127.0.0.2"}); err != nil {
		t.Fatal(err)
	}
	if err := runner.ReconcileOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	awaitRetiredHTTP(t, &servers.web)
	request("127.0.0.2")
	record.ExpiresAt = time.Now().Add(-time.Second)
	if err := store.UpsertDynamicConfigPart(record); err != nil {
		t.Fatal(err)
	}
	if err := runner.ReconcileOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	awaitRetiredHTTP(t, &servers.web)
	for _, address := range []string{"127.0.0.1", "127.0.0.2"} {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", address, port), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			t.Fatalf("expired dynamic console still accepts on %s", address)
		}
	}
}
