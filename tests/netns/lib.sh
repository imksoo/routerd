#!/usr/bin/env bash

set -euo pipefail

TEST_NAME="${TEST_NAME:-$(basename "$0" .sh)}"
TEST_ID="${ROUTERD_NETNS_PREFIX:-rd}-${TEST_NAME//[^a-zA-Z0-9]/-}-$$"
# shellcheck disable=SC2034 # consumed by netns tests after sourcing this library
TEST_IF_ID="${ROUTERD_NETNS_IF_PREFIX:-rd}$$"
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
