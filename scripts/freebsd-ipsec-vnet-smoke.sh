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
mkdir -p "$work"
peer="$work/peer"
jail_name="routerd-ipsec-vnet-$$"
epair_host=
own_epair_module=0
host_service_touched=0
strongswan_was_running=0
strongswan_enable_rc=0
peer_pid_relocated=0

host_addr=198.18.10.1
peer_addr=198.18.10.2
host_ts=10.250.1.1
peer_ts=10.250.2.1
connection=native-tunnel
psk='routerd-native-vnet-disposable-psk'

cleanup() {
  rc=$?
  if [ "$host_service_touched" -eq 1 ]; then
    if [ "$strongswan_was_running" -eq 1 ]; then
      service strongswan onestart >>"$evidence/cleanup.log" 2>&1 || true
    else
      service strongswan onestop >>"$evidence/cleanup.log" 2>&1 || true
    fi
    if [ "$strongswan_enable_rc" -eq 0 ]; then
      sysrc "strongswan_enable=$(cat "$work/strongswan-enable.before")" >>"$evidence/cleanup.log" 2>&1 || true
    else
      sysrc -x strongswan_enable >>"$evidence/cleanup.log" 2>&1 || true
    fi
  fi
  rm -f /usr/local/etc/routerd/swanctl/routerd-*.conf \
    /usr/local/etc/routerd/swanctl/routerd.conf \
    /usr/local/etc/routerd/swanctl/.routerd-pending-load \
    /usr/local/etc/swanctl/conf.d/routerd-*.conf \
    /usr/local/etc/swanctl/conf.d/routerd.conf \
    /usr/local/etc/swanctl/conf.d/.routerd-pending-load >>"$evidence/cleanup.log" 2>&1 || true
  peer_safe_to_stop=1
  if [ "$host_service_touched" -eq 1 ] && service strongswan status >/dev/null 2>&1; then
    peer_safe_to_stop=0
    echo 'refusing peer teardown while host charon remains live' >>"$evidence/cleanup.log"
  fi
  if jls -j "$jail_name" >/dev/null 2>&1; then
    if [ "$peer_safe_to_stop" -eq 1 ]; then
      jexec "$jail_name" pkill -TERM charon >>"$evidence/cleanup.log" 2>&1 || true
      jail -r "$jail_name" >>"$evidence/cleanup.log" 2>&1 || true
    fi
  fi
  if [ -n "$epair_host" ] && ifconfig "$epair_host" >/dev/null 2>&1; then
    ifconfig "$epair_host" destroy >>"$evidence/cleanup.log" 2>&1 || true
  fi
  if [ "$own_epair_module" -eq 1 ]; then
    kldunload if_epair >>"$evidence/cleanup.log" 2>&1 || true
  fi
  ifconfig lo0 inet "$host_ts" -alias >/dev/null 2>&1 || true
  printf 'cleanup=complete rc=%s peer_pid_relocated=%s\n' "$rc" "$peer_pid_relocated" >>"$evidence/cleanup.log"
	if [ "$rc" -ne 0 ]; then
		echo "freebsd-ipsec-vnet failed; redacted diagnostics follow" >&2
		for log in \
			"$evidence/strongswan-before.log" \
			"$evidence/strongswan-status.before" \
			"$evidence/strongswan-enable.before.stderr" \
			"$peer/peer-charon.log" \
			"$evidence/peer-load.log" \
			"$evidence/apply-invalid.log" \
			"$evidence/apply-1.log" \
			"$evidence/initiate-1.log" \
			"$evidence/cleanup.log"; do
			if [ -f "$log" ]; then
				echo "--- ${log#$evidence/}" >&2
				sed "s/$psk/[REDACTED]/g" "$log" >&2
			fi
		done
	fi
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

run_bounded() {
  limit=$1
  label=$2
  shift 2
  set +e
  timeout -k 2 "$limit" "$@"
  command_rc=$?
  set -e
  if [ "$command_rc" -eq 124 ]; then
    echo "timed out: $label after ${limit}s" >&2
  fi
  return "$command_rc"
}

if ! kldstat -q -m if_epair; then
  kldload if_epair
  own_epair_module=1
fi
echo 'ipsec-vnet step=host-service-status' >&2
if run_bounded 10 host-service-status service strongswan status >"$evidence/strongswan-status.before" 2>&1; then
  strongswan_was_running=1
fi
set +e
sysrc -n strongswan_enable >"$work/strongswan-enable.before" 2>"$evidence/strongswan-enable.before.stderr"
strongswan_enable_rc=$?
set -e
printf 'strongswan_running_before=%s\nstrongswan_enable_rc_before=%s\n' \
  "$strongswan_was_running" "$strongswan_enable_rc" >"$evidence/strongswan-before.log"
if [ "$strongswan_was_running" -eq 1 ]; then
  echo 'native IPsec VNET lab requires no pre-existing host charon PID owner' >&2
  exit 1
fi
epair_host=$(ifconfig epair create)
epair_peer="${epair_host%a}b"
ifconfig "$epair_host" inet "$host_addr/30" up
ifconfig lo0 inet "$host_ts/32" alias
jail -c name="$jail_name" path=/ host.hostname="$jail_name" persist vnet allow.raw_sockets=1 \
  vnet.interface="$epair_peer"
jexec "$jail_name" ifconfig lo0 up
jexec "$jail_name" ifconfig "$epair_peer" inet "$peer_addr/30" up
jexec "$jail_name" ifconfig lo0 inet "$peer_ts/32" alias
echo 'ipsec-vnet step=underlay-ping' >&2
run_bounded 15 underlay-ping ping -n -c 1 "$peer_addr" >"$evidence/underlay-ping.log" 2>&1

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
echo 'ipsec-vnet step=peer-vici-wait' >&2
wait_for 'peer VICI socket' test -S "$peer/charon.vici"
# charon uses the compile-time /var/run/charon.pid even inside a path=/ VNET
# jail. Relocate that peer-owned file before the host rc.d service starts its
# own charon, otherwise the host daemon refuses the second live PID.
[ -f /var/run/charon.pid ] || { echo 'peer charon did not create /var/run/charon.pid' >&2; exit 1; }
mv /var/run/charon.pid "$peer/peer-charon.pid"
peer_pid_relocated=1
printf 'peer_pid_relocated=%s\n' "$(cat "$peer/peer-charon.pid")" >"$evidence/peer-pid.log"
[ ! -e /var/run/charon.pid ] || { echo 'peer charon PID relocation did not clear host path' >&2; exit 1; }
echo 'ipsec-vnet step=peer-load' >&2
run_bounded 30 peer-load jexec "$jail_name" /usr/local/sbin/swanctl --uri "unix://$peer/charon.vici" --load-all --file "$peer/swanctl.conf" \
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

cat >"$work/invalid-router.yaml" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: native-ipsec-vnet-invalid-load}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: IPsecConnection
    metadata: {name: $connection}
    spec:
      localAddress: $host_addr
      remoteAddress: $peer_addr
      preSharedKey: $psk
      phase1Proposals: [definitely-invalid-ike-proposal]
      leftSubnet: $host_ts/32
      rightSubnet: $peer_ts/32
EOF

host_service_touched=1
echo 'ipsec-vnet step=invalid-production-apply' >&2
if run_bounded 30 invalid-production-apply "$routerd" apply --once --config "$work/invalid-router.yaml" >"$evidence/apply-invalid.log" 2>&1; then
  echo 'invalid IKE proposal unexpectedly loaded' >&2
  exit 1
fi
grep -Eiq 'swanctl|proposal|load' "$evidence/apply-invalid.log"
if grep -F "$psk" "$evidence/apply-invalid.log" >/dev/null; then
  echo 'invalid load diagnostic leaked the disposable PSK' >&2
  exit 1
fi
echo 'ipsec-vnet step=valid-production-apply' >&2
run_bounded 30 valid-production-apply "$routerd" apply --once --config "$work/router.yaml" >"$evidence/apply-1.log" 2>&1
echo 'ipsec-vnet step=initial-initiate' >&2
run_bounded 30 initial-initiate /usr/local/sbin/swanctl --initiate --ike "$connection" --child net >"$evidence/initiate-1.log" 2>&1
echo 'ipsec-vnet step=initial-sa-wait' >&2
wait_for 'initial IKE SA' sh -c "/usr/local/sbin/swanctl --list-sas --ike '$connection' | grep -q ESTABLISHED"
echo 'ipsec-vnet step=initial-host-to-peer-ping' >&2
run_bounded 15 initial-host-to-peer-ping ping -n -S "$host_ts" -c 2 "$peer_ts" >"$evidence/traffic-1.log" 2>&1
echo 'ipsec-vnet step=initial-peer-to-host-ping' >&2
run_bounded 15 initial-peer-to-host-ping jexec "$jail_name" ping -n -S "$peer_ts" -c 2 "$host_ts" >"$evidence/traffic-peer-1.log" 2>&1

echo 'ipsec-vnet step=rekey' >&2
run_bounded 30 rekey /usr/local/sbin/swanctl --rekey --ike "$connection" >"$evidence/rekey.log" 2>&1
echo 'ipsec-vnet step=rekey-sa-wait' >&2
wait_for 'rekeyed IKE SA' sh -c "/usr/local/sbin/swanctl --list-sas --ike '$connection' | grep -q ESTABLISHED"
echo 'ipsec-vnet step=rekey-host-to-peer-ping' >&2
run_bounded 15 rekey-host-to-peer-ping ping -n -S "$host_ts" -c 2 "$peer_ts" >"$evidence/traffic-rekey.log" 2>&1
echo 'ipsec-vnet step=rekey-peer-to-host-ping' >&2
run_bounded 15 rekey-peer-to-host-ping jexec "$jail_name" ping -n -S "$peer_ts" -c 2 "$host_ts" >"$evidence/traffic-peer-rekey.log" 2>&1

echo 'ipsec-vnet step=host-service-stop' >&2
run_bounded 30 host-service-stop service strongswan onestop >"$evidence/host-stop.log" 2>&1
echo 'ipsec-vnet step=restart-production-apply' >&2
run_bounded 30 restart-production-apply "$routerd" apply --once --config "$work/router.yaml" >"$evidence/apply-restart.log" 2>&1
echo 'ipsec-vnet step=restart-initiate' >&2
run_bounded 30 restart-initiate /usr/local/sbin/swanctl --initiate --ike "$connection" --child net >"$evidence/initiate-restart.log" 2>&1
echo 'ipsec-vnet step=restart-sa-wait' >&2
wait_for 'restarted IKE SA' sh -c "/usr/local/sbin/swanctl --list-sas --ike '$connection' | grep -q ESTABLISHED"
echo 'ipsec-vnet step=restart-host-to-peer-ping' >&2
run_bounded 15 restart-host-to-peer-ping ping -n -S "$host_ts" -c 2 "$peer_ts" >"$evidence/traffic-restart.log" 2>&1
echo 'ipsec-vnet step=restart-peer-to-host-ping' >&2
run_bounded 15 restart-peer-to-host-ping jexec "$jail_name" ping -n -S "$peer_ts" -c 2 "$host_ts" >"$evidence/traffic-peer-restart.log" 2>&1

cat >"$work/empty-router.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: native-ipsec-vnet-teardown}
spec: {}
EOF
echo 'ipsec-vnet step=teardown-production-apply' >&2
run_bounded 30 teardown-production-apply "$routerd" apply --once --config "$work/empty-router.yaml" >"$evidence/apply-teardown.log" 2>&1
if /usr/local/sbin/swanctl --list-conns | grep -F "$connection" >/dev/null; then
  echo 'routerd-owned IPsec connection remained after teardown' >&2
  exit 1
fi

printf 'ipsec-invalid-load=actionable-no-secret-leak\nipsec-apply=ok\nipsec-psk-auth=ok\nipsec-bidirectional-traffic=ok\nipsec-rekey=ok\nipsec-restart-recovery=ok\nipsec-teardown=ok\n' >"$evidence/summary.log"
printf 'freebsd-ipsec-vnet=ok\n' >"$evidence/result"
