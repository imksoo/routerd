// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestHealthCheckSystemdUnit(t *testing.T) {
	unitWithPre := string(SystemdUnit("routerd.service", api.SystemdUnitSpec{
		ExecStartPre: []string{"/usr/local/sbin/routerd", "apply", "--once"},
		ExecStart:    []string{"/usr/local/sbin/routerd", "serve"},
	}))
	if !strings.Contains(unitWithPre, "ExecStartPre=/usr/local/sbin/routerd apply --once") {
		t.Fatalf("unit missing ExecStartPre:\n%s", unitWithPre)
	}
	unit := string(HealthCheckSystemdUnit(HealthCheckSystemdOptions{
		BinaryPath:      "/usr/local/sbin/routerd-healthcheck",
		Resource:        "internet-icmp",
		Target:          "1.1.1.1",
		Protocol:        "icmp",
		SourceInterface: "ds-routerd-test",
		SourceAddress:   "192.0.2.10",
		Interval:        "30s",
		Timeout:         "3s",
		SocketPath:      "/run/routerd/healthcheck/internet-icmp.sock",
		StateFile:       "/var/lib/routerd/healthcheck/internet-icmp/state.json",
		EventFile:       "/var/lib/routerd/healthcheck/internet-icmp/events.jsonl",
	}))
	for _, want := range []string{
		"Description=routerd healthcheck internet-icmp",
		"ExecStart=/usr/local/sbin/routerd-healthcheck --resource \"internet-icmp\" --target \"1.1.1.1\" --protocol \"icmp\"",
		"--source-interface \"ds-routerd-test\"",
		"--source-address \"192.0.2.10\"",
		"--socket \"/run/routerd/healthcheck/internet-icmp.sock\"",
		"RuntimeDirectoryPreserve=yes",
		"ProtectSystem=strict",
		"CapabilityBoundingSet=CAP_NET_RAW",
		"AmbientCapabilities=CAP_NET_RAW",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
}
