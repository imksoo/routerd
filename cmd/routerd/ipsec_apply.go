// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/ipsec"
	"github.com/imksoo/routerd/pkg/platform"
)

type ipsecRuntimeApplyOptions struct {
	ConfigDir       string
	ConfigFile      string
	LegacyConfigDir string
	Load            func(context.Context) error
}

const ipsecPendingLoadMarker = ".routerd-pending-load"

const freeBSDStrongSwanServiceTimeout = 30 * time.Second
const freeBSDStrongSwanProbeTimeout = 2 * time.Second

func applyIPsecConnections(ctx context.Context, router *api.Router) ([]string, error) {
	configDir := ipsecConfigDir()
	configFile := filepath.Join(configDir, "routerd.conf")
	return applyIPsecConnectionsWithOptions(ctx, router, ipsecRuntimeApplyOptions{
		ConfigDir:       configDir,
		ConfigFile:      configFile,
		LegacyConfigDir: ipsecLegacyConfigDir(),
		Load: func(ctx context.Context) error {
			if err := ensureFreeBSDStrongSwan(ctx); err != nil {
				return err
			}
			loadCtx, cancel := context.WithTimeout(ctx, freeBSDStrongSwanServiceTimeout)
			defer cancel()
			return (ipsec.Controller{Binary: ipsecSwanctlPath(), ConfigFile: configFile}).LoadAll(loadCtx)
		},
	})
}

func ensureFreeBSDStrongSwan(ctx context.Context) error {
	if platformDefaults.OS != platform.OSFreeBSD {
		return nil
	}
	serviceCtx, cancel := context.WithTimeout(ctx, freeBSDStrongSwanServiceTimeout)
	defer cancel()
	if out, err := exec.CommandContext(serviceCtx, "sysrc", "strongswan_enable=YES").CombinedOutput(); err != nil {
		return fmt.Errorf("enable strongswan service: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if err := exec.CommandContext(serviceCtx, "service", "strongswan", "status").Run(); err == nil {
		return nil
	}
	// The packaged rc.d vici start hook unconditionally runs its own
	// package-default swanctl --load-all. routerd owns a separate aggregate,
	// so launch the same daemon supervisor directly and wait for VICI before
	// routerd performs its authoritative --load-all --file transaction. daemon
	// is itself long-lived, so Start+Release is required instead of Run.
	startupOutput, err := startCommandWithFileOutput("/usr/sbin/daemon", "-S", "-P", "/var/run/daemon-charon.pid", "/usr/local/libexec/ipsec/charon", "--use-syslog")
	if err != nil {
		return fmt.Errorf("start strongswan supervisor: %w", err)
	}
	if err := waitForFreeBSDStrongSwanVICI(serviceCtx); err != nil {
		startupText := readAndRemoveCommandOutput(startupOutput)
		stopErr := stopFreeBSDStrongSwanSupervisor()
		return fmt.Errorf("start strongswan service: %w: startup=%s cleanup=%v", err, strings.TrimSpace(startupText), stopErr)
	}
	_ = os.Remove(startupOutput)
	return nil
}

func waitForFreeBSDStrongSwanVICI(ctx context.Context) error {
	var lastErr error
	for {
		probeCtx, cancel := context.WithTimeout(ctx, freeBSDStrongSwanProbeTimeout)
		err := exec.CommandContext(probeCtx, ipsecSwanctlPath(), "--stats").Run()
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for strongswan VICI: %w: %v", ctx.Err(), lastErr)
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func stopFreeBSDStrongSwanSupervisor() error {
	ctx, cancel := context.WithTimeout(context.Background(), freeBSDStrongSwanServiceTimeout)
	defer cancel()
	out, err := runCommandWithFileOutput(ctx, "service", "strongswan", "onestop")
	if err != nil {
		return fmt.Errorf("stop strongswan supervisor: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runCommandWithFileOutput avoids exec.Cmd's pipe-drain wait when an rc.d
// script launches a daemon that inherits stdout or stderr. A regular file can
// remain open in that daemon without preventing Run from observing the rc.d
// process exit.
func runCommandWithFileOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	output, err := os.CreateTemp("", "routerd-command-*.log")
	if err != nil {
		return nil, fmt.Errorf("create command output file: %w", err)
	}
	path := output.Name()
	defer os.Remove(path)

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = output
	cmd.Stderr = output
	runErr := cmd.Run()
	closeErr := output.Close()
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return nil, fmt.Errorf("read command output: %w", readErr)
	}
	if runErr != nil {
		return data, runErr
	}
	if closeErr != nil {
		return data, fmt.Errorf("close command output: %w", closeErr)
	}
	return data, nil
}

// startCommandWithFileOutput starts a long-lived supervisor without binding
// its lifetime to an apply request. Its regular output file is safe for a
// daemon child to inherit; the returned path is removed by the caller.
func startCommandWithFileOutput(name string, args ...string) (string, error) {
	output, err := os.CreateTemp("", "routerd-command-*.log")
	if err != nil {
		return "", fmt.Errorf("create command output file: %w", err)
	}
	path := output.Name()
	cmd := exec.Command(name, args...)
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		_ = output.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := output.Close(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = os.Remove(path)
		return "", fmt.Errorf("close command output: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("release command process: %w", err)
	}
	return path, nil
}

func readAndRemoveCommandOutput(path string) string {
	data, _ := os.ReadFile(path)
	_ = os.Remove(path)
	return string(data)
}

func applyIPsecConnectionsWithOptions(ctx context.Context, router *api.Router, opts ipsecRuntimeApplyOptions) ([]string, error) {
	if router == nil {
		return nil, nil
	}
	dir := strings.TrimSpace(opts.ConfigDir)
	if dir == "" {
		return nil, fmt.Errorf("IPsec swanctl configuration directory is required")
	}
	configFile := strings.TrimSpace(opts.ConfigFile)
	if configFile == "" {
		configFile = filepath.Join(dir, "routerd.conf")
	}
	if filepath.Dir(configFile) != dir || filepath.Base(configFile) != "routerd.conf" {
		return nil, fmt.Errorf("IPsec swanctl aggregate configuration must be %s", filepath.Join(dir, "routerd.conf"))
	}
	legacyDir := strings.TrimSpace(opts.LegacyConfigDir)
	if legacyDir != "" && filepath.Clean(legacyDir) == filepath.Clean(dir) {
		legacyDir = ""
	}
	desired := map[string][]byte{}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "IPsecConnection" {
			continue
		}
		spec, err := resource.IPsecConnectionSpec()
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(resource.Metadata.Name)
		if name == "" || filepath.Base(name) != name {
			return nil, fmt.Errorf("invalid IPsec connection name %q", resource.Metadata.Name)
		}
		data, err := ipsec.RenderSwanctl(name, spec)
		if err != nil {
			return nil, fmt.Errorf("render IPsec connection %s: %w", resource.ID(), err)
		}
		desired["routerd-"+name+".conf"] = data
	}

	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read IPsec swanctl directory %s: %w", dir, err)
	}
	var legacyEntries []os.DirEntry
	if legacyDir != "" {
		legacyEntries, err = os.ReadDir(legacyDir)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read legacy IPsec swanctl directory %s: %w", legacyDir, err)
		}
	}
	pending, err := ipsecPendingLoad(dir)
	if err != nil {
		return nil, err
	}
	hasManaged := false
	for _, entry := range entries {
		if isRouterdIPsecRuntimeFile(entry.Name()) {
			hasManaged = true
			break
		}
	}
	hasLegacyManaged := false
	for _, entry := range legacyEntries {
		if isLegacyRouterdIPsecRuntimeFile(entry.Name()) {
			hasLegacyManaged = true
			break
		}
	}
	shouldLoad := len(desired) > 0 || pending || hasManaged || hasLegacyManaged
	if shouldLoad {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, fmt.Errorf("create IPsec swanctl directory %s: %w", dir, err)
		}
		if err := writeIPsecPendingLoadMarker(dir); err != nil {
			return nil, err
		}
	}

	var changed []string
	for _, name := range sortedIPsecConfigNames(desired) {
		path := filepath.Join(dir, name)
		fileChanged, err := writeFileIfChanged(path, desired[name], 0600)
		if err != nil {
			return changed, fmt.Errorf("write IPsec swanctl configuration %s: %w", path, err)
		}
		if fileChanged {
			changed = append(changed, path)
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		if !isManagedIPsecConnectionConfig(name) || desired[name] != nil {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return changed, fmt.Errorf("refuse to remove symlinked IPsec swanctl configuration %s", filepath.Join(dir, name))
		}
		info, err := entry.Info()
		if err != nil {
			return changed, fmt.Errorf("inspect stale IPsec swanctl configuration %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return changed, fmt.Errorf("refuse to remove non-regular IPsec swanctl configuration %s", filepath.Join(dir, name))
		}
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil {
			return changed, fmt.Errorf("remove stale IPsec swanctl configuration %s: %w", path, err)
		}
		changed = append(changed, "removed:"+path)
	}
	for _, entry := range legacyEntries {
		name := entry.Name()
		if !isLegacyRouterdIPsecRuntimeFile(name) {
			continue
		}
		path := filepath.Join(legacyDir, name)
		if err := removeRouterdIPsecRuntimeFile(path); err != nil {
			return changed, fmt.Errorf("remove legacy IPsec swanctl configuration %s: %w", path, err)
		}
		changed = append(changed, "removed:"+path)
	}
	if !shouldLoad {
		return nil, nil
	}
	aggregate, err := renderIPsecAggregate(sortedIPsecConfigNames(desired), dir)
	if err != nil {
		return changed, err
	}
	if fileChanged, err := writeFileIfChanged(configFile, aggregate, 0600); err != nil {
		return changed, fmt.Errorf("write IPsec swanctl aggregate configuration %s: %w", configFile, err)
	} else if fileChanged {
		changed = append(changed, configFile)
	}
	if opts.Load == nil {
		return changed, fmt.Errorf("load IPsec swanctl configuration: no loader configured")
	}
	if err := opts.Load(ctx); err != nil {
		return changed, fmt.Errorf("load IPsec swanctl configuration: %w", err)
	}
	if len(desired) == 0 {
		if err := os.Remove(configFile); err != nil && !os.IsNotExist(err) {
			return changed, fmt.Errorf("remove empty IPsec swanctl aggregate configuration %s: %w", configFile, err)
		}
	}
	// Keep the durable pending marker until teardown has removed the temporary
	// empty aggregate.  A failed removal or crash must force the next apply to
	// rerun the complete load/cleanup transaction instead of stranding a stale
	// routerd.conf with no retry signal.
	if err := os.Remove(filepath.Join(dir, ipsecPendingLoadMarker)); err != nil && !os.IsNotExist(err) {
		return changed, fmt.Errorf("remove IPsec pending load marker: %w", err)
	}
	return changed, nil
}

func isManagedIPsecConnectionConfig(name string) bool {
	return strings.HasPrefix(name, "routerd-") && strings.HasSuffix(name, ".conf")
}

func isRouterdIPsecRuntimeFile(name string) bool {
	return isManagedIPsecConnectionConfig(name) || name == "routerd.conf" || name == ipsecPendingLoadMarker
}

func isLegacyRouterdIPsecRuntimeFile(name string) bool {
	return isRouterdIPsecRuntimeFile(name)
}

func removeRouterdIPsecRuntimeFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refuse to remove non-regular routerd IPsec runtime file")
	}
	return os.Remove(path)
}

func renderIPsecAggregate(names []string, dir string) ([]byte, error) {
	var b strings.Builder
	for _, name := range names {
		if !isManagedIPsecConnectionConfig(name) {
			return nil, fmt.Errorf("invalid managed IPsec swanctl configuration name %q", name)
		}
		fmt.Fprintf(&b, "include %s\n", filepath.Join(dir, name))
	}
	return []byte(b.String()), nil
}

func ipsecPendingLoad(dir string) (bool, error) {
	path := filepath.Join(dir, ipsecPendingLoadMarker)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect IPsec pending load marker: %w", err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("refuse non-regular IPsec pending load marker %s", path)
	}
	return true, nil
}

func writeIPsecPendingLoadMarker(dir string) error {
	path := filepath.Join(dir, ipsecPendingLoadMarker)
	if pending, err := ipsecPendingLoad(dir); err != nil {
		return err
	} else if pending {
		return nil
	}
	if err := os.WriteFile(path, []byte("pending\n"), 0600); err != nil {
		return fmt.Errorf("write IPsec pending load marker: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("chmod IPsec pending load marker: %w", err)
	}
	return nil
}

func sortedIPsecConfigNames(values map[string][]byte) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func ipsecConfigDir() string {
	if platformDefaults.OS == platform.OSFreeBSD {
		return "/usr/local/etc/routerd/swanctl"
	}
	return "/etc/routerd/swanctl"
}

func ipsecLegacyConfigDir() string {
	if platformDefaults.OS == platform.OSFreeBSD {
		return "/usr/local/etc/swanctl/conf.d"
	}
	return "/etc/swanctl/conf.d"
}

func ipsecSwanctlPath() string {
	if platformDefaults.OS == platform.OSFreeBSD {
		return "/usr/local/sbin/swanctl"
	}
	return "swanctl"
}
