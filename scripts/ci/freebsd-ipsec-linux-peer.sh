#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause
set -euo pipefail
action=${1:?usage: $0 start|stop}
peer_addr=${ROUTERD_IPSEC_PEER_ADDR:?ROUTERD_IPSEC_PEER_ADDR is required}
guest_addr=${ROUTERD_IPSEC_GUEST_ADDR:?ROUTERD_IPSEC_GUEST_ADDR is required}
topology=${ROUTERD_IPSEC_TOPOLOGY:-slirp}
# Linux interface names are limited to IFNAMSIZ-1 (15 bytes).  Keep the
# defaults aligned with the workflow so standalone fixture use is valid too.
bridge=${ROUTERD_IPSEC_BRIDGE:-rd-ipsec-br}
tap=${ROUTERD_IPSEC_TAP:-rd-ipsec-tap}
netns=${ROUTERD_IPSEC_NETNS:-rd-ipsec-ns}
veth_host=${ROUTERD_IPSEC_VETH_HOST:-rd-ipsec-vh}
veth_peer=${ROUTERD_IPSEC_VETH_PEER:-rd-ipsec-vp}
psk=routerd-native-linux-peer-disposable-psk
run_id=${GITHUB_RUN_ID:?GITHUB_RUN_ID is required}
attempt=${GITHUB_RUN_ATTEMPT:?GITHUB_RUN_ATTEMPT is required}
[[ "$run_id" =~ ^[1-9][0-9]*$ && "$attempt" =~ ^[1-9][0-9]*$ ]] || exit 2
dir="/tmp/routerd-ipsec-peer-${run_id}-${attempt}"
case "$dir" in /tmp/routerd-ipsec-peer-"$run_id"-"$attempt") ;; *) exit 2;; esac
peer_config=/etc/swanctl/conf.d/routerd-native-tunnel.conf
mark_owned() { sudo touch "$dir/owner.$1"; }
is_owned() { sudo test -f "$dir/owner.$1"; }
clear_owned() { sudo rm -f "$dir/owner.$1"; }
peer_exec() {
  if [[ "$topology" == tap ]]; then
    sudo ip netns exec "$netns" "$@"
  else
    sudo "$@"
  fi
}
setup_tap_topology() {
  [[ "$topology" == tap ]] || return 0
  [[ "$bridge" =~ ^[a-zA-Z0-9_.-]+$ && "$tap" =~ ^[a-zA-Z0-9_.-]+$ && "$netns" =~ ^[a-zA-Z0-9_.-]+$ && "$veth_host" =~ ^[a-zA-Z0-9_.-]+$ && "$veth_peer" =~ ^[a-zA-Z0-9_.-]+$ ]] || return 2
  for link in "$bridge" "$tap" "$veth_host"; do
    if sudo ip link show dev "$link" >/dev/null 2>&1; then
      echo "tap topology refuses pre-existing link: $link" >&2
      return 1
    fi
  done
  if sudo ip netns list | awk -v want="$netns" '$1 == want { found=1 } END { exit !found }'; then
    echo "tap topology refuses pre-existing namespace: $netns" >&2
    return 1
  fi
  sudo ip link add "$bridge" type bridge
  mark_owned bridge
  sudo ip link set "$bridge" up
  sudo ip tuntap add dev "$tap" mode tap user "$(id -un)"
  mark_owned tap
  sudo ip link set "$tap" master "$bridge"
  sudo ip link set "$tap" up
  sudo ip netns add "$netns"
  mark_owned netns
  sudo ip link add "$veth_host" type veth peer name "$veth_peer"
  mark_owned veth
  sudo ip link set "$veth_host" master "$bridge"
  sudo ip link set "$veth_host" up
  sudo ip link set "$veth_peer" netns "$netns"
  sudo ip netns exec "$netns" ip link set lo up
  sudo ip netns exec "$netns" ip link set "$veth_peer" up
  sudo ip netns exec "$netns" ip addr add "$peer_addr/24" dev "$veth_peer"
  sudo ip netns exec "$netns" ip route add 10.250.1.1/32 via "$guest_addr" dev "$veth_peer"
  mark_owned peer-selector-route
}
cleanup_tap_topology() {
  # This namespace is fixture-owned.  Do not delete it while its verifier or
  # charon remains live: report and terminate only its own namespace PIDs.
  if is_owned netns && sudo ip netns pids "$netns" | grep -Eq '[0-9]'; then
    sudo ip netns pids "$netns" | xargs -r sudo kill -TERM
    sleep 1
  fi
  if is_owned netns && sudo ip netns pids "$netns" | grep -Eq '[0-9]'; then
    return 1
  fi
  if is_owned peer-selector-route; then
    sudo ip netns exec "$netns" ip route del 10.250.1.1/32 via "$guest_addr" dev "$veth_peer"
    clear_owned peer-selector-route
  fi
  if is_owned netns; then sudo ip netns del "$netns"; clear_owned netns; fi
  if is_owned veth && sudo ip link show dev "$veth_host" >/dev/null 2>&1; then sudo ip link del "$veth_host"; fi
  if is_owned veth; then clear_owned veth; fi
  if is_owned tap && sudo ip link show dev "$tap" >/dev/null 2>&1; then sudo ip link del "$tap"; fi
  if is_owned tap; then clear_owned tap; fi
  if is_owned bridge && sudo ip link show dev "$bridge" >/dev/null 2>&1; then sudo ip link del "$bridge"; fi
  if is_owned bridge; then clear_owned bridge; fi
}
wait_for_swanctl() {
  for _ in $(seq 1 30); do
    if peer_exec /usr/sbin/swanctl --stats >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  echo 'peer-start: package swanctl did not become ready' >&2
  return 1
}
emit_failure() {
  for f in "$dir/peer-load.log" "$dir/peer-list-conns.log" "$dir/reverse-verifier.log" "$dir/reverse-verifier.stdout.log"; do
    if sudo test -f "$f"; then
      echo "--- ${f##*/}" >&2
      sudo sed "s/$psk/[REDACTED]/g" "$f" >&2
    fi
  done
  echo '--- strongswan-starter-journal' >&2
  sudo journalctl --no-pager -u strongswan-starter -n 200 2>&1 | sed "s/$psk/[REDACTED]/g" >&2 || true
  echo '--- charon-journal' >&2
  sudo journalctl --no-pager -t charon -n 200 2>&1 | sed "s/$psk/[REDACTED]/g" >&2 || true
}
stop_peer_service() {
  if [[ "$topology" == tap ]]; then
    peer_exec /usr/sbin/ipsec stop
  else
    sudo systemctl stop strongswan-starter
    if sudo systemctl is-active --quiet strongswan-starter; then return 1; fi
  fi
  if peer_exec timeout 5 /usr/sbin/swanctl --stats >/dev/null 2>&1; then return 1; fi
  return 0
}
cleanup() {
  rc=${1:-$?}
  cleanup_rc=0
  if [[ ${cleanup_started:-0} -eq 1 ]]; then return "$rc"; fi
  cleanup_started=1
  if [[ "$rc" -ne 0 ]]; then emit_failure; fi
  if ! stop_peer_service; then cleanup_rc=1; fi
  if [[ "$topology" == tap ]]; then
    peer_exec ip addr del 10.250.2.1/32 dev lo 2>/dev/null || true
  else
    sudo ip addr del 10.250.2.1/32 dev lo 2>/dev/null || true
  fi
  if is_owned verifier && sudo test -s "$dir/verifier.pid"; then
    verifier_pid=$(sudo cat "$dir/verifier.pid")
    verifier_owned=0
    if [[ "$topology" == tap ]] && sudo ip netns pids "$netns" | grep -Fxq "$verifier_pid" && sudo sh -c 'tr "\0" "\n" <"$1" | grep -Fxq "$2"' sh "/proc/$verifier_pid/environ" "PEER_DIR=$dir"; then
      verifier_owned=1
    elif [[ "$topology" != tap ]] && sudo kill -0 "$verifier_pid" 2>/dev/null && sudo sh -c 'tr "\0" "\n" <"$1" | grep -Fxq "$2"' sh "/proc/$verifier_pid/environ" "PEER_DIR=$dir"; then
      verifier_owned=1
    fi
    if [[ "$verifier_owned" -eq 1 ]]; then
      sudo kill -TERM "$verifier_pid" 2>/dev/null || true
    else
      cleanup_rc=1
    fi
    clear_owned verifier
  fi
  if is_owned peer-config; then sudo rm -f -- "$peer_config"; clear_owned peer-config; fi
  cleanup_tap_topology || cleanup_rc=1
  # Markers stay available until both config and topology cleanup complete.
  sudo rm -rf -- "$dir"
  if [[ "$rc" -eq 0 && "$cleanup_rc" -ne 0 ]]; then return "$cleanup_rc"; fi
  return "$rc"
}
case "$action" in
start)
  trap 'cleanup "$?"' ERR INT TERM
  sudo apt-get update -qq
  sudo apt-get install -y -qq strongswan-swanctl strongswan-charon netcat-openbsd
  # A package post-install may have started the host service.  The TAP peer
  # owns only the namespace process and must not share that daemon or PID file.
  sudo systemctl stop strongswan-starter strongswan 2>/dev/null || true
  if sudo systemctl is-active --quiet strongswan-starter; then exit 1; fi
  sudo test ! -e "$dir"
  sudo install -d -m 0700 "$dir"
  if [[ "$topology" == tap ]]; then
    setup_tap_topology
  else
    sudo systemctl stop strongswan-starter strongswan 2>/dev/null || true
    if sudo systemctl is-active --quiet strongswan-starter; then exit 1; fi
  fi
  sudo install -d -m 0755 /etc/swanctl/conf.d
  sudo test ! -e "$peer_config"
  if [[ "$topology" == tap ]]; then
    peer_exec ip addr add 10.250.2.1/32 dev lo
  else
    sudo ip addr add 10.250.2.1/32 dev lo
  fi
  sudo tee "$peer_config" >/dev/null <<EOF
connections {
  native-tunnel {
    version = 2
    local_addrs = $peer_addr
    remote_addrs = %any
    proposals = aes256-sha256-modp2048
    local {
      auth = psk
      id = $peer_addr
    }
    remote {
      auth = psk
      id = $guest_addr
    }
    children {
      net {
        local_ts = 10.250.2.1/32
        remote_ts = 10.250.1.1/32
        esp_proposals = aes256-sha256
        start_action = trap
      }
    }
  }
}
secrets {
  ike-peer {
    id-1 = $peer_addr
    id-2 = $guest_addr
    secret = "$psk"
  }
}
EOF
  mark_owned peer-config
  if [[ "$topology" == tap ]]; then
    peer_exec /usr/sbin/ipsec start
  else
    sudo systemctl start strongswan-starter
    sudo systemctl is-active --quiet strongswan-starter
  fi
  wait_for_swanctl
  if [[ "$topology" == tap ]]; then
    if sudo sh -c 'ip netns exec "$1" /usr/sbin/swanctl --load-all --file "$2" >"$3" 2>&1' sh "$netns" "$peer_config" "$dir/peer-load.log"; then load_rc=0; else load_rc=$?; fi
  else
    if sudo sh -c '/usr/sbin/swanctl --load-all --file "$1" >"$2" 2>&1' sh "$peer_config" "$dir/peer-load.log"; then load_rc=0; else load_rc=$?; fi
  fi
  if [[ "$load_rc" -ne 0 ]]; then
    echo 'peer-start: swanctl load failed' >&2
    exit 1
  fi
  if [[ "$topology" == tap ]]; then
    if sudo sh -c 'ip netns exec "$1" /usr/sbin/swanctl --list-conns >"$2" 2>&1' sh "$netns" "$dir/peer-list-conns.log"; then list_rc=0; else list_rc=$?; fi
  else
    if sudo sh -c '/usr/sbin/swanctl --list-conns >"$1" 2>&1' sh "$dir/peer-list-conns.log"; then list_rc=0; else list_rc=$?; fi
  fi
  if [[ "$list_rc" -ne 0 ]] || ! sudo grep -Fq native-tunnel "$dir/peer-list-conns.log"; then
    echo 'peer-start: native-tunnel missing after swanctl load' >&2
    exit 1
  fi
  if [[ "$topology" == tap ]]; then verifier_prefix=(sudo ip netns exec "$netns" env -u RUNNER_TRACKING_ID); else verifier_prefix=(sudo env -u RUNNER_TRACKING_ID); fi
  # shellcheck disable=SC2016 # expanded by the peer-side bash, not this shell
  "${verifier_prefix[@]}" PEER_DIR="$dir" nohup bash -c '
    set -eu
    exec >"$PEER_DIR/reverse-verifier.stdout.log" 2>&1 < /dev/null
    printf "%s\n" "$$" >"$PEER_DIR/verifier.pid"
    for phase_port in initial:19091:19191 rekey:19092:19192 restart:19093:19193; do
      phase=${phase_port%%:*}; rest=${phase_port#*:}; port=${rest%%:*}; ack=${rest#*:}
      timeout 120 nc -l -p "$port" >/dev/null
      established=0
      for _ in $(seq 1 30); do
        if /usr/sbin/swanctl --list-sas | grep -q ESTABLISHED; then
          established=1
          echo "phase=$phase" >>"$PEER_DIR/reverse-verifier.log"
          ping -n -I 10.250.2.1 -c 2 10.250.1.1 >>"$PEER_DIR/reverse-verifier.log" 2>&1
          ip -s xfrm state >>"$PEER_DIR/reverse-verifier.log" 2>&1 || true
          ip -s xfrm policy >>"$PEER_DIR/reverse-verifier.log" 2>&1 || true
          printf ok | timeout 120 nc -l -p "$ack" >>"$PEER_DIR/reverse-verifier.log" 2>&1
          break
        fi
        sleep 1
      done
      test "$established" -eq 1
    done
  ' >/dev/null 2>&1 < /dev/null &
  verifier_pid=
  for _ in $(seq 1 30); do
    if sudo test -s "$dir/verifier.pid"; then verifier_pid=$(sudo cat "$dir/verifier.pid"); break; fi
    sleep 1
  done
  [ -n "$verifier_pid" ] || { echo 'peer-start: reverse verifier did not record PID' >&2; exit 1; }
  if [[ "$topology" == tap ]]; then
    sudo ip netns pids "$netns" | grep -Fxq "$verifier_pid"
    sudo sh -c 'tr "\0" "\n" <"$1" | grep -Fxq "$2"' sh "/proc/$verifier_pid/environ" "PEER_DIR=$dir"
  else
    sudo kill -0 "$verifier_pid"
    sudo sh -c 'tr "\0" "\n" <"$1" | grep -Fxq "$2"' sh "/proc/$verifier_pid/environ" "PEER_DIR=$dir"
  fi
  mark_owned verifier
  ready=0
  for _ in $(seq 1 30); do
    if [[ "$topology" == tap ]]; then
      sudo ip netns exec "$netns" ss -ltn | grep -Eq '[:.]19091[[:space:]]' && { ready=1; break; }
    else
      sudo ss -ltn | grep -Eq '[:.]19091[[:space:]]' && { ready=1; break; }
    fi
    sleep 1
  done
  [ "$ready" -eq 1 ] || { echo 'peer-start: reverse verifier did not listen on 19091' >&2; exit 1; }
  trap - ERR INT TERM
  ;;
stop)
  trap 'cleanup "$?"' EXIT
  rc=0
  if ! sudo test -f "$dir/reverse-verifier.log"; then
    echo 'peer-stop: reverse verifier did not run' >&2
    rc=1
  else
    for phase in initial rekey restart; do sudo grep -F "phase=$phase" "$dir/reverse-verifier.log" || rc=1; done
    [ "$(sudo grep -c '2 received' "$dir/reverse-verifier.log")" -eq 3 ] || rc=1
    sudo grep -Eq 'src |dir (in|out)' "$dir/reverse-verifier.log" || rc=1
    sudo cat "$dir/reverse-verifier.log" || rc=1
  fi
  emit_failure
  if cleanup "$rc"; then cleanup_rc=0; else cleanup_rc=$?; fi
  trap - EXIT
  if [[ "$rc" -eq 0 ]]; then exit "$cleanup_rc"; fi
  exit "$rc"
  ;;
*) exit 2;;
esac
