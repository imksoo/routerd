// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/ipsec"
	"github.com/imksoo/routerd/pkg/platform"
)

type ipsecRuntimeApplyOptions struct {
	ConfigDir string
	Load      func(context.Context) error
}

const ipsecPendingLoadMarker = ".routerd-pending-load"

func applyIPsecConnections(ctx context.Context, router *api.Router) ([]string, error) {
	return applyIPsecConnectionsWithOptions(ctx, router, ipsecRuntimeApplyOptions{
		ConfigDir: ipsecConfigDir(),
		Load: func(ctx context.Context) error {
			return (ipsec.Controller{Binary: ipsecSwanctlPath()}).LoadAll(ctx)
		},
	})
}

func applyIPsecConnectionsWithOptions(ctx context.Context, router *api.Router, opts ipsecRuntimeApplyOptions) ([]string, error) {
	if router == nil {
		return nil, nil
	}
	dir := strings.TrimSpace(opts.ConfigDir)
	if dir == "" {
		return nil, fmt.Errorf("IPsec swanctl configuration directory is required")
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
	pending, err := ipsecPendingLoad(dir)
	if err != nil {
		return nil, err
	}
	hasManaged := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "routerd-") && strings.HasSuffix(entry.Name(), ".conf") {
			hasManaged = true
			break
		}
	}
	shouldLoad := len(desired) > 0 || pending || hasManaged
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
		if !strings.HasPrefix(name, "routerd-") || !strings.HasSuffix(name, ".conf") || desired[name] != nil {
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
	if !shouldLoad {
		return nil, nil
	}
	if opts.Load == nil {
		return changed, fmt.Errorf("load IPsec swanctl configuration: no loader configured")
	}
	if err := opts.Load(ctx); err != nil {
		return changed, fmt.Errorf("load IPsec swanctl configuration: %w", err)
	}
	if err := os.Remove(filepath.Join(dir, ipsecPendingLoadMarker)); err != nil && !os.IsNotExist(err) {
		return changed, fmt.Errorf("remove IPsec pending load marker: %w", err)
	}
	return changed, nil
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
