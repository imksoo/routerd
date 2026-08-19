// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRouterdServiceSystemdSpecDoesNotConstrainWritePaths(t *testing.T) {
	unit := string(SystemdUnit(RouterdUnitName, RouterdServiceSystemdSpec()))
	for _, notWant := range []string{"ExecStartPre=/usr/local/sbin/routerd check", "ProtectSystem=", "ProtectHome=", "PrivateTmp=", "ReadWritePaths=", "RestrictAddressFamilies="} {
		if strings.Contains(unit, notWant) {
			t.Fatalf("routerd.service must not contain %q:\n%s", notWant, unit)
		}
	}
	for _, want := range []string{
		"ExecStart=/usr/local/sbin/routerd serve",
		"RuntimeDirectory=routerd routerd/bgp routerd/dhcpv6-client routerd/dhcpv4-client routerd/pppoe-client routerd/dns-resolver",
		"StateDirectory=routerd",
		"AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE CAP_SETUID CAP_SETGID CAP_CHOWN",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("routerd.service missing %q:\n%s", want, unit)
		}
	}
}

func TestPackagedRouterdSystemdUnitMatchesRuntimeRenderer(t *testing.T) {
	want := string(SystemdUnit(RouterdUnitName, RouterdServiceSystemdSpec()))
	path := filepath.Join("..", "..", "contrib", "systemd", "routerd.service")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read packaged routerd.service: %v", err)
	}
	if string(got) != want {
		t.Fatalf("packaged routerd.service differs from the runtime renderer:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
