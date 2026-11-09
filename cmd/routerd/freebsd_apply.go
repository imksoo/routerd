// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/imksoo/routerd/internal/mapsort"
	"github.com/imksoo/routerd/pkg/api"
)

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
	for _, name := range mapsort.Keys(scripts) {
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
