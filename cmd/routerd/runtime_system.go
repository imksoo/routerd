// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/sysctlprofile"
)

func applyRuntimeSysctls(router *api.Router) ([]string, error) {
	var applied []string
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Sysctl":
			spec, err := res.SysctlSpec()
			if err != nil {
				return nil, err
			}
			changed, err := applyRuntimeSysctl(spec.Key, spec.Value, api.BoolDefault(spec.Runtime, true), spec.Optional)
			if err != nil {
				return nil, err
			}
			if changed {
				applied = append(applied, spec.Key)
			}
		case "SysctlProfile":
			spec, err := res.SysctlProfileSpec()
			if err != nil {
				return nil, err
			}
			if !api.BoolDefault(spec.Runtime, true) {
				continue
			}
			entries, err := sysctlprofile.Entries(spec.Profile, spec.Overrides)
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				changed, err := applyRuntimeSysctl(entry.Key, entry.Value, true, entry.Optional)
				if err != nil {
					return nil, err
				}
				if changed {
					applied = append(applied, entry.Key)
				}
			}
		default:
			continue
		}
	}
	return applied, nil
}

func applyRuntimeSysctl(key, value string, runtime bool, optional bool) (bool, error) {
	if !runtime {
		return false, nil
	}
	currentOut, err := exec.Command("sysctl", "-n", key).CombinedOutput()
	if err == nil && strings.TrimSpace(string(currentOut)) == value {
		return false, nil
	}
	if err := runLogged("sysctl", "-w", key+"="+value); err != nil {
		if optional {
			return false, nil
		}
		return false, err
	}
	return true, nil
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
	return platform.IsNixOSHost()
}

func runtimeDnsmasqServicePath(path string) string {
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

func applyNetworkConfig(netplanPath string, netplanData []byte, networkdFiles []render.File) ([]string, error) {
	changedNetworkdFiles, createdNetworkdFiles, err := applyFiles(networkdFiles)
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
	} else if hasNewNetdevFiles(createdNetworkdFiles) {
		if err := runLogged("systemctl", "restart", "systemd-networkd"); err != nil {
			return nil, err
		}
	} else {
		if hasNetworkdUnitFiles(changedNetworkdFiles) {
			if err := runLogged("networkctl", "reload"); err != nil {
				return nil, err
			}
		}
		for _, ifname := range changedNetworkdInterfaces(changedNetworkdFiles) {
			if err := runLogged("networkctl", "reconfigure", ifname); err != nil {
				return nil, err
			}
		}
	}
	return changedFiles, nil
}

func applyRuntimeLinuxNetworkResources(router *api.Router) ([]string, error) {
	if !platformFeatures.HasIproute2 {
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
	var changed []string
	for _, res := range router.Spec.Resources {
		if res.Kind != "Interface" {
			continue
		}
		spec, err := res.InterfaceSpec()
		if err != nil {
			return changed, err
		}
		if !spec.Managed || spec.Owner == "external" || !spec.AdminUp || strings.TrimSpace(spec.IfName) == "" {
			continue
		}
		if !linuxLinkIsUp(spec.IfName) {
			if err := runLogged("ip", "link", "set", "dev", spec.IfName, "up"); err != nil {
				return changed, err
			}
			changed = append(changed, "link:"+spec.IfName)
		}
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv4StaticAddress" {
			continue
		}
		spec, err := res.IPv4StaticAddressSpec()
		if err != nil {
			return changed, err
		}
		ifname := aliases[spec.Interface]
		if strings.TrimSpace(ifname) == "" || strings.TrimSpace(spec.Address) == "" {
			continue
		}
		if linuxIPv4AddressPresent(ifname, spec.Address) {
			continue
		}
		if err := runLogged("ip", "-4", "addr", "add", spec.Address, "dev", ifname); err != nil {
			return changed, err
		}
		changed = append(changed, "addr:"+ifname+":"+spec.Address)
	}
	return changed, nil
}

func linuxLinkIsUp(ifname string) bool {
	out, err := exec.Command("ip", "-o", "link", "show", "dev", ifname).CombinedOutput()
	return err == nil && strings.Contains(string(out), "UP")
}

func linuxIPv4AddressPresent(ifname, address string) bool {
	want := strings.TrimSpace(address)
	if host, _, ok := strings.Cut(want, "/"); ok {
		want = host
	}
	out, err := exec.Command("ip", "-4", "-o", "addr", "show", "dev", ifname).CombinedOutput()
	if err != nil {
		return false
	}
	for _, field := range strings.Fields(string(out)) {
		got := field
		if host, _, ok := strings.Cut(got, "/"); ok {
			got = host
		}
		if got == want {
			return true
		}
	}
	return false
}

func applyLinuxPackages(router *api.Router) ([]string, error) {
	if platformDefaults.OS != platform.OSLinux || isNixOSHost() {
		return nil, nil
	}
	osName := linuxPackageOSName()
	if osName == "" {
		return nil, nil
	}
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
		set, ok := packageSetForOSMain(spec, osName)
		if !ok {
			continue
		}
		manager := defaultString(set.Manager, defaultLinuxPackageManager(osName))
		if manager != "apt" && manager != "apk" {
			continue
		}
		for _, name := range set.Names {
			name = strings.TrimSpace(name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			switch manager {
			case "apt":
				out, err := exec.Command("dpkg-query", "-W", "-f=${Status}", name).CombinedOutput()
				if err != nil || strings.TrimSpace(string(out)) != "install ok installed" {
					missing = append(missing, name)
				}
			case "apk":
				if _, err := exec.Command("apk", "info", "-e", name).CombinedOutput(); err != nil {
					missing = append(missing, name)
				}
			}
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}
	manager := defaultLinuxPackageManager(osName)
	switch manager {
	case "apt":
		args := append([]string{"install", "-y"}, missing...)
		if err := runLogged("apt-get", args...); err != nil {
			return nil, err
		}
	case "apk":
		args := append([]string{"add", "--no-cache"}, missing...)
		if err := runLogged("apk", args...); err != nil {
			return nil, err
		}
	default:
		return nil, nil
	}
	out := make([]string, 0, len(missing))
	for _, name := range missing {
		out = append(out, manager+":"+name)
	}
	return out, nil
}

func linuxPackageOSName() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "ubuntu"
	}
	return linuxPackageOSNameFrom(string(data))
}

func linuxPackageOSNameFrom(text string) string {
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || key != "ID" {
			continue
		}
		value = strings.Trim(value, "\"'")
		switch value {
		case "ubuntu", "debian", "alpine":
			return value
		}
	}
	if strings.Contains(text, "ID_LIKE=debian") || strings.Contains(text, "ID_LIKE=\"debian\"") {
		return "ubuntu"
	}
	return ""
}

func defaultLinuxPackageManager(osName string) string {
	switch osName {
	case "ubuntu", "debian":
		return "apt"
	case "alpine":
		return "apk"
	default:
		return ""
	}
}

func packageSetForOSMain(spec api.PackageSpec, osName string) (api.OSPackageSetSpec, bool) {
	for _, set := range spec.Packages {
		if set.OS == osName {
			return set, true
		}
	}
	return api.OSPackageSetSpec{}, false
}

func applyNftablesConfig(path string, data []byte) ([]string, error) {
	managedTables := []struct {
		family string
		name   string
		header string
	}{
		{family: "inet", name: "routerd_filter", header: "table inet routerd_filter"},
		{family: "inet", name: "routerd_mss", header: "table inet routerd_mss"},
		{family: "ip", name: "routerd_forcefrag", header: "table ip routerd_forcefrag"},
		{family: "bridge", name: "routerd_l2_filter", header: "table bridge routerd_l2_filter"},
		{family: "ip", name: "routerd_dnat", header: "table ip routerd_dnat"},
		{family: "ip", name: "routerd_nat", header: "table ip routerd_nat"},
		{family: "ip6", name: "routerd_nat", header: "table ip6 routerd_nat"},
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
			existingTables[table.family+"/"+table.name] = true
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
	nat6Missing := bytes.Contains(data, []byte("table ip6 routerd_nat")) && exec.Command("nft", "list", "table", "ip6", "routerd_nat").Run() != nil
	policyMissing := bytes.Contains(data, []byte("table ip routerd_policy")) && exec.Command("nft", "list", "table", "ip", "routerd_policy").Run() != nil
	filterMissing := bytes.Contains(data, []byte("table inet routerd_filter")) && exec.Command("nft", "list", "table", "inet", "routerd_filter").Run() != nil
	mssMissing := bytes.Contains(data, []byte("table inet routerd_mss")) && exec.Command("nft", "list", "table", "inet", "routerd_mss").Run() != nil
	l2FilterMissing := bytes.Contains(data, []byte("table bridge routerd_l2_filter")) && exec.Command("nft", "list", "table", "bridge", "routerd_l2_filter").Run() != nil
	dnatMissing := bytes.Contains(data, []byte("table ip routerd_dnat")) && exec.Command("nft", "list", "table", "ip", "routerd_dnat").Run() != nil
	staleManaged := false
	for _, table := range managedTables {
		if existingTables[table.family+"/"+table.name] && !bytes.Contains(data, []byte(table.header)) {
			staleManaged = true
			break
		}
	}
	if !changed && !natMissing && !nat6Missing && !policyMissing && !filterMissing && !mssMissing && !l2FilterMissing && !dnatMissing && !staleManaged {
		return nil, nil
	}
	applyPath := path
	var cleanupPath string
	if staleManaged {
		var staged bytes.Buffer
		for _, table := range managedTables {
			if existingTables[table.family+"/"+table.name] && !bytes.Contains(data, []byte(table.header)) {
				staged.WriteString("delete table " + table.family + " " + table.name + "\n")
			}
		}
		staged.Write(data)
		file, err := os.CreateTemp(filepathDir(path), ".routerd-nft-*.nft")
		if err != nil {
			return nil, fmt.Errorf("create temporary nftables config: %w", err)
		}
		cleanupPath = file.Name()
		if _, err := file.Write(staged.Bytes()); err != nil {
			_ = file.Close()
			_ = os.Remove(cleanupPath)
			return nil, fmt.Errorf("write temporary nftables config %s: %w", cleanupPath, err)
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(cleanupPath)
			return nil, fmt.Errorf("close temporary nftables config %s: %w", cleanupPath, err)
		}
		defer os.Remove(cleanupPath)
		if err := runLogged("nft", "-c", "-f", cleanupPath); err != nil {
			return nil, fmt.Errorf("validate nftables config %s: %w", cleanupPath, err)
		}
		applyPath = cleanupPath
	}
	if err := runLogged("nft", "-f", applyPath); err != nil {
		return nil, err
	}
	if changed {
		return []string{path}, nil
	}
	return []string{"nftables:routerd"}, nil
}
