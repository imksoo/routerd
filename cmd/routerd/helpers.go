// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"routerd/pkg/api"
	"routerd/pkg/apply"
	"routerd/pkg/eventlog"
	"routerd/pkg/render"
	statuswriter "routerd/pkg/status"
)

func applyFiles(files []render.File) (changed, created []string, err error) {
	for _, file := range files {
		if mkErr := os.MkdirAll(filepathDir(file.Path), 0755); mkErr != nil {
			return nil, nil, fmt.Errorf("create directory for %s: %w", file.Path, mkErr)
		}
		existed := false
		if _, statErr := os.Stat(file.Path); statErr == nil {
			existed = true
		}
		didChange, writeErr := writeFileIfChanged(file.Path, file.Data, 0644)
		if writeErr != nil {
			return nil, nil, fmt.Errorf("write %s: %w", file.Path, writeErr)
		}
		if didChange {
			changed = append(changed, file.Path)
			if !existed {
				created = append(created, file.Path)
			}
		}
	}
	return changed, created, nil
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
		info, statErr := os.Stat(path)
		if statErr != nil {
			return false, statErr
		}
		if info.Mode().Perm() != perm {
			if err := os.Chmod(path, perm); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return false, err
	}
	if err := os.Chmod(path, perm); err != nil {
		return false, err
	}
	return true, nil
}

func hasNetworkdUnitFiles(paths []string) bool {
	for _, path := range paths {
		if strings.HasSuffix(path, ".netdev") || (strings.HasSuffix(path, ".network") && !strings.Contains(path, ".network.d/")) {
			return true
		}
	}
	return false
}

func hasNewNetdevFiles(paths []string) bool {
	for _, path := range paths {
		if strings.HasSuffix(path, ".netdev") {
			return true
		}
	}
	return false
}

func changedNetworkdInterfaces(paths []string) []string {
	var ifnames []string
	for _, path := range paths {
		if !strings.Contains(path, ".network.d/") {
			continue
		}
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

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func routerInterfaceAliases(resources []api.Resource) map[string]string {
	aliases := map[string]string{}
	for _, res := range resources {
		if res.Kind != "Interface" {
			continue
		}
		spec, err := res.InterfaceSpec()
		if err != nil {
			continue
		}
		aliases[res.Metadata.Name] = spec.IfName
	}
	return aliases
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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

func defaultStatusFile() string {
	return platformDefaults.StatusFile()
}

func defaultSocketPath() string {
	return platformDefaults.SocketFile()
}

func defaultStatusSocketPath() string {
	return platformDefaults.StatusSocketFile()
}

func writeResult(stdout io.Writer, statusFile string, result *apply.Result) error {
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
