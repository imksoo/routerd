#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Disposable Linux VM only. The peer is rp-pppoe, not a routerd simulator.
# awk programs use their own field references; EXIT invokes cleanup indirectly.
# shellcheck disable=SC2016,SC2317
set -euo pipefail

if [[ ${1:-} != --inside ]]; then
  [[ ${1:-} == --disposable-vm && $# == 5 ]] || {
    echo "usage: $0 --disposable-vm WORK PRODUCT_BIN EXTRACTED_DEPS pap|chap" >&2; exit 2;
  }
  work=$(realpath -e "$2"); product=$(realpath -e "$3"); deps=$(realpath -e "$4"); auth=$5
  [[ $(id -u) == 0 && $(uname -s) == Linux && -c /dev/ppp ]]
  [[ -d /sys/module/pppoe && -d /sys/module/pppox ]] || {
    echo "INCONCLUSIVE: prepare and record PPPoE kernel prerequisites in this disposable VM" >&2; exit 3;
  }
  [[ $(stat -c '%u:%a' "$work") == 0:700 && ! -e "$work/started" ]]
  case "$auth" in pap|chap) ;; *) exit 2;; esac
  for cmd in ip unshare mount python3 curl jq tcpdump; do command -v "$cmd" >/dev/null; done
  [[ -x "$product/routerd" && -x "$product/routerd-pppoe-client" && -x "$deps/usr/sbin/pppd" && -x "$deps/usr/sbin/pppoe-server" ]]
  touch "$work/started"
  mkdir -p "$work/etc/ppp" "$work/state" "$work/run"
  for name in os-release passwd group nsswitch.conf hosts resolv.conf machine-id; do
    [[ ! -e /etc/$name ]] || cp -L "/etc/$name" "$work/etc/$name"
  done
  # pppd's plugin path is compiled in. Bind only in the child mount namespace;
  # restore an initially absent empty mountpoint after the child has exited.
  made_plugin_dir=0
  if [[ ! -e /usr/lib/pppd ]]; then mkdir /usr/lib/pppd; made_plugin_dir=1; fi
  cleanup_outer() {
    local rc=$?
    if [[ $made_plugin_dir == 1 ]]; then rmdir /usr/lib/pppd || rc=1; fi
    printf '%s\n' "$rc" >"$work/outer.exit"
    if [[ $rc == 0 ]]; then printf 'linux-pppoe-runtime=ok\n'; fi
    exit "$rc"
  }
  trap cleanup_outer EXIT
  ip -j link show | jq -S '[.[]|{ifindex,ifname,mtu,address,master,kind:.linkinfo.info_kind}]' >"$work/outer-links.before.json"
  rc=0
  unshare --mount --net --pid --fork --mount-proc bash "$0" --inside "$work" "$product" "$deps" "$auth" || rc=$?
  ip -j link show | jq -S '[.[]|{ifindex,ifname,mtu,address,master,kind:.linkinfo.info_kind}]' >"$work/outer-links.after.json"
  cmp "$work/outer-links.before.json" "$work/outer-links.after.json" || rc=1
  exit "$rc"
fi

work=$2; product=$3; deps=$4; auth=$5
stage=setup; router_pid=; server_pid=; capture_pid=; peer=pppoe-ac; peer_created=0; state_mounted=0
cleanup() {
  local rc=$? child_rc
  trap - EXIT
  printf '%s\n' "$stage" >"$work/last-stage"
  if [[ -n $router_pid ]]; then
    kill -TERM "$router_pid" 2>/dev/null || true
    child_rc=0; wait "$router_pid" || child_rc=$?
    printf '%s\n' "$child_rc" >"$work/router.wait"
    [[ $child_rc == 0 ]] || rc=1
  fi
  if [[ -n $capture_pid ]]; then
    kill -TERM "$capture_pid" 2>/dev/null || true
    wait "$capture_pid" 2>/dev/null || true
  fi
  if [[ -n $server_pid ]]; then
    kill -TERM "$server_pid" 2>/dev/null || true
    child_rc=0; wait "$server_pid" || child_rc=$?
    printf '%s\n' "$child_rc" >"$work/server.wait"
    [[ $child_rc == 0 ]] || rc=1
  fi
  if [[ $peer_created == 1 ]] && ip netns list | cut -d' ' -f1 | grep -Fxq "$peer"; then
    # rp-pppoe returns before pppd's normal LCP termination grace completes.
    # Observe only this fixture's namespace; do not kill unrelated processes.
    local peer_deadline=$((SECONDS+5))
    while :; do
      ip netns pids "$peer" >"$work/peer-pids.after"
      [[ -s "$work/peer-pids.after" && $SECONDS -lt $peer_deadline ]] || break
      sleep .05
    done
    [[ ! -s "$work/peer-pids.after" ]] || rc=1
    ip netns del "$peer" || rc=1
  fi
  ip -j link show >"$work/links.after-cleanup.json"
  ip -j -4 route show table all >"$work/routes4.after-cleanup.json"
  pgrep -a -x pppd >"$work/pppd.after" || true
  [[ ! -s "$work/pppd.after" ]] || rc=1
  if [[ $state_mounted == 1 ]]; then cp -a /var/lib/routerd/. "$work/state/" || rc=1; fi
  printf 'exit=%s stage=%s\n' "$rc" "$stage" >"$work/result.txt"
  exit "$rc"
}
trap cleanup EXIT
mount --make-rprivate /
mount -t sysfs sysfs /sys
mount --bind "$work/etc" /etc
mount --bind "$deps/usr/lib/pppd" /usr/lib/pppd
mount -o remount,bind,ro /usr/lib/pppd
mount -t tmpfs tmpfs /run
mount -t tmpfs tmpfs /var/lib/routerd
state_mounted=1
export PATH="$product:$deps/usr/sbin:/usr/sbin:/usr/bin:/sbin:/bin"
export HOME="$work"
ip link set lo up
ip netns add "$peer"
peer_created=1
ip link add pppoe-wan type veth peer name pppoe-peer
ip link set pppoe-peer netns "$peer"
ip link set pppoe-wan up
ip netns exec "$peer" ip link set lo up
ip netns exec "$peer" ip link set pppoe-peer up
printf '%s\n' 'routerd * "pppoe-test-only" *' > /etc/ppp/pap-secrets
cp /etc/ppp/pap-secrets /etc/ppp/chap-secrets
chmod 0600 /etc/ppp/*-secrets
cat >"$work/server.options" <<EOF
auth
require-$auth
refuse-eap
name routerd-ac
mtu 1454
mru 1454
lcp-echo-interval 3
lcp-echo-failure 3
logfile $work/peer-pppd.log
EOF
ip netns exec "$peer" "$deps/usr/sbin/pppoe-server" -F -k -I pppoe-peer -C routerd-ac -S routerd-lifecycle -L 198.18.10.1 -R 198.18.10.2 -N 1 -O "$work/server.options" -q "$deps/usr/sbin/pppd" -X "$work/peer.pid" >"$work/peer-server.log" 2>&1 &
server_pid=$!
# Discovery + authentication header only; never capture PAP credentials.
tcpdump -n -e -vv -l -s 26 -i pppoe-wan 'ether proto 0x8863 or ether proto 0x8864' >"$work/wire-headers.log" 2>&1 &
capture_pid=$!
for _ in $(seq 1 50); do
  if [[ -s "$work/peer.pid" ]] && ip netns exec "$peer" python3 -c 'import pathlib;raise SystemExit(not any(l.split()[3].upper()=="8863" for l in pathlib.Path("/proc/net/packet").read_text().splitlines()[1:]))'; then break; fi
  kill -0 "$server_pid"
  sleep .1
done
[[ -s "$work/peer.pid" ]]
ip netns exec "$peer" python3 -c 'import pathlib;raise SystemExit(not any(l.split()[3].upper()=="8863" for l in pathlib.Path("/proc/net/packet").read_text().splitlines()[1:]))'
cat >"$work/router.yaml" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: isolated-pppoe}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: wan}
    spec: {ifname: pppoe-wan}
  - apiVersion: net.routerd.net/v1alpha1
    kind: PPPoESession
    metadata: {name: lifecycle}
    spec:
      interface: wan
      username: routerd
      password: pppoe-test-only
      authMethod: $auth
      serviceName: routerd-lifecycle
      acName: routerd-ac
      mtu: 1454
      mru: 1454
      managed: true
EOF
"$product/routerd" validate --config "$work/router.yaml" >"$work/validate.log" 2>&1
"$product/routerd" serve --config "$work/router.yaml" --controllers=daemon-supervisor,daemon-status,pppoe-session --state-file /var/lib/routerd/state.db --ledger-file /var/lib/routerd/state.db --socket /run/routerd/control.sock --status-socket /run/routerd/status.sock --status-file /run/routerd/status.json --http-listen 127.0.0.1:65432 >"$work/router.log" 2>&1 &
router_pid=$!
printf '%s\n' "$router_pid" >"$work/router.pid"
socket=/run/routerd/pppoe-client/lifecycle.sock
ipc() { curl --connect-timeout 1 --max-time 4 --fail --silent --show-error --unix-socket "$socket" "$@"; }
wait_connected() {
  local label=$1 started=$SECONDS
  # An immediate restart can meet a peer still releasing its only session.
  # Keep pppd's normal persist/holdoff behavior; do not inject reconnect commands.
  for _ in $(seq 1 350); do
    if [[ -S $socket ]] && ipc http://localhost/v1/status >"$work/$label.status.json"; then
      if jq -e '.resources[0].phase=="Connected" and .resources[0].observed.currentAddress=="198.18.10.2" and .resources[0].observed.peerAddress=="198.18.10.1"' "$work/$label.status.json" >/dev/null; then
        printf '%s\n' "$((SECONDS-started))" >"$work/$label.connect-seconds"
        return 0
      fi
    fi
    kill -0 "$router_pid"; kill -0 "$server_pid"; sleep .2
  done
  echo "PPPoE connection did not become ready: $label" >&2; return 1
}
traffic() {
  local label=$1
  ping -n -c 3 -W 2 198.18.10.1 >"$work/$label.client-to-peer.ping" 2>&1
  ip netns exec "$peer" ping -n -c 3 -W 2 198.18.10.2 >"$work/$label.peer-to-client.ping" 2>&1
  ip -s -j addr show dev ppp-lifecycle >"$work/$label.link.json"
  ip netns exec "$peer" ip -s -j addr show >"$work/$label.peer-links.json"
  local client_ll peer_ll peer_if
  client_ll=$(jq -er '.[0].addr_info[]|select(.family=="inet6" and .scope=="link")|.local' "$work/$label.link.json")
  peer_ll=$(jq -er '.[0].addr_info[]|select(.family=="inet6" and .scope=="link")|.address' "$work/$label.link.json")
  peer_if=$(jq -er '.[]|select(.link_type=="ppp")|.ifname' "$work/$label.peer-links.json")
  ping -6 -n -c 3 -W 2 -I ppp-lifecycle "$peer_ll" >"$work/$label.client-to-peer-ipv6.ping" 2>&1
  ip netns exec "$peer" ping -6 -n -c 3 -W 2 -I "$peer_if" "$client_ll" >"$work/$label.peer-to-client-ipv6.ping" 2>&1
  ip -j -4 route show table all >"$work/$label.routes4.json"
  ip -j -6 route show table all >"$work/$label.routes6.json"
}
stage=acquire
wait_connected initial
stage=traffic
traffic initial
# Confirm the actual framework caller published the same negotiated source.
for _ in $(seq 1 50); do
  "$product/routerctl" get PPPoESession/lifecycle --socket /run/routerd/control.sock -o json >"$work/controller-status.json" 2>"$work/controller-status.err" || true
  jq -e '..|objects|select(.currentAddress?=="198.18.10.2" and .peerAddress?=="198.18.10.1")' "$work/controller-status.json" >/dev/null && break
  sleep .2
done
jq -e '..|objects|select(.currentAddress?=="198.18.10.2" and .peerAddress?=="198.18.10.1")' "$work/controller-status.json" >/dev/null
stage=stop
ipc -X POST http://localhost/v1/commands/stop >"$work/stop.json"
jq -e '.accepted==true' "$work/stop.json" >/dev/null
if ip link show dev ppp-lifecycle >"$work/interface-after-stop.txt" 2>&1; then echo "PPP interface survived stop" >&2; exit 1; fi
stage=restart
ipc -X POST http://localhost/v1/commands/start >"$work/start.json"
jq -e '.accepted==true' "$work/start.json" >/dev/null
wait_connected restarted
traffic restarted
stage=daemon-termination
ps -eo pid,ppid,stat,args >"$work/processes-before-termination.txt"
python3 -c 'import json,time;print(json.dumps({"unixNs":time.time_ns(),"monotonicNs":time.monotonic_ns()}))' >"$work/termination-start.json"
kill -TERM "$router_pid"
router_rc=0; wait "$router_pid" || router_rc=$?
router_pid=
printf '%s\n' "$router_rc" >"$work/router.wait"
[[ $router_rc == 0 ]]
python3 -c 'import json,time;print(json.dumps({"unixNs":time.time_ns(),"monotonicNs":time.monotonic_ns()}))' >"$work/router-wait-time.json"
if ip link show dev ppp-lifecycle >"$work/interface-after-termination.txt" 2>&1; then echo "PPP interface survived daemon exit" >&2; exit 1; fi
stage=completed
