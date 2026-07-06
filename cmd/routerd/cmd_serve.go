// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	mobilitycontroller "github.com/imksoo/routerd/pkg/controller/mobility"
	provideractioncontroller "github.com/imksoo/routerd/pkg/controller/provideraction"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventlog"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
	statuswriter "github.com/imksoo/routerd/pkg/status"
	"github.com/imksoo/routerd/pkg/webconsole"
)

func filterControllerDefaultStatuses(statuses []controlapi.ControllerStatus, enabled []string) []controlapi.ControllerStatus {
	if len(enabled) == 0 {
		return statuses
	}
	allowed := make(map[string]struct{}, len(enabled))
	for _, name := range enabled {
		name = strings.TrimSpace(name)
		if name == "" || name == "all" {
			return statuses
		}
		allowed[name] = struct{}{}
	}
	out := make([]controlapi.ControllerStatus, 0, len(statuses))
	for _, status := range statuses {
		if _, ok := allowed[status.Name]; ok {
			out = append(out, status)
		}
	}
	return out
}

func controllerResourceKinds(name string) []string {
	switch name {
	case "address":
		return []string{"IPv4StaticAddress", "IPv6DelegatedAddress", "IPv6RAAddress"}
	case "dhcpv4client":
		return []string{"DHCPv4Client"}
	case "dhcpv6":
		return []string{"DHCPv6Server", "IPv6RouterAdvertisement"}
	case "dhcp-lease-sync":
		return []string{"DHCPv4ServerLeaseSync", "DHCPv6ServerLeaseSync", "DHCPv6PrefixDelegationLeaseSync"}
	case "nat44-session-sync":
		return []string{"NAT44SessionSync"}
	case "dns-resolver":
		return []string{"DNSResolver", "DNSForwarder", "DNSUpstream", "DNSZone"}
	case "dslite":
		return []string{"DSLiteTunnel"}
	case "firewall":
		return []string{"FirewallZone", "FirewallPolicy", "FirewallRule", "FirewallFlowPinhole", "ClientPolicy", "PortForward", "IngressService", "IPAddressSet", "LocalServiceRedirect"}
	case "ingress":
		return []string{"IngressService"}
	case "bgp":
		return []string{"BGPRouter", "BGPPeer", "BFD"}
	case "vrrp":
		return []string{"VirtualAddress"}
	case "nat":
		return []string{"NAT44Rule", "NAT44FlowDNATPinhole", "PortForward", "IngressService", "IPAddressSet", "LocalServiceRedirect"}
	case "network-adoption":
		return []string{"NetworkAdoption"}
	case "package":
		return []string{"Package"}
	case "kernel-module":
		return []string{"KernelModule"}
	case "pppoesession":
		return []string{"PPPoESession"}
	case "route":
		return []string{"IPv4Route", "IPv4StaticRoute", "IPv6StaticRoute", "ClusterNetworkRoute", "EgressRoutePolicy"}
	case "service-unit":
		return []string{"ServiceUnit", "TailscaleNode", "HealthCheck", "FirewallEventLog", "TrafficFlowLog"}
	default:
		return nil
	}
}

func publishControllerModeEvents(ctx context.Context, b *bus.Bus, controllers []controlapi.ControllerStatus) {
	if b == nil {
		return
	}
	for _, controller := range controllers {
		severity := daemonapi.SeverityInfo
		if controller.Mode == "dry-run" {
			severity = daemonapi.SeverityWarning
		}
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.controller.mode.changed", severity)
		event.Attributes = map[string]string{
			"controller":    controller.Name,
			"mode":          controller.Mode,
			"previousMode":  "unknown",
			"reason":        string(controller.Reason),
			"message":       controller.Message,
			"resourceKinds": strings.Join(controller.ResourceKinds, ","),
		}
		_ = b.Publish(ctx, event)
	}
}

var cleanupLedgerOwnedOrphansForServe = cleanupLedgerOwnedOrphans
var ensureLoopbackUpForServe = ensureLoopbackUp

func ensureLoopbackUp() error {
	if runtime.GOOS != "linux" {
		return nil
	}
	return exec.Command("ip", "link", "set", "lo", "up").Run()
}

func cleanupServeLedgerOwnedOrphans(router *api.Router, ledgerPath string, logger *eventlog.Logger) ([]string, error) {
	removed, err := cleanupLedgerOwnedOrphansForServe(router, ledgerPath)
	if err != nil {
		if logger != nil {
			logger.Emit(eventlog.LevelWarning, "serve", "ledger orphan cleanup encountered an error", map[string]string{"error": err.Error()})
		}
		return nil, err
	}
	if len(removed) > 0 && logger != nil {
		logger.Emit(eventlog.LevelInfo, "serve", "removed ledger-owned orphaned artifacts", map[string]string{
			"count":     strconv.Itoa(len(removed)),
			"artifacts": strings.Join(removed, ","),
		})
	}
	return removed, nil
}

type serveBootFallback struct {
	Used           bool
	Generation     int64
	CanonicalError error
}

func loadServeRouter(configPath string, store routerstate.GenerationHistoryReader) (*api.Router, serveBootFallback, error) {
	router, err := config.Load(configPath)
	if err == nil {
		if validateErr := config.Validate(router); validateErr == nil {
			return router, serveBootFallback{}, nil
		} else {
			err = fmt.Errorf("validate config %s: %w", configPath, validateErr)
		}
	}
	fallbackRouter, generation, fallbackErr := lastGoodServeRouter(store)
	if fallbackErr != nil {
		return nil, serveBootFallback{}, fmt.Errorf("load canonical config failed (%v); last-good fallback failed: %w", err, fallbackErr)
	}
	return fallbackRouter, serveBootFallback{Used: true, Generation: generation, CanonicalError: err}, nil
}

func lastGoodServeRouter(store routerstate.GenerationHistoryReader) (*api.Router, int64, error) {
	if store == nil {
		return nil, 0, errors.New("state store is unavailable")
	}
	records, err := store.ListGenerations(1000)
	if err != nil {
		return nil, 0, err
	}
	var skipped []string
	for _, rec := range records {
		if !rec.HasYAML || !configCommitPhase(rec.Phase) {
			continue
		}
		configYAML, ok, err := store.GenerationConfig(rec.Generation)
		if err != nil {
			return nil, 0, err
		}
		if !ok {
			continue
		}
		source := fmt.Sprintf("generation %d", rec.Generation)
		router, err := config.LoadBytes([]byte(configYAML), source)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%d: %v", rec.Generation, err))
			continue
		}
		if err := config.Validate(router); err != nil {
			skipped = append(skipped, fmt.Sprintf("%d: validate config %s: %v", rec.Generation, source, err))
			continue
		}
		return router, rec.Generation, nil
	}
	if len(skipped) > 0 {
		return nil, 0, fmt.Errorf("no valid last-good config generation found; skipped invalid generation(s): %s", strings.Join(skipped, "; "))
	}
	return nil, 0, errors.New("no last-good config generation found")
}

func emitServeBootFallbackWarning(stderr io.Writer, logger *eventlog.Logger, configPath string, fallback serveBootFallback) {
	if !fallback.Used {
		return
	}
	message := fmt.Sprintf("routerd serve could not load canonical config %s (%v); booting from last-good generation %d. Fix the canonical config and apply a valid candidate.", configPath, fallback.CanonicalError, fallback.Generation)
	if stderr != nil {
		fmt.Fprintf(stderr, "WARNING: %s\n", message)
	}
	if logger != nil {
		logger.Emit(eventlog.LevelWarning, "serve", message, map[string]string{
			"config":     configPath,
			"generation": strconv.FormatInt(fallback.Generation, 10),
		})
	}
}

func startPeerGroupSyncServer(ctx context.Context, store *routerstate.SQLiteStore, logger *eventlog.Logger) error {
	listener, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", strconv.Itoa(mobilitycontroller.PeerGroupSyncPort)))
	if err != nil {
		return fmt.Errorf("listen peer-group sync: %w", err)
	}
	server := &http.Server{Handler: mobilitycontroller.NewPeerGroupSyncServer(store)}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		if logger != nil {
			logger.Emit(eventlog.LevelInfo, "serve", "peer-group sync server listening", map[string]string{"listen": listener.Addr().String()})
		}
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed && logger != nil {
			logger.Emit(eventlog.LevelWarning, "serve", "peer-group sync server stopped", map[string]string{"error": err.Error()})
		}
	}()
	return nil
}

var legacyServeBoolFlags = []string{
	"controller-chain",
	"controller-chain-dry-run-address",
	"controller-chain-dry-run-dhcpv4lease",
	"controller-chain-dry-run-dhcpv6",
	"controller-chain-dry-run-dns-resolver",
	"controller-chain-dry-run-dslite",
	"controller-chain-dry-run-firewall",
	"controller-chain-dry-run-nat",
	"controller-chain-dry-run-network-adoption",
	"controller-chain-dry-run-package",
	"controller-chain-dry-run-ra",
	"controller-chain-dry-run-route",
	"controller-chain-dry-run-systemd-unit",
}

var legacyServeStringFlags = []string{
	"observe-interval",
	"controller-chain-daemon-sockets",
	"controller-chain-dnsmasq-command",
	"controller-chain-dnsmasq-config",
	"controller-chain-dnsmasq-listen-addresses",
	"controller-chain-dnsmasq-pid",
	"controller-chain-dnsmasq-port",
}

type repeatedStringFlag []string

func (f *repeatedStringFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

func (f *repeatedStringFlag) Set(value string) error {
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			*f = append(*f, part)
		}
	}
	return nil
}

type controlAPIHTTPConfig struct {
	Enabled       bool
	Listen        string
	AllowPrefixes []netip.Prefix
	Token         string
	TLS           controlAPITLSConfig
}

type controlAPITLSConfig struct {
	CertFile     string
	KeyFile      string
	ClientCAFile string
}

const (
	defaultControlAPIListenAddress = "127.0.0.1"
	defaultControlAPIPort          = 65432
)

func registerLegacyServeFlags(fs *flag.FlagSet) {
	for _, name := range legacyServeBoolFlags {
		fs.Bool(name, false, "ignored legacy controller-chain compatibility flag")
	}
	for _, name := range legacyServeStringFlags {
		fs.String(name, "", "ignored legacy controller-chain compatibility flag")
	}
}

func ignoredLegacyServeFlagNames(setFlags map[string]bool) []string {
	ignored := make([]string, 0, len(legacyServeBoolFlags)+len(legacyServeStringFlags))
	for _, name := range legacyServeBoolFlags {
		if setFlags[name] {
			ignored = append(ignored, "--"+name)
		}
	}
	for _, name := range legacyServeStringFlags {
		if setFlags[name] {
			ignored = append(ignored, "--"+name)
		}
	}
	sort.Strings(ignored)
	return ignored
}

func warnIgnoredLegacyServeFlags(stderr io.Writer, setFlags map[string]bool) {
	ignored := ignoredLegacyServeFlagNames(setFlags)
	if len(ignored) == 0 || stderr == nil {
		return
	}
	fmt.Fprintf(stderr, "warning: routerd serve ignored legacy flag(s): %s\n", strings.Join(ignored, ", "))
}

func serveCommand(args []string, stdout, stderr io.Writer) (err error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	statusFile := fs.String("status-file", defaultStatusFile(), "status file")
	socketPath := fs.String("socket", defaultSocketPath(), "Unix domain socket path")
	statusSocketPath := fs.String("status-socket", defaultStatusSocketPath(), "read-only status Unix domain socket path")
	httpListen := fs.String("http-listen", "", "TCP listen address for the mutation/control API; defaults to 127.0.0.1:65432")
	var httpAllowCIDRs repeatedStringFlag
	fs.Var(&httpAllowCIDRs, "http-allow-cidr", "source CIDR allowed to use --http-listen; repeat or comma-separate; defaults to loopback only")
	httpTokenFile := fs.String("http-token-file", "", "file containing bearer token required by the HTTP mutation/control API")
	httpTokenEnv := fs.String("http-token-env", "", "environment variable containing bearer token required by the HTTP mutation/control API")
	httpTLSCertFile := fs.String("http-tls-cert-file", "", "TLS certificate file for the HTTP mutation/control API")
	httpTLSKeyFile := fs.String("http-tls-key-file", "", "TLS private key file for the HTTP mutation/control API")
	httpTLSClientCAFile := fs.String("http-tls-client-ca-file", "", "CA bundle for requiring and verifying HTTP ControlAPI client certificates")
	statePath := fs.String("state-file", defaultStatePath, "routerd state database file")
	controllerNames := fs.String("controllers", "all", "comma-separated controller names to run; use bgp for isolated BGP labs")
	applyInterval := fs.Duration("apply-interval", 0, "periodic apply interval; 0 disables scheduled apply")
	netplanPath := fs.String("netplan-file", defaultNetplanPath, "routerd-managed netplan file")
	dnsmasqConfigPath := fs.String("dnsmasq-file", defaultDnsmasqConfigPath, "routerd-managed dnsmasq config file")
	dnsmasqServicePath := fs.String("dnsmasq-service-file", defaultDnsmasqServicePath, "routerd-managed dnsmasq systemd unit file")
	nftablesPath := fs.String("nftables-file", defaultNftablesPath, "routerd-managed nftables ruleset file")
	ledgerPath := fs.String("ledger-file", defaultLedgerPath, "routerd ownership ledger file")
	bgpSocketPath := fs.String("bgp-socket", "/run/routerd/bgp/gobgp.sock", "routerd-bgp GoBGP gRPC Unix socket path")
	bgpControlSocketPath := fs.String("bgp-control-socket", "", "routerd-bgp control Unix socket path")
	bgpStatePath := fs.String("bgp-state-file", "", "routerd-bgp applied state JSON path")
	gracefulStopTimeout := fs.Duration("graceful-stop-timeout", 20*time.Second, "wait up to this duration for mobility make-before-break handoff on SIGTERM/SIGINT; 0 disables")
	once := fs.Bool("once", false, "converge once and exit without serving control sockets")
	sandbox := fs.Bool("sandbox", false, "serve control API in a dry-run sandbox with no host mutation")
	sandboxRoot := fs.String("root", "", "sandbox root directory used with --sandbox")
	registerLegacyServeFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})
	warnIgnoredLegacyServeFlags(stderr, setFlags)
	if *sandbox {
		if err := configureServeSandbox(
			*sandboxRoot,
			setFlags,
			configPath,
			statusFile,
			socketPath,
			statusSocketPath,
			statePath,
			netplanPath,
			dnsmasqConfigPath,
			dnsmasqServicePath,
			nftablesPath,
			ledgerPath,
			bgpSocketPath,
			bgpControlSocketPath,
			bgpStatePath,
		); err != nil {
			return err
		}
	}
	if *statusSocketPath == *socketPath {
		return errors.New("--status-socket must differ from --socket")
	}
	enabledControllers := parseControllerNames(*controllerNames)
	controllerStatuses := filterControllerDefaultStatuses(controllerDefaultStatuses(), enabledControllers)
	controllerRuntime := controlapi.NewControllerRuntimeStore(controllerStatuses)
	var stateStore *routerstate.SQLiteStore
	stateStore, err = routerstate.OpenSQLite(*statePath)
	if err != nil {
		return err
	}
	defer stateStore.Close()
	router, bootFallback, err := loadServeRouter(*configPath, stateStore)
	if err != nil {
		return err
	}
	httpControl, err := resolveControlAPIHTTPConfig(router, strings.TrimSpace(*httpListen), []string(httpAllowCIDRs), api.SecretValueSourceSpec{File: strings.TrimSpace(*httpTokenFile), Env: strings.TrimSpace(*httpTokenEnv)}, controlAPITLSConfig{CertFile: strings.TrimSpace(*httpTLSCertFile), KeyFile: strings.TrimSpace(*httpTLSKeyFile), ClientCAFile: strings.TrimSpace(*httpTLSClientCAFile)}, setFlags)
	if err != nil {
		return err
	}
	routerMu := &sync.RWMutex{}
	var chainRunner *controllerchain.Runner
	currentRouter := func() *api.Router {
		routerMu.RLock()
		defer routerMu.RUnlock()
		return router
	}
	setCurrentRouter := func(next *api.Router) {
		routerMu.Lock()
		defer routerMu.Unlock()
		router = next
		if chainRunner != nil {
			chainRunner.Router = next
		}
	}
	logger, err := eventlog.New(router)
	if err != nil {
		return err
	}
	defer closeLogger(logger, "serve", &err)
	emitServeBootFallbackWarning(stderr, logger, *configPath, bootFallback)
	logger.Emit(eventlog.LevelInfo, "serve", "routerd daemon starting", map[string]string{
		"config":        *configPath,
		"socket":        *socketPath,
		"statusSocket":  *statusSocketPath,
		"applyInterval": applyInterval.String(),
	})
	if !*sandbox {
		if loopbackErr := ensureLoopbackUpForServe(); loopbackErr != nil {
			logger.Emit(eventlog.LevelWarning, "serve", "failed to ensure loopback is up", map[string]string{"error": loopbackErr.Error()})
		}
	}
	applyOpts := applyOptions{
		ConfigPath:         *configPath,
		StatusFile:         *statusFile,
		NetplanPath:        *netplanPath,
		DnsmasqConfigPath:  *dnsmasqConfigPath,
		DnsmasqServicePath: runtimeDnsmasqServicePath(*dnsmasqServicePath),
		NftablesPath:       *nftablesPath,
		LedgerPath:         *ledgerPath,
		StatePath:          *statePath,
		SkipConfigCommit:   bootFallback.Used,
		DryRun:             *sandbox,
		SkipServiceManager: *sandbox,
		Sandbox:            *sandbox,
	}
	cache := &resultCache{}

	signalCtx, cancelSignalCtx := context.WithCancel(context.Background())
	defer cancelSignalCtx()
	signalCh := make(chan os.Signal, 2)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalCh)
	stop := make(chan struct{})
	var stopOnce sync.Once
	closeStop := func() {
		stopOnce.Do(func() { close(stop) })
	}
	defer closeStop()
	ctx, cancelControllers := context.WithCancel(signalCtx)
	defer cancelControllers()
	go func() {
		<-stop
		cancelControllers()
	}()
	var controllerBus *bus.Bus
	if _, cleanupErr := cleanupUnsupportedLegacyObjectStatuses(router, stateStore, *statePath, time.Now().UTC(), logger); cleanupErr != nil {
		logger.Emit(eventlog.LevelWarning, "serve", "stale state cleanup encountered an error", map[string]string{"error": cleanupErr.Error()})
	}
	_, _ = cleanupServeLedgerOwnedOrphans(router, *ledgerPath, logger)
	engine := apply.New()
	if result, observeErr := engine.Observe(router); observeErr == nil {
		cache.Store(result)
		_ = statuswriter.Write(*statusFile, result)
	} else {
		logger.Emit(eventlog.LevelWarning, "serve", "initial observe failed", map[string]string{"error": observeErr.Error()})
	}
	controllerBus = bus.NewWithStore(stateStore)
	controllerBus.SetLogger(slog.Default())
	publishControllerModeEvents(ctx, controllerBus, controllerStatuses)
	peerGroupSyncClient := mobilitycontroller.NewPeerGroupSyncClient(stateStore)
	if !*once && !*sandbox && (mobilitycontroller.HasPublishedPeerGroups(router) || mobilitycontroller.HasPublishedMemberSets(router)) {
		if err := startPeerGroupSyncServer(ctx, stateStore, logger); err != nil {
			return err
		}
	}
	controllerOpts := controllerchain.Options{
		SuperviseClientDaemons: true,
		SuperviseDNSResolvers:  false,
		DnsmasqCommand:         "dnsmasq",
		DnsmasqConfig:          "/run/routerd/dnsmasq.conf",
		DnsmasqPID:             "/run/routerd/dnsmasq.pid",
		DnsmasqPort:            53,
		DnsmasqListen:          []string{"127.0.0.1"},
		NftablesPath:           "/run/routerd/nat44.nft",
		FirewallPath:           "/run/routerd/firewall.nft",
		LedgerPath:             *ledgerPath,
		NftCommand:             "nft",
		BGPSocketPath:          *bgpSocketPath,
		BGPControlSocketPath:   *bgpControlSocketPath,
		BGPStatePath:           *bgpStatePath,
		ConntrackInterval:      30 * time.Second,
		ControllerObserver:     controllerRuntime,
		EnabledControllers:     enabledControllers,
		PeerGroupSyncClient:    peerGroupSyncClient,
		MemberSetSyncClient:    peerGroupSyncClient,
	}
	if *sandbox {
		applySandboxControllerOptions(&controllerOpts, *dnsmasqConfigPath, *nftablesPath)
	}
	chainRunner = &controllerchain.Runner{
		Router: router,
		Bus:    controllerBus,
		Store:  stateStore,
		Opts:   controllerOpts,
	}
	if *once {
		_, err := runServeChainOnce(ctx, chainRunner, router, applyOpts, stateStore, stdout, logger)
		return err
	}
	if err := chainRunner.Start(ctx); err != nil {
		return err
	}
	go func() {
		sig, ok := <-signalCh
		if !ok {
			return
		}
		logger.Emit(eventlog.LevelInfo, "serve", "routerd daemon stopping", map[string]string{"signal": sig.String()})
		if !*sandbox && *gracefulStopTimeout > 0 {
			handoffCtx, handoffCancel := context.WithTimeout(context.Background(), *gracefulStopTimeout+5*time.Second)
			err := runGracefulStopHandoff(handoffCtx, currentRouter(), stateStore, gracefulStopOptions{
				Timeout:          *gracefulStopTimeout,
				PollInterval:     time.Second,
				BGPPaths:         bgpdaemon.NewControlClient(controllerOpts.BGPControlSocketPath),
				MemberSetSync:    controllerOpts.MemberSetSyncClient,
				ProviderAction:   provideractioncontroller.Controller{Bus: controllerBus, Runner: controllerOpts.ProviderActionRunner, DryRun: controllerOpts.DryRunProviderAction},
				Logger:           logger,
				ControllerLogger: controllerOpts.Logger,
			})
			handoffCancel()
			if err != nil {
				logger.Emit(eventlog.LevelWarning, "serve", "graceful mobility stop did not complete", map[string]string{"error": err.Error()})
			}
		}
		closeStop()
		cancelSignalCtx()
		select {
		case sig := <-signalCh:
			logger.Emit(eventlog.LevelWarning, "serve", "second stop signal received; forcing shutdown", map[string]string{"signal": sig.String()})
			closeStop()
			cancelSignalCtx()
		default:
		}
	}()
	mutator := serveConfigMutator{
		configPath: *configPath,
		statePath:  *statePath,
		baseOpts:   applyOpts,
		cache:      cache,
		logger:     logger,
		getRouter:  currentRouter,
		setRouter:  setCurrentRouter,
	}
	applyMu := &sync.Mutex{}
	if *applyInterval > 0 {
		go runApplySchedule(stop, *applyInterval, currentRouter, applyOpts, cache, logger, applyMu)
	}
	if webConsoleResourcePresent(router) {
		var webStore routerstate.Store
		var webObjectStore routerstate.ObjectStatusStore
		if stateStore != nil {
			webStore = stateStore
			webObjectStore = stateStore
		} else {
			opened, openErr := routerstate.OpenSQLite(*statePath)
			if openErr != nil {
				return openErr
			}
			defer opened.Close()
			webStore = opened
			webObjectStore = opened
		}
		console, ok, webErr := webConsoleFromRouter(router, webObjectStore)
		if webErr != nil {
			return webErr
		}
		if ok {
			if err := startWebConsole(ctx, console, router, webStore, controllerBus, cache, logger, *configPath, controllerRuntime.Snapshot, configuredDHCPLeasePaths("/run/routerd/dnsmasq.conf")); err != nil {
				return err
			}
		}
	}

	listener, err := listenUnixSocket(*socketPath, 0o660)
	if err != nil {
		return err
	}
	groupOwnMutationSocket(*socketPath)
	defer listener.Close()

	statusHandler := func(r *http.Request) (*controlapi.Status, error) {
		status := controlapi.NewStatus(resultWithLatestGeneration(cache.Load(), stateStore))
		status.Status.Phase = overallStatusPhase(status.Status.Phase, stateStore)
		status.Status.ResourcePhaseIssues = resourcePhaseIssues(stateStore)
		controllers := controllerRuntime.Snapshot()
		if stateStore != nil {
			controllers = augmentControllerStatusesFromState(controllers, stateStore)
		}
		status.Status.Controllers = controllers
		return &status, nil
	}
	controllersHandler := func(r *http.Request) (*controlapi.Controllers, error) {
		statuses := controllerRuntime.Snapshot()
		if stateStore != nil {
			statuses = augmentControllerStatusesFromState(statuses, stateStore)
		}
		controllers := controlapi.NewControllers(statuses)
		return &controllers, nil
	}
	runtimeHandler := func(r *http.Request) (*controlapi.RuntimeStats, error) {
		stats := collectRuntimeStats()
		return &stats, nil
	}
	handler := controlapi.Handler{
		Status:      statusHandler,
		Controllers: controllersHandler,
		Runtime:     runtimeHandler,
		Get:         serveGetHandler(currentRouter, stateStore, statusHandler, controllersHandler, runtimeHandler),
		Describe:    serveDescribeHandler(currentRouter, stateStore),
		Probe:       serveProbeHandler(currentRouter, stateStore),
		Connections: func(r *http.Request, req controlapi.ConnectionsRequest) (*controlapi.ConnectionTable, error) {
			table, err := observe.Connections(req.Limit)
			if err != nil {
				return nil, err
			}
			apiTable := controlapi.NewConnectionTable(table)
			return &apiTable, nil
		},
		DNSQueries: func(r *http.Request, req controlapi.DNSQueriesRequest) (*controlapi.DNSQueries, error) {
			filter, err := dnsQueryFilterFromRequest(req)
			if err != nil {
				return nil, err
			}
			rows, err := listDNSQueriesReadOnly(r.Context(), configuredDNSQueryLogPath(currentRouter()), filter)
			if err != nil {
				return nil, err
			}
			result := controlapi.NewDNSQueries(rows)
			return &result, nil
		},
		DNSQueriesAggregate: func(r *http.Request, req controlapi.DNSQueriesRequest) (*controlapi.DNSQueriesAggregate, error) {
			filter, err := dnsQueryFilterFromRequest(req)
			if err != nil {
				return nil, err
			}
			agg, err := aggregateDNSQueriesReadOnly(r.Context(), configuredDNSQueryLogPath(currentRouter()), filter)
			if err != nil {
				return nil, err
			}
			result := controlapi.NewDNSQueriesAggregate(agg)
			return &result, nil
		},
		TrafficFlows: func(r *http.Request, req controlapi.TrafficFlowsRequest) (*controlapi.TrafficFlows, error) {
			filter, err := trafficFlowFilterFromRequest(req)
			if err != nil {
				return nil, err
			}
			rows, err := listTrafficFlowsReadOnly(r.Context(), configuredTrafficFlowLogPath(currentRouter()), filter)
			if err != nil {
				return nil, err
			}
			result := controlapi.NewTrafficFlows(rows)
			return &result, nil
		},
		TrafficFlowsAggregate: func(r *http.Request, req controlapi.TrafficFlowsRequest) (*controlapi.TrafficFlowsAggregate, error) {
			filter, err := trafficFlowFilterFromRequest(req)
			if err != nil {
				return nil, err
			}
			agg, err := aggregateTrafficFlowsReadOnly(r.Context(), configuredTrafficFlowLogPath(currentRouter()), filter)
			if err != nil {
				return nil, err
			}
			result := controlapi.NewTrafficFlowsAggregate(agg)
			return &result, nil
		},
		FirewallLogs: func(r *http.Request, req controlapi.FirewallLogsRequest) (*controlapi.FirewallLogs, error) {
			since, err := logQuerySince(req.Since)
			if err != nil {
				return nil, err
			}
			rows, err := listFirewallLogsReadOnly(r.Context(), configuredFirewallLogPath(currentRouter()), logstore.FirewallLogFilter{Since: since, Action: req.Action, Src: req.Src, Limit: req.Limit})
			if err != nil {
				return nil, err
			}
			result := controlapi.NewFirewallLogs(rows)
			return &result, nil
		},
		Apply: func(r *http.Request, req controlapi.ApplyRequest) (*controlapi.ApplyResult, error) {
			applyMu.Lock()
			defer applyMu.Unlock()
			return mutator.apply(r, req)
		},
		Plan: func(r *http.Request, req controlapi.PlanRequest) (*controlapi.PlanResult, error) {
			applyMu.Lock()
			defer applyMu.Unlock()
			return mutator.plan(r, req)
		},
		Delete: func(r *http.Request, req controlapi.DeleteRequest) (*controlapi.DeleteResult, error) {
			applyMu.Lock()
			defer applyMu.Unlock()
			return mutator.delete(r, req)
		},
		Validate: func(r *http.Request, req controlapi.ValidateRequest) (*controlapi.ValidateResult, error) {
			applyMu.Lock()
			defer applyMu.Unlock()
			return mutator.validate(r, req)
		},
		SubmitSAMEnrollmentClaim: func(r *http.Request, req controlapi.SAMEnrollmentClaimSubmitRequest) (*controlapi.SAMEnrollmentClaimSubmitResult, error) {
			applyMu.Lock()
			defer applyMu.Unlock()
			result, err := submitSAMEnrollmentClaim(currentRouter(), stateStore, req, time.Now().UTC())
			if err != nil {
				return nil, err
			}
			if controllerBus != nil {
				event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd"}, "routerd.resource.status.changed", daemonapi.SeverityInfo)
				event.Attributes = map[string]string{
					"resource":      result.ClaimRef,
					"dynamicSource": result.DynamicSource,
					"reason":        "sam-enrollment-claim-submitted",
				}
				_ = controllerBus.Publish(r.Context(), event)
			}
			logger.Emit(eventlog.LevelInfo, "sam-enrollment", "accepted SAMEnrollmentClaim", map[string]string{
				"claim":         result.ClaimRef,
				"dynamicSource": result.DynamicSource,
			})
			return result, nil
		},
		RevokeSAMEnrollmentClaim: func(r *http.Request, req controlapi.SAMEnrollmentClaimRevokeRequest) (*controlapi.SAMEnrollmentClaimRevokeResult, error) {
			applyMu.Lock()
			defer applyMu.Unlock()
			result, err := revokeSAMEnrollmentClaim(currentRouter(), stateStore, req, time.Now().UTC())
			if err != nil {
				return nil, err
			}
			if controllerBus != nil {
				event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd"}, "routerd.resource.status.changed", daemonapi.SeverityInfo)
				event.Attributes = map[string]string{
					"resource":      result.ClaimRef,
					"dynamicSource": result.DynamicSource,
					"reason":        "sam-enrollment-claim-revoked",
				}
				_ = controllerBus.Publish(r.Context(), event)
			}
			logger.Emit(eventlog.LevelWarning, "sam-enrollment", "revoked SAMEnrollmentClaim", map[string]string{
				"claim":         result.ClaimRef,
				"dynamicSource": result.DynamicSource,
				"reason":        result.Reason,
			})
			return result, nil
		},
		GetSAMRRSet: func(r *http.Request, req controlapi.SAMRRSetGetRequest) (*controlapi.SAMRRSetGetResult, error) {
			applyMu.Lock()
			defer applyMu.Unlock()
			return getSAMRRSetForAcceptedClaim(currentRouter(), stateStore, req, time.Now().UTC())
		},
		SetLogLevel: func(r *http.Request, req controlapi.LogLevelRequest) (*controlapi.LogLevelResult, error) {
			level := strings.TrimSpace(req.Level)
			effective := "default"
			switch level {
			case "", "default":
				eventlog.SetLevelOverride(nil)
			case "debug", "info", "warning", "error":
				override := eventlog.Level(level)
				eventlog.SetLevelOverride(&override)
				effective = string(override)
			default:
				return nil, fmt.Errorf("%w: unsupported log level %q", controlapi.ErrBadRequest, req.Level)
			}
			logger.Emit(eventlog.LevelInfo, "serve", "log level override changed", map[string]string{"level": effective})
			result := controlapi.NewLogLevelResult(effective)
			return &result, nil
		},
		DHCPLeaseEvent: func(r *http.Request, req controlapi.DHCPLeaseEventRequest) (*controlapi.DHCPLeaseEventResult, error) {
			if req.Action == "" || req.IP == "" {
				return nil, controlapi.ErrBadRequest
			}
			if holdDays := dhcpStickyHoldDays(currentRouter(), req.IP); holdDays > 0 {
				stickyLog, err := logstore.OpenDHCPStickyLog(dhcpStickyLogPath())
				if err != nil {
					return nil, err
				}
				defer stickyLog.Close()
				if err := stickyLog.RecordLeaseEvent(r.Context(), req.Action, req.MAC, req.IP, req.Hostname, holdDays, time.Now().UTC()); err != nil {
					return nil, err
				}
			}
			if controllerBus != nil {
				topic := "routerd.dhcp.lease." + req.Action
				event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd-dhcp-event-relay", Kind: "routerd-dhcp-event-relay"}, topic, daemonapi.SeverityInfo)
				event.Attributes = map[string]string{"mac": req.MAC, "ip": req.IP, "hostname": req.Hostname}
				_ = controllerBus.Publish(r.Context(), event)
			}
			result := controlapi.NewDHCPLeaseEventResult()
			return &result, nil
		},
	}
	statusListener, err := listenUnixSocket(*statusSocketPath, 0o660)
	if err != nil {
		return err
	}
	// The status API is read-only. If a routerd group exists, hand the socket
	// to it (root:routerd, 0o660) so non-root operators in that group can run
	// `routerctl get status` without sudo. Otherwise fall back to world-accessible
	// so non-root tooling keeps working. Done in-process so it does not depend
	// on the unit's Group= setting.
	groupOwnStatusSocket(*statusSocketPath)
	defer statusListener.Close()
	// Issue #40 (follow-up to .0244): IdleTimeout only fires when the
	// connection is *idle*. Polling clients that fire every <120 s keep
	// the keep-alive connection technically non-idle, so an idle timeout
	// of 2 m never triggers. The end result was the same fd accumulation
	// .0244 was supposed to fix: routerd.db fds flat at 4, but all_fd
	// climbing ~+4 per minute. Disable HTTP keep-alives on both internal
	// API servers so every request gets a fresh accept that closes
	// immediately after the response. Unix-socket accept is cheap; this
	// trades a tiny per-request cost for a hard guarantee that fd count
	// cannot drift upward over long uptime. Read/write/idle timeouts are
	// kept as belt-and-suspenders for malformed peers.
	statusServer := &http.Server{
		Handler: controlapi.Handler{
			Status:       handler.Status,
			Controllers:  handler.Controllers,
			Runtime:      handler.Runtime,
			Get:          handler.Get,
			Describe:     handler.Describe,
			Probe:        handler.Probe,
			Connections:  handler.Connections,
			DNSQueries:   handler.DNSQueries,
			TrafficFlows: handler.TrafficFlows,
			FirewallLogs: handler.FirewallLogs,
			Plan:         handler.Plan,
			Validate:     handler.Validate,
		},
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	statusServer.SetKeepAlivesEnabled(false)
	defer statusServer.Close()
	go func() {
		if serveErr := statusServer.Serve(statusListener); serveErr != nil && serveErr != http.ErrServerClosed {
			logger.Emit(eventlog.LevelError, "serve", "read-only status API stopped", map[string]string{"error": serveErr.Error()})
		}
	}()
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	server.SetKeepAlivesEnabled(false)
	var httpServer *http.Server
	if httpControl.Enabled {
		httpListener, err := net.Listen("tcp", httpControl.Listen)
		if err != nil {
			return fmt.Errorf("listen control HTTP API: %w", err)
		}
		defer httpListener.Close()
		if httpControl.TLS.CertFile != "" {
			tlsConfig, err := controlAPIServerTLSConfig(httpControl.TLS)
			if err != nil {
				return fmt.Errorf("configure control HTTP API TLS: %w", err)
			}
			httpListener = tls.NewListener(httpListener, tlsConfig)
		}
		httpServer = &http.Server{
			Handler:           controlAPIAdmissionHandler(handler, httpControl.AllowPrefixes, httpControl.Token),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       2 * time.Minute,
		}
		httpServer.SetKeepAlivesEnabled(false)
		defer httpServer.Close()
		go func() {
			if serveErr := httpServer.Serve(httpListener); serveErr != nil && serveErr != http.ErrServerClosed {
				logger.Emit(eventlog.LevelError, "serve", "control HTTP API stopped", map[string]string{"error": serveErr.Error(), "listen": httpControl.Listen})
			}
		}()
	}
	go func() {
		<-signalCtx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = statusServer.Shutdown(shutdownCtx)
		_ = server.Shutdown(shutdownCtx)
		if httpServer != nil {
			_ = httpServer.Shutdown(shutdownCtx)
		}
	}()
	fmt.Fprintf(stdout, "routerd serving control API on unix://%s\n", *socketPath)
	fmt.Fprintf(stdout, "routerd serving read-only status API on unix://%s\n", *statusSocketPath)
	if httpControl.Enabled {
		scheme := "http"
		if httpControl.TLS.CertFile != "" {
			scheme = "https"
		}
		fmt.Fprintf(stdout, "routerd serving control API on %s://%s\n", scheme, httpControl.Listen)
	}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

const sandboxDefaultRouterYAML = `apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: sandbox
spec:
  resources: []
`

func configureServeSandbox(root string, setFlags map[string]bool, configPath, statusFile, socketPath, statusSocketPath, statePath, netplanPath, dnsmasqConfigPath, dnsmasqServicePath, nftablesPath, ledgerPath, bgpSocketPath, bgpControlSocketPath, bgpStatePath *string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return errors.New("--sandbox requires --root <dir>")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	root = filepath.Clean(absRoot)
	sandboxDefaults := platformDefaults
	sandboxDefaults.SysconfDir = filepath.Join(root, "etc", "routerd")
	sandboxDefaults.PluginDir = filepath.Join(root, "usr", "local", "libexec", "routerd", "plugins")
	sandboxDefaults.RuntimeDir = filepath.Join(root, "run", "routerd")
	sandboxDefaults.StateDir = filepath.Join(root, "var", "lib", "routerd")
	sandboxDefaults.NetplanFile = filepath.Join(root, "etc", "netplan", "99-routerd.yaml")
	sandboxDefaults.NetworkdDropinDir = filepath.Join(root, "etc", "systemd", "network")
	sandboxDefaults.SystemdSystemDir = filepath.Join(root, "etc", "systemd", "system")
	sandboxDefaults.DnsmasqConfigFile = filepath.Join(root, "etc", "routerd", "dnsmasq.conf")
	sandboxDefaults.DnsmasqServiceFile = filepath.Join(root, "run", "routerd", "routerd-dnsmasq.service")
	sandboxDefaults.NftablesFile = filepath.Join(root, "run", "routerd", "nftables.conf")
	sandboxDefaults.DefaultRouteNftablesFile = filepath.Join(root, "run", "routerd", "default-route.nft")
	sandboxDefaults.TimesyncdDropinFile = filepath.Join(root, "etc", "systemd", "timesyncd.conf.d", "routerd.conf")
	sandboxDefaults.PPPoEChapSecretsFile = filepath.Join(root, "etc", "ppp", "chap-secrets")
	sandboxDefaults.PPPoEPapSecretsFile = filepath.Join(root, "etc", "ppp", "pap-secrets")
	platformDefaults = sandboxDefaults
	defaultNetplanPath = sandboxDefaults.NetplanFile
	defaultDnsmasqConfigPath = sandboxDefaults.DnsmasqConfigFile
	defaultDnsmasqServicePath = sandboxDefaults.DnsmasqServiceFile
	defaultNftablesPath = sandboxDefaults.NftablesFile
	defaultRouteNftablesPath = sandboxDefaults.DefaultRouteNftablesFile
	defaultTimesyncdPath = sandboxDefaults.TimesyncdDropinFile
	defaultLedgerPath = sandboxDefaults.DBFile()
	defaultStatePath = sandboxDefaults.DBFile()
	runtimeKeepalivedConfigPath = filepath.Join(root, "etc", "keepalived", "keepalived.conf")
	pppoeCHAPSecretsPath = sandboxDefaults.PPPoEChapSecretsFile
	pppoePAPSecretsPath = sandboxDefaults.PPPoEPapSecretsFile
	pdClientLeaseDir = filepath.Join(sandboxDefaults.StateDir, "dhcpv6-client")

	if !setFlags["config"] {
		*configPath = sandboxDefaults.ConfigFile()
	}
	if !setFlags["status-file"] {
		*statusFile = sandboxDefaults.StatusFile()
	}
	if !setFlags["socket"] {
		*socketPath = sandboxDefaults.SocketFile()
	}
	if !setFlags["status-socket"] {
		*statusSocketPath = sandboxDefaults.StatusSocketFile()
	}
	if !setFlags["state-file"] {
		*statePath = sandboxDefaults.DBFile()
	}
	if !setFlags["netplan-file"] {
		*netplanPath = sandboxDefaults.NetplanFile
	}
	if !setFlags["dnsmasq-file"] {
		*dnsmasqConfigPath = sandboxDefaults.DnsmasqConfigFile
	}
	if !setFlags["dnsmasq-service-file"] {
		*dnsmasqServicePath = sandboxDefaults.DnsmasqServiceFile
	}
	if !setFlags["nftables-file"] {
		*nftablesPath = sandboxDefaults.NftablesFile
	}
	if !setFlags["ledger-file"] {
		*ledgerPath = filepath.Join(sandboxDefaults.StateDir, "artifacts.json")
	}
	if !setFlags["bgp-socket"] {
		*bgpSocketPath = filepath.Join(sandboxDefaults.RuntimeDir, "bgp", "gobgp.sock")
	}
	if !setFlags["bgp-control-socket"] {
		*bgpControlSocketPath = filepath.Join(sandboxDefaults.RuntimeDir, "bgp", "control.sock")
	}
	if !setFlags["bgp-state-file"] {
		*bgpStatePath = filepath.Join(sandboxDefaults.StateDir, "bgp", "applied.json")
	}
	for _, dir := range []string{
		sandboxDefaults.SysconfDir,
		sandboxDefaults.RuntimeDir,
		sandboxDefaults.StateDir,
		filepathDir(*configPath),
		filepathDir(*statusFile),
		filepathDir(*socketPath),
		filepathDir(*statusSocketPath),
		filepathDir(*statePath),
		filepathDir(*netplanPath),
		filepathDir(*dnsmasqConfigPath),
		filepathDir(*dnsmasqServicePath),
		filepathDir(*nftablesPath),
		filepathDir(*ledgerPath),
		filepathDir(*bgpSocketPath),
		filepathDir(*bgpControlSocketPath),
		filepathDir(*bgpStatePath),
	} {
		if dir == "" || dir == "." {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if _, err := os.Stat(*configPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(*configPath, []byte(sandboxDefaultRouterYAML), 0o644); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}

func applySandboxControllerOptions(opts *controllerchain.Options, dnsmasqConfigPath, nftablesPath string) {
	opts.SuperviseClientDaemons = false
	opts.DryRunAddress = true
	opts.DryRunDSLite = true
	opts.DryRunRoute = true
	opts.DryRunDHCPv6 = true
	opts.DryRunDHCPv4Client = true
	opts.DryRunPPPoESession = true
	opts.DryRunDNSResolver = true
	opts.DryRunEventFederation = true
	opts.DryRunEventSubscription = true
	opts.DryRunLeaseSync = true
	opts.DryRunNAT44SessionSync = true
	opts.DryRunProviderAction = true
	opts.DryRunNAT = true
	opts.DryRunIngress = true
	opts.DryRunFirewall = true
	opts.DryRunBGP = true
	opts.DryRunVRRP = true
	opts.DryRunPackage = true
	opts.DryRunNetworkAdoption = true
	opts.DryRunServiceUnit = true
	opts.DnsmasqConfig = dnsmasqConfigPath
	opts.DnsmasqPID = filepath.Join(platformDefaults.RuntimeDir, "dnsmasq.pid")
	opts.NftablesPath = filepath.Join(platformDefaults.RuntimeDir, "nat44.nft")
	if strings.TrimSpace(nftablesPath) != "" {
		opts.NftablesPath = nftablesPath
	}
	opts.FirewallPath = filepath.Join(platformDefaults.RuntimeDir, "firewall.nft")
}

// groupOwnStatusSocket makes the read-only status socket reachable by members
// of the routerd group (root:routerd, 0o660). Connecting to a unix socket needs
// write access, so the group needs read+write. The socket's group owner follows
// the process egid by default, so set it explicitly instead of relying on the
// service unit's Group= setting. If the routerd group does not exist, fall back
// to world-accessible (0o666) so non-root operators are not locked out.
func groupOwnStatusSocket(path string) {
	if grp, err := user.LookupGroup("routerd"); err == nil {
		if gid, convErr := strconv.Atoi(grp.Gid); convErr == nil {
			if chErr := os.Chown(path, -1, gid); chErr == nil {
				_ = os.Chmod(path, 0o660)
				return
			}
		}
	}
	_ = os.Chmod(path, 0o666)
}

// groupOwnMutationSocket keeps the privileged mutation/control socket gated by
// filesystem permissions. If the routerd group exists, grant that group access;
// otherwise keep the socket 0660 with the process group instead of falling back
// to world-writable access.
func groupOwnMutationSocket(path string) {
	if grp, err := user.LookupGroup("routerd"); err == nil {
		if gid, convErr := strconv.Atoi(grp.Gid); convErr == nil {
			_ = os.Chown(path, -1, gid)
		}
	}
	_ = os.Chmod(path, 0o660)
}

// collectRuntimeStats samples the current process's heap, goroutine, GC, and
// file-descriptor footprint. It runs inside the live `routerd serve` process so
// runtime.ReadMemStats / runtime.NumGoroutine reflect routerd itself. All fields
// are observational and best-effort: fd counts are 0 when /proc or the rlimit
// syscall is unavailable (e.g. non-Linux), never an error.
func collectRuntimeStats() controlapi.RuntimeStats {
	stats := controlapi.NewRuntimeStats()
	stats.CollectedAt = time.Now().UTC()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	stats.HeapAllocBytes = m.HeapAlloc
	stats.HeapInuseBytes = m.HeapInuse
	stats.HeapObjects = m.HeapObjects
	stats.StackInuseBytes = m.StackInuse
	stats.SysBytes = m.Sys
	stats.NumGC = m.NumGC
	stats.GCPauseTotalNs = m.PauseTotalNs
	if m.LastGC != 0 {
		stats.LastGC = time.Unix(0, int64(m.LastGC)).UTC()
	}

	stats.NumGoroutine = runtime.NumGoroutine()

	if count, ok := openFDCount(); ok {
		stats.OpenFDs = count
	}
	stats.MaxFDs = softFDLimit()
	return stats
}

// openFDCount counts entries in /proc/self/fd. It returns (0, false) when the
// directory cannot be read (non-Linux, restricted /proc, etc.).
func openFDCount() (int, bool) {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0, false
	}
	// Subtract one for the fd opened by ReadDir on the directory itself so the
	// count reflects fds that exist independent of this sample.
	count := len(entries)
	if count > 0 {
		count--
	}
	return count, true
}

// softFDLimit returns the RLIMIT_NOFILE soft limit, or 0 when unavailable.
// syscall.Rlimit.Cur is uint64 on Linux and int64 on FreeBSD, so normalize via
// uint64() and treat any negative value (or error) as unavailable.
func softFDLimit() uint64 {
	var rl syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rl); err != nil {
		return 0
	}
	if rl.Cur < 0 {
		return 0
	}
	return uint64(rl.Cur)
}

func listenUnixSocket(path string, perm os.FileMode) (net.Listener, error) {
	dir := filepathDir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		return nil, err
	}
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, perm); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}

func defaultControlAPIHTTPConfig() controlAPIHTTPConfig {
	prefixes, _ := parseControlAPIAllowCIDRs([]string{"127.0.0.1/32", "::1/128"})
	return controlAPIHTTPConfig{
		Enabled:       false,
		Listen:        net.JoinHostPort(defaultControlAPIListenAddress, strconv.Itoa(defaultControlAPIPort)),
		AllowPrefixes: prefixes,
	}
}

func resolveControlAPIHTTPConfig(router *api.Router, cliListen string, cliAllowCIDRs []string, cliTokenFrom api.SecretValueSourceSpec, cliTLS controlAPITLSConfig, setFlags map[string]bool) (controlAPIHTTPConfig, error) {
	cfg := defaultControlAPIHTTPConfig()
	if router != nil {
		var found *api.ControlAPISpec
		for _, res := range router.Spec.Resources {
			if res.APIVersion != api.SystemAPIVersion || res.Kind != "ControlAPI" {
				continue
			}
			spec, err := res.ControlAPISpec()
			if err != nil {
				return controlAPIHTTPConfig{}, err
			}
			if found != nil {
				return controlAPIHTTPConfig{}, errors.New("only one system.routerd.net/v1alpha1 ControlAPI resource is supported")
			}
			found = &spec
		}
		if found != nil {
			cfg.Enabled = true
			if found.Enabled != nil {
				cfg.Enabled = *found.Enabled
			}
			if strings.TrimSpace(found.ListenAddress) != "" || found.Port != 0 {
				listenAddress := defaultControlAPIListenAddress
				if strings.TrimSpace(found.ListenAddress) != "" {
					listenAddress = strings.TrimSpace(found.ListenAddress)
				}
				port := defaultControlAPIPort
				if found.Port != 0 {
					port = found.Port
				}
				cfg.Listen = net.JoinHostPort(listenAddress, strconv.Itoa(port))
			}
			if len(found.AllowCIDRs) > 0 {
				prefixes, err := parseControlAPIAllowCIDRs(found.AllowCIDRs)
				if err != nil {
					return controlAPIHTTPConfig{}, err
				}
				cfg.AllowPrefixes = prefixes
			}
			token, err := controlAPITokenFromSecretSource(found.TokenFrom)
			if err != nil {
				return controlAPIHTTPConfig{}, fmt.Errorf("ControlAPI tokenFrom: %w", err)
			}
			cfg.Token = token
			cfg.TLS = controlAPITLSConfig{
				CertFile:     strings.TrimSpace(found.TLS.CertFile),
				KeyFile:      strings.TrimSpace(found.TLS.KeyFile),
				ClientCAFile: strings.TrimSpace(found.TLS.ClientCAFile),
			}
		}
	}
	if setFlags["http-listen"] {
		if cliListen == "" {
			return controlAPIHTTPConfig{}, errors.New("--http-listen must not be empty")
		}
		cfg.Enabled = true
		cfg.Listen = cliListen
	}
	if setFlags["http-allow-cidr"] {
		prefixes, err := parseControlAPIAllowCIDRs(cliAllowCIDRs)
		if err != nil {
			return controlAPIHTTPConfig{}, err
		}
		cfg.AllowPrefixes = prefixes
	}
	if setFlags["http-token-file"] || setFlags["http-token-env"] {
		token, err := controlAPITokenFromSecretSource(cliTokenFrom)
		if err != nil {
			return controlAPIHTTPConfig{}, fmt.Errorf("HTTP control API token: %w", err)
		}
		cfg.Token = token
	}
	if setFlags["http-tls-cert-file"] || setFlags["http-tls-key-file"] || setFlags["http-tls-client-ca-file"] {
		cfg.TLS = controlAPITLSConfig{
			CertFile:     strings.TrimSpace(cliTLS.CertFile),
			KeyFile:      strings.TrimSpace(cliTLS.KeyFile),
			ClientCAFile: strings.TrimSpace(cliTLS.ClientCAFile),
		}
	}
	if cfg.Enabled && len(cfg.AllowPrefixes) == 0 {
		return controlAPIHTTPConfig{}, errors.New("control HTTP API source allow CIDRs must not be empty")
	}
	if err := validateControlAPITLSConfig(cfg.TLS); err != nil {
		return controlAPIHTTPConfig{}, err
	}
	return cfg, nil
}

func validateControlAPITLSConfig(cfg controlAPITLSConfig) error {
	cert := strings.TrimSpace(cfg.CertFile)
	key := strings.TrimSpace(cfg.KeyFile)
	clientCA := strings.TrimSpace(cfg.ClientCAFile)
	if (cert == "") != (key == "") {
		return errors.New("control HTTP API TLS cert file and key file must be set together")
	}
	if clientCA != "" && cert == "" {
		return errors.New("control HTTP API TLS client CA requires cert file and key file")
	}
	return nil
}

func controlAPIServerTLSConfig(cfg controlAPITLSConfig) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(strings.TrimSpace(cfg.CertFile), strings.TrimSpace(cfg.KeyFile))
	if err != nil {
		return nil, fmt.Errorf("load server certificate: %w", err)
	}
	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	if strings.TrimSpace(cfg.ClientCAFile) != "" {
		data, err := os.ReadFile(strings.TrimSpace(cfg.ClientCAFile))
		if err != nil {
			return nil, fmt.Errorf("read client CA file %q: %w", strings.TrimSpace(cfg.ClientCAFile), err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("client CA file %q contains no PEM certificates", strings.TrimSpace(cfg.ClientCAFile))
		}
		tlsConfig.ClientCAs = pool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return tlsConfig, nil
}

func controlAPITokenFromSecretSource(source api.SecretValueSourceSpec) (string, error) {
	hasFile := strings.TrimSpace(source.File) != ""
	hasEnv := strings.TrimSpace(source.Env) != ""
	if !hasFile && !hasEnv {
		return "", nil
	}
	if hasFile == hasEnv {
		return "", errors.New("tokenFrom.file or tokenFrom.env must be set, but not both")
	}
	var value string
	switch {
	case hasFile:
		path := strings.TrimSpace(source.File)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read token file %q: %w", path, err)
		}
		value = string(data)
	case hasEnv:
		name := strings.TrimSpace(source.Env)
		found, ok := os.LookupEnv(name)
		if !ok {
			return "", fmt.Errorf("read token env %q: not set", name)
		}
		value = found
	}
	value = strings.TrimSpace(value)
	if source.Base64 {
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return "", fmt.Errorf("decode base64 token: %w", err)
		}
		value = strings.TrimSpace(string(decoded))
	}
	if value == "" {
		return "", errors.New("token must not be empty")
	}
	return value, nil
}

func parseControlAPIAllowCIDRs(cidrs []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(cidrs))
	for i, cidr := range cidrs {
		text := strings.TrimSpace(cidr)
		if text == "" {
			return nil, fmt.Errorf("control HTTP API allow CIDR %d must not be empty", i)
		}
		prefix, err := netip.ParsePrefix(text)
		if err != nil {
			return nil, fmt.Errorf("control HTTP API allow CIDR %d must be valid: %w", i, err)
		}
		if prefix.Bits() == 0 {
			addr := prefix.Addr().Unmap()
			if addr.Is4() && addr == netip.IPv4Unspecified() {
				return nil, fmt.Errorf("control HTTP API allow CIDR %d must not be 0.0.0.0/0", i)
			}
			if addr.Is6() && addr == netip.IPv6Unspecified() {
				return nil, fmt.Errorf("control HTTP API allow CIDR %d must not be ::/0", i)
			}
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func controlAPIAdmissionHandler(next http.Handler, allowPrefixes []netip.Prefix, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addr, err := remoteAddrIP(r.RemoteAddr)
		if err != nil || !controlAPISourceAllowed(addr, allowPrefixes) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if token != "" && !controlAPITokenAllowed(r, token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="routerd-control-api"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func controlAPITokenAllowed(r *http.Request, want string) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if len(auth) <= len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return false
	}
	got := strings.TrimSpace(auth[len(prefix):])
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func remoteAddrIP(remoteAddr string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return netip.Addr{}, err
	}
	return addr.Unmap(), nil
}

func controlAPISourceAllowed(addr netip.Addr, allowPrefixes []netip.Prefix) bool {
	addr = addr.Unmap()
	for _, prefix := range allowPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func parseControllerNames(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" || value == "all" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" && part != "all" {
			out = append(out, part)
		}
	}
	return out
}

func logQuerySince(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "1h"
	}
	duration, err := logstore.ParseRetention(value)
	if err != nil {
		return time.Time{}, err
	}
	return time.Now().Add(-duration).UTC(), nil
}

func listDNSQueriesReadOnly(ctx context.Context, path string, filter logstore.DNSQueryFilter) ([]logstore.DNSQuery, error) {
	store, err := logstore.OpenDNSQueryLogReadOnly(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer store.Close()
	return store.List(ctx, filter)
}

func aggregateDNSQueriesReadOnly(ctx context.Context, path string, filter logstore.DNSQueryFilter) (logstore.DNSQueryAggregate, error) {
	store, err := logstore.OpenDNSQueryLogReadOnly(path)
	if err != nil {
		if os.IsNotExist(err) {
			return logstore.DNSQueryAggregate{Since: filter.Since, Until: filter.Until}, nil
		}
		return logstore.DNSQueryAggregate{}, err
	}
	defer store.Close()
	return store.Aggregate(ctx, filter)
}

func listTrafficFlowsReadOnly(ctx context.Context, path string, filter logstore.TrafficFlowFilter) ([]logstore.TrafficFlow, error) {
	store, err := logstore.OpenTrafficFlowLogReadOnly(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer store.Close()
	return store.List(ctx, filter)
}

func aggregateTrafficFlowsReadOnly(ctx context.Context, path string, filter logstore.TrafficFlowFilter) (logstore.TrafficFlowAggregate, error) {
	store, err := logstore.OpenTrafficFlowLogReadOnly(path)
	if err != nil {
		if os.IsNotExist(err) {
			return logstore.TrafficFlowAggregate{Since: filter.Since, Until: filter.Until}, nil
		}
		return logstore.TrafficFlowAggregate{}, err
	}
	defer store.Close()
	return store.Aggregate(ctx, filter)
}

func dnsQueryFilterFromRequest(req controlapi.DNSQueriesRequest) (logstore.DNSQueryFilter, error) {
	since, until, err := resolveSinceUntil(req.Since, req.From, req.To)
	if err != nil {
		return logstore.DNSQueryFilter{}, err
	}
	return logstore.DNSQueryFilter{
		Since:         since,
		Until:         until,
		Client:        req.Client,
		QName:         req.QName,
		QNameSuffix:   req.QNameSuffix,
		ResponseCode:  req.ResponseCode,
		Upstream:      req.Upstream,
		DurationMinUS: req.DurationMinUS,
		Limit:         req.Limit,
	}, nil
}

func trafficFlowFilterFromRequest(req controlapi.TrafficFlowsRequest) (logstore.TrafficFlowFilter, error) {
	since, until, err := resolveSinceUntil(req.Since, req.From, req.To)
	if err != nil {
		return logstore.TrafficFlowFilter{}, err
	}
	return logstore.TrafficFlowFilter{
		Since:      since,
		Until:      until,
		Client:     req.Client,
		Peer:       req.Peer,
		PeerSuffix: req.PeerSuffix,
		Protocol:   req.Protocol,
		Asymmetric: req.Asymmetric,
		Limit:      req.Limit,
	}, nil
}

// resolveSinceUntil picks (since, until) from a duration-based "since" and the
// optional absolute "from" / "to" parameters. Absolute parameters take precedence.
func resolveSinceUntil(sinceStr, fromStr, toStr string) (time.Time, time.Time, error) {
	var since, until time.Time
	if v := strings.TrimSpace(fromStr); v != "" {
		t, err := parseControlAbsTime(v)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		since = t
	} else if v := strings.TrimSpace(sinceStr); v != "" {
		t, err := logQuerySince(v)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		since = t
	} else {
		t, err := logQuerySince("1h")
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		since = t
	}
	if v := strings.TrimSpace(toStr); v != "" {
		t, err := parseControlAbsTime(v)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		until = t
	}
	return since, until, nil
}

// parseControlAbsTime parses an absolute timestamp using several layouts.
// Bare layouts without timezone are interpreted as UTC.
func parseControlAbsTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse %q (expected RFC3339 like 2026-05-27T20:00:00+09:00)", value)
}

func listFirewallLogsReadOnly(ctx context.Context, path string, filter logstore.FirewallLogFilter) ([]logstore.FirewallLogEntry, error) {
	store, err := logstore.OpenFirewallLogReadOnly(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer store.Close()
	return store.List(ctx, filter)
}

func configuredDNSQueryLogPath(router *api.Router) string {
	fallback := platformDefaults.StateDir + "/dns-queries.db"
	if router == nil {
		return fallback
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "DNSResolver" {
			continue
		}
		spec, err := resource.DNSResolverSpec()
		if err != nil || !spec.QueryLog.Enabled {
			continue
		}
		if strings.TrimSpace(spec.QueryLog.Path) != "" {
			return spec.QueryLog.Path
		}
	}
	return fallback
}

func configuredTrafficFlowLogPath(router *api.Router) string {
	fallback := platformDefaults.StateDir + "/traffic-flows.db"
	if router == nil {
		return fallback
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "TrafficFlowLog" {
			continue
		}
		spec, err := resource.TrafficFlowLogSpec()
		if err != nil || !spec.Enabled {
			continue
		}
		if strings.TrimSpace(spec.Path) != "" {
			return spec.Path
		}
	}
	return fallback
}

func configuredDHCPLeasePaths(controllerDnsmasqConfig string) []string {
	var paths []string
	if leaseFile := dnsmasqLeaseFileForConfig(controllerDnsmasqConfig); strings.TrimSpace(leaseFile) != "" {
		paths = append(paths, leaseFile)
	}
	if leaseFile := dnsmasqLeaseFileForPlatform(); strings.TrimSpace(leaseFile) != "" {
		paths = append(paths, leaseFile)
	}
	paths = append(paths, platform.DnsmasqLeaseCandidates(platformDefaults, platformFeatures)...)
	return dedupeStrings(paths)
}

func dnsmasqLeaseFileForConfig(configPath string) string {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return ""
	}
	leaseDir := filepath.Dir(configPath)
	if strings.TrimSpace(leaseDir) == "" || leaseDir == "." {
		return ""
	}
	return filepath.Join(leaseDir, "dnsmasq.leases")
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func configuredFirewallLogPath(router *api.Router) string {
	fallback := platformDefaults.FirewallLogFile()
	if router == nil {
		return fallback
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "FirewallEventLog" {
			continue
		}
		spec, err := resource.FirewallEventLogSpec()
		if err != nil || !spec.Enabled {
			continue
		}
		if strings.TrimSpace(spec.Path) != "" {
			return spec.Path
		}
	}
	return fallback
}

func webConsoleResourcePresent(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind == "WebConsole" {
			return true
		}
	}
	return false
}

func webConsoleFromRouter(router *api.Router, store routerstate.ObjectStatusStore) (api.WebConsoleSpec, bool, error) {
	if router == nil {
		return api.WebConsoleSpec{}, false, nil
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "WebConsole" {
			continue
		}
		spec, err := resource.WebConsoleSpec()
		if err != nil {
			continue
		}
		if spec.Enabled != nil && !*spec.Enabled {
			return api.WebConsoleSpec{}, false, nil
		}
		resolved, err := resolveWebConsoleListenAddress(router, store, spec)
		if err != nil {
			return api.WebConsoleSpec{}, false, err
		}
		spec.ListenAddress = resolved
		if spec.Port == 0 {
			spec.Port = 8080
		}
		if spec.BasePath == "" {
			spec.BasePath = "/"
		}
		if spec.Title == "" {
			spec.Title = "routerd"
		}
		return spec, true, nil
	}
	return api.WebConsoleSpec{}, false, nil
}

func resolveWebConsoleListenAddress(router *api.Router, store routerstate.ObjectStatusStore, spec api.WebConsoleSpec) (string, error) {
	if strings.TrimSpace(spec.ListenAddressFrom.Resource) != "" {
		if address := firstAddressValue(resourcequery.Values(store, spec.ListenAddressFrom)); address != "" {
			return address, nil
		}
		if address := firstInterfaceAddressValue(router, spec.ListenAddressFrom); address != "" {
			return address, nil
		}
		if strings.TrimSpace(spec.ListenAddress) == "" {
			return "", fmt.Errorf("web console listenAddressFrom unresolved: %s.%s", spec.ListenAddressFrom.Resource, spec.ListenAddressFrom.Field)
		}
	}
	if strings.TrimSpace(spec.ListenAddress) != "" {
		return strings.TrimSpace(spec.ListenAddress), nil
	}
	return "127.0.0.1", nil
}

func firstAddressValue(values []string) string {
	for _, value := range values {
		if address := statusAddressValue(value); address != "" {
			return address
		}
	}
	return ""
}

func firstInterfaceAddressValue(router *api.Router, source api.StatusValueSourceSpec) string {
	kind, name, ok := resourcequery.SplitResource(source.Resource)
	if !ok || kind != "Interface" || router == nil {
		return ""
	}
	ifname := ""
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "Interface" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.InterfaceSpec()
		if err == nil {
			ifname = strings.TrimSpace(spec.IfName)
		}
		break
	}
	if ifname == "" {
		return ""
	}
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return ""
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return ""
	}
	field := strings.TrimSpace(source.Field)
	var candidates []string
	for _, addr := range addrs {
		value := statusAddressValue(addr.String())
		if value == "" {
			continue
		}
		parsed, err := netip.ParseAddr(value)
		if err != nil || parsed.IsLinkLocalUnicast() {
			continue
		}
		if (field == "ipv4Addresses" || field == "primaryIPv4") && !parsed.Is4() {
			continue
		}
		if (field == "ipv6Addresses" || field == "primaryIPv6") && !parsed.Is6() {
			continue
		}
		candidates = append(candidates, value)
	}
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func statusAddressValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Addr().String()
	}
	if addr, err := netip.ParseAddr(value); err == nil {
		return addr.String()
	}
	return ""
}

func startWebConsole(ctx context.Context, spec api.WebConsoleSpec, router *api.Router, store routerstate.Store, eventBus *bus.Bus, cache *resultCache, logger *eventlog.Logger, configPath string, controllerStatuses func() []controlapi.ControllerStatus, dhcpLeasePaths []string) error {
	addr := net.JoinHostPort(spec.ListenAddress, fmt.Sprintf("%d", spec.Port))
	handler := webconsole.New(webconsole.Options{
		Router:                 router,
		Store:                  store,
		Result:                 cache.Load,
		Connections:            observe.Connections,
		Title:                  spec.Title,
		BasePath:               spec.BasePath,
		ConsoleLinks:           webConsoleLinks(spec.Links),
		DNSQueryLogPath:        platformDefaults.StateDir + "/dns-queries.db",
		TrafficFlowLogPath:     platformDefaults.StateDir + "/traffic-flows.db",
		FirewallLogPath:        platformDefaults.FirewallLogFile(),
		DHCPFingerprintLogPath: platformDefaults.StateDir + "/dhcp-fingerprints.db",
		DHCPStickyLogPath:      dhcpStickyLogPath(),
		DHCPLeasePaths:         dhcpLeasePaths,
		ConfigPath:             configPath,
		ControllerStatuses:     controllerStatuses,
		Bus:                    eventBus,
	})
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 2 * time.Minute}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		if logger != nil {
			logger.Emit(eventlog.LevelInfo, "serve", "routerd web console starting", map[string]string{"listen": addr, "basePath": spec.BasePath})
		}
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed && logger != nil {
			logger.Emit(eventlog.LevelWarning, "serve", "routerd web console stopped", map[string]string{"error": err.Error()})
		}
	}()
	return nil
}

func webConsoleLinks(links []api.WebConsoleLinkSpec) []webconsole.ConsoleLink {
	out := make([]webconsole.ConsoleLink, 0, len(links))
	for _, link := range links {
		out = append(out, webconsole.ConsoleLink{
			Label:       link.Label,
			URL:         link.URL,
			Description: link.Description,
		})
	}
	return out
}

type resultCache struct {
	mu     sync.RWMutex
	result *apply.Result
}

func (c *resultCache) Store(result *apply.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.result = result
}

func (c *resultCache) Load() *apply.Result {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.result
}

func resultWithLatestGeneration(result *apply.Result, store *routerstate.SQLiteStore) *apply.Result {
	if store == nil {
		return result
	}
	generation := store.LatestGeneration()
	if generation == 0 {
		return result
	}
	if result == nil {
		return &apply.Result{Generation: generation}
	}
	next := *result
	next.Generation = generation
	return &next
}

func overallStatusPhase(base string, lister routerstate.ObjectStatusLister) string {
	if strings.TrimSpace(base) == "" {
		base = "Unknown"
	}
	if lister == nil {
		return base
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return base
	}
	phase := base
	for _, item := range statuses {
		resourcePhase := strings.TrimSpace(statusStringMap(item.Status, "phase"))
		if resourcePhase == "" {
			continue
		}
		phase = worseStatusPhase(phase, resourcePhase, statusStringMap(item.Status, "reason"))
		if phase == "Error" {
			break
		}
	}
	return phase
}

func resourcePhaseIssues(lister routerstate.ObjectStatusLister) []controlapi.ResourcePhaseIssue {
	if lister == nil {
		return nil
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return nil
	}
	var out []controlapi.ResourcePhaseIssue
	for _, item := range statuses {
		phase := strings.TrimSpace(statusStringMap(item.Status, "phase"))
		reason := statusStringMap(item.Status, "reason")
		if phase == "" || statusPhaseRank(phase, reason) <= 0 {
			continue
		}
		out = append(out, controlapi.ResourcePhaseIssue{
			APIVersion: item.APIVersion,
			Kind:       item.Kind,
			Name:       item.Name,
			Phase:      phase,
			Reason:     reason,
			Message:    statusStringMap(item.Status, "message"),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		leftRank := statusPhaseRank(out[i].Phase, out[i].Reason)
		rightRank := statusPhaseRank(out[j].Phase, out[j].Reason)
		if leftRank != rightRank {
			return leftRank > rightRank
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func worseStatusPhase(current, candidate, candidateReason string) string {
	if statusPhaseRank(candidate, candidateReason) > statusPhaseRank(current, "") {
		return canonicalOverallPhase(candidate, candidateReason)
	}
	return canonicalOverallPhase(current, "")
}

func statusPhaseRank(phase, reason string) int {
	if statusPhaseSuppressedByReason(phase, reason) {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "error", "blocked", "failed", "unhealthy":
		return 4
	case "pending", "starting", "acquiring", "refreshing", "rebinding", "rendered":
		return 3
	case "degraded", "down", "lost", "expired", "nohealthybackends":
		return 2
	case "unknown", "":
		return 1
	case "healthy", "applied", "active", "established", "bound", "running", "ready", "up", "installed", "configured", "synced", "observed", "removed", "skipped":
		return 0
	default:
		return 1
	}
}

func canonicalOverallPhase(phase, reason string) string {
	if statusPhaseSuppressedByReason(phase, reason) {
		return "Healthy"
	}
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "error", "blocked", "failed", "unhealthy":
		return "Error"
	case "pending", "starting", "acquiring", "refreshing", "rebinding", "rendered":
		return "Pending"
	case "degraded", "down", "lost", "expired", "nohealthybackends":
		return "Degraded"
	case "unknown", "":
		return "Unknown"
	case "healthy", "applied", "active", "established", "bound", "running", "ready", "up", "installed", "configured", "synced", "observed", "removed", "skipped":
		return "Healthy"
	default:
		return phase
	}
}

func statusPhaseSuppressedByReason(phase, reason string) bool {
	if !strings.EqualFold(strings.TrimSpace(phase), "Pending") {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "whenfalse", "dependsonfalse":
		return true
	default:
		return false
	}
}

func augmentControllerStatusesFromState(controllers []controlapi.ControllerStatus, store routerstate.Store) []controlapi.ControllerStatus {
	lister, ok := store.(routerstate.ObjectStatusLister)
	if !ok || len(controllers) == 0 {
		return controllers
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return controllers
	}
	var lastReloadAt *time.Time
	var lastRestartAt *time.Time
	var lastActionAt *time.Time
	var lastChangeReason string
	for _, item := range statuses {
		if item.APIVersion != api.NetAPIVersion || item.Kind != "VirtualAddress" {
			continue
		}
		if t := parseStatusTime(statusStringMap(item.Status, "lastReloadAt")); newerTime(t, lastReloadAt) {
			lastReloadAt = t
			if newerTime(t, lastActionAt) {
				lastActionAt = t
				lastChangeReason = statusStringMap(item.Status, "lastChangeReason")
			}
		}
		if t := parseStatusTime(statusStringMap(item.Status, "lastRestartAt")); newerTime(t, lastRestartAt) {
			lastRestartAt = t
			if newerTime(t, lastActionAt) {
				lastActionAt = t
				lastChangeReason = statusStringMap(item.Status, "lastChangeReason")
			}
		}
	}
	if lastReloadAt == nil && lastRestartAt == nil && lastChangeReason == "" {
		return controllers
	}
	out := append([]controlapi.ControllerStatus(nil), controllers...)
	for i := range out {
		if out[i].Name != "vrrp" {
			continue
		}
		out[i].LastReloadAt = lastReloadAt
		out[i].LastRestartAt = lastRestartAt
		out[i].LastChangeReason = lastChangeReason
	}
	return out
}

func parseStatusTime(value string) *time.Time {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return &parsed
}

func newerTime(candidate, current *time.Time) bool {
	if candidate == nil {
		return false
	}
	return current == nil || candidate.After(*current)
}

func statusStringMap(status map[string]any, key string) string {
	if status == nil {
		return ""
	}
	switch value := status[key].(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	default:
		return ""
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func runApplySchedule(stop <-chan struct{}, interval time.Duration, router func() *api.Router, opts applyOptions, cache *resultCache, logger *eventlog.Logger, applyMu *sync.Mutex) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			applyMu.Lock()
			result, err := runApplyOnce(router(), opts, io.Discard, logger)
			applyMu.Unlock()
			if err != nil {
				logger.Emit(eventlog.LevelError, "serve", "scheduled apply failed", map[string]string{"error": err.Error()})
				continue
			}
			cache.Store(result)
		}
	}
}

func runServeChainOnce(ctx context.Context, runner *controllerchain.Runner, router *api.Router, opts applyOptions, store *routerstate.SQLiteStore, stdout io.Writer, logger *eventlog.Logger) (*apply.Result, error) {
	if runner == nil {
		return nil, errors.New("controller chain runner is nil")
	}
	configYAML := routerConfigYAML(router, opts)
	var generation int64
	generationFinished := false
	if !opts.DryRun && store != nil {
		var err error
		generation, err = store.BeginGeneration(routerConfigHash(router))
		if err != nil {
			return nil, err
		}
		if err := store.RecordGenerationConfig(generation, configYAML); err != nil {
			return nil, err
		}
		defer func() {
			if generation != 0 && !generationFinished {
				_ = store.FinishGeneration(generation, "Errored", nil)
			}
		}()
		if err := recordHostInventoryState(store); err != nil {
			return nil, err
		}
	}
	if err := runner.ReconcileOnce(ctx); err != nil {
		return nil, err
	}
	result, err := apply.New().Observe(router)
	if err != nil {
		return nil, err
	}
	if generation != 0 {
		result.Generation = generation
	}
	if err := writeResult(stdout, opts.StatusFile, result); err != nil {
		return nil, err
	}
	if !opts.DryRun && store != nil && generation != 0 {
		_ = store.FinishGeneration(generation, result.Phase, result.Warnings)
		generationFinished = true
	}
	if logger != nil {
		logger.Emit(eventlog.LevelInfo, "serve", "routerd serve once completed", map[string]string{
			"phase":      result.Phase,
			"generation": strconv.FormatInt(result.Generation, 10),
		})
	}
	return result, nil
}

func observedIPv6PrefixesByInterface(router *api.Router) map[string][]string {
	out := map[string][]string{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "Interface" {
			continue
		}
		spec, err := res.InterfaceSpec()
		if err != nil || spec.IfName == "" {
			continue
		}
		out[spec.IfName] = ipv6Prefixes(spec.IfName)
	}
	return out
}

func ipv6Prefixes(ifname string) []string {
	if platformDefaults.OS == platform.OSFreeBSD {
		return freeBSDIPv6Prefixes(ifname)
	}
	out, err := exec.Command("ip", "-6", "route", "show", "dev", ifname, "proto", "kernel").CombinedOutput()
	if err != nil {
		return nil
	}
	var prefixes []string
	for _, field := range strings.Fields(string(out)) {
		prefix, err := netip.ParsePrefix(field)
		if err == nil && prefix.Addr().Is6() {
			prefixes = append(prefixes, prefix.Masked().String())
		}
	}
	return prefixes
}

func observedIPv6AddressesByInterface(router *api.Router) map[string][]string {
	out := map[string][]string{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "Interface" {
			continue
		}
		spec, err := res.InterfaceSpec()
		if err != nil || spec.IfName == "" {
			continue
		}
		out[spec.IfName] = ipv6Addresses(spec.IfName)
	}
	return out
}

func ipv6Addresses(ifname string) []string {
	entries := ipv6AddressEntries(ifname)
	addrs := make([]string, 0, len(entries))
	for _, entry := range entries {
		addrs = append(addrs, entry.Address)
	}
	return addrs
}

type ipv6AddressEntry struct {
	Address   string
	PrefixLen int
}

func ipv6AddressEntries(ifname string) []ipv6AddressEntry {
	if platformDefaults.OS == platform.OSFreeBSD {
		return freeBSDIPv6AddressEntries(ifname)
	}
	out, err := exec.Command("ip", "-brief", "-6", "addr", "show", "dev", ifname).CombinedOutput()
	if err != nil {
		return nil
	}
	var entries []ipv6AddressEntry
	for _, field := range strings.Fields(string(out)) {
		addrPart, _, ok := strings.Cut(field, "/")
		if !ok {
			continue
		}
		addr, err := netip.ParseAddr(addrPart)
		if err == nil && addr.Is6() {
			bits := 128
			if prefix, err := netip.ParsePrefix(field); err == nil {
				bits = prefix.Bits()
			}
			entries = append(entries, ipv6AddressEntry{Address: addr.String(), PrefixLen: bits})
		}
	}
	return entries
}

func freeBSDIPv6Prefixes(ifname string) []string {
	out, err := exec.Command("ifconfig", ifname).CombinedOutput()
	if err != nil {
		return nil
	}
	prefixes, _ := parseFreeBSDIfconfigIPv6(string(out))
	return prefixes
}

func freeBSDIPv6AddressEntries(ifname string) []ipv6AddressEntry {
	out, err := exec.Command("ifconfig", ifname).CombinedOutput()
	if err != nil {
		return nil
	}
	_, entries := parseFreeBSDIfconfigIPv6Entries(string(out))
	return entries
}

func parseFreeBSDIfconfigIPv6(out string) ([]string, []string) {
	prefixes, entries := parseFreeBSDIfconfigIPv6Entries(out)
	addrs := make([]string, 0, len(entries))
	for _, entry := range entries {
		addrs = append(addrs, entry.Address)
	}
	return prefixes, addrs
}

func parseFreeBSDIfconfigIPv6Entries(out string) ([]string, []ipv6AddressEntry) {
	var prefixes []string
	var entries []ipv6AddressEntry
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "inet6" {
			continue
		}
		addrText := fields[1]
		if base, _, ok := strings.Cut(addrText, "%"); ok {
			addrText = base
		}
		addr, err := netip.ParseAddr(addrText)
		if err != nil || !addr.Is6() {
			continue
		}
		bits := 64
		for i := 2; i+1 < len(fields); i++ {
			if fields[i] != "prefixlen" {
				continue
			}
			parsed, err := strconv.Atoi(fields[i+1])
			if err == nil && parsed >= 0 && parsed <= 128 {
				bits = parsed
			}
			break
		}
		entries = append(entries, ipv6AddressEntry{Address: addr.String(), PrefixLen: bits})
		if !addr.IsLinkLocalUnicast() {
			prefixes = append(prefixes, netip.PrefixFrom(addr, bits).Masked().String())
		}
	}
	return prefixes, entries
}

func observedDNSServersByInterface(router *api.Router) map[string][]string {
	out := map[string][]string{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "Interface" {
			continue
		}
		spec, err := res.InterfaceSpec()
		if err != nil || spec.IfName == "" {
			continue
		}
		out[spec.IfName] = resolvectlDNS(spec.IfName)
	}
	return out
}

func resolvectlDNS(ifname string) []string {
	out, err := exec.Command("resolvectl", "dns", ifname).CombinedOutput()
	if err != nil {
		return nil
	}
	fields := strings.Fields(strings.ReplaceAll(string(out), ":", " "))
	var servers []string
	for _, field := range fields {
		addr, err := netip.ParseAddr(field)
		if err == nil {
			servers = append(servers, addr.String())
		}
	}
	return servers
}
