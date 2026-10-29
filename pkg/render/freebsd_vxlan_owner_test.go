// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestFreeBSDVXLANFailedApplyRetainsOwnerAndCanRestart(t *testing.T) {
	data := FreeBSDVXLANRCDScript("fixture", vxlanConfig{IfName: "vx10", VNI: 10, LocalAddress: "192.0.2.1", Remotes: []string{"192.0.2.2"}, UDPPort: 4789}, nil)
	dir := t.TempDir()
	body := string(data)
	for _, line := range []string{". /etc/rc.subr\n", "load_rc_config $name\n", "run_rc_command \"$1\"\n"} {
		body = strings.ReplaceAll(body, line, "")
	}
	body = strings.ReplaceAll(body, "/var/run/routerd", dir)
	body = strings.ReplaceAll(body, "/sbin/ifconfig", "ifconfig")
	script := body + `
ifconfig() {
  printf '%s\n' "$*" >> "$FIXTURE_DIR/commands"
  if [ "$1" = -l ]; then
    [ ! -f "$FIXTURE_DIR/read-error" ] || return 1
    [ ! -f "$FIXTURE_DIR/native" ] || echo vx10
    return 0
  fi
  case "$2" in
    create) [ -r "$marker" ] || exit 30; : > "$FIXTURE_DIR/native" ;;
    destroy)
      [ -f "$FIXTURE_DIR/retry" ] || return 1
      rm -f "$FIXTURE_DIR/native" ;;
    vxlanid) [ -f "$FIXTURE_DIR/retry" ] || return 1 ;;
  esac
}
fixture_start
[ "$?" -ne 0 ] && [ -r "$marker" ] && [ -f "$FIXTURE_DIR/native" ] || exit 31
: > "$FIXTURE_DIR/read-error"
fixture_stop
[ "$?" -ne 0 ] && [ -r "$marker" ] || exit 32
rm -f "$FIXTURE_DIR/read-error"
: > "$FIXTURE_DIR/retry"
fixture_start || exit 33
fixture_start || exit 34
[ "$(grep -c '^vx10 create$' "$FIXTURE_DIR/commands")" = 1 ] || exit 35
fixture_stop || exit 36
[ ! -e "$marker" ] && [ ! -e "$FIXTURE_DIR/native" ] || exit 37
`
	command := exec.Command("sh")
	command.Stdin = strings.NewReader(script)
	command.Env = append(os.Environ(), "FIXTURE_DIR="+dir)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated VXLAN owner lost failure/retry proof: %v\n%s", err, output)
	}
}
