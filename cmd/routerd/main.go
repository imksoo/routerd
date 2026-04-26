package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/config"
	"routerd/pkg/controlapi"
	"routerd/pkg/eventlog"
	"routerd/pkg/observe"
	"routerd/pkg/reconcile"
	"routerd/pkg/render"
	statuswriter "routerd/pkg/status"
)

const (
	defaultConfigPath         = "/usr/local/etc/routerd/router.yaml"
	defaultPluginDir          = "/usr/local/libexec/routerd/plugins"
	defaultNetplanPath        = "/etc/netplan/90-routerd.yaml"
	defaultDnsmasqConfigPath  = "/usr/local/etc/routerd/dnsmasq.conf"
	defaultDnsmasqServicePath = "/etc/systemd/system/routerd-dnsmasq.service"
	defaultNftablesPath       = "/usr/local/etc/routerd/nftables.nft"
	routerdDnsmasqService     = "routerd-dnsmasq.service"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return errors.New("missing command")
	}

	switch args[0] {
	case "validate":
		return validateCommand(args[1:], stdout)
	case "observe":
		return configCommand(args[1:], stdout, "observe")
	case "plan":
		return configCommand(args[1:], stdout, "plan")
	case "reconcile":
		return reconcileCommand(args[1:], stdout)
	case "serve":
		return serveCommand(args[1:], stdout)
	case "run":
		return configCommand(args[1:], stdout, "run")
	case "status":
		return statusCommand(args[1:], stdout)
	case "plugin":
		return pluginCommand(args[1:], stdout)
	case "help", "-h", "--help":
		usage(stdout)
		return nil
	default:
		usage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func validateCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireExistingFile(*configPath); err != nil {
		return err
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if err := config.Validate(router); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "config %s exists\n", *configPath)
	fmt.Fprintln(stdout, "config is valid")
	return nil
}

func configCommand(args []string, stdout io.Writer, name string) (err error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	statusFile := fs.String("status-file", defaultStatusFile(), "status file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	logger, err := eventlog.New(router)
	if err != nil {
		return err
	}
	defer closeLogger(logger, name, &err)
	logger.Emit(eventlog.LevelInfo, name, "routerd command started", map[string]string{"config": *configPath})
	engine := reconcile.New()
	switch name {
	case "observe":
		result, err := engine.Observe(router)
		if err != nil {
			return err
		}
		return writeResult(stdout, *statusFile, result)
	case "plan":
		result, err := engine.Plan(router)
		if err != nil {
			return err
		}
		return writeResult(stdout, *statusFile, result)
	case "run":
		return errors.New("run is not implemented yet")
	default:
		return fmt.Errorf("unknown config command %s", name)
	}
}

func reconcileCommand(args []string, stdout io.Writer) (err error) {
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	statusFile := fs.String("status-file", defaultStatusFile(), "status file")
	netplanPath := fs.String("netplan-file", defaultNetplanPath, "routerd-managed netplan file")
	dnsmasqConfigPath := fs.String("dnsmasq-file", defaultDnsmasqConfigPath, "routerd-managed dnsmasq config file")
	dnsmasqServicePath := fs.String("dnsmasq-service-file", defaultDnsmasqServicePath, "routerd-managed dnsmasq systemd unit file")
	nftablesPath := fs.String("nftables-file", defaultNftablesPath, "routerd-managed nftables ruleset file")
	once := fs.Bool("once", false, "run one reconcile loop")
	dryRun := fs.Bool("dry-run", false, "plan without applying changes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*once {
		return errors.New("reconcile currently requires --once")
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	logger, err := eventlog.New(router)
	if err != nil {
		return err
	}
	defer closeLogger(logger, "reconcile", &err)
	logger.Emit(eventlog.LevelInfo, "reconcile", "routerd command started", map[string]string{
		"config": *configPath,
		"dryRun": fmt.Sprintf("%t", *dryRun),
	})
	opts := reconcileApplyOptions{
		ConfigPath:          *configPath,
		StatusFile:          *statusFile,
		NetplanPath:         *netplanPath,
		DnsmasqConfigPath:   *dnsmasqConfigPath,
		DnsmasqServicePath:  *dnsmasqServicePath,
		NftablesPath:        *nftablesPath,
		DryRun:              *dryRun,
		AnnounceDryRunToCLI: true,
	}
	_, err = runReconcileOnce(router, opts, stdout, logger)
	return err
}

type reconcileApplyOptions struct {
	ConfigPath          string
	StatusFile          string
	NetplanPath         string
	DnsmasqConfigPath   string
	DnsmasqServicePath  string
	NftablesPath        string
	DryRun              bool
	AnnounceDryRunToCLI bool
}

func runReconcileOnce(router *api.Router, opts reconcileApplyOptions, stdout io.Writer, logger *eventlog.Logger) (*reconcile.Result, error) {
	engine := reconcile.New()
	result, err := engine.Plan(router)
	if err != nil {
		return nil, err
	}
	if !opts.DryRun {
		netplanData, err := render.Netplan(router)
		if err != nil {
			return nil, err
		}
		networkdFiles, err := render.NetworkdDropins(router)
		if err != nil {
			return nil, err
		}
		dnsmasqConfig, err := render.DnsmasqConfig(router, render.DnsmasqRuntime{
			DHCPv4DNSServersByInterface: observedDNSServersByInterface(router),
			DHCPv6DNSServersByInterface: observedDNSServersByInterface(router),
			IPv6AddressesByInterface:    observedIPv6AddressesByInterface(router),
			IPv6PrefixesByInterface:     observedIPv6PrefixesByInterface(router),
		})
		if err != nil {
			return nil, err
		}
		nftablesConfig, err := render.NftablesIPv4SourceNAT(router)
		if err != nil {
			return nil, err
		}
		logger.Emit(eventlog.LevelInfo, "reconcile", "routerd plan completed", map[string]string{
			"phase":     result.Phase,
			"resources": fmt.Sprintf("%d", len(result.Resources)),
		})
		networkChangedFiles, err := applyNetworkConfig(opts.NetplanPath, netplanData, networkdFiles)
		if err != nil {
			return nil, err
		}
		dnsmasqChangedFiles, err := applyDnsmasqConfig(opts.DnsmasqConfigPath, opts.DnsmasqServicePath, dnsmasqConfig)
		if err != nil {
			return nil, err
		}
		nftablesChangedFiles, err := applyNftablesConfig(opts.NftablesPath, nftablesConfig)
		if err != nil {
			return nil, err
		}
		appliedTunnels, err := applyDSLiteTunnels(router)
		if err != nil {
			return nil, err
		}
		appliedRuntime, err := applyRuntimeSysctls(router)
		if err != nil {
			return nil, err
		}
		appliedReversePathFilters, err := applyIPv4ReversePathFilters(router)
		if err != nil {
			return nil, err
		}
		appliedPolicyRoutes, err := applyIPv4PolicyRoutes(router)
		if err != nil {
			return nil, err
		}
		changedFiles := append(networkChangedFiles, dnsmasqChangedFiles...)
		changedFiles = append(changedFiles, nftablesChangedFiles...)
		if len(changedFiles) == 0 {
			if len(appliedRuntime) == 0 {
				fmt.Fprintln(stdout, "network configuration already up to date")
			}
		} else {
			for _, path := range changedFiles {
				fmt.Fprintf(stdout, "wrote %s\n", path)
			}
			if len(networkChangedFiles) > 0 {
				fmt.Fprintln(stdout, "applied network configuration")
			}
			if len(dnsmasqChangedFiles) > 0 {
				fmt.Fprintln(stdout, "applied dnsmasq")
			}
			if len(nftablesChangedFiles) > 0 {
				fmt.Fprintln(stdout, "applied nftables")
			}
		}
		for _, key := range appliedRuntime {
			fmt.Fprintf(stdout, "applied sysctl %s\n", key)
		}
		for _, key := range appliedReversePathFilters {
			fmt.Fprintf(stdout, "applied IPv4 reverse path filter %s\n", key)
		}
		for _, tunnel := range appliedTunnels {
			fmt.Fprintf(stdout, "applied DS-Lite tunnel %s\n", tunnel)
		}
		for _, route := range appliedPolicyRoutes {
			fmt.Fprintf(stdout, "applied IPv4 policy route %s\n", route)
		}
		logger.Emit(eventlog.LevelInfo, "reconcile", "routerd changes applied", map[string]string{
			"changedFiles":        fmt.Sprintf("%d", len(changedFiles)),
			"runtimeSysctls":      fmt.Sprintf("%d", len(appliedRuntime)),
			"reversePathFilters":  fmt.Sprintf("%d", len(appliedReversePathFilters)),
			"dsliteTunnels":       fmt.Sprintf("%d", len(appliedTunnels)),
			"ipv4PolicyRouteSets": fmt.Sprintf("%d", len(appliedPolicyRoutes)),
		})
		if err := writeResult(stdout, opts.StatusFile, result); err != nil {
			return nil, err
		}
		return result, nil
	}
	if opts.AnnounceDryRunToCLI {
		fmt.Fprintf(stdout, "dry-run reconcile plan for %s\n", opts.ConfigPath)
	}
	logger.Emit(eventlog.LevelInfo, "reconcile", "routerd dry-run completed", map[string]string{
		"phase":     result.Phase,
		"resources": fmt.Sprintf("%d", len(result.Resources)),
	})
	if err := writeResult(stdout, opts.StatusFile, result); err != nil {
		return nil, err
	}
	return result, nil
}

func serveCommand(args []string, stdout io.Writer) (err error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	statusFile := fs.String("status-file", defaultStatusFile(), "status file")
	socketPath := fs.String("socket", defaultSocketPath(), "Unix domain socket path")
	observeInterval := fs.Duration("observe-interval", 30*time.Second, "periodic observe interval; 0 disables scheduled observe")
	reconcileInterval := fs.Duration("reconcile-interval", 0, "periodic reconcile interval; 0 disables scheduled reconcile")
	netplanPath := fs.String("netplan-file", defaultNetplanPath, "routerd-managed netplan file")
	dnsmasqConfigPath := fs.String("dnsmasq-file", defaultDnsmasqConfigPath, "routerd-managed dnsmasq config file")
	dnsmasqServicePath := fs.String("dnsmasq-service-file", defaultDnsmasqServicePath, "routerd-managed dnsmasq systemd unit file")
	nftablesPath := fs.String("nftables-file", defaultNftablesPath, "routerd-managed nftables ruleset file")
	if err := fs.Parse(args); err != nil {
		return err
	}
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
		"config":            *configPath,
		"socket":            *socketPath,
		"observeInterval":   observeInterval.String(),
		"reconcileInterval": reconcileInterval.String(),
	})

	cache := &resultCache{}
	engine := reconcile.New()
	if result, observeErr := engine.Observe(router); observeErr == nil {
		cache.Store(result)
		_ = statuswriter.Write(*statusFile, result)
	} else {
		logger.Emit(eventlog.LevelWarning, "serve", "initial observe failed", map[string]string{"error": observeErr.Error()})
	}

	stop := make(chan struct{})
	defer close(stop)
	if *observeInterval > 0 {
		go runObserveSchedule(stop, *observeInterval, router, cache, *statusFile, logger)
	}
	reconcileOpts := reconcileApplyOptions{
		ConfigPath:         *configPath,
		StatusFile:         *statusFile,
		NetplanPath:        *netplanPath,
		DnsmasqConfigPath:  *dnsmasqConfigPath,
		DnsmasqServicePath: *dnsmasqServicePath,
		NftablesPath:       *nftablesPath,
	}
	applyMu := &sync.Mutex{}
	if *reconcileInterval > 0 {
		go runReconcileSchedule(stop, *reconcileInterval, router, reconcileOpts, cache, logger, applyMu)
	}

	if err := os.MkdirAll(filepathDir(*socketPath), 0755); err != nil {
		return err
	}
	_ = os.Remove(*socketPath)
	listener, err := net.Listen("unix", *socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(*socketPath, 0660); err != nil {
		return err
	}

	handler := controlapi.Handler{
		Status: func(r *http.Request) (*controlapi.Status, error) {
			status := controlapi.NewStatus(cache.Load())
			return &status, nil
		},
		NAPT: func(r *http.Request, req controlapi.NAPTRequest) (*controlapi.NAPTTable, error) {
			table, err := observe.NAPT(req.Limit)
			if err != nil {
				return nil, err
			}
			apiTable := controlapi.NewNAPTTable(table)
			return &apiTable, nil
		},
		Reconcile: func(r *http.Request, req controlapi.ReconcileRequest) (*controlapi.ReconcileResult, error) {
			opts := reconcileOpts
			opts.DryRun = req.DryRun
			applyMu.Lock()
			defer applyMu.Unlock()
			result, err := runReconcileOnce(router, opts, io.Discard, logger)
			if err != nil {
				return nil, err
			}
			cache.Store(result)
			apiResult := controlapi.NewReconcileResult(result)
			return &apiResult, nil
		},
	}
	server := &http.Server{Handler: handler}
	fmt.Fprintf(stdout, "routerd serving control API on unix://%s\n", *socketPath)
	return server.Serve(listener)
}

type resultCache struct {
	mu     sync.RWMutex
	result *reconcile.Result
}

func (c *resultCache) Store(result *reconcile.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.result = result
}

func (c *resultCache) Load() *reconcile.Result {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.result
}

func runObserveSchedule(stop <-chan struct{}, interval time.Duration, router *api.Router, cache *resultCache, statusFile string, logger *eventlog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			engine := reconcile.New()
			result, err := engine.Observe(router)
			if err != nil {
				logger.Emit(eventlog.LevelWarning, "serve", "scheduled observe failed", map[string]string{"error": err.Error()})
				continue
			}
			cache.Store(result)
			if err := statuswriter.Write(statusFile, result); err != nil {
				logger.Emit(eventlog.LevelWarning, "serve", "scheduled status write failed", map[string]string{"error": err.Error()})
				continue
			}
			logger.Emit(eventlog.LevelDebug, "serve", "scheduled observe completed", map[string]string{"phase": result.Phase})
		}
	}
}

func runReconcileSchedule(stop <-chan struct{}, interval time.Duration, router *api.Router, opts reconcileApplyOptions, cache *resultCache, logger *eventlog.Logger, applyMu *sync.Mutex) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			applyMu.Lock()
			result, err := runReconcileOnce(router, opts, io.Discard, logger)
			applyMu.Unlock()
			if err != nil {
				logger.Emit(eventlog.LevelError, "serve", "scheduled reconcile failed", map[string]string{"error": err.Error()})
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
	out, err := exec.Command("ip", "-brief", "-6", "addr", "show", "dev", ifname).CombinedOutput()
	if err != nil {
		return nil
	}
	var addrs []string
	for _, field := range strings.Fields(string(out)) {
		addrPart, _, ok := strings.Cut(field, "/")
		if !ok {
			continue
		}
		addr, err := netip.ParseAddr(addrPart)
		if err == nil && addr.Is6() {
			addrs = append(addrs, addr.String())
		}
	}
	return addrs
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

func applyRuntimeSysctls(router *api.Router) ([]string, error) {
	var applied []string
	for _, res := range router.Spec.Resources {
		if res.Kind != "Sysctl" {
			continue
		}
		spec, err := res.SysctlSpec()
		if err != nil {
			return nil, err
		}
		if !api.BoolDefault(spec.Runtime, true) {
			continue
		}
		currentOut, err := exec.Command("sysctl", "-n", spec.Key).CombinedOutput()
		if err == nil && strings.TrimSpace(string(currentOut)) == spec.Value {
			continue
		}
		if err := runLogged("sysctl", "-w", spec.Key+"="+spec.Value); err != nil {
			return nil, err
		}
		applied = append(applied, spec.Key)
	}
	return applied, nil
}

func applyIPv4ReversePathFilters(router *api.Router) ([]string, error) {
	aliases := map[string]string{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Interface":
			spec, err := res.InterfaceSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = spec.IfName
		case "DSLiteTunnel":
			spec, err := res.DSLiteTunnelSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = defaultString(spec.TunnelName, res.Metadata.Name)
		}
	}
	var applied []string
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv4ReversePathFilter" {
			continue
		}
		spec, err := res.IPv4ReversePathFilterSpec()
		if err != nil {
			return nil, err
		}
		target := spec.Target
		if target == "interface" {
			target = aliases[spec.Interface]
		}
		if target == "" {
			return nil, fmt.Errorf("%s references target with empty interface name", res.ID())
		}
		key := "net.ipv4.conf." + target + ".rp_filter"
		value, err := ipv4ReversePathFilterValue(spec.Mode)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.ID(), err)
		}
		currentOut, err := exec.Command("sysctl", "-n", key).CombinedOutput()
		if err == nil && strings.TrimSpace(string(currentOut)) == value {
			continue
		}
		if err := runLogged("sysctl", "-w", key+"="+value); err != nil {
			return nil, err
		}
		applied = append(applied, key)
	}
	return applied, nil
}

func ipv4ReversePathFilterValue(mode string) (string, error) {
	switch mode {
	case "disabled":
		return "0", nil
	case "strict":
		return "1", nil
	case "loose":
		return "2", nil
	default:
		return "", fmt.Errorf("unsupported rp_filter mode %q", mode)
	}
}

func applyNetworkConfig(netplanPath string, netplanData []byte, networkdFiles []render.File) ([]string, error) {
	changedNetworkdFiles, err := applyFiles(networkdFiles)
	if err != nil {
		return nil, err
	}
	netplanChanged, err := writeFileIfChanged(netplanPath, netplanData, 0600)
	if err != nil {
		return nil, fmt.Errorf("write netplan %s: %w", netplanPath, err)
	}
	var changedFiles []string
	changedFiles = append(changedFiles, changedNetworkdFiles...)
	if netplanChanged {
		changedFiles = append(changedFiles, netplanPath)
	}
	if len(changedFiles) == 0 {
		return nil, nil
	}
	if netplanChanged {
		if err := runLogged("netplan", "generate"); err != nil {
			return nil, err
		}
		if err := runLogged("netplan", "apply"); err != nil {
			return nil, err
		}
	} else {
		for _, ifname := range changedNetworkdInterfaces(changedNetworkdFiles) {
			if err := runLogged("networkctl", "reconfigure", ifname); err != nil {
				return nil, err
			}
		}
	}
	return changedFiles, nil
}

func applyNftablesConfig(path string, data []byte) ([]string, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if _, err := exec.LookPath("nft"); err != nil {
		return nil, fmt.Errorf("nft is required for managed IPv4 source NAT: %w", err)
	}
	if err := os.MkdirAll(filepathDir(path), 0755); err != nil {
		return nil, fmt.Errorf("create directory for %s: %w", path, err)
	}
	changed, err := writeFileIfChanged(path, data, 0644)
	if err != nil {
		return nil, fmt.Errorf("write nftables config %s: %w", path, err)
	}
	natMissing := exec.Command("nft", "list", "table", "ip", "routerd_nat").Run() != nil
	policyMissing := bytes.Contains(data, []byte("table ip routerd_policy")) && exec.Command("nft", "list", "table", "ip", "routerd_policy").Run() != nil
	if !changed && !natMissing && !policyMissing {
		return nil, nil
	}
	_ = exec.Command("nft", "delete", "table", "ip", "routerd_nat").Run()
	_ = exec.Command("nft", "delete", "table", "ip", "routerd_policy").Run()
	if err := runLogged("nft", "-f", path); err != nil {
		return nil, err
	}
	if changed {
		return []string{path}, nil
	}
	return []string{"nftables:routerd"}, nil
}

func applyIPv4PolicyRoutes(router *api.Router) ([]string, error) {
	aliases := map[string]string{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Interface":
			spec, err := res.InterfaceSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = spec.IfName
		case "DSLiteTunnel":
			spec, err := res.DSLiteTunnelSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = defaultString(spec.TunnelName, res.Metadata.Name)
		}
	}
	var applied []string
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "IPv4PolicyRoute":
			spec, err := res.IPv4PolicyRouteSpec()
			if err != nil {
				return nil, err
			}
			target := api.IPv4PolicyRouteTarget{
				Name:              res.Metadata.Name,
				OutboundInterface: spec.OutboundInterface,
				Table:             spec.Table,
				Priority:          spec.Priority,
				Mark:              spec.Mark,
				RouteMetric:       spec.RouteMetric,
			}
			label, err := applyIPv4PolicyRouteTarget(res.ID(), aliases, target)
			if err != nil {
				return nil, err
			}
			applied = append(applied, label)
		case "IPv4PolicyRouteSet":
			spec, err := res.IPv4PolicyRouteSetSpec()
			if err != nil {
				return nil, err
			}
			for i, target := range spec.Targets {
				targetName := target.Name
				if targetName == "" {
					targetName = fmt.Sprintf("%s-%d", res.Metadata.Name, i)
				}
				target.Name = targetName
				label, err := applyIPv4PolicyRouteTarget(res.ID(), aliases, target)
				if err != nil {
					return nil, err
				}
				applied = append(applied, label)
			}
		}
	}
	return applied, nil
}

func applyIPv4PolicyRouteTarget(resourceID string, aliases map[string]string, target api.IPv4PolicyRouteTarget) (string, error) {
	ifname := aliases[target.OutboundInterface]
	if ifname == "" {
		return "", fmt.Errorf("%s references outbound interface with empty ifname", resourceID)
	}
	metric := target.RouteMetric
	if metric == 0 {
		metric = 50
	}
	if err := runLogged("ip", "-4", "route", "replace", "default", "dev", ifname, "table", fmt.Sprintf("%d", target.Table), "metric", fmt.Sprintf("%d", metric)); err != nil {
		return "", fmt.Errorf("%s route table: %w", resourceID, err)
	}
	if err := ensureIPv4FwmarkRule(target.Priority, target.Mark, target.Table); err != nil {
		return "", fmt.Errorf("%s policy rule: %w", resourceID, err)
	}
	name := target.Name
	if name == "" {
		name = target.OutboundInterface
	}
	return fmt.Sprintf("%s(table=%d,mark=0x%x)", name, target.Table, target.Mark), nil
}

func ensureIPv4FwmarkRule(priority, mark, table int) error {
	priorityText := fmt.Sprintf("%d", priority)
	markText := fmt.Sprintf("0x%x", mark)
	tableText := fmt.Sprintf("%d", table)
	out, err := exec.Command("ip", "-4", "rule", "show", "priority", priorityText).CombinedOutput()
	if err == nil {
		line := string(out)
		if strings.Contains(line, "fwmark "+markText) && strings.Contains(line, "lookup "+tableText) {
			return nil
		}
	}
	_ = exec.Command("ip", "-4", "rule", "del", "priority", priorityText).Run()
	return runLogged("ip", "-4", "rule", "add", "priority", priorityText, "fwmark", markText, "table", tableText)
}

func applyDSLiteTunnels(router *api.Router) ([]string, error) {
	aliases := map[string]string{}
	delegated := map[string]api.IPv6DelegatedAddressSpec{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Interface":
			spec, err := res.InterfaceSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = spec.IfName
		case "IPv6DelegatedAddress":
			spec, err := res.IPv6DelegatedAddressSpec()
			if err != nil {
				return nil, err
			}
			delegated[res.Metadata.Name] = spec
		}
	}
	var applied []string
	for _, res := range router.Spec.Resources {
		if res.Kind != "DSLiteTunnel" {
			continue
		}
		spec, err := res.DSLiteTunnelSpec()
		if err != nil {
			return nil, err
		}
		ifname := aliases[spec.Interface]
		tunnelName := defaultString(spec.TunnelName, res.Metadata.Name)
		remote := spec.RemoteAddress
		if remote == "" {
			remote, err = resolveAAAAWithServers(spec.AFTRFQDN, spec.AFTRDNSServers, spec.AFTRAddressOrdinal, spec.AFTRAddressSelection)
			if err != nil {
				return nil, fmt.Errorf("%s resolve AFTR: %w", res.ID(), err)
			}
		}
		local, localIfName, err := dsliteLocalAddress(spec, ifname, aliases, delegated)
		if err != nil {
			return nil, fmt.Errorf("%s local address: %w", res.ID(), err)
		}
		if localIfName != "" {
			ensured, err := ensureIPv6LocalAddress(localIfName, local)
			if err != nil {
				return nil, fmt.Errorf("%s ensure local address: %w", res.ID(), err)
			}
			if ensured {
				applied = append(applied, localIfName+":"+local)
			}
		}
		changed, err := ensureDSLiteTunnel(tunnelName, ifname, local, remote, spec)
		if err != nil {
			return nil, fmt.Errorf("%s apply tunnel: %w", res.ID(), err)
		}
		if changed {
			applied = append(applied, tunnelName)
		}
	}
	return applied, nil
}

func dsliteLocalAddress(spec api.DSLiteTunnelSpec, ifname string, aliases map[string]string, delegated map[string]api.IPv6DelegatedAddressSpec) (string, string, error) {
	switch defaultString(spec.LocalAddressSource, "interface") {
	case "interface":
		if spec.LocalAddress != "" {
			return spec.LocalAddress, "", nil
		}
		local := firstGlobalIPv6(ipv6Addresses(ifname))
		if local == "" {
			return "", "", fmt.Errorf("no global IPv6 address on %s", ifname)
		}
		return local, "", nil
	case "static":
		if spec.LocalAddress == "" {
			return "", "", fmt.Errorf("localAddress is required")
		}
		return spec.LocalAddress, "", nil
	case "delegatedAddress":
		delegatedSpec, ok := delegated[spec.LocalDelegatedAddress]
		if !ok {
			return "", "", fmt.Errorf("missing IPv6DelegatedAddress %q", spec.LocalDelegatedAddress)
		}
		localIfName := aliases[delegatedSpec.Interface]
		if localIfName == "" {
			return "", "", fmt.Errorf("missing Interface %q for delegated address %q", delegatedSpec.Interface, spec.LocalDelegatedAddress)
		}
		suffix := defaultString(spec.LocalAddressSuffix, delegatedSpec.AddressSuffix)
		local, err := deriveIPv6AddressFromInterface(localIfName, suffix)
		if err != nil {
			return "", "", err
		}
		return local, localIfName, nil
	default:
		return "", "", fmt.Errorf("unsupported localAddressSource %q", spec.LocalAddressSource)
	}
}

func deriveIPv6AddressFromInterface(ifname, suffix string) (string, error) {
	return deriveIPv6Address(ipv6Prefixes(ifname), suffix)
}

func deriveIPv6Address(prefixes []string, suffix string) (string, error) {
	suffixAddr, err := netip.ParseAddr(suffix)
	if err != nil || !suffixAddr.Is6() {
		return "", fmt.Errorf("invalid IPv6 suffix %q", suffix)
	}
	suffixBytes := suffixAddr.As16()
	for _, value := range prefixes {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !prefix.Addr().Is6() {
			continue
		}
		addrBytes := prefix.Masked().Addr().As16()
		for i := range addrBytes {
			addrBytes[i] |= suffixBytes[i]
		}
		return netip.AddrFrom16(addrBytes).String(), nil
	}
	return "", fmt.Errorf("no IPv6 prefix available")
}

func ensureIPv6LocalAddress(ifname, address string) (bool, error) {
	for _, value := range ipv6Addresses(ifname) {
		if value == address {
			return false, nil
		}
	}
	if err := runLogged("ip", "-6", "addr", "add", address+"/128", "dev", ifname); err != nil {
		return false, err
	}
	return true, nil
}

func ensureDSLiteTunnel(name, ifname, local, remote string, spec api.DSLiteTunnelSpec) (bool, error) {
	desiredRouteMetric := spec.RouteMetric
	if desiredRouteMetric == 0 {
		desiredRouteMetric = 50
	}
	encapLimit := defaultString(spec.EncapsulationLimit, "none")
	show, showErr := exec.Command("ip", "-6", "tunnel", "show", name).CombinedOutput()
	needsRecreate := showErr != nil || !strings.Contains(string(show), "remote "+remote) || !strings.Contains(string(show), "local "+local)
	if needsRecreate {
		_ = exec.Command("ip", "-6", "tunnel", "del", name).Run()
		args := []string{"-6", "tunnel", "add", name, "mode", "ipip6", "remote", remote, "local", local, "dev", ifname, "encaplimit", encapLimit}
		if err := runLogged("ip", args...); err != nil {
			return false, err
		}
	}
	if spec.MTU != 0 {
		if err := runLogged("ip", "link", "set", "dev", name, "mtu", fmt.Sprintf("%d", spec.MTU)); err != nil {
			return false, err
		}
	}
	if err := runLogged("ip", "link", "set", "dev", name, "up"); err != nil {
		return false, err
	}
	if spec.DefaultRoute {
		routeOut, routeErr := exec.Command("ip", "-4", "route", "show", "default", "dev", name, "metric", fmt.Sprintf("%d", desiredRouteMetric)).CombinedOutput()
		routeMissing := routeErr != nil || strings.TrimSpace(string(routeOut)) == ""
		if routeMissing {
			if err := runLogged("ip", "-4", "route", "replace", "default", "dev", name, "metric", fmt.Sprintf("%d", desiredRouteMetric)); err != nil {
				return false, err
			}
			needsRecreate = true
		}
	}
	return needsRecreate, nil
}

func resolveAAAAWithServers(host string, servers []string, ordinal int, selection string) (string, error) {
	if len(servers) == 0 {
		return resolveAAAA(host, "", ordinal, selection)
	}
	var lastErr error
	for _, server := range servers {
		value, err := resolveAAAA(host, server, ordinal, selection)
		if err == nil {
			return value, nil
		}
		lastErr = err
	}
	return "", lastErr
}

func resolveAAAA(host, server string, ordinal int, selection string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resolver := net.DefaultResolver
	if server != "" {
		resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "udp", net.JoinHostPort(server, "53"))
			},
		}
	}
	addrs, err := resolver.LookupNetIP(ctx, "ip6", host)
	if err != nil {
		return "", err
	}
	var values []string
	for _, addr := range addrs {
		if addr.Is6() {
			values = append(values, addr.String())
		}
	}
	sort.Strings(values)
	if len(values) == 0 {
		return "", fmt.Errorf("no AAAA records for %s", host)
	}
	return selectAAAA(values, ordinal, selection)
}

func selectAAAA(values []string, ordinal int, selection string) (string, error) {
	if len(values) == 0 {
		return "", fmt.Errorf("no AAAA records")
	}
	if ordinal == 0 {
		ordinal = 1
	}
	if selection == "" {
		selection = "ordinal"
	}
	if selection == "ordinalModulo" {
		index := (ordinal - 1) % len(values)
		return values[index], nil
	}
	if ordinal < 1 || ordinal > len(values) {
		return "", fmt.Errorf("AAAA ordinal %d is outside available record count %d", ordinal, len(values))
	}
	return values[ordinal-1], nil
}

func firstGlobalIPv6(values []string) string {
	for _, value := range values {
		addr, err := netip.ParseAddr(value)
		if err == nil && addr.Is6() && !addr.IsLinkLocalUnicast() {
			return addr.String()
		}
	}
	return ""
}

func applyDnsmasqConfig(configPath, servicePath string, configData []byte) ([]string, error) {
	if len(configData) == 0 {
		return nil, nil
	}
	if _, err := exec.LookPath("dnsmasq"); err != nil {
		return nil, fmt.Errorf("dnsmasq is required for managed IPv4 DHCP service: %w", err)
	}

	var changedFiles []string
	if err := os.MkdirAll(filepathDir(configPath), 0755); err != nil {
		return nil, fmt.Errorf("create directory for %s: %w", configPath, err)
	}
	changed, err := writeFileIfChanged(configPath, configData, 0644)
	if err != nil {
		return nil, fmt.Errorf("write dnsmasq config %s: %w", configPath, err)
	}
	if changed {
		changedFiles = append(changedFiles, configPath)
	}

	if err := os.MkdirAll(filepathDir(servicePath), 0755); err != nil {
		return nil, fmt.Errorf("create directory for %s: %w", servicePath, err)
	}
	serviceChanged, err := writeFileIfChanged(servicePath, render.DnsmasqServiceUnit(configPath), 0644)
	if err != nil {
		return nil, fmt.Errorf("write dnsmasq service %s: %w", servicePath, err)
	}
	if serviceChanged {
		changedFiles = append(changedFiles, servicePath)
		if err := runLogged("systemctl", "daemon-reload"); err != nil {
			return nil, err
		}
	}

	if len(changedFiles) > 0 {
		if err := runLogged("systemctl", "enable", routerdDnsmasqService); err != nil {
			return nil, err
		}
		if err := runLogged("systemctl", "restart", routerdDnsmasqService); err != nil {
			return nil, err
		}
		return changedFiles, nil
	}
	if err := runLogged("systemctl", "is-active", "--quiet", routerdDnsmasqService); err != nil {
		if err := runLogged("systemctl", "enable", "--now", routerdDnsmasqService); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func applyFiles(files []render.File) ([]string, error) {
	var changedFiles []string
	for _, file := range files {
		if err := os.MkdirAll(filepathDir(file.Path), 0755); err != nil {
			return nil, fmt.Errorf("create directory for %s: %w", file.Path, err)
		}
		changed, err := writeFileIfChanged(file.Path, file.Data, 0644)
		if err != nil {
			return nil, fmt.Errorf("write %s: %w", file.Path, err)
		}
		if changed {
			changedFiles = append(changedFiles, file.Path)
		}
	}
	return changedFiles, nil
}

func filepathDir(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx <= 0 {
		return "."
	}
	return path[:idx]
}

func writeFileIfChanged(path string, data []byte, perm os.FileMode) (bool, error) {
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, data) {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return false, err
	}
	return true, nil
}

func changedNetworkdInterfaces(paths []string) []string {
	var ifnames []string
	for _, path := range paths {
		base := filepathBase(filepathDir(path))
		if strings.HasSuffix(base, ".network.d") {
			base = strings.TrimSuffix(base, ".network.d")
		}
		if strings.HasPrefix(base, "10-netplan-") {
			base = strings.TrimPrefix(base, "10-netplan-")
		}
		if base != "" {
			ifnames = append(ifnames, base)
		}
	}
	return uniqueStrings(ifnames)
}

func filepathBase(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func runLogged(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w: %s", name, args, err, string(out))
	}
	return nil
}

func closeLogger(logger *eventlog.Logger, command string, errp *error) {
	if logger == nil {
		return
	}
	if *errp != nil {
		logger.Emit(eventlog.LevelError, command, "routerd command failed", map[string]string{"error": (*errp).Error()})
	} else {
		logger.Emit(eventlog.LevelInfo, command, "routerd command completed", nil)
	}
	if err := logger.Close(); err != nil && *errp == nil {
		*errp = err
	}
}

func statusCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	statusFile := fs.String("status-file", defaultStatusFile(), "status file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "status file: %s\n", *statusFile)
	return nil
}

func pluginCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("missing plugin subcommand")
	}

	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("plugin list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		pluginDir := fs.String("plugin-dir", defaultPluginDir, "plugin directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "plugin listing is not implemented yet for %s\n", *pluginDir)
		return nil
	case "inspect":
		fs := flag.NewFlagSet("plugin inspect", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		pluginDir := fs.String("plugin-dir", defaultPluginDir, "plugin directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New("plugin inspect requires a plugin name")
		}
		fmt.Fprintf(stdout, "plugin inspect is not implemented yet for %s in %s\n", fs.Arg(0), *pluginDir)
		return nil
	default:
		return fmt.Errorf("unknown plugin subcommand %q", args[0])
	}
}

func requireExistingFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	return nil
}

func defaultRuntimeDir() string {
	if runtime.GOOS == "freebsd" {
		return "/var/run/routerd"
	}
	return "/run/routerd"
}

func defaultStateDir() string {
	if runtime.GOOS == "freebsd" {
		return "/var/db/routerd"
	}
	return "/var/lib/routerd"
}

func defaultStatusFile() string {
	return defaultRuntimeDir() + "/status.json"
}

func defaultSocketPath() string {
	return defaultRuntimeDir() + "/routerd.sock"
}

func writeResult(stdout io.Writer, statusFile string, result *reconcile.Result) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, string(data))
	if statusFile != "" {
		if err := statuswriter.Write(statusFile, result); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote status %s\n", statusFile)
	}
	return nil
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerd <command> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  validate --config <path>")
	fmt.Fprintln(w, "  observe --config <path>")
	fmt.Fprintln(w, "  plan --config <path>")
	fmt.Fprintln(w, "  reconcile --config <path> --once [--dry-run]")
	fmt.Fprintln(w, "  serve --config <path> [--socket <path>]")
	fmt.Fprintln(w, "  run --config <path>")
	fmt.Fprintln(w, "  status [--status-file <path>]")
	fmt.Fprintln(w, "  plugin list --plugin-dir <path>")
	fmt.Fprintln(w, "  plugin inspect <plugin-name> --plugin-dir <path>")
}
