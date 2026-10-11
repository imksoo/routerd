package netns_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPartialPreparationCleansOwnedHostLinks(t *testing.T) {
	for _, mode := range []string{"move", "master", "cleanup-failure", "mss-left", "mss-right"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			work := filepath.Join(base, "evidence")
			if err := os.Mkdir(work, 0700); err != nil {
				t.Fatal(err)
			}
			lib, err := filepath.Abs("lib.sh")
			if err != nil {
				t.Fatal(err)
			}
			mss, err := os.ReadFile("vxlan-l2-mss-clamp.sh")
			if err != nil {
				t.Fatal(err)
			}
			start := strings.Index(string(mss), "LEFT=\"")
			end := strings.Index(string(mss), "\nfor side in")
			if start < 0 || end <= start {
				t.Fatal("actual MSS preparation anchors missing")
			}
			script := `
set -e
ip() {
  printf '%s\n' "$*" >>"$CASE_DIR/calls"
  if [ "$1 $2" = "link add" ]; then
    touch "$CASE_DIR/link-$3"
    if [ "$5" = veth ]; then
      peer=${@: -1}
      touch "$CASE_DIR/link-$peer"
      printf '%s\n' "$peer" >"$CASE_DIR/peer-$3"
      printf '%s\n' "$3" >"$CASE_DIR/peer-$peer"
    fi
  elif [ "$1 $2" = "link show" ]; then
    test -f "$CASE_DIR/link-$3"
  elif [ "$1 $2" = "link delete" ]; then
    if [ "$MODE" = cleanup-failure ]; then echo mock-delete-failed >&2;return 19;fi
    peer=$(cat "$CASE_DIR/peer-$3" 2>/dev/null || true)
    rm -f "$CASE_DIR/link-$3"
    if [ -n "$peer" ];then rm -f "$CASE_DIR/link-$peer";fi
  elif [ "$1 $2" = "link set" ]; then
    if [ "${4:-}" = netns ]; then
      if [ "$MODE" = move ] || [ "$MODE" = cleanup-failure ] || { [ "$MODE" = mss-left ] && [[ "$3" = *m2a ]]; } || { [ "$MODE" = mss-right ] && [[ "$3" = *m3a ]]; }; then echo mock-move-failed >&2;return 23;fi
      if [[ "$MODE" = mss-* ]]; then rm -f "$CASE_DIR/link-$3";fi
    fi
    if [ "${4:-}" = master ];then echo mock-master-failed >&2;return 23;fi
  fi
}
source "$LIB_PATH"
TEST_IF_ID=fixture
if [[ "$MODE" = mss-* ]]; then
 eval "$ACTUAL_MSS_PREPARATION"
elif [ "$MODE" = master ]; then
 create_bridge_segment fixture-br left eth0 10.0.0.1/24
else
 create_veth_pair left eth0 10.0.0.1/24 right eth0 10.0.0.2/24
fi
echo unexpected-success
`
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(), "CASE_DIR="+base, "WORKDIR="+work, "LIB_PATH="+lib, "MODE="+mode, "ACTUAL_MSS_PREPARATION="+string(mss[start:end]))
			output, err := cmd.CombinedOutput()
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 23 {
				t.Fatalf("original preparation failure lost: %v\n%s", err, output)
			}
			if strings.Contains(string(output), "unexpected-success") {
				t.Fatalf("continued after failed preparation:\n%s", output)
			}
			raw, err := os.ReadFile(filepath.Join(work, "cleanup.log"))
			if err != nil {
				t.Fatal(err)
			}
			cleanup := "0"
			if mode == "cleanup-failure" {
				cleanup = "2"
				if !strings.Contains(string(raw), "mock-delete-failed") {
					t.Fatalf("cleanup stderr discarded: %s", raw)
				}
			}
			if !strings.Contains(string(raw), "test_exit=23 cleanup_exit="+cleanup) {
				t.Fatalf("wrong cleanup disposition: %s", raw)
			}
			links, err := filepath.Glob(filepath.Join(base, "link-*"))
			if err != nil {
				t.Fatal(err)
			}
			if mode != "cleanup-failure" && len(links) != 0 {
				t.Fatalf("owned host links leaked: %v; cleanup=%s", links, raw)
			}
			if mode == "cleanup-failure" && len(links) == 0 {
				t.Fatal("fixture did not retain failed cleanup evidence")
			}
		})
	}
}
