#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Exercise the production IPv6StaticRoute controller against an isolated
# epair/VNET peer. It proves native route add, actual forwarding, daemon
# restart readback, and owned withdrawal without changing a VM NIC.
set -eu

usage() {
  echo 'usage: freebsd-ipv6-static-route-smoke.sh --routerd /absolute/routerd' >&2
}

routerd=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --routerd) routerd=${2:?missing routerd path}; shift 2 ;;
    *) usage; exit 2 ;;
  esac
done

[ "$(uname -s)" = FreeBSD ] || { echo 'FreeBSD is required' >&2; exit 2; }
[ "$(id -u)" -eq 0 ] || { echo 'root is required' >&2; exit 2; }
[ -x "$routerd" ] || { echo 'an executable --routerd is required' >&2; exit 2; }

work=$(mktemp -d /tmp/routerd-ipv6-static-route.XXXXXX)
epair_host=
jail_name="routerd-ipv6-static-route-$$"
daemon_pid=
own_epair_module=0
route_destination='fd42:151::99'
route_prefix="$route_destination/128"
peer_address='fd42:150::2'
host_address='fd42:150::1'

stop_daemon() {
  if [ -n "$daemon_pid" ] && kill -0 "$daemon_pid" 2>/dev/null; then
    kill -TERM "$daemon_pid"
    wait "$daemon_pid"
  fi
  daemon_pid=
}

daemon_running() {
  [ -n "$daemon_pid" ] || return 1
  kill -0 "$daemon_pid" 2>/dev/null || return 1
  state=$(ps -o state= -p "$daemon_pid" 2>/dev/null || true)
  case "$state" in *Z*) return 1 ;; esac
}

route_present() {
  route -n get -inet6 "$route_destination" 2>/dev/null | \
    grep -F "destination: $route_destination" >/dev/null && \
    route -n get -inet6 "$route_destination" 2>/dev/null | \
    grep -F "gateway: $peer_address" >/dev/null && \
    route -n get -inet6 "$route_destination" 2>/dev/null | \
    grep -F "interface: $epair_host" >/dev/null && \
    route -n get -inet6 "$route_destination" 2>/dev/null | \
    grep -F 'STATIC' >/dev/null
}

wait_for() {
  expected=$1
  attempt=0
  while [ "$attempt" -lt 30 ]; do
    daemon_running || return 1
    if route_present; then current=present; else current=absent; fi
    [ "$current" = "$expected" ] && return 0
    attempt=$((attempt + 1))
    sleep 1
  done
  return 1
}

cleanup() {
  rc=$?
  set +e
  stop_daemon
  if [ -n "$epair_host" ]; then
    route -n delete -inet6 "$route_prefix" "$peer_address" >/dev/null 2>&1 || true
  fi
  if jls -j "$jail_name" >/dev/null 2>&1; then jail -r "$jail_name" >/dev/null 2>&1 || true; fi
  if [ -n "$epair_host" ] && ifconfig "$epair_host" >/dev/null 2>&1; then ifconfig "$epair_host" destroy >/dev/null 2>&1 || true; fi
  if [ "$own_epair_module" -eq 1 ]; then kldunload if_epair >/dev/null 2>&1 || true; fi
  rm -rf "$work"
  exit "$rc"
}
trap cleanup EXIT HUP INT TERM

if ! kldstat -q -m if_epair; then
  kldload if_epair
  own_epair_module=1
fi

epair_host=$(ifconfig epair create)
epair_peer="${epair_host%a}b"
ifconfig "$epair_host" inet6 "$host_address/64" up
jail -c name="$jail_name" path=/ host.hostname="$jail_name" persist vnet vnet.interface="$epair_peer"
jexec "$jail_name" ifconfig lo0 up
jexec "$jail_name" ifconfig "$epair_peer" inet6 "$peer_address/64" up
jexec "$jail_name" ifconfig lo0 inet6 "$route_prefix" alias
ping6 -n -c 1 "$peer_address" >/dev/null

cat >"$work/router.yaml" <<EOF_CONFIG
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-ipv6-static-route}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: upstream}
    spec: {ifname: $epair_host, managed: false}
  - apiVersion: net.routerd.net/v1alpha1
    kind: IPv6StaticRoute
    metadata: {name: peer-loopback}
    spec: {interface: upstream, destination: $route_prefix, via: $peer_address}
EOF_CONFIG
cat >"$work/empty.yaml" <<'EOF_EMPTY'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-ipv6-static-route}
spec: {resources: []}
EOF_EMPTY

start_daemon() {
  config=$1
  "$routerd" serve --config "$config" --controllers all \
    --state-file "$work/state.db" --ledger-file "$work/ledger.db" \
    --status-file "$work/status.json" --socket "$work/control.sock" \
    --status-socket "$work/status.sock" --graceful-stop-timeout 0 \
    >"$work/routerd.log" 2>&1 &
  daemon_pid=$!
  wait_for present || { cat "$work/routerd.log" >&2; return 1; }
}

start_daemon "$work/router.yaml"
ping6 -n -c 2 "$route_destination" >/dev/null
stop_daemon
route_present
start_daemon "$work/router.yaml"
ping6 -n -c 2 "$route_destination" >/dev/null
stop_daemon
"$routerd" serve --config "$work/empty.yaml" --controllers all \
  --state-file "$work/state.db" --ledger-file "$work/ledger.db" \
  --status-file "$work/status.json" --socket "$work/control.sock" \
  --status-socket "$work/status.sock" --graceful-stop-timeout 0 \
  >"$work/routerd.log" 2>&1 &
daemon_pid=$!
wait_for absent || { cat "$work/routerd.log" >&2; exit 1; }
if ping6 -n -c 1 -W 1 "$route_destination" >/dev/null 2>&1; then
  echo 'withdrawn IPv6StaticRoute still forwarded traffic' >&2
  exit 1
fi
printf '%s\n' 'PASS: FreeBSD IPv6StaticRoute installs, forwards, survives daemon restart, and withdraws in an isolated VNET fixture'
