// SPDX-License-Identifier: BSD-3-Clause

package hostcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var defaultSearchDirs = []string{
	"/run/current-system/sw/bin",
	"/run/current-system/sw/sbin",
	"/usr/local/bin",
	"/usr/local/sbin",
	"/usr/bin",
	"/usr/sbin",
	"/bin",
	"/sbin",
}

// Resolve returns an executable path for host commands that may live outside
// the service PATH, such as commands installed through package profiles.
func Resolve(name string, extraDirs ...string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	if strings.Contains(name, "/") {
		return name
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	for _, dir := range append(extraDirs, defaultSearchDirs...) {
		if dir = strings.TrimSpace(dir); dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if isExecutable(candidate) {
			return candidate
		}
	}
	return name
}

func ResolveConntrack(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "conntrack"
	}
	return Resolve(name)
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0111 != 0
}
