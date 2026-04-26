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
	defaultRouteNftablesPath  = "/usr/local/etc/routerd/default-route.nft"
	defaultTimesyncdPath      = "/etc/systemd/timesyncd.conf.d/routerd.conf"
	routerdDnsmasqService     = "routerd-dnsmasq.service"
	pppoeCHAPSecretsPath      = "/etc/ppp/chap-secrets"
	pppoePAPSecretsPath       = "/etc/ppp/pap-secrets"
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
		timesyncdConfig, err := render.TimesyncdConfig(router)
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
		appliedIPv6DelegatedAddresses, err := applyIPv6DelegatedAddresses(router)
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
		pppoeChangedFiles, err := applyPPPoEConfig(router)
		if err != nil {
			return nil, err
		}
		timesyncdChangedFiles, err := applyTimesyncdConfig(defaultTimesyncdPath, timesyncdConfig)
		if err != nil {
			return nil, err
		}
		appliedTunnels, err := applyDSLiteTunnels(router)
		if err != nil {
			return nil, err
		}
		appliedPolicyRoutes, err := applyIPv4PolicyRoutes(router)
		if err != nil {
			return nil, err
		}
		appliedDefaultRoutes, err := applyIPv4DefaultRoutePolicies(router)
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
		changedFiles := append(networkChangedFiles, dnsmasqChangedFiles...)
		changedFiles = append(changedFiles, nftablesChangedFiles...)
		changedFiles = append(changedFiles, pppoeChangedFiles...)
		changedFiles = append(changedFiles, timesyncdChangedFiles...)
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
			if len(pppoeChangedFiles) > 0 {
				fmt.Fprintln(stdout, "applied PPPoE")
			}
			if len(timesyncdChangedFiles) > 0 {
				fmt.Fprintln(stdout, "applied NTP client")
			}
		}
		for _, key := range appliedRuntime {
			fmt.Fprintf(stdout, "applied sysctl %s\n", key)
		}
		for _, key := range appliedReversePathFilters {
			fmt.Fprintf(stdout, "applied IPv4 reverse path filter %s\n", key)
		}
		for _, address := range appliedIPv6DelegatedAddresses {
			fmt.Fprintf(stdout, "applied IPv6 delegated address %s\n", address)
		}
		for _, tunnel := range appliedTunnels {
			fmt.Fprintf(stdout, "applied DS-Lite tunnel %s\n", tunnel)
		}
		for _, route := range appliedDefaultRoutes {
			fmt.Fprintf(stdout, "applied IPv4 default route %s\n", route)
		}
		for _, route := range appliedPolicyRoutes {
			fmt.Fprintf(stdout, "applied IPv4 policy route %s\n", route)
		}
		logger.Emit(eventlog.LevelInfo, "reconcile", "routerd changes applied", map[string]string{
			"changedFiles":        fmt.Sprintf("%d", len(changedFiles)),
			"runtimeSysctls":      fmt.Sprintf("%d", len(appliedRuntime)),
			"reversePathFilters":  fmt.Sprintf("%d", len(appliedReversePathFilters)),
			"ipv6DelegatedAddrs":  fmt.Sprintf("%d", len(appliedIPv6DelegatedAddresses)),
			"pppoeFiles":          fmt.Sprintf("%d", len(pppoeChangedFiles)),
			"ntpFiles":            fmt.Sprintf("%d", len(timesyncdChangedFiles)),
			"dsliteTunnels":       fmt.Sprintf("%d", len(appliedTunnels)),
			"ipv4DefaultRoutes":   fmt.Sprintf("%d", len(appliedDefaultRoutes)),
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
		case "PPPoEInterface":
			spec, err := res.PPPoEInterfaceSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = defaultString(spec.IfName, "ppp-"+res.Metadata.Name)
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

func applyIPv6DelegatedAddresses(router *api.Router) ([]string, error) {
	aliases := map[string]string{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "Interface" {
			continue
		}
		spec, err := res.InterfaceSpec()
		if err != nil {
			return nil, err
		}
		aliases[res.Metadata.Name] = spec.IfName
	}
	var applied []string
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv6DelegatedAddress" {
			continue
		}
		spec, err := res.IPv6DelegatedAddressSpec()
		if err != nil {
			return nil, err
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			return nil, fmt.Errorf("%s references interface with empty ifname", res.ID())
		}
		address, err := deriveIPv6AddressFromInterface(ifname, spec.AddressSuffix)
		if err != nil {
			return nil, fmt.Errorf("%s derive delegated address: %w", res.ID(), err)
		}
		ensured, err := ensureIPv6LocalAddress(ifname, address)
		if err != nil {
			return nil, fmt.Errorf("%s ensure delegated address: %w", res.ID(), err)
		}
		if ensured {
			applied = append(applied, ifname+":"+address)
		}
	}
	return applied, nil
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
		case "PPPoEInterface":
			spec, err := res.PPPoEInterfaceSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = defaultString(spec.IfName, "ppp-"+res.Metadata.Name)
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

func applyIPv4DefaultRoutePolicies(router *api.Router) ([]string, error) {
	aliases, err := outboundAliases(router)
	if err != nil {
		return nil, err
	}
	routeSets, err := ipv4PolicyRouteSets(router)
	if err != nil {
		return nil, err
	}
	healthChecks, err := evaluateHealthChecks(router, aliases)
	if err != nil {
		return nil, err
	}
	var applied []string
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv4DefaultRoutePolicy" {
			continue
		}
		spec, err := res.IPv4DefaultRoutePolicySpec()
		if err != nil {
			return nil, err
		}
		candidate, ok := selectIPv4DefaultRouteCandidate(spec.Candidates, healthChecks)
		if !ok {
			return nil, fmt.Errorf("%s has no healthy IPv4 default route candidate", res.ID())
		}
		var healthy []api.IPv4DefaultRoutePolicyCandidate
		for _, target := range spec.Candidates {
			if target.HealthCheck != "" && !healthChecks[target.HealthCheck] {
				continue
			}
			healthy = append(healthy, target)
			if target.RouteSet != "" {
				continue
			}
			label, err := applyIPv4DefaultRouteCandidate(res.ID(), aliases, target)
			if err != nil {
				return nil, err
			}
			applied = append(applied, label)
		}
		if err := applyIPv4DefaultRoutePolicyMarks(res.ID(), spec, candidate, healthy, routeSets); err != nil {
			return nil, err
		}
		applied = append(applied, "active="+defaultRouteCandidateLabel(candidate))
	}
	return applied, nil
}

func ipv4PolicyRouteSets(router *api.Router) (map[string]api.IPv4PolicyRouteSetSpec, error) {
	routeSets := map[string]api.IPv4PolicyRouteSetSpec{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv4PolicyRouteSet" {
			continue
		}
		spec, err := res.IPv4PolicyRouteSetSpec()
		if err != nil {
			return nil, err
		}
		routeSets[res.Metadata.Name] = spec
	}
	return routeSets, nil
}

func defaultRouteCandidateLabel(candidate api.IPv4DefaultRoutePolicyCandidate) string {
	if candidate.Name != "" {
		return candidate.Name
	}
	if candidate.RouteSet != "" {
		return candidate.RouteSet
	}
	return candidate.Interface
}

func outboundAliases(router *api.Router) (map[string]string, error) {
	aliases := map[string]string{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Interface":
			spec, err := res.InterfaceSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = spec.IfName
		case "PPPoEInterface":
			spec, err := res.PPPoEInterfaceSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = defaultString(spec.IfName, "ppp-"+res.Metadata.Name)
		case "DSLiteTunnel":
			spec, err := res.DSLiteTunnelSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = defaultString(spec.TunnelName, res.Metadata.Name)
		}
	}
	return aliases, nil
}

func evaluateHealthChecks(router *api.Router, aliases map[string]string) (map[string]bool, error) {
	result := map[string]bool{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "HealthCheck" {
			continue
		}
		spec, err := res.HealthCheckSpec()
		if err != nil {
			return nil, err
		}
		healthy, err := runHealthCheck(router, spec, aliases)
		if err != nil {
			return nil, fmt.Errorf("%s health check: %w", res.ID(), err)
		}
		result[res.Metadata.Name] = healthy
	}
	return result, nil
}

func runHealthCheck(router *api.Router, spec api.HealthCheckSpec, aliases map[string]string) (bool, error) {
	if defaultString(spec.Type, "ping") != "ping" {
		return false, fmt.Errorf("unsupported health check type %q", spec.Type)
	}
	target, family, err := resolveHealthCheckTarget(router, spec, aliases)
	if err != nil {
		return false, nil
	}
	timeout := defaultString(spec.Timeout, "3s")
	duration, err := time.ParseDuration(timeout)
	if err != nil {
		return false, err
	}
	if duration < time.Second {
		duration = time.Second
	}
	cmdName := "ping"
	args := []string{"-c", "1", "-W", fmt.Sprintf("%d", int(duration.Seconds()))}
	if family == "ipv6" {
		cmdName = "ping"
		args = append([]string{"-6"}, args...)
	} else {
		args = append([]string{"-4"}, args...)
	}
	if spec.Interface != "" {
		ifname := healthCheckPingInterface(router, spec, aliases)
		if ifname == "" {
			return false, fmt.Errorf("missing ifname for %s", spec.Interface)
		}
		args = append(args, "-I", ifname)
	}
	args = append(args, target)
	ctx, cancel := context.WithTimeout(context.Background(), duration+time.Second)
	defer cancel()
	err = exec.CommandContext(ctx, cmdName, args...).Run()
	return err == nil, nil
}

func healthCheckPingInterface(router *api.Router, spec api.HealthCheckSpec, aliases map[string]string) string {
	if defaultString(spec.TargetSource, "auto") == "dsliteRemote" || (spec.TargetSource == "" && healthInterfaceKind(router, spec.Interface) == "DSLiteTunnel") {
		for _, res := range router.Spec.Resources {
			if res.Kind != "DSLiteTunnel" || res.Metadata.Name != spec.Interface {
				continue
			}
			tunnel, err := res.DSLiteTunnelSpec()
			if err != nil {
				return ""
			}
			return aliases[tunnel.Interface]
		}
	}
	return aliases[spec.Interface]
}

func resolveHealthCheckTarget(router *api.Router, spec api.HealthCheckSpec, aliases map[string]string) (string, string, error) {
	if spec.Target != "" {
		family := spec.AddressFamily
		if family == "" {
			addr, err := netip.ParseAddr(spec.Target)
			if err != nil {
				return "", "", err
			}
			if addr.Is6() {
				family = "ipv6"
			} else {
				family = "ipv4"
			}
		}
		return spec.Target, family, nil
	}
	source := defaultString(spec.TargetSource, "auto")
	if source == "auto" {
		if healthInterfaceKind(router, spec.Interface) == "DSLiteTunnel" {
			source = "dsliteRemote"
		} else {
			source = "defaultGateway"
		}
	}
	switch source {
	case "defaultGateway":
		ifname := aliases[spec.Interface]
		if ifname == "" {
			return "", "", fmt.Errorf("missing ifname for %s", spec.Interface)
		}
		target, err := currentIPv4HealthTargetForInterface(ifname)
		if err != nil {
			return "", "", err
		}
		return target, "ipv4", nil
	case "dsliteRemote":
		target, err := dsliteRemoteAddress(router, spec.Interface)
		if err != nil {
			return "", "", err
		}
		return target, "ipv6", nil
	case "static":
		return "", "", fmt.Errorf("target is required when targetSource is static")
	default:
		return "", "", fmt.Errorf("unsupported targetSource %q", source)
	}
}

func healthInterfaceKind(router *api.Router, name string) string {
	for _, res := range router.Spec.Resources {
		if res.Metadata.Name == name {
			return res.Kind
		}
	}
	return ""
}

func dsliteRemoteAddress(router *api.Router, name string) (string, error) {
	for _, res := range router.Spec.Resources {
		if res.Kind != "DSLiteTunnel" || res.Metadata.Name != name {
			continue
		}
		spec, err := res.DSLiteTunnelSpec()
		if err != nil {
			return "", err
		}
		if spec.RemoteAddress != "" {
			return spec.RemoteAddress, nil
		}
		if spec.AFTRFQDN == "" {
			return "", fmt.Errorf("%s has no remoteAddress or aftrFQDN", res.ID())
		}
		return resolveAAAAWithServers(spec.AFTRFQDN, spec.AFTRDNSServers, spec.AFTRAddressOrdinal, spec.AFTRAddressSelection)
	}
	return "", fmt.Errorf("missing DSLiteTunnel %q", name)
}

func selectIPv4DefaultRouteCandidate(candidates []api.IPv4DefaultRoutePolicyCandidate, health map[string]bool) (api.IPv4DefaultRoutePolicyCandidate, bool) {
	ordered := append([]api.IPv4DefaultRoutePolicyCandidate{}, candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Priority < ordered[j].Priority
	})
	for _, candidate := range ordered {
		if candidate.HealthCheck != "" && !health[candidate.HealthCheck] {
			continue
		}
		return candidate, true
	}
	return api.IPv4DefaultRoutePolicyCandidate{}, false
}

func applyIPv4DefaultRouteCandidate(resourceID string, aliases map[string]string, candidate api.IPv4DefaultRoutePolicyCandidate) (string, error) {
	ifname := aliases[candidate.Interface]
	if ifname == "" {
		return "", fmt.Errorf("%s references default route interface with empty ifname", resourceID)
	}
	metric := candidate.RouteMetric
	if metric == 0 {
		metric = 50
	}
	source := defaultString(candidate.GatewaySource, "none")
	args := []string{"-4", "route", "replace", "default"}
	switch source {
	case "none":
		args = append(args, "dev", ifname)
	case "static":
		args = append(args, "via", candidate.Gateway, "dev", ifname)
	case "dhcp4":
		gateway, err := currentIPv4DefaultGatewayForInterface(ifname)
		if err != nil {
			return "", fmt.Errorf("%s DHCPv4 gateway on %s: %w", resourceID, ifname, err)
		}
		args = append(args, "via", gateway, "dev", ifname)
	default:
		return "", fmt.Errorf("unsupported gatewaySource %q", source)
	}
	args = append(args, "table", fmt.Sprintf("%d", candidate.Table), "metric", fmt.Sprintf("%d", metric))
	if err := runLogged("ip", args...); err != nil {
		return "", err
	}
	if err := ensureIPv4FwmarkRule(candidate.Priority, candidate.Mark, candidate.Table); err != nil {
		return "", err
	}
	name := defaultString(candidate.Name, candidate.Interface)
	return fmt.Sprintf("%s(%s,table=%d,mark=0x%x,metric=%d)", name, ifname, candidate.Table, candidate.Mark, metric), nil
}

func applyIPv4DefaultRoutePolicyMarks(resourceID string, spec api.IPv4DefaultRoutePolicySpec, active api.IPv4DefaultRoutePolicyCandidate, healthy []api.IPv4DefaultRoutePolicyCandidate, routeSets map[string]api.IPv4PolicyRouteSetSpec) error {
	if _, err := exec.LookPath("nft"); err != nil {
		return fmt.Errorf("nft is required for IPv4 default route policy: %w", err)
	}
	data, err := renderIPv4DefaultRoutePolicyMarks(resourceID, spec, active, healthy, routeSets)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepathDir(defaultRouteNftablesPath), 0755); err != nil {
		return err
	}
	if _, err := writeFileIfChanged(defaultRouteNftablesPath, data, 0644); err != nil {
		return err
	}
	_ = exec.Command("nft", "delete", "table", "ip", "routerd_default_route").Run()
	return runLogged("nft", "-f", defaultRouteNftablesPath)
}

func renderIPv4DefaultRoutePolicyMarks(resourceID string, spec api.IPv4DefaultRoutePolicySpec, active api.IPv4DefaultRoutePolicyCandidate, healthy []api.IPv4DefaultRoutePolicyCandidate, routeSets map[string]api.IPv4PolicyRouteSetSpec) ([]byte, error) {
	matches, err := ipv4PolicyMatches(resourceID, spec.SourceCIDRs, spec.DestinationCIDRs)
	if err != nil {
		return nil, err
	}
	healthyMarks := make([]string, 0, len(healthy))
	for _, candidate := range healthy {
		if candidate.RouteSet != "" {
			routeSet, ok := routeSets[candidate.RouteSet]
			if !ok {
				return nil, fmt.Errorf("%s references missing IPv4PolicyRouteSet %q", resourceID, candidate.RouteSet)
			}
			for _, target := range routeSet.Targets {
				healthyMarks = append(healthyMarks, fmt.Sprintf("0x%x", target.Mark))
			}
			continue
		}
		healthyMarks = append(healthyMarks, fmt.Sprintf("0x%x", candidate.Mark))
	}
	sort.Strings(healthyMarks)
	var buf bytes.Buffer
	buf.WriteString("# Generated by routerd. Do not edit by hand.\n")
	buf.WriteString("table ip routerd_default_route {\n")
	buf.WriteString("  chain prerouting {\n")
	buf.WriteString("    type filter hook prerouting priority -151; policy accept;\n")
	activeRouteSet := active.RouteSet != ""
	activeMark := fmt.Sprintf("0x%x", active.Mark)
	for _, match := range matches {
		prefix := strings.TrimSpace(match)
		if prefix != "" {
			prefix += " "
		}
		if len(healthyMarks) > 0 {
			set := "{ " + strings.Join(healthyMarks, ", ") + " }"
			buf.WriteString("    " + prefix + "ct mark " + set + " meta mark set ct mark\n")
			if activeRouteSet {
				buf.WriteString("    " + prefix + "ct mark != 0x0 ct mark != " + set + " meta mark set 0x0 ct mark set meta mark\n")
			} else {
				buf.WriteString("    " + prefix + "ct mark != 0x0 ct mark != " + set + " meta mark set " + activeMark + " ct mark set meta mark\n")
			}
		}
		if !activeRouteSet {
			buf.WriteString("    " + prefix + "ct mark 0x0 meta mark set " + activeMark + " ct mark set meta mark\n")
		}
	}
	buf.WriteString("  }\n")
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}

func ipv4PolicyMatches(resourceID string, sourceCIDRs, destinationCIDRs []string) ([]string, error) {
	var sources []string
	if len(sourceCIDRs) == 0 {
		sources = []string{""}
	} else {
		for _, cidr := range sourceCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return nil, fmt.Errorf("%s has invalid IPv4 source CIDR %q", resourceID, cidr)
			}
			sources = append(sources, "ip saddr "+prefix.Masked().String())
		}
	}
	var destinations []string
	if len(destinationCIDRs) == 0 {
		destinations = []string{""}
	} else {
		for _, cidr := range destinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return nil, fmt.Errorf("%s has invalid IPv4 destination CIDR %q", resourceID, cidr)
			}
			destinations = append(destinations, "ip daddr "+prefix.Masked().String())
		}
	}
	var matches []string
	for _, source := range sources {
		for _, destination := range destinations {
			matches = append(matches, strings.TrimSpace(strings.Join([]string{source, destination}, " ")))
		}
	}
	return matches, nil
}

func currentIPv4DefaultGatewayForInterface(ifname string) (string, error) {
	out, err := exec.Command("ip", "-4", "route", "show", "default", "dev", ifname).CombinedOutput()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	for i, field := range fields {
		if field == "via" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}
	return "", fmt.Errorf("no gateway found")
}

func currentIPv4HealthTargetForInterface(ifname string) (string, error) {
	if gateway, err := currentIPv4DefaultGatewayForInterface(ifname); err == nil {
		return gateway, nil
	}
	out, err := exec.Command("ip", "-4", "addr", "show", "dev", ifname).CombinedOutput()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	for i, field := range fields {
		if field == "peer" && i+1 < len(fields) {
			addr := strings.SplitN(fields[i+1], "/", 2)[0]
			if parsed, err := netip.ParseAddr(addr); err == nil && parsed.Is4() {
				return parsed.String(), nil
			}
		}
	}
	return "", fmt.Errorf("no IPv4 default gateway or peer found")
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

func applyTimesyncdConfig(path string, configData []byte) ([]string, error) {
	if len(configData) == 0 {
		return nil, nil
	}
	if _, err := exec.LookPath("timedatectl"); err != nil {
		return nil, fmt.Errorf("systemd-timesyncd support requires timedatectl: %w", err)
	}
	if err := os.MkdirAll(filepathDir(path), 0755); err != nil {
		return nil, err
	}
	changed, err := writeFileIfChanged(path, configData, 0644)
	if err != nil {
		return nil, err
	}
	if err := runLogged("timedatectl", "set-ntp", "true"); err != nil {
		return nil, err
	}
	if changed {
		if err := runLogged("systemctl", "restart", "systemd-timesyncd.service"); err != nil {
			return nil, err
		}
		return []string{path}, nil
	}
	if err := runLogged("systemctl", "is-active", "--quiet", "systemd-timesyncd.service"); err != nil {
		if err := runLogged("systemctl", "enable", "--now", "systemd-timesyncd.service"); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func applyPPPoEConfig(router *api.Router) ([]string, error) {
	config, err := render.PPPoE(router, pppoePassword)
	if err != nil {
		return nil, err
	}
	if len(config.Files) == 0 && len(config.Secrets) == 0 {
		return nil, nil
	}
	if !pppdAvailable() {
		return nil, errors.New("pppd is required for managed PPPoE interfaces")
	}

	var changedFiles []string
	for _, file := range config.Files {
		if err := os.MkdirAll(filepathDir(file.Path), 0755); err != nil {
			return nil, fmt.Errorf("create directory for %s: %w", file.Path, err)
		}
		changed, err := writeFileIfChanged(file.Path, file.Data, file.Perm)
		if err != nil {
			return nil, fmt.Errorf("write PPPoE file %s: %w", file.Path, err)
		}
		if changed {
			changedFiles = append(changedFiles, file.Path)
		}
	}
	if len(config.Secrets) > 0 {
		for _, path := range []string{pppoeCHAPSecretsPath, pppoePAPSecretsPath} {
			changed, err := updatePPPoESecrets(path, config.Secrets)
			if err != nil {
				return nil, err
			}
			if changed {
				changedFiles = append(changedFiles, path)
			}
		}
	}

	if containsSystemdUnit(changedFiles) {
		if err := runLogged("systemctl", "daemon-reload"); err != nil {
			return nil, err
		}
	}
	for _, unit := range config.Units {
		if len(changedFiles) > 0 {
			if err := runLogged("systemctl", "enable", unit); err != nil {
				return nil, err
			}
			if err := runLogged("systemctl", "restart", unit); err != nil {
				return nil, err
			}
			continue
		}
		if err := runLogged("systemctl", "is-active", "--quiet", unit); err != nil {
			if err := runLogged("systemctl", "enable", "--now", unit); err != nil {
				return nil, err
			}
		}
	}
	return changedFiles, nil
}

func pppdAvailable() bool {
	if _, err := exec.LookPath("pppd"); err == nil {
		return true
	}
	if st, err := os.Stat("/usr/sbin/pppd"); err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
		return true
	}
	return false
}

func pppoePassword(res api.Resource, spec api.PPPoEInterfaceSpec) (string, error) {
	if spec.Password != "" {
		return spec.Password, nil
	}
	data, err := os.ReadFile(spec.PasswordFile)
	if err != nil {
		return "", fmt.Errorf("%s read passwordFile %s: %w", res.ID(), spec.PasswordFile, err)
	}
	password := strings.TrimRight(string(data), "\r\n")
	if password == "" {
		return "", fmt.Errorf("%s passwordFile %s is empty", res.ID(), spec.PasswordFile)
	}
	return password, nil
}

func updatePPPoESecrets(path string, entries []render.PPPoESecretEntry) (bool, error) {
	if err := os.MkdirAll(filepathDir(path), 0755); err != nil {
		return false, fmt.Errorf("create directory for %s: %w", path, err)
	}
	current, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read PPP secrets %s: %w", path, err)
	}
	desired := replaceManagedPPPoEBlocks(string(current), entries)
	return writeFileIfChanged(path, []byte(desired), 0600)
}

func replaceManagedPPPoEBlocks(current string, entries []render.PPPoESecretEntry) string {
	lines := strings.Split(current, "\n")
	var kept []string
	skip := false
	for _, line := range lines {
		if strings.HasPrefix(line, "# BEGIN routerd pppoe ") {
			skip = true
			continue
		}
		if strings.HasPrefix(line, "# END routerd pppoe ") {
			skip = false
			continue
		}
		if !skip {
			kept = append(kept, line)
		}
	}
	text := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	var buf bytes.Buffer
	if text != "" {
		buf.WriteString(text)
		buf.WriteString("\n")
	}
	for _, entry := range entries {
		buf.WriteString("# BEGIN routerd pppoe " + entry.Name + "\n")
		buf.WriteString(render.PPPoESecretLine(entry))
		buf.WriteString("# END routerd pppoe " + entry.Name + "\n")
	}
	return buf.String()
}

func containsSystemdUnit(paths []string) bool {
	for _, path := range paths {
		if strings.HasPrefix(path, "/etc/systemd/system/") && strings.HasSuffix(path, ".service") {
			return true
		}
	}
	return false
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
