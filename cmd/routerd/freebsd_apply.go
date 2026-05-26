// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/eventlog"
	"github.com/imksoo/routerd/pkg/render"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func runFreeBSDApplyOnce(router *api.Router, opts applyOptions, stdout io.Writer, logger *eventlog.Logger, engine *apply.Engine, result *apply.Result, generation int64, stateStore routerstate.Store) (*apply.Result, error) {
	policy := effectiveApplyPolicy(router)
	var applyErrors []string
	recordStageError := func(stage string, err error) error {
		if err == nil {
			return nil
		}
		msg := fmt.Sprintf("%s: %v", stage, err)
		result.Warnings = append(result.Warnings, msg)
		applyErrors = append(applyErrors, msg)
		logger.Emit(eventlog.LevelError, "apply", "routerd FreeBSD apply stage failed", map[string]string{"stage": stage, "error": err.Error()})
		if policy.Mode != "progressive" {
			return fmt.Errorf("%s: %w", stage, err)
		}
		return nil
	}

	var changedFreeBSD []string
	if err := recordStageError("freebsd-network", func() error {
		var err error
		var fbWarnings []string
		rcScriptDir := platformDefaults.RCScriptDir
		if opts.SkipServiceManager {
			rcScriptDir = ""
		}
		changedFreeBSD, fbWarnings, err = applyFreeBSDConfigWithOptions(router, stateStore, defaultFreeBSDDHClientPath, defaultFreeBSDMPD5Path, defaultFreeBSDPFPath, rcScriptDir, freeBSDConfigApplyOptions{
			ManageServices: !opts.SkipServiceManager,
		})
		for _, w := range fbWarnings {
			result.Warnings = append(result.Warnings, w)
			logger.Emit(eventlog.LevelWarning, "apply", w, map[string]string{"stage": "freebsd-network"})
		}
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
	var appliedIPv6DelegatedAddresses []string
	if err := recordStageError("ipv6-delegated-address", func() error {
		var err error
		appliedIPv6DelegatedAddresses, err = applyIPv6DelegatedAddressesWithState(router, stateStore)
		return err
	}()); err != nil {
		return nil, err
	}
	var dnsmasqChangedFiles []string
	if err := recordStageError("dnsmasq", func() error {
		dnsmasqConfig, dnsmasqWarnings, err := render.DnsmasqConfig(router, render.DnsmasqRuntime{
			DHCPv4DNSServersByInterface: observedDNSServersByInterface(router),
			DHCPv6DNSServersByInterface: observedDNSServersByInterface(router),
			IPv6AddressesByInterface:    observedIPv6AddressesByInterface(router),
			IPv6PrefixesByInterface:     observedIPv6PrefixesByInterface(router),
			StickyHosts:                 dhcpStickyHostsFromLog(router, time.Now().UTC()),
			RuntimeDir:                  platformDefaults.RuntimeDir,
			LeaseFile:                   dnsmasqLeaseFileForPlatform(),
		})
		if err != nil {
			return err
		}
		for _, w := range dnsmasqWarnings {
			result.Warnings = append(result.Warnings, w)
			logger.Emit(eventlog.LevelWarning, "apply", w, map[string]string{"stage": "dnsmasq"})
		}
		dnsmasqServicePath := opts.DnsmasqServicePath
		if opts.SkipServiceManager {
			dnsmasqServicePath = ""
		}
		dnsmasqChangedFiles, err = applyDnsmasqConfig(opts.DnsmasqConfigPath, dnsmasqServicePath, dnsmasqConfig)
		return err
	}()); err != nil {
		return nil, err
	}
	var appliedTunnels []string
	if err := recordStageError("ds-lite", func() error {
		var err error
		appliedTunnels, err = applyDSLiteTunnelsWithState(router, stateStore)
		return err
	}()); err != nil {
		return nil, err
	}
	var cleanedPreDSLiteOrphans []string
	if err := recordStageError("ds-lite-cleanup", func() error {
		var err error
		cleanedPreDSLiteOrphans, err = cleanupStaleDSLiteTunnels(router)
		cleanedAliases, aliasErr := cleanupStaleDSLiteIPv4Aliases(router)
		cleanedPreDSLiteOrphans = append(cleanedPreDSLiteOrphans, cleanedAliases...)
		if err != nil {
			return err
		}
		return aliasErr
	}()); err != nil {
		return nil, err
	}
	var cleanedLegacyState []string
	if err := recordStageError("freebsd-legacy-state-cleanup", func() error {
		var err error
		cleanedLegacyState, err = cleanupLegacyFreeBSDStateDir()
		return err
	}()); err != nil {
		return nil, err
	}
	var appliedIPv6DefaultRoutes []string
	if err := recordStageError("ipv6-default-route", func() error {
		var err error
		appliedIPv6DefaultRoutes, err = applyFreeBSDIPv6DefaultRoutes(router)
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
	for _, address := range appliedIPv6DelegatedAddresses {
		fmt.Fprintf(stdout, "applied IPv6 delegated address %s\n", address)
	}
	for _, path := range dnsmasqChangedFiles {
		fmt.Fprintf(stdout, "applied dnsmasq %s\n", path)
	}
	for _, tunnel := range appliedTunnels {
		fmt.Fprintf(stdout, "applied DS-Lite tunnel %s\n", tunnel)
	}
	for _, artifact := range cleanedPreDSLiteOrphans {
		fmt.Fprintf(stdout, "removed orphaned owned artifact %s\n", artifact)
	}
	for _, artifact := range cleanedLegacyState {
		fmt.Fprintf(stdout, "moved legacy FreeBSD state %s\n", artifact)
	}
	for _, route := range appliedIPv6DefaultRoutes {
		fmt.Fprintf(stdout, "applied IPv6 default route %s\n", route)
	}
	if len(changedFreeBSD) == 0 && len(appliedRuntime) == 0 && len(appliedHostnames) == 0 && len(appliedIPv6DelegatedAddresses) == 0 && len(dnsmasqChangedFiles) == 0 && len(appliedTunnels) == 0 && len(cleanedPreDSLiteOrphans) == 0 && len(cleanedLegacyState) == 0 && len(appliedIPv6DefaultRoutes) == 0 {
		fmt.Fprintln(stdout, "FreeBSD configuration already up to date")
	}

	var cleanedLedgerOrphans []string
	var rememberedArtifacts int
	if len(applyErrors) == 0 {
		var err error
		cleanedLedgerOrphans, err = cleanupLedgerOwnedOrphans(router, opts.LedgerPath)
		if err != nil {
			return nil, err
		}
		rememberedArtifacts, err = rememberAppliedArtifacts(router, opts.LedgerPath, generation)
		if err != nil {
			return nil, err
		}
		if err := recordLastAppliedPath(router, stateStore, opts.ConfigPath); err != nil {
			return nil, err
		}
	} else {
		result.Warnings = append(result.Warnings, "skipped ownership recording because FreeBSD apply completed with stage errors")
	}
	if rememberedArtifacts > 0 {
		fmt.Fprintf(stdout, "remembered %d owned artifacts\n", rememberedArtifacts)
	}
	for _, artifact := range cleanedLedgerOrphans {
		fmt.Fprintf(stdout, "removed orphaned owned artifact %s\n", artifact)
	}

	applyWarnings := append([]string{}, result.Warnings...)
	next, err := engine.Plan(router)
	if err != nil {
		return nil, err
	}
	if generation != 0 {
		next.Generation = generation
	}
	next.Warnings = append(next.Warnings, applyWarnings...)
	if len(applyErrors) > 0 {
		next.Phase = "Degraded"
	}
	if err := appendLedgerOwnedOrphans(next, router, opts.LedgerPath, false); err != nil {
		return nil, err
	}
	if err := writeResult(stdout, opts.StatusFile, next); err != nil {
		return nil, err
	}
	logger.Emit(eventlog.LevelInfo, "apply", "routerd FreeBSD changes applied", map[string]string{
		"freebsdChanges":      fmt.Sprintf("%d", len(changedFreeBSD)),
		"runtimeSysctls":      fmt.Sprintf("%d", len(appliedRuntime)),
		"hostnames":           fmt.Sprintf("%d", len(appliedHostnames)),
		"delegatedAddresses":  fmt.Sprintf("%d", len(appliedIPv6DelegatedAddresses)),
		"dnsmasqFiles":        fmt.Sprintf("%d", len(dnsmasqChangedFiles)),
		"dsliteTunnels":       fmt.Sprintf("%d", len(appliedTunnels)),
		"dsliteCleanup":       fmt.Sprintf("%d", len(cleanedPreDSLiteOrphans)),
		"legacyStateCleanup":  fmt.Sprintf("%d", len(cleanedLegacyState)),
		"ipv6DefaultRoutes":   fmt.Sprintf("%d", len(appliedIPv6DefaultRoutes)),
		"ledgerCleanup":       fmt.Sprintf("%d", len(cleanedLedgerOrphans)),
		"rememberedArtifacts": fmt.Sprintf("%d", rememberedArtifacts),
	})
	return next, nil
}

type freeBSDConfigApplyOptions struct {
	ManageServices bool
}

func applyFreeBSDConfigWithOptions(router *api.Router, stateStore routerstate.Store, dhclientPath, mpd5Path, pfPath, rcScriptDir string, opts freeBSDConfigApplyOptions) ([]string, []string, error) {
	data, err := render.FreeBSDWithPPPoEPasswords(router, pppoePassword)
	if err != nil {
		return nil, nil, err
	}
	warnings := append([]string(nil), data.Warnings...)
	rcValues, err := parseFreeBSDRCConf(data.RCConf)
	if err != nil {
		return nil, warnings, err
	}
	var changed []string
	var restartIfnames []string
	appliedPackages, err := applyFreeBSDPackages(router)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("pkg install: %v", err))
	} else {
		changed = append(changed, appliedPackages...)
	}
	newKeys := sortedStringMapKeys(rcValues)
	for _, key := range newKeys {
		value := rcValues[key]
		currentOut, err := exec.Command("sysrc", key).CombinedOutput()
		if err == nil && parseFreeBSDSysrcValue(key, currentOut) == value {
			continue
		}
		if err := runLogged("sysrc", key+"="+value); err != nil {
			return changed, warnings, err
		}
		changed = append(changed, "sysrc:"+key)
		if ifname := freeBSDIfconfigKeyInterface(key); ifname != "" {
			restartIfnames = append(restartIfnames, ifname)
		}
	}
	if stateStore != nil {
		stateStore.Set(freebsdSysrcStateKey, strings.Join(newKeys, ","), "applyFreeBSDConfig: tracked sysrc keys")
	}
	if len(data.DHCPClient) > 0 && dhclientPath != "" {
		fileChanged, err := writeFileIfChanged(dhclientPath, data.DHCPClient, 0644)
		if err != nil {
			return changed, warnings, err
		}
		if fileChanged {
			changed = append(changed, dhclientPath)
			restartIfnames = append(restartIfnames, freeBSDDHCPClientIfnames(data.DHCPClient)...)
		}
	}
	if len(data.NTP) > 0 {
		ntpPath := filepath.Join(defaultString(platformDefaults.SysconfDir, "/usr/local/etc/routerd"), "ntp.conf")
		if err := os.MkdirAll(filepathDir(ntpPath), 0755); err != nil {
			return changed, warnings, err
		}
		fileChanged, err := writeFileIfChanged(ntpPath, data.NTP, 0644)
		if err != nil {
			return changed, warnings, err
		}
		if fileChanged {
			changed = append(changed, ntpPath)
		}
		if opts.ManageServices && (fileChanged || freeBSDRCValuesChanged(changed, "ntpd_") || !freeBSDServiceRunning("ntpd")) && rcValues["ntpd_enable"] == "YES" && freeBSDServiceExists("ntpd") {
			action := "restart"
			if !freeBSDServiceRunning("ntpd") {
				action = "start"
			}
			if err := runLogged("service", "ntpd", action); err != nil {
				warnings = append(warnings, fmt.Sprintf("service ntpd %s: %v", action, err))
			} else {
				changed = append(changed, "service:ntpd")
			}
		}
	}
	if len(data.PF) > 0 && pfPath != "" {
		applied, err := applyFreeBSDPFConfigWithOptions(data.PF, pfPath, freeBSDPFApplyOptions{ManageServices: opts.ManageServices})
		if err != nil {
			return changed, warnings, err
		}
		changed = append(changed, applied...)
	}
	if opts.ManageServices && len(data.RCDScripts) > 0 && rcScriptDir != "" {
		applied, err := applyFreeBSDRCDScripts(data.RCDScripts, rcScriptDir)
		if err != nil {
			return changed, warnings, err
		}
		changed = append(changed, applied...)
	}
	if len(data.MPD5) > 0 && mpd5Path != "" {
		if err := os.MkdirAll(filepathDir(mpd5Path), 0755); err != nil {
			return changed, warnings, err
		}
		fileChanged, err := writeFileIfChanged(mpd5Path, data.MPD5, 0600)
		if err != nil {
			return changed, warnings, err
		}
		if fileChanged {
			changed = append(changed, mpd5Path)
		}
		if opts.ManageServices && (fileChanged || freeBSDRCValuesChanged(changed, "mpd_") || !freeBSDServiceRunning("mpd5")) && rcValues["mpd_enable"] == "YES" && freeBSDServiceExists("mpd5") {
			if err := runLogged("service", "mpd5", "restart"); err != nil {
				return changed, warnings, err
			}
			changed = append(changed, "service:mpd5")
		}
	}
	if opts.ManageServices && rcValues["tailscaled_enable"] == "YES" && freeBSDServiceExists("tailscaled") && !freeBSDServiceRunning("tailscaled") {
		if err := runLogged("service", "tailscaled", "onestart"); err != nil {
			return changed, warnings, err
		}
		changed = append(changed, "service:tailscaled")
	}
	for _, ifname := range orderFreeBSDNetifRestarts(compactStringList(restartIfnames)) {
		if freeBSDProtectedIfnames(router)[ifname] {
			changed = append(changed, "netif:skipped-protected:"+ifname)
			continue
		}
		if err := runLogged("service", "netif", "restart", ifname); err != nil {
			return changed, warnings, err
		}
		changed = append(changed, "netif:"+ifname)
	}
	return changed, warnings, nil
}

func applyFreeBSDPackages(router *api.Router) ([]string, error) {
	var missing []string
	seen := map[string]bool{}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "Package" {
			continue
		}
		spec, err := resource.PackageSpec()
		if err != nil {
			return nil, err
		}
		set, ok := packageSetForOSMain(spec, "freebsd")
		if !ok {
			continue
		}
		for _, name := range set.Names {
			name = strings.TrimSpace(name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			if _, err := exec.Command("pkg", "info", "-e", name).CombinedOutput(); err != nil {
				missing = append(missing, name)
			}
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}
	args := append([]string{"install", "-y"}, missing...)
	if err := runLogged("pkg", args...); err != nil {
		return nil, err
	}
	return []string{"pkg:" + strings.Join(missing, ",")}, nil
}

type freeBSDPFApplyOptions struct {
	ManageServices bool
}

func applyFreeBSDPFConfigWithOptions(data []byte, pfPath string, opts freeBSDPFApplyOptions) ([]string, error) {
	if len(data) == 0 || pfPath == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepathDir(pfPath), 0755); err != nil {
		return nil, fmt.Errorf("create directory for %s: %w", pfPath, err)
	}
	fileChanged, err := writeFileIfChanged(pfPath, data, 0600)
	if err != nil {
		return nil, fmt.Errorf("write pf config %s: %w", pfPath, err)
	}
	for _, module := range []string{"pf", "pflog"} {
		if err := ensureFreeBSDKernelModule(module); err != nil {
			return nil, err
		}
	}
	if err := runLogged("pfctl", "-nf", pfPath); err != nil {
		return nil, err
	}
	var changed []string
	if fileChanged {
		if err := runLogged("pfctl", "-f", pfPath); err != nil {
			return nil, err
		}
		changed = append(changed, pfPath)
	}
	if !freeBSDPFEnabled() {
		if err := runLogged("pfctl", "-e"); err != nil {
			return changed, err
		}
		changed = append(changed, "pfctl:-e")
	}
	if !opts.ManageServices {
		return changed, nil
	}
	if !freeBSDServiceRunning("pf") {
		if err := runLogged("service", "pf", "onestart"); err != nil {
			return changed, err
		}
		changed = append(changed, "service:pf")
	}
	if !freeBSDServiceRunning("pflog") {
		if err := runLogged("service", "pflog", "onestart"); err != nil {
			return changed, err
		}
		changed = append(changed, "service:pflog")
	}
	return changed, nil
}

func freeBSDPFEnabled() bool {
	out, err := exec.Command("pfctl", "-s", "info").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Status: Enabled")
}

func ensureFreeBSDKernelModule(module string) error {
	if module == "" {
		return nil
	}
	if err := exec.Command("kldstat", "-q", "-m", module).Run(); err == nil {
		return nil
	}
	out, err := exec.Command("kldload", module).CombinedOutput()
	if err == nil {
		return nil
	}
	text := strings.ToLower(string(out))
	if strings.Contains(text, "file exists") || strings.Contains(text, "already loaded") {
		return nil
	}
	return fmt.Errorf("kldload %s: %w: %s", module, err, strings.TrimSpace(string(out)))
}

func applyFreeBSDRCDScripts(scripts map[string][]byte, rcScriptDir string) ([]string, error) {
	if len(scripts) == 0 || rcScriptDir == "" {
		return nil, nil
	}
	if err := os.MkdirAll(rcScriptDir, 0755); err != nil {
		return nil, fmt.Errorf("create rc.d directory %s: %w", rcScriptDir, err)
	}
	var changed []string
	disabled, err := disableStaleFreeBSDRCDBackups(rcScriptDir)
	if err != nil {
		return changed, err
	}
	changed = append(changed, disabled...)
	for _, name := range sortedByteSliceMapKeys(scripts) {
		path := filepath.Join(rcScriptDir, name)
		fileChanged, err := writeFileIfChanged(path, scripts[name], 0555)
		if err != nil {
			return changed, fmt.Errorf("write rc.d script %s: %w", path, err)
		}
		if fileChanged {
			changed = append(changed, path)
		}
		if name == "routerd" {
			if fileChanged {
				changed = append(changed, "service:routerd:restart-required")
			}
			continue
		}
		if fileChanged && freeBSDServiceRunning(name) {
			if err := runFreeBSDService(name, "onerestart"); err != nil {
				return changed, err
			}
			changed = append(changed, "service:"+name)
			continue
		}
		if !freeBSDServiceRunning(name) {
			if err := runFreeBSDService(name, "onestart"); err != nil {
				return changed, err
			}
			changed = append(changed, "service:"+name)
		}
	}
	return changed, nil
}

func disableStaleFreeBSDRCDBackups(rcScriptDir string) ([]string, error) {
	entries, err := os.ReadDir(rcScriptDir)
	if err != nil {
		return nil, err
	}
	var changed []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !(strings.HasPrefix(name, "routerd.backup.") || strings.HasPrefix(name, "routerd.recovery.") || strings.HasPrefix(name, "routerd.recursive-bad.")) {
			continue
		}
		path := filepath.Join(rcScriptDir, name)
		info, err := entry.Info()
		if err != nil {
			return changed, err
		}
		if info.Mode()&0111 == 0 {
			continue
		}
		if err := os.Chmod(path, info.Mode()&^os.FileMode(0111)); err != nil {
			return changed, err
		}
		changed = append(changed, "rc.d:disable-stale:"+path)
	}
	return changed, nil
}

func freeBSDProtectedIfnames(router *api.Router) map[string]bool {
	aliases := map[string]string{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "Interface" {
			continue
		}
		spec, err := res.InterfaceSpec()
		if err != nil {
			continue
		}
		aliases[res.Metadata.Name] = spec.IfName
	}
	protected := map[string]bool{}
	for _, name := range effectiveApplyPolicy(router).ProtectedInterfaces {
		if ifname := aliases[name]; ifname != "" {
			protected[ifname] = true
		}
	}
	return protected
}

func parseFreeBSDSysrcValue(key string, out []byte) string {
	line := strings.TrimSpace(string(out))
	prefix := key + ":"
	if value, ok := strings.CutPrefix(line, prefix); ok {
		return strings.TrimSpace(value)
	}
	return line
}

func freeBSDRCValuesChanged(changed []string, prefix string) bool {
	for _, item := range changed {
		key, ok := strings.CutPrefix(item, "sysrc:")
		if ok && strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
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

func freeBSDServiceRunning(name string) bool {
	return exec.Command("service", name, "status").Run() == nil
}

func runFreeBSDService(name, action string) error {
	err := runLogged("service", name, action)
	if err == nil {
		return nil
	}
	if action == "onestart" {
		text := strings.ToLower(err.Error())
		if strings.Contains(text, "already running") || strings.Contains(text, "process already running") {
			return nil
		}
	}
	return err
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
	name = strings.TrimSuffix(name, "_ipv6")
	if base, _, ok := strings.Cut(name, "_alias"); ok {
		return base
	}
	return name
}

func orderFreeBSDNetifRestarts(ifnames []string) []string {
	out := append([]string(nil), ifnames...)
	rank := func(name string) int {
		switch {
		case strings.HasPrefix(name, "vxlan"):
			return 0
		case strings.HasPrefix(name, "bridge"):
			return 2
		default:
			return 1
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i]), rank(out[j])
		if ri != rj {
			return ri < rj
		}
		return out[i] < out[j]
	})
	return out
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

func sortedByteSliceMapKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
