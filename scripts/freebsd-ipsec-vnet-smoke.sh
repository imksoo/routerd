#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Exercise the production IPsec apply seam against a second charon in an
# isolated VNET jail.  The host daemon is started and loaded only by routerd;
# the peer has private strongSwan, VICI, PID, and swanctl configuration paths.
set -eu

usage() {
  echo 'usage: freebsd-ipsec-vnet-smoke.sh --routerd /absolute/routerd --evidence-dir /absolute/dir' >&2
}

routerd=
evidence=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --routerd) routerd=${2:?missing routerd path}; shift 2 ;;
    --evidence-dir) evidence=${2:?missing evidence directory}; shift 2 ;;
    *) usage; exit 2 ;;
  esac
done

[ "$(uname -s)" = FreeBSD ] || { echo 'FreeBSD is required' >&2; exit 2; }
[ -x "$routerd" ] || { echo 'an executable --routerd is required' >&2; exit 2; }
[ -n "$evidence" ] || { echo '--evidence-dir is required' >&2; exit 2; }
pkg info -e strongswan || { echo 'strongswan package is required' >&2; exit 2; }

mkdir -p "$evidence"
work="$evidence/work"
peer="$work/peer"
jail_name="routerd-ipsec-vnet-$$"
epair_host=
own_epair_module=0
host_service_started=0

host_addr=198.18.10.1
peer_addr=198.18.10.2
host_ts=10.250.1.1
peer_ts=10.250.2.1
connection=native-tunnel
psk='routerd-native-vnet-disposable-psk'

cleanup() {
  rc=$?
  if [ "$host_service_started" -eq 1 ]; then
    service strongswan onestop >>"$evidence/cleanup.log" 2>&1 || true
  fi
  rm -f /usr/local/etc/swanctl/conf.d/routerd-*.conf \
    /usr/local/etc/swanctl/conf.d/routerd.conf \
    /usr/local/etc/swanctl/conf.d/.routerd-pending-load >>"$evidence/cleanup.log" 2>&1 || true
  if jls -j "$jail_name" >/dev/null 2>&1; then
    jexec "$jail_name" pkill -TERM charon >>"$evidence/cleanup.log" 2>&1 || true
    jail -r "$jail_name" >>"$evidence/cleanup.log" 2>&1 || true
  fi
  if [ -n "$epair_host" ] && ifconfig "$epair_host" >/dev/null 2>&1; then
    ifconfig "$epair_host" destroy >>"$evidence/cleanup.log" 2>&1 || true
  fi
  if [ "$own_epair_module" -eq 1 ]; then
    kldunload if_epair >>"$evidence/cleanup.log" 2>&1 || true
  fi
  ifconfig lo0 inet "$host_ts" -alias >/dev/null 2>&1 || true
  printf 'cleanup=complete rc=%s\n' "$rc" >>"$evidence/cleanup.log"
  return "$rc"
}
trap cleanup EXIT HUP INT TERM

wait_for() {
  description=$1
  shift
  for _ in $(jot 30); do
    if "$@"; then
      return 0
    fi
    sleep 1
  done
  echo "timed out waiting for $description" >&2
  return 1
}

if ! kldstat -q -m if_epair; then
  kldload if_epair
  own_epair_module=1
fi
epair_host=$(ifconfig epair create)
epair_peer="${epair_host%a}b"
ifconfig "$epair_host" inet "$host_addr/30" up
ifconfig lo0 inet "$host_ts/32" alias
jail -c name="$jail_name" path=/ host.hostname="$jail_name" persist vnet \
  vnet.interface="$epair_peer"
jexec "$jail_name" ifconfig lo0 up
jexec "$jail_name" ifconfig "$epair_peer" inet "$peer_addr/30" up
jexec "$jail_name" ifconfig lo0 inet "$peer_ts/32" alias
ping -n -c 1 "$peer_addr" >"$evidence/underlay-ping.log"

mkdir -p "$peer/vty"
chmod 700 "$peer" "$peer/vty"
cat >"$peer/strongswan.conf" <<EOF
charon {
  load_modular = yes
  plugins {
    include /usr/local/etc/strongswan.d/charon/*.conf
    vici {
      socket = unix://$peer/charon.vici
    }
  }
}
EOF
cat >"$peer/swanctl.conf" <<EOF
connections {
  $connection {
    version = 2
    local_addrs = $peer_addr
    remote_addrs = $host_addr
    local {
      auth = psk
      id = $peer_addr
    }
    remote {
      auth = psk
      id = $host_addr
    }
    children {
      net {
        local_ts = $peer_ts/32
        remote_ts = $host_ts/32
        esp_proposals = aes256-sha256
        start_action = trap
      }
    }
  }
}
secrets {
  ike-peer {
    id-1 = $peer_addr
    id-2 = $host_addr
    secret = "$psk"
  }
}
EOF

jexec "$jail_name" daemon -p "$peer/charon.pid" -o "$peer/peer-charon.log" \
  env STRONGSWAN_CONF="$peer/strongswan.conf" /usr/local/libexec/ipsec/charon --use-syslog
wait_for 'peer VICI socket' test -S "$peer/charon.vici"
jexec "$jail_name" /usr/local/sbin/swanctl --uri "unix://$peer/charon.vici" --load-all --file "$peer/swanctl.conf" \
  >"$evidence/peer-load.log" 2>&1

cat >"$work/router.yaml" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: native-ipsec-vnet}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: IPsecConnection
    metadata: {name: $connection}
    spec:
      localAddress: $host_addr
      remoteAddress: $peer_addr
      preSharedKey: $psk
      leftSubnet: $host_ts/32
      rightSubnet: $peer_ts/32
EOF

"$routerd" apply --once --config "$work/router.yaml" >"$evidence/apply-1.log" 2>&1
host_service_started=1
/usr/local/sbin/swanctl --initiate --ike "$connection" --child net >"$evidence/initiate-1.log" 2>&1
wait_for 'initial IKE SA' sh -c "/usr/local/sbin/swanctl --list-sas --ike '$connection' | grep -q ESTABLISHED"
ping -n -S "$host_ts" -c 2 "$peer_ts" >"$evidence/traffic-1.log"

/usr/local/sbin/swanctl --rekey --ike "$connection" >"$evidence/rekey.log" 2>&1
wait_for 'rekeyed IKE SA' sh -c "/usr/local/sbin/swanctl --list-sas --ike '$connection' | grep -q ESTABLISHED"
ping -n -S "$host_ts" -c 2 "$peer_ts" >"$evidence/traffic-rekey.log"

service strongswan onestop >"$evidence/host-stop.log" 2>&1
"$routerd" apply --once --config "$work/router.yaml" >"$evidence/apply-restart.log" 2>&1
/usr/local/sbin/swanctl --initiate --ike "$connection" --child net >"$evidence/initiate-restart.log" 2>&1
wait_for 'restarted IKE SA' sh -c "/usr/local/sbin/swanctl --list-sas --ike '$connection' | grep -q ESTABLISHED"
ping -n -S "$host_ts" -c 2 "$peer_ts" >"$evidence/traffic-restart.log"

cat >"$work/empty-router.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: native-ipsec-vnet-teardown}
spec: {}
EOF
"$routerd" apply --once --config "$work/empty-router.yaml" >"$evidence/apply-teardown.log" 2>&1
if /usr/local/sbin/swanctl --list-conns | grep -F "$connection" >/dev/null; then
  echo 'routerd-owned IPsec connection remained after teardown' >&2
  exit 1
fi

printf 'ipsec-apply=ok\nipsec-psk-auth=ok\nipsec-traffic=ok\nipsec-rekey=ok\nipsec-restart-recovery=ok\nipsec-teardown=ok\n' >"$evidence/summary.log"
printf 'freebsd-ipsec-vnet=ok\n' >"$evidence/result"
