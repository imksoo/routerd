#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Exercise the production IPsec apply seam against a second charon in an
# isolated VNET jail.  The host daemon is started and loaded only by routerd;
# the peer has private strongSwan, VICI, PID, and swanctl configuration paths.
set -eu
# Preserve the native workflow's stderr even when a bounded command call is
# redirected into a disposable evidence log.
exec 3>&2

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
apply_state="$work/routerd-apply-state.db"
apply_ledger="$work/routerd-apply-ledger.db"
jail_name="routerd-ipsec-vnet-$$"
epair_host=
own_epair_module=0
host_service_touched=0
strongswan_was_running=0
strongswan_enable_rc=0
peer_pid_relocated=0
host_pid_relocated=0
cleanup_started=0

host_addr=198.18.10.1
peer_addr=198.18.10.2
host_ts=10.250.1.1
peer_ts=10.250.2.1
connection=native-tunnel
psk='routerd-native-vnet-disposable-psk'

emit_failure_diagnostics() {
  echo 'freebsd-ipsec-vnet failed; redacted diagnostics follow' >&3
  for log in \
    "$evidence/strongswan-before.log" \
    "$evidence/strongswan-status.before" \
    "$evidence/strongswan-enable.before.stderr" \
    "$peer/peer-charon.log" \
    "$evidence/peer-load.log" \
    "$evidence/apply-invalid.log" \
    "$evidence/apply-1.log" \
    "$evidence/initiate-1.log" \
    "$evidence/rekey.log" \
    "$evidence/host-stop.log" \
    "$evidence/apply-restart.log" \
    "$evidence/initiate-restart.log" \
    "$evidence/apply-teardown.log"; do
    if [ -f "$log" ]; then
      echo "--- ${log#"$evidence"/}" >&3
      sed "s/$psk/[REDACTED]/g" "$log" >&3
    fi
  done
}

cleanup() {
  rc=$?
  if [ "$cleanup_started" -eq 1 ]; then
    return "$rc"
  fi
  cleanup_started=1
  # Print before any recovery operation can block, mutate, or remove evidence.
  if [ "$rc" -ne 0 ]; then
    emit_failure_diagnostics
  fi
  trap - EXIT HUP INT TERM
  if [ "$host_pid_relocated" -eq 1 ] && [ -f "$work/host-charon.pid" ] && [ ! -e /var/run/charon.pid ]; then
    mv "$work/host-charon.pid" /var/run/charon.pid >>"$evidence/cleanup.log" 2>&1 || true
    host_pid_relocated=0
  fi
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
  printf 'cleanup=complete rc=%s host_pid_relocated=%s peer_pid_relocated=%s\n' "$rc" "$host_pid_relocated" "$peer_pid_relocated" >>"$evidence/cleanup.log"
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
  # vmactions terminates an otherwise healthy SSH command after roughly 30
  # seconds without output. This is only a progress emitter: timeout(1)
  # remains the sole command deadline/kill mechanism.
  (
    while :; do
      sleep 5
      echo "ipsec-vnet waiting=$label" >&3
    done
  ) &
  heartbeat_pid=$!
  if timeout -k 2 "$limit" "$@"; then
    command_rc=0
  else
    command_rc=$?
  fi
  if kill "$heartbeat_pid" >/dev/null 2>&1; then
    heartbeat_kill_rc=0
  else
    heartbeat_kill_rc=$?
  fi
  if wait "$heartbeat_pid" >/dev/null 2>&1; then
    heartbeat_wait_rc=0
  else
    heartbeat_wait_rc=$?
  fi
  : "$heartbeat_kill_rc" "$heartbeat_wait_rc"
  if [ "$command_rc" -eq 124 ]; then
    echo "timed out: $label after ${limit}s" >&3
  fi
  return "$command_rc"
}

# This is deliberately a one-shot diagnostic for a production apply that has
# written its actionable swanctl error but is still alive.  It does not retry
# the apply: at 35 seconds it captures the Go stacks and exact process/PID-file
# ownership, then terminates only the routerd process it started.
run_invalid_apply_diagnostic() {
  log=$1
  shift
  "$@" >"$log" 2>&1 &
  apply_pid=$!
  printf 'invalid_apply_pid=%s\n' "$apply_pid" >"$evidence/invalid-apply-process.meta"
  elapsed=0
  while [ "$elapsed" -lt 35 ]; do
    if ! kill -0 "$apply_pid" >/dev/null 2>&1; then
      break
    fi
    sleep 5
    elapsed=$((elapsed + 5))
    echo "ipsec-vnet waiting=invalid-production-apply elapsed=${elapsed}s" >&3
  done
  if kill -0 "$apply_pid" >/dev/null 2>&1; then
    echo 'ipsec-vnet diagnostic=invalid-apply-still-live signal=QUIT' >&3
    ps -axo pid,ppid,pgid,sid,stat,command >"$evidence/invalid-apply-processes.before" 2>&1 || true
    {
      printf '%s\n' '--- invalid apply process tree before QUIT'
      grep -E "^[[:space:]]*${apply_pid}[[:space:]]|[[:space:]](charon|daemon)[[:space:]]" "$evidence/invalid-apply-processes.before" || true
      for pidfile in /var/run/daemon-charon.pid /var/run/charon.pid; do
        if [ -f "$pidfile" ]; then
          printf '%s=' "$pidfile"
          cat "$pidfile"
        else
          printf '%s=absent\n' "$pidfile"
        fi
      done
    } >"$evidence/invalid-apply-pids.before" 2>&1
    cat "$evidence/invalid-apply-pids.before" >&3
    kill -QUIT "$apply_pid" || true
    sleep 3
    # Go writes SIGQUIT goroutine stacks to stderr; stderr is the evidence log
    # above, so relay the redacted completed stack to the workflow progress FD.
    echo '--- invalid apply Go stack/output after QUIT' >&3
    sed "s/$psk/[REDACTED]/g" "$log" >&3
    ps -axo pid,ppid,pgid,sid,stat,command >"$evidence/invalid-apply-processes.after-quit" 2>&1 || true
    if kill -0 "$apply_pid" >/dev/null 2>&1; then
      echo 'ipsec-vnet diagnostic=invalid-apply-still-live signal=TERM' >&3
      kill -TERM "$apply_pid" || true
      sleep 3
    fi
    if kill -0 "$apply_pid" >/dev/null 2>&1; then
      echo 'ipsec-vnet diagnostic=invalid-apply-still-live signal=KILL' >&3
      kill -KILL "$apply_pid" || true
    fi
  fi
  if wait "$apply_pid"; then
    apply_rc=0
  else
    apply_rc=$?
  fi
  return "$apply_rc"
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
invalid_apply_started=$(date +%s)
if run_invalid_apply_diagnostic "$evidence/apply-invalid.log" "$routerd" apply --once --config "$work/invalid-router.yaml" \
  --state-file "$apply_state" --ledger-file "$apply_ledger" --status-file "$evidence/apply-invalid.status.json"; then
  invalid_apply_rc=0
else
  invalid_apply_rc=$?
fi
invalid_apply_finished=$(date +%s)
printf 'ipsec-vnet invalid-production-apply rc=%s elapsed=%ss\n' "$invalid_apply_rc" "$((invalid_apply_finished - invalid_apply_started))" >&3
if [ "$invalid_apply_rc" -eq 124 ]; then
  echo 'invalid production apply timed out; redacted diagnostic follows' >&3
  sed "s/$psk/[REDACTED]/g" "$evidence/apply-invalid.log" >&3
  exit 1
fi
if [ "$invalid_apply_rc" -eq 0 ]; then
  echo 'invalid IKE proposal unexpectedly loaded' >&2
  exit 1
fi
grep -Eiq 'swanctl|proposal|load' "$evidence/apply-invalid.log"
if grep -F "$psk" "$evidence/apply-invalid.log" >/dev/null; then
  echo 'invalid load diagnostic leaked the disposable PSK' >&2
  exit 1
fi
echo 'ipsec-vnet step=valid-production-apply' >&2
valid_apply_started=$(date +%s)
if run_bounded 45 valid-production-apply "$routerd" apply --once --config "$work/router.yaml" \
  --state-file "$apply_state" --ledger-file "$apply_ledger" --status-file "$evidence/apply-valid.status.json" >"$evidence/apply-1.log" 2>&1; then
  valid_apply_rc=0
else
  valid_apply_rc=$?
fi
valid_apply_finished=$(date +%s)
printf 'ipsec-vnet valid-production-apply rc=%s elapsed=%ss\n' "$valid_apply_rc" "$((valid_apply_finished - valid_apply_started))" >&3
[ "$valid_apply_rc" -eq 0 ]

# path=/ VNET jails share charon's compile-time PID file. Qualify routerd's
# host service/load first, then temporarily relocate its live PID while the
# isolated peer starts and restore it once the peer PID is private.
[ -f /var/run/charon.pid ] || { echo 'host charon did not create /var/run/charon.pid' >&2; exit 1; }
mv /var/run/charon.pid "$work/host-charon.pid"
host_pid_relocated=1
printf 'host_pid_relocated=%s\n' "$(cat "$work/host-charon.pid")" >"$evidence/host-pid.log"

jexec "$jail_name" daemon -p "$peer/charon.pid" -o "$peer/peer-charon.log" \
  env STRONGSWAN_CONF="$peer/strongswan.conf" /usr/local/libexec/ipsec/charon --use-syslog
echo 'ipsec-vnet step=peer-vici-wait' >&2
wait_for 'peer VICI socket' test -S "$peer/charon.vici"
[ -f /var/run/charon.pid ] || { echo 'peer charon did not create /var/run/charon.pid' >&2; exit 1; }
mv /var/run/charon.pid "$peer/peer-charon.pid"
peer_pid_relocated=1
printf 'peer_pid_relocated=%s\n' "$(cat "$peer/peer-charon.pid")" >"$evidence/peer-pid.log"
mv "$work/host-charon.pid" /var/run/charon.pid
host_pid_relocated=0
echo 'ipsec-vnet step=peer-load' >&2
run_bounded 30 peer-load jexec "$jail_name" /usr/local/sbin/swanctl --uri "unix://$peer/charon.vici" --load-all --file "$peer/swanctl.conf" \
  >"$evidence/peer-load.log" 2>&1
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
run_bounded 45 restart-production-apply "$routerd" apply --once --config "$work/router.yaml" \
  --state-file "$apply_state" --ledger-file "$apply_ledger" --status-file "$evidence/apply-restart.status.json" >"$evidence/apply-restart.log" 2>&1
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
