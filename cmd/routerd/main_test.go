// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventlog"
	"github.com/imksoo/routerd/pkg/pdclient"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resource"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestServeConfigMutatorPlanExplainsUnsupportedStateSchema(t *testing.T) {
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
	if !strings.Contains(err.Error(), "unsupported routerd state database format") || !strings.Contains(err.Error(), "fresh routerd 1.5") {
		t.Fatalf("planRouter error is not actionable: %v", err)
	}
	if strings.Contains(err.Error(), "SQL logic error") || strings.Contains(err.Error(), "no such table") || strings.Contains(err.Error(), "objects") {
		t.Fatalf("planRouter leaked internal schema details: %v", err)
	}
}

func TestIPsecRuntimePathsArePlatformSpecific(t *testing.T) {
	oldDefaults := platformDefaults
	t.Cleanup(func() { platformDefaults = oldDefaults })
	platformDefaults.OS = platform.OSLinux
	platformDefaults.SysconfDir = "/etc/routerd"
	if got := ipsecConfigDir(); got != "/etc/routerd/swanctl" {
		t.Fatalf("Linux config dir = %q", got)
	}
	if got := ipsecSwanctlPath(); got != "swanctl" {
		t.Fatalf("Linux swanctl path = %q", got)
	}
	platformDefaults.OS = platform.OSFreeBSD
	platformDefaults.SysconfDir = "/usr/local/etc/routerd"
	if got := ipsecConfigDir(); got != "/usr/local/etc/routerd/swanctl" {
		t.Fatalf("FreeBSD config dir = %q", got)
	}
	if got := ipsecSwanctlPath(); got != "/usr/local/sbin/swanctl" {
		t.Fatalf("FreeBSD swanctl path = %q", got)
	}
	platformDefaults.SysconfDir = filepath.Join(t.TempDir(), "routerd")
	if got := ipsecConfigDir(); got != filepath.Join(platformDefaults.SysconfDir, "swanctl") {
		t.Fatalf("private config dir escaped configured root: %q", got)
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

func TestApplyChainControllerOptionsSkipLegacyClientUnits(t *testing.T) {
	opts := applyChainControllerOptions(applyOptions{})
	if !opts.SkipLegacyClientUnits {
		t.Fatal("one-shot apply must not synthesize legacy DHCP client service units")
	}
	if opts.SuperviseClientDaemons {
		t.Fatal("one-shot apply must leave client daemon supervision to long-running routerd")
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
		attemptHooks: &applyAttemptHooks{configureControllers: func(*controllerchain.Options) { t.Fatal("plan entered controller execution") }},
		DryRun:       true,
		StatePath:    statePath,
		LedgerPath:   ledgerPath,
		StatusFile:   statusPath,
		ConfigPath:   filepath.Join(dir, "router.yaml"),
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

func TestRunApplyChainOncePlansBackupEffectiveRouterAgainstDeclaredGraph(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "routerd.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "lan-gw-v4", map[string]any{"role": "backup"}); err != nil {
		t.Fatalf("save backup role: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close state: %v", err)
	}

	master := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
		"VirtualAddress/lan-gw-v4.role": {Equals: "master"},
	}}
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "backup-effective"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "wan0", Managed: false, Owner: "external"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lan0", Managed: false, Owner: "external"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "lan-gw-v4"}, Spec: api.VirtualAddressSpec{Family: "ipv4", Interface: "lan", Address: "192.0.2.1/32", Mode: "vrrp", VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 18, Priority: 100, Peers: []string{"192.0.2.2"}}}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan", When: master}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-base"}, Spec: api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", AddressSuffix: "::1", DependsOn: []api.ResourceDependencySpec{{Resource: "DHCPv6PrefixDelegation/wan-pd", Phase: "Bound"}}}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "lan-resolver"}, Spec: api.DNSResolverSpec{Listen: []api.DNSResolverListenSpec{{AddressFrom: []api.StatusValueSourceSpec{{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"}}}}, Sources: []api.DNSResolverSourceSpec{{Name: "upstream", Kind: "upstream", Match: []string{"."}, Upstreams: []string{"udp://[2001:db8::53]:53"}}}}},
		}},
	}

	result, err := runApplyChainOnce(context.Background(), router, applyOptions{
		DryRun:            true,
		Sandbox:           true,
		StatePath:         statePath,
		LedgerPath:        filepath.Join(dir, "ledger.db"),
		StatusFile:        filepath.Join(dir, "status.json"),
		DnsmasqConfigPath: filepath.Join(dir, "dnsmasq.conf"),
		NftablesPath:      filepath.Join(dir, "nftables.conf"),
		ConfigPath:        filepath.Join(dir, "router.yaml"),
	}, io.Discard, &eventlog.Logger{})
	if err != nil {
		t.Fatalf("run backup apply chain: %v", err)
	}
	if findApplyResult(result, api.NetAPIVersion+"/DHCPv6PrefixDelegation/wan-pd") != nil {
		t.Fatalf("backup apply plan contains master-only PD: %#v", result.Resources)
	}
	if findApplyResult(result, api.NetAPIVersion+"/IPv6DelegatedAddress/lan-base") == nil {
		t.Fatalf("backup apply plan omitted delegated address: %#v", result.Resources)
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
	runCleanupCommand = func(_ context.Context, name string, args ...string) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil
	}
	label, err := cleanupLedgerOwnedArtifact(t.Context(), resource.Artifact{
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
	cleanupLedgerOwnedOrphansForServe = func(_ context.Context, r *api.Router, ledgerPath string) ([]string, error) {
		gotRouter = r
		gotLedgerPath = ledgerPath
		return []string{"systemd.service/routerd-old.service"}, nil
	}
	removed, err := cleanupServeLedgerOwnedOrphans(t.Context(), router, "/var/lib/routerd/ledger.db", nil)
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
	cleanupLedgerOwnedOrphansForServe = func(context.Context, *api.Router, string) ([]string, error) {
		return nil, wantErr
	}
	removed, err := cleanupServeLedgerOwnedOrphans(t.Context(), &api.Router{}, "/var/lib/routerd/ledger.db", nil)
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

func TestRunApplyChainOnceCommitsCanonicalConfigAndGenerationYAML(t *testing.T) {
	dir := t.TempDir()
	setMissingSAMForwardChainIPTables(t)
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
		Sandbox:            true,
		DnsmasqConfigPath:  filepath.Join(dir, "dnsmasq.conf"),
		NftablesPath:       filepath.Join(dir, "nat44.nft"),
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

func TestServeConfigMutatorApplyReplaceCommitsCanonicalAndGeneration(t *testing.T) {
	dir := t.TempDir()
	setMissingSAMForwardChainIPTables(t)
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
			Sandbox:            true,
			DnsmasqConfigPath:  filepath.Join(dir, "dnsmasq.conf"),
			NftablesPath:       filepath.Join(dir, "nat44.nft"),
		},
		cache:                   &resultCache{},
		logger:                  &eventlog.Logger{},
		applyRuntimeWithOutcome: testCommitRuntime(func() *api.Router { return router }),
		getRouter:               func() *api.Router { return router },
		setRouter:               func(next *api.Router) { router = next },
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
	if result.Result.Phase != "Committed" || result.Result.Generation == 0 {
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
		cache:                   &resultCache{},
		logger:                  &eventlog.Logger{},
		applyRuntimeWithOutcome: testCommitRuntime(func() *api.Router { return router }),
		getRouter:               func() *api.Router { return router },
		setRouter:               func(next *api.Router) { router = next },
	}
	result, err := mutator.apply(nil, controlapi.ApplyRequest{
		CandidateYAML: testRouterYAML("sandbox-router"),
		Replace:       true,
	})
	if err != nil {
		t.Fatalf("sandbox apply: %v", err)
	}
	if result.Result.Generation == 0 || result.Result.Phase != "Committed" {
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
	previousRuntimeDir := platformDefaults.RuntimeDir
	platformDefaults.RuntimeDir = filepath.Join(dir, "runtime")
	t.Cleanup(func() { platformDefaults.RuntimeDir = previousRuntimeDir })
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

func TestStartControllerRuntimeReturnsWhenStoppedDuringBootstrap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := make(chan struct{})
	ready := make(chan struct{})
	entered := make(chan struct{})
	release := make(chan struct{})
	type startResult struct {
		started bool
		err     error
	}
	result := make(chan startResult, 1)
	go func() {
		started, err := startControllerRuntime(ctx, stop, func(context.Context) error {
			close(entered)
			<-release
			return nil
		}, ready)
		result <- startResult{started: started, err: err}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("controller bootstrap did not start")
	}
	close(stop)
	select {
	case result := <-result:
		if result.err != nil {
			t.Fatalf("startControllerRuntime during stop: %v", result.err)
		}
		if result.started {
			t.Fatal("startControllerRuntime reported ready after stop")
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("startControllerRuntime waited for blocked bootstrap after stop")
	}
	select {
	case <-ready:
		t.Fatal("controller runtime became ready after stop")
	default:
	}
	close(release)
}

func TestStartControllerRuntimePrefersStopOverBootstrapError(t *testing.T) {
	stop := make(chan struct{})
	entered := make(chan struct{})
	release := make(chan struct{})
	result := make(chan struct {
		started bool
		err     error
	}, 1)
	go func() {
		started, err := startControllerRuntime(context.Background(), stop, func(context.Context) error {
			close(entered)
			<-release
			return context.Canceled
		}, nil)
		result <- struct {
			started bool
			err     error
		}{started: started, err: err}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("controller bootstrap did not start")
	}
	close(stop)
	close(release)
	select {
	case got := <-result:
		if got.err != nil || got.started {
			t.Fatalf("startControllerRuntime = (%t, %v), want (false, nil)", got.started, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("startControllerRuntime did not return after concurrent stop")
	}
}

func TestServeMutationAdmissionRejectsStop(t *testing.T) {
	var mu sync.Mutex
	var stopping atomic.Bool
	admission := serveMutationAdmission{mu: &mu, stopping: &stopping}
	unlock, err := admission.lock()
	if err != nil {
		t.Fatalf("admission before stop: %v", err)
	}
	unlock()
	stopping.Store(true)
	if _, err := admission.lock(); !errors.Is(err, errServeMutationStopping) {
		t.Fatalf("admission after stop error = %v, want %v", err, errServeMutationStopping)
	}
}

func TestRunGracefulStopWhenIdleBoundedReturnsAtDeadline(t *testing.T) {
	var mu sync.Mutex
	entered := make(chan struct{})
	release := make(chan struct{})
	started := time.Now()
	ran, completed, err := runGracefulStopWhenIdleBounded(&mu, 20*time.Millisecond, func() error {
		close(entered)
		<-release
		return nil
	})
	if !ran || completed {
		t.Fatalf("runGracefulStopWhenIdleBounded = (ran=%t, completed=%t), want (true, false)", ran, completed)
	}
	if !errors.Is(err, errGracefulStopShutdownDeadline) {
		t.Fatalf("runGracefulStopWhenIdleBounded error = %v, want deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("runGracefulStopWhenIdleBounded waited %s for blocked handoff", elapsed)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("bounded handoff did not start")
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for !mu.TryLock() {
		if time.Now().After(deadline) {
			t.Fatal("bounded handoff did not release mutation lock")
		}
		time.Sleep(time.Millisecond)
	}
	mu.Unlock()
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
	previousRuntimeDir := platformDefaults.RuntimeDir
	platformDefaults.RuntimeDir = filepath.Join(dir, "runtime")
	t.Cleanup(func() { platformDefaults.RuntimeDir = previousRuntimeDir })
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

func TestSandboxControllerOptionsRouteArtifactsIntoArtifactDirectory(t *testing.T) {
	oldDefaults := platformDefaults
	dir := t.TempDir()
	platformDefaults.RuntimeDir = filepath.Join(dir, "run", "routerd")
	t.Cleanup(func() { platformDefaults = oldDefaults })

	var opts controllerchain.Options
	artifacts := filepath.Join(dir, "artifacts")
	applySandboxControllerOptions(&opts, filepath.Join(artifacts, "dnsmasq.conf"), filepath.Join(artifacts, "nat44.nft"))
	if !opts.DryRunSysctl {
		t.Fatal("sandbox must dry-run sysctl changes")
	}

	if got, want := opts.DnsmasqPID, filepath.Join(artifacts, "dnsmasq.pid"); got != want {
		t.Fatalf("DnsmasqPID = %q, want %q", got, want)
	}
	if got, want := opts.NftablesPath, filepath.Join(artifacts, "nat44.nft"); got != want {
		t.Fatalf("NftablesPath = %q, want %q", got, want)
	}
	if got, want := opts.FirewallPath, filepath.Join(artifacts, "firewall.nft"); got != want {
		t.Fatalf("FirewallPath = %q, want %q", got, want)
	}
	if got, want := opts.PathMTUPath, filepath.Join(artifacts, "mss.nft"); got != want {
		t.Fatalf("PathMTUPath = %q, want %q", got, want)
	}
	if got, want := opts.ForceFragmentPath, filepath.Join(artifacts, "forcefrag.nft"); got != want {
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
	for _, cidr := range []string{"0.0.0.0/0", "::/0", "203.0.113.1/0", "2001:db8::1/0"} {
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
	gen2 := seedGeneration(t, store, "hash-2", testRouterYAML("rollback-two"), true, "Errored")
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	var stdout strings.Builder
	if err := rollbackCommand([]string{"--list", "--state-file", statePath}, &stdout, io.Discard); err != nil {
		t.Fatalf("rollback --list: %v", err)
	}

	rows := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(rows) != 3 || !reflect.DeepEqual(strings.Fields(rows[0]), []string{"generation", "started_at", "finished_at", "phase", "config", "attempt"}) {
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
	want := []row{{gen2, "Errored", "yes", "(latest)"}, {gen1, "Applied", "yes", ""}}
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
	var identities []string
	for _, resource := range result.Resources {
		identities = append(identities, resource.ID)
	}
	want := []string{api.NetAPIVersion + "/Interface/selected-interface", api.RouterAPIVersion + "/Router/rollback-selected/derived-host-runtime"}
	if !reflect.DeepEqual(identities, want) {
		t.Fatalf("selected generation resource identities = %#v, want %#v", identities, want)
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
	phase := func() string {
		statuses, err := store.ListObjectStatuses()
		if err != nil {
			t.Fatal(err)
		}
		return overallStatusPhase("Healthy", statuses)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Reservation", "client", map[string]any{"phase": "Pending", "reason": "WhenFalse"}); err != nil {
		t.Fatalf("save when false status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DNSForwarder", "default", map[string]any{"matches": 1}); err != nil {
		t.Fatalf("save status without phase: %v", err)
	}
	if got := phase(); got != "Healthy" {
		t.Fatalf("phase = %q, want Healthy for WhenFalse pending and phase-less status", got)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "lan", map[string]any{"phase": "Pending"}); err != nil {
		t.Fatalf("save pending status: %v", err)
	}
	if got := phase(); got != "Pending" {
		t.Fatalf("phase = %q, want Pending", got)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "lan", map[string]any{"phase": "Reconverging", "reason": "GoBGPReconverging"}); err != nil {
		t.Fatalf("save reconverging status: %v", err)
	}
	if got := phase(); got != "Pending" {
		t.Fatalf("phase = %q, want Pending for reconverging", got)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPPeer", "worker", map[string]any{"phase": "Error"}); err != nil {
		t.Fatalf("save error status: %v", err)
	}
	if got := phase(); got != "Error" {
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
	statuses, err := store.ListObjectStatuses()
	if err != nil {
		t.Fatal(err)
	}
	issues := resourcePhaseIssues(statuses)
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
	events, err := store.ReadEvents(t.Context(), api.RouterAPIVersion, "Router", "test-router", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[0].Reason != "StaleStateCleanup" {
		t.Fatalf("events = %+v, want StaleStateCleanup", events)
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

func TestServePreGCKeepsTunnelClaimsForPlatformController(t *testing.T) {
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
			if targetOS == platform.OSLinux {
				// Display claims grant no Linux kernel authority. Only the native
				// owner journal can authorize the independently tested teardown.
				if len(calls) != 0 {
					t.Fatalf("display-only claim caused native mutation: %#v", calls)
				}
				if status := store.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "old"); status["interfaceOwned"] != true {
					t.Fatalf("unconfirmed tunnel claim was lost: %#v", status)
				}
				return
			}
			want := []string{"ifconfig", "old-tun", "destroy"}
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
					UnderlayInterface: "wg-hybrid",
					LocalEndpoint:     "10.99.0.1",
					BGP:               api.SAMTransportBGPProfileSpec{RouterRef: "BGPRouter/core", PeerASN: 64512},
					PeersFrom: []api.SAMTransportPeersSourceSpec{{
						Resource: "SAMNodeSet/fabric-nodes",
						NodeRefs: []string{"core-b"},
					}},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"},
				Metadata: api.ObjectMeta{Name: "fabric-nodes"},
				Spec: api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{
					{NodeRef: "core-a", SAMEndpoint: "10.99.0.1"},
					{NodeRef: "core-b", SAMEndpoint: "10.99.0.2"},
				}},
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

func TestRouterWithIPv6PDClientOptionsRejectsUnsupportedExternalClients(t *testing.T) {
	for _, client := range []string{api.IPv6PDClientNetworkd, api.IPv6PDClientDHCPv6C, api.IPv6PDClientDHCPCD} {
		router := testRouterWithPrefixDelegation(api.DHCPv6PrefixDelegationSpec{Interface: "wan", Client: client})
		if _, _, err := routerWithIPv6PDClientOptions(router, applyOptions{}, "linux"); err == nil {
			t.Fatalf("unsupported configured client accepted: %s", client)
		}
		router = testRouterWithPrefixDelegation(api.DHCPv6PrefixDelegationSpec{Interface: "wan"})
		if _, _, err := routerWithIPv6PDClientOptions(router, applyOptions{OverrideClient: client}, "freebsd"); err == nil {
			t.Fatalf("unsupported override accepted: %s", client)
		}
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

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestPublishVRRPTransitionSignalsWakesControllerBus(t *testing.T) {
	eventBus := bus.New()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events, unsubscribe := eventBus.Subscribe(ctx, bus.Subscription{Topics: []string{daemonapi.EventVRRPRoleTransition}}, 1)
	defer unsubscribe()
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	go func() {
		publishVRRPTransitionSignals(ctx, signals, eventBus)
		close(done)
	}()
	signals <- syscall.SIGUSR1
	select {
	case event := <-events:
		if event.Type != daemonapi.EventVRRPRoleTransition || event.Daemon.Name != "keepalived" {
			t.Fatalf("VRRP transition event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("VRRP transition signal did not reach controller bus")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("VRRP signal forwarder did not stop")
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

func TestAppendPrefixDelegationStateWarnings(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
		Metadata: api.ObjectMeta{Name: "wan-pd"},
		Spec:     api.DHCPv6PrefixDelegationSpec{Interface: "wan"},
	}}}}
	store := routerstate.NewMemory()
	store.SavePDLease("wan-pd", routerstate.PDLease{
		LastPrefix: "2001:db8:3d60:1240::/60",
	})
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
  "updatedAt": %q,
  "prefixes": [{"prefix":"2001:db8:1230::/60","preferredSeconds":14400,"validSeconds":14400,"acquiredAt":%q}]
}`, acquiredAt, updatedAt, acquiredAt))
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
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := recordObservedPrefixDelegationState(router, store, nil); err != nil {
		t.Fatalf("record PD state: %v", err)
	}
	result := &apply.Result{}
	appendPrefixDelegationStateWarnings(result, router, store)
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	lease, ok, readErr := store.ReadPDLease("wan-pd")
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !ok {
		t.Fatal("lease missing")
	}
	if lease.CurrentPrefix != "2001:db8:1230::/60" || lease.ValidSeconds != 14400 || lease.LastReplyAt.Format(time.RFC3339) != acquiredAt {
		t.Fatalf("lease = %+v", lease)
	}
}

func TestManagedPDClientSnapshotMatchesDefaultProfileImplicitDUID(t *testing.T) {
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	ifname := ""
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback == 0 && len(iface.HardwareAddr) == 6 {
			ifname = iface.Name
			break
		}
	}
	if ifname == "" {
		t.Skip("an Ethernet interface is unavailable")
	}
	duid, err := interfacePDClientDUID(ifname)
	if err != nil || duid == "" {
		t.Fatalf("read default interface DUID: %v", err)
	}
	now := time.Now().UTC()
	snapshot := pdclient.Snapshot{Resource: "wan-pd", Interface: ifname, State: pdclient.StateBound, CurrentPrefix: "2001:db8:1200::/60", ClientDUID: duid, IAID: 1, Valid: 60, AcquiredAt: now, ExpiresAt: now.Add(time.Minute), Prefixes: []pdclient.PrefixSnapshot{{Prefix: "2001:db8:1200::/60", Preferred: 60, Valid: 60, AcquiredAt: now}}}
	if !managedPDClientSnapshotIdentityMatches(snapshot, "wan-pd", ifname, api.DHCPv6PrefixDelegationSpec{Interface: "wan"}, duid) {
		t.Fatal("default profile implicit link-layer DUID must match daemon rule")
	}
}

func TestManagedPDObservationDoesNotResurrectRevokedPreviousPrefixes(t *testing.T) {
	now := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	previous := routerstate.PDLease{LastPrefix: "2001:db8:1200:1240::/60", LastReplyAt: now.Add(-time.Minute), ValidSeconds: 7200, PreviousPrefixes: []routerstate.PDPreviousPrefix{{Prefix: "2001:db8:1200:1240::/60", ExpiresAt: now.Add(time.Hour)}}}
	snapshot := pdclient.Snapshot{State: pdclient.StateBound, CurrentPrefix: "2001:db8:1200:1220::/60", UpdatedAt: now, Prefixes: []pdclient.PrefixSnapshot{{Prefix: "2001:db8:1200:1220::/60", AcquiredAt: now, Preferred: 14400, Valid: 14400}, {Prefix: "2001:db8:1200:1230::/60", AcquiredAt: now, Preferred: 1800, Valid: 3600}}}
	merged := routerstate.ObservePDLease(previous, snapshot, snapshot.UpdatedAt)
	if got := merged.ValidPreviousPrefixes(now); len(got) != 1 || got[0].Prefix != "2001:db8:1200:1230::/60" {
		t.Fatalf("daemon revocation was replaced by old history: %+v", got)
	}
	snapshot.Prefixes = snapshot.Prefixes[:1]
	snapshot.UpdatedAt = now.Add(time.Minute)
	merged = routerstate.ObservePDLease(merged, snapshot, snapshot.UpdatedAt)
	if got := merged.ValidPreviousPrefixes(now.Add(time.Minute)); len(got) != 0 {
		t.Fatalf("omitted daemon ownership was reintroduced: %+v", got)
	}
}

func TestRecordPrefixDelegationStateClearsIdentityWhenConfiguredDUIDChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = store.Close() }()
	if _, err := store.BeginGeneration("test"); err != nil {
		t.Fatalf("begin generation: %v", err)
	}
	lease, _, err := store.ConfigurePDLease("wan-pd", routerstate.PDLeaseConfig{Client: api.IPv6PDClientRouterd, Profile: api.IPv6PDProfileDefault, UplinkIfname: "ens18", ClientDUID: "00030001020000000001", IAID: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease.LastPrefix = "2001:db8:3d60:1240::/60"
	lease.DUID = "00030001020000000001"
	lease.DUIDText = "00:03:00:01:02:00:00:00:00:01"
	lease.IAID = "3394439514"
	if err := store.SavePDLease("wan-pd", lease); err != nil {
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
			Spec: api.DHCPv6PrefixDelegationSpec{
				Interface:  "wan",
				Client:     api.IPv6PDClientRouterd,
				Profile:    api.IPv6PDProfileDefault,
				ClientDUID: "00030001020000000002",
			},
		},
	}}}
	if err := recordObservedPrefixDelegationState(router, store, bus.NewWithStore(store)); err != nil {
		t.Fatalf("record PD state: %v", err)
	}
	lease, ok, readErr := store.ReadPDLease("wan-pd")
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !ok {
		t.Fatal("lease missing")
	}
	if lease.DUID != "" || lease.DUIDText != "" || lease.IAID != "" {
		t.Fatalf("identity was not cleared: %+v", lease)
	}
	if lease.LastPrefix != "2001:db8:3d60:1240::/60" {
		t.Fatalf("last prefix = %q, want preserved", lease.LastPrefix)
	}
	if got := lease.Client; got != api.IPv6PDClientRouterd {
		t.Fatalf("client = %q, want routerd-dhcpv6-client", got)
	}
	events, err := store.ListEvents(routerstate.EventQuery{Topic: "routerd.resource.status.changed"})
	if err != nil || len(events) != 1 || events[0].ResourceName != "wan-pd" {
		t.Fatalf("atomic PD source transition missing: %+v %v", events, err)
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
	if err := recordHostInventoryState(context.Background(), store); err != nil {
		t.Fatalf("record inventory: %v", err)
	}
	status := store.ObjectStatus(api.RouterAPIVersion, "Inventory", "host")
	if status == nil || status["os"] == nil || status["commands"] == nil {
		t.Fatalf("inventory status = %#v", status)
	}
	events, err := store.ReadEvents(t.Context(), api.RouterAPIVersion, "Inventory", "host", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Reason != "InventoryObserved" {
		t.Fatalf("events = %#v", events)
	}
	if err := recordHostInventoryState(context.Background(), store); err != nil {
		t.Fatalf("record inventory again: %v", err)
	}
	events, err = store.ReadEvents(t.Context(), api.RouterAPIVersion, "Inventory", "host", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("unchanged inventory should not add event: %#v", events)
	}
}

func testResourceWithSpecWhen(kind string, spec api.ResourceSpec) api.Resource {
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

	filtered := mustWhenFilteredRouter(t, false, router, routerstate.New())
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
	filtered := mustWhenFilteredRouter(t, false, router, store)
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

	filtered := mustWhenFilteredRouter(t, false, router, routerstate.New())
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

// setMissingSAMForwardChainIPTables supplies the ordinary iptables response
// for a router with no routerd-owned SAM chain. Empty-desired reconciliation
// intentionally probes ownership and must still fail on real permission or
// parse errors; these command tests exercise a config with no host chain.
func setMissingSAMForwardChainIPTables(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	ipTables := filepath.Join(binDir, "iptables")
	const script = "#!/bin/sh\nprintf '%s\\n' 'iptables: No chain/target/match by that name.' >&2\nexit 1\n"
	if err := os.WriteFile(ipTables, []byte(script), 0755); err != nil {
		t.Fatalf("write iptables fake: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPDObservationPropagatesPersistenceFailure(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "routerd15-closed-write-fixture"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan", ClientDUID: "00030001020000000001"}},
	}}}
	err = recordObservedPrefixDelegationState(router, store, nil)
	if !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("failed PD observation was acknowledged: %v", err)
	}
}

func (s *fakeStaleCleanupStore) ReadVariable(name string) (routerstate.Value, error) {
	return s.Get(name), nil
}

func (s *fakeStaleCleanupStore) ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error) {
	statuses, err := s.ListObjectStatuses()
	if err != nil {
		return nil, err
	}
	for _, status := range statuses {
		if status.APIVersion == apiVersion && status.Kind == kind && status.Name == name {
			return status.Status, nil
		}
	}
	return nil, nil
}
