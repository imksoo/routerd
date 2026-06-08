#!/usr/bin/env bash
set -euo pipefail

# shellcheck disable=SC2034
TEST_NAME="keepalived-no-spurious-restart"
# shellcheck source=tests/netns/lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_common
require_cmd keepalived
require_cmd go

NS="${TEST_ID}-r1"
create_ns "$NS"
create_veth_pair "$NS" eth0 10.88.67.1/24 "$NS" eth1 10.88.67.2/24

CONFIG="$WORKDIR/keepalived.conf"
PIDFILE="$WORKDIR/keepalived.pid"
LOGFILE="$WORKDIR/keepalived.log"
ACTION_LOG="$WORKDIR/rc-service.log"
IP_BIN="$(command -v ip)"
add_cleanup "test -s '$PIDFILE' && kill \"\$(cat '$PIDFILE')\""

cat >"$WORKDIR/rc-service" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

svc=$1
action=$2
[ "$svc" = keepalived ] || exit 2

running() {
  [ -s "$KEEPALIVED_PIDFILE" ] && kill -0 "$(cat "$KEEPALIVED_PIDFILE")" 2>/dev/null
}

case "$action" in
  status)
    running
    ;;
  reload)
    running || exit 1
    echo "reload $(date +%s)" >>"$KEEPALIVED_ACTION_LOG"
    kill -HUP "$(cat "$KEEPALIVED_PIDFILE")"
    ;;
  restart)
    echo "restart $(date +%s)" >>"$KEEPALIVED_ACTION_LOG"
    if running; then
      kill "$(cat "$KEEPALIVED_PIDFILE")" 2>/dev/null || true
      wait "$(cat "$KEEPALIVED_PIDFILE")" 2>/dev/null || true
    fi
    ip netns exec "$KEEPALIVED_NS" keepalived -n -l -f "$KEEPALIVED_CONFIG" >>"$KEEPALIVED_LOG" 2>&1 &
    echo "$!" >"$KEEPALIVED_PIDFILE"
    ;;
  *)
    exit 2
    ;;
esac
EOF
chmod 0755 "$WORKDIR/rc-service"

cat >"$WORKDIR/ip" <<EOF
#!/usr/bin/env bash
set -euo pipefail
exec "$IP_BIN" netns exec "\$KEEPALIVED_NS" "$IP_BIN" "\$@"
EOF
chmod 0755 "$WORKDIR/ip"

cat >"$WORKDIR/no_spurious.go" <<'EOF'
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	vrrpcontroller "github.com/imksoo/routerd/pkg/controller/vrrp"
)

type store map[string]map[string]any

func (s store) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s store) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if status := s[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func main() {
	config := os.Getenv("KEEPALIVED_CONFIG")
	rcService := os.Getenv("FAKE_RC_SERVICE")
	ipCmd := os.Getenv("FAKE_IP")
	pidfile := os.Getenv("KEEPALIVED_PIDFILE")
	if config == "" || rcService == "" || ipCmd == "" || pidfile == "" {
		panic("missing test environment")
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "eth0", Managed: false},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "vip"},
			Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan",
				Address:   "10.88.67.100/32",
				Mode:      "vrrp",
				VRRP:      api.VirtualAddressVRRPSpec{VirtualRouterID: 67, Priority: 150},
			},
		},
	}}}
	controller := vrrpcontroller.Controller{
		Router:     router,
		Store:      store{},
		ConfigPath: config,
		OpenRC:     true,
		RCService:  rcService,
		IP:         ipCmd,
	}
	ctx := context.Background()
	if err := controller.Reconcile(ctx); err != nil {
		panic(err)
	}
	firstPID, err := waitPID(pidfile, 10*time.Second)
	if err != nil {
		panic(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		if err := controller.Reconcile(ctx); err != nil {
			panic(err)
		}
		pid, err := waitPID(pidfile, time.Second)
		if err != nil {
			panic(err)
		}
		if pid != firstPID {
			panic(fmt.Sprintf("keepalived PID changed: %s -> %s", firstPID, pid))
		}
	}
}

func waitPID(path string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && strings.TrimSpace(string(data)) != "" {
			return strings.TrimSpace(string(data)), nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "", fmt.Errorf("timed out waiting for %s", path)
}
EOF

KEEPALIVED_NS="$NS" \
KEEPALIVED_CONFIG="$CONFIG" \
KEEPALIVED_PIDFILE="$PIDFILE" \
KEEPALIVED_LOG="$LOGFILE" \
KEEPALIVED_ACTION_LOG="$ACTION_LOG" \
FAKE_RC_SERVICE="$WORKDIR/rc-service" \
FAKE_IP="$WORKDIR/ip" \
go run "$WORKDIR/no_spurious.go"

restart_count=$(grep -c '^restart ' "$ACTION_LOG" 2>/dev/null || true)
if [[ "$restart_count" -ne 1 ]]; then
  fail "expected exactly one initial keepalived restart, got $restart_count"
fi

log "ok: repeated VRRP reconciles did not restart keepalived for 60s"
