// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestFreeBSDRCDSignalsOnlyItsRecordedSupervisorAndChild(t *testing.T) {
	data, err := FreeBSDRCDScript("fixture", api.SystemdUnitSpec{ExecStart: []string{"/usr/local/sbin/routerd-fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, line := range []string{". /etc/rc.subr\n", "load_rc_config $name\n", "run_rc_command \"$1\"\n"} {
		body = strings.ReplaceAll(body, line, "")
	}
	for _, scenario := range []string{"owned", "foreign-supervisor", "foreign-parent", "missing-pidfiles", "negative-pid"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			if scenario != "missing-pidfiles" {
				for name, pid := range map[string]string{"daemon": "123", "child": "124"} {
					if scenario == "negative-pid" {
						pid = "-1"
					}
					if err := os.WriteFile(filepath.Join(dir, name), []byte(pid+"\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			script := body + `
daemon_pidfile="$FIXTURE_DIR/daemon"
child_pidfile="$FIXTURE_DIR/child"
warn() { echo "$*" >&2; }
sleep() { :; }
kill() {
  if [ "$1" = -0 ]; then
    [ ! -f "$FIXTURE_DIR/gone" ] && { [ "$2" = 123 ] || [ "$2" = 124 ]; }
    return
  fi
  printf '%s\n' "$*" >> "$FIXTURE_DIR/signals"
  : > "$FIXTURE_DIR/gone"
}
ps() {
  case "$2" in
    comm=) echo daemon ;;
    command=)
      if [ "$SCENARIO" = foreign-supervisor ]; then echo 'daemon: somebody-else[124]'; else echo 'daemon: fixture[124]'; fi ;;
    ppid=)
      if [ "$SCENARIO" = foreign-parent ]; then echo 125; else echo 123; fi ;;
    *) echo '124 /usr/local/sbin/routerd-fixture' ;;
  esac
}
fixture_stop
`
			command := exec.Command("sh")
			command.Stdin = strings.NewReader(script)
			command.Env = append(os.Environ(), "FIXTURE_DIR="+dir, "SCENARIO="+scenario)
			output, runErr := command.CombinedOutput()
			signals, readErr := os.ReadFile(filepath.Join(dir, "signals"))
			if scenario == "owned" {
				if runErr != nil || string(signals) != "123\n124\n" {
					t.Fatalf("owned pair failed to stop: %v %v %s; signals=%q", runErr, readErr, output, signals)
				}
				return
			}
			if scenario == "foreign-parent" {
				// The title-bound supervisor is ours, while its recorded child
				// PID was reused. Only the supervisor may be signalled.
				if string(signals) != "123\n" {
					t.Fatalf("signalled a foreign child: %q (%v %s)", signals, runErr, output)
				}
				return
			}
			if len(signals) != 0 {
				t.Fatalf("signalled an unowned process in %s: %q (%v %s)", scenario, signals, runErr, output)
			}
			if scenario == "foreign-supervisor" && runErr == nil {
				t.Fatalf("live unknown child was falsely acknowledged as stopped: %s", output)
			}
		})
	}
}
