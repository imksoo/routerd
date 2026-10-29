// SPDX-License-Identifier: BSD-3-Clause

package install_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFreeBSDServiceRequiresNativePIDOwnershipBeforeSignals(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(repoRoot(t), "contrib", "freebsd", "routerd"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	body = strings.Replace(body, ". /etc/rc.subr", "", 1)
	body = strings.Replace(body, `load_rc_config "$name"`, "", 1)
	body = strings.Replace(body, `run_rc_command "$1"`, "", 1)
	for _, scenario := range []string{"absent", "reused", "wrong-source", "read-error", "unlocked", "owned"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			child, supervisor := filepath.Join(dir, "child.pid"), filepath.Join(dir, "supervisor.pid")
			for path, pid := range map[string]string{child: "111", supervisor: "222"} {
				if scenario != "absent" {
					if err := os.WriteFile(path, []byte(pid+"\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			script := filepath.Join(dir, "fixture.sh")
			tail := fmt.Sprintf(`
pidfile=%q
supervisor_pidfile=%q
warn() { echo "$*" >&2; }
ps() {
  [ "$SCENARIO" != read-error ] || return 1
  [ ! -e "$STOPPED" ] || return 0
  case "$SCENARIO" in
    reused) echo '111 sleep /bin/sleep 30'; echo '222 sleep /bin/sleep 30' ;;
    wrong-source) echo '111 routerd /usr/local/sbin/routerd serve --config /foreign/router.yaml'; echo '222 daemon daemon: routerd[111] (daemon)' ;;
    *) echo '111 routerd /usr/local/sbin/routerd serve --config /usr/local/etc/routerd/router.yaml --socket /var/run/routerd/routerd.sock --status-socket /var/run/routerd/routerd-status.sock --apply-interval 60s'; echo '222 daemon daemon: routerd[111] (daemon)' ;;
  esac
}
pgrep() {
  [ "$1 $2" = '-L -F' ] || return 99
  [ "$SCENARIO" != unlocked ] || return 3
  cat "$3"
}
pkill() {
  [ "$2 $3" = '-L -F' ] || return 98
  printf 'signal %%s\n' "$*" >> "$SIGNALS"
  : > "$STOPPED"
}
sleep() { :; }
routerd_stop
`, child, supervisor)
			if err := os.WriteFile(script, []byte(body+tail), 0700); err != nil {
				t.Fatal(err)
			}
			signals := filepath.Join(dir, "signals")
			cmd := exec.Command("sh", script)
			cmd.Env = append(os.Environ(), "SCENARIO="+scenario, "SIGNALS="+signals, "STOPPED="+filepath.Join(dir, "stopped"))
			out, err := cmd.CombinedOutput()
			wantOK := scenario == "absent" || scenario == "owned"
			if (err == nil) != wantOK {
				t.Fatalf("stop success=%v want=%v: %v\n%s", err == nil, wantOK, err, out)
			}
			got, _ := os.ReadFile(signals)
			if scenario == "owned" {
				if !strings.Contains(string(got), "-TERM -L -F "+supervisor+" -x daemon") {
					t.Fatalf("owned native supervisor not signalled: %s", got)
				}
				if _, err := os.Stat(child); !os.IsNotExist(err) {
					t.Fatalf("successful stop retained child pidfile: %v", err)
				}
			} else if len(got) != 0 {
				t.Fatalf("unproven native process signalled: %s", got)
			}
			if !wantOK {
				if _, err := os.Stat(child); err != nil {
					t.Fatalf("failed stop discarded recovery state: %v", err)
				}
			}
		})
	}
}

func TestUninstallStopFailureRetainsPayloadAndService(t *testing.T) {
	for _, tc := range []struct{ osName, tool, service, env string }{
		{"Linux", "systemctl", "routerd.service", "ROUTERD_UNINSTALL_SYSTEMD_SYSTEM_DIR"},
		{"FreeBSD", "service", "routerd", "ROUTERD_UNINSTALL_RCD_DIR"},
	} {
		t.Run(tc.osName, func(t *testing.T) {
			dir := t.TempDir()
			prefix := filepath.Join(dir, "prefix")
			services := filepath.Join(dir, "services")
			bin := filepath.Join(dir, "bin")
			payload := filepath.Join(prefix, "sbin", "routerd")
			writeExecutable(t, payload, "routerd payload\n")
			owned := filepath.Join(services, tc.service)
			writeExecutable(t, owned, "# routerd-managed-service: v1\nowned\n")
			writeExecutable(t, filepath.Join(bin, "uname"), "#!/bin/sh\necho "+tc.osName+"\n")
			writeExecutable(t, filepath.Join(bin, tc.tool), "#!/bin/sh\nexit 9\n")
			cmd := exec.Command(filepath.Join(repoRoot(t), "packaging", "uninstall.sh"), "--prefix", prefix, "--yes")
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "ROUTERD_UNINSTALL_FORCE_SERVICE_MANAGER=1", tc.env+"="+services)
			out, err := cmd.CombinedOutput()
			if err == nil || strings.Contains(string(out), "uninstall completed") {
				t.Fatalf("failed stop reported success: %v\n%s", err, out)
			}
			for _, path := range []string{payload, owned} {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("failed stop removed %s: %v", path, err)
				}
			}
		})
	}
}
