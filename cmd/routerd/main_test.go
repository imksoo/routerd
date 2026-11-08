// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
	"github.com/imksoo/routerd/pkg/pdclient"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resource"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestServeConfigMutatorPlanSanitizesUninitializedStateSchema(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "uninitialized.db")
	db, err := sql.Open("sqlite", statePath)
	if err != nil {
		t.Fatalf("open sqlite directly: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE placeholder (id INTEGER)`); err != nil {
		t.Fatalf("create placeholder table: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
	}
	mutator := serveConfigMutator{
		statePath: statePath,
		baseOpts: applyOptions{
			StatePath:          statePath,
			LedgerPath:         filepath.Join(dir, "ledger.db"),
			SkipServiceManager: true,
		},
		logger: &eventlog.Logger{},
	}
	_, err = mutator.planRouter(router, "")
	if err == nil {
		t.Fatal("planRouter succeeded with uninitialized state schema")
	}
	if !strings.Contains(err.Error(), "state database is not initialized") {
		t.Fatalf("planRouter error is not actionable: %v", err)
	}
	if strings.Contains(err.Error(), "SQL logic error") || strings.Contains(err.Error(), "no such table") || strings.Contains(err.Error(), "objects") {
		t.Fatalf("planRouter leaked internal schema details: %v", err)
	}
}

func TestApplyIPsecConnectionsSynchronizesWholeOwnedDirectory(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "routerd-stale.conf")
	if err := os.WriteFile(stale, []byte("stale\n"), 0600); err != nil {
		t.Fatalf("write stale configuration: %v", err)
	}
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "ipsec-test"},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPsecConnection"},
			Metadata: api.ObjectMeta{Name: "site-a"},
			Spec: api.IPsecConnectionSpec{
				LocalAddress: "198.51.100.10", RemoteAddress: "203.0.113.20", PreSharedKey: "secret",
				LeftSubnet: "10.0.0.0/24", RightSubnet: "10.10.0.0/24",
			},
		}}},
	}
	loads := 0
	changed, err := applyIPsecConnectionsWithOptions(context.Background(), router, ipsecRuntimeApplyOptions{
		ConfigDir: dir,
		Load: func(context.Context) error {
			loads++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("apply IPsec connections: %v", err)
	}
	if loads != 1 {
		t.Fatalf("load count = %d, want 1", loads)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale configuration stat = %v, want not exist", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "routerd-site-a.conf"))
	if err != nil || !strings.Contains(string(data), "secrets {") {
		t.Fatalf("managed configuration = %q, err=%v", data, err)
	}
	aggregate, err := os.ReadFile(filepath.Join(dir, "routerd.conf"))
	if err != nil || !strings.Contains(string(aggregate), "include "+filepath.Join(dir, "routerd-site-a.conf")) {
		t.Fatalf("aggregate configuration = %q, err=%v", aggregate, err)
	}
	for _, name := range ipsecSwanctlCredentialDirectories {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !info.IsDir() {
			t.Fatalf("swanctl credential directory %s: info=%v err=%v", name, info, err)
		}
	}
	if len(changed) != 3 {
		t.Fatalf("changed = %v, want managed write, aggregate, and stale removal", changed)
	}
	if _, err := os.Stat(filepath.Join(dir, ipsecPendingLoadMarker)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending marker stat = %v, want removed after successful load", err)
	}

	changed, err = applyIPsecConnectionsWithOptions(context.Background(), router, ipsecRuntimeApplyOptions{
		ConfigDir: dir,
		Load: func(context.Context) error {
			loads++
			return nil
		},
	})
	if err != nil || len(changed) != 0 || loads != 2 {
		t.Fatalf("unchanged reload changed=%v loads=%d err=%v", changed, loads, err)
	}

	changed, err = applyIPsecConnectionsWithOptions(context.Background(), &api.Router{}, ipsecRuntimeApplyOptions{
		ConfigDir: dir,
		Load: func(context.Context) error {
			loads++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("remove managed IPsec connection: %v", err)
	}
	if loads != 3 || len(changed) != 2 || !strings.HasPrefix(changed[0], "removed:") {
		t.Fatalf("removal changed=%v loads=%d", changed, loads)
	}
	if _, err := os.Stat(filepath.Join(dir, "routerd.conf")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty aggregate stat = %v, want removed", err)
	}
}

func TestApplyIPsecConnectionsRetriesPendingLoad(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPsecConnection"},
		Metadata: api.ObjectMeta{Name: "site-a"},
		Spec: api.IPsecConnectionSpec{
			LocalAddress: "198.51.100.10", RemoteAddress: "203.0.113.20", PreSharedKey: "secret",
			LeftSubnet: "10.0.0.0/24", RightSubnet: "10.10.0.0/16",
		},
	}}}}
	loadErr := errors.New("charon unavailable")
	_, err := applyIPsecConnectionsWithOptions(context.Background(), router, ipsecRuntimeApplyOptions{
		ConfigDir: dir,
		Load:      func(context.Context) error { return loadErr },
	})
	if !errors.Is(err, loadErr) {
		t.Fatalf("first load error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ipsecPendingLoadMarker)); err != nil {
		t.Fatalf("pending marker after failed load: %v", err)
	}
	loads := 0
	changed, err := applyIPsecConnectionsWithOptions(context.Background(), router, ipsecRuntimeApplyOptions{
		ConfigDir: dir,
		Load: func(context.Context) error {
			loads++
			return nil
		},
	})
	if err != nil || loads != 1 || len(changed) != 0 {
		t.Fatalf("retry changed=%v loads=%d err=%v", changed, loads, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ipsecPendingLoadMarker)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending marker after retry = %v, want removed", err)
	}
}

func TestApplyIPsecConnectionsMigratesOnlyRouterdLegacyConfigs(t *testing.T) {
	dir := t.TempDir()
	legacy := t.TempDir()
	for name, data := range map[string]string{
		"routerd-site-a.conf":        "legacy connection\n",
		"routerd.conf":               "legacy aggregate\n",
		ipsecPendingLoadMarker:       "pending\n",
		"operator-managed-site.conf": "operator config\n",
	} {
		if err := os.WriteFile(filepath.Join(legacy, name), []byte(data), 0600); err != nil {
			t.Fatalf("write legacy %s: %v", name, err)
		}
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPsecConnection"},
		Metadata: api.ObjectMeta{Name: "site-b"},
		Spec: api.IPsecConnectionSpec{
			LocalAddress: "198.51.100.10", RemoteAddress: "203.0.113.20", PreSharedKey: "secret",
			LeftSubnet: "10.0.0.0/24", RightSubnet: "10.10.0.0/16",
		},
	}}}}
	loads := 0
	if _, err := applyIPsecConnectionsWithOptions(context.Background(), router, ipsecRuntimeApplyOptions{
		ConfigDir:       dir,
		LegacyConfigDir: legacy,
		Load: func(context.Context) error {
			loads++
			return nil
		},
	}); err != nil {
		t.Fatalf("migrate legacy configurations: %v", err)
	}
	if loads != 1 {
		t.Fatalf("load count = %d, want 1", loads)
	}
	for _, name := range []string{"routerd-site-a.conf", "routerd.conf", ipsecPendingLoadMarker} {
		if _, err := os.Lstat(filepath.Join(legacy, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("legacy %s stat = %v, want removed", name, err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(legacy, "operator-managed-site.conf")); err != nil || string(data) != "operator config\n" {
		t.Fatalf("operator config = %q, err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "routerd-site-b.conf")); err != nil {
		t.Fatalf("new runtime configuration missing: %v", err)
	}
}

func TestApplyIPsecConnectionsRefusesLegacyRouterdSymlink(t *testing.T) {
	dir := t.TempDir()
	legacy := t.TempDir()
	link := filepath.Join(legacy, "routerd.conf")
	if err := os.Symlink(filepath.Join(legacy, "operator.conf"), link); err != nil {
		t.Fatalf("create legacy symlink: %v", err)
	}
	_, err := applyIPsecConnectionsWithOptions(context.Background(), &api.Router{}, ipsecRuntimeApplyOptions{
		ConfigDir:       dir,
		LegacyConfigDir: legacy,
		Load:            func(context.Context) error { return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "non-regular") {
		t.Fatalf("legacy symlink error = %v", err)
	}
	if info, statErr := os.Lstat(link); statErr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("legacy symlink changed: info=%v err=%v", info, statErr)
	}
}

func TestApplyIPsecConnectionsKeepsPendingMarkerWhenEmptyAggregateRemovalFails(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPsecConnection"},
		Metadata: api.ObjectMeta{Name: "site-a"},
		Spec: api.IPsecConnectionSpec{
			LocalAddress: "198.51.100.10", RemoteAddress: "203.0.113.20", PreSharedKey: "secret",
			LeftSubnet: "10.0.0.0/24", RightSubnet: "10.10.0.0/16",
		},
	}}}}
	if _, err := applyIPsecConnectionsWithOptions(context.Background(), router, ipsecRuntimeApplyOptions{
		ConfigDir: dir,
		Load:      func(context.Context) error { return nil },
	}); err != nil {
		t.Fatalf("seed managed IPsec configuration: %v", err)
	}
	aggregate := filepath.Join(dir, "routerd.conf")
	_, err := applyIPsecConnectionsWithOptions(context.Background(), &api.Router{}, ipsecRuntimeApplyOptions{
		ConfigDir: dir,
		Load: func(context.Context) error {
			if err := os.Remove(aggregate); err != nil {
				return err
			}
			if err := os.Mkdir(aggregate, 0700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(aggregate, "keep"), []byte("x"), 0600)
		},
	})
	if err == nil || !strings.Contains(err.Error(), "remove empty IPsec swanctl aggregate") {
		t.Fatalf("empty aggregate teardown error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ipsecPendingLoadMarker)); statErr != nil {
		t.Fatalf("pending marker after aggregate removal failure: %v", statErr)
	}
}

func TestApplyIPsecConnectionsPassesCallerContextToLoad(t *testing.T) {
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request"), "apply-ctx")
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPsecConnection"},
		Metadata: api.ObjectMeta{Name: "site-a"},
		Spec: api.IPsecConnectionSpec{
			LocalAddress: "198.51.100.10", RemoteAddress: "203.0.113.20", PreSharedKey: "secret",
			LeftSubnet: "10.0.0.0/24", RightSubnet: "10.10.0.0/16",
		},
	}}}}
	_, err := applyIPsecConnectionsWithOptions(ctx, router, ipsecRuntimeApplyOptions{
		ConfigDir: dir,
		Load: func(got context.Context) error {
			if got.Value(contextKey("request")) != "apply-ctx" {
				t.Fatalf("load context value = %v", got.Value(contextKey("request")))
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("apply IPsec connections: %v", err)
	}
}

func TestIPsecRuntimePathsArePlatformSpecific(t *testing.T) {
	oldDefaults := platformDefaults
	t.Cleanup(func() { platformDefaults = oldDefaults })
	platformDefaults.OS = platform.OSLinux
	if got := ipsecConfigDir(); got != "/etc/routerd/swanctl" {
		t.Fatalf("Linux config dir = %q", got)
	}
	if got := ipsecLegacyConfigDir(); got != "/etc/swanctl/conf.d" {
		t.Fatalf("Linux legacy config dir = %q", got)
	}
	if got := ipsecSwanctlPath(); got != "swanctl" {
		t.Fatalf("Linux swanctl path = %q", got)
	}
	platformDefaults.OS = platform.OSFreeBSD
	if got := ipsecConfigDir(); got != "/usr/local/etc/routerd/swanctl" {
		t.Fatalf("FreeBSD config dir = %q", got)
	}
	if got := ipsecLegacyConfigDir(); got != "/usr/local/etc/swanctl/conf.d" {
		t.Fatalf("FreeBSD legacy config dir = %q", got)
	}
	if got := ipsecSwanctlPath(); got != "/usr/local/sbin/swanctl" {
		t.Fatalf("FreeBSD swanctl path = %q", got)
	}
}

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

func TestApplyCommandDryRunUsesChainOnceWithoutCreatingStateDB(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	stateDir := filepath.Join(dir, "state")
	statePath := filepath.Join(stateDir, "routerd.db")
	ledgerDir := filepath.Join(dir, "ledger")
	ledgerPath := filepath.Join(ledgerDir, "routerd.db")
	statusPath := filepath.Join(dir, "status.json")
	if err := os.WriteFile(configPath, []byte(testRouterYAML("apply-chain-dry-run")), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout strings.Builder
	if err := applyCommand([]string{
		"--config", configPath,
		"--once",
		"--dry-run",
		"--state-file", statePath,
		"--ledger-file", ledgerPath,
		"--status-file", statusPath,
		"--skip-service-manager",
	}, &stdout, io.Discard); err != nil {
		t.Fatalf("apply --once --dry-run: %v", err)
	}
	if !strings.Contains(stdout.String(), "dry-run apply plan") || !strings.Contains(stdout.String(), `"phase": "Healthy"`) {
		t.Fatalf("apply dry-run output missing plan/result:\n%s", stdout.String())
	}
	if _, err := os.Stat(statusPath); err != nil {
		t.Fatalf("status file: %v", err)
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

func TestRunDispatchesApplyDryRun(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	statePath := filepath.Join(dir, "state", "routerd.db")
	ledgerPath := filepath.Join(dir, "ledger", "routerd.db")
	statusPath := filepath.Join(dir, "status.json")
	if err := os.WriteFile(configPath, []byte(testRouterYAML("apply-dispatch-dry-run")), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout strings.Builder
	if err := run([]string{
		"apply",
		"--config", configPath,
		"--once",
		"--dry-run",
		"--state-file", statePath,
		"--ledger-file", ledgerPath,
		"--status-file", statusPath,
		"--skip-service-manager",
	}, &stdout, io.Discard); err != nil {
		t.Fatalf("routerd apply --once --dry-run: %v", err)
	}
	if !strings.Contains(stdout.String(), "dry-run apply plan") || !strings.Contains(stdout.String(), `"phase": "Healthy"`) {
		t.Fatalf("apply dry-run output missing plan/result:\n%s", stdout.String())
	}
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run state db stat error = %v, want not exist", err)
	}
}

func TestCleanupLedgerOwnedArtifactIPv6AddressLinux(t *testing.T) {
	oldFeatures := platformFeatures
	oldRun := runCleanupCommand
	t.Cleanup(func() {
		platformFeatures = oldFeatures
		runCleanupCommand = oldRun
	})
	platformFeatures = platform.Features{HasIproute2: true}
	var gotName string
	var gotArgs []string
	runCleanupCommand = func(name string, args ...string) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil
	}
	label, err := cleanupLedgerOwnedArtifact(resource.Artifact{
		Kind:  "net.ipv6.address",
		Name:  "ens19:2001:db8::1/64",
		Owner: api.NetAPIVersion + "/VirtualAddress/old-v6",
	})
	if err != nil {
		t.Fatalf("cleanup IPv6 address: %v", err)
	}
	if label != "net.ipv6.address/ens19:2001:db8::1/64" {
		t.Fatalf("label = %q", label)
	}
	if gotName != "ip" || !reflect.DeepEqual(gotArgs, []string{"-6", "addr", "del", "2001:db8::1/64", "dev", "ens19"}) {
		t.Fatalf("command = %s %#v", gotName, gotArgs)
	}
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

func TestLoadServeRouterFallsBackToLastGoodGeneration(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	statePath := filepath.Join(dir, "routerd.db")
	if err := os.WriteFile(configPath, []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {}
spec:
  resources: []
`), 0644); err != nil {
		t.Fatalf("write invalid canonical config: %v", err)
	}
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	good := seedGeneration(t, store, "hash-good", testRouterYAML("last-good-router"), true, "Healthy")
	_ = seedGeneration(t, store, "hash-bad", testRouterYAML("newer-errored-router"), true, "Errored")

	router, fallback, err := loadServeRouter(configPath, store)
	if err != nil {
		t.Fatalf("load serve router: %v", err)
	}
	if !fallback.Used || fallback.Generation != good {
		t.Fatalf("fallback = %+v, want generation %d", fallback, good)
	}
	if router.Metadata.Name != "last-good-router" {
		t.Fatalf("router name = %q, want last-good-router", router.Metadata.Name)
	}
	var stderr strings.Builder
	emitServeBootFallbackWarning(&stderr, nil, configPath, fallback)
	if got := stderr.String(); !strings.Contains(got, "WARNING:") || !strings.Contains(got, fmt.Sprintf("generation %d", good)) {
		t.Fatalf("fallback warning missing details:\n%s", got)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}
}

func TestServeConfigMutatorRejectsInvalidCandidateWithoutChangingCanonical(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	original := testRouterYAML("stable-router")
	if err := os.WriteFile(configPath, []byte(original), 0644); err != nil {
		t.Fatalf("write canonical config: %v", err)
	}
	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load router: %v", err)
	}
	mutator := serveConfigMutator{
		configPath: configPath,
		statePath:  filepath.Join(dir, "routerd.db"),
		baseOpts:   applyOptions{ConfigPath: configPath, StatePath: filepath.Join(dir, "routerd.db"), LedgerPath: filepath.Join(dir, "ledger.db"), SkipServiceManager: true},
		cache:      &resultCache{},
		logger:     &eventlog.Logger{},
		getRouter:  func() *api.Router { return router },
		setRouter:  func(next *api.Router) { router = next },
	}
	_, err = mutator.apply(nil, controlapi.ApplyRequest{CandidateYAML: `apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {}
spec:
  resources: []
`, Replace: true})
	if err == nil {
		t.Fatal("mutating apply succeeded, want validation error")
	}
	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read canonical config: %v", readErr)
	}
	if string(data) != original {
		t.Fatalf("canonical changed after invalid candidate:\n%s", data)
	}
}

func TestServeConfigMutatorDeleteNoReconcileCommitsForRestartWithoutChangingRuntime(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	statePath := filepath.Join(dir, "routerd.db")
	input := `apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: delete-router
spec:
  resources:
    # resource to remove
    - apiVersion: net.routerd.net/v1alpha1
      kind: Hostname
      metadata:
        name: appliance
      spec:
        hostname: appliance.example
`
	if err := os.WriteFile(configPath, []byte(input), 0644); err != nil {
		t.Fatalf("write canonical config: %v", err)
	}
	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load router: %v", err)
	}
	mutator := serveConfigMutator{
		configPath: configPath,
		statePath:  statePath,
		baseOpts:   applyOptions{ConfigPath: configPath, StatePath: statePath, LedgerPath: filepath.Join(dir, "ledger.db"), SkipServiceManager: true},
		cache:      &resultCache{},
		logger:     &eventlog.Logger{},
		getRouter:  func() *api.Router { return router },
		setRouter:  func(next *api.Router) { router = next },
	}
	result, err := mutator.delete(nil, controlapi.DeleteRequest{Target: "Hostname/appliance", NoReconcile: true})
	if err != nil {
		t.Fatalf("delete mutation: %v", err)
	}
	if result.Result == nil || result.Result.Phase != "Committed" || result.Result.Generation == 0 {
		t.Fatalf("delete result = %+v", result)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read canonical config: %v", err)
	}
	if strings.Contains(string(data), "appliance") || strings.Contains(string(data), "resource to remove") {
		t.Fatalf("canonical still contains deleted resource:\n%s", data)
	}
	if len(router.Spec.Resources) != 1 {
		t.Fatalf("running generation changed before restart: resources = %d, want 1", len(router.Spec.Resources))
	}
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := os.WriteFile(configPath, []byte("not: valid: YAML\n"), 0644); err != nil {
		t.Fatalf("corrupt canonical config: %v", err)
	}
	fallbackRouter, fallback, err := loadServeRouter(configPath, routerstate.GenerationHistoryReader(store))
	if err != nil {
		t.Fatalf("load committed fallback: %v", err)
	}
	if !fallback.Used || fallback.Generation != result.Result.Generation {
		t.Fatalf("committed fallback = %+v, want generation %d", fallback, result.Result.Generation)
	}
	if len(fallbackRouter.Spec.Resources) != 0 {
		t.Fatalf("committed fallback resources = %d, want 0", len(fallbackRouter.Spec.Resources))
	}
}

func TestServeOnceConvergesAndExits(t *testing.T) {
	loopbackEnsured := false
	originalEnsureLoopback := ensureLoopbackUpForServe
	ensureLoopbackUpForServe = func() error {
		loopbackEnsured = true
		return nil
	}
	t.Cleanup(func() { ensureLoopbackUpForServe = originalEnsureLoopback })

	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	statePath := filepath.Join(dir, "routerd.db")
	statusPath := filepath.Join(dir, "status.json")
	ledgerPath := filepath.Join(dir, "ledger.db")
	if err := os.WriteFile(configPath, []byte(testRouterYAML("serve-once-router")), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout strings.Builder
	err := serveCommand([]string{
		"--config", configPath,
		"--state-file", statePath,
		"--status-file", statusPath,
		"--ledger-file", ledgerPath,
		"--controllers", "log-retention",
		"--once",
	}, &stdout, io.Discard)
	if err != nil {
		t.Fatalf("serve --once: %v", err)
	}
	if !strings.Contains(stdout.String(), `"phase": "Healthy"`) {
		t.Fatalf("serve --once output missing result:\n%s", stdout.String())
	}
	if !loopbackEnsured {
		t.Fatal("serve --once did not ensure loopback is up")
	}
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	defer func() { _ = store.Close() }()
	if got := store.LatestGeneration(); got == 0 {
		t.Fatal("serve --once did not record a generation")
	}
	if _, err := os.Stat(statusPath); err != nil {
		t.Fatalf("status file: %v", err)
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

func TestServeAcceptsLegacyControllerChainFlags(t *testing.T) {
	loopbackEnsured := false
	originalEnsureLoopback := ensureLoopbackUpForServe
	ensureLoopbackUpForServe = func() error {
		loopbackEnsured = true
		return nil
	}
	t.Cleanup(func() { ensureLoopbackUpForServe = originalEnsureLoopback })

	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	statePath := filepath.Join(dir, "routerd.db")
	statusPath := filepath.Join(dir, "status.json")
	ledgerPath := filepath.Join(dir, "ledger.db")
	if err := os.WriteFile(configPath, []byte(testRouterYAML("serve-legacy-flags-router")), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout, stderr strings.Builder
	err := serveCommand([]string{
		"--config", configPath,
		"--state-file", statePath,
		"--status-file", statusPath,
		"--ledger-file", ledgerPath,
		"--controllers", "log-retention",
		"--once",
		"--observe-interval", "30s",
		"--controller-chain",
		"--controller-chain-daemon-sockets", "wan-pd=/run/routerd/dhcpv6-client/wan-pd.sock",
		"--controller-chain-dnsmasq-command", "/usr/local/sbin/dnsmasq",
		"--controller-chain-dnsmasq-config", "/run/routerd/dnsmasq.conf",
		"--controller-chain-dnsmasq-listen-addresses", "127.0.0.1",
		"--controller-chain-dnsmasq-pid", "/run/routerd/dnsmasq.pid",
		"--controller-chain-dnsmasq-port", "53",
		"--controller-chain-dry-run-address=false",
		"--controller-chain-dry-run-dhcpv4lease=false",
		"--controller-chain-dry-run-dhcpv6=false",
		"--controller-chain-dry-run-dns-resolver=false",
		"--controller-chain-dry-run-dslite=false",
		"--controller-chain-dry-run-firewall=false",
		"--controller-chain-dry-run-nat=false",
		"--controller-chain-dry-run-network-adoption=false",
		"--controller-chain-dry-run-package=false",
		"--controller-chain-dry-run-ra=false",
		"--controller-chain-dry-run-route=false",
		"--controller-chain-dry-run-systemd-unit=false",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("serve --once with legacy flags: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"phase": "Healthy"`) {
		t.Fatalf("serve --once output missing result:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "ignored legacy flag") || !strings.Contains(stderr.String(), "--observe-interval") || !strings.Contains(stderr.String(), "--controller-chain-dnsmasq-command") {
		t.Fatalf("missing legacy flag warning:\n%s", stderr.String())
	}
	if !loopbackEnsured {
		t.Fatal("serve --once did not ensure loopback is up")
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

func TestControlAPIAdmissionUsesRemoteAddrOnly(t *testing.T) {
	prefixes, err := parseControlAPIAllowCIDRs([]string{"127.0.0.1/32"})
	if err != nil {
		t.Fatalf("parseControlAPIAllowCIDRs: %v", err)
	}
	called := false
	handler := controlAPIAdmissionHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}), prefixes, "")
	req := httptest.NewRequest(http.MethodGet, "http://routerd.test/api/control.routerd.net/v1alpha1/status", nil)
	req.RemoteAddr = "10.30.0.25:45678"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if called {
		t.Fatal("handler was called for rejected source")
	}
}

func TestControlAPIAdmissionRequiresBearerTokenWhenConfigured(t *testing.T) {
	prefixes, err := parseControlAPIAllowCIDRs([]string{"127.0.0.1/32"})
	if err != nil {
		t.Fatalf("parseControlAPIAllowCIDRs: %v", err)
	}
	var calls int
	handler := controlAPIAdmissionHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}), prefixes, "secret-token")

	req := httptest.NewRequest(http.MethodGet, "http://routerd.test/api/control.routerd.net/v1alpha1/status", nil)
	req.RemoteAddr = "127.0.0.1:45678"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, "http://routerd.test/api/control.routerd.net/v1alpha1/status", nil)
	req.RemoteAddr = "127.0.0.1:45678"
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, "http://routerd.test/api/control.routerd.net/v1alpha1/status", nil)
	req.RemoteAddr = "127.0.0.1:45678"
	req.Header.Set("Authorization", "Bearer secret-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("valid token status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
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

func TestRollbackToGenerationDryRunUsesStoredConfig(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "routerd.db")
	statusPath := filepath.Join(dir, "status.json")
	ledgerPath := filepath.Join(dir, "ledger.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	selectedYAML := strings.Replace(testRouterYAML("rollback-selected"), "resources: []", "resources:\n    - apiVersion: net.routerd.net/v1alpha1\n      kind: Interface\n      metadata:\n        name: selected-interface\n      spec:\n        ifname: lo\n        managed: false\n        owner: external", 1)
	otherYAML := strings.Replace(selectedYAML, "selected-interface", "other-interface", 1)
	gen := seedGeneration(t, store, "hash-selected", selectedYAML, true, "Applied")
	latest := seedGeneration(t, store, "hash-other", otherYAML, true, "Applied")
	before, err := store.ListGenerations(20)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	var stdout strings.Builder
	err = rollbackCommand([]string{
		"--to", fmt.Sprintf("%d", gen),
		"--dry-run",
		"--state-file", statePath,
		"--ledger-file", ledgerPath,
		"--status-file", statusPath,
		"--skip-service-manager",
	}, &stdout, io.Discard)
	if err != nil {
		t.Fatalf("rollback --to --dry-run: %v", err)
	}
	if !strings.Contains(stdout.String(), "dry-run apply plan") {
		t.Fatalf("dry-run output missing plan banner:\n%s", stdout.String())
	}
	store, err = routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	defer func() { _ = store.Close() }()
	if got := store.LatestGeneration(); got != latest {
		t.Fatalf("latest generation after dry-run = %d, want %d", got, latest)
	}
	var result apply.Result
	data, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("typed selected plan: %v", err)
	}
	after, err := store.ListGenerations(20)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("stored generation records changed: before=%#v after=%#v", before, after)
	}
	if _, err := os.Stat(ledgerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run mutated ownership ledger: %v", err)
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

func TestStaleObjectStatusesKeepsConfiguredAndSyntheticKinds(t *testing.T) {
	statuses := []routerstate.ObjectStatus{
		{APIVersion: api.NetAPIVersion, Kind: "PPPoEInterface", Name: "wan"},
		{APIVersion: api.NetAPIVersion, Kind: "Interface", Name: "wan"},
		{APIVersion: api.NetAPIVersion, Kind: "BGPRouter", Name: "lan"},
		{APIVersion: api.RouterAPIVersion, Kind: "Inventory", Name: "host"},
		{APIVersion: api.NetAPIVersion, Kind: "ConntrackObserver", Name: "default"},
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}},
	}}}
	got := staleObjectStatuses(router, statuses)
	if len(got) != 1 || got[0].Kind != "PPPoEInterface" {
		t.Fatalf("stale statuses = %+v, want only PPPoEInterface", got)
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

func TestRouterWithIPv6PDClientOptionsResolvesFlavorDefaults(t *testing.T) {
	router := testRouterWithPrefixDelegation(api.DHCPv6PrefixDelegationSpec{
		Interface: "wan",
		Profile:   api.IPv6PDProfileNTTHGWLANPD,
	})

	got, warnings, err := routerWithIPv6PDClientOptions(router, applyOptions{}, "linux")
	if err != nil {
		t.Fatalf("resolve PD options: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	spec, err := got.Spec.Resources[1].DHCPv6PrefixDelegationSpec()
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if spec.Client != api.IPv6PDClientRouterd {
		t.Fatalf("client = %q, want routerd-dhcpv6-client", spec.Client)
	}
	if spec.Profile != api.IPv6PDProfileNTTHGWLANPD {
		t.Fatalf("profile = %q, want original profile", spec.Profile)
	}

}

func TestRouterWithIPv6PDClientOptionsOverridesAndWarns(t *testing.T) {
	router := testRouterWithPrefixDelegation(api.DHCPv6PrefixDelegationSpec{
		Interface: "wan",
		Client:    api.IPv6PDClientDHCPv6C,
		Profile:   api.IPv6PDProfileNTTHGWLANPD,
	})

	got, warnings, err := routerWithIPv6PDClientOptions(router, applyOptions{
		OverrideClient: api.IPv6PDClientDHCPCD,
	}, "freebsd")
	if err != nil {
		t.Fatalf("resolve overridden PD options: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one known-ng warning", warnings)
	}
	if !strings.Contains(warnings[0], "Known") && !strings.Contains(warnings[0], "known problematic") {
		t.Fatalf("warning does not describe known problem: %q", warnings[0])
	}
	spec, err := got.Spec.Resources[1].DHCPv6PrefixDelegationSpec()
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if spec.Client != api.IPv6PDClientDHCPCD {
		t.Fatalf("client = %q, want override dhcpcd", spec.Client)
	}

	original, err := router.Spec.Resources[1].DHCPv6PrefixDelegationSpec()
	if err != nil {
		t.Fatalf("read original spec: %v", err)
	}
	if original.Client != api.IPv6PDClientDHCPv6C {
		t.Fatalf("original router mutated: client = %q", original.Client)
	}
}

func TestRouterWithIPv6PDClientOptionsRejectsInvalidOverride(t *testing.T) {
	router := testRouterWithPrefixDelegation(api.DHCPv6PrefixDelegationSpec{Interface: "wan"})
	if _, _, err := routerWithIPv6PDClientOptions(router, applyOptions{OverrideClient: "bad"}, "linux"); err == nil {
		t.Fatal("expected invalid override client to be rejected")
	}
	if _, _, err := routerWithIPv6PDClientOptions(router, applyOptions{OverrideProfile: "bad"}, "linux"); err == nil {
		t.Fatal("expected invalid override profile to be rejected")
	}
}

func testRouterWithPrefixDelegation(spec api.DHCPv6PrefixDelegationSpec) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
				Metadata: api.ObjectMeta{Name: "wan-pd"},
				Spec:     spec,
			},
		}},
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

func TestDeriveIPv6Address(t *testing.T) {
	got, err := deriveIPv6Address([]string{"2001:db8:3d60:1220::/64"}, "::100")
	if err != nil {
		t.Fatalf("derive IPv6 address: %v", err)
	}
	if got != "2001:db8:3d60:1220::100" {
		t.Fatalf("address = %s, want 2001:db8:3d60:1220::100", got)
	}
}

func TestDeriveIPv6AddressFromGlobalAddress(t *testing.T) {
	got, err := deriveIPv6AddressFromGlobalAddress([]string{
		"fe80::3",
		"2001:db8:3d60:1220::3",
	}, "::100")
	if err != nil {
		t.Fatalf("derive IPv6 address from global address: %v", err)
	}
	if got != "2001:db8:3d60:1220::100" {
		t.Fatalf("address = %s, want 2001:db8:3d60:1220::100", got)
	}
}

func TestDeriveIPv6AddressFromDelegatedPrefix(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		subnetID string
		suffix   string
		want     string
	}{
		{
			name:     "documentation /60 first subnet",
			prefix:   "2001:db8:3d60:1220::/60",
			subnetID: "0",
			suffix:   "::1",
			want:     "2001:db8:3d60:1220::1",
		},
		{
			name:     "documentation /60 hex subnet",
			prefix:   "2001:db8:3d60:1220::/60",
			subnetID: "a",
			suffix:   "::3",
			want:     "2001:db8:3d60:122a::3",
		},
		{
			name:     "documentation /56 decimal subnet",
			prefix:   "2001:db8:3d60:1200::/56",
			subnetID: "16",
			suffix:   "::100",
			want:     "2001:db8:3d60:1210::100",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := deriveIPv6AddressFromDelegatedPrefix(tt.prefix, tt.subnetID, tt.suffix)
			if err != nil {
				t.Fatalf("derive IPv6 delegated address: %v", err)
			}
			if got != tt.want {
				t.Fatalf("address = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDelegatedPrefixFromObservedUsesKernelPrefix(t *testing.T) {
	got, ok := delegatedPrefixFromObserved([]string{
		"fe80::/64",
		"2001:db8:3d60:1240::/64",
	}, nil, 60)
	if !ok {
		t.Fatal("delegated prefix not found")
	}
	if got != "2001:db8:3d60:1240::/60" {
		t.Fatalf("prefix = %s, want 2001:db8:3d60:1240::/60", got)
	}
}

func TestDelegatedPrefixFromObservedIgnoresStandaloneAddress(t *testing.T) {
	if got, ok := delegatedPrefixFromObserved(nil, []string{
		"fe80::3",
		"2001:db8:3d60:1240::2",
	}, 60); ok {
		t.Fatalf("prefix = %s, want no prefix from standalone address", got)
	}
}

func TestDelegatedPrefixFromObservedIgnoresHostRoute(t *testing.T) {
	if got, ok := delegatedPrefixFromObserved([]string{
		"2001:db8:3d60:1240::2/128",
	}, nil, 60); ok {
		t.Fatalf("prefix = %s, want no prefix from host route", got)
	}
}

func TestDelegatedPrefixFromAddressEntriesIgnoresManagedSuffixWhenFreshClientAddressExists(t *testing.T) {
	got, ok := delegatedPrefixFromAddressEntries([]ipv6AddressEntry{
		{Address: "fe80::1", PrefixLen: 64},
		{Address: "2001:db8:3d60:1240::1", PrefixLen: 64},
		{Address: "2001:db8:3d60:1220:be24:11ff:fea3:c1f4", PrefixLen: 64},
	}, 60, map[uint64]bool{ipv6HostSuffix64(netip.MustParseAddr("::1")): true})
	if !ok {
		t.Fatal("delegated prefix not found")
	}
	if got != "2001:db8:3d60:1220::/60" {
		t.Fatalf("prefix = %s, want 2001:db8:3d60:1220::/60", got)
	}
}

func TestDelegatedPrefixFromAddressEntriesFallsBackWhenOnlyManagedSuffixExists(t *testing.T) {
	if got, ok := delegatedPrefixFromAddressEntries([]ipv6AddressEntry{
		{Address: "2001:db8:3d60:1230::3", PrefixLen: 64},
	}, 60, map[uint64]bool{ipv6HostSuffix64(netip.MustParseAddr("::3")): true}); ok {
		t.Fatalf("prefix = %s, want no prefix from filtered managed suffix", got)
	}
}

func TestDeprecatedIPv6LocalAddressCommand(t *testing.T) {
	name, args := deprecatedIPv6LocalAddressCommand(platform.OSLinux, "ens19", "2001:db8::1", 299)
	if got, want := strings.Join(append([]string{name}, args...), " "), "ip -6 addr replace 2001:db8::1/64 dev ens19 preferred_lft 0 valid_lft 299"; got != want {
		t.Fatalf("Linux command = %q, want %q", got, want)
	}
	name, args = deprecatedIPv6LocalAddressCommand(platform.OSFreeBSD, "vtnet1", "2001:db8::1", 299)
	if got, want := strings.Join(append([]string{name}, args...), " "), "ifconfig vtnet1 inet6 2001:db8::1 prefixlen 64 pltime 0 vltime 299 alias"; got != want {
		t.Fatalf("FreeBSD command = %q, want %q", got, want)
	}
}

func TestParseFreeBSDIfconfigIPv6(t *testing.T) {
	prefixes, addrs := parseFreeBSDIfconfigIPv6(`vtnet1: flags=1008843<UP,BROADCAST,RUNNING,SIMPLEX,MULTICAST,LOWER_UP> metric 0 mtu 1500
	inet 192.0.2.1 netmask 0xffffff00 broadcast 192.0.2.255
	inet6 fe80::be24:11ff:fea3:c1f4%vtnet1 prefixlen 64 scopeid 0x2
	inet6 2001:db8:3d60:1240:be24:11ff:fea3:c1f4 prefixlen 64
`)
	if len(prefixes) != 1 || prefixes[0] != "2001:db8:3d60:1240::/64" {
		t.Fatalf("prefixes = %v, want delegated /64", prefixes)
	}
	wantAddrs := []string{"fe80::be24:11ff:fea3:c1f4", "2001:db8:3d60:1240:be24:11ff:fea3:c1f4"}
	if fmt.Sprint(addrs) != fmt.Sprint(wantAddrs) {
		t.Fatalf("addrs = %v, want %v", addrs, wantAddrs)
	}
	_, entries := parseFreeBSDIfconfigIPv6Entries(`vtnet1: flags=1008843<UP,BROADCAST,RUNNING,SIMPLEX,MULTICAST,LOWER_UP> metric 0 mtu 1500
	inet6 2001:db8:3d60:1240::1 prefixlen 64
`)
	if len(entries) != 1 || entries[0].Address != "2001:db8:3d60:1240::1" || entries[0].PrefixLen != 64 {
		t.Fatalf("entries = %#v, want address with prefixlen 64", entries)
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

func TestRecordPrefixDelegationStateUsesManagedDaemonLease(t *testing.T) {
	dir := t.TempDir()
	oldDir := pdClientLeaseDir
	pdClientLeaseDir = dir
	t.Cleanup(func() { pdClientLeaseDir = oldDir })
	leaseDir := filepath.Join(dir, "wan-pd")
	if err := os.MkdirAll(leaseDir, 0755); err != nil {
		t.Fatal(err)
	}
	acquiredAt := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	updatedAt := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339)
	data := []byte(fmt.Sprintf(`{
  "resource": "wan-pd",
  "interface": "ens18",
  "state": "bound",
  "currentPrefix": "2001:db8:1230::/60",
  "serverDUID": "00030001020000000001",
  "clientDuid": "00030001020000000103",
  "iaid": 1,
  "t1Seconds": 7200,
  "t2Seconds": 12600,
  "preferredSeconds": 14400,
  "validSeconds": 14400,
  "acquiredAt": %q,
  "updatedAt": %q
}`, acquiredAt, updatedAt))
	if err := os.WriteFile(filepath.Join(leaseDir, "lease.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "ens18"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
			Metadata: api.ObjectMeta{Name: "wan-pd"},
			Spec:     api.DHCPv6PrefixDelegationSpec{Interface: "wan", ClientDUID: "00030001020000000103", IAID: "1"},
		},
	}}}
	store := routerstate.New()
	if _, err := recordObservedPrefixDelegationState(router, store); err != nil {
		t.Fatalf("record PD state: %v", err)
	}
	result := &apply.Result{}
	appendPrefixDelegationStateWarnings(result, router, store)
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	lease, ok := routerstate.PDLeaseFromStore(store, "ipv6PrefixDelegation.wan-pd")
	if !ok {
		t.Fatal("lease missing")
	}
	if lease.CurrentPrefix != "2001:db8:1230::/60" || lease.VLTime != "14400" || lease.LastReplyAt != acquiredAt {
		t.Fatalf("lease = %+v", lease)
	}
}

func TestManagedPDClientSnapshotMatchesDefaultProfileImplicitDUID(t *testing.T) {
	mac := strings.TrimSpace(readFirstString("/sys/class/net/lo/address"))
	duid := linkLayerDUIDFromMAC(mac)
	if duid == "" {
		t.Skip("loopback MAC is unavailable")
	}
	now := time.Now().UTC()
	snapshot := pdclient.Snapshot{Resource: "wan-pd", Interface: "lo", State: pdclient.StateBound, CurrentPrefix: "2001:db8:1200::/60", ClientDUID: duid, IAID: 1, Valid: 60, AcquiredAt: now, ExpiresAt: now.Add(time.Minute)}
	if !managedPDClientSnapshotMatches(snapshot, "wan-pd", "lo", api.DHCPv6PrefixDelegationSpec{Interface: "wan"}, now) {
		t.Fatal("default profile implicit link-layer DUID must match daemon rule")
	}
}

func TestMergePDLeaseSnapshotPersistsMultiplePreviousPrefixes(t *testing.T) {
	now := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	merged := mergePDLeaseSnapshot(routerstate.PDLease{
		LastPrefix:  "2001:db8:1200:1240::/60",
		LastReplyAt: now.Add(-time.Minute).Format(time.RFC3339Nano),
		VLTime:      "7200",
	}, pdclient.Snapshot{
		CurrentPrefix: "2001:db8:1200:1220::/60",
		PreviousPrefixes: []pdclient.PreviousPrefixSnapshot{
			{Prefix: "2001:db8:1200:1230::/60", ExpiresAt: now.Add(time.Hour)},
		},
		AcquiredAt: now,
		Valid:      14400,
		UpdatedAt:  now,
	})
	if got := merged.ValidPreviousPrefixes(now); len(got) != 2 || got[0].Prefix != "2001:db8:1200:1230::/60" || got[1].Prefix != "2001:db8:1200:1240::/60" {
		t.Fatalf("merged previous prefixes = %#v", got)
	}
	merged = mergePDLeaseSnapshot(merged, pdclient.Snapshot{CurrentPrefix: "2001:db8:1200:1220::/60", AcquiredAt: now, Valid: 14400, UpdatedAt: now.Add(time.Minute)})
	if got := merged.ValidPreviousPrefixes(now.Add(time.Minute)); len(got) != 2 {
		t.Fatalf("seeded previous prefixes were not preserved = %#v", got)
	}
}

func TestRecordPrefixDelegationStateClearsIdentityWhenClientChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = store.Close() }()
	if _, err := store.BeginGeneration("test"); err != nil {
		t.Fatalf("begin generation: %v", err)
	}
	store.Set("ipv6PrefixDelegation.wan-pd.client", "networkd", "old client")
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{
		LastPrefix: "2001:db8:3d60:1240::/60",
		DUID:       "00030001020000000001",
		DUIDText:   "00:03:00:01:02:00:00:00:00:01",
		IAID:       "3394439514",
	}), "old lease")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "ens18"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
			Metadata: api.ObjectMeta{Name: "wan-pd"},
			Spec: api.DHCPv6PrefixDelegationSpec{
				Interface: "wan",
				Client:    "dhcpcd",
				Profile:   "ntt-hgw-lan-pd",
			},
		},
	}}}
	if _, err := recordObservedPrefixDelegationState(router, store); err != nil {
		t.Fatalf("record PD state: %v", err)
	}
	lease, ok := routerstate.PDLeaseFromStore(store, "ipv6PrefixDelegation.wan-pd")
	if !ok {
		t.Fatal("lease missing")
	}
	if lease.DUID != "" || lease.DUIDText != "" || lease.IAID != "" {
		t.Fatalf("identity was not cleared: %+v", lease)
	}
	if lease.LastPrefix != "2001:db8:3d60:1240::/60" {
		t.Fatalf("last prefix = %q, want preserved", lease.LastPrefix)
	}
	if got := store.Get("ipv6PrefixDelegation.wan-pd.client").Value; got != "dhcpcd" {
		t.Fatalf("client = %q, want dhcpcd", got)
	}
	events := store.Events(api.NetAPIVersion, "DHCPv6PrefixDelegation", "wan-pd", 10)
	found := false
	for _, event := range events {
		if event.Reason == "PDClientChanged" {
			found = true
		}
	}
	if !found {
		t.Fatalf("PDClientChanged event missing: %+v", events)
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
