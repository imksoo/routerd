// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func applyDnsmasqConfig(configPath, servicePath string, configData []byte) ([]string, error) {
	if len(configData) == 0 {
		return nil, nil
	}
	dnsmasqPath, err := findDnsmasqPath()
	if err != nil {
		return nil, fmt.Errorf("dnsmasq is required for managed IPv4 DHCP service: %w", err)
	}
	if platformDefaults.OS == platform.OSFreeBSD {
		return applyFreeBSDDnsmasqConfig(configPath, servicePath, configData, dnsmasqPath)
	}
	if isNixOSHost() {
		return applyNixOSDnsmasqConfig(configPath, configData, dnsmasqPath)
	}
	if platformFeatures.HasOpenRC {
		return applyOpenRCDnsmasqConfig(configPath, servicePath, configData, dnsmasqPath)
	}
	if !platformFeatures.HasSystemd {
		return applyDirectDnsmasqConfig(configPath, configData, dnsmasqPath)
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

func applyVRRPArtifactsOnce(router *api.Router, store routerstate.Store) ([]string, bool, error) {
	if router == nil || platformDefaults.OS == platform.OSFreeBSD || isNixOSHost() || !routerHasVirtualAddress(router) {
		return nil, false, nil
	}
	data, err := render.KeepalivedConfig(router, routerInterfaceAliases(router.Spec.Resources))
	if err != nil || len(data) == 0 {
		return nil, false, err
	}
	if err := os.MkdirAll(filepath.Dir(runtimeKeepalivedConfigPath), 0755); err != nil {
		return nil, false, err
	}
	changed, err := writeFileIfChanged(runtimeKeepalivedConfigPath, data, 0644)
	if err != nil {
		return nil, false, err
	}
	if statusStore, ok := store.(routerstate.ObjectStatusStore); ok {
		if err := saveVRRPRenderedStatuses(router, statusStore, changed); err != nil {
			return nil, false, err
		}
	}
	if !changed {
		return nil, false, nil
	}
	return []string{runtimeKeepalivedConfigPath}, true, nil
}

func saveVRRPRenderedStatuses(router *api.Router, store routerstate.ObjectStatusStore, changed bool) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	aliases := routerInterfaceAliases(router.Spec.Resources)
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "VirtualAddress" {
			continue
		}
		address, ifname, mode := "", "", ""
		spec, err := resource.VirtualAddressSpec()
		if err != nil {
			return err
		}
		address = spec.Address
		if resolved, err := render.VirtualAddress(router, spec); err == nil {
			address = resolved
		}
		ifname = aliases[spec.Interface]
		mode = spec.Mode
		status := map[string]any{
			"phase":      "Rendered",
			"backend":    "keepalived",
			"address":    address,
			"ifname":     ifname,
			"configPath": runtimeKeepalivedConfigPath,
			"applyWith":  "routerd serve",
			"changed":    changed,
			"dryRun":     false,
			"observedAt": now,
		}
		if mode == "vrrp" {
			status["role"] = "unknown"
		}
		if err := store.SaveObjectStatus(api.NetAPIVersion, resource.Kind, resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	return nil
}

func routerHasVirtualAddress(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, res := range router.Spec.Resources {
		if res.APIVersion == api.NetAPIVersion && res.Kind == "VirtualAddress" {
			return true
		}
	}
	return false
}

func routerHasBGP(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, res := range router.Spec.Resources {
		if res.APIVersion == api.NetAPIVersion && (res.Kind == "BGPRouter" || res.Kind == "BGPPeer") {
			return true
		}
	}
	return false
}

func applyBGPArtifactsOnce(router *api.Router, store routerstate.Store) ([]string, bool, error) {
	if router == nil || platformDefaults.OS == platform.OSFreeBSD || isNixOSHost() || !routerHasBGP(router) {
		return nil, false, nil
	}
	if statusStore, ok := store.(routerstate.ObjectStatusStore); ok {
		if err := saveBGPServeManagedStatuses(router, statusStore); err != nil {
			return nil, false, err
		}
	}
	return nil, false, nil
}

func saveBGPServeManagedStatuses(router *api.Router, store routerstate.ObjectStatusStore) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || (resource.Kind != "BGPRouter" && resource.Kind != "BGPPeer") {
			continue
		}
		status := map[string]any{
			"phase":      "Pending",
			"backend":    "gobgp",
			"applyWith":  "routerd serve",
			"changed":    false,
			"dryRun":     false,
			"reason":     "GoBGPServeManaged",
			"observedAt": now,
			"conditions": []map[string]any{{"type": "Configured", "status": "False", "reason": "GoBGPServeManaged", "message": "BGP is managed by routerd serve through the routerd-bgp daemon"}},
		}
		if err := store.SaveObjectStatus(api.NetAPIVersion, resource.Kind, resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	return nil
}

func applyDirectDnsmasqConfig(configPath string, configData []byte, dnsmasqPath string) ([]string, error) {
	var changedFiles []string
	if err := os.MkdirAll(filepathDir(configPath), 0755); err != nil {
		return nil, fmt.Errorf("create directory for %s: %w", configPath, err)
	}
	if err := os.MkdirAll(platformDefaults.RuntimeDir, 0755); err != nil {
		return nil, fmt.Errorf("create runtime directory %s: %w", platformDefaults.RuntimeDir, err)
	}
	leaseFile := dnsmasqLeaseFileForPlatform()
	if leaseFile == "" {
		leaseFile = strings.TrimRight(platformDefaults.RuntimeDir, "/") + "/dnsmasq.leases"
	}
	if err := os.MkdirAll(filepathDir(leaseFile), 0755); err != nil {
		return nil, fmt.Errorf("create dnsmasq lease directory %s: %w", filepathDir(leaseFile), err)
	}
	changed, err := writeFileIfChanged(configPath, configData, 0644)
	if err != nil {
		return nil, fmt.Errorf("write dnsmasq config %s: %w", configPath, err)
	}
	if changed {
		changedFiles = append(changedFiles, configPath)
	}
	out, err := exec.Command(dnsmasqPath, "--test", "--conf-file="+configPath).CombinedOutput()
	if err != nil {
		return changedFiles, fmt.Errorf("%s --test --conf-file=%s: %w: %s", dnsmasqPath, configPath, err, strings.TrimSpace(string(out)))
	}
	pidFile := strings.TrimRight(platformDefaults.RuntimeDir, "/") + "/dnsmasq.pid"
	running := processRunningFromPIDFile(pidFile)
	if changed && running {
		_ = stopProcessFromPIDFile(pidFile)
		running = false
	}
	if !running {
		cmd := exec.Command(dnsmasqPath, "--conf-file="+configPath, "--pid-file="+pidFile)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return changedFiles, fmt.Errorf("start dnsmasq: %w", err)
		}
		changedFiles = append(changedFiles, "process:dnsmasq")
	}
	return changedFiles, nil
}

func applyOpenRCDnsmasqConfig(configPath, servicePath string, configData []byte, dnsmasqPath string) ([]string, error) {
	if servicePath == "" {
		if platformDefaults.OpenRCScriptDir == "" {
			return nil, errors.New("OpenRC script directory is not configured")
		}
		servicePath = filepath.Join(platformDefaults.OpenRCScriptDir, "routerd_dnsmasq")
	}
	var changedFiles []string
	if err := os.MkdirAll(filepathDir(configPath), 0755); err != nil {
		return nil, fmt.Errorf("create directory for %s: %w", configPath, err)
	}
	if err := os.MkdirAll(platformDefaults.RuntimeDir, 0755); err != nil {
		return nil, fmt.Errorf("create runtime directory %s: %w", platformDefaults.RuntimeDir, err)
	}
	leaseFile := dnsmasqLeaseFileForPlatform()
	if leaseFile == "" {
		leaseFile = strings.TrimRight(platformDefaults.StateDir, "/") + "/dnsmasq/dnsmasq.leases"
	}
	if err := os.MkdirAll(filepathDir(leaseFile), 0755); err != nil {
		return nil, fmt.Errorf("create dnsmasq lease directory %s: %w", filepathDir(leaseFile), err)
	}
	changed, err := writeFileIfChanged(configPath, configData, 0644)
	if err != nil {
		return nil, fmt.Errorf("write dnsmasq config %s: %w", configPath, err)
	}
	if changed {
		changedFiles = append(changedFiles, configPath)
	}
	out, err := exec.Command(dnsmasqPath, "--test", "--conf-file="+configPath).CombinedOutput()
	if err != nil {
		return changedFiles, fmt.Errorf("%s --test --conf-file=%s: %w: %s", dnsmasqPath, configPath, err, strings.TrimSpace(string(out)))
	}
	if err := os.MkdirAll(filepathDir(servicePath), 0755); err != nil {
		return changedFiles, fmt.Errorf("create directory for %s: %w", servicePath, err)
	}
	script, err := render.DnsmasqOpenRCScript(configPath, dnsmasqPath)
	if err != nil {
		return changedFiles, err
	}
	serviceChanged, err := writeFileIfChanged(servicePath, script, 0755)
	if err != nil {
		return changedFiles, fmt.Errorf("write OpenRC dnsmasq script %s: %w", servicePath, err)
	}
	if serviceChanged {
		changedFiles = append(changedFiles, servicePath)
	}
	enabledServices := openRCEnabledServices()
	if !enabledServices["routerd_dnsmasq"] {
		if err := runLogged("rc-update", "add", "routerd_dnsmasq", "default"); err != nil {
			return changedFiles, err
		}
	}
	active := openRCServiceActive("routerd_dnsmasq")
	if len(changedFiles) > 0 && active {
		if err := runLogged("rc-service", "routerd_dnsmasq", "restart"); err != nil {
			return changedFiles, err
		}
		changedFiles = append(changedFiles, "service:routerd_dnsmasq")
		return changedFiles, nil
	}
	if !active {
		if err := runLogged("rc-service", "routerd_dnsmasq", "start"); err != nil {
			return changedFiles, err
		}
		changedFiles = append(changedFiles, "service:routerd_dnsmasq")
	}
	return changedFiles, nil
}

func processRunningFromPIDFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func stopProcessFromPIDFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	_ = process.Signal(syscall.SIGTERM)
	for range 20 {
		if process.Signal(syscall.Signal(0)) != nil {
			_ = os.Remove(path)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = process.Signal(syscall.SIGKILL)
	_ = os.Remove(path)
	return nil
}

func findDnsmasqPath() (string, error) {
	dnsmasqPath, err := exec.LookPath("dnsmasq")
	if err == nil {
		return dnsmasqPath, nil
	}
	for _, path := range []string{
		"/run/current-system/sw/bin/dnsmasq",
		"/nix/var/nix/profiles/system/sw/bin/dnsmasq",
		"/usr/local/sbin/dnsmasq",
		"/usr/sbin/dnsmasq",
	} {
		if st, statErr := os.Stat(path); statErr == nil && !st.IsDir() && st.Mode()&0111 != 0 {
			return path, nil
		}
	}
	return "", err
}

func applyNixOSDnsmasqConfig(configPath string, configData []byte, dnsmasqPath string) ([]string, error) {
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
	if err := runLogged(dnsmasqPath, "--test", "--conf-file="+configPath); err != nil {
		return nil, err
	}
	if err := removeStaleNixOSRuntimeDnsmasqUnit(&changedFiles); err != nil {
		return nil, err
	}
	if changed {
		if err := restartNixOSDnsmasqService(); err != nil {
			return nil, err
		}
		return changedFiles, nil
	}
	if err := runLogged("systemctl", "is-active", "--quiet", routerdDnsmasqService); err != nil {
		if err := restartNixOSDnsmasqService(); err != nil {
			return nil, err
		}
	}
	return changedFiles, nil
}

func removeStaleNixOSRuntimeDnsmasqUnit(changedFiles *[]string) error {
	// Temporary migration cleanup for hosts that received the old imperative
	// runtime unit before dnsmasq moved into the generated NixOS module.
	// Remove after the first release containing generated routerd-dnsmasq
	// NixOS service ownership has completed one deployment cycle.
	path := "/run/systemd/system/" + routerdDnsmasqService
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if changedFiles != nil {
		*changedFiles = append(*changedFiles, path)
	}
	return runLogged("systemctl", "daemon-reload")
}

func restartNixOSDnsmasqService() error {
	_ = runLogged("systemctl", "reset-failed", routerdDnsmasqService)
	return runLogged("systemctl", "restart", routerdDnsmasqService)
}

func applyFreeBSDDnsmasqConfig(configPath, servicePath string, configData []byte, dnsmasqPath string) ([]string, error) {
	var changedFiles []string
	if err := os.MkdirAll(filepathDir(configPath), 0755); err != nil {
		return nil, fmt.Errorf("create directory for %s: %w", configPath, err)
	}
	leaseFile := dnsmasqLeaseFileForPlatform()
	if leaseFile != "" {
		if err := os.MkdirAll(filepathDir(leaseFile), 0755); err != nil {
			return nil, fmt.Errorf("create dnsmasq lease directory %s: %w", filepathDir(leaseFile), err)
		}
	}
	changed, err := writeFileIfChanged(configPath, configData, 0644)
	if err != nil {
		return nil, fmt.Errorf("write dnsmasq config %s: %w", configPath, err)
	}
	if changed {
		changedFiles = append(changedFiles, configPath)
	}
	out, err := exec.Command(dnsmasqPath, "--test", "--conf-file="+configPath).CombinedOutput()
	if err != nil {
		return changedFiles, fmt.Errorf("%s --test --conf-file=%s: %w: %s", dnsmasqPath, configPath, err, strings.TrimSpace(string(out)))
	}
	if err := os.MkdirAll(platformDefaults.RuntimeDir, 0755); err != nil {
		return changedFiles, fmt.Errorf("create runtime directory %s: %w", platformDefaults.RuntimeDir, err)
	}
	if servicePath == "" {
		return changedFiles, nil
	}
	if err := os.MkdirAll(filepathDir(servicePath), 0755); err != nil {
		return changedFiles, fmt.Errorf("create directory for %s: %w", servicePath, err)
	}
	serviceChanged, err := writeFileIfChanged(servicePath, render.DnsmasqRCScript(configPath, platformDefaults.RuntimeDir, filepathDir(leaseFile), dnsmasqPath), 0555)
	if err != nil {
		return changedFiles, fmt.Errorf("write dnsmasq rc.d script %s: %w", servicePath, err)
	}
	if serviceChanged {
		changedFiles = append(changedFiles, servicePath)
	}
	if err := runLogged("sysrc", "routerd_dnsmasq_enable=YES"); err != nil {
		return changedFiles, err
	}
	if len(changedFiles) > 0 || !freeBSDServiceRunning("routerd_dnsmasq") {
		if freeBSDServiceRunning("routerd_dnsmasq") {
			if err := runLogged("service", "routerd_dnsmasq", "restart"); err != nil {
				return changedFiles, err
			}
		} else {
			if err := runLogged("service", "routerd_dnsmasq", "start"); err != nil {
				return changedFiles, err
			}
		}
		changedFiles = append(changedFiles, "service:routerd_dnsmasq")
	}
	return changedFiles, nil
}

func dnsmasqLeaseFileForPlatform() string {
	if platformDefaults.OS == platform.OSFreeBSD {
		return strings.TrimRight(platformDefaults.StateDir, "/") + "/dnsmasq/dnsmasq.leases"
	}
	if platformFeatures.HasOpenRC {
		return strings.TrimRight(platformDefaults.StateDir, "/") + "/dnsmasq/dnsmasq.leases"
	}
	return ""
}

func dhcpStickyLogPath() string {
	return strings.TrimRight(platformDefaults.StateDir, "/") + "/dhcp-sticky.db"
}

func dhcpStickyHoldDays(router *api.Router, ip string) int {
	if router == nil {
		return 0
	}
	wantV6 := strings.Contains(strings.TrimSpace(ip), ":")
	holdDays := 0
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "DHCPv4Server":
			if wantV6 {
				continue
			}
			spec, err := res.DHCPv4ServerSpec()
			if err == nil && defaultString(spec.Server, "dnsmasq") == "dnsmasq" && spec.StickyHoldDays > holdDays {
				holdDays = spec.StickyHoldDays
			}
		case "DHCPv6Server":
			if !wantV6 {
				continue
			}
			spec, err := res.DHCPv6ServerSpec()
			if err == nil && defaultString(spec.Server, "dnsmasq") == "dnsmasq" && spec.StickyHoldDays > holdDays {
				holdDays = spec.StickyHoldDays
			}
		}
	}
	return holdDays
}

func dhcpStickyHostsFromLog(router *api.Router, now time.Time) []render.DHCPStickyHost {
	if dhcpStickyHoldDays(router, "192.0.2.1") == 0 && dhcpStickyHoldDays(router, "2001:db8::1") == 0 {
		return nil
	}
	path := dhcpStickyLogPath()
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	stickyLog, err := logstore.OpenDHCPStickyLogReadOnly(path)
	if err != nil {
		return nil
	}
	defer stickyLog.Close()
	rows, err := stickyLog.List(context.Background(), logstore.DHCPStickyFilter{HeldOnly: true, Now: now, Limit: 10000})
	if err != nil {
		return nil
	}
	out := make([]render.DHCPStickyHost, 0, len(rows))
	for _, row := range rows {
		out = append(out, render.DHCPStickyHost{
			MACAddress: row.MAC,
			IPAddress:  row.IP,
			Hostname:   row.Hostname,
			Family:     row.Family,
		})
	}
	return out
}

func applyFreeBSDIPv6DefaultRoutes(router *api.Router) ([]string, error) {
	if platformDefaults.OS != platform.OSFreeBSD {
		return nil, nil
	}
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
		if res.Kind != "DHCPv6Address" && res.Kind != "IPv6RAAddress" {
			continue
		}
		var iface string
		switch res.Kind {
		case "DHCPv6Address":
			spec, err := res.DHCPv6AddressSpec()
			if err != nil {
				return nil, err
			}
			iface = spec.Interface
		case "IPv6RAAddress":
			spec, err := res.IPv6RAAddressSpec()
			if err != nil {
				return nil, err
			}
			if !api.BoolDefault(spec.Managed, true) {
				continue
			}
			iface = spec.Interface
		}
		ifname := aliases[iface]
		if ifname == "" {
			return nil, fmt.Errorf("%s references interface with empty ifname", res.ID())
		}
		currentOut, currentErr := exec.Command("sysctl", "-n", "net.inet6.ip6.rfc6204w3").CombinedOutput()
		if currentErr != nil || strings.TrimSpace(string(currentOut)) != "1" {
			if err := runLogged("sysctl", "-w", "net.inet6.ip6.rfc6204w3=1"); err != nil {
				return applied, err
			}
			applied = append(applied, "sysctl:net.inet6.ip6.rfc6204w3")
		}
		if freeBSDHasIPv6DefaultRoute() {
			continue
		}
		if _, err := exec.LookPath("rtsol"); err != nil {
			continue
		}
		if err := runLogged("rtsol", ifname); err != nil {
			return applied, err
		}
		applied = append(applied, "rtsol:"+ifname)
	}
	return applied, nil
}

func freeBSDHasIPv6DefaultRoute() bool {
	out, err := exec.Command("netstat", "-rn", "-f", "inet6").CombinedOutput()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "default ") {
			return true
		}
	}
	return false
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

func applySystemdUnitResources(router *api.Router) ([]string, error) {
	_ = router
	return nil, nil
}

func applyOpenRCServiceResources(router *api.Router) ([]string, error) {
	if !platformFeatures.HasOpenRC {
		return nil, nil
	}
	if platformDefaults.OpenRCScriptDir == "" {
		return nil, errors.New("OpenRC script directory is not configured")
	}
	cfg, err := render.OpenRCWithOptions(router, render.OpenRCOptions{IncludeDnsmasq: false})
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(platformDefaults.OpenRCScriptDir, 0755); err != nil {
		return nil, fmt.Errorf("create OpenRC script directory %s: %w", platformDefaults.OpenRCScriptDir, err)
	}
	var changedFiles []string
	changedServices := map[string]bool{}
	enabledServices := openRCEnabledServices()
	for _, name := range sortedByteMapKeysMain(cfg.InitScripts) {
		path := filepath.Join(platformDefaults.OpenRCScriptDir, name)
		changed, err := writeFileIfChanged(path, cfg.InitScripts[name], 0755)
		if err != nil {
			return nil, fmt.Errorf("write OpenRC script %s: %w", path, err)
		}
		if changed {
			changedFiles = append(changedFiles, path)
			changedServices[name] = true
		}
	}
	for _, service := range cfg.Services {
		if service.Enabled {
			if !enabledServices[service.Name] {
				if err := runLogged("rc-update", "add", service.Name, "default"); err != nil {
					return nil, err
				}
				enabledServices[service.Name] = true
			}
		} else if enabledServices[service.Name] {
			if err := runLogged("rc-update", "del", service.Name, "default"); err != nil {
				return nil, err
			}
			delete(enabledServices, service.Name)
		}
		if service.Name == "keepalived" {
			continue
		}
		active := openRCServiceActive(service.Name)
		if service.Started {
			if service.Name == "routerd" && changedServices[service.Name] && active {
				continue
			}
			if changedServices[service.Name] && active {
				if err := runLogged("rc-service", service.Name, "restart"); err != nil {
					return nil, err
				}
				continue
			}
			if !active {
				if err := runLogged("rc-service", service.Name, "start"); err != nil {
					return nil, err
				}
			}
			continue
		}
		if service.Name == "routerd" {
			continue
		}
		if active {
			if err := runLogged("rc-service", service.Name, "stop"); err != nil {
				return nil, err
			}
		}
	}
	return changedFiles, nil
}

func openRCEnabledServices() map[string]bool {
	enabled := map[string]bool{}
	out, err := exec.Command("rc-update", "show", "default").CombinedOutput()
	if err != nil {
		return enabled
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			enabled[fields[0]] = true
		}
	}
	return enabled
}

func openRCServiceActive(name string) bool {
	return exec.Command("rc-service", name, "status").Run() == nil
}

func sortedByteMapKeysMain(values map[string][]byte) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func scheduleRouterdServiceRestartLogged() error {
	return runLogged(
		"systemd-run",
		"--unit", fmt.Sprintf("routerd-self-restart-%d-%d.service", os.Getpid(), time.Now().UnixNano()),
		"--description", "Restart routerd after managed unit update",
		"--on-active=10s",
		"--collect",
		"systemctl", "restart", "routerd.service",
	)
}

func mergeEnvironmentEntries(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	out := make([]string, 0, len(base)+len(extra))
	seen := map[string]int{}
	for _, value := range base {
		key := environmentEntryKey(value)
		seen[key] = len(out)
		out = append(out, value)
	}
	for _, value := range extra {
		key := environmentEntryKey(value)
		if idx, ok := seen[key]; ok {
			out[idx] = value
			continue
		}
		seen[key] = len(out)
		out = append(out, value)
	}
	return out
}

func environmentEntryKey(value string) string {
	if idx := strings.IndexByte(value, '='); idx > 0 {
		return value[:idx]
	}
	return value
}

func applyPPPoEConfig(router *api.Router) ([]string, error) {
	config, err := render.PPPoE(router, pppoePassword)
	if err != nil {
		return nil, err
	}
	if len(config.Files) == 0 && len(config.Secrets) == 0 {
		return nil, nil
	}
	if len(config.Units) > 0 && !pppdAvailable() {
		return nil, errors.New("pppd is required for managed PPPoE interfaces")
	}

	nixOS := isNixOSHost()
	managedUnits := map[string]bool{}
	for _, unit := range config.Units {
		managedUnits[unit] = true
	}
	for _, unit := range config.DisabledUnits {
		managedUnits[unit] = true
	}
	var changedFiles []string
	for _, file := range config.Files {
		if strings.HasPrefix(file.Path, "/etc/systemd/system/") && strings.HasSuffix(file.Path, ".service") && nixOS {
			unit := filepath.Base(file.Path)
			if !managedUnits[unit] {
				continue
			}
			file.Path = filepath.Join("/run/systemd/system", unit)
		}
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
	for _, unit := range config.DisabledUnits {
		if nixOS {
			if err := runLogged("systemctl", "stop", unit); err != nil {
				return nil, err
			}
			_ = runLogged("systemctl", "reset-failed", unit)
			continue
		}
		if err := runLogged("systemctl", "disable", "--now", unit); err != nil {
			return nil, err
		}
		_ = runLogged("systemctl", "reset-failed", unit)
	}
	for _, unit := range config.Units {
		if nixOS {
			if len(changedFiles) > 0 {
				if err := runLogged("systemctl", "restart", unit); err != nil {
					return nil, err
				}
				continue
			}
			if err := runLogged("systemctl", "is-active", "--quiet", unit); err != nil {
				if err := runLogged("systemctl", "start", unit); err != nil {
					return nil, err
				}
			}
			continue
		}
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

func pppoePassword(res api.Resource, spec api.PPPoESessionSpec) (string, error) {
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
		if (strings.HasPrefix(path, "/etc/systemd/system/") || strings.HasPrefix(path, "/run/systemd/system/")) && strings.HasSuffix(path, ".service") {
			return true
		}
	}
	return false
}
