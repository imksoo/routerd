#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TEST_NAME="${TEST_NAME:-$(basename "$0" .sh)}"
TEST_ID="${ROUTERD_NETNS_PREFIX:-rd}-${TEST_NAME//[^a-zA-Z0-9]/-}-$$"
WORKDIR="${WORKDIR:-$(mktemp -d "/tmp/${TEST_ID}.XXXXXX")}"
CLEANUP=()
VETH_COUNTER=0

log() {
  printf '[%s] %s\n' "$TEST_NAME" "$*" >&2
}

fail() {
  log "FAIL: $*"
  exit 1
}

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    fail "run explicitly with sudo: sudo $0"
  fi
}

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    fail "missing required command: $cmd"
  fi
}

frr_cmd() {
  local name="$1"
  if command -v "$name" >/dev/null 2>&1; then
    command -v "$name"
    return 0
  fi
  if [[ -x "/usr/lib/frr/$name" ]]; then
    printf '/usr/lib/frr/%s\n' "$name"
    return 0
  fi
  fail "missing FRR command: $name"
}

require_common() {
  require_root
  require_cmd ip
  require_cmd timeout
  require_cmd python3
}

add_cleanup() {
  CLEANUP+=("$*")
}

cleanup_all() {
  local i
  set +e
  for ((i=${#CLEANUP[@]}-1; i>=0; i--)); do
    eval "${CLEANUP[$i]}" >/dev/null 2>&1 || true
  done
  rm -rf "$WORKDIR"
}

trap cleanup_all EXIT

create_ns() {
  local ns="$1"
  ip netns add "$ns"
  add_cleanup "ip netns delete '$ns'"
  ip -n "$ns" link set lo up
}

create_veth_pair() {
 local ns_a="$1" if_a="$2" addr_a="$3" ns_b="$4" if_b="$5" addr_b="$6"
  VETH_COUNTER=$((VETH_COUNTER + 1))
  local host_a="rdv${VETH_COUNTER}a" host_b="rdv${VETH_COUNTER}b"
  ip link add "$host_a" type veth peer name "$host_b"
  ip link set "$host_a" netns "$ns_a"
  ip link set "$host_b" netns "$ns_b"
  ip -n "$ns_a" link set "$host_a" name "$if_a"
  ip -n "$ns_b" link set "$host_b" name "$if_b"
  ip -n "$ns_a" addr add "$addr_a" dev "$if_a"
  ip -n "$ns_b" addr add "$addr_b" dev "$if_b"
  ip -n "$ns_a" link set "$if_a" up
  ip -n "$ns_b" link set "$if_b" up
}

create_bridge_segment() {
  local bridge="$1"
  shift
  ip link add "$bridge" type bridge
  add_cleanup "ip link delete '$bridge'"
  ip link set "$bridge" up
  while (($#)); do
    local ns="$1" ifname="$2" addr="$3"
    shift 3
    VETH_COUNTER=$((VETH_COUNTER + 1))
    local host_if="rdb${VETH_COUNTER}a"
    ip link add "$host_if" type veth peer name "${host_if}p"
    ip link set "$host_if" master "$bridge"
    ip link set "$host_if" up
    ip link set "${host_if}p" netns "$ns"
    ip -n "$ns" link set "${host_if}p" name "$ifname"
    ip -n "$ns" addr add "$addr" dev "$ifname"
    ip -n "$ns" link set "$ifname" up
  done
}

wait_for() {
  local seconds="$1"
  shift
  local end=$((SECONDS + seconds))
  until "$@"; do
    if ((SECONDS >= end)); then
      return 1
    fi
    sleep 0.2
  done
}

write_file() {
  local path="$1"
  shift
  cat >"$path" <<EOF
$*
EOF
}

frr_prepare_pathspace() {
  local pathspace="$1"
  install -d -m 0755 "/run/frr/$pathspace" "/var/run/frr/$pathspace"
  if id frr >/dev/null 2>&1; then
    chown frr:frr "/run/frr/$pathspace" "/var/run/frr/$pathspace" || true
  fi
  add_cleanup "rm -rf '/run/frr/$pathspace' '/var/run/frr/$pathspace'"
}

start_frr() {
  local ns="$1" pathspace="$2" zebra_conf="$3" bgpd_conf="$4"
  local zebra bgpd
  zebra="$(frr_cmd zebra)"
  bgpd="$(frr_cmd bgpd)"
  frr_prepare_pathspace "$pathspace"
  ip netns exec "$ns" "$zebra" -N "$pathspace" -f "$zebra_conf" -i "$WORKDIR/$pathspace-zebra.pid" -d
  ip netns exec "$ns" "$bgpd" -N "$pathspace" -f "$bgpd_conf" -i "$WORKDIR/$pathspace-bgpd.pid" -d
  add_cleanup "test -f '$WORKDIR/$pathspace-zebra.pid' && kill \"\$(cat '$WORKDIR/$pathspace-zebra.pid')\""
  add_cleanup "test -f '$WORKDIR/$pathspace-bgpd.pid' && kill \"\$(cat '$WORKDIR/$pathspace-bgpd.pid')\""
}

stop_bgpd() {
  local pathspace="$1"
  if [[ -f "$WORKDIR/$pathspace-bgpd.pid" ]]; then
    kill "$(cat "$WORKDIR/$pathspace-bgpd.pid")" 2>/dev/null || true
    rm -f "$WORKDIR/$pathspace-bgpd.pid"
  fi
}

vtysh_ns() {
  local ns="$1" pathspace="$2"
  shift 2
  ip netns exec "$ns" vtysh -N "$pathspace" "$@"
}

frr_reload_ns() {
  local ns="$1" pathspace="$2" config="$3"
  local reload
  reload="$(frr_cmd frr-reload.py)"
  ip netns exec "$ns" "$reload" --pathspace "$pathspace" --reload "$config"
}

basic_zebra_conf() {
  local name="$1"
  cat <<EOF
hostname $name-zebra
password zebra
log file $WORKDIR/$name-zebra.log
EOF
}

start_echo_server() {
  local ns="$1" bind="$2" port="$3" label="$4"
  local script="$WORKDIR/echo-${label}.py"
  cat >"$script" <<'PY'
import socket
import sys
import threading

bind = sys.argv[1]
port = int(sys.argv[2])
label = sys.argv[3]

sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
sock.bind((bind, port))
sock.listen(16)

def handle(conn):
    with conn:
        conn.sendall((label + "\n").encode())
        while True:
            data = conn.recv(1024)
            if not data:
                return
            conn.sendall((label + ":" ).encode() + data)

while True:
    conn, _ = sock.accept()
    threading.Thread(target=handle, args=(conn,), daemon=True).start()
PY
  ip netns exec "$ns" python3 "$script" "$bind" "$port" "$label" &
  local pid=$!
  add_cleanup "kill '$pid'"
}
