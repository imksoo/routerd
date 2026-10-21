// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
	"strings"
	"testing"
)

func TestBGPGeneratedUnitUsesControllerPaths(t *testing.T) {
	defaults := platform.Defaults{RuntimeDir: "/run/routerd", StateDir: "/var/lib/routerd"}
	c := SystemdUnitController{BGPSocketPath: "/custom/run/bgp.sock", BGPControlSocketPath: "/custom/run/control.sock", BGPStatePath: "/custom/state/applied.json"}
	unit := render.SystemdUnit(render.BGPUnitName, c.bgpSystemdSpec(defaults))
	for _, want := range []string{"--socket /custom/run/bgp.sock", "--control-socket /custom/run/control.sock", "--state-file /custom/state/applied.json"} {
		if !strings.Contains(string(unit), want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
}
