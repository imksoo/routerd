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
	"syscall"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/eventlog"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
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

func TestApplyFilesReportsCreatedAndChanged(t *testing.T) {
	dir := t.TempDir()
	netdevPath := filepath.Join(dir, "10-routerd-vxlan100.netdev")
	dropinPath := filepath.Join(dir, "ens18.network.d", "90-routerd.conf")
	netdevData := []byte("[NetDev]\nName=vxlan100\nKind=vxlan\n")
	dropinData := []byte("[Network]\nDHCP=yes\n")

	if err := os.MkdirAll(filepath.Dir(dropinPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dropinPath, dropinData, 0644); err != nil {
		t.Fatalf("seed dropin: %v", err)
	}
	if err := os.Chmod(dropinPath, 0644); err != nil {
		t.Fatalf("chmod dropin: %v", err)
	}

	changed, created, err := applyFiles([]render.File{
		{Path: netdevPath, Data: netdevData},
		{Path: dropinPath, Data: dropinData},
	})
	if err != nil {
		t.Fatalf("applyFiles: %v", err)
	}
	if len(changed) != 1 || changed[0] != netdevPath {
		t.Fatalf("changed = %v, want [%s]", changed, netdevPath)
	}
	if len(created) != 1 || created[0] != netdevPath {
		t.Fatalf("created = %v, want [%s]", created, netdevPath)
	}

	changed, created, err = applyFiles([]render.File{
		{Path: netdevPath, Data: append(netdevData, '\n')},
	})
	if err != nil {
		t.Fatalf("applyFiles second call: %v", err)
	}
	if len(changed) != 1 || changed[0] != netdevPath {
		t.Fatalf("second call changed = %v", changed)
	}
	if len(created) != 0 {
		t.Fatalf("second call created = %v, want none", created)
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

func TestEnsureFreeBSDStrongSwanLoadsIPsecBeforeService(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	logPath := filepath.Join(dir, "calls.log")
	for _, command := range []string{"kldload", "sysrc", "service"} {
		body := fmt.Sprintf("#!/bin/sh\necho %s \"$@\" >> %q\n", command, logPath)
		switch command {
		case "service":
			body += "[ \"$2\" = status ] && exit 0\nexit 1\n"
		default:
			body += "exit 0\n"
		}
		writeExecutable(t, filepath.Join(binDir, command), body)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldDefaults := platformDefaults
	platformDefaults.OS = platform.OSFreeBSD
	t.Cleanup(func() { platformDefaults = oldDefaults })

	if err := ensureFreeBSDStrongSwan(context.Background()); err != nil {
		t.Fatalf("ensure FreeBSD strongSwan: %v", err)
	}
	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	calls := string(got)
	kernel := strings.Index(calls, "kldload -n ipsec")
	sysrc := strings.Index(calls, "sysrc strongswan_enable=YES")
	if kernel < 0 || sysrc < 0 || kernel > sysrc {
		t.Fatalf("kernel module must load before service enablement:\n%s", calls)
	}
}

func TestEnsureFreeBSDStrongSwanFailsClosedWhenIPsecModuleLoadFails(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	sysrcPath := filepath.Join(dir, "sysrc-called")
	writeExecutable(t, filepath.Join(binDir, "kldload"), "#!/bin/sh\necho 'ipsec unavailable' >&2\nexit 23\n")
	writeExecutable(t, filepath.Join(binDir, "sysrc"), fmt.Sprintf("#!/bin/sh\ntouch %q\n", sysrcPath))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldDefaults := platformDefaults
	platformDefaults.OS = platform.OSFreeBSD
	t.Cleanup(func() { platformDefaults = oldDefaults })

	err := ensureFreeBSDStrongSwan(context.Background())
	if err == nil || !strings.Contains(err.Error(), "load FreeBSD IPsec kernel module") || !strings.Contains(err.Error(), "ipsec unavailable") {
		t.Fatalf("module-load failure = %v", err)
	}
	if _, statErr := os.Stat(sysrcPath); !os.IsNotExist(statErr) {
		t.Fatalf("service enablement ran after module failure: %v", statErr)
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

func TestCleanupServeLedgerOwnedOrphansUsesConfiguredLedger(t *testing.T) {
	oldCleanup := cleanupLedgerOwnedOrphansForServe
	t.Cleanup(func() { cleanupLedgerOwnedOrphansForServe = oldCleanup })
	router := &api.Router{Metadata: api.ObjectMeta{Name: "test-router"}}
	var gotRouter *api.Router
	var gotLedgerPath string
	cleanupLedgerOwnedOrphansForServe = func(r *api.Router, ledgerPath string) ([]string, error) {
		gotRouter = r
		gotLedgerPath = ledgerPath
		return []string{"systemd.service/routerd-old.service"}, nil
	}
	removed, err := cleanupServeLedgerOwnedOrphans(router, "/var/lib/routerd/ledger.db", nil)
	if err != nil {
		t.Fatalf("cleanup serve ledger owned orphans: %v", err)
	}
	if gotRouter != router || gotLedgerPath != "/var/lib/routerd/ledger.db" {
		t.Fatalf("cleanup called with router=%p path=%q", gotRouter, gotLedgerPath)
	}
	if !reflect.DeepEqual(removed, []string{"systemd.service/routerd-old.service"}) {
		t.Fatalf("removed = %#v", removed)
	}
}

func TestCleanupServeLedgerOwnedOrphansReturnsErrorWithoutFailingCaller(t *testing.T) {
	oldCleanup := cleanupLedgerOwnedOrphansForServe
	t.Cleanup(func() { cleanupLedgerOwnedOrphansForServe = oldCleanup })
	wantErr := errors.New("ledger locked")
	cleanupLedgerOwnedOrphansForServe = func(*api.Router, string) ([]string, error) {
		return nil, wantErr
	}
	removed, err := cleanupServeLedgerOwnedOrphans(&api.Router{}, "/var/lib/routerd/ledger.db", nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if removed != nil {
		t.Fatalf("removed = %#v, want nil", removed)
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

func TestLoadTransientStateStoreOpensExistingSQLiteReadOnly(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	store.Set("manual.mode", "keep", "seed")
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	t.Cleanup(func() {
		_ = os.Chmod(dir, 0755)
		_ = os.Chmod(statePath, 0644)
	})
	if err := os.Chmod(statePath, 0444); err != nil {
		t.Fatalf("chmod state: %v", err)
	}
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("chmod state dir: %v", err)
	}

	transient, err := loadTransientStateStore(statePath)
	if err != nil {
		t.Fatalf("load transient read-only sqlite: %v", err)
	}
	if _, ok := transient.(*routerstate.JSONStore); !ok {
		t.Fatalf("transient store type = %T, want JSON snapshot", transient)
	}
	if got := transient.Get("manual.mode"); got.Status != routerstate.StatusSet || got.Value != "keep" {
		t.Fatalf("snapshot manual.mode = %+v, want keep", got)
	}
}

func TestConfigCommandPlanObserveAcceptJSONState(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	statePath := filepath.Join(dir, "state.json")
	if err := os.WriteFile(configPath, []byte(testRouterYAML("json-state-router")), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	store := routerstate.New()
	store.Set("manual.mode", "keep", "seed")
	if err := store.Save(statePath); err != nil {
		t.Fatalf("save json state: %v", err)
	}

	for _, name := range []string{"plan", "observe"} {
		t.Run(name, func(t *testing.T) {
			var stdout strings.Builder
			statusPath := filepath.Join(dir, name+"-status.json")
			err := configCommand([]string{
				"--config", configPath,
				"--state-file", statePath,
				"--status-file", statusPath,
			}, &stdout, name)
			if err != nil {
				t.Fatalf("%s with json state: %v", name, err)
			}
			if stdout.String() == "" {
				t.Fatalf("%s produced empty output", name)
			}
			if _, err := os.Stat(statusPath); err != nil {
				t.Fatalf("%s status file: %v", name, err)
			}
		})
	}
}

func TestRunApplyChainOnceCommitsCanonicalConfigAndGenerationYAML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	statePath := filepath.Join(dir, "routerd.db")
	statusPath := filepath.Join(dir, "status.json")
	ledgerPath := filepath.Join(dir, "ledger.db")
	input := `# keep operator comment
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  # keep name comment
  name: canonical-apply
spec:
  resources: []
`
	if err := os.WriteFile(configPath, []byte(input), 0640); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Chmod(configPath, 0640); err != nil {
		t.Fatalf("chmod config: %v", err)
	}
	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	_, err = runApplyChainOnce(context.Background(), router, applyOptions{
		ConfigPath:         configPath,
		StatePath:          statePath,
		StatusFile:         statusPath,
		LedgerPath:         ledgerPath,
		SkipServiceManager: true,
	}, io.Discard, &eventlog.Logger{})
	if err != nil {
		t.Fatalf("apply once: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read committed config: %v", err)
	}
	for _, want := range []string{"# keep operator comment", "# keep name comment"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("committed config lost %q:\n%s", want, data)
		}
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat committed config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0640 {
		t.Fatalf("committed config mode = %v, want 0640", got)
	}

	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	defer func() { _ = store.Close() }()
	generation := store.LatestGeneration()
	configYAML, ok, err := store.GenerationConfig(generation)
	if err != nil {
		t.Fatalf("generation config: %v", err)
	}
	if !ok {
		t.Fatalf("generation %d has no config yaml", generation)
	}
	if !strings.Contains(configYAML, "# keep operator comment") || !strings.Contains(configYAML, "# keep name comment") {
		t.Fatalf("generation config lost comments:\n%s", configYAML)
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

func TestServeConfigMutatorApplyReplaceCommitsCanonicalAndGeneration(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	statePath := filepath.Join(dir, "routerd.db")
	statusPath := filepath.Join(dir, "status.json")
	ledgerPath := filepath.Join(dir, "ledger.db")
	if err := os.WriteFile(configPath, []byte(testRouterYAML("old-router")), 0644); err != nil {
		t.Fatalf("write canonical config: %v", err)
	}
	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load router: %v", err)
	}
	mutator := serveConfigMutator{
		configPath: configPath,
		statePath:  statePath,
		baseOpts: applyOptions{
			ConfigPath:         configPath,
			StatePath:          statePath,
			StatusFile:         statusPath,
			LedgerPath:         ledgerPath,
			SkipServiceManager: true,
		},
		cache:     &resultCache{},
		logger:    &eventlog.Logger{},
		getRouter: func() *api.Router { return router },
		setRouter: func(next *api.Router) { router = next },
	}
	candidate := `# replacement comment
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: new-router
spec:
  resources: []
`
	result, err := mutator.apply(nil, controlapi.ApplyRequest{CandidateYAML: candidate, Replace: true})
	if err != nil {
		t.Fatalf("mutating apply: %v", err)
	}
	if result.Result.Phase != "Healthy" || result.Result.Generation == 0 {
		t.Fatalf("result = %+v", result.Result)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read canonical config: %v", err)
	}
	if !strings.Contains(string(data), "name: new-router") || !strings.Contains(string(data), "# replacement comment") {
		t.Fatalf("canonical config was not replaced with candidate:\n%s", data)
	}
	if router.Metadata.Name != "new-router" {
		t.Fatalf("in-memory router = %q, want new-router", router.Metadata.Name)
	}
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	defer func() { _ = store.Close() }()
	configYAML, ok, err := store.GenerationConfig(result.Result.Generation)
	if err != nil {
		t.Fatalf("generation config: %v", err)
	}
	if !ok || !strings.Contains(configYAML, "name: new-router") {
		t.Fatalf("generation config = ok %t:\n%s", ok, configYAML)
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

func TestServeConfigMutatorDeleteNoReconcileUpdatesCanonical(t *testing.T) {
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
	if len(router.Spec.Resources) != 0 {
		t.Fatalf("in-memory resources = %d, want 0", len(router.Spec.Resources))
	}
}

func TestServeConfigMutatorSandboxApplyCommitsCanonicalAndDryRuns(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	statePath := filepath.Join(dir, "routerd.db")
	if err := os.WriteFile(configPath, []byte(testRouterYAML("stable-router")), 0644); err != nil {
		t.Fatalf("write canonical config: %v", err)
	}
	router, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load router: %v", err)
	}
	mutator := serveConfigMutator{
		configPath: configPath,
		statePath:  statePath,
		baseOpts: applyOptions{
			ConfigPath:         configPath,
			StatePath:          statePath,
			LedgerPath:         filepath.Join(dir, "ledger.db"),
			SkipServiceManager: true,
			Sandbox:            true,
		},
		cache:     &resultCache{},
		logger:    &eventlog.Logger{},
		getRouter: func() *api.Router { return router },
		setRouter: func(next *api.Router) { router = next },
	}
	result, err := mutator.apply(nil, controlapi.ApplyRequest{
		CandidateYAML: testRouterYAML("sandbox-router"),
		Replace:       true,
	})
	if err != nil {
		t.Fatalf("sandbox apply: %v", err)
	}
	if result.Result.Generation == 0 || result.Result.Phase != "Healthy" {
		t.Fatalf("sandbox apply result = %+v", result.Result)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read canonical config: %v", err)
	}
	if !strings.Contains(string(data), "name: sandbox-router") {
		t.Fatalf("sandbox canonical was not updated:\n%s", data)
	}
	if router.Metadata.Name != "sandbox-router" {
		t.Fatalf("in-memory router name = %q, want sandbox-router", router.Metadata.Name)
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

func TestSandboxControllerOptionsRoutePathMTUArtifactsIntoRuntimeDir(t *testing.T) {
	oldDefaults := platformDefaults
	dir := t.TempDir()
	platformDefaults.RuntimeDir = filepath.Join(dir, "run", "routerd")
	t.Cleanup(func() { platformDefaults = oldDefaults })

	var opts controllerchain.Options
	applySandboxControllerOptions(&opts, "", "")

	if got, want := opts.PathMTUPath, filepath.Join(platformDefaults.RuntimeDir, "mss.nft"); got != want {
		t.Fatalf("PathMTUPath = %q, want %q", got, want)
	}
	if got, want := opts.ForceFragmentPath, filepath.Join(platformDefaults.RuntimeDir, "forcefrag.nft"); got != want {
		t.Fatalf("ForceFragmentPath = %q, want %q", got, want)
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
	output := stdout.String()
	for _, text := range []string{"generation", "started_at", "finished_at", "phase", "config", fmt.Sprintf("%d", gen1), fmt.Sprintf("%d", gen2), "yes", "(current)"} {
		if !strings.Contains(output, text) {
			t.Fatalf("rollback list missing %q:\n%s", text, output)
		}
	}
	if strings.Index(output, fmt.Sprintf("%d", gen2)) > strings.Index(output, fmt.Sprintf("%d", gen1)) {
		t.Fatalf("generations are not newest-first:\n%s", output)
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
	gen := seedGeneration(t, store, "hash-1", testRouterYAML("rollback-dry-run"), true, "Applied")
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
	if got := store.LatestGeneration(); got != gen {
		t.Fatalf("latest generation after dry-run = %d, want %d", got, gen)
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

func TestHasNewNetdevFiles(t *testing.T) {
	if !hasNewNetdevFiles([]string{"/etc/systemd/network/10-vxlan.netdev"}) {
		t.Fatal("expected new .netdev to be detected")
	}
	if hasNewNetdevFiles([]string{"/etc/systemd/network/10-vxlan.network"}) {
		t.Fatal("plain .network should not trigger new-netdev path")
	}
	if hasNewNetdevFiles(nil) {
		t.Fatal("nil should not trigger")
	}
}

func TestApplyNetworkConfigSkipsUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	netplanPath := filepath.Join(dir, "netplan", "90-routerd.yaml")
	dropinPath := filepath.Join(dir, "systemd", "10-netplan-ens18.network.d", "90-routerd-dhcpv6-pd.conf")
	netplanData := []byte("network:\n  version: 2\n")
	dropinData := []byte("[Network]\nDHCP=yes\n")

	if err := os.MkdirAll(filepath.Dir(netplanPath), 0755); err != nil {
		t.Fatalf("create netplan dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dropinPath), 0755); err != nil {
		t.Fatalf("create dropin dir: %v", err)
	}
	if err := os.WriteFile(netplanPath, netplanData, 0600); err != nil {
		t.Fatalf("write netplan fixture: %v", err)
	}
	if err := os.WriteFile(dropinPath, dropinData, 0644); err != nil {
		t.Fatalf("write dropin fixture: %v", err)
	}
	if err := os.Chmod(dropinPath, 0644); err != nil {
		t.Fatalf("chmod dropin fixture: %v", err)
	}

	changed, err := applyNetworkConfig(netplanPath, netplanData, []render.File{
		{Path: dropinPath, Data: dropinData},
	})
	if err != nil {
		t.Fatalf("apply network config: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed files = %v, want none", changed)
	}
}

func TestDeleteCommandRemovesStateAndLedgerForResource(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	ledgerPath := filepath.Join(dir, "artifacts.json")
	store := routerstate.New()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{LastPrefix: "2001:db8::/60"}), "test")
	if err := store.Save(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}
	ledger := resource.NewLedger()
	ledger.Remember([]resource.Artifact{{
		Kind:  "file",
		Name:  "/tmp/routerd-test",
		Owner: "net.routerd.net/v1alpha1/DHCPv6PrefixDelegation/wan-pd",
	}})
	if err := ledger.Save(ledgerPath); err != nil {
		t.Fatalf("save ledger: %v", err)
	}

	var out strings.Builder
	if err := deleteCommand([]string{"--state-file", statePath, "--ledger-file", ledgerPath, "pd/wan-pd"}, &out); err != nil {
		t.Fatalf("delete command: %v", err)
	}
	if !strings.Contains(out.String(), "delete net.routerd.net/v1alpha1/DHCPv6PrefixDelegation/wan-pd") {
		t.Fatalf("delete output = %s", out.String())
	}
	loadedState, err := routerstate.Load(statePath)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if got := loadedState.Get("ipv6PrefixDelegation.wan-pd.lease"); got.Status != routerstate.StatusUnknown {
		t.Fatalf("state after delete = %+v, want unknown", got)
	}
	loadedLedger, err := resource.LoadLedger(ledgerPath)
	if err != nil {
		t.Fatalf("load ledger: %v", err)
	}
	defer func() { _ = loadedLedger.Close() }()
	if len(loadedLedger.All()) != 0 {
		t.Fatalf("ledger after delete = %+v, want empty", loadedLedger.All())
	}
}

func TestDeleteCommandForceRemovesStaleUnsupportedKindState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "routerd.db")
	ledgerPath := filepath.Join(dir, "artifacts.json")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open sqlite state: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "PPPoEInterface", "wan", map[string]any{"phase": "Applied"}); err != nil {
		t.Fatalf("save stale object status: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite state: %v", err)
	}
	if err := resource.NewLedger().Save(ledgerPath); err != nil {
		t.Fatalf("save ledger: %v", err)
	}

	var out strings.Builder
	if err := deleteCommand([]string{"--state-file", statePath, "--ledger-file", ledgerPath, "--force", "PPPoEInterface/wan"}, &out); err != nil {
		t.Fatalf("delete command: %v", err)
	}
	if !strings.Contains(out.String(), "delete net.routerd.net/v1alpha1/PPPoEInterface/wan") {
		t.Fatalf("delete output = %s", out.String())
	}
	store, err = routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("reopen sqlite state: %v", err)
	}
	defer store.Close()
	if status := store.ObjectStatus(api.NetAPIVersion, "PPPoEInterface", "wan"); len(status) != 0 {
		t.Fatalf("stale status after delete = %+v, want none", status)
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

func TestCleanupUnsupportedLegacyObjectStatusesKeepsWhenFalseStatus(t *testing.T) {
	now := time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC)
	store := &fakeStaleCleanupStore{
		now: now,
		values: map[string]routerstate.Value{
			"gate": {Status: routerstate.StatusSet, Value: "no", Since: now, UpdatedAt: now},
		},
		statuses: []routerstate.ObjectStatus{
			{APIVersion: api.NetAPIVersion, Kind: "BGPPeer", Name: "fabric", Status: map[string]any{"phase": "Pending", "reason": "WhenFalse"}},
			{APIVersion: api.NetAPIVersion, Kind: "TailscaleNode", Name: "old", Status: map[string]any{"phase": "Error"}},
		},
	}
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test-router"},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: "fabric"},
			Spec: api.BGPPeerSpec{
				RouterRef: "core",
				PeerASN:   64512,
				Peers:     []string{"192.0.2.1"},
				When:      api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"gate": {Equals: "yes"}}},
			},
		}}},
	}

	result, err := cleanupUnsupportedLegacyObjectStatuses(router, store, filepath.Join(t.TempDir(), "routerd.db"), now, nil)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if result.Skipped || len(result.Removed) != 1 || result.Removed[0].Kind != "TailscaleNode" {
		t.Fatalf("cleanup result = %+v, want only stale TailscaleNode removed", result)
	}
	if got := strings.Join(store.deleted, ","); got != api.NetAPIVersion+"/TailscaleNode/old" {
		t.Fatalf("deleted = %s, want only stale TailscaleNode", got)
	}
}

func TestCleanupUnsupportedLegacyObjectStatusesDefersOwnedTunnelInterfaceTeardown(t *testing.T) {
	for _, targetOS := range []platform.OS{platform.OSLinux, platform.OSFreeBSD} {
		t.Run(string(targetOS), func(t *testing.T) {
			store := &fakeStaleCleanupStore{statuses: []routerstate.ObjectStatus{
				{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface", Name: "old-gif", Status: map[string]any{"interfaceOwned": true, "managedBy": "routerd"}},
				{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface", Name: "foreign-gif", Status: map[string]any{"interfaceOwned": false}},
				{APIVersion: api.NetAPIVersion, Kind: "TailscaleNode", Name: "old", Status: map[string]any{"phase": "Error"}},
			}}
			result, err := cleanupUnsupportedLegacyObjectStatusesForOS(&api.Router{}, store, filepath.Join(t.TempDir(), "routerd.db"), time.Now().UTC(), nil, targetOS)
			if err != nil {
				t.Fatalf("cleanup: %v", err)
			}
			if result.Skipped || len(result.Removed) != 2 {
				t.Fatalf("cleanup result = %+v, want foreign TunnelInterface and TailscaleNode only", result)
			}
			if got := strings.Join(store.deleted, ","); got != api.HybridAPIVersion+"/TunnelInterface/foreign-gif,"+api.NetAPIVersion+"/TailscaleNode/old" {
				t.Fatalf("deleted = %s, want only non-owned stale rows", got)
			}
		})
	}
}

func TestServePreGCLeavesOwnedTunnelInterfaceForControllerTeardown(t *testing.T) {
	for _, targetOS := range []platform.OS{platform.OSLinux, platform.OSFreeBSD} {
		t.Run(string(targetOS), func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "routerd.db")
			store, err := routerstate.OpenSQLite(statePath)
			if err != nil {
				t.Fatalf("open state: %v", err)
			}
			defer store.Close()
			if err := store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", "old", map[string]any{
				"managedBy": "routerd", "interfaceOwned": true, "ifname": "old-tun",
			}); err != nil {
				t.Fatalf("save owned tunnel status: %v", err)
			}
			if _, err := cleanupUnsupportedLegacyObjectStatusesForOS(&api.Router{}, store, statePath, time.Now().UTC(), nil, targetOS); err != nil {
				t.Fatalf("pre-controller cleanup: %v", err)
			}
			if status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "old"); !status["interfaceOwned"].(bool) {
				t.Fatalf("owned tunnel status removed before controller: %#v", status)
			}
			var calls [][]string
			controller := controllerchain.TunnelInterfaceController{
				Router: &api.Router{}, Store: store, OS: targetOS,
				Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
					calls = append(calls, append([]string{name}, args...))
					return nil, nil
				},
			}
			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatalf("controller teardown: %v", err)
			}
			want := []string{"ip", "link", "del", "dev", "old-tun"}
			if targetOS == platform.OSFreeBSD {
				want = []string{"ifconfig", "old-tun", "destroy"}
			}
			if len(calls) != 1 || !reflect.DeepEqual(calls[0], want) {
				t.Fatalf("calls = %#v, want %#v", calls, want)
			}
			if status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "old"); len(status) != 0 {
				t.Fatalf("status after controller teardown = %#v, want none", status)
			}
		})
	}
}

func TestCleanupUnsupportedLegacyObjectStatusesUsesDynamicEffectiveView(t *testing.T) {
	now := time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC)
	owner := []api.OwnerRef{{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile", Name: "fabric"}}
	dynamicTunnel := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "sam-core-a", OwnerRefs: owner},
		Spec: api.TunnelInterfaceSpec{
			Mode:            "ipip",
			Local:           "10.99.0.1",
			Remote:          "10.99.0.2",
			Address:         "10.255.0.0/31",
			TrustedUnderlay: true,
		},
	}
	store := &fakeStaleCleanupStore{
		now:   now,
		parts: []routerstate.DynamicConfigPartRecord{dynamicConfigPartRecordForCleanupTest(t, "SAMTransportProfile/fabric/node/core-a", []api.Resource{dynamicTunnel}, now.Add(time.Hour))},
		statuses: []routerstate.ObjectStatus{
			{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface", Name: "sam-core-a", Status: map[string]any{"phase": "Applied"}},
			{APIVersion: api.NetAPIVersion, Kind: "TailscaleNode", Name: "old", Status: map[string]any{"phase": "Error"}},
		},
	}
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test-router"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
				Metadata: api.ObjectMeta{Name: "core"},
				Spec:     api.BGPRouterSpec{ASN: 64512, RouterID: "10.255.0.1"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wg-hybrid"},
				Spec:     api.InterfaceSpec{IfName: "wg-hybrid"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile"},
				Metadata: api.ObjectMeta{Name: "fabric"},
				Spec: api.SAMTransportProfileSpec{
					SelfNodeRef:       "core-a",
					Mode:              "ipip",
					Encryption:        "wireguard",
					InnerPrefix:       "10.255.0.0/24",
					TopologyNodeRefs:  []string{"core-a", "core-b"},
					UnderlayInterface: "wg-hybrid",
					LocalEndpoint:     "10.99.0.1",
					BGP:               api.SAMTransportBGPProfileSpec{RouterRef: "BGPRouter/core", PeerASN: 64512},
					Peers:             []api.SAMTransportPeerSpec{{NodeRef: "core-b", RemoteEndpoint: "10.99.0.2"}},
				},
			},
		}},
	}

	result, err := cleanupUnsupportedLegacyObjectStatusesForOS(router, store, filepath.Join(t.TempDir(), "routerd.db"), now, nil, platform.OSLinux)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if result.Skipped || len(result.Removed) != 1 || result.Removed[0].Kind != "TailscaleNode" {
		t.Fatalf("cleanup result = %+v, want dynamic TunnelInterface preserved and stale TailscaleNode removed", result)
	}
	if got := strings.Join(store.deleted, ","); got != api.NetAPIVersion+"/TailscaleNode/old" {
		t.Fatalf("deleted = %s, want only stale TailscaleNode", got)
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

func TestCleanupUnsupportedLegacyObjectStatusesSkipsWhenSnapshotFails(t *testing.T) {
	dir := t.TempDir()
	notDir := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(notDir, []byte("not a dir"), 0644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	store := &fakeStaleCleanupStore{
		statuses: []routerstate.ObjectStatus{
			{APIVersion: api.NetAPIVersion, Kind: "PPPoEInterface", Name: "wan", Status: map[string]any{"phase": "Applied"}},
			{APIVersion: api.NetAPIVersion, Kind: "Interface", Name: "wan", Status: map[string]any{"phase": "Applied"}},
		},
	}
	router := &api.Router{
		Metadata: api.ObjectMeta{Name: "test-router"},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "ens18"},
		}}},
	}

	result, err := cleanupUnsupportedLegacyObjectStatuses(router, store, filepath.Join(notDir, "routerd.db"), time.Now(), nil)
	if err != nil {
		t.Fatalf("cleanup returned error: %v", err)
	}
	if !result.Skipped || len(result.Removed) != 1 {
		t.Fatalf("cleanup result = %+v, want skipped with one candidate", result)
	}
	if len(store.deleted) != 0 {
		t.Fatalf("deleted = %v, want none when snapshot fails", store.deleted)
	}
	if len(store.events) != 1 || store.events[0].Reason != "StaleStateCleanupSkipped" {
		t.Fatalf("events = %+v, want skipped audit event", store.events)
	}
}

func TestDeleteCommandFileTargetsRouterResources(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "router.yaml")
	if err := os.WriteFile(configPath, []byte(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifName: ens18
`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	targets, err := deleteTargets(nil, configPath, "", false, "")
	if err != nil {
		t.Fatalf("delete targets: %v", err)
	}
	if len(targets) != 1 || targets[0].Kind != "Interface" || targets[0].Name != "wan" {
		t.Fatalf("targets = %+v", targets)
	}
}

type fakeStaleCleanupStore struct {
	statuses []routerstate.ObjectStatus
	deleted  []string
	events   []routerstate.Event
	values   map[string]routerstate.Value
	now      time.Time
	parts    []routerstate.DynamicConfigPartRecord
}

func (s *fakeStaleCleanupStore) ListObjectStatuses() ([]routerstate.ObjectStatus, error) {
	return append([]routerstate.ObjectStatus(nil), s.statuses...), nil
}

func (s *fakeStaleCleanupStore) DeleteObject(apiVersion, kind, name string) error {
	s.deleted = append(s.deleted, apiVersion+"/"+kind+"/"+name)
	return nil
}

func (s *fakeStaleCleanupStore) RecordEvent(apiVersion, kind, name, eventType, reason, message string) error {
	s.events = append(s.events, routerstate.Event{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Type:       eventType,
		Reason:     reason,
		Message:    message,
	})
	return nil
}

func (s *fakeStaleCleanupStore) Events(apiVersion, kind, name string, limit int) []routerstate.Event {
	var out []routerstate.Event
	for _, event := range s.events {
		if event.APIVersion == apiVersion && event.Kind == kind && event.Name == name {
			out = append(out, event)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *fakeStaleCleanupStore) Get(name string) routerstate.Value {
	if s.values != nil {
		if value, ok := s.values[name]; ok {
			return value
		}
	}
	now := s.Now()
	return routerstate.Value{Status: routerstate.StatusUnknown, Since: now, UpdatedAt: now}
}

func (s *fakeStaleCleanupStore) Age(string) time.Duration {
	return 0
}

func (s *fakeStaleCleanupStore) Now() time.Time {
	if !s.now.IsZero() {
		return s.now
	}
	return time.Now().UTC()
}

func (s *fakeStaleCleanupStore) ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error) {
	return append([]routerstate.DynamicConfigPartRecord(nil), s.parts...), nil
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

func TestApplyFreeBSDConfigDoesNotReclaimStaleSysrcKeys(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	stateLog := filepath.Join(dir, "sysrc-calls.log")
	writeExecutable(t, filepath.Join(binDir, "sysrc"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
case "$1" in
  -x) exit 0 ;;
  *) echo "$1: NO"; exit 0 ;;
esac
`, stateLog))
	writeExecutable(t, filepath.Join(binDir, "service"), `#!/bin/sh
if [ "$1" = "dhcp6c" ] && [ "$2" = "status" ]; then exit 0; fi
exit 0
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "vtnet0", Managed: false, Owner: "external"}},
	}}}

	store := routerstate.New()
	store.Set(freebsdSysrcStateKey, "ifconfig_vxlan102,ifconfig_vxlan103,gateway_enable", "test seed")

	_, _, err := applyFreeBSDConfigWithOptions(router, store, "", "", "", "", freeBSDConfigApplyOptions{ManageServices: true})
	if err != nil {
		t.Fatalf("apply FreeBSD config: %v", err)
	}

	got, err := os.ReadFile(stateLog)
	if err != nil {
		t.Fatalf("read sysrc log: %v", err)
	}
	if strings.Contains(string(got), "-x ") {
		t.Fatalf("FreeBSD apply must not remove sysrc keys implicitly:\n%s", got)
	}
}

func TestApplyFreeBSDConfigAppliesPFAndRCDScripts(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	sysrcLog := filepath.Join(dir, "sysrc.log")
	serviceLog := filepath.Join(dir, "service.log")
	pfctlLog := filepath.Join(dir, "pfctl.log")
	writeExecutable(t, filepath.Join(binDir, "sysrc"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
case "$1" in
  -x) exit 0 ;;
  *) echo "$1: NO"; exit 0 ;;
esac
`, sysrcLog))
	writeExecutable(t, filepath.Join(binDir, "service"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
if [ "$1" = "-l" ]; then
  printf 'pf\npflog\nrouterd_healthcheck_internet\n'
  exit 0
fi
if [ "$2" = "status" ]; then
  exit 1
fi
exit 0
`, serviceLog))
	writeExecutable(t, filepath.Join(binDir, "pfctl"), fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
if [ "$1" = "-s" ]; then
  echo "Status: Disabled"
fi
exit 0
`, pfctlLog))
	writeExecutable(t, filepath.Join(binDir, "kldstat"), `#!/bin/sh
exit 1
`)
	writeExecutable(t, filepath.Join(binDir, "kldload"), `#!/bin/sh
exit 0
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "vtnet0", Managed: false, Owner: "external"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
			Metadata: api.ObjectMeta{Name: "internet"},
			Spec:     api.HealthCheckSpec{Daemon: "routerd-healthcheck", Target: "1.1.1.1", Protocol: "icmp"},
		},
	}}}

	pfPath := filepath.Join(dir, "etc", "pf.conf")
	rcDir := filepath.Join(dir, "rc.d")
	changed, warnings, err := applyFreeBSDConfigWithOptions(router, routerstate.New(), "", "", pfPath, rcDir, freeBSDConfigApplyOptions{
		ManageServices: true,
		PFEnabled:      func() bool { return false },
	})
	if err != nil {
		t.Fatalf("apply FreeBSD config: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	for _, path := range []string{pfPath, filepath.Join(rcDir, "routerd_healthcheck_internet")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to be written: %v", path, err)
		}
	}
	pfctlCalls, err := os.ReadFile(pfctlLog)
	if err != nil {
		t.Fatalf("read pfctl log: %v", err)
	}
	for _, want := range []string{"-nf " + pfPath, "-f " + pfPath} {
		if !strings.Contains(string(pfctlCalls), want) {
			t.Fatalf("pfctl calls missing %q:\n%s", want, pfctlCalls)
		}
	}
	if !strings.Contains(string(pfctlCalls), "-e") {
		t.Fatalf("pfctl calls missing enable:\n%s", pfctlCalls)
	}
	serviceCalls, err := os.ReadFile(serviceLog)
	if err != nil {
		t.Fatalf("read service log: %v", err)
	}
	for _, want := range []string{"pf onestart", "pflog onestart", "routerd_healthcheck_internet onestart"} {
		if !strings.Contains(string(serviceCalls), want) {
			t.Fatalf("service calls missing %q:\n%s", want, serviceCalls)
		}
	}
	gotChanged := strings.Join(changed, "\n")
	for _, want := range []string{pfPath, filepath.Join(rcDir, "routerd_healthcheck_internet"), "service:pf", "service:pflog", "service:routerd_healthcheck_internet"} {
		if !strings.Contains(gotChanged, want) {
			t.Fatalf("changed missing %q:\n%v", want, changed)
		}
	}
}

func TestApplyFreeBSDPFConfigSkipServiceManagerDoesNotStartServices(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	serviceLog := filepath.Join(dir, "service.log")
	pfctlLog := filepath.Join(dir, "pfctl.log")
	writeExecutable(t, filepath.Join(binDir, "service"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
exit 1
`, serviceLog))
	writeExecutable(t, filepath.Join(binDir, "pfctl"), fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
if [ "$1" = "-s" ]; then
  echo "Status: Disabled"
fi
exit 0
`, pfctlLog))
	writeExecutable(t, filepath.Join(binDir, "kldstat"), `#!/bin/sh
exit 1
`)
	writeExecutable(t, filepath.Join(binDir, "kldload"), `#!/bin/sh
exit 0
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	pfPath := filepath.Join(dir, "etc", "pf.conf")
	changed, err := applyFreeBSDPFConfigWithOptions([]byte("pass\n"), pfPath, freeBSDPFApplyOptions{
		ManageServices: false,
		PFEnabled:      func() bool { return false },
	})
	if err != nil {
		t.Fatalf("apply FreeBSD pf config: %v", err)
	}
	pfctlCalls, err := os.ReadFile(pfctlLog)
	if err != nil {
		t.Fatalf("read pfctl log: %v", err)
	}
	for _, want := range []string{"-nf " + pfPath, "-f " + pfPath, "-e"} {
		if !strings.Contains(string(pfctlCalls), want) {
			t.Fatalf("pfctl calls missing %q:\n%s", want, pfctlCalls)
		}
	}
	if serviceCalls, err := os.ReadFile(serviceLog); err == nil && len(serviceCalls) > 0 {
		t.Fatalf("service must not be called when service management is skipped:\n%s", serviceCalls)
	}
	gotChanged := strings.Join(changed, "\n")
	for _, want := range []string{pfPath, "pfctl:-e"} {
		if !strings.Contains(gotChanged, want) {
			t.Fatalf("changed missing %q:\n%v", want, changed)
		}
	}
	if strings.Contains(gotChanged, "service:") {
		t.Fatalf("changed contains service operation despite skipped service manager:\n%v", changed)
	}
}

func TestApplyFreeBSDConfigContinuesAfterNTPStartFailure(t *testing.T) {
	dir := t.TempDir()
	oldDefaults := platformDefaults
	platformDefaults.SysconfDir = filepath.Join(dir, "usr", "local", "etc", "routerd")
	t.Cleanup(func() { platformDefaults = oldDefaults })
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	serviceLog := filepath.Join(dir, "service.log")
	writeExecutable(t, filepath.Join(binDir, "sysrc"), `#!/bin/sh
case "$1" in
  -x) exit 0 ;;
  *) echo "$1: NO"; exit 0 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "service"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
if [ "$1" = "-l" ]; then
  printf 'ntpd\nrouterd_healthcheck_internet\n'
  exit 0
fi
if [ "$1" = "ntpd" ] && [ "$2" = "start" ]; then
  echo "ntpd failed to start" >&2
  exit 1
fi
if [ "$2" = "status" ]; then
  exit 1
fi
exit 0
`, serviceLog))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPClient"},
			Metadata: api.ObjectMeta{Name: "time"},
			Spec: api.NTPClientSpec{
				Provider: "ntpd",
				Managed:  true,
				Source:   "static",
				Servers:  []string{"ntp.example.net"},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
			Metadata: api.ObjectMeta{Name: "internet"},
			Spec:     api.HealthCheckSpec{Daemon: "routerd-healthcheck", Target: "1.1.1.1", Protocol: "icmp"},
		},
	}}}

	rcDir := filepath.Join(dir, "rc.d")
	changed, warnings, err := applyFreeBSDConfigWithOptions(router, routerstate.New(), "", "", "", rcDir, freeBSDConfigApplyOptions{ManageServices: true})
	if err != nil {
		t.Fatalf("apply FreeBSD config should continue after ntpd failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rcDir, "routerd_healthcheck_internet")); err != nil {
		t.Fatalf("expected rc.d script to be written after ntpd warning: %v", err)
	}
	if !stringSliceContainsPrefix(warnings, "service ntpd start:") {
		t.Fatalf("warnings = %v, want ntpd warning", warnings)
	}
	if !stringSliceContains(changed, filepath.Join(rcDir, "routerd_healthcheck_internet")) {
		t.Fatalf("changed = %v, want rc.d script path", changed)
	}
}

func TestApplyFreeBSDConfigSkipServiceManagerDoesNotStartServices(t *testing.T) {
	dir := t.TempDir()
	oldDefaults := platformDefaults
	platformDefaults.SysconfDir = filepath.Join(dir, "usr", "local", "etc", "routerd")
	t.Cleanup(func() { platformDefaults = oldDefaults })

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	serviceLog := filepath.Join(dir, "service.log")
	writeExecutable(t, filepath.Join(binDir, "sysrc"), `#!/bin/sh
case "$1" in
  -x) exit 0 ;;
  *) echo "$1: NO"; exit 0 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "service"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
if [ "$1" = "-l" ]; then
  printf 'ntpd\nrouterd_healthcheck_internet\n'
  exit 0
fi
exit 1
`, serviceLog))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPClient"},
			Metadata: api.ObjectMeta{Name: "time"},
			Spec: api.NTPClientSpec{
				Provider: "ntpd",
				Managed:  true,
				Source:   "static",
				Servers:  []string{"ntp.example.net"},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
			Metadata: api.ObjectMeta{Name: "internet"},
			Spec:     api.HealthCheckSpec{Daemon: "routerd-healthcheck", Target: "1.1.1.1", Protocol: "icmp"},
		},
	}}}

	rcDir := filepath.Join(dir, "rc.d")
	changed, warnings, err := applyFreeBSDConfigWithOptions(router, routerstate.New(), "", "", "", rcDir, freeBSDConfigApplyOptions{ManageServices: false})
	if err != nil {
		t.Fatalf("apply FreeBSD config: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if _, err := os.Stat(filepath.Join(rcDir, "routerd_healthcheck_internet")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rc.d script should not be written when service management is skipped: %v", err)
	}
	if serviceCalls, err := os.ReadFile(serviceLog); err == nil && len(serviceCalls) > 0 {
		t.Fatalf("service must not be called when service management is skipped:\n%s", serviceCalls)
	}
	if !stringSliceContainsPrefix(changed, "sysrc:ntpd_") {
		t.Fatalf("changed = %v, want ntpd sysrc keys to remain persisted", changed)
	}
}

func TestApplyFreeBSDRCDScriptsDoesNotRestartRouterdFromApply(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	serviceLog := filepath.Join(dir, "service.log")
	writeExecutable(t, filepath.Join(binDir, "service"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
if [ "$2" = "status" ]; then
  exit 0
fi
exit 0
`, serviceLog))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	changed, err := applyFreeBSDRCDScripts(map[string][]byte{
		"routerd": []byte("#!/bin/sh\n# PROVIDE: routerd\n"),
	}, filepath.Join(dir, "rc.d"))
	if err != nil {
		t.Fatalf("apply rc.d scripts: %v", err)
	}
	if !stringSliceContains(changed, "service:routerd:restart-required") {
		t.Fatalf("changed = %v, want restart-required marker", changed)
	}
	serviceCalls, err := os.ReadFile(serviceLog)
	if err == nil && strings.Contains(string(serviceCalls), "routerd") {
		t.Fatalf("routerd service must not be controlled from routerd apply:\n%s", serviceCalls)
	}
}

func TestApplySystemdUnitResourcesNoopsWithoutConfigResource(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	commandLog := filepath.Join(dir, "commands.log")
	writeExecutable(t, filepath.Join(binDir, "systemctl"), fmt.Sprintf(`#!/bin/sh
echo "systemctl $@" >> %q
exit 0
`, commandLog))
	writeExecutable(t, filepath.Join(binDir, "systemd-run"), fmt.Sprintf(`#!/bin/sh
echo "systemd-run $@" >> %q
exit 0
`, commandLog))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	oldDefaults := platformDefaults
	oldFeatures := platformFeatures
	platformDefaults = platform.Defaults{
		OS:               platform.OSLinux,
		SystemdSystemDir: filepath.Join(dir, "systemd"),
	}
	platformFeatures = platform.Features{HasSystemd: true}
	t.Cleanup(func() {
		platformDefaults = oldDefaults
		platformFeatures = oldFeatures
	})

	router := &api.Router{Spec: api.RouterSpec{}}
	changed, err := applySystemdUnitResources(router)
	if err != nil {
		t.Fatalf("apply systemd unit resources: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed = %v, want no direct apply-time service manager changes", changed)
	}
	if _, err := os.Stat(commandLog); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("apply --once path must not call service manager, command log err=%v", err)
	}
}

func TestApplyFreeBSDConfigContinuesAfterPackageFailure(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	writeExecutable(t, filepath.Join(binDir, "pkg"), `#!/bin/sh
exit 3
`)
	writeExecutable(t, filepath.Join(binDir, "sysrc"), `#!/bin/sh
if [ "$#" -eq 1 ]; then
  echo "$1: NO"
  exit 0
fi
exit 0
`)
	writeExecutable(t, filepath.Join(binDir, "service"), `#!/bin/sh
if [ "$2" = "status" ]; then
  exit 1
fi
exit 0
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Package"},
			Metadata: api.ObjectMeta{Name: "deps"},
			Spec: api.PackageSpec{Packages: []api.OSPackageSetSpec{{
				OS:      "freebsd",
				Manager: "pkg",
				Names:   []string{"dnsmasq"},
			}}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
			Metadata: api.ObjectMeta{Name: "internet"},
			Spec:     api.HealthCheckSpec{Daemon: "routerd-healthcheck", Target: "1.1.1.1", Protocol: "icmp"},
		},
	}}}

	rcDir := filepath.Join(dir, "rc.d")
	changed, warnings, err := applyFreeBSDConfigWithOptions(router, routerstate.New(), "", "", "", rcDir, freeBSDConfigApplyOptions{ManageServices: true})
	if err != nil {
		t.Fatalf("apply FreeBSD config should continue after package failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rcDir, "routerd_healthcheck_internet")); err != nil {
		t.Fatalf("expected rc.d script to be written after package warning: %v", err)
	}
	if !stringSliceContainsPrefix(warnings, "pkg install:") {
		t.Fatalf("warnings = %v, want package warning", warnings)
	}
	if !stringSliceContains(changed, filepath.Join(rcDir, "routerd_healthcheck_internet")) {
		t.Fatalf("changed = %v, want rc.d script path", changed)
	}
}

func TestLinuxPackageOSNameFromRecognizesDebianLike(t *testing.T) {
	got := linuxPackageOSNameFrom("NAME=\"Pop!_OS\"\nID=pop\nID_LIKE=debian\n")
	if got != "ubuntu" {
		t.Fatalf("linuxPackageOSNameFrom = %q, want ubuntu", got)
	}
	if got := defaultLinuxPackageManager(got); got != "apt" {
		t.Fatalf("defaultLinuxPackageManager = %q, want apt", got)
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

func TestRunFreeBSDServiceTreatsAlreadyRunningAsIdempotentStart(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	writeExecutable(t, filepath.Join(binDir, "service"), `#!/bin/sh
echo "daemon: process already running, pid: 10220" >&2
exit 1
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := runFreeBSDService("routerd_dhcpv6_client_wan_pd", "onestart"); err != nil {
		t.Fatalf("onestart already running should be idempotent: %v", err)
	}
	if err := runFreeBSDService("routerd_dhcpv6_client_wan_pd", "onerestart"); err == nil {
		t.Fatal("onerestart failure should not be hidden")
	}
}

func TestApplyFreeBSDDnsmasqConfigValidatesAndUsesPersistentLeaseDirectory(t *testing.T) {
	dir := t.TempDir()
	oldDefaults := platformDefaults
	platformDefaults = platform.Defaults{
		OS:         platform.OSFreeBSD,
		RuntimeDir: filepath.Join(dir, "var", "run", "routerd"),
		StateDir:   filepath.Join(dir, "var", "db", "routerd"),
	}
	t.Cleanup(func() { platformDefaults = oldDefaults })

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	dnsmasqLog := filepath.Join(dir, "dnsmasq.log")
	serviceLog := filepath.Join(dir, "service.log")
	sysrcLog := filepath.Join(dir, "sysrc.log")
	writeExecutable(t, filepath.Join(binDir, "dnsmasq"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
exit 0
`, dnsmasqLog))
	writeExecutable(t, filepath.Join(binDir, "sysrc"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
exit 0
`, sysrcLog))
	writeExecutable(t, filepath.Join(binDir, "service"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
if [ "$1" = "-l" ]; then
  printf 'routerd_dnsmasq\n'
  exit 0
fi
if [ "$2" = "status" ]; then
  exit 1
fi
exit 0
`, serviceLog))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	configPath := filepath.Join(dir, "usr", "local", "etc", "routerd", "dnsmasq.conf")
	servicePath := filepath.Join(dir, "usr", "local", "etc", "rc.d", "routerd_dnsmasq")
	changed, err := applyFreeBSDDnsmasqConfig(configPath, servicePath, []byte("port=0\ndhcp-leasefile="+dnsmasqLeaseFileForPlatform()+"\n"), filepath.Join(binDir, "dnsmasq"))
	if err != nil {
		t.Fatalf("apply FreeBSD dnsmasq: %v", err)
	}
	for _, path := range []string{
		configPath,
		servicePath,
		filepath.Join(platformDefaults.StateDir, "dnsmasq"),
		platformDefaults.RuntimeDir,
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}
	if !containsString(changed, configPath) || !containsString(changed, servicePath) {
		t.Fatalf("changed = %v", changed)
	}
	dnsmasqCalls, err := os.ReadFile(dnsmasqLog)
	if err != nil {
		t.Fatalf("read dnsmasq log: %v", err)
	}
	if !strings.Contains(string(dnsmasqCalls), "--test --conf-file="+configPath) {
		t.Fatalf("dnsmasq --test not called:\n%s", dnsmasqCalls)
	}
	serviceData, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatalf("read rc.d script: %v", err)
	}
	for _, want := range []string{
		`mkdir -p "` + platformDefaults.RuntimeDir + `"`,
		`mkdir -p "` + filepath.Join(platformDefaults.StateDir, "dnsmasq") + `"`,
	} {
		if !strings.Contains(string(serviceData), want) {
			t.Fatalf("rc.d script missing %q:\n%s", want, serviceData)
		}
	}
}

func TestApplyFreeBSDDnsmasqConfigWithoutServicePathDoesNotManageService(t *testing.T) {
	dir := t.TempDir()
	oldDefaults := platformDefaults
	platformDefaults = platform.Defaults{
		OS:         platform.OSFreeBSD,
		RuntimeDir: filepath.Join(dir, "var", "run", "routerd"),
		StateDir:   filepath.Join(dir, "var", "db", "routerd"),
	}
	t.Cleanup(func() { platformDefaults = oldDefaults })

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	dnsmasqLog := filepath.Join(dir, "dnsmasq.log")
	serviceLog := filepath.Join(dir, "service.log")
	writeExecutable(t, filepath.Join(binDir, "dnsmasq"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
exit 0
`, dnsmasqLog))
	writeExecutable(t, filepath.Join(binDir, "service"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
exit 1
`, serviceLog))
	writeExecutable(t, filepath.Join(binDir, "sysrc"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
exit 1
`, serviceLog))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	configPath := filepath.Join(dir, "usr", "local", "etc", "routerd", "dnsmasq.conf")
	changed, err := applyFreeBSDDnsmasqConfig(configPath, "", []byte("port=0\ndhcp-leasefile="+dnsmasqLeaseFileForPlatform()+"\n"), filepath.Join(binDir, "dnsmasq"))
	if err != nil {
		t.Fatalf("apply FreeBSD dnsmasq: %v", err)
	}
	if !containsString(changed, configPath) {
		t.Fatalf("changed = %v, want config path", changed)
	}
	if _, err := os.Stat(platformDefaults.RuntimeDir); err != nil {
		t.Fatalf("expected runtime dir to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(platformDefaults.StateDir, "dnsmasq")); err != nil {
		t.Fatalf("expected lease dir to exist: %v", err)
	}
	if serviceCalls, err := os.ReadFile(serviceLog); err == nil && len(serviceCalls) > 0 {
		t.Fatalf("service/sysrc must not be called without a service path:\n%s", serviceCalls)
	}
}

func TestApplyDnsmasqConfigRemovesOwnedLinuxArtifactsWhenDesiredIsEmpty(t *testing.T) {
	dir := t.TempDir()
	oldDefaults, oldFeatures := platformDefaults, platformFeatures
	platformDefaults = platform.Defaults{OS: platform.OSLinux}
	platformFeatures = platform.Features{HasSystemd: true}
	t.Cleanup(func() {
		platformDefaults, platformFeatures = oldDefaults, oldFeatures
	})

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "systemctl.log")
	writeExecutable(t, filepath.Join(binDir, "systemctl"), fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %q\n", logPath))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	configPath := filepath.Join(dir, "etc", "routerd", "dnsmasq.conf")
	servicePath := filepath.Join(dir, "etc", "systemd", "routerd-dnsmasq.service")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(servicePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(routerdGeneratedConfigMarker+"port=0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servicePath, render.DnsmasqServiceUnit(configPath, "/usr/sbin/dnsmasq"), 0644); err != nil {
		t.Fatal(err)
	}

	changed, err := applyDnsmasqConfig(configPath, servicePath, nil)
	if err != nil {
		t.Fatalf("remove managed dnsmasq: %v", err)
	}
	if !containsString(changed, configPath) || !containsString(changed, servicePath) {
		t.Fatalf("changed = %v", changed)
	}
	for _, path := range []string{configPath, servicePath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s remains after desired removal: %v", path, err)
		}
	}
	data, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(data), "disable --now "+routerdDnsmasqService) {
		t.Fatalf("systemctl calls = %q, want owned disable/stop", data)
	}
}

func TestApplyDnsmasqConfigRemovesOwnedFreeBSDArtifactsWhenDesiredIsEmpty(t *testing.T) {
	dir := t.TempDir()
	oldDefaults, oldFeatures := platformDefaults, platformFeatures
	platformDefaults = platform.Defaults{OS: platform.OSFreeBSD}
	platformFeatures = platform.Features{}
	t.Cleanup(func() {
		platformDefaults, platformFeatures = oldDefaults, oldFeatures
	})

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "rc.log")
	for _, name := range []string{"service", "sysrc"} {
		writeExecutable(t, filepath.Join(binDir, name), fmt.Sprintf("#!/bin/sh\necho %s:\"$@\" >> %q\n", name, logPath))
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	configPath := filepath.Join(dir, "usr", "local", "etc", "routerd", "dnsmasq.conf")
	servicePath := filepath.Join(dir, "usr", "local", "etc", "rc.d", "routerd_dnsmasq")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(servicePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(routerdGeneratedConfigMarker+"port=0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servicePath, render.DnsmasqRCScript(configPath, filepath.Join(dir, "run"), filepath.Join(dir, "lease"), "/usr/local/sbin/dnsmasq"), 0555); err != nil {
		t.Fatal(err)
	}

	if _, err := applyDnsmasqConfig(configPath, servicePath, nil); err != nil {
		t.Fatalf("remove managed dnsmasq: %v", err)
	}
	for _, path := range []string{configPath, servicePath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s remains after desired removal: %v", path, err)
		}
	}
	data, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(data), "service:routerd_dnsmasq onestop") || !strings.Contains(string(data), "sysrc:routerd_dnsmasq_enable=NO") {
		t.Fatalf("FreeBSD lifecycle calls = %q", data)
	}
}

func TestApplyDnsmasqConfigPreservesForeignArtifactsWhenDesiredIsEmpty(t *testing.T) {
	dir := t.TempDir()
	oldDefaults, oldFeatures := platformDefaults, platformFeatures
	platformDefaults = platform.Defaults{OS: platform.OSLinux}
	platformFeatures = platform.Features{HasSystemd: true}
	t.Cleanup(func() {
		platformDefaults, platformFeatures = oldDefaults, oldFeatures
	})
	configPath := filepath.Join(dir, "dnsmasq.conf")
	servicePath := filepath.Join(dir, "routerd-dnsmasq.service")
	if err := os.WriteFile(configPath, []byte("# administrator owned\nport=0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servicePath, []byte("# administrator owned\n"), 0644); err != nil {
		t.Fatal(err)
	}
	changed, err := applyDnsmasqConfig(configPath, servicePath, nil)
	if err != nil {
		t.Fatalf("preserve foreign dnsmasq: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed = %v, want no foreign mutation", changed)
	}
	for _, path := range []string{configPath, servicePath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("foreign %s was removed: %v", path, err)
		}
	}
}

func TestApplyDnsmasqConfigPreservesOwnedConfigWithForeignLinuxServiceWhenDesiredIsEmpty(t *testing.T) {
	dir := t.TempDir()
	oldDefaults, oldFeatures := platformDefaults, platformFeatures
	platformDefaults = platform.Defaults{OS: platform.OSLinux}
	platformFeatures = platform.Features{HasSystemd: true}
	t.Cleanup(func() {
		platformDefaults, platformFeatures = oldDefaults, oldFeatures
	})
	configPath := filepath.Join(dir, "dnsmasq.conf")
	servicePath := filepath.Join(dir, "routerd-dnsmasq.service")
	if err := os.WriteFile(configPath, []byte(routerdGeneratedConfigMarker+"port=0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servicePath, []byte("[Unit]\nDescription=administrator managed dnsmasq\n"), 0644); err != nil {
		t.Fatal(err)
	}
	changed, err := applyDnsmasqConfig(configPath, servicePath, nil)
	if err != nil {
		t.Fatalf("preserve ownership collision: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed = %v, want no collision mutation", changed)
	}
	for _, path := range []string{configPath, servicePath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("collision path %s was removed: %v", path, err)
		}
	}
}

func TestApplyDnsmasqConfigPreservesOwnedConfigWithForeignFreeBSDServiceWhenDesiredIsEmpty(t *testing.T) {
	dir := t.TempDir()
	oldDefaults, oldFeatures := platformDefaults, platformFeatures
	platformDefaults = platform.Defaults{OS: platform.OSFreeBSD}
	platformFeatures = platform.Features{}
	t.Cleanup(func() {
		platformDefaults, platformFeatures = oldDefaults, oldFeatures
	})
	configPath := filepath.Join(dir, "dnsmasq.conf")
	servicePath := filepath.Join(dir, "routerd_dnsmasq")
	if err := os.WriteFile(configPath, []byte(routerdGeneratedConfigMarker+"port=0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servicePath, []byte("#!/bin/sh\n# administrator-owned rc.d service\n"), 0555); err != nil {
		t.Fatal(err)
	}
	changed, err := applyDnsmasqConfig(configPath, servicePath, nil)
	if err != nil {
		t.Fatalf("preserve ownership collision: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed = %v, want no collision mutation", changed)
	}
	for _, path := range []string{configPath, servicePath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("collision path %s was removed: %v", path, err)
		}
	}
}

func TestApplyFreeBSDDSLiteSupportsDynamicDelegatedAddress(t *testing.T) {
	dir := t.TempDir()
	oldDefaults := platformDefaults
	platformDefaults = platform.Defaults{OS: platform.OSFreeBSD}
	t.Cleanup(func() { platformDefaults = oldDefaults })

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	ifconfigLog := filepath.Join(dir, "ifconfig.log")
	routeLog := filepath.Join(dir, "route.log")
	writeExecutable(t, filepath.Join(binDir, "ifconfig"), fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
if [ "$1" = "vtnet1" ] && [ "$#" -eq 1 ]; then
  cat <<'OUT'
vtnet1: flags=1008843<UP,BROADCAST,RUNNING,SIMPLEX,MULTICAST> metric 0 mtu 1500
	inet6 fe80::1%%vtnet1 prefixlen 64 scopeid 0x2
	inet6 2001:db8:1234:5678::abcd prefixlen 64
OUT
  exit 0
fi
if [ "$#" -eq 1 ]; then
  exit 1
fi
exit 0
`, ifconfigLog))
	writeExecutable(t, filepath.Join(binDir, "route"), fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
if [ "$1" = "-n" ] && [ "$2" = "get" ]; then
  printf 'gateway: 192.0.2.1\ninterface: vtnet0\n'
  exit 0
fi
exit 0
`, routeLog))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "vtnet0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "vtnet1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-ipv6"}, Spec: api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", SubnetID: "0", AddressSuffix: "::1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite"}, Spec: api.DSLiteTunnelSpec{
			Interface:             "wan",
			TunnelName:            "ds-lite",
			LocalAddressSource:    "delegatedAddress",
			LocalDelegatedAddress: "lan-ipv6",
			LocalAddressSuffix:    "::11",
			AFTRIPv6:              "2001:db8::feed",
			MTU:                   1454,
			DefaultRoute:          true,
		}},
	}}}

	store := routerstate.New()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{CurrentPrefix: "2001:db8:1234:5678::/64"}), "test")
	changed, err := applyDSLiteTunnelsWithState(router, store)
	if err != nil {
		t.Fatalf("apply DS-Lite tunnels: %v", err)
	}
	if !containsString(changed, "ds-lite") {
		t.Fatalf("changed = %v, want ds-lite", changed)
	}
	gif := freeBSDDSLiteRuntimeIfName("ds-lite")
	ifconfigCalls, err := os.ReadFile(ifconfigLog)
	if err != nil {
		t.Fatalf("read ifconfig log: %v", err)
	}
	for _, want := range []string{
		"vtnet1 inet6 2001:db8:1234:5678::11 prefixlen 64 alias",
		gif + " create",
		gif + " inet6 tunnel 2001:db8:1234:5678::11 2001:db8::feed",
		gif + " inet 192.0.0.2 192.0.0.1 netmask 255.255.255.255",
		gif + " mtu 1454",
		gif + " up",
	} {
		if !strings.Contains(string(ifconfigCalls), want) {
			t.Fatalf("ifconfig calls missing %q:\n%s", want, ifconfigCalls)
		}
	}
	routeCalls, err := os.ReadFile(routeLog)
	if err != nil {
		t.Fatalf("read route log: %v", err)
	}
	if !strings.Contains(string(routeCalls), "-n change default 192.0.0.1") {
		t.Fatalf("route change not called for %s:\n%s", gif, routeCalls)
	}
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

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0755); err != nil {
		t.Fatalf("write %s: %v", path, err)
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

func TestReplaceManagedPPPoEBlocks(t *testing.T) {
	current := "# existing\nold * value *\n# BEGIN routerd pppoe old\n\"u\" * \"old\" *\n# END routerd pppoe old\n"
	got := replaceManagedPPPoEBlocks(current, []render.PPPoESecretEntry{
		{Name: "wan", Username: "user@example.jp", Password: "secret"},
	})
	for _, want := range []string{
		"# existing\nold * value *\n",
		"# BEGIN routerd pppoe wan\n",
		"\"user@example.jp\" * \"secret\" *\n",
		"# END routerd pppoe wan\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("managed secrets missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\"u\" * \"old\"") {
		t.Fatalf("old managed block was not removed:\n%s", got)
	}
}

func TestRenderEgressRoutePolicyDefaultMarks(t *testing.T) {
	data, err := renderEgressRoutePolicyDefaultMarks(
		"test/default",
		api.EgressRoutePolicySpec{
			SourceCIDRs:      []string{"192.168.10.0/24"},
			DestinationCIDRs: []string{"0.0.0.0/0"},
		},
		api.EgressRoutePolicyCandidate{Name: "pppoe", Mark: 273},
		[]api.EgressRoutePolicyCandidate{
			{Name: "dslite", Mark: 272},
			{Name: "pppoe", Mark: 273},
		},
	)
	if err != nil {
		t.Fatalf("render default route policy marks: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"add table ip routerd_default_route",
		"flush table ip routerd_default_route",
		"table ip routerd_default_route",
		"ip saddr 192.168.10.0/24 ip daddr 0.0.0.0/0 ct mark { 0x110, 0x111 } meta mark set ct mark",
		"ct mark != 0x0 ct mark != { 0x110, 0x111 } meta mark set 0x111 ct mark set meta mark",
		"ct mark 0x0 meta mark set 0x111 ct mark set meta mark",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestRenderEgressRoutePolicyDefaultMarksTargetCandidateActive(t *testing.T) {
	data, err := renderEgressRoutePolicyDefaultMarks(
		"test/default",
		api.EgressRoutePolicySpec{
			SourceCIDRs:      []string{"192.168.10.0/24"},
			DestinationCIDRs: []string{"0.0.0.0/0"},
		},
		api.EgressRoutePolicyCandidate{Name: "dslite", Targets: []api.EgressRoutePolicyTarget{
			{Name: "transix-a", Mark: 256},
			{Name: "transix-b", Mark: 257},
			{Name: "transix-c", Mark: 258},
		}},
		[]api.EgressRoutePolicyCandidate{
			{Name: "dslite", Targets: []api.EgressRoutePolicyTarget{
				{Name: "transix-a", Mark: 256},
				{Name: "transix-b", Mark: 257},
				{Name: "transix-c", Mark: 258},
			}},
		},
	)
	if err != nil {
		t.Fatalf("render default route policy marks: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"ip saddr 192.168.10.0/24 ip daddr 0.0.0.0/0 ct mark { 0x100, 0x101, 0x102 } meta mark set ct mark",
		"ct mark != 0x0 ct mark != { 0x100, 0x101, 0x102 } meta mark set 0x0 ct mark set meta mark",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ct mark 0x0 meta mark set 0x") {
		t.Fatalf("target candidate should leave new flows unmarked for EgressRoutePolicy hashing:\n%s", got)
	}
}

func TestSelectIPv4DefaultRouteCandidateTreatsMissingHealthCheckAsUp(t *testing.T) {
	candidate, ok := selectIPv4DefaultRouteCandidate([]api.EgressRoutePolicyCandidate{
		{Name: "preferred", Priority: 10, HealthCheck: "preferred-check"},
		{Name: "fallback", Priority: 20},
	}, map[string]bool{"preferred-check": false})
	if !ok {
		t.Fatal("candidate not selected")
	}
	if candidate.Name != "fallback" {
		t.Fatalf("candidate = %s, want fallback", candidate.Name)
	}
}

func TestAvailableIPv4DefaultRouteCandidatesSkipsMissingTargetDevices(t *testing.T) {
	candidates := []api.EgressRoutePolicyCandidate{
		{Name: "dslite", Priority: 10, Targets: []api.EgressRoutePolicyTarget{
			{Interface: "ds-lite-a"},
			{Interface: "ds-lite-b"},
		}},
		{Name: "wan", Priority: 20, Interface: "wan", HealthCheck: "wan-check"},
	}
	aliases := map[string]string{
		"wan":       "ens18",
		"ds-lite-a": "ds-lite-a",
		"ds-lite-b": "ds-lite-b",
	}
	available := availableIPv4DefaultRouteCandidates(effectiveRouterAvailability{
		Router:     &api.Router{},
		Aliases:    aliases,
		Health:     map[string]bool{"wan-check": true},
		LinkExists: func(ifname string) bool { return ifname == "ens18" },
	}, candidates)
	if len(available) != 1 || available[0].Name != "wan" {
		t.Fatalf("available candidates = %+v, want wan only", available)
	}
}

func TestAvailableIPv4DefaultRouteCandidatesKeepsTargetWithAnyDevice(t *testing.T) {
	candidates := []api.EgressRoutePolicyCandidate{
		{Name: "dslite", Priority: 10, Targets: []api.EgressRoutePolicyTarget{
			{Interface: "ds-lite-a"},
			{Interface: "ds-lite-b"},
		}},
		{Name: "wan", Priority: 20, Interface: "wan"},
	}
	aliases := map[string]string{
		"wan":       "ens18",
		"ds-lite-a": "ds-lite-a",
		"ds-lite-b": "ds-lite-b",
	}
	available := availableIPv4DefaultRouteCandidates(effectiveRouterAvailability{
		Router:     &api.Router{},
		Aliases:    aliases,
		LinkExists: func(ifname string) bool { return ifname == "ens18" || ifname == "ds-lite-b" },
	}, candidates)
	if len(available) != 2 || available[0].Name != "dslite" {
		t.Fatalf("available candidates = %+v, want dslite first", available)
	}
}

func TestAvailableIPv4DefaultRouteCandidatesSkipsDSLiteWithoutLocalAddress(t *testing.T) {
	candidates := []api.EgressRoutePolicyCandidate{
		{Name: "dslite", Priority: 10, Targets: []api.EgressRoutePolicyTarget{{Interface: "ds-lite-a"}}},
		{Name: "wan", Priority: 20, Interface: "wan"},
	}
	aliases := map[string]string{
		"wan":       "ens18",
		"lan":       "ens19",
		"ds-lite-a": "ds-lite-a",
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
			Metadata: api.ObjectMeta{Name: "ds-lite-a"},
			Spec: api.DSLiteTunnelSpec{
				Interface:             "wan",
				LocalAddressSource:    "delegatedAddress",
				LocalDelegatedAddress: "lan-ipv6",
				LocalAddressSuffix:    "::3",
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"},
			Metadata: api.ObjectMeta{Name: "lan-ipv6"},
			Spec: api.IPv6DelegatedAddressSpec{
				PrefixDelegation: "wan-pd",
				Interface:        "lan",
				AddressSuffix:    "::3",
			},
		},
	}}}
	available := availableIPv4DefaultRouteCandidates(effectiveRouterAvailability{
		Router:     router,
		Aliases:    aliases,
		LinkExists: func(ifname string) bool { return ifname == "ens18" || ifname == "ds-lite-a" },
	}, candidates)
	if len(available) != 1 || available[0].Name != "wan" {
		t.Fatalf("available candidates = %+v, want wan only", available)
	}
}

func TestApplyDSLiteSkipsAFTRResolutionWithoutDelegatedPrefix(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "ens18"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "ens19"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"},
			Metadata: api.ObjectMeta{Name: "lan-ipv6"},
			Spec: api.IPv6DelegatedAddressSpec{
				PrefixDelegation: "wan-pd",
				Interface:        "lan",
				AddressSuffix:    "::3",
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
			Metadata: api.ObjectMeta{Name: "transix-a"},
			Spec: api.DSLiteTunnelSpec{
				Interface:             "wan",
				TunnelName:            "ds-transix-a",
				AFTRFQDN:              "invalid.invalid",
				AFTRDNSServers:        []string{"2001:db8::53"},
				AFTRAddressOrdinal:    1,
				LocalAddressSource:    "delegatedAddress",
				LocalDelegatedAddress: "lan-ipv6",
				LocalAddressSuffix:    "::100",
			},
		},
	}}}
	applied, err := applyDSLiteTunnelsWithState(router, routerstate.New())
	if err != nil {
		t.Fatalf("apply DS-Lite without delegated prefix: %v", err)
	}
	if len(applied) != 1 || applied[0] != "removed-unusable:ds-transix-a" {
		t.Fatalf("applied = %v, want removed-unusable tunnel", applied)
	}
}

func TestDSLiteAFTRDNSServersIncludeAFTRSourceStatus(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv6Information", "wan-info", map[string]any{
		"phase":      "Ready",
		"dnsServers": []string{"2404:1a8:7f01:a::3", "2404:1a8:7f01:b::3"},
	}); err != nil {
		t.Fatalf("save status: %v", err)
	}
	got := dsliteAFTRDNSServersWithState(api.DSLiteTunnelSpec{
		AFTRDNSServers: []string{"2001:db8::53"},
		AFTRFrom: api.StatusValueSourceSpec{
			Resource: "DHCPv6Information/wan-info",
			Field:    "aftrName",
		},
	}, store)
	want := []string{"2001:db8::53", "2404:1a8:7f01:a::3", "2404:1a8:7f01:b::3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dns servers = %#v, want %#v", got, want)
	}
}

func TestDNSServerAddressAddsDefaultPort(t *testing.T) {
	tests := map[string]string{
		"127.0.0.1":         "127.0.0.1:53",
		"127.0.0.1:1053":    "127.0.0.1:1053",
		"2001:db8::53":      "[2001:db8::53]:53",
		"[2001:db8::53]:53": "[2001:db8::53]:53",
	}
	for input, want := range tests {
		if got := dnsServerAddress(input); got != want {
			t.Fatalf("dnsServerAddress(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResolveHealthCheckTargetDSLiteRemoteAddress(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
				Metadata: api.ObjectMeta{Name: "transix"},
				Spec: api.DSLiteTunnelSpec{
					Interface:     "wan",
					TunnelName:    "ds-transix",
					RemoteAddress: "2404:8e00::feed:100",
				},
			},
		}},
	}
	target, family, err := resolveHealthCheckTarget(router, api.HealthCheckSpec{
		Interface:    "transix",
		TargetSource: "dsliteRemote",
	}, map[string]string{"transix": "ds-transix"})
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	if target != "2404:8e00::feed:100" || family != "ipv6" {
		t.Fatalf("target/family = %s/%s, want 2404:8e00::feed:100/ipv6", target, family)
	}
}

func TestHealthCheckPingSourceUsesDSLiteLocalAddress(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
				Metadata: api.ObjectMeta{Name: "transix"},
				Spec: api.DSLiteTunnelSpec{
					Interface:          "wan",
					TunnelName:         "ds-transix",
					LocalAddressSource: "static",
					LocalAddress:       "2001:db8::3",
					RemoteAddress:      "2001:db8::100",
				},
			},
		}},
	}
	source := healthCheckPingSource(router, api.HealthCheckSpec{
		Interface:    "transix",
		TargetSource: "dsliteRemote",
	}, map[string]string{"wan": "ens18", "transix": "ds-transix"})
	if source != "2001:db8::3" {
		t.Fatalf("ping source = %q, want 2001:db8::3", source)
	}
}

func TestChangedNetworkdInterfaces(t *testing.T) {
	got := changedNetworkdInterfaces([]string{
		"/etc/systemd/network/10-netplan-ens19.network.d/90-routerd-dhcpv6-pd.conf",
		"/etc/systemd/network/10-netplan-ens19.network.d/90-routerd-extra.conf",
		"/etc/systemd/network/10-netplan-ens18.network.d/90-routerd-dhcpv6-pd.conf",
	})
	want := []string{"ens19", "ens18"}
	if len(got) != len(want) {
		t.Fatalf("interfaces = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("interfaces = %v, want %v", got, want)
		}
	}
}

func TestManagedHostnames(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Hostname"},
				Metadata: api.ObjectMeta{Name: "system-hostname"},
				Spec:     api.HostnameSpec{Hostname: "router03.example.internal", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Hostname"},
				Metadata: api.ObjectMeta{Name: "observed-hostname"},
				Spec:     api.HostnameSpec{Hostname: "ignored.example", Managed: false},
			},
		}},
	}
	got, err := managedHostnames(router)
	if err != nil {
		t.Fatalf("managed hostnames: %v", err)
	}
	if len(got) != 1 || got[0] != "router03.example.internal" {
		t.Fatalf("managed hostnames = %v, want router03.example.internal", got)
	}
}

func TestDriftedAdoptionCandidates(t *testing.T) {
	candidates := []apply.AdoptionCandidate{
		{
			Kind:     "host.hostname",
			Name:     "system",
			Desired:  map[string]string{"hostname": "router03.example.internal"},
			Observed: map[string]string{"hostname": "router03"},
		},
		{
			Kind:     "linux.ipv4.routeTable",
			Name:     "table=111",
			Desired:  map[string]string{"table": "111", "ifname": "ppp0"},
			Observed: map[string]string{"table": "111", "ifname": "ppp0"},
		},
	}
	got := driftedAdoptionCandidates(candidates)
	if len(got) != 1 || got[0].Kind != "host.hostname" {
		t.Fatalf("drifted candidates = %+v, want hostname only", got)
	}
}

func TestAdoptedArtifactsForResultDeduplicates(t *testing.T) {
	artifacts := []resource.Artifact{
		{Kind: "nft.table", Name: "routerd_nat", Owner: "one"},
		{Kind: "nft.table", Name: "routerd_nat", Owner: "one"},
		{Kind: "host.hostname", Name: "system", Owner: "host"},
	}
	got := adoptedArtifactsForResult(artifacts)
	if len(got) != 2 {
		t.Fatalf("adopted artifacts = %+v, want two", got)
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

func TestConflictingManagedIPv6Addresses(t *testing.T) {
	got := conflictingManagedIPv6Addresses([]ipv6AddressEntry{
		{Address: "fe80::1", PrefixLen: 64},
		{Address: "2001:db8:3d60:1240::1", PrefixLen: 64},
		{Address: "2001:db8:3d60:1220::1", PrefixLen: 64},
		{Address: "2001:db8:3d60:1220::100", PrefixLen: 128},
	}, "2001:db8:3d60:1220::1", ipv6HostSuffix64(netip.MustParseAddr("::1")))
	if len(got) != 1 || got[0].Address != "2001:db8:3d60:1240::1" {
		t.Fatalf("conflicts = %#v, want stale ::1 only", got)
	}
}

func TestManagedDelegatedIPv6TargetsIncludeDelegatedAddressAndDSLiteSources(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-ipv6"}, Spec: api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", SubnetID: "0", AddressSuffix: "::3"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}, Spec: api.DSLiteTunnelSpec{Interface: "wan", LocalAddressSource: "delegatedAddress", LocalDelegatedAddress: "lan-ipv6", LocalAddressSuffix: "::100"}},
	}}}
	store := routerstate.New()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{CurrentPrefix: "2001:db8:3d60:1220::/60"}), "test")

	targets, err := managedDelegatedIPv6Targets(router, store)
	if err != nil {
		t.Fatalf("managed targets: %v", err)
	}
	for _, want := range []string{"2001:db8:3d60:1220::3", "2001:db8:3d60:1220::100"} {
		if !targets.DesiredByInterface["ens19"][want] {
			t.Fatalf("desired targets = %#v, missing %s", targets.DesiredByInterface, want)
		}
	}
	for _, suffix := range []string{"::3", "::100"} {
		addr := netip.MustParseAddr(suffix)
		if !targets.SuffixesByInterface["ens19"][ipv6HostSuffix64(addr)] {
			t.Fatalf("suffix targets = %#v, missing %s", targets.SuffixesByInterface, suffix)
		}
	}
}

func TestManagedDelegatedIPv6TargetsTrackSuffixWithoutCurrentPrefix(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-ipv6"}, Spec: api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", AddressSuffix: "::3"}},
	}}}
	store := routerstate.New()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{LastPrefix: "2001:db8:3d60:1220::/60"}), "test")

	targets, err := managedDelegatedIPv6Targets(router, store)
	if err != nil {
		t.Fatalf("managed targets: %v", err)
	}
	if len(targets.DesiredByInterface["ens19"]) != 0 {
		t.Fatalf("desired targets = %#v, want none without current prefix", targets.DesiredByInterface)
	}
	if !targets.SuffixesByInterface["ens19"][ipv6HostSuffix64(netip.MustParseAddr("::3"))] {
		t.Fatalf("suffix targets = %#v, want ::3 tracked for stale cleanup", targets.SuffixesByInterface)
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
			Spec:     api.DHCPv6PrefixDelegationSpec{Interface: "wan"},
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

func TestStateWhenRequiresSetAndEqual(t *testing.T) {
	store := routerstate.New()
	when := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
		"wan.ipv6.mode": {Equals: "pd-ready"},
	}}
	if resourceWhenMatches(when, store) {
		t.Fatal("unknown state matched equals")
	}
	store.Unset("wan.ipv6.mode", "observed absent")
	if resourceWhenMatches(when, store) {
		t.Fatal("unset state matched equals")
	}
	store.Set("wan.ipv6.mode", "address-only", "observed fallback")
	if resourceWhenMatches(when, store) {
		t.Fatal("different set value matched equals")
	}
	store.Set("wan.ipv6.mode", "pd-ready", "observed pd")
	if !resourceWhenMatches(when, store) {
		t.Fatal("matching set value did not match equals")
	}
}

func TestStateWhenAllAnyAndNested(t *testing.T) {
	store := routerstate.New()
	store.Set("wan.a", "up", "test")
	store.Set("wan.b", "down", "test")
	store.Set("wan.c", "ready", "test")

	all := api.ResourceWhenSpec{All: []api.ResourceWhenSpec{
		{State: map[string]api.StateMatchSpec{"wan.a": {Equals: "up"}}},
		{State: map[string]api.StateMatchSpec{"wan.c": {Equals: "ready"}}},
	}}
	if !resourceWhenMatches(all, store) {
		t.Fatal("all predicate did not match")
	}

	any := api.ResourceWhenSpec{Any: []api.ResourceWhenSpec{
		{State: map[string]api.StateMatchSpec{"wan.a": {Equals: "down"}}},
		{State: map[string]api.StateMatchSpec{"wan.b": {Equals: "down"}}},
	}}
	if !resourceWhenMatches(any, store) {
		t.Fatal("any predicate did not match")
	}

	nested := api.ResourceWhenSpec{Any: []api.ResourceWhenSpec{
		{All: []api.ResourceWhenSpec{
			{State: map[string]api.StateMatchSpec{"wan.a": {Equals: "up"}}},
			{State: map[string]api.StateMatchSpec{"wan.b": {Equals: "up"}}},
		}},
		{All: []api.ResourceWhenSpec{
			{State: map[string]api.StateMatchSpec{"wan.a": {Equals: "up"}}},
			{State: map[string]api.StateMatchSpec{"wan.c": {Equals: "ready"}}},
		}},
	}}
	if !resourceWhenMatches(nested, store) {
		t.Fatal("nested any/all predicate did not match")
	}
}

func TestStateWhenSinglePredicateEqualsAllSugar(t *testing.T) {
	store := routerstate.New()
	store.Set("wan.ipv6.mode", "pd-ready", "test")
	leaf := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"wan.ipv6.mode": {Equals: "pd-ready"}}}
	all := api.ResourceWhenSpec{All: []api.ResourceWhenSpec{leaf}}
	if resourceWhenMatches(leaf, store) != resourceWhenMatches(all, store) {
		t.Fatal("single predicate and one-element all are not equivalent")
	}
}

func TestResourceWhenCoversResourceLevelWhenSpecs(t *testing.T) {
	want := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"wan.ready": {Equals: "true"}}}
	for _, tc := range []api.Resource{
		testResourceWithSpecWhen("ObservabilityPipeline", api.ObservabilityPipelineSpec{When: want}),
		testResourceWithSpecWhen("RouterdCluster", api.RouterdClusterSpec{When: want}),
		testResourceWithSpecWhen("VirtualAddress", api.VirtualAddressSpec{Family: "ipv4", When: want}),
		testResourceWithSpecWhen("BGPRouter", api.BGPRouterSpec{When: want}),
		testResourceWithSpecWhen("BGPPeer", api.BGPPeerSpec{When: want}),
		testResourceWithSpecWhen("BFD", api.BFDSpec{When: want}),
		testResourceWithSpecWhen("TailscaleNode", api.TailscaleNodeSpec{When: want}),
		testResourceWithSpecWhen("NTPClient", api.NTPClientSpec{When: want}),
		testResourceWithSpecWhen("NTPServer", api.NTPServerSpec{When: want}),
		testResourceWithSpecWhen("DHCPv4Client", api.DHCPv4ClientSpec{When: want}),
		testResourceWithSpecWhen("ClusterNetworkRoute", api.ClusterNetworkRouteSpec{When: want}),
		testResourceWithSpecWhen("DHCPv4Server", api.DHCPv4ServerSpec{When: want}),
		testResourceWithSpecWhen("DHCPv4Reservation", api.DHCPv4ReservationSpec{When: want}),
		testResourceWithSpecWhen("IPv6DelegatedAddress", api.IPv6DelegatedAddressSpec{When: want}),
		testResourceWithSpecWhen("DHCPv6Server", api.DHCPv6ServerSpec{When: want}),
		testResourceWithSpecWhen("DHCPv4ServerLeaseSync", api.DHCPv4ServerLeaseSyncSpec{When: want}),
		testResourceWithSpecWhen("DHCPv6ServerLeaseSync", api.DHCPv6ServerLeaseSyncSpec{When: want}),
		testResourceWithSpecWhen("DHCPv6PrefixDelegationLeaseSync", api.DHCPv6PrefixDelegationLeaseSyncSpec{When: want}),
		testResourceWithSpecWhen("DHCPv6PrefixDelegation", api.DHCPv6PrefixDelegationSpec{When: want}),
		testResourceWithSpecWhen("DHCPv6Information", api.DHCPv6InformationSpec{When: want}),
		testResourceWithSpecWhen("IPv6RouterAdvertisement", api.IPv6RouterAdvertisementSpec{When: want}),
		testResourceWithSpecWhen("DSLiteTunnel", api.DSLiteTunnelSpec{When: want}),
		testResourceWithSpecWhen("DNSForwarder", api.DNSForwarderSpec{When: want}),
		testResourceWithSpecWhen("DNSResolver", api.DNSResolverSpec{When: want}),
		testResourceWithSpecWhen("DNSUpstream", api.DNSUpstreamSpec{When: want}),
		testResourceWithSpecWhen("EventGroup", api.EventGroupSpec{When: want}),
		testResourceWithSpecWhen("HealthCheck", api.HealthCheckSpec{When: want}),
		testResourceWithSpecWhen("NAT44Rule", api.NAT44RuleSpec{When: want}),
		testResourceWithSpecWhen("NAT44SessionSync", api.NAT44SessionSyncSpec{When: want}),
		testResourceWithSpecWhen("PortForward", api.PortForwardSpec{When: want}),
		testResourceWithSpecWhen("IngressService", api.IngressServiceSpec{When: want}),
		testResourceWithSpecWhen("IPAddressSet", api.IPAddressSetSpec{When: want}),
		testResourceWithSpecWhen("LocalServiceRedirect", api.LocalServiceRedirectSpec{When: want}),
		testResourceWithSpecWhen("FirewallFlowPinhole", api.FirewallFlowPinholeSpec{When: want}),
		testResourceWithSpecWhen("EgressRoutePolicy", api.EgressRoutePolicySpec{When: want}),
	} {
		t.Run(tc.Kind, func(t *testing.T) {
			if got := resourceWhen(tc); !reflect.DeepEqual(got, want) {
				t.Fatalf("resourceWhen(%s) = %#v, want %#v", tc.Kind, got, want)
			}
		})
	}
}

func testResourceWithSpecWhen(kind string, spec any) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{Kind: kind},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec:     spec,
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

func TestSelectAAAAByOrdinal(t *testing.T) {
	values := []string{
		"2404:8e00::feed:100",
		"2404:8e00::feed:101",
		"2404:8e00::feed:102",
	}
	got, err := selectAAAA(values, 2, "ordinal")
	if err != nil {
		t.Fatalf("select AAAA: %v", err)
	}
	if got != "2404:8e00::feed:101" {
		t.Fatalf("AAAA = %s, want 2404:8e00::feed:101", got)
	}
}

func TestSelectAAAAModulo(t *testing.T) {
	values := []string{
		"2404:8e00::feed:100",
		"2404:8e00::feed:101",
	}
	got, err := selectAAAA(values, 3, "ordinalModulo")
	if err != nil {
		t.Fatalf("select AAAA: %v", err)
	}
	if got != "2404:8e00::feed:100" {
		t.Fatalf("AAAA = %s, want 2404:8e00::feed:100", got)
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

func TestStartCommandWithFileOutputReleasesLongLivedSupervisor(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	started := time.Now()
	outputPath, err := startCommandWithFileOutput("sh", "-c", `sleep 5 & printf '%s\n' "$!" > "$1"; printf 'parent exited\n'`, "sh", pidPath)
	if err != nil {
		t.Fatalf("start command: %v", err)
	}
	defer os.Remove(outputPath)
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("start/release waited %s for long-lived child", elapsed)
	}
	if err := waitForFile(pidPath, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := waitForFileText(outputPath, "parent exited", time.Second); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(outputPath)
	if err != nil || !strings.Contains(string(out), "parent exited") {
		t.Fatalf("output = %q, err=%v, want parent output", out, err)
	}
	pidText, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read child PID: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidText)))
	if err != nil {
		t.Fatalf("parse child PID %q: %v", pidText, err)
	}
	child, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("find child process: %v", err)
	}
	if err := child.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("child process is not live after Start/Release: %v", err)
	}
	_ = child.Kill()
	_, _ = child.Wait()
}

func waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", path)
}

func waitForFileText(path, want string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), want) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %q in %s", want, path)
}
