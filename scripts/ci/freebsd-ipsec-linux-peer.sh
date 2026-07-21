#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause
set -euo pipefail
action=${1:?usage: $0 start|stop}
peer_addr=${ROUTERD_IPSEC_PEER_ADDR:?ROUTERD_IPSEC_PEER_ADDR is required}
psk=routerd-native-linux-peer-disposable-psk
base=${RUNNER_TEMP:?RUNNER_TEMP is required}
dir="$base/routerd-ipsec-peer"
case "$dir" in "$base"/routerd-ipsec-peer) ;; *) exit 2;; esac
log="$dir/peer.log"
emit_failure() {
  for f in "$dir/peer.log" "$dir/reverse-verifier.log"; do
    if sudo test -f "$f"; then
      echo "--- ${f##*/}" >&2
      sudo sed "s/$psk/[REDACTED]/g" "$f" >&2
    fi
  done
}
cleanup() {
  rc=$?
  if [[ ${cleanup_started:-0} -eq 1 ]]; then return "$rc"; fi
  cleanup_started=1
  if [[ "$rc" -ne 0 ]]; then emit_failure; fi
  if [[ -s "$dir/charon.pid" ]]; then sudo kill -TERM "$(sudo cat "$dir/charon.pid")" 2>/dev/null || true; fi
  sudo ip addr del 10.250.2.1/32 dev lo 2>/dev/null || true
  if sudo test -s "$dir/verifier.pid"; then sudo kill -TERM "$(sudo cat "$dir/verifier.pid")" 2>/dev/null || true; fi
  sudo rm -rf -- "$dir"
  return "$rc"
}
case "$action" in
start)
  trap cleanup ERR INT TERM
  sudo apt-get update -qq
  sudo apt-get install -y -qq strongswan-swanctl strongswan-charon netcat-openbsd
  sudo systemctl stop strongswan-starter strongswan 2>/dev/null || true
  sudo rm -rf -- "$dir"; sudo install -d -m 0700 "$dir"
  sudo ip addr add 10.250.2.1/32 dev lo
  sudo tee "$dir/strongswan.conf" >/dev/null <<EOF
charon {
  load_modular = yes
  filelog {
    peer-log {
      path = $log
      append = no
      flush_line = yes
      default = 2
      cfg = 2
      enc = 2
      ike = 2
      net = 2
    }
  }
  plugins {
    include /etc/strongswan.d/charon/*.conf
    vici {
      socket = unix://$dir/charon.vici
    }
  }
}
EOF
  sudo tee "$dir/swanctl.conf" >/dev/null <<EOF
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
      id = %any
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
    id-2 = %any
    secret = "$psk"
  }
}
EOF
  sudo sh -c "exec env STRONGSWAN_CONF='$dir/strongswan.conf' /usr/lib/ipsec/charon --use-syslog >>'$log' 2>&1" &
  echo $! | sudo tee "$dir/charon.pid" >/dev/null
  for _ in $(seq 1 30); do sudo test -S "$dir/charon.vici" && break; sleep 1; done
  sudo test -S "$dir/charon.vici"
  sudo /usr/sbin/swanctl --uri "unix://$dir/charon.vici" --load-all --file "$dir/swanctl.conf"
  sudo env PEER_DIR="$dir" bash -c '
    set -eu
    for phase_port in initial:19091:19191 rekey:19092:19192 restart:19093:19193; do
      phase=${phase_port%%:*}; rest=${phase_port#*:}; port=${rest%%:*}; ack=${rest#*:}
      timeout 120 nc -l -p "$port" >/dev/null
      established=0
      for _ in $(seq 1 30); do
        if /usr/sbin/swanctl --uri "unix://$PEER_DIR/charon.vici" --list-sas | grep -q ESTABLISHED; then
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
  trap cleanup EXIT
  rc=0
  for phase in initial rekey restart; do sudo grep -F "phase=$phase" "$dir/reverse-verifier.log" || rc=1; done
  [ "$(sudo grep -c '2 received' "$dir/reverse-verifier.log")" -eq 3 ] || rc=1
  sudo grep -Eq 'src |dir (in|out)' "$dir/reverse-verifier.log" || rc=1
  sudo cat "$dir/reverse-verifier.log" || rc=1
  cleanup
  exit "$rc"
  ;;
*) exit 2;;
esac
