package chain

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
)

func TestConntrackdUnitStartsDaemonWithoutOwningRouterdRuntimeDirectory(t *testing.T) {
	unit := string(render.SystemdUnit("routerd-conntrackd@test.service", api.SystemdUnitSpec{
		Type: "notify", ExecStart: []string{"/usr/sbin/conntrackd", "-C", "/etc/conntrackd/routerd-test.conf"},
		Conflicts: []string{"conntrackd.service"}, Restart: "on-failure", RestartSec: "2s",
	}))
	for _, want := range []string{
		"ExecStart=/usr/sbin/conntrackd -C /etc/conntrackd/routerd-test.conf",
		"Conflicts=conntrackd.service",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
	for _, notWant := range []string{" -n ", "RuntimeDirectory=routerd"} {
		if strings.Contains(unit, notWant) {
			t.Fatalf("unit must not contain %q:\n%s", notWant, unit)
		}
	}
}
