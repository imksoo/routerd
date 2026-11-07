//go:build linux

// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestSystemdDnsmasqProcessUsesVerifiedUnitWhenExecutableReadIsDenied(t *testing.T) {
	binary := buildDnsmasqProcessFixture(t)
	spec, _, _ := dnsmasqSystemdFixture(t, binary)
	if err := ensureSystemdDnsmasqService(t.Context(), filepath.Dir(spec.Config), binary, spec.Config, spec.PIDFile, false); err != nil {
		t.Fatal(err)
	}
	pid := dnsmasqUnitFixturePID(t, spec)
	previous := readNativeDnsmasqExecutable
	readNativeDnsmasqExecutable = func(path string) (string, error) {
		if path == filepath.Join("/proc", strconv.Itoa(pid), "exe") {
			return "", os.ErrPermission
		}
		return previous(path)
	}
	t.Cleanup(func() { readNativeDnsmasqExecutable = previous })
	if process, err := systemdDnsmasqProcess(t.Context(), spec); err != nil || process.PID != pid {
		t.Fatalf("verified systemd process = %+v, %v", process, err)
	}
	if _, err := readNativeDnsmasqProcess(t.Context(), pid); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("generic native observer accepted unreadable executable: %v", err)
	}
}
