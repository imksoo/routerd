// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/eventlog"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestApplyChainControllerOptionsSkipLegacyClientUnits(t *testing.T) {
	opts := applyChainControllerOptions(applyOptions{})
	if !opts.SkipLegacyClientUnits {
		t.Fatal("one-shot apply must not synthesize legacy DHCP client service units")
	}
	if opts.SuperviseClientDaemons {
		t.Fatal("one-shot apply must leave client daemon supervision to long-running routerd")
	}
}

func TestRouterWithDryRunLeaseFilesKeepsDHCPLeaseStateInArtifacts(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.DHCPv4ServerSpec{LeaseFile: "/var/lib/routerd/dnsmasq/dnsmasq.leases"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "lan-v6"}, Spec: api.DHCPv6ServerSpec{LeaseFile: "/var/lib/routerd/dnsmasq/dnsmasq.leases"}},
	}}}

	got, err := routerWithDryRunLeaseFiles(router, dir)
	if err != nil {
		t.Fatalf("sandbox DHCP lease paths: %v", err)
	}
	want := filepath.Join(dir, "dnsmasq.leases")
	for _, resource := range got.Spec.Resources {
		switch resource.Kind {
		case "DHCPv4Server":
			spec, err := resource.DHCPv4ServerSpec()
			if err != nil {
				t.Fatal(err)
			}
			if spec.LeaseFile != want {
				t.Fatalf("DHCPv4 dry-run lease file = %q, want %q", spec.LeaseFile, want)
			}
		case "DHCPv6Server":
			spec, err := resource.DHCPv6ServerSpec()
			if err != nil {
				t.Fatal(err)
			}
			if spec.LeaseFile != want {
				t.Fatalf("DHCPv6 dry-run lease file = %q, want %q", spec.LeaseFile, want)
			}
		}
	}

	original, err := router.Spec.Resources[0].DHCPv4ServerSpec()
	if err != nil {
		t.Fatal(err)
	}
	if original.LeaseFile != "/var/lib/routerd/dnsmasq/dnsmasq.leases" {
		t.Fatalf("dry-run mutated input lease file = %q", original.LeaseFile)
	}
}

func TestRunApplyChainOnceDryRunDoesNotCreateStateDB(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	statePath := filepath.Join(stateDir, "routerd.db")
	ledgerDir := filepath.Join(dir, "ledger")
	ledgerPath := filepath.Join(ledgerDir, "routerd.db")
	statusPath := filepath.Join(dir, "status.json")
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{
			Name: "test-router",
		},
	}
	result, err := runApplyChainOnce(context.Background(), router, applyOptions{
		DryRun:     true,
		StatePath:  statePath,
		LedgerPath: ledgerPath,
		StatusFile: statusPath,
		ConfigPath: filepath.Join(dir, "router.yaml"),
	}, io.Discard, &eventlog.Logger{})
	if err != nil {
		t.Fatalf("dry-run apply: %v", err)
	}
	if result.Generation != 0 {
		t.Fatalf("dry-run generation = %d, want 0", result.Generation)
	}
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run state db stat error = %v, want not exist", err)
	}
	if _, err := os.Stat(stateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run state dir stat error = %v, want not exist", err)
	}
	if _, err := os.Stat(ledgerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run ledger db stat error = %v, want not exist", err)
	}
	if _, err := os.Stat(ledgerDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run ledger dir stat error = %v, want not exist", err)
	}
}

func findApplyResult(result *apply.Result, id string) *apply.ResourceResult {
	for i := range result.Resources {
		if result.Resources[i].ID == id {
			return &result.Resources[i]
		}
	}
	return nil
}

func TestManagementPlaneBlocksNonDryRunApply(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test-router"},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "ManagementAccess"},
			Metadata: api.ObjectMeta{Name: "main"},
			Spec:     api.ManagementAccessSpec{Interfaces: []string{"mgmt0"}},
		}}},
	}

	var stderr strings.Builder
	_, err := checkManagementPlaneBeforeApply(router, applyOptions{MgmtLockoutWriter: &stderr})
	if err == nil || !strings.Contains(err.Error(), "management plane lockout risk") {
		t.Fatalf("checkManagementPlaneBeforeApply error = %v, want lockout risk", err)
	}
	if !strings.Contains(stderr.String(), "management-plane FAIL Interface/mgmt0") {
		t.Fatalf("stderr missing management finding:\n%s", stderr.String())
	}

	stderr.Reset()
	warnings, err := checkManagementPlaneBeforeApply(router, applyOptions{AllowMgmtLockout: true, MgmtLockoutWriter: &stderr})
	if err != nil {
		t.Fatalf("allow management lockout: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatalf("warnings = %#v, want management-plane warning", warnings)
	}
	if !strings.Contains(stderr.String(), "management-plane WARN Interface/mgmt0") {
		t.Fatalf("stderr missing allowed warning:\n%s", stderr.String())
	}
}

func TestRunApplyChainOnceDryRunDoesNotMutateExistingStateDB(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "routerd.db")
	statusPath := filepath.Join(dir, "status.json")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	store.Set("manual.mode", "keep", "seed")
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{
			Name: "test-router",
		},
	}
	result, err := runApplyChainOnce(context.Background(), router, applyOptions{
		DryRun:     true,
		StatePath:  statePath,
		StatusFile: statusPath,
		ConfigPath: filepath.Join(dir, "router.yaml"),
	}, io.Discard, &eventlog.Logger{})
	if err != nil {
		t.Fatalf("dry-run apply: %v", err)
	}
	if result.Generation != 0 {
		t.Fatalf("dry-run generation = %d, want 0", result.Generation)
	}

	store, err = routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	defer func() { _ = store.Close() }()
	if got := store.Get("manual.mode"); got.Status != routerstate.StatusSet || got.Value != "keep" {
		t.Fatalf("seed state = %+v, want keep", got)
	}
	if got := store.Get("wan.mode"); got.Status != routerstate.StatusUnknown {
		t.Fatalf("dry-run wrote wan.mode = %+v, want unknown", got)
	}
	if got := store.LatestGeneration(); got != 0 {
		t.Fatalf("latest generation after dry-run = %d, want 0", got)
	}
}

func TestRouterConfigYAMLRejectsChangedSource(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "router.yaml")
	if err := os.WriteFile(configPath, []byte(testRouterYAML("original-router")), 0644); err != nil {
		t.Fatalf("write original config: %v", err)
	}
	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load original config: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(testRouterYAML("changed-router")), 0644); err != nil {
		t.Fatalf("replace config: %v", err)
	}
	if _, err := routerConfigYAML(router, applyOptions{ConfigPath: configPath}); err == nil || !strings.Contains(err.Error(), "changed after it was loaded") {
		t.Fatalf("changed config error = %v, want source-change error", err)
	}
}

func TestConfigGenerationHashIncludesEffectiveRouter(t *testing.T) {
	source := testRouterYAML("generation-router")
	first := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "generation-router"}}
	second := *first
	second.Spec.Apply.Mode = "warn"
	firstHash, err := configGenerationHash(source, first)
	if err != nil {
		t.Fatalf("hash first effective router: %v", err)
	}
	secondHash, err := configGenerationHash(source, &second)
	if err != nil {
		t.Fatalf("hash changed effective router: %v", err)
	}
	if firstHash == secondHash {
		t.Fatalf("effective-router change did not change generation hash: %s", firstHash)
	}
}

func TestScheduledServeReconcileDoesNotCreateConfigGeneration(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	defer func() { _ = store.Close() }()
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "scheduled-reconcile-router"}}
	runner := &controllerchain.Runner{Router: router, Bus: bus.New(), Store: store, Opts: controllerchain.Options{EnabledControllers: []string{"log-retention"}}}
	initialOpts := applyOptions{ConfigYAMLOverride: testRouterYAML("scheduled-reconcile-router")}
	if _, err := runServeChainOnce(context.Background(), runner, router, initialOpts, store, io.Discard, nil); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	before := store.LatestGeneration()
	if before == 0 {
		t.Fatal("initial reconcile did not create a generation")
	}
	unchanged, err := runServeChainOnce(context.Background(), runner, router, initialOpts, store, io.Discard, nil)
	if err != nil {
		t.Fatalf("unchanged reconcile: %v", err)
	}
	if unchanged.Generation != before {
		t.Fatalf("unchanged reconcile result generation = %d, want %d", unchanged.Generation, before)
	}
	if got := store.LatestGeneration(); got != before {
		t.Fatalf("generation after unchanged reconcile = %d, want %d", got, before)
	}
	if _, err := runServeChainScheduledOnce(context.Background(), runner, router, applyOptions{ConfigPath: filepath.Join(t.TempDir(), "changed-after-boot.yaml")}, store, io.Discard, nil); err != nil {
		t.Fatalf("scheduled reconcile: %v", err)
	}
	if got := store.LatestGeneration(); got != before {
		t.Fatalf("generation after scheduled reconcile = %d, want %d", got, before)
	}
}

func TestConfigCommitPhaseIncludesCommittedConfig(t *testing.T) {
	for _, phase := range []string{"Healthy", "Applied", "Committed"} {
		if !configCommitPhase(phase) {
			t.Fatalf("configCommitPhase(%q) = false, want true", phase)
		}
	}
	if configCommitPhase("Errored") {
		t.Fatal("configCommitPhase(Errored) = true, want false")
	}
}

func TestControlAPIHTTPConfigDefaultsToDisabled(t *testing.T) {
	cfg, err := resolveControlAPIHTTPConfig(testControlAPIRouter("control-defaults"), "", nil, api.SecretValueSourceSpec{}, controlAPITLSConfig{}, map[string]bool{})
	if err != nil {
		t.Fatalf("resolveControlAPIHTTPConfig: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("control HTTP API should be disabled by default when no ControlAPI resource is present")
	}
}

func TestControlAPIHTTPConfigEnabledByResource(t *testing.T) {
	router := testControlAPIRouter("control-resource")
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ControlAPI"},
		Metadata: api.ObjectMeta{Name: "default"},
		Spec:     api.ControlAPISpec{},
	})
	cfg, err := resolveControlAPIHTTPConfig(router, "", nil, api.SecretValueSourceSpec{}, controlAPITLSConfig{}, map[string]bool{})
	if err != nil {
		t.Fatalf("resolveControlAPIHTTPConfig: %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("control HTTP API should be enabled when ControlAPI resource is present")
	}
	if cfg.Listen != "127.0.0.1:65432" {
		t.Fatalf("Listen = %q, want 127.0.0.1:65432", cfg.Listen)
	}
	if !controlAPISourceAllowed(netip.MustParseAddr("127.0.0.1"), cfg.AllowPrefixes) {
		t.Fatal("default policy should allow IPv4 loopback")
	}
	if !controlAPISourceAllowed(netip.MustParseAddr("::1"), cfg.AllowPrefixes) {
		t.Fatal("default policy should allow IPv6 loopback")
	}
	if controlAPISourceAllowed(netip.MustParseAddr("10.30.0.25"), cfg.AllowPrefixes) {
		t.Fatal("default policy should reject non-loopback source")
	}
}

func TestControlAPIHTTPConfigAcceptsNarrowCIDR(t *testing.T) {
	router := testControlAPIRouter("control-narrow")
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ControlAPI"},
		Metadata: api.ObjectMeta{Name: "default"},
		Spec: api.ControlAPISpec{
			ListenAddress: "10.30.0.10",
			Port:          65432,
			AllowCIDRs:    []string{"10.30.0.0/24"},
		},
	})
	cfg, err := resolveControlAPIHTTPConfig(router, "", nil, api.SecretValueSourceSpec{}, controlAPITLSConfig{}, map[string]bool{})
	if err != nil {
		t.Fatalf("resolveControlAPIHTTPConfig: %v", err)
	}
	if cfg.Listen != "10.30.0.10:65432" {
		t.Fatalf("Listen = %q, want 10.30.0.10:65432", cfg.Listen)
	}
	if !controlAPISourceAllowed(netip.MustParseAddr("10.30.0.25"), cfg.AllowPrefixes) {
		t.Fatal("configured narrow CIDR should allow matching source")
	}
	if controlAPISourceAllowed(netip.MustParseAddr("10.31.0.25"), cfg.AllowPrefixes) {
		t.Fatal("configured narrow CIDR should reject nonmatching source")
	}
}

func TestControlAPIHTTPConfigRejectsWideOpenCIDRs(t *testing.T) {
	for _, cidr := range []string{"0.0.0.0/0", "::/0"} {
		if _, err := parseControlAPIAllowCIDRs([]string{cidr}); err == nil {
			t.Fatalf("parseControlAPIAllowCIDRs(%q) succeeded, want error", cidr)
		}
	}
}

func TestControlAPIHTTPConfigReadsTokenFromResourceEnv(t *testing.T) {
	t.Setenv("ROUTERD_TEST_CONTROL_TOKEN", " test-token \n")
	router := testControlAPIRouter("control-token")
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ControlAPI"},
		Metadata: api.ObjectMeta{Name: "default"},
		Spec: api.ControlAPISpec{
			TokenFrom: api.SecretValueSourceSpec{Env: "ROUTERD_TEST_CONTROL_TOKEN"},
		},
	})
	cfg, err := resolveControlAPIHTTPConfig(router, "", nil, api.SecretValueSourceSpec{}, controlAPITLSConfig{}, map[string]bool{})
	if err != nil {
		t.Fatalf("resolveControlAPIHTTPConfig: %v", err)
	}
	if cfg.Token != "test-token" {
		t.Fatalf("Token = %q, want trimmed token", cfg.Token)
	}
}

func TestControlAPIHTTPConfigCLITokenFileOverridesResource(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "control-token")
	if err := os.WriteFile(tokenFile, []byte("cli-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ROUTERD_TEST_CONTROL_TOKEN", "resource-token")
	router := testControlAPIRouter("control-token")
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ControlAPI"},
		Metadata: api.ObjectMeta{Name: "default"},
		Spec: api.ControlAPISpec{
			TokenFrom: api.SecretValueSourceSpec{Env: "ROUTERD_TEST_CONTROL_TOKEN"},
		},
	})
	cfg, err := resolveControlAPIHTTPConfig(router, "", nil, api.SecretValueSourceSpec{File: tokenFile}, controlAPITLSConfig{}, map[string]bool{"http-token-file": true})
	if err != nil {
		t.Fatalf("resolveControlAPIHTTPConfig: %v", err)
	}
	if cfg.Token != "cli-token" {
		t.Fatalf("Token = %q, want CLI token", cfg.Token)
	}
}

func TestControlAPIHTTPConfigAcceptsTLSResource(t *testing.T) {
	router := testControlAPIRouter("control-tls")
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ControlAPI"},
		Metadata: api.ObjectMeta{Name: "default"},
		Spec: api.ControlAPISpec{
			TLS: api.ControlAPITLSSpec{
				CertFile:     "/etc/routerd/control.crt",
				KeyFile:      "/etc/routerd/control.key",
				ClientCAFile: "/etc/routerd/control-client-ca.pem",
			},
		},
	})
	cfg, err := resolveControlAPIHTTPConfig(router, "", nil, api.SecretValueSourceSpec{}, controlAPITLSConfig{}, map[string]bool{})
	if err != nil {
		t.Fatalf("resolveControlAPIHTTPConfig: %v", err)
	}
	if cfg.TLS.CertFile != "/etc/routerd/control.crt" || cfg.TLS.KeyFile != "/etc/routerd/control.key" || cfg.TLS.ClientCAFile != "/etc/routerd/control-client-ca.pem" {
		t.Fatalf("TLS = %#v", cfg.TLS)
	}
}

func TestControlAPIHTTPConfigRejectsIncompleteTLS(t *testing.T) {
	_, err := resolveControlAPIHTTPConfig(testControlAPIRouter("control-tls"), "", nil, api.SecretValueSourceSpec{}, controlAPITLSConfig{CertFile: "/etc/routerd/control.crt"}, map[string]bool{"http-tls-cert-file": true})
	if err == nil || !strings.Contains(err.Error(), "cert file and key file") {
		t.Fatalf("resolveControlAPIHTTPConfig err = %v, want incomplete TLS error", err)
	}
}

func testControlAPIRouter(name string) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     api.RouterSpec{Resources: []api.Resource{}},
	}
}

func TestRollbackListShowsStoredGenerations(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	gen1 := seedGeneration(t, store, "hash-1", testRouterYAML("rollback-one"), true, "Applied")
	gen2 := seedGeneration(t, store, "hash-2", testRouterYAML("rollback-two"), true, "Applied")
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	var stdout strings.Builder
	if err := rollbackCommand([]string{"--list", "--state-file", statePath}, &stdout, io.Discard); err != nil {
		t.Fatalf("rollback --list: %v", err)
	}

	rows := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(rows) != 3 || !reflect.DeepEqual(strings.Fields(rows[0]), []string{"generation", "started_at", "finished_at", "phase", "config", "current"}) {
		t.Fatalf("typed generation table: %q", stdout.String())
	}
	type row struct {
		generation             int64
		phase, config, current string
	}
	var got []row
	for _, text := range rows[1:] {
		fields := strings.Fields(text)
		if len(fields) < 5 || len(fields) > 6 {
			t.Fatalf("invalid generation row %q", text)
		}
		generation, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			t.Fatalf("generation identity: %v", err)
		}
		for _, stamp := range fields[1:3] {
			if _, err := time.Parse(time.RFC3339, stamp); err != nil {
				t.Fatalf("generation timestamp %q: %v", stamp, err)
			}
		}
		current := ""
		if len(fields) == 6 {
			current = fields[5]
		}
		got = append(got, row{generation, fields[3], fields[4], current})
	}
	want := []row{{gen2, "Applied", "yes", "(current)"}, {gen1, "Applied", "yes", ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newest-first generation rows = %#v, want %#v", got, want)
	}
}

func TestRollbackToGenerationErrors(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	genWithoutConfig := seedGeneration(t, store, "hash-1", "", false, "Applied")
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	if err := rollbackCommand([]string{"--to", "99", "--state-file", statePath}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "generation 99 not found") {
		t.Fatalf("missing generation error = %v", err)
	}
	if err := rollbackCommand([]string{"--to", fmt.Sprintf("%d", genWithoutConfig), "--state-file", statePath}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "has no saved config") {
		t.Fatalf("missing config error = %v", err)
	}
}

func seedGeneration(t *testing.T, store *routerstate.SQLiteStore, hash, configYAML string, recordConfig bool, phase string) int64 {
	t.Helper()
	generation, err := store.BeginGeneration(hash)
	if err != nil {
		t.Fatalf("begin generation: %v", err)
	}
	if recordConfig {
		if err := store.RecordGenerationConfig(generation, configYAML); err != nil {
			t.Fatalf("record generation config: %v", err)
		}
	}
	if err := store.FinishGeneration(generation, phase, nil); err != nil {
		t.Fatalf("finish generation: %v", err)
	}
	return generation
}

func testRouterYAML(name string) string {
	return fmt.Sprintf(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: %s
spec:
  resources: []
`, name)
}

func TestControllerDefaultStatusesAreLive(t *testing.T) {
	got := controllerDefaultStatuses()
	if len(got) == 0 {
		t.Fatal("controller statuses are empty")
	}
	for _, status := range got {
		if status.Mode != "live" || status.Reason != controlapi.ControllerModeReasonLive {
			t.Fatalf("status = %+v, want live", status)
		}
		if len(status.ResourceKinds) == 0 {
			t.Fatalf("status = %+v missing resource kinds", status)
		}
	}
}

func TestFilterControllerDefaultStatuses(t *testing.T) {
	all := controllerDefaultStatuses()
	filtered := filterControllerDefaultStatuses(all, parseControllerNames("bgp"))
	if len(filtered) != 1 {
		t.Fatalf("filtered len = %d, want 1: %+v", len(filtered), filtered)
	}
	if filtered[0].Name != "bgp" {
		t.Fatalf("filtered[0].Name = %q, want bgp", filtered[0].Name)
	}
	if got := filterControllerDefaultStatuses(all, parseControllerNames("all")); len(got) != len(all) {
		t.Fatalf("all filtered len = %d, want %d", len(got), len(all))
	}
}

func TestOverallStatusPhaseUsesResourceStatuses(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Reservation", "client", map[string]any{"phase": "Pending", "reason": "WhenFalse"}); err != nil {
		t.Fatalf("save when false status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DNSForwarder", "default", map[string]any{"matches": 1}); err != nil {
		t.Fatalf("save status without phase: %v", err)
	}
	if got := overallStatusPhase("Healthy", store); got != "Healthy" {
		t.Fatalf("phase = %q, want Healthy for WhenFalse pending and phase-less status", got)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "lan", map[string]any{"phase": "Pending"}); err != nil {
		t.Fatalf("save pending status: %v", err)
	}
	if got := overallStatusPhase("Healthy", store); got != "Pending" {
		t.Fatalf("phase = %q, want Pending", got)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "lan", map[string]any{"phase": "Reconverging", "reason": "GoBGPReconverging"}); err != nil {
		t.Fatalf("save reconverging status: %v", err)
	}
	if got := overallStatusPhase("Healthy", store); got != "Pending" {
		t.Fatalf("phase = %q, want Pending for reconverging", got)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPPeer", "worker", map[string]any{"phase": "Error"}); err != nil {
		t.Fatalf("save error status: %v", err)
	}
	if got := overallStatusPhase("Healthy", store); got != "Error" {
		t.Fatalf("phase = %q, want Error", got)
	}
}

func TestResourcePhaseIssuesReportsNonHealthyStatuses(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4ServerLeaseSync", "lan", map[string]any{"phase": "Synced"}); err != nil {
		t.Fatalf("save synced status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DNSUpstream", "default", map[string]any{"address": "192.0.2.53"}); err != nil {
		t.Fatalf("save status without phase: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Reservation", "client", map[string]any{"phase": "Pending", "reason": "WhenFalse"}); err != nil {
		t.Fatalf("save pending status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6PrefixDelegation", "wan-pd", map[string]any{"phase": "Pending", "reason": "NoPrefix"}); err != nil {
		t.Fatalf("save real pending status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "NAT44SessionSync", "sessions", map[string]any{"phase": "Error", "reason": "SyncFailed", "message": "ssh failed"}); err != nil {
		t.Fatalf("save error status: %v", err)
	}
	issues := resourcePhaseIssues(store)
	if len(issues) != 2 {
		t.Fatalf("issues = %+v, want 2 entries", issues)
	}
	if issues[0].Kind != "NAT44SessionSync" || issues[0].Name != "sessions" || issues[0].Phase != "Error" || issues[0].Reason != "SyncFailed" {
		t.Fatalf("first issue = %+v", issues[0])
	}
	if issues[1].Kind != "DHCPv6PrefixDelegation" || issues[1].Name != "wan-pd" || issues[1].Phase != "Pending" || issues[1].Reason != "NoPrefix" {
		t.Fatalf("second issue = %+v", issues[1])
	}
}

func TestListenUnixSocketSetsMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd-status.sock")
	listener, err := listenUnixSocket(path, 0o666)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	defer listener.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o666); got != want {
		t.Fatalf("socket mode = %v, want %v", got, want)
	}
}

func TestGroupOwnMutationSocketKeepsPrivilegedMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd-control.sock")
	listener, err := listenUnixSocket(path, 0o666)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	defer listener.Close()

	groupOwnMutationSocket(path)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o660); got != want {
		t.Fatalf("mutation socket mode = %v, want %v", got, want)
	}
}

func TestControllerResourceKindsUseCanonicalNames(t *testing.T) {
	kinds := controllerResourceKinds("dhcpv6")
	for _, kind := range kinds {
		if kind == "IPv6DHCPv6Server" {
			t.Fatalf("controllerResourceKinds returned legacy kind %q in %v", kind, kinds)
		}
	}
	if !reflect.DeepEqual(kinds, []string{"DHCPv6Server", "IPv6RouterAdvertisement"}) {
		t.Fatalf("dhcpv6 resource kinds = %v", kinds)
	}
}

func TestCleanupUnsupportedLegacyObjectStatusesRemovesLegacyAndDeletedResourceRows(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open sqlite state: %v", err)
	}
	defer store.Close()
	if err := store.SaveObjectStatus(api.NetAPIVersion, "PPPoEInterface", "wan", map[string]any{"phase": "Applied"}); err != nil {
		t.Fatalf("save stale legacy status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "wan", map[string]any{"phase": "Applied"}); err != nil {
		t.Fatalf("save supported status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "TailscaleNode", "old", map[string]any{"phase": "Error", "reason": "ApplyFailed"}); err != nil {
		t.Fatalf("save deleted resource status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "ConntrackObserver", "default", map[string]any{"phase": "Error"}); err != nil {
		t.Fatalf("save synthetic controller status: %v", err)
	}
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test-router"},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "ens18"},
		}}},
	}

	result, err := cleanupUnsupportedLegacyObjectStatuses(router, store, statePath, time.Date(2026, 5, 21, 7, 45, 0, 0, time.UTC), nil)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if result.Skipped || len(result.Removed) != 2 {
		t.Fatalf("cleanup result = %+v, want two removals", result)
	}
	if got := staleObjectStatusIDs(result.Removed); got != "net.routerd.net/v1alpha1/PPPoEInterface/wan,net.routerd.net/v1alpha1/TailscaleNode/old" {
		t.Fatalf("removed = %s", got)
	}
	if _, err := os.Stat(result.SnapshotPath); err != nil {
		t.Fatalf("snapshot stat: %v", err)
	}
	if !strings.HasSuffix(result.SnapshotPath, ".json") {
		t.Fatalf("snapshot path = %q, want JSON snapshot", result.SnapshotPath)
	}
	var snapshot staleStateCleanupSnapshot
	data, err := os.ReadFile(result.SnapshotPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if got := staleObjectStatusIDs(snapshot.Resources); got != "net.routerd.net/v1alpha1/PPPoEInterface/wan,net.routerd.net/v1alpha1/TailscaleNode/old" {
		t.Fatalf("snapshot resources = %s", got)
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "PPPoEInterface", "wan"); len(status) != 0 {
		t.Fatalf("legacy status after cleanup = %+v, want none", status)
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "Interface", "wan"); status["phase"] != "Applied" {
		t.Fatalf("supported status after cleanup = %+v, want preserved", status)
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "TailscaleNode", "old"); len(status) != 0 {
		t.Fatalf("deleted resource status after cleanup = %+v, want none", status)
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "ConntrackObserver", "default"); status["phase"] != "Error" {
		t.Fatalf("synthetic controller status after cleanup = %+v, want preserved", status)
	}
	events := store.Events(api.RouterAPIVersion, "Router", "test-router", 10)
	if len(events) == 0 || events[0].Reason != "StaleStateCleanup" {
		t.Fatalf("events = %+v, want StaleStateCleanup", events)
	}
}

func dynamicConfigPartRecordForCleanupTest(t *testing.T, source string, resources []api.Resource, expiresAt time.Time) routerstate.DynamicConfigPartRecord {
	t.Helper()
	raw, err := json.Marshal(resources)
	if err != nil {
		t.Fatalf("marshal resources: %v", err)
	}
	return routerstate.DynamicConfigPartRecord{
		Source:        source,
		Generation:    1,
		ObservedAt:    time.Now().UTC(),
		ExpiresAt:     expiresAt,
		Digest:        source + "-digest",
		ResourcesJSON: string(raw),
		Status:        "active",
	}
}

func TestWriteFileIfChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.conf")

	changed, err := writeFileIfChanged(path, []byte("one\n"), 0644)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	if !changed {
		t.Fatal("first write changed = false, want true")
	}

	changed, err = writeFileIfChanged(path, []byte("one\n"), 0644)
	if err != nil {
		t.Fatalf("same write: %v", err)
	}
	if changed {
		t.Fatal("same write changed = true, want false")
	}

	changed, err = writeFileIfChanged(path, []byte("two\n"), 0644)
	if err != nil {
		t.Fatalf("different write: %v", err)
	}
	if !changed {
		t.Fatal("different write changed = false, want true")
	}
}

func TestParseFreeBSDRCConf(t *testing.T) {
	got, err := parseFreeBSDRCConf([]byte(`# Generated
gateway_enable="YES"
ifconfig_vtnet2="DHCP"
ifconfig_vtnet0_ipv6="inet6 accept_rtadv"
`))
	if err != nil {
		t.Fatalf("parse rc.conf: %v", err)
	}
	for key, want := range map[string]string{
		"gateway_enable":       "YES",
		"ifconfig_vtnet2":      "DHCP",
		"ifconfig_vtnet0_ipv6": "inet6 accept_rtadv",
	} {
		if got[key] != want {
			t.Fatalf("%s = %q, want %q", key, got[key], want)
		}
	}
	if ifname := freeBSDIfconfigKeyInterface("ifconfig_vtnet0_ipv6"); ifname != "vtnet0" {
		t.Fatalf("ifconfig key interface = %q, want vtnet0", ifname)
	}
	ifnames := freeBSDDHCPClientIfnames([]byte("interface \"vtnet2\" {\n  ignore routers;\n};\n"))
	if len(ifnames) != 1 || ifnames[0] != "vtnet2" {
		t.Fatalf("dhclient ifnames = %v, want [vtnet2]", ifnames)
	}
}

func TestParseFreeBSDSysrcValue(t *testing.T) {
	tests := []struct {
		name string
		key  string
		out  string
		want string
	}{
		{name: "dash value", key: "dhcp6c_flags", out: "dhcp6c_flags: -n\n", want: "-n"},
		{name: "quoted style not emitted", key: "ifconfig_vtnet0", out: "ifconfig_vtnet0: DHCP\n", want: "DHCP"},
		{name: "fallback raw", key: "missing", out: "NO\n", want: "NO"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseFreeBSDSysrcValue(tt.key, []byte(tt.out)); got != tt.want {
				t.Fatalf("parseFreeBSDSysrcValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyFreeBSDRCDScriptsDisablesExecutableBackups(t *testing.T) {
	dir := t.TempDir()
	rcDir := filepath.Join(dir, "rc.d")
	if err := os.MkdirAll(rcDir, 0755); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(rcDir, "routerd.recovery.20260510T235157Z")
	if err := os.WriteFile(backup, []byte("#!/bin/sh\n# PROVIDE: routerd\n"), 0555); err != nil {
		t.Fatal(err)
	}
	changed, err := applyFreeBSDRCDScripts(map[string][]byte{
		"routerd": []byte("#!/bin/sh\n# PROVIDE: routerd\n"),
	}, rcDir)
	if err != nil {
		t.Fatal(err)
	}
	if !stringSliceContainsPrefix(changed, "rc.d:disable-stale:") {
		t.Fatalf("changed = %v, want stale rc.d backup disable marker", changed)
	}
	info, err := os.Stat(backup)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0111 != 0 {
		t.Fatalf("backup remained executable: %v", info.Mode())
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func stringSliceContainsPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func TestCleanupLegacyFreeBSDStateDirMovesVarLibRouterd(t *testing.T) {
	dir := t.TempDir()
	oldDefaults := platformDefaults
	oldLegacy := legacyFreeBSDStateDir
	platformDefaults = platform.Defaults{
		OS:       platform.OSFreeBSD,
		StateDir: filepath.Join(dir, "var", "db", "routerd"),
	}
	legacyFreeBSDStateDir = filepath.Join(dir, "var", "lib", "routerd")
	t.Cleanup(func() {
		platformDefaults = oldDefaults
		legacyFreeBSDStateDir = oldLegacy
	})

	staleLease := filepath.Join(legacyFreeBSDStateDir, "dhcpv6-client", "wan-pd", "lease.json")
	if err := os.MkdirAll(filepath.Dir(staleLease), 0755); err != nil {
		t.Fatalf("create legacy state: %v", err)
	}
	if err := os.WriteFile(staleLease, []byte(`{"currentPrefix":"2001:db8::/60"}`), 0644); err != nil {
		t.Fatalf("write stale lease: %v", err)
	}

	moved, err := cleanupLegacyFreeBSDStateDir()
	if err != nil {
		t.Fatalf("cleanup legacy state: %v", err)
	}
	if len(moved) != 1 || !strings.Contains(moved[0], "legacy-var-lib-routerd-") {
		t.Fatalf("moved = %v", moved)
	}
	if _, err := os.Stat(legacyFreeBSDStateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy state dir still exists or unexpected stat error: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(platformDefaults.StateDir, "legacy-var-lib-routerd-*", "dhcpv6-client", "wan-pd", "lease.json"))
	if err != nil {
		t.Fatalf("glob moved lease: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("moved stale lease matches = %v", matches)
	}
}

func TestFreeBSDProtectedIfnames(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{
		Apply: api.ApplyPolicySpec{ProtectedInterfaces: []string{"mgmt"}},
		Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "vtnet0", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "mgmt"},
				Spec:     api.InterfaceSpec{IfName: "vtnet2", Managed: true},
			},
		},
	}}
	got := freeBSDProtectedIfnames(router)
	if !got["vtnet2"] {
		t.Fatalf("protected ifnames = %v, want vtnet2", got)
	}
	if got["vtnet0"] {
		t.Fatalf("protected ifnames = %v, did not want vtnet0", got)
	}
}

func TestParseDHCPCDDumpLeasePD(t *testing.T) {
	out := []byte(`reason=BOUND6
interface=ens18
protocol=dhcp6
dhcp6_client_id=00030001020000000103
dhcp6_server_id=00030001020000000001
dhcp6_ia_pd1_iaid=00000001
dhcp6_ia_pd1_t1=7200
dhcp6_ia_pd1_t2=12600
dhcp6_ia_pd1_prefix1_pltime=14400
dhcp6_ia_pd1_prefix1_vltime=14400
dhcp6_ia_pd1_prefix1_length=60
dhcp6_ia_pd1_prefix1=2001:db8:3d60:1220::
`)
	prefix, lease, ok := parseDHCPCDDumpLeasePD(out, 60)
	if !ok {
		t.Fatal("parseDHCPCDDumpLeasePD ok = false, want true")
	}
	if prefix != "2001:db8:3d60:1220::/60" {
		t.Fatalf("prefix = %q, want documentation /60", prefix)
	}
	if lease.T1 != "7200" || lease.T2 != "12600" || lease.PLTime != "14400" || lease.VLTime != "14400" {
		t.Fatalf("lease = %#v", lease)
	}
}

func TestObserveFreeBSDDHCPv6CIdentityPayload(t *testing.T) {
	payload := freeBSDDHCPv6CDUIDPayload([]byte{
		0x0e, 0x00,
		0x00, 0x01, 0x00, 0x01, 0x31, 0x82, 0x0f, 0x6f, 0x02, 0x00, 0x00, 0x00, 0x01, 0x01,
	})
	if got := colonHex(payload); got != "00:01:00:01:31:82:0f:6f:02:00:00:00:01:01" {
		t.Fatalf("DUID payload = %s", got)
	}
	if got := configuredOrDefaultDHCPv6CIAID("00000001"); got != "1" {
		t.Fatalf("IAID = %s, want decimal conversion", got)
	}
}

func TestAppendPrefixDelegationStateWarnings(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
		Metadata: api.ObjectMeta{Name: "wan-pd"},
		Spec:     api.DHCPv6PrefixDelegationSpec{Interface: "wan"},
	}}}}
	store := routerstate.New()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{
		LastPrefix: "2001:db8:3d60:1240::/60",
	}), "test")
	result := &apply.Result{}
	appendPrefixDelegationStateWarnings(result, router, store)
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "2001:db8:3d60:1240::/60") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestAppendPrefixDelegationStateWarningsWithoutLastPrefix(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
		Metadata: api.ObjectMeta{Name: "wan-pd"},
		Spec:     api.DHCPv6PrefixDelegationSpec{Interface: "wan"},
	}}}}
	store := routerstate.New()
	store.Unset("ipv6PrefixDelegation.wan-pd.currentPrefix", "test")
	result := &apply.Result{}
	appendPrefixDelegationStateWarnings(result, router, store)
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "no delegated prefix has been recorded") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestRecordHostInventoryState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := store.BeginGeneration("test"); err != nil {
		t.Fatalf("begin generation: %v", err)
	}
	if err := recordHostInventoryState(store); err != nil {
		t.Fatalf("record inventory: %v", err)
	}
	status := store.ObjectStatus(api.RouterAPIVersion, "Inventory", "host")
	if status == nil || status["os"] == nil || status["commands"] == nil {
		t.Fatalf("inventory status = %#v", status)
	}
	events := store.Events(api.RouterAPIVersion, "Inventory", "host", 10)
	if len(events) != 1 || events[0].Reason != "InventoryObserved" {
		t.Fatalf("events = %#v", events)
	}
	if err := recordHostInventoryState(store); err != nil {
		t.Fatalf("record inventory again: %v", err)
	}
	events = store.Events(api.RouterAPIVersion, "Inventory", "host", 10)
	if len(events) != 1 {
		t.Fatalf("unchanged inventory should not add event: %#v", events)
	}
}

func TestParseRFC4361ClientID(t *testing.T) {
	identity := parseRFC4361ClientID("ff000000010003000102005e102030")
	if identity.IAID != "00000001" {
		t.Fatalf("IAID = %q, want 00000001", identity.IAID)
	}
	if identity.DUID != "0003000102005e102030" {
		t.Fatalf("DUID = %q, want link-layer DUID", identity.DUID)
	}
}

func TestLinkLayerDUIDFromMAC(t *testing.T) {
	got := linkLayerDUIDFromMAC("02:00:5e:10:20:30")
	if got != "0003000102005e102030" {
		t.Fatalf("DUID = %q, want 0003000102005e102030", got)
	}
}

func TestFilterRouterByWhenFiltersDHCPv6Information(t *testing.T) {
	when := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
		"VirtualAddress/lan-gw-v4.role": {Equals: "master"},
	}}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
			Metadata: api.ObjectMeta{Name: "wan-pd"},
			Spec: api.DHCPv6PrefixDelegationSpec{
				Interface: "wan",
				When:      when,
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Information"},
			Metadata: api.ObjectMeta{Name: "wan-info"},
			Spec: api.DHCPv6InformationSpec{
				Interface: "wan",
				DependsOn: []api.ResourceDependencySpec{{
					Resource: "DHCPv6PrefixDelegation/wan-pd",
					Phase:    "Bound",
				}},
				When: when,
			},
		},
	}}}

	filtered := filterRouterByWhen(router, routerstate.New())
	if len(filtered.Spec.Resources) != 0 {
		t.Fatalf("resources = %+v, want none", filtered.Spec.Resources)
	}
}

func TestFilterEgressRoutePolicyCandidatesByWhen(t *testing.T) {
	store := routerstate.New()
	store.Set("wan.ipv6.mode", "address-only", "test")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "default-v4"}, Spec: api.EgressRoutePolicySpec{Candidates: []api.EgressRoutePolicyCandidate{
			{Name: "dslite", Priority: 10, Targets: []api.EgressRoutePolicyTarget{{Interface: "dslite-a"}}, When: api.ResourceWhenSpec{Any: []api.ResourceWhenSpec{
				{State: map[string]api.StateMatchSpec{"wan.ipv6.mode": {Equals: "pd-ready"}}},
				{State: map[string]api.StateMatchSpec{"wan.ipv6.mode": {Equals: "address-only"}}},
			}}},
			{Name: "pppoe", Interface: "wan-pppoe", Priority: 20, When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"wan.ipv6.mode": {Equals: "ipv4-only"}}}},
		}}},
	}}}
	filtered := filterRouterByWhen(router, store)
	spec, err := filtered.Spec.Resources[0].EgressRoutePolicySpec()
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Candidates) != 1 || spec.Candidates[0].Name != "dslite" {
		t.Fatalf("candidates = %+v, want only dslite", spec.Candidates)
	}
}

func TestFilterRouterByWhenPreservesImplicitBFDRef(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: "rr"},
			Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/fabric",
				PeerASN:   64512,
				Peers:     []string{"10.99.0.2"},
				BFD:       "BFD/implicit",
			},
		},
	}}}

	filtered := filterRouterByWhen(router, routerstate.New())
	if len(filtered.Spec.Resources) != 1 {
		t.Fatalf("resources = %d, want only BGPPeer", len(filtered.Spec.Resources))
	}
	spec, err := filtered.Spec.Resources[0].BGPPeerSpec()
	if err != nil {
		t.Fatal(err)
	}
	if spec.BFD != "BFD/implicit" {
		t.Fatalf("BGPPeer BFD ref = %q, want implicit ref preserved", spec.BFD)
	}
}

func TestWebConsoleResolvesListenAddressFromResourceStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer store.Close()
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "mgmt", map[string]any{
		"phase":         "Up",
		"ipv4Addresses": []string{"192.168.123.129/24"},
	}); err != nil {
		t.Fatalf("save status: %v", err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "WebConsole"},
			Metadata: api.ObjectMeta{Name: "mgmt"},
			Spec: api.WebConsoleSpec{
				ListenAddressFrom: api.StatusValueSourceSpec{Resource: "Interface/mgmt", Field: "ipv4Addresses"},
				Port:              8080,
			},
		},
	}}}
	spec, ok, err := webConsoleFromRouter(router, store)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("web console disabled")
	}
	if spec.ListenAddress != "192.168.123.129" {
		t.Fatalf("listen address = %q", spec.ListenAddress)
	}
}

func TestConfiguredDHCPLeasePathsPreferControllerDnsmasqConfig(t *testing.T) {
	paths := configuredDHCPLeasePaths("/tmp/routerd/dnsmasq.conf")
	if len(paths) == 0 {
		t.Fatal("no lease paths")
	}
	if paths[0] != "/tmp/routerd/dnsmasq.leases" {
		t.Fatalf("first lease path = %q, want controller dnsmasq lease file; paths=%v", paths[0], paths)
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] {
			t.Fatalf("duplicate lease path %q in %v", path, paths)
		}
		seen[path] = true
	}
}
