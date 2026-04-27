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
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/config"
	"routerd/pkg/controlapi"
	"routerd/pkg/eventlog"
	"routerd/pkg/observe"
	"routerd/pkg/platform"
	"routerd/pkg/reconcile"
	"routerd/pkg/render"
	"routerd/pkg/resource"
	routerstate "routerd/pkg/state"
	statuswriter "routerd/pkg/status"
)

const (
	routerdDnsmasqService = "routerd-dnsmasq.service"
)

var (
	platformDefaults, platformFeatures = platform.Current()

	defaultConfigPath          = platformDefaults.ConfigFile()
	defaultPluginDir           = platformDefaults.PluginDir
	defaultNetplanPath         = platformDefaults.NetplanFile
	defaultDnsmasqConfigPath   = platformDefaults.DnsmasqConfigFile
	defaultDnsmasqServicePath  = platformDefaults.DnsmasqServiceFile
	defaultFreeBSDDHClientPath = platformDefaults.FreeBSDDHClientConfigFile
	defaultFreeBSDDHCP6CPath   = platformDefaults.FreeBSDDHCP6CConfigFile
	defaultNftablesPath        = platformDefaults.NftablesFile
	defaultRouteNftablesPath   = platformDefaults.DefaultRouteNftablesFile
	defaultTimesyncdPath       = platformDefaults.TimesyncdDropinFile
	defaultLedgerPath          = platformDefaults.LedgerFile()
	defaultStatePath           = platformDefaults.StateDir + "/state.json"
	pppoeCHAPSecretsPath       = platformDefaults.PPPoEChapSecretsFile
	pppoePAPSecretsPath        = platformDefaults.PPPoEPapSecretsFile
)

var errNoIPv6PrefixAvailable = errors.New("no IPv6 prefix available")

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
	case "adopt":
		return adoptCommand(args[1:], stdout)
	case "render":
		return renderCommand(args[1:], stdout)
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

func renderCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("render requires a target: nixos or freebsd")
	}
	switch args[0] {
	case "nixos":
		return renderNixOSCommand(args[1:], stdout)
	case "freebsd":
		return renderFreeBSDCommand(args[1:], stdout)
	default:
		return fmt.Errorf("unknown render target %q", args[0])
	}
}

func renderFreeBSDCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("render freebsd", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	outDir := fs.String("out-dir", "", "output directory for FreeBSD generated files; writes rc.conf fragment to stdout when empty")
	if err := fs.Parse(args); err != nil {
		return err
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if err := config.Validate(router); err != nil {
		return err
	}
	data, err := render.FreeBSD(router)
	if err != nil {
		return err
	}
	if *outDir == "" {
		_, err := stdout.Write(data.RCConf)
		return err
	}
	if err := os.MkdirAll(*outDir, 0755); err != nil {
		return err
	}
	files := map[string][]byte{
		"rc.conf.d-routerd": data.RCConf,
	}
	if len(data.DHCP6C) > 0 {
		files["dhcp6c.conf"] = data.DHCP6C
	}
	if len(data.DHCPClient) > 0 {
		files["dhclient.conf"] = data.DHCPClient
	}
	for name, content := range files {
		path := strings.TrimRight(*outDir, "/") + "/" + name
		if err := os.WriteFile(path, content, 0644); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wrote %s\n", path)
	}
	return nil
}

func renderNixOSCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("render nixos", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	outPath := fs.String("out", "", "output path for routerd-generated.nix; writes to stdout when empty")
	if err := fs.Parse(args); err != nil {
		return err
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if err := config.Validate(router); err != nil {
		return err
	}
	data, err := render.NixOSModule(router)
	if err != nil {
		return err
	}
	if *outPath == "" {
		_, err := stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepathDir(*outPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(*outPath, data, 0644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s\n", *outPath)
	return nil
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
	stateStore, err := routerstate.Load(defaultStatePath)
	if err != nil {
		return err
	}
	stateChanges, err := evaluateStatePolicies(router, stateStore)
	if err != nil {
		return err
	}
	effectiveRouter := filterRouterByWhen(router, stateStore)
	switch name {
	case "observe":
		result, err := engine.Observe(effectiveRouter)
		if err != nil {
			return err
		}
		appendStatePolicyResults(result, router, stateStore, stateChanges)
		return writeResult(stdout, *statusFile, result)
	case "plan":
		result, err := engine.Plan(effectiveRouter)
		if err != nil {
			return err
		}
		appendStatePolicyResults(result, router, stateStore, stateChanges)
		return writeResult(stdout, *statusFile, result)
	case "run":
		return errors.New("run is not implemented yet")
	default:
		return fmt.Errorf("unknown config command %s", name)
	}
}

func adoptCommand(args []string, stdout io.Writer) (err error) {
	fs := flag.NewFlagSet("adopt", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath, "config path")
	statusFile := fs.String("status-file", defaultStatusFile(), "status file")
	ledgerPath := fs.String("ledger-file", defaultLedgerPath, "routerd ownership ledger file")
	candidatesOnly := fs.Bool("candidates", false, "list adoption candidates without changing host state or the ownership ledger")
	apply := fs.Bool("apply", false, "record adoption candidates in the ownership ledger without changing host state")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *candidatesOnly == *apply {
		return errors.New("adopt requires exactly one of --candidates or --apply")
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	logger, err := eventlog.New(router)
	if err != nil {
		return err
	}
	defer closeLogger(logger, "adopt", &err)
	logger.Emit(eventlog.LevelInfo, "adopt", "routerd command started", map[string]string{"config": *configPath})
	ledger, err := resource.LoadLedger(*ledgerPath)
	if err != nil {
		return err
	}
	engine := reconcile.New()
	candidates, artifacts, err := engine.AdoptionCandidateArtifacts(router, ledger)
	if err != nil {
		return err
	}
	result := &reconcile.Result{
		Generation:         time.Now().Unix(),
		Timestamp:          time.Now().UTC(),
		Phase:              "Healthy",
		AdoptionCandidates: candidates,
	}
	if *apply {
		if drifted := driftedAdoptionCandidates(candidates); len(drifted) > 0 {
			result.Phase = "Blocked"
			result.Warnings = append(result.Warnings, fmt.Sprintf("%d adoption candidates have observed attributes that differ from desired state; reconcile or update config before adopting", len(drifted)))
			if err := writeResult(stdout, *statusFile, result); err != nil {
				return err
			}
			return errors.New("adoption blocked by drifted candidates")
		}
		ledger.Remember(artifacts)
		if err := ledger.Save(*ledgerPath); err != nil {
			return err
		}
		result.AdoptedArtifacts = adoptedArtifactsForResult(artifacts)
		result.AdoptionCandidates = nil
	}
	return writeResult(stdout, *statusFile, result)
}

func driftedAdoptionCandidates(candidates []reconcile.AdoptionCandidate) []reconcile.AdoptionCandidate {
	var drifted []reconcile.AdoptionCandidate
	for _, candidate := range candidates {
		for key, desiredValue := range candidate.Desired {
			if candidate.Observed[key] != desiredValue {
				drifted = append(drifted, candidate)
				break
			}
		}
	}
	return drifted
}

func adoptedArtifactsForResult(artifacts []resource.Artifact) []reconcile.AdoptedArtifact {
	out := make([]reconcile.AdoptedArtifact, 0, len(artifacts))
	seen := map[string]bool{}
	for _, artifact := range artifacts {
		if seen[artifact.Identity()] {
			continue
		}
		seen[artifact.Identity()] = true
		out = append(out, reconcile.AdoptedArtifact{
			Kind:  artifact.Kind,
			Name:  artifact.Name,
			Owner: artifact.Owner,
		})
	}
	return out
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
	ledgerPath := fs.String("ledger-file", defaultLedgerPath, "routerd ownership ledger file")
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
		DnsmasqServicePath:  runtimeDnsmasqServicePath(*dnsmasqServicePath),
		NftablesPath:        *nftablesPath,
		LedgerPath:          *ledgerPath,
		StatePath:           defaultStatePath,
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
	LedgerPath          string
	StatePath           string
	DryRun              bool
	AnnounceDryRunToCLI bool
}

func effectiveReconcilePolicy(router *api.Router) api.ReconcilePolicySpec {
	policy := router.Spec.Reconcile
	if policy.Mode == "" {
		policy.Mode = "strict"
	}
	policy.ProtectedInterfaces = compactStringList(policy.ProtectedInterfaces)
	policy.ProtectedZones = compactStringList(policy.ProtectedZones)
	return policy
}

func compactStringList(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func runReconcileOnce(router *api.Router, opts reconcileApplyOptions, stdout io.Writer, logger *eventlog.Logger) (*reconcile.Result, error) {
	stateStore, err := routerstate.Load(defaultString(opts.StatePath, defaultStatePath))
	if err != nil {
		return nil, err
	}
	stateChanges, err := recordObservedPrefixDelegationState(router, stateStore)
	if err != nil {
		return nil, err
	}
	policyChanges, err := evaluateStatePolicies(router, stateStore)
	if err != nil {
		return nil, err
	}
	stateChanges = append(stateChanges, policyChanges...)
	effectiveRouter := filterRouterByWhen(router, stateStore)
	engine := reconcile.New()
	result, err := engine.Plan(effectiveRouter)
	if err != nil {
		return nil, err
	}
	appendStatePolicyResults(result, router, stateStore, stateChanges)
	if err := appendLedgerOwnedOrphans(result, effectiveRouter, opts.LedgerPath); err != nil {
		return nil, err
	}
	if !opts.DryRun {
		if err := os.MkdirAll(filepathDir(defaultString(opts.StatePath, defaultStatePath)), 0755); err != nil {
			return nil, err
		}
		if err := stateStore.Save(defaultString(opts.StatePath, defaultStatePath)); err != nil {
			return nil, err
		}
		logger.Emit(eventlog.LevelInfo, "reconcile", "routerd plan completed", map[string]string{
			"phase":     result.Phase,
			"resources": fmt.Sprintf("%d", len(result.Resources)),
		})
		if platformDefaults.OS == platform.OSFreeBSD {
			return runFreeBSDReconcileOnce(effectiveRouter, opts, stdout, logger, engine, result)
		}
		policy := effectiveReconcilePolicy(effectiveRouter)
		protectedCritical := len(policy.ProtectedInterfaces) > 0 || len(policy.ProtectedZones) > 0
		var applyErrors []string
		recordStageError := func(stage string, err error) error {
			if err == nil {
				return nil
			}
			msg := fmt.Sprintf("%s: %v", stage, err)
			result.Warnings = append(result.Warnings, msg)
			applyErrors = append(applyErrors, msg)
			logger.Emit(eventlog.LevelError, "reconcile", "routerd apply stage failed", map[string]string{"stage": stage, "error": err.Error()})
			if policy.Mode != "progressive" {
				return fmt.Errorf("%s: %w", stage, err)
			}
			return nil
		}

		var networkChangedFiles []string
		if err := recordStageError("network", func() error {
			netplanData, err := render.Netplan(effectiveRouter)
			if err != nil {
				return err
			}
			if isNixOSHost() {
				netplanData = nil
			}
			networkdFiles, err := render.NetworkdDropins(effectiveRouter)
			if err != nil {
				return err
			}
			networkChangedFiles, err = applyNetworkConfig(opts.NetplanPath, netplanData, networkdFiles)
			return err
		}()); err != nil {
			return nil, err
		}

		var nftablesChangedFiles []string
		if err := recordStageError("nftables", func() error {
			nftablesConfig, err := render.NftablesIPv4SourceNAT(effectiveRouter)
			if err != nil {
				return err
			}
			nftablesChangedFiles, err = applyNftablesConfig(opts.NftablesPath, nftablesConfig)
			return err
		}()); err != nil {
			return nil, err
		}

		var appliedIPv6DelegatedAddresses []string
		if err := recordStageError("ipv6-delegated-address", func() error {
			var err error
			appliedIPv6DelegatedAddresses, err = applyIPv6DelegatedAddresses(effectiveRouter)
			return err
		}()); err != nil {
			return nil, err
		}

		var dnsmasqChangedFiles []string
		if err := recordStageError("dnsmasq", func() error {
			dnsmasqConfig, err := render.DnsmasqConfig(effectiveRouter, render.DnsmasqRuntime{
				DHCPv4DNSServersByInterface: observedDNSServersByInterface(effectiveRouter),
				DHCPv6DNSServersByInterface: observedDNSServersByInterface(effectiveRouter),
				IPv6AddressesByInterface:    observedIPv6AddressesByInterface(effectiveRouter),
				IPv6PrefixesByInterface:     observedIPv6PrefixesByInterface(effectiveRouter),
			})
			if err != nil {
				return err
			}
			dnsmasqChangedFiles, err = applyDnsmasqConfig(opts.DnsmasqConfigPath, opts.DnsmasqServicePath, dnsmasqConfig)
			return err
		}()); err != nil {
			return nil, err
		}

		var pppoeChangedFiles []string
		if err := recordStageError("pppoe", func() error {
			var err error
			pppoeChangedFiles, err = applyPPPoEConfig(effectiveRouter)
			return err
		}()); err != nil {
			return nil, err
		}

		var timesyncdChangedFiles []string
		if err := recordStageError("timesyncd", func() error {
			timesyncdConfig, err := render.TimesyncdConfig(effectiveRouter)
			if err != nil {
				return err
			}
			if isNixOSHost() {
				timesyncdConfig = nil
			}
			timesyncdChangedFiles, err = applyTimesyncdConfig(defaultTimesyncdPath, timesyncdConfig)
			return err
		}()); err != nil {
			return nil, err
		}

		var cleanedPreDSLiteOrphans []string
		if len(applyErrors) == 0 {
			var err error
			cleanedPreDSLiteOrphans, err = cleanupLedgerOwnedOrphansMatching(effectiveRouter, opts.LedgerPath, func(artifact resource.Artifact) bool {
				return artifact.Kind == "linux.ipip6.tunnel"
			})
			if err != nil {
				return nil, err
			}
		}

		var appliedTunnels []string
		if err := recordStageError("ds-lite", func() error {
			var err error
			appliedTunnels, err = applyDSLiteTunnels(effectiveRouter)
			return err
		}()); err != nil {
			return nil, err
		}

		var appliedPolicyRoutes []string
		if err := recordStageError("ipv4-policy-routes", func() error {
			var err error
			appliedPolicyRoutes, err = applyIPv4PolicyRoutes(effectiveRouter)
			return err
		}()); err != nil {
			return nil, err
		}

		var appliedDefaultRoutes []string
		if err := recordStageError("ipv4-default-route-policy", func() error {
			var err error
			appliedDefaultRoutes, err = applyIPv4DefaultRoutePolicies(effectiveRouter)
			return err
		}()); err != nil {
			return nil, err
		}

		var cleanedPolicyRules []string
		if len(applyErrors) == 0 || !protectedCritical {
			if err := recordStageError("ipv4-policy-rule-cleanup", func() error {
				var err error
				cleanedPolicyRules, err = cleanupIPv4ManagedFwmarkRules(effectiveRouter)
				return err
			}()); err != nil {
				return nil, err
			}
		} else {
			result.Warnings = append(result.Warnings, "skipped policy-rule cleanup because an earlier progressive apply stage failed")
		}

		var appliedRuntime []string
		if err := recordStageError("sysctl", func() error {
			var err error
			appliedRuntime, err = applyRuntimeSysctls(effectiveRouter)
			return err
		}()); err != nil {
			return nil, err
		}

		var appliedReversePathFilters []string
		if err := recordStageError("rp-filter", func() error {
			var err error
			appliedReversePathFilters, err = applyIPv4ReversePathFilters(effectiveRouter)
			return err
		}()); err != nil {
			return nil, err
		}

		var appliedHostnames []string
		if err := recordStageError("hostname", func() error {
			var err error
			appliedHostnames, err = applyHostnames(effectiveRouter)
			return err
		}()); err != nil {
			return nil, err
		}

		var cleanedLedgerOrphans []string
		var rememberedArtifacts int
		if len(applyErrors) == 0 {
			var err error
			cleanedLedgerOrphans, err = cleanupLedgerOwnedOrphans(effectiveRouter, opts.LedgerPath)
			if err != nil {
				return nil, err
			}
			rememberedArtifacts, err = rememberReconciledArtifacts(effectiveRouter, opts.LedgerPath)
			if err != nil {
				return nil, err
			}
		} else {
			result.Warnings = append(result.Warnings, "skipped ledger orphan cleanup and ownership recording because reconcile completed with stage errors")
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
		for _, hostname := range appliedHostnames {
			fmt.Fprintf(stdout, "applied hostname %s\n", hostname)
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
		for _, rule := range cleanedPolicyRules {
			fmt.Fprintf(stdout, "removed stale IPv4 policy rule %s\n", rule)
		}
		for _, artifact := range cleanedPreDSLiteOrphans {
			fmt.Fprintf(stdout, "removed orphaned owned artifact %s\n", artifact)
		}
		for _, artifact := range cleanedLedgerOrphans {
			fmt.Fprintf(stdout, "removed orphaned owned artifact %s\n", artifact)
		}
		if rememberedArtifacts > 0 {
			fmt.Fprintf(stdout, "remembered %d owned artifacts\n", rememberedArtifacts)
		}
		logger.Emit(eventlog.LevelInfo, "reconcile", "routerd changes applied", map[string]string{
			"changedFiles":        fmt.Sprintf("%d", len(changedFiles)),
			"runtimeSysctls":      fmt.Sprintf("%d", len(appliedRuntime)),
			"reversePathFilters":  fmt.Sprintf("%d", len(appliedReversePathFilters)),
			"hostnames":           fmt.Sprintf("%d", len(appliedHostnames)),
			"ipv6DelegatedAddrs":  fmt.Sprintf("%d", len(appliedIPv6DelegatedAddresses)),
			"pppoeFiles":          fmt.Sprintf("%d", len(pppoeChangedFiles)),
			"ntpFiles":            fmt.Sprintf("%d", len(timesyncdChangedFiles)),
			"dsliteTunnels":       fmt.Sprintf("%d", len(appliedTunnels)),
			"ipv4DefaultRoutes":   fmt.Sprintf("%d", len(appliedDefaultRoutes)),
			"ipv4PolicyRouteSets": fmt.Sprintf("%d", len(appliedPolicyRoutes)),
			"ipv4PolicyRulesGone": fmt.Sprintf("%d", len(cleanedPolicyRules)),
			"ownedOrphansGone":    fmt.Sprintf("%d", len(cleanedPreDSLiteOrphans)+len(cleanedLedgerOrphans)),
			"rememberedArtifacts": fmt.Sprintf("%d", rememberedArtifacts),
		})
		applyWarnings := append([]string{}, result.Warnings...)
		result, err = engine.Plan(effectiveRouter)
		if err != nil {
			return nil, err
		}
		result.Warnings = append(result.Warnings, applyWarnings...)
		if len(applyErrors) > 0 {
			result.Phase = "Degraded"
		}
		if err := appendLedgerOwnedOrphans(result, effectiveRouter, opts.LedgerPath); err != nil {
			return nil, err
		}
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

func runFreeBSDReconcileOnce(router *api.Router, opts reconcileApplyOptions, stdout io.Writer, logger *eventlog.Logger, engine *reconcile.Engine, result *reconcile.Result) (*reconcile.Result, error) {
	policy := effectiveReconcilePolicy(router)
	var applyErrors []string
	recordStageError := func(stage string, err error) error {
		if err == nil {
			return nil
		}
		msg := fmt.Sprintf("%s: %v", stage, err)
		result.Warnings = append(result.Warnings, msg)
		applyErrors = append(applyErrors, msg)
		logger.Emit(eventlog.LevelError, "reconcile", "routerd FreeBSD apply stage failed", map[string]string{"stage": stage, "error": err.Error()})
		if policy.Mode != "progressive" {
			return fmt.Errorf("%s: %w", stage, err)
		}
		return nil
	}

	var changedFreeBSD []string
	if err := recordStageError("freebsd-network", func() error {
		var err error
		changedFreeBSD, err = applyFreeBSDConfig(router, defaultFreeBSDDHClientPath, defaultFreeBSDDHCP6CPath)
		return err
	}()); err != nil {
		return nil, err
	}
	var appliedRuntime []string
	if err := recordStageError("sysctl", func() error {
		var err error
		appliedRuntime, err = applyRuntimeSysctls(router)
		return err
	}()); err != nil {
		return nil, err
	}
	var appliedHostnames []string
	if err := recordStageError("hostname", func() error {
		var err error
		appliedHostnames, err = applyHostnames(router)
		return err
	}()); err != nil {
		return nil, err
	}

	for _, item := range changedFreeBSD {
		fmt.Fprintf(stdout, "applied FreeBSD network configuration %s\n", item)
	}
	for _, key := range appliedRuntime {
		fmt.Fprintf(stdout, "applied sysctl %s\n", key)
	}
	for _, hostname := range appliedHostnames {
		fmt.Fprintf(stdout, "applied hostname %s\n", hostname)
	}
	if len(changedFreeBSD) == 0 && len(appliedRuntime) == 0 && len(appliedHostnames) == 0 {
		fmt.Fprintln(stdout, "FreeBSD configuration already up to date")
	}

	var rememberedArtifacts int
	if len(applyErrors) == 0 {
		var err error
		rememberedArtifacts, err = rememberReconciledArtifacts(router, opts.LedgerPath)
		if err != nil {
			return nil, err
		}
	} else {
		result.Warnings = append(result.Warnings, "skipped ownership recording because FreeBSD reconcile completed with stage errors")
	}
	if rememberedArtifacts > 0 {
		fmt.Fprintf(stdout, "remembered %d owned artifacts\n", rememberedArtifacts)
	}

	applyWarnings := append([]string{}, result.Warnings...)
	next, err := engine.Plan(router)
	if err != nil {
		return nil, err
	}
	next.Warnings = append(next.Warnings, applyWarnings...)
	if len(applyErrors) > 0 {
		next.Phase = "Degraded"
	}
	if err := appendLedgerOwnedOrphans(next, router, opts.LedgerPath); err != nil {
		return nil, err
	}
	if err := writeResult(stdout, opts.StatusFile, next); err != nil {
		return nil, err
	}
	logger.Emit(eventlog.LevelInfo, "reconcile", "routerd FreeBSD changes applied", map[string]string{
		"freebsdChanges":      fmt.Sprintf("%d", len(changedFreeBSD)),
		"runtimeSysctls":      fmt.Sprintf("%d", len(appliedRuntime)),
		"hostnames":           fmt.Sprintf("%d", len(appliedHostnames)),
		"rememberedArtifacts": fmt.Sprintf("%d", rememberedArtifacts),
	})
	return next, nil
}

type stateChange struct {
	Name  string
	Value routerstate.Value
}

func evaluateStatePolicies(router *api.Router, store *routerstate.Store) ([]stateChange, error) {
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
	var changes []stateChange
	for _, res := range router.Spec.Resources {
		if res.Kind != "StatePolicy" {
			continue
		}
		spec, err := res.StatePolicySpec()
		if err != nil {
			return nil, err
		}
		applied := false
		for _, value := range spec.Values {
			ok, err := evaluateStateConditions(router, aliases, store, spec, value)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", res.ID(), err)
			}
			if ok {
				changes = append(changes, stateChange{Name: spec.Variable, Value: store.Set(spec.Variable, value.Value, res.ID())})
				applied = true
				break
			}
		}
		if !applied {
			changes = append(changes, stateChange{Name: spec.Variable, Value: store.Unset(spec.Variable, res.ID()+": no value matched")})
		}
	}
	return changes, nil
}

func recordObservedPrefixDelegationState(router *api.Router, store *routerstate.Store) ([]stateChange, error) {
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
	delegatedByPD := map[string][]api.Resource{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv6DelegatedAddress" {
			continue
		}
		spec, err := res.IPv6DelegatedAddressSpec()
		if err != nil {
			return nil, err
		}
		delegatedByPD[spec.PrefixDelegation] = append(delegatedByPD[spec.PrefixDelegation], res)
	}

	var changes []stateChange
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv6PrefixDelegation" {
			continue
		}
		spec, err := res.IPv6PrefixDelegationSpec()
		if err != nil {
			return nil, err
		}
		prefixLength := stateEffectiveIPv6PDPrefixLength(defaultString(spec.Profile, "default"), spec.PrefixLength)
		base := "ipv6PrefixDelegation." + res.Metadata.Name
		if ifname := aliases[spec.Interface]; ifname != "" {
			changes = append(changes, stateChange{Name: base + ".uplinkIfname", Value: store.Set(base+".uplinkIfname", ifname, res.ID()+": observed uplink interface")})
			for _, change := range observedPrefixDelegationIdentityState(base, ifname, defaultString(spec.Client, "networkd"), defaultString(spec.Profile, "default"), store, res.ID()) {
				changes = append(changes, change)
			}
		}
		changes = append(changes, stateChange{Name: base + ".client", Value: store.Set(base+".client", defaultString(spec.Client, "networkd"), res.ID()+": configured DHCPv6-PD client")})
		changes = append(changes, stateChange{Name: base + ".profile", Value: store.Set(base+".profile", defaultString(spec.Profile, "default"), res.ID()+": configured DHCPv6-PD profile")})
		if prefixLength > 0 {
			changes = append(changes, stateChange{Name: base + ".prefixLength", Value: store.Set(base+".prefixLength", strconv.Itoa(prefixLength), res.ID()+": configured prefix length")})
		}

		var observedPrefix, observedIfname string
		for _, delegated := range delegatedByPD[res.Metadata.Name] {
			delegatedSpec, err := delegated.IPv6DelegatedAddressSpec()
			if err != nil {
				return nil, err
			}
			ifname := aliases[delegatedSpec.Interface]
			if ifname == "" {
				continue
			}
			prefix, ok := delegatedPrefixFromObservedInterface(ifname, prefixLength)
			if ok {
				observedPrefix = prefix
				observedIfname = ifname
				break
			}
		}
		if observedPrefix == "" {
			changes = append(changes, stateChange{Name: base + ".currentPrefix", Value: store.Unset(base+".currentPrefix", res.ID()+": no delegated prefix observable")})
			continue
		}
		changes = append(changes,
			stateChange{Name: base + ".currentPrefix", Value: store.Set(base+".currentPrefix", observedPrefix, res.ID()+": observed delegated prefix")},
			stateChange{Name: base + ".lastPrefix", Value: store.Set(base+".lastPrefix", observedPrefix, res.ID()+": observed delegated prefix")},
			stateChange{Name: base + ".downstreamIfname", Value: store.Set(base+".downstreamIfname", observedIfname, res.ID()+": observed delegated prefix")},
		)
	}
	return changes, nil
}

func observedPrefixDelegationIdentityState(base, ifname, client, profile string, store *routerstate.Store, owner string) []stateChange {
	var changes []stateChange
	if client != "networkd" {
		return changes
	}
	identity := observeNetworkdDHCPIdentity(ifname)
	if identity.IAID != "" {
		changes = append(changes, stateChange{Name: base + ".iaid", Value: store.Set(base+".iaid", identity.IAID, owner+": observed DHCP IAID")})
	}
	if identity.DUID != "" {
		changes = append(changes, stateChange{Name: base + ".duid", Value: store.Set(base+".duid", identity.DUID, owner+": observed DHCP DUID")})
	}
	if identity.DUIDText != "" {
		changes = append(changes, stateChange{Name: base + ".duidText", Value: store.Set(base+".duidText", identity.DUIDText, owner+": observed DHCPv6 DUID")})
	}
	if identity.Source != "" {
		changes = append(changes, stateChange{Name: base + ".identitySource", Value: store.Set(base+".identitySource", identity.Source, owner+": observed DHCP identity source")})
	}
	if expected := expectedPrefixDelegationDUID(ifname, profile); expected != "" {
		changes = append(changes, stateChange{Name: base + ".expectedDUID", Value: store.Set(base+".expectedDUID", expected, owner+": expected DHCPv6 DUID for profile")})
	}
	return changes
}

type dhcpIdentity struct {
	IAID     string
	DUID     string
	DUIDText string
	Source   string
}

func observeNetworkdDHCPIdentity(ifname string) dhcpIdentity {
	ifindex := strings.TrimSpace(readFirstString(filepath.Join("/sys/class/net", ifname, "ifindex")))
	if ifindex == "" {
		return dhcpIdentity{}
	}
	leaseValues := parseKeyValueFile(filepath.Join("/run/systemd/netif/leases", ifindex))
	identity := parseRFC4361ClientID(leaseValues["CLIENTID"])
	if identity.Source != "" {
		identity.Source = "systemd-networkd-lease"
	}
	linkValues := parseKeyValueFile(filepath.Join("/run/systemd/netif/links", ifindex))
	if value := strings.Trim(linkValues["DHCP6_CLIENT_DUID"], `"`); value != "" {
		identity.DUIDText = value
		if identity.Source == "" {
			identity.Source = "systemd-networkd-link"
		}
	}
	return identity
}

func parseRFC4361ClientID(value string) dhcpIdentity {
	value = strings.ToLower(strings.TrimSpace(strings.Trim(value, `"`)))
	if len(value) < 12 || !strings.HasPrefix(value, "ff") {
		return dhcpIdentity{}
	}
	iaid := value[2:10]
	duid := value[10:]
	if !isLowerHex(iaid) || !isLowerHex(duid) {
		return dhcpIdentity{}
	}
	return dhcpIdentity{IAID: iaid, DUID: duid, Source: "rfc4361-clientid"}
}

func expectedPrefixDelegationDUID(ifname, profile string) string {
	switch profile {
	case "ntt-ngn-direct-hikari-denwa", "ntt-hgw-lan-pd":
	default:
		return ""
	}
	mac := strings.TrimSpace(readFirstString(filepath.Join("/sys/class/net", ifname, "address")))
	return linkLayerDUIDFromMAC(mac)
}

func linkLayerDUIDFromMAC(mac string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(mac)), ":")
	if len(parts) != 6 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("00030001")
	for _, part := range parts {
		if len(part) != 2 || !isLowerHex(part) {
			return ""
		}
		builder.WriteString(part)
	}
	return builder.String()
}

func isLowerHex(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func readFirstString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func parseKeyValueFile(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values
}

func delegatedPrefixFromObservedInterface(ifname string, prefixLength int) (string, bool) {
	return delegatedPrefixFromObserved(ipv6Prefixes(ifname), ipv6Addresses(ifname), prefixLength)
}

func delegatedPrefixFromObserved(prefixes, addresses []string, prefixLength int) (string, bool) {
	for _, value := range prefixes {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !prefix.Addr().Is6() || prefix.Addr().IsLinkLocalUnicast() {
			continue
		}
		if prefixLength > 0 && prefixLength <= prefix.Bits() {
			prefix = netip.PrefixFrom(prefix.Addr(), prefixLength)
		}
		return prefix.Masked().String(), true
	}
	for _, value := range addresses {
		addr, err := netip.ParseAddr(value)
		if err != nil || !addr.Is6() || addr.IsLinkLocalUnicast() {
			continue
		}
		bits := prefixLength
		if bits <= 0 || bits > 128 {
			bits = 64
		}
		return netip.PrefixFrom(addr, bits).Masked().String(), true
	}
	return "", false
}

func stateEffectiveIPv6PDPrefixLength(profile string, configured int) int {
	if configured != 0 {
		return configured
	}
	if profile == "ntt-ngn-direct-hikari-denwa" || profile == "ntt-hgw-lan-pd" {
		return 60
	}
	return 0
}

func evaluateStateConditions(router *api.Router, aliases map[string]string, store *routerstate.Store, policy api.StatePolicySpec, value api.StateValueSpec) (bool, error) {
	if value.When.IPv6PrefixDelegation.Resource != "" || value.When.IPv6PrefixDelegation.Available != nil {
		ok, known, err := stateIPv6PrefixDelegationAvailable(router, aliases, value.When.IPv6PrefixDelegation)
		predicateName := policy.Variable + "." + value.Value + ".ipv6PrefixDelegation"
		if err != nil || !known {
			store.Forget(predicateName, "ipv6 prefix delegation unknown")
			return false, err
		}
		if ok {
			store.Set(predicateName, "available", "ipv6 prefix delegation available")
		} else {
			store.Unset(predicateName, "ipv6 prefix delegation unavailable")
		}
		if value.When.IPv6PrefixDelegation.Available != nil && ok != *value.When.IPv6PrefixDelegation.Available {
			return false, nil
		}
		if value.When.IPv6PrefixDelegation.UnavailableFor != "" {
			duration, err := time.ParseDuration(value.When.IPv6PrefixDelegation.UnavailableFor)
			if err != nil {
				return false, err
			}
			if store.Get(predicateName).Status != routerstate.StatusUnset || store.Age(predicateName) < duration {
				return false, nil
			}
		}
	}
	if value.When.IPv6Address.Global != nil || value.When.IPv6Address.Interface != "" {
		ifname := aliases[defaultString(value.When.IPv6Address.Interface, policy.Interface)]
		hasGlobal := firstGlobalIPv6(ipv6Addresses(ifname)) != ""
		if value.When.IPv6Address.Global != nil && hasGlobal != *value.When.IPv6Address.Global {
			return false, nil
		}
	}
	if value.When.DNSResolve.Name != "" {
		addrs, err := resolveStateDNS(value.When.DNSResolve, aliases)
		if err != nil || len(addrs) == 0 {
			return false, nil
		}
	}
	return true, nil
}

func stateIPv6PrefixDelegationAvailable(router *api.Router, aliases map[string]string, cond api.StateIPv6PrefixDelegationCondition) (bool, bool, error) {
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv6DelegatedAddress" {
			continue
		}
		spec, err := res.IPv6DelegatedAddressSpec()
		if err != nil {
			return false, false, err
		}
		if cond.Resource != "" && spec.PrefixDelegation != cond.Resource {
			continue
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			continue
		}
		if _, err := deriveIPv6AddressFromInterface(ifname, spec.AddressSuffix); err == nil {
			return true, true, nil
		}
	}
	return false, true, nil
}

func resolveStateDNS(spec api.StateDNSResolveCondition, aliases map[string]string) ([]string, error) {
	if defaultString(spec.Type, "AAAA") != "AAAA" {
		return nil, fmt.Errorf("unsupported DNS resolve type %q", spec.Type)
	}
	servers := spec.UpstreamServers
	if len(servers) == 0 || defaultString(spec.UpstreamSource, "system") == "system" {
		return net.LookupHost(spec.Name)
	}
	var out []string
	for _, server := range servers {
		addrs, err := resolveAAAAWithServers(spec.Name, []string{server}, 0, "")
		if err == nil && addrs != "" {
			out = append(out, addrs)
		}
	}
	return out, nil
}

func filterRouterByWhen(router *api.Router, store *routerstate.Store) *api.Router {
	filtered := *router
	filtered.Spec.Resources = nil
	for _, res := range router.Spec.Resources {
		if res.Kind == "StatePolicy" {
			continue
		}
		when := resourceWhen(res)
		if resourceWhenMatches(when, store) {
			if res.Kind == "IPv4DefaultRoutePolicy" {
				res = filterDefaultRoutePolicyCandidatesByWhen(res, store)
			}
			filtered.Spec.Resources = append(filtered.Spec.Resources, res)
		}
	}
	return &filtered
}

func filterDefaultRoutePolicyCandidatesByWhen(res api.Resource, store *routerstate.Store) api.Resource {
	spec, err := res.IPv4DefaultRoutePolicySpec()
	if err != nil {
		return res
	}
	var candidates []api.IPv4DefaultRoutePolicyCandidate
	for _, candidate := range spec.Candidates {
		if resourceWhenMatches(candidate.When, store) {
			candidates = append(candidates, candidate)
		}
	}
	spec.Candidates = candidates
	res.Spec = spec
	return res
}

func resourceWhen(res api.Resource) api.ResourceWhenSpec {
	switch res.Kind {
	case "IPv4DHCPScope":
		spec, _ := res.IPv4DHCPScopeSpec()
		return spec.When
	case "IPv6DelegatedAddress":
		spec, _ := res.IPv6DelegatedAddressSpec()
		return spec.When
	case "IPv6DHCPScope":
		spec, _ := res.IPv6DHCPScopeSpec()
		return spec.When
	case "DSLiteTunnel":
		spec, _ := res.DSLiteTunnelSpec()
		return spec.When
	case "HealthCheck":
		spec, _ := res.HealthCheckSpec()
		return spec.When
	case "IPv4SourceNAT":
		spec, _ := res.IPv4SourceNATSpec()
		return spec.When
	case "IPv4PolicyRouteSet":
		spec, _ := res.IPv4PolicyRouteSetSpec()
		return spec.When
	default:
		return api.ResourceWhenSpec{}
	}
}

func resourceWhenMatches(when api.ResourceWhenSpec, store *routerstate.Store) bool {
	if len(when.State) == 0 {
		return true
	}
	for name, match := range when.State {
		if !stateMatch(store, name, match) {
			return false
		}
	}
	return true
}

func stateMatch(store *routerstate.Store, name string, match api.StateMatchSpec) bool {
	value := store.Get(name)
	ok := true
	if match.Status != "" {
		ok = ok && value.Status == match.Status
	}
	if match.Exists != nil {
		if *match.Exists {
			ok = ok && value.Status == routerstate.StatusSet
		} else {
			ok = ok && value.Status == routerstate.StatusUnset
		}
	}
	if match.Equals != "" {
		ok = ok && value.Status == routerstate.StatusSet && value.Value == match.Equals
	}
	if len(match.In) > 0 {
		ok = ok && value.Status == routerstate.StatusSet && stringIn(value.Value, match.In)
	}
	if match.Contains != "" {
		ok = ok && value.Status == routerstate.StatusSet && strings.Contains(value.Value, match.Contains)
	}
	if !ok {
		return false
	}
	if match.For != "" {
		duration, err := time.ParseDuration(match.For)
		if err != nil || store.Age(name) < duration {
			return false
		}
	}
	return true
}

func appendStatePolicyResults(result *reconcile.Result, router *api.Router, store *routerstate.Store, changes []stateChange) {
	changed := map[string]routerstate.Value{}
	for _, change := range changes {
		changed[change.Name] = change.Value
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "StatePolicy" {
			continue
		}
		spec, err := res.StatePolicySpec()
		if err != nil {
			continue
		}
		value := store.Get(spec.Variable)
		if changedValue, ok := changed[spec.Variable]; ok {
			value = changedValue
		}
		result.Resources = append(result.Resources, reconcile.ResourceResult{
			ID:    res.ID(),
			Phase: "Healthy",
			Observed: map[string]string{
				"variable": spec.Variable,
				"status":   value.Status,
				"value":    value.Value,
				"since":    value.Since.Format(time.RFC3339),
			},
			Plan: []string{"evaluate state variable " + spec.Variable},
		})
	}
}

func stringIn(value string, values []string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func appendLedgerOwnedOrphans(result *reconcile.Result, router *api.Router, ledgerPath string) error {
	if ledgerPath == "" {
		return nil
	}
	ledger, err := resource.LoadLedger(ledgerPath)
	if err != nil {
		return err
	}
	engine := reconcile.New()
	orphans, _, err := engine.LedgerOwnedOrphans(router, ledger)
	if err != nil {
		return err
	}
	if len(orphans) == 0 {
		return nil
	}
	result.Orphans = appendUniqueOrphans(result.Orphans, orphans)
	result.Warnings = append(result.Warnings, fmt.Sprintf("%d ledger-owned orphaned artifacts found", len(orphans)))
	if result.Phase == "Healthy" {
		result.Phase = "Drifted"
	}
	return nil
}

func appendUniqueOrphans(existing, additions []reconcile.OrphanedArtifact) []reconcile.OrphanedArtifact {
	seen := map[string]int{}
	for i, orphan := range existing {
		seen[orphan.Name+"/"+orphan.Remediation] = i
	}
	for _, orphan := range additions {
		id := orphan.Name + "/" + orphan.Remediation
		if index, ok := seen[id]; ok {
			if existing[index].Owner == "" && orphan.Owner != "" {
				existing[index] = orphan
			}
			continue
		}
		seen[id] = len(existing)
		existing = append(existing, orphan)
	}
	return existing
}

func cleanupLedgerOwnedOrphans(router *api.Router, ledgerPath string) ([]string, error) {
	return cleanupLedgerOwnedOrphansMatching(router, ledgerPath, func(resource.Artifact) bool { return true })
}

func cleanupLedgerOwnedOrphansMatching(router *api.Router, ledgerPath string, match func(resource.Artifact) bool) ([]string, error) {
	if ledgerPath == "" {
		return nil, nil
	}
	ledger, err := resource.LoadLedger(ledgerPath)
	if err != nil {
		return nil, err
	}
	engine := reconcile.New()
	_, artifacts, err := engine.LedgerOwnedOrphans(router, ledger)
	if err != nil {
		return nil, err
	}
	var removed []string
	var removedArtifacts []resource.Artifact
	for _, artifact := range artifacts {
		if match != nil && !match(artifact) {
			continue
		}
		label, err := cleanupLedgerOwnedArtifact(artifact)
		if err != nil {
			return removed, err
		}
		if label == "" {
			continue
		}
		removed = append(removed, label)
		removedArtifacts = append(removedArtifacts, artifact)
	}
	if len(removedArtifacts) > 0 {
		ledger.Forget(removedArtifacts)
		if err := ledger.Save(ledgerPath); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func cleanupLedgerOwnedArtifact(artifact resource.Artifact) (string, error) {
	switch artifact.Kind {
	case "linux.ipip6.tunnel":
		if err := runLogged("ip", "-6", "tunnel", "del", artifact.Name); err != nil {
			return "", err
		}
		return artifact.Kind + "/" + artifact.Name, nil
	case "nft.table":
		family := artifact.Attributes["family"]
		name := artifact.Attributes["name"]
		if !strings.HasPrefix(name, "routerd_") {
			return "", nil
		}
		if err := runLogged("nft", "delete", "table", family, name); err != nil {
			return "", err
		}
		return artifact.Kind + "/" + name, nil
	case "systemd.service":
		if !strings.HasPrefix(artifact.Name, "routerd-") || !strings.HasSuffix(artifact.Name, ".service") {
			return "", nil
		}
		if err := runLogged("systemctl", "disable", "--now", artifact.Name); err != nil {
			return "", err
		}
		unitPath := "/etc/systemd/system/" + artifact.Name
		if err := os.Remove(unitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err := runLogged("systemctl", "daemon-reload"); err != nil {
			return "", err
		}
		return artifact.Kind + "/" + artifact.Name, nil
	default:
		return "", nil
	}
}

func rememberReconciledArtifacts(router *api.Router, ledgerPath string) (int, error) {
	if ledgerPath == "" {
		return 0, nil
	}
	engine := reconcile.New()
	artifacts, err := engine.ReconciledOwnedArtifacts(router)
	if err != nil {
		return 0, err
	}
	ledger, err := resource.LoadLedger(ledgerPath)
	if err != nil {
		return 0, err
	}
	ledger.Remember(artifacts)
	if err := ledger.Save(ledgerPath); err != nil {
		return 0, err
	}
	return len(adoptedArtifactsForResult(artifacts)), nil
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
	ledgerPath := fs.String("ledger-file", defaultLedgerPath, "routerd ownership ledger file")
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
		DnsmasqServicePath: runtimeDnsmasqServicePath(*dnsmasqServicePath),
		NftablesPath:       *nftablesPath,
		LedgerPath:         *ledgerPath,
		StatePath:          defaultStatePath,
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
		if target != "all" && target != "default" && !linkExists(target) {
			continue
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

func applyHostnames(router *api.Router) ([]string, error) {
	desired, err := managedHostnames(router)
	if err != nil {
		return nil, err
	}
	if len(desired) == 0 {
		return nil, nil
	}
	if len(desired) > 1 {
		return nil, fmt.Errorf("multiple managed Hostname resources are not supported: %s", strings.Join(desired, ","))
	}
	hostname := desired[0]
	currentOut, err := exec.Command("hostname").CombinedOutput()
	if err == nil && strings.TrimSpace(string(currentOut)) == hostname {
		return nil, nil
	}
	if err := runLogged("hostnamectl", "set-hostname", hostname); err != nil {
		if platformDefaults.OS == platform.OSFreeBSD {
			if err := runLogged("sysrc", "hostname="+hostname); err != nil {
				return nil, err
			}
			if fallbackErr := runLogged("hostname", hostname); fallbackErr != nil {
				return nil, fallbackErr
			}
			return []string{hostname}, nil
		}
		if !isNixOSHost() {
			return nil, err
		}
		if fallbackErr := runLogged("hostname", hostname); fallbackErr != nil {
			return nil, fmt.Errorf("%w; fallback hostname failed: %v", err, fallbackErr)
		}
	}
	return []string{hostname}, nil
}

func isNixOSHost() bool {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "ID=nixos" || line == `ID="nixos"` {
			return true
		}
	}
	return false
}

func runtimeDnsmasqServicePath(path string) string {
	if isNixOSHost() && path == defaultDnsmasqServicePath {
		return "/run/systemd/system/" + routerdDnsmasqService
	}
	return path
}

func managedHostnames(router *api.Router) ([]string, error) {
	var hostnames []string
	for _, res := range router.Spec.Resources {
		if res.Kind != "Hostname" {
			continue
		}
		spec, err := res.HostnameSpec()
		if err != nil {
			return nil, err
		}
		if !spec.Managed {
			continue
		}
		hostnames = append(hostnames, spec.Hostname)
	}
	return hostnames, nil
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
	if len(netplanData) == 0 {
		return changedNetworkdFiles, nil
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

func applyFreeBSDConfig(router *api.Router, dhclientPath, dhcp6cPath string) ([]string, error) {
	data, err := render.FreeBSD(router)
	if err != nil {
		return nil, err
	}
	rcValues, err := parseFreeBSDRCConf(data.RCConf)
	if err != nil {
		return nil, err
	}
	var changed []string
	var restartIfnames []string
	for _, key := range sortedStringMapKeys(rcValues) {
		value := rcValues[key]
		currentOut, err := exec.Command("sysrc", "-n", key).CombinedOutput()
		if err == nil && strings.TrimSpace(string(currentOut)) == value {
			continue
		}
		if err := runLogged("sysrc", key+"="+value); err != nil {
			return changed, err
		}
		changed = append(changed, "sysrc:"+key)
		if ifname := freeBSDIfconfigKeyInterface(key); ifname != "" {
			restartIfnames = append(restartIfnames, ifname)
		}
	}
	if len(data.DHCPClient) > 0 && dhclientPath != "" {
		fileChanged, err := writeFileIfChanged(dhclientPath, data.DHCPClient, 0644)
		if err != nil {
			return changed, err
		}
		if fileChanged {
			changed = append(changed, dhclientPath)
			restartIfnames = append(restartIfnames, freeBSDDHCPClientIfnames(data.DHCPClient)...)
		}
	}
	if len(data.DHCP6C) > 0 && dhcp6cPath != "" {
		fileChanged, err := writeFileIfChanged(dhcp6cPath, data.DHCP6C, 0644)
		if err != nil {
			return changed, err
		}
		if fileChanged {
			changed = append(changed, dhcp6cPath)
		}
		if (fileChanged || rcValues["dhcp6c_enable"] == "YES") && freeBSDServiceExists("dhcp6c") {
			if err := runLogged("service", "dhcp6c", "restart"); err != nil {
				return changed, err
			}
			changed = append(changed, "service:dhcp6c")
		}
	}
	for _, ifname := range compactStringList(restartIfnames) {
		if err := runLogged("service", "netif", "restart", ifname); err != nil {
			return changed, err
		}
		changed = append(changed, "netif:"+ifname)
	}
	return changed, nil
}

func freeBSDServiceExists(name string) bool {
	out, err := exec.Command("service", "-l").CombinedOutput()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

func parseFreeBSDRCConf(data []byte) (map[string]string, error) {
	values := map[string]string{}
	for lineNo, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid rc.conf line %d: %q", lineNo+1, raw)
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"`)
		if key == "" {
			return nil, fmt.Errorf("invalid rc.conf line %d: empty key", lineNo+1)
		}
		values[key] = value
	}
	return values, nil
}

func freeBSDIfconfigKeyInterface(key string) string {
	if !strings.HasPrefix(key, "ifconfig_") {
		return ""
	}
	name := strings.TrimPrefix(key, "ifconfig_")
	return strings.TrimSuffix(name, "_ipv6")
}

func freeBSDDHCPClientIfnames(data []byte) []string {
	var ifnames []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "interface ") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, "interface "))
		name = strings.TrimSuffix(name, "{")
		name = strings.Trim(strings.TrimSpace(name), `"`)
		ifnames = append(ifnames, name)
	}
	return ifnames
}

func sortedStringMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func applyNftablesConfig(path string, data []byte) ([]string, error) {
	managedTables := []struct {
		family string
		name   string
		header string
	}{
		{family: "inet", name: "routerd_filter", header: "table inet routerd_filter"},
		{family: "inet", name: "routerd_mss", header: "table inet routerd_mss"},
		{family: "ip", name: "routerd_dnat", header: "table ip routerd_dnat"},
		{family: "ip", name: "routerd_nat", header: "table ip routerd_nat"},
		{family: "ip", name: "routerd_policy", header: "table ip routerd_policy"},
	}
	if len(data) == 0 {
		if _, err := exec.LookPath("nft"); err != nil {
			return nil, nil
		}
		existingManaged := false
		for _, table := range managedTables {
			if exec.Command("nft", "list", "table", table.family, table.name).Run() == nil {
				existingManaged = true
				break
			}
		}
		if !existingManaged {
			return nil, nil
		}
		for _, table := range managedTables {
			_ = exec.Command("nft", "delete", "table", table.family, table.name).Run()
		}
		return []string{"nftables:routerd"}, nil
	}
	if _, err := exec.LookPath("nft"); err != nil {
		return nil, fmt.Errorf("nft is required for managed nftables resources: %w", err)
	}
	existingTables := map[string]bool{}
	for _, table := range managedTables {
		if exec.Command("nft", "list", "table", table.family, table.name).Run() == nil {
			existingTables[table.name] = true
		}
	}
	if err := os.MkdirAll(filepathDir(path), 0755); err != nil {
		return nil, fmt.Errorf("create directory for %s: %w", path, err)
	}
	changed, err := writeFileIfChanged(path, data, 0644)
	if err != nil {
		return nil, fmt.Errorf("write nftables config %s: %w", path, err)
	}
	if err := runLogged("nft", "-c", "-f", path); err != nil {
		return nil, fmt.Errorf("validate nftables config %s: %w", path, err)
	}
	natMissing := bytes.Contains(data, []byte("table ip routerd_nat")) && exec.Command("nft", "list", "table", "ip", "routerd_nat").Run() != nil
	policyMissing := bytes.Contains(data, []byte("table ip routerd_policy")) && exec.Command("nft", "list", "table", "ip", "routerd_policy").Run() != nil
	filterMissing := bytes.Contains(data, []byte("table inet routerd_filter")) && exec.Command("nft", "list", "table", "inet", "routerd_filter").Run() != nil
	mssMissing := bytes.Contains(data, []byte("table inet routerd_mss")) && exec.Command("nft", "list", "table", "inet", "routerd_mss").Run() != nil
	dnatMissing := bytes.Contains(data, []byte("table ip routerd_dnat")) && exec.Command("nft", "list", "table", "ip", "routerd_dnat").Run() != nil
	staleManaged := false
	for _, table := range managedTables {
		if existingTables[table.name] && !bytes.Contains(data, []byte(table.header)) {
			staleManaged = true
			break
		}
	}
	if !changed && !natMissing && !policyMissing && !filterMissing && !mssMissing && !dnatMissing && !staleManaged {
		return nil, nil
	}
	for _, table := range managedTables {
		_ = exec.Command("nft", "delete", "table", table.family, table.name).Run()
	}
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
			if errors.Is(err, errNoIPv6PrefixAvailable) {
				applied = append(applied, "skipped-unavailable:"+ifname)
				continue
			}
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
			label, err := applyIPv4PolicyRouteTarget(res.ID(), aliases, target, false)
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
				label, err := applyIPv4PolicyRouteTarget(res.ID(), aliases, target, true)
				if err != nil {
					return nil, err
				}
				if label == "" {
					continue
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
		available := availableIPv4DefaultRouteCandidates(effectiveRouterAvailability{Router: router, Aliases: aliases, RouteSets: routeSets, Health: healthChecks, LinkExists: linkExists}, spec.Candidates)
		candidate, ok := selectIPv4DefaultRouteCandidate(available, healthChecks)
		if !ok {
			return nil, fmt.Errorf("%s has no healthy IPv4 default route candidate", res.ID())
		}
		var healthy []api.IPv4DefaultRoutePolicyCandidate
		for _, target := range available {
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

type effectiveRouterAvailability struct {
	Router     *api.Router
	Aliases    map[string]string
	RouteSets  map[string]api.IPv4PolicyRouteSetSpec
	Health     map[string]bool
	LinkExists func(string) bool
}

func availableIPv4DefaultRouteCandidates(ctx effectiveRouterAvailability, candidates []api.IPv4DefaultRoutePolicyCandidate) []api.IPv4DefaultRoutePolicyCandidate {
	var available []api.IPv4DefaultRoutePolicyCandidate
	for _, candidate := range candidates {
		if candidate.HealthCheck != "" && !ctx.Health[candidate.HealthCheck] {
			continue
		}
		if candidate.RouteSet != "" {
			routeSet, ok := ctx.RouteSets[candidate.RouteSet]
			if !ok || !ipv4RouteSetHasAvailableTarget(ctx, routeSet) {
				continue
			}
			available = append(available, candidate)
			continue
		}
		ifname := ctx.Aliases[candidate.Interface]
		if ifname == "" || !ctx.LinkExists(ifname) {
			continue
		}
		available = append(available, candidate)
	}
	return available
}

func ipv4RouteSetHasAvailableTarget(ctx effectiveRouterAvailability, routeSet api.IPv4PolicyRouteSetSpec) bool {
	for _, target := range routeSet.Targets {
		ifname := ctx.Aliases[target.OutboundInterface]
		if ifname != "" && ctx.LinkExists(ifname) && routeSetTargetUsable(ctx, target.OutboundInterface) {
			return true
		}
	}
	return false
}

func routeSetTargetUsable(ctx effectiveRouterAvailability, name string) bool {
	for _, res := range ctx.Router.Spec.Resources {
		if res.Metadata.Name != name {
			continue
		}
		if res.Kind != "DSLiteTunnel" {
			return true
		}
		spec, err := res.DSLiteTunnelSpec()
		if err != nil {
			return false
		}
		ifname := ctx.Aliases[spec.Interface]
		delegated, err := ipv6DelegatedAddressSpecs(ctx.Router)
		if err != nil {
			return false
		}
		_, _, err = dsliteLocalAddress(spec, ifname, ctx.Aliases, delegated)
		return err == nil
	}
	return true
}

func ipv6DelegatedAddressSpecs(router *api.Router) (map[string]api.IPv6DelegatedAddressSpec, error) {
	delegated := map[string]api.IPv6DelegatedAddressSpec{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv6DelegatedAddress" {
			continue
		}
		spec, err := res.IPv6DelegatedAddressSpec()
		if err != nil {
			return nil, err
		}
		delegated[res.Metadata.Name] = spec
	}
	return delegated, nil
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
		source := healthCheckPingSource(router, spec, aliases)
		if source == "" {
			if defaultString(spec.TargetSource, "auto") == "dsliteRemote" || (spec.TargetSource == "" && healthInterfaceKind(router, spec.Interface) == "DSLiteTunnel") {
				return false, nil
			}
			return false, fmt.Errorf("missing ping source for %s", spec.Interface)
		}
		args = append(args, "-I", source)
	}
	args = append(args, target)
	ctx, cancel := context.WithTimeout(context.Background(), duration+time.Second)
	defer cancel()
	err = exec.CommandContext(ctx, cmdName, args...).Run()
	return err == nil, nil
}

func healthCheckPingSource(router *api.Router, spec api.HealthCheckSpec, aliases map[string]string) string {
	if defaultString(spec.TargetSource, "auto") == "dsliteRemote" || (spec.TargetSource == "" && healthInterfaceKind(router, spec.Interface) == "DSLiteTunnel") {
		for _, res := range router.Spec.Resources {
			if res.Kind != "DSLiteTunnel" || res.Metadata.Name != spec.Interface {
				continue
			}
			tunnel, err := res.DSLiteTunnelSpec()
			if err != nil {
				return ""
			}
			delegated, err := ipv6DelegatedAddressSpecs(router)
			if err != nil {
				return ""
			}
			local, _, err := dsliteLocalAddress(tunnel, aliases[tunnel.Interface], aliases, delegated)
			if err != nil {
				return ""
			}
			return local
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

type ipv4FwmarkRule struct {
	Priority int
	Mark     int
	Table    int
}

func cleanupIPv4ManagedFwmarkRules(router *api.Router) ([]string, error) {
	desired, err := desiredIPv4FwmarkArtifacts(router)
	if err != nil {
		return nil, err
	}
	desiredTables := map[int]bool{}
	for _, artifact := range desired {
		rule, ok := ipv4FwmarkRuleFromArtifact(artifact)
		if ok {
			desiredTables[rule.Table] = true
		}
	}
	current, err := currentIPv4FwmarkArtifacts()
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, artifact := range resource.Orphans(desired, current, managedIPv4FwmarkArtifact) {
		rule, ok := ipv4FwmarkRuleFromArtifact(artifact)
		if !ok {
			continue
		}
		if err := deleteIPv4FwmarkRule(rule); err != nil {
			return nil, err
		}
		label := fmt.Sprintf("priority=%d mark=0x%x table=%d", rule.Priority, rule.Mark, rule.Table)
		if !desiredTables[rule.Table] {
			if err := flushIPv4RouteTable(rule.Table); err != nil {
				return nil, err
			}
			label += " table=flushed"
		}
		removed = append(removed, label)
	}
	return removed, nil
}

func staleIPv4ManagedFwmarkRules(desired map[ipv4FwmarkRule]bool, current []ipv4FwmarkRule) []ipv4FwmarkRule {
	var desiredArtifacts []resource.Artifact
	for rule := range desired {
		desiredArtifacts = append(desiredArtifacts, ipv4FwmarkRuleArtifact("", rule))
	}
	var currentArtifacts []resource.Artifact
	for _, rule := range current {
		currentArtifacts = append(currentArtifacts, ipv4FwmarkRuleArtifact("", rule))
	}
	orphanArtifacts := resource.Orphans(desiredArtifacts, currentArtifacts, managedIPv4FwmarkArtifact)
	stale := make([]ipv4FwmarkRule, 0, len(orphanArtifacts))
	for _, artifact := range orphanArtifacts {
		if rule, ok := ipv4FwmarkRuleFromArtifact(artifact); ok {
			stale = append(stale, rule)
		}
	}
	return stale
}

func desiredIPv4FwmarkArtifacts(router *api.Router) ([]resource.Artifact, error) {
	var desired []resource.Artifact
	add := func(priority, mark, table int) {
		if priority == 0 || mark == 0 || table == 0 {
			return
		}
		desired = append(desired, ipv4FwmarkRuleArtifact("", ipv4FwmarkRule{Priority: priority, Mark: mark, Table: table}))
	}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "IPv4PolicyRoute":
			spec, err := res.IPv4PolicyRouteSpec()
			if err != nil {
				return nil, err
			}
			add(spec.Priority, spec.Mark, spec.Table)
		case "IPv4PolicyRouteSet":
			spec, err := res.IPv4PolicyRouteSetSpec()
			if err != nil {
				return nil, err
			}
			for _, target := range spec.Targets {
				add(target.Priority, target.Mark, target.Table)
			}
		case "IPv4DefaultRoutePolicy":
			spec, err := res.IPv4DefaultRoutePolicySpec()
			if err != nil {
				return nil, err
			}
			for _, candidate := range spec.Candidates {
				if candidate.RouteSet != "" {
					continue
				}
				add(candidate.Priority, candidate.Mark, candidate.Table)
			}
		}
	}
	return desired, nil
}

func desiredIPv4FwmarkRules(router *api.Router) (map[ipv4FwmarkRule]bool, error) {
	artifacts, err := desiredIPv4FwmarkArtifacts(router)
	if err != nil {
		return nil, err
	}
	desired := map[ipv4FwmarkRule]bool{}
	for _, artifact := range artifacts {
		if rule, ok := ipv4FwmarkRuleFromArtifact(artifact); ok {
			desired[rule] = true
		}
	}
	return desired, nil
}

func currentIPv4FwmarkArtifacts() ([]resource.Artifact, error) {
	out, err := exec.Command("ip", "-4", "rule", "show").CombinedOutput()
	if err != nil {
		return nil, err
	}
	var rules []resource.Artifact
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		rule := ipv4FwmarkRule{}
		priority, err := strconv.Atoi(strings.TrimSuffix(fields[0], ":"))
		if err != nil {
			continue
		}
		rule.Priority = priority
		for i, field := range fields {
			switch field {
			case "fwmark":
				if i+1 >= len(fields) {
					continue
				}
				mark, err := strconv.ParseInt(strings.SplitN(fields[i+1], "/", 2)[0], 0, 64)
				if err != nil {
					continue
				}
				rule.Mark = int(mark)
			case "lookup":
				if i+1 >= len(fields) {
					continue
				}
				table, err := strconv.Atoi(fields[i+1])
				if err != nil {
					continue
				}
				rule.Table = table
			}
		}
		if rule.Mark != 0 && rule.Table != 0 {
			rules = append(rules, ipv4FwmarkRuleArtifact("", rule))
		}
	}
	return rules, nil
}

func currentIPv4FwmarkRules() ([]ipv4FwmarkRule, error) {
	artifacts, err := currentIPv4FwmarkArtifacts()
	if err != nil {
		return nil, err
	}
	rules := make([]ipv4FwmarkRule, 0, len(artifacts))
	for _, artifact := range artifacts {
		if rule, ok := ipv4FwmarkRuleFromArtifact(artifact); ok {
			rules = append(rules, rule)
		}
	}
	return rules, nil
}

func ipv4FwmarkRuleArtifact(owner string, rule ipv4FwmarkRule) resource.Artifact {
	return resource.Artifact{
		Kind:  "linux.ipv4.fwmarkRule",
		Name:  fmt.Sprintf("priority=%d,mark=0x%x,table=%d", rule.Priority, rule.Mark, rule.Table),
		Owner: owner,
		Attributes: map[string]string{
			"priority": fmt.Sprintf("%d", rule.Priority),
			"mark":     fmt.Sprintf("0x%x", rule.Mark),
			"table":    fmt.Sprintf("%d", rule.Table),
		},
	}
}

func ipv4FwmarkRuleFromArtifact(artifact resource.Artifact) (ipv4FwmarkRule, bool) {
	priority, err := strconv.Atoi(artifact.Attributes["priority"])
	if err != nil {
		return ipv4FwmarkRule{}, false
	}
	mark, err := strconv.ParseInt(artifact.Attributes["mark"], 0, 64)
	if err != nil {
		return ipv4FwmarkRule{}, false
	}
	table, err := strconv.Atoi(artifact.Attributes["table"])
	if err != nil {
		return ipv4FwmarkRule{}, false
	}
	return ipv4FwmarkRule{Priority: priority, Mark: int(mark), Table: table}, true
}

func managedIPv4FwmarkArtifact(artifact resource.Artifact) bool {
	rule, ok := ipv4FwmarkRuleFromArtifact(artifact)
	return ok && routerdManagedMark(rule.Mark)
}

func routerdManagedMark(mark int) bool {
	return mark >= 0x100 && mark <= 0x1ff
}

func deleteIPv4FwmarkRule(rule ipv4FwmarkRule) error {
	return runLogged(
		"ip", "-4", "rule", "del",
		"priority", fmt.Sprintf("%d", rule.Priority),
		"fwmark", fmt.Sprintf("0x%x", rule.Mark),
		"table", fmt.Sprintf("%d", rule.Table),
	)
}

func flushIPv4RouteTable(table int) error {
	return runLogged("ip", "-4", "route", "flush", "table", fmt.Sprintf("%d", table))
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

func applyIPv4PolicyRouteTarget(resourceID string, aliases map[string]string, target api.IPv4PolicyRouteTarget, skipMissingLink bool) (string, error) {
	ifname := aliases[target.OutboundInterface]
	if ifname == "" {
		return "", fmt.Errorf("%s references outbound interface with empty ifname", resourceID)
	}
	if !linkExists(ifname) {
		if skipMissingLink {
			return "", nil
		}
		return "", fmt.Errorf("%s outbound interface %s does not exist", resourceID, ifname)
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

func linkExists(ifname string) bool {
	return exec.Command("ip", "link", "show", "dev", ifname).Run() == nil
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
	for {
		out, err := exec.Command("ip", "-4", "rule", "show", "priority", priorityText).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			break
		}
		if err := exec.Command("ip", "-4", "rule", "del", "priority", priorityText).Run(); err != nil {
			break
		}
	}
	return runLogged("ip", "-4", "rule", "add", "priority", priorityText, "fwmark", markText, "table", tableText)
}

func applyDSLiteTunnels(router *api.Router) ([]string, error) {
	aliases := map[string]string{}
	for _, res := range router.Spec.Resources {
		if res.Kind == "Interface" {
			spec, err := res.InterfaceSpec()
			if err != nil {
				return nil, err
			}
			aliases[res.Metadata.Name] = spec.IfName
		}
	}
	delegated, err := ipv6DelegatedAddressSpecs(router)
	if err != nil {
		return nil, err
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
			if !errors.Is(err, errNoIPv6PrefixAvailable) {
				return nil, fmt.Errorf("%s local address: %w", res.ID(), err)
			}
			_ = deleteDSLiteTunnel(tunnelName)
			applied = append(applied, "removed-unusable:"+tunnelName)
			continue
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

func deleteDSLiteTunnel(name string) error {
	if name == "" {
		return nil
	}
	return exec.Command("ip", "-6", "tunnel", "del", name).Run()
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
	if address, err := deriveIPv6Address(ipv6Prefixes(ifname), suffix); err == nil {
		return address, nil
	}
	return deriveIPv6AddressFromGlobalAddress(ipv6Addresses(ifname), suffix)
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
		if prefix.Addr().IsLinkLocalUnicast() {
			continue
		}
		addrBytes := prefix.Masked().Addr().As16()
		for i := range addrBytes {
			addrBytes[i] |= suffixBytes[i]
		}
		return netip.AddrFrom16(addrBytes).String(), nil
	}
	return "", errNoIPv6PrefixAvailable
}

func deriveIPv6AddressFromGlobalAddress(addresses []string, suffix string) (string, error) {
	suffixAddr, err := netip.ParseAddr(suffix)
	if err != nil || !suffixAddr.Is6() {
		return "", fmt.Errorf("invalid IPv6 suffix %q", suffix)
	}
	suffixBytes := suffixAddr.As16()
	for _, value := range addresses {
		addr, err := netip.ParseAddr(value)
		if err != nil || !addr.Is6() || addr.IsLinkLocalUnicast() {
			continue
		}
		addrBytes := addr.As16()
		for i := 8; i < len(addrBytes); i++ {
			addrBytes[i] = 0
		}
		for i := range addrBytes {
			addrBytes[i] |= suffixBytes[i]
		}
		return netip.AddrFrom16(addrBytes).String(), nil
	}
	return "", errNoIPv6PrefixAvailable
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
	dnsmasqPath, err := exec.LookPath("dnsmasq")
	if err != nil {
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
	serviceChanged, err := writeFileIfChanged(servicePath, render.DnsmasqServiceUnit(configPath, dnsmasqPath), 0644)
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
		if !strings.HasPrefix(servicePath, "/run/systemd/system/") {
			if err := runLogged("systemctl", "enable", routerdDnsmasqService); err != nil {
				return nil, err
			}
		}
		if err := runLogged("systemctl", "restart", routerdDnsmasqService); err != nil {
			return nil, err
		}
		return changedFiles, nil
	}
	if err := runLogged("systemctl", "is-active", "--quiet", routerdDnsmasqService); err != nil {
		if strings.HasPrefix(servicePath, "/run/systemd/system/") {
			if err := runLogged("systemctl", "restart", routerdDnsmasqService); err != nil {
				return nil, err
			}
		} else {
			if err := runLogged("systemctl", "enable", "--now", routerdDnsmasqService); err != nil {
				return nil, err
			}
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
	return platformDefaults.RuntimeDir
}

func defaultStateDir() string {
	return platformDefaults.StateDir
}

func defaultStatusFile() string {
	return platformDefaults.StatusFile()
}

func defaultSocketPath() string {
	return platformDefaults.SocketFile()
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
	fmt.Fprintln(w, "  adopt --config <path> --candidates")
	fmt.Fprintln(w, "  adopt --config <path> --apply")
	fmt.Fprintln(w, "  render nixos --config <path> [--out <path>]")
	fmt.Fprintln(w, "  reconcile --config <path> --once [--dry-run]")
	fmt.Fprintln(w, "  serve --config <path> [--socket <path>]")
	fmt.Fprintln(w, "  run --config <path>")
	fmt.Fprintln(w, "  status [--status-file <path>]")
	fmt.Fprintln(w, "  plugin list --plugin-dir <path>")
	fmt.Fprintln(w, "  plugin inspect <plugin-name> --plugin-dir <path>")
}
