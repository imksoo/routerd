#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause
set -euo pipefail
action=${1:?usage: $0 start|stop}
base=${RUNNER_TEMP:?RUNNER_TEMP is required}
dir="$base/routerd-ipsec-peer"
phase_dir=${GITHUB_WORKSPACE:?GITHUB_WORKSPACE is required}/.ci-ipsec-phase
case "$dir" in "$base"/routerd-ipsec-peer) ;; *) exit 2;; esac
log="$dir/peer.log"
cleanup() {
  rc=$?
  if [[ -s "$dir/charon.pid" ]]; then sudo kill -TERM "$(sudo cat "$dir/charon.pid")" 2>/dev/null || true; fi
  sudo ip addr del 10.250.2.1/32 dev lo 2>/dev/null || true
  if [[ -s "$dir/verifier.pid" ]]; then sudo kill -TERM "$(sudo cat "$dir/verifier.pid")" 2>/dev/null || true; fi
  sudo rm -rf -- "$dir"
  return "$rc"
}
case "$action" in
start)
  trap cleanup ERR INT TERM
  sudo apt-get update -qq
  sudo apt-get install -y -qq strongswan-swanctl strongswan-charon
  sudo systemctl stop strongswan-starter strongswan 2>/dev/null || true
  sudo rm -rf -- "$dir"; sudo install -d -m 0700 "$dir"
  sudo ip addr add 10.250.2.1/32 dev lo
  rm -rf -- "$phase_dir"; mkdir -p "$phase_dir"
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
    vici { socket = unix://$dir/charon.vici }
  }
}
EOF
  sudo tee "$dir/swanctl.conf" >/dev/null <<'EOF'
connections {
  foreign-sentinel { version = 2 local_addrs = %any remote_addrs = %any local { auth = psk } remote { auth = psk } }
  native-tunnel {
    version = 2
    local_addrs = %any
    remote_addrs = %any
    proposals = aes256-sha256-modp2048
    local { auth = psk id = 10.0.2.2 }
    remote { auth = psk id = %any }
    children { net { local_ts = 10.250.2.1/32 remote_ts = 10.250.1.1/32 esp_proposals = aes256-sha256 start_action = trap } }
  }
}
secrets { peer { id-1 = 10.0.2.2 id-2 = %any secret = "routerd-native-linux-peer-disposable-psk" } }
EOF
  sudo sh -c "exec env STRONGSWAN_CONF='$dir/strongswan.conf' /usr/lib/ipsec/charon --use-syslog >>'$log' 2>&1" &
  echo $! | sudo tee "$dir/charon.pid" >/dev/null
  for _ in $(seq 1 30); do sudo test -S "$dir/charon.vici" && break; sleep 1; done
  sudo test -S "$dir/charon.vici"
  sudo /usr/sbin/swanctl --uri "unix://$dir/charon.vici" --load-all --file "$dir/swanctl.conf"
  sudo env PEER_DIR="$dir" PHASE_DIR="$phase_dir" bash -c '
    set -eu
    last=""
    for _ in $(seq 1 180); do
      phase=$(cat "$PHASE_DIR/phase" 2>/dev/null || true)
      if [ -n "$phase" ] && [ "$phase" != "$last" ]; then
        if /usr/sbin/swanctl --uri "unix://$PEER_DIR/charon.vici" --list-sas | grep -q ESTABLISHED; then
          echo "phase=$phase" >>"$PEER_DIR/reverse-verifier.log"
          ping -n -I 10.250.2.1 -c 2 10.250.1.1 >>"$PEER_DIR/reverse-verifier.log" 2>&1
          ip -s xfrm state >>"$PEER_DIR/reverse-verifier.log" 2>&1 || true
          ip -s xfrm policy >>"$PEER_DIR/reverse-verifier.log" 2>&1 || true
          last=$phase
        fi
      fi
      sleep 1
    done
  ' & echo $! | sudo tee "$dir/verifier.pid" >/dev/null
  trap - ERR INT TERM
  ;;
stop)
  sudo /usr/sbin/swanctl --uri "unix://$dir/charon.vici" --list-conns | grep -F foreign-sentinel
  for phase in initial rekey restart; do sudo grep -F "phase=$phase" "$dir/reverse-verifier.log"; done
  [ "$(sudo grep -c '2 received' "$dir/reverse-verifier.log")" -eq 3 ]
  sudo grep -Eq 'src |dir (in|out)' "$dir/reverse-verifier.log"
  cat "$dir/reverse-verifier.log"
  cleanup
  ;;
*) exit 2;;
esac
