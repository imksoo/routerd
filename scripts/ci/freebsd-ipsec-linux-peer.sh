#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause
set -euo pipefail
action=${1:?usage: $0 start|stop}
peer_addr=${ROUTERD_IPSEC_PEER_ADDR:?ROUTERD_IPSEC_PEER_ADDR is required}
guest_addr=${ROUTERD_IPSEC_GUEST_ADDR:?ROUTERD_IPSEC_GUEST_ADDR is required}
psk=routerd-native-linux-peer-disposable-psk
run_id=${GITHUB_RUN_ID:?GITHUB_RUN_ID is required}
attempt=${GITHUB_RUN_ATTEMPT:?GITHUB_RUN_ATTEMPT is required}
[[ "$run_id" =~ ^[1-9][0-9]*$ && "$attempt" =~ ^[1-9][0-9]*$ ]] || exit 2
dir="/tmp/routerd-ipsec-peer-${run_id}-${attempt}"
case "$dir" in /tmp/routerd-ipsec-peer-"$run_id"-"$attempt") ;; *) exit 2;; esac
peer_config=/etc/swanctl/conf.d/routerd-native-tunnel.conf
peer_config_created=0
wait_for_swanctl() {
  for _ in $(seq 1 30); do
    if sudo /usr/sbin/swanctl --stats >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  echo 'peer-start: package swanctl did not become ready' >&2
  return 1
}
emit_failure() {
  for f in "$dir/peer-load.log" "$dir/peer-list-conns.log" "$dir/reverse-verifier.log"; do
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
  sudo systemctl stop strongswan-starter
  if sudo systemctl is-active --quiet strongswan-starter; then return 1; fi
  if sudo timeout 5 /usr/sbin/swanctl --stats >/dev/null 2>&1; then return 1; fi
  return 0
}
cleanup() {
  rc=${1:-$?}
  cleanup_rc=0
  if [[ ${cleanup_started:-0} -eq 1 ]]; then return "$rc"; fi
  cleanup_started=1
  if [[ "$rc" -ne 0 ]]; then emit_failure; fi
  if ! stop_peer_service; then cleanup_rc=1; fi
  sudo ip addr del 10.250.2.1/32 dev lo 2>/dev/null || true
  if sudo test -s "$dir/verifier.pid"; then sudo kill -TERM "$(sudo cat "$dir/verifier.pid")" 2>/dev/null || true; fi
  if [[ "$peer_config_created" -eq 1 ]]; then sudo rm -f -- "$peer_config"; fi
  sudo rm -rf -- "$dir"
  if [[ "$rc" -eq 0 && "$cleanup_rc" -ne 0 ]]; then return "$cleanup_rc"; fi
  return "$rc"
}
case "$action" in
start)
  trap 'cleanup "$?"' ERR INT TERM
  sudo apt-get update -qq
  sudo apt-get install -y -qq strongswan-swanctl strongswan-charon netcat-openbsd
  sudo systemctl stop strongswan-starter strongswan 2>/dev/null || true
  if sudo systemctl is-active --quiet strongswan-starter; then exit 1; fi
  sudo rm -rf -- "$dir"; sudo install -d -m 0700 "$dir"
  sudo install -d -m 0755 /etc/swanctl/conf.d
  sudo test ! -e "$peer_config"
  sudo ip addr add 10.250.2.1/32 dev lo
  sudo tee "$peer_config" >/dev/null <<EOF
connections {
  native-tunnel {
    version = 2
    local_addrs = %any
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
  peer {
    id-1 = $peer_addr
    id-2 = $guest_addr
    secret = "$psk"
  }
}
EOF
  peer_config_created=1
  sudo systemctl start strongswan-starter
  sudo systemctl is-active --quiet strongswan-starter
  wait_for_swanctl
  if ! sudo sh -c '/usr/sbin/swanctl --load-all --file "$1" >"$2" 2>&1' sh "$peer_config" "$dir/peer-load.log"; then
    echo 'peer-start: swanctl load failed' >&2
    exit 1
  fi
  if ! sudo sh -c '/usr/sbin/swanctl --list-conns >"$1" 2>&1' sh "$dir/peer-list-conns.log" || ! sudo grep -Fq native-tunnel "$dir/peer-list-conns.log"; then
    echo 'peer-start: native-tunnel missing after swanctl load' >&2
    exit 1
  fi
  sudo env PEER_DIR="$dir" bash -c '
    set -eu
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
  ' & echo $! | sudo tee "$dir/verifier.pid" >/dev/null
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
