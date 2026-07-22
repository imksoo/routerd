#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Exercise routerd's generated CARP lifecycle on two isolated VNET jails over
# a disposable bridge.  This proves MASTER/BACKUP election, failover, restart,
# owned cleanup, and refusal to adopt an unmarked foreign CARP address.
set -eu

routerd=
evidence_dir=
while [ "$#" -gt 0 ]; do
  case "$1" in
  --routerd) routerd=$2; shift 2 ;;
  --evidence-dir) evidence_dir=$2; shift 2 ;;
  *) echo "usage: $0 --routerd PATH --evidence-dir DIR" >&2; exit 2 ;;
  esac
done
[ -x "$routerd" ]
[ -n "$evidence_dir" ]
[ "$(uname -s)" = FreeBSD ]

mkdir -p "$evidence_dir"
work=$(mktemp -d /var/tmp/routerd-carp-runtime.XXXXXX)
bridge=
a_host=
b_host=
a_if=
b_if=
jail_a='routerd-carp-a'
jail_b='routerd-carp-b'
a_jail_created=0
b_jail_created=0
a_started=0
b_started=0

run_a() { jexec "$jail_a" env ROUTERD_RUNTIME_DIR="$work/a-runtime" routerd_carp_enable=YES sh "$work/routerd-carp-a" "$@"; }
run_b() { jexec "$jail_b" env ROUTERD_RUNTIME_DIR="$work/b-runtime" routerd_carp_enable=YES sh "$work/routerd-carp-b" "$@"; }

cleanup() {
  rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "freebsd-carp-runtime failure evidence:" >&2
    for log in "$evidence_dir"/*; do
      [ -f "$log" ] || continue
      echo "--- $log" >&2
      cat "$log" >&2 || true
    done
  fi
  if [ "$a_started" -eq 1 ]; then run_a onestop >>"$evidence_dir/carp-a-cleanup.log" 2>&1 || rc=1; fi
  if [ "$b_started" -eq 1 ]; then run_b onestop >>"$evidence_dir/carp-b-cleanup.log" 2>&1 || rc=1; fi
  if [ "$a_jail_created" -eq 1 ]; then jail -r "$jail_a" >>"$evidence_dir/cleanup.log" 2>&1 || rc=1; fi
  if [ "$b_jail_created" -eq 1 ]; then jail -r "$jail_b" >>"$evidence_dir/cleanup.log" 2>&1 || rc=1; fi
  if [ -n "$bridge" ] && ifconfig "$bridge" >/dev/null 2>&1; then ifconfig "$bridge" destroy >>"$evidence_dir/cleanup.log" 2>&1 || rc=1; fi
  if [ -n "$a_host" ] && ifconfig "$a_host" >/dev/null 2>&1; then ifconfig "$a_host" destroy >>"$evidence_dir/cleanup.log" 2>&1 || rc=1; fi
  if [ -n "$b_host" ] && ifconfig "$b_host" >/dev/null 2>&1; then ifconfig "$b_host" destroy >>"$evidence_dir/cleanup.log" 2>&1 || rc=1; fi
  rm -rf "$work"
  exit "$rc"
}
trap cleanup EXIT HUP INT TERM

kldload carp >"$evidence_dir/kldload-carp.log" 2>&1 || true
kldstat -m carp >"$evidence_dir/carp-module.log"

bridge=$(ifconfig bridge create)
a_host=$(ifconfig epair create)
b_host=$(ifconfig epair create)
a_if=${a_host%a}b
b_if=${b_host%a}b
ifconfig "$bridge" addm "$a_host" addm "$b_host" up
jail -c name="$jail_a" path=/ host.hostname="$jail_a" persist vnet allow.raw_sockets=1 vnet.interface="$a_if"
a_jail_created=1
jail -c name="$jail_b" path=/ host.hostname="$jail_b" persist vnet allow.raw_sockets=1 vnet.interface="$b_if"
b_jail_created=1
jexec "$jail_a" ifconfig lo0 up
jexec "$jail_b" ifconfig lo0 up
jexec "$jail_a" ifconfig "$a_if" inet 198.18.232.1/24 up
jexec "$jail_b" ifconfig "$b_if" inet 198.18.232.2/24 up

write_router() {
  path=$1 ifname=$2 priority=$3
  cat >"$path" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: lifecycle-carp-$priority
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lifecycle-lan
      spec:
        ifname: $ifname
        managed: false
        owner: external
    - apiVersion: net.routerd.net/v1alpha1
      kind: VirtualAddress
      metadata:
        name: lifecycle-carp-vip
      spec:
        family: ipv4
        interface: lifecycle-lan
        address: 198.18.232.100/24
        mode: vrrp
        vrrp:
          virtualRouterID: 232
          priority: $priority
          advertInterval: 1s
          authentication: lifecycle-carp-secret
EOF
}
write_router "$work/a.yaml" "$a_if" 150
write_router "$work/b.yaml" "$b_if" 100
"$routerd" render freebsd --config "$work/a.yaml" --out-dir "$work/a-rendered" >"$evidence_dir/render-a.log"
"$routerd" render freebsd --config "$work/b.yaml" --out-dir "$work/b-rendered" >"$evidence_dir/render-b.log"
cp "$work/a-rendered/rc.d-routerd_carp" "$work/routerd-carp-a"
cp "$work/b-rendered/rc.d-routerd_carp" "$work/routerd-carp-b"
chmod 0555 "$work/routerd-carp-a" "$work/routerd-carp-b"
grep -F 'foreign CARP state is already present; refusing mutation' "$work/routerd-carp-a" >"$evidence_dir/render-carp.log"
grep -F 'vhid' "$work/routerd-carp-a" >>"$evidence_dir/render-carp.log"

run_a start >"$evidence_dir/carp-a-start.log" 2>&1
a_started=1
run_b start >"$evidence_dir/carp-b-start.log" 2>&1
b_started=1

wait_role() {
  jail_name=$1 ifname=$2 role=$3 logfile=$4
  i=0
  while [ "$i" -lt 20 ]; do
    jexec "$jail_name" ifconfig "$ifname" >"$logfile" 2>&1 || return 1
    if grep -F "carp: $role" "$logfile" >/dev/null; then return 0; fi
    i=$((i + 1)); sleep 1
  done
  return 1
}
wait_role "$jail_a" "$a_if" MASTER "$evidence_dir/carp-a-master.log"
wait_role "$jail_b" "$b_if" BACKUP "$evidence_dir/carp-b-backup.log"
jexec "$jail_b" ping -S 198.18.232.2 -c 3 198.18.232.100 >"$evidence_dir/carp-initial-vip-ping.log"

run_a onestop >"$evidence_dir/carp-a-stop-failover.log" 2>&1
a_started=0
wait_role "$jail_b" "$b_if" MASTER "$evidence_dir/carp-b-master-after-failover.log"
jexec "$jail_a" ping -S 198.18.232.1 -c 3 198.18.232.100 >"$evidence_dir/carp-failover-vip-ping.log"

run_a start >"$evidence_dir/carp-a-restart.log" 2>&1
a_started=1
run_a onestatus >"$evidence_dir/carp-a-status-restart.log" 2>&1
run_b onestatus >"$evidence_dir/carp-b-status-restart.log" 2>&1

run_a onestop >"$evidence_dir/carp-a-stop.log" 2>&1
a_started=0
run_b onestop >"$evidence_dir/carp-b-stop.log" 2>&1
b_started=0
if jexec "$jail_a" ifconfig "$a_if" >"$evidence_dir/carp-a-after-stop.log" 2>&1 && grep -F 'vhid 232' "$evidence_dir/carp-a-after-stop.log" >/dev/null; then
  echo "routerd-owned CARP state survived stop" >&2; exit 1
fi

# Exercise both foreign collision predicates independently: desired address
# under another VHID, then the desired VHID with another address.
jexec "$jail_a" ifconfig "$a_if" inet vhid 233 advbase 1 advskew 100 pass lifecycle-carp-secret alias 198.18.232.100/24
if run_a start >"$evidence_dir/carp-foreign-address-refusal.log" 2>&1; then
  echo "generated CARP service adopted a foreign desired address" >&2; exit 1
fi
jexec "$jail_a" ifconfig "$a_if" >"$evidence_dir/carp-foreign-address-preserved.log"
grep -F 'vhid 233' "$evidence_dir/carp-foreign-address-preserved.log"
jexec "$jail_a" ifconfig "$a_if" inet 198.18.232.100/24 -alias
jexec "$jail_a" ifconfig "$a_if" inet vhid 232 advbase 1 advskew 100 pass lifecycle-carp-secret alias 198.18.232.101/24
if run_a start >"$evidence_dir/carp-foreign-vhid-refusal.log" 2>&1; then
  echo "generated CARP service adopted a foreign matching VHID" >&2; exit 1
fi
jexec "$jail_a" ifconfig "$a_if" >"$evidence_dir/carp-foreign-vhid-preserved.log"
grep -F 'vhid 232' "$evidence_dir/carp-foreign-vhid-preserved.log"
jexec "$jail_a" ifconfig "$a_if" inet 198.18.232.101/24 -alias

printf '%s\n' \
  'carp-generated-vnet-master-backup-failover-restart-stop=ok' \
  'carp-vip-ping-before-after-failover=ok' \
  'carp-foreign-address-and-vhid=generated-refusal-preserved' \
  'carp-owned-cleanup=ok' >"$evidence_dir/summary.log"
printf 'freebsd-carp-runtime=ok\n' >"$evidence_dir/result"
