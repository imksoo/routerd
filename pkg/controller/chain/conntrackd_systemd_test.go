package chain

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/render"
)

func TestConntrackdUnitStartsDaemonWithoutOwningRouterdRuntimeDirectory(t *testing.T) {
	unit := string(render.SystemdUnit("routerd-conntrackd@test.service", conntrackdSystemdSpec("test", "/etc/conntrackd/routerd-test.conf")))
	for _, want := range []string{
		"ExecStartPre=/bin/rm -f /run/routerd/conntrackd.lock /run/routerd/conntrackd.ctl",
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
