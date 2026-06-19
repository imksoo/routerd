// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"
)

func TestEventdSystemdSpec(t *testing.T) {
	spec := EventdSystemdSpec("edge", "/usr/local/sbin/routerd-eventd", "/var/lib/routerd/eventd/edge/config.json")
	unit := string(SystemdUnit("routerd-eventd@edge.service", spec))
	for _, want := range []string{
		"Description=routerd event federation edge",
		"ExecStart=/usr/local/sbin/routerd-eventd daemon",
		"--config-file /var/lib/routerd/eventd/edge/config.json",
		"Restart=always",
		"AmbientCapabilities=CAP_NET_BIND_SERVICE",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
	for _, notWant := range []string{"RestrictAddressFamilies=", "ProtectSystem=", "ProtectHome=", "PrivateTmp=", "ReadWritePaths="} {
		if strings.Contains(unit, notWant) {
			t.Fatalf("unit must not contain %q:\n%s", notWant, unit)
		}
	}
}

func TestEventdSystemdSpecDefaults(t *testing.T) {
	spec := EventdSystemdSpec("edge", "", "")
	if got := spec.ExecStart[0]; got != "/usr/local/sbin/routerd-eventd" {
		t.Fatalf("default binary path = %q", got)
	}
	if spec.ExecStart[3] != "/var/lib/routerd/eventd/edge/config.json" {
		t.Fatalf("default config path = %q", spec.ExecStart[3])
	}
}
