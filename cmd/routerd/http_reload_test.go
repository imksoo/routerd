// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"gopkg.in/yaml.v3"
)

func unusedHTTPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func httpConfigRouter(controlPort, webPort int, tokenEnv, title string, enabled bool) *api.Router {
	return &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "http-runtime"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ControlAPI"}, Metadata: api.ObjectMeta{Name: "control"}, Spec: api.ControlAPISpec{Enabled: &enabled, ListenAddress: "127.0.0.1", Port: controlPort, TokenFrom: api.SecretValueSourceSpec{Env: tokenEnv}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "WebConsole"}, Metadata: api.ObjectMeta{Name: "console"}, Spec: api.WebConsoleSpec{Enabled: &enabled, ListenAddress: "127.0.0.1", Port: webPort, Title: title}},
	}}}
}

func awaitRetiredHTTP(t *testing.T, owner *ownedHTTPServer) {
	t.Helper()
	owner.mu.Lock()
	retired := append([]*httpServerInstance(nil), owner.retired...)
	owner.mu.Unlock()
	for _, instance := range retired {
		select {
		case <-instance.drained:
		case <-time.After(2 * time.Second):
			t.Fatal("admitted HTTP request did not release its owner")
		}
	}
}

func TestHTTPMutationPublishesTokenTitleBindRecoveryAndDisable(t *testing.T) {
	for _, reload := range []bool{false, true} {
		t.Run(fmt.Sprintf("reload-%t", reload), func(t *testing.T) {
			testHTTPMutationPublishesTokenTitleBindRecoveryAndDisable(t, reload)
		})
	}
}

func testHTTPMutationPublishesTokenTitleBindRecoveryAndDisable(t *testing.T, reload bool) {
	t.Setenv("ROUTERD_HTTP_OLD_TOKEN", "old-token")
	t.Setenv("ROUTERD_HTTP_NEW_TOKEN", "new-token")
	controlPort, webPort := unusedHTTPPort(t), unusedHTTPPort(t)
	initial := httpConfigRouter(controlPort, webPort, "ROUTERD_HTTP_OLD_TOKEN", "old console", true)
	dir := t.TempDir()
	path, statePath := filepath.Join(dir, "router.yaml"), filepath.Join(dir, "state.db")
	data, err := yaml.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var current atomic.Pointer[api.Router]
	current.Store(initial)
	cache := &resultCache{}
	mutator := serveConfigMutator{configPath: path, statePath: statePath, cache: cache, getRouter: current.Load, setRouter: current.Store,
		baseOpts: applyOptions{ConfigPath: path, StatePath: statePath, LedgerPath: filepath.Join(dir, "ledger.db"), SkipServiceManager: true, Sandbox: true}}
	handler := controlapi.Handler{
		Status: func(*http.Request) (*controlapi.Status, error) {
			status := controlapi.NewStatus(cache.Load(), current.Load())
			return &status, nil
		},
		Apply: func(r *http.Request, request controlapi.ApplyRequest) (*controlapi.ApplyResult, error) {
			return mutator.apply(r, request)
		},
	}
	servers := &serveHTTPServers{controlHandler: handler,
		controlConfiguration: func(candidate *api.Router) (controlAPIHTTPConfig, error) {
			return resolveControlAPIHTTPConfig(candidate, "", nil, api.SecretValueSourceSpec{}, controlAPITLSConfig{}, nil)
		},
		webConfiguration: func(candidate *api.Router) (api.WebConsoleSpec, bool, error) {
			return webConsoleFromRouter(candidate, store)
		},
		newWebHandler: func(spec api.WebConsoleSpec, candidate *api.Router) http.Handler {
			return webConsoleHTTPHandler(spec, candidate, current.Load, store, nil, cache, path, nil, nil)
		}}
	gate := &sync.RWMutex{}
	mutator.baseOpts.MutationGate = gate
	runner := &controllerchain.Runner{Router: initial, Store: store, Bus: bus.NewWithStore(store), Opts: controllerchain.Options{EnabledControllers: []string{"log-retention"}, MutationGate: gate, ReconcileHTTPServers: func(ctx context.Context, observation controllerchain.RuntimeRouterObservation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return servers.reconcile(observation)
	}}}
	mutator.applyRuntimeWithOutcome = runner.ApplyRuntimeWithOutcome
	mutator.activeRuntime = runner.ActiveRuntimeSnapshot
	runtimeCtx, stopRuntime := context.WithCancel(t.Context())
	if err := runner.Start(runtimeCtx); err != nil {
		stopRuntime()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopRuntime()
		ctx, cancel := context.WithTimeout(context.Background(), controllerchain.RuntimeShutdownGrace)
		defer cancel()
		if err := runner.Wait(ctx); err != nil && !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
		if err := servers.close(ctx); err != nil {
			t.Error(err)
		}
	})
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", controlPort)
	old := controlapi.NewHTTPClient(baseURL).WithBearerToken("old-token")
	if _, err := old.Status(t.Context()); err != nil {
		t.Fatalf("initial token: %v", err)
	}
	apply := func(client *controlapi.Client, router *api.Router) error {
		t.Helper()
		data, err := yaml.Marshal(router)
		if err != nil {
			t.Fatal(err)
		}
		// This checks publication and recovery, not a two-second latency SLA.
		// Allow the durable apply to finish on a contended test filesystem.
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		_, err = client.Apply(ctx, controlapi.ApplyRequest{CandidateYAML: string(data), Replace: true})
		return err
	}
	updated := httpConfigRouter(controlPort, webPort, "ROUTERD_HTTP_NEW_TOKEN", "updated console", true)
	if reload {
		// Exercise the actual generation replacement path together with HTTP
		// publication. The selected test controllers do not start DNS services.
		dns, err := config.LoadBytes([]byte(dnsRuntimeYAML("127.0.0.1")), "candidate")
		if err != nil {
			t.Fatal(err)
		}
		updated.Spec.Resources = append(updated.Spec.Resources, dns.Spec.Resources...)
	}
	if changed, _ := runtimeShapeChanged(initial, updated); changed != reload {
		t.Fatalf("HTTP fixture selected wrong runtime path: reload=%t changed=%t", reload, changed)
	}
	if err := apply(old, updated); err != nil {
		t.Fatalf("same-listener self mutation: %v", err)
	}
	var apiErr *controlapi.APIError
	if _, err := old.Status(t.Context()); !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old token retained authority: %v", err)
	}
	client := controlapi.NewHTTPClient(baseURL).WithBearerToken("new-token")
	if _, err := client.Status(t.Context()); err != nil {
		t.Fatalf("new token did not take effect: %v", err)
	}
	webURL := fmt.Sprintf("http://127.0.0.1:%d/", webPort)
	response, err := http.Get(webURL)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || !strings.Contains(string(body), "updated console") || strings.Contains(string(body), "old console") {
		t.Fatalf("web handler remained stale: %q %v", body, readErr)
	}
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	nextPort := busy.Addr().(*net.TCPAddr).Port
	next := httpConfigRouter(nextPort, webPort, "ROUTERD_HTTP_NEW_TOKEN", "after recovery", true)
	if err := apply(client, next); err == nil {
		t.Fatal("busy candidate listener reported success")
	}
	canonical, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	console, _, err := webConsoleFromRouter(canonical, store)
	if err != nil || console.Title != "after recovery" {
		t.Fatalf("failed activation lost committed desired HTTP configuration: %+v %v", console, err)
	}
	// The selected controller graph now owns the durable candidate. Its HTTP
	// effect failed, so observe the published handler rather than treating that
	// selected graph as proof that every resource has converged.
	response, err = http.Get(webURL)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || !strings.Contains(string(body), "updated console") || strings.Contains(string(body), "after recovery") {
		t.Fatalf("failed bind replaced the prior actual web handler: %q %v", body, readErr)
	}
	if _, err := client.Status(t.Context()); err != nil {
		t.Fatalf("failed bind stopped prior listener: %v", err)
	}
	if err := busy.Close(); err != nil {
		t.Fatal(err)
	}
	if err := apply(client, next); err != nil {
		t.Fatalf("failed listener did not recover: %v", err)
	}
	awaitRetiredHTTP(t, &servers.control)
	client = controlapi.NewHTTPClient(fmt.Sprintf("http://127.0.0.1:%d", nextPort)).WithBearerToken("new-token")
	if _, err := client.Status(t.Context()); err != nil {
		t.Fatalf("replacement listener inactive: %v", err)
	}
	disabled := httpConfigRouter(nextPort, webPort, "ROUTERD_HTTP_NEW_TOKEN", "disabled", false)
	if err := apply(client, disabled); err != nil {
		t.Fatalf("disable waited for its own mutation request: %v", err)
	}
	awaitRetiredHTTP(t, &servers.control)
	awaitRetiredHTTP(t, &servers.web)
	for _, port := range []int{controlPort, nextPort, webPort} {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			t.Fatalf("retired listener %d still accepts", port)
		}
	}
	rows, err := store.ListGenerations(10)
	failed, committed := 0, 0
	for _, row := range rows {
		if row.Phase == "Errored" {
			failed++
		}
		if row.Phase == "Committed" {
			committed++
		}
	}
	if err != nil || len(rows) != 4 || failed != 1 || committed != 3 {
		t.Fatalf("failed postcommit activation and its durable retry lost generation evidence: %+v %v", rows, err)
	}
}

func TestHTTPCloseWaitsForAdmittedOperationAfterStoppingAdmissions(t *testing.T) {
	owner := &ownedHTTPServer{}
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
	defer func() {
		releaseHandler()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := owner.close(ctx); err != nil {
			t.Error(err)
		}
	}()
	prepared, err := owner.prepare("127.0.0.1:0", &servedHTTPConfig{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release // models an operation that has not acknowledged cancellation
		_, _ = io.WriteString(w, "finished")
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.close()
	if err := prepared.publish(); err != nil {
		t.Fatal(err)
	}
	address := owner.current.listener.Addr().String()
	done := make(chan error, 1)
	go func() {
		response, err := (&http.Client{Timeout: 2 * time.Second}).Get("http://" + address + "/")
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request was not admitted")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	err = owner.close(ctx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unacknowledged handler was reported stopped: %v", err)
	}
	releaseHandler()
	ctx, cancel = context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := owner.close(ctx); err != nil {
		t.Fatalf("finished handler did not release its owner: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("admitted request was prematurely terminated: %v", err)
	}
}

func TestHTTPReconcileReopensUnexpectedlyClosedListener(t *testing.T) {
	var owner ownedHTTPServer
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	defer owner.close(ctx)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "recovered") })
	configuration := &servedHTTPConfig{Handler: handler}
	prepared, err := owner.prepare("127.0.0.1:0", configuration)
	if err != nil {
		t.Fatal(err)
	}
	address := prepared.listener.Addr().String()
	// Preserve the actual fixed address so a repair must rebind the same port.
	prepared.listen = address
	if err := prepared.publish(); err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	instance := owner.current
	owner.mu.Unlock()
	if err := instance.listener.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-instance.done:
	case <-ctx.Done():
		t.Fatal("HTTP Serve did not exit")
	}
	for {
		prepared, err = owner.prepare(address, configuration)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(err)
		case <-time.After(time.Millisecond):
		}
	}
	defer prepared.close()
	if err := prepared.publish(); err != nil {
		t.Fatal(err)
	}
	client := http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + address + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || string(data) != "recovered" {
		t.Fatalf("reopened endpoint: %q %v", data, err)
	}
}
