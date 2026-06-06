// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
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
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
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
		return []string{"FirewallZone", "FirewallPolicy", "FirewallRule", "ClientPolicy", "PortForward", "IngressService", "IPAddressSet", "LocalServiceRedirect"}
	case "ingress":
		return []string{"IngressService"}
	case "bgp":
		return []string{"BGPRouter", "BGPPeer", "BFD"}
	case "vrrp":
		return []string{"VirtualAddress"}
	case "nat":
		return []string{"NAT44Rule", "PortForward", "IngressService", "IPAddressSet", "LocalServiceRedirect"}
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

func serveCommand(args []string, stdout, stderr io.Writer) (err error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	statusFile := fs.String("status-file", defaultStatusFile(), "status file")
	socketPath := fs.String("socket", defaultSocketPath(), "Unix domain socket path")
	statusSocketPath := fs.String("status-socket", defaultStatusSocketPath(), "read-only status Unix domain socket path")
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *statusSocketPath == *socketPath {
		return errors.New("--status-socket must differ from --socket")
	}
	enabledControllers := parseControllerNames(*controllerNames)
	controllerStatuses := filterControllerDefaultStatuses(controllerDefaultStatuses(), enabledControllers)
	controllerRuntime := controlapi.NewControllerRuntimeStore(controllerStatuses)
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	logger, err := eventlog.New(router)
	if err != nil {
		return err
	}
	defer closeLogger(logger, "serve", &err)
	logger.Emit(eventlog.LevelInfo, "serve", "routerd daemon starting", map[string]string{
		"config":        *configPath,
		"socket":        *socketPath,
		"statusSocket":  *statusSocketPath,
		"applyInterval": applyInterval.String(),
	})
	cache := &resultCache{}
	engine := apply.New()
	if result, observeErr := engine.Observe(router); observeErr == nil {
		cache.Store(result)
		_ = statuswriter.Write(*statusFile, result)
	} else {
		logger.Emit(eventlog.LevelWarning, "serve", "initial observe failed", map[string]string{"error": observeErr.Error()})
	}

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	stop := make(chan struct{})
	var stopOnce sync.Once
	closeStop := func() {
		stopOnce.Do(func() { close(stop) })
	}
	defer closeStop()
	go func() {
		<-signalCtx.Done()
		closeStop()
	}()
	ctx, cancelControllers := context.WithCancel(signalCtx)
	defer cancelControllers()
	go func() {
		<-stop
		cancelControllers()
	}()
	var controllerBus *bus.Bus
	var stateStore *routerstate.SQLiteStore
	stateStore, err = routerstate.OpenSQLite(*statePath)
	if err != nil {
		return err
	}
	defer stateStore.Close()
	if _, cleanupErr := cleanupUnsupportedLegacyObjectStatuses(router, stateStore, *statePath, time.Now().UTC(), logger); cleanupErr != nil {
		logger.Emit(eventlog.LevelWarning, "serve", "stale state cleanup encountered an error", map[string]string{"error": cleanupErr.Error()})
	}
	controllerBus = bus.NewWithStore(stateStore)
	controllerBus.SetLogger(slog.Default())
	publishControllerModeEvents(ctx, controllerBus, controllerStatuses)
	chainRunner := controllerchain.Runner{
		Router: router,
		Bus:    controllerBus,
		Store:  stateStore,
		Opts: controllerchain.Options{
			SuperviseClientDaemons: true,
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
		},
	}
	if err := chainRunner.Start(ctx); err != nil {
		return err
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
	}
	applyMu := &sync.Mutex{}
	if *applyInterval > 0 {
		go runApplySchedule(stop, *applyInterval, router, applyOpts, cache, logger, applyMu)
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
	defer listener.Close()

	handler := controlapi.Handler{
		Status: func(r *http.Request) (*controlapi.Status, error) {
			status := controlapi.NewStatus(resultWithLatestGeneration(cache.Load(), stateStore))
			status.Status.Phase = overallStatusPhase(status.Status.Phase, stateStore)
			status.Status.ResourcePhaseIssues = resourcePhaseIssues(stateStore)
			controllers := controllerRuntime.Snapshot()
			if stateStore != nil {
				controllers = augmentControllerStatusesFromState(controllers, stateStore)
			}
			status.Status.Controllers = controllers
			return &status, nil
		},
		Controllers: func(r *http.Request) (*controlapi.Controllers, error) {
			statuses := controllerRuntime.Snapshot()
			if stateStore != nil {
				statuses = augmentControllerStatusesFromState(statuses, stateStore)
			}
			controllers := controlapi.NewControllers(statuses)
			return &controllers, nil
		},
		Runtime: func(r *http.Request) (*controlapi.RuntimeStats, error) {
			stats := collectRuntimeStats()
			return &stats, nil
		},
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
			rows, err := listDNSQueriesReadOnly(r.Context(), configuredDNSQueryLogPath(router), filter)
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
			agg, err := aggregateDNSQueriesReadOnly(r.Context(), configuredDNSQueryLogPath(router), filter)
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
			rows, err := listTrafficFlowsReadOnly(r.Context(), configuredTrafficFlowLogPath(router), filter)
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
			agg, err := aggregateTrafficFlowsReadOnly(r.Context(), configuredTrafficFlowLogPath(router), filter)
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
			rows, err := listFirewallLogsReadOnly(r.Context(), configuredFirewallLogPath(router), logstore.FirewallLogFilter{Since: since, Action: req.Action, Src: req.Src, Limit: req.Limit})
			if err != nil {
				return nil, err
			}
			result := controlapi.NewFirewallLogs(rows)
			return &result, nil
		},
		Apply: func(r *http.Request, req controlapi.ApplyRequest) (*controlapi.ApplyResult, error) {
			opts := applyOpts
			opts.DryRun = req.DryRun
			applyMu.Lock()
			defer applyMu.Unlock()
			result, err := runApplyOnce(router, opts, io.Discard, logger)
			if err != nil {
				return nil, err
			}
			cache.Store(result)
			apiResult := controlapi.NewApplyResult(result)
			return &apiResult, nil
		},
		Delete: func(r *http.Request, req controlapi.DeleteRequest) (*controlapi.DeleteResult, error) {
			if req.Target == "" {
				return nil, controlapi.ErrBadRequest
			}
			target, err := deleteTargetFromArg(req.Target)
			if err != nil {
				if !req.Force {
					return nil, err
				}
				target, err = forceDeleteTargetFromArg(req.Target, defaultStatePath, req.TargetAPIVersion)
				if err != nil {
					return nil, err
				}
			}
			result, err := performDeleteTargets([]deleteTarget{target}, defaultStatePath, *ledgerPath, req.DryRun)
			if err != nil {
				return nil, err
			}
			return &result, nil
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
			if holdDays := dhcpStickyHoldDays(router, req.IP); holdDays > 0 {
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
	// `routerctl status` without sudo. Otherwise fall back to world-accessible
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
			Status:      handler.Status,
			Controllers: handler.Controllers,
			Runtime:     handler.Runtime,
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
	go func() {
		<-signalCtx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = statusServer.Shutdown(shutdownCtx)
		_ = server.Shutdown(shutdownCtx)
	}()
	fmt.Fprintf(stdout, "routerd serving control API on unix://%s\n", *socketPath)
	fmt.Fprintf(stdout, "routerd serving read-only status API on unix://%s\n", *statusSocketPath)
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
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
	if leaseFile := dnsmasqLeaseFileForPlatform(); strings.TrimSpace(leaseFile) != "" {
		paths = append(paths, leaseFile)
	}
	if leaseFile := dnsmasqLeaseFileForConfig(controllerDnsmasqConfig); strings.TrimSpace(leaseFile) != "" {
		paths = append(paths, leaseFile)
	}
	paths = append(paths, platform.DnsmasqLeaseCandidates(platformDefaults, platformFeatures)...)
	paths = append(paths, "/run/routerd/dnsmasq.leases", "/var/lib/misc/dnsmasq.leases")
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
		resourcePhase := strings.TrimSpace(fmt.Sprint(item.Status["phase"]))
		if resourcePhase == "" {
			continue
		}
		phase = worseStatusPhase(phase, resourcePhase)
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
		phase := strings.TrimSpace(fmt.Sprint(item.Status["phase"]))
		if phase == "" || statusPhaseRank(phase) <= 0 {
			continue
		}
		out = append(out, controlapi.ResourcePhaseIssue{
			APIVersion: item.APIVersion,
			Kind:       item.Kind,
			Name:       item.Name,
			Phase:      phase,
			Reason:     statusStringMap(item.Status, "reason"),
			Message:    statusStringMap(item.Status, "message"),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		leftRank := statusPhaseRank(out[i].Phase)
		rightRank := statusPhaseRank(out[j].Phase)
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

func worseStatusPhase(current, candidate string) string {
	if statusPhaseRank(candidate) > statusPhaseRank(current) {
		return canonicalOverallPhase(candidate)
	}
	return canonicalOverallPhase(current)
}

func statusPhaseRank(phase string) int {
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

func canonicalOverallPhase(phase string) string {
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

func runApplySchedule(stop <-chan struct{}, interval time.Duration, router *api.Router, opts applyOptions, cache *resultCache, logger *eventlog.Logger, applyMu *sync.Mutex) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			applyMu.Lock()
			result, err := runApplyOnce(router, opts, io.Discard, logger)
			applyMu.Unlock()
			if err != nil {
				logger.Emit(eventlog.LevelError, "serve", "scheduled apply failed", map[string]string{"error": err.Error()})
				continue
			}
			cache.Store(result)
		}
	}
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
