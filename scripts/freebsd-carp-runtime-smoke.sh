#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Exercise routerd's CARP actor on two isolated VNET jails over
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
command -v sqlite3 >/dev/null
command -v jq >/dev/null

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

empty_config="$work/empty.yaml"
cat >"$empty_config" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: lifecycle-carp-empty}
spec: {resources: []}
EOF

apply_actor() {
  side=$1 label=$2 config=$3 jail_name=$4
  jexec "$jail_name" "$routerd" serve --once --controllers vrrp \
    --config "$config" --state-file "$work/$side-state.db" --ledger-file "$work/$side-ledger.db" \
    --status-file "$work/$side-status-$label.json" --socket "$work/$side-api-$label.sock" \
    --status-socket "$work/$side-status-$label.sock"
}
run_a() { apply_actor a "$1" "$2" "$jail_a"; }
run_b() { apply_actor b "$1" "$2" "$jail_b"; }

observe_actor() {
  side=$1 label=$2 ifname=$3
  sqlite3 "$work/$side-state.db" "SELECT status FROM objects WHERE api_version='net.routerd.net/v1alpha1' AND kind='VirtualAddress' AND name='lifecycle-carp-vip';" >"$evidence_dir/carp-$side-$label.json"
  jq -e --arg ifname "$ifname" '.phase == "Applied" and .backend == "carp" and .ifname == $ifname and .address == "198.18.232.100/32"' "$evidence_dir/carp-$side-$label.json" >/dev/null
  sqlite3 "$work/$side-state.db" "SELECT status FROM objects WHERE api_version='routerd.net/v1alpha1' AND kind='Router' AND name='vrrp-native';" >"$evidence_dir/carp-$side-native-$label.json"
  jq -e --arg ifname "$ifname" '(.pending | length) == 0 and (.applied | length) == 1 and .applied[0].backend == "carp" and .applied[0].name == "lifecycle-carp-vip" and .applied[0].ifname == $ifname and .applied[0].address == "198.18.232.100/32" and .applied[0].vhid == 232' "$evidence_dir/carp-$side-native-$label.json" >/dev/null
}

observe_withdrawal() {
  side=$1 label=$2
  sqlite3 "$work/$side-state.db" "SELECT status FROM objects WHERE api_version='routerd.net/v1alpha1' AND kind='Router' AND name='vrrp-native';" >"$evidence_dir/carp-$side-native-$label.json"
  jq -e '(.pending | length) == 0 and (.applied | length) == 0' "$evidence_dir/carp-$side-native-$label.json" >/dev/null
}

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
  if [ "$a_started" -eq 1 ]; then run_a cleanup "$empty_config" >>"$evidence_dir/carp-a-cleanup.log" 2>&1 || rc=1; fi
  if [ "$b_started" -eq 1 ]; then run_b cleanup "$empty_config" >>"$evidence_dir/carp-b-cleanup.log" 2>&1 || rc=1; fi
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
        address: 198.18.232.100/32
        mode: vrrp
        vrrp:
          virtualRouterID: 232
          priority: $priority
          authentication: lifecycle-carp-secret
EOF
}
write_router "$work/a.yaml" "$a_if" 150
write_router "$work/b.yaml" "$b_if" 100
"$routerd" render freebsd --config "$work/a.yaml" --out-dir "$work/a-rendered" >"$evidence_dir/render-a.log"
"$routerd" render freebsd --config "$work/b.yaml" --out-dir "$work/b-rendered" >"$evidence_dir/render-b.log"
test ! -e "$work/a-rendered/rc.d-routerd_carp"
test ! -e "$work/b-rendered/rc.d-routerd_carp"
if grep -q 'routerd_carp_enable' "$work/a-rendered/rc.conf.d-routerd"; then
  echo "rendered boot configuration duplicates the CARP actor" >&2; exit 1
fi

a_started=1
run_a boot "$work/a.yaml" >"$evidence_dir/carp-a-boot.log" 2>&1
observe_actor a boot "$a_if"
b_started=1
run_b boot "$work/b.yaml" >"$evidence_dir/carp-b-boot.log" 2>&1
observe_actor b boot "$b_if"

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

# New actor processes must retain the exact applied receipt on restart and
# refresh the owned native parameters when the declared priority changes.
run_a restart "$work/a.yaml" >"$evidence_dir/carp-a-restart.log" 2>&1
observe_actor a restart "$a_if"
jq -e '.changed == false' "$evidence_dir/carp-a-restart.json" >/dev/null
cmp "$evidence_dir/carp-a-native-boot.json" "$evidence_dir/carp-a-native-restart.json"
write_router "$work/a.yaml" "$a_if" 160
run_a change "$work/a.yaml" >"$evidence_dir/carp-a-change.log" 2>&1
observe_actor a change "$a_if"
jq -e '.priority == 160 and .changed == true' "$evidence_dir/carp-a-change.json" >/dev/null
jexec "$jail_a" ifconfig "$a_if" >"$evidence_dir/carp-a-changed-interface.log"
grep -E 'vhid 232 .*advskew 94([[:space:]]|$)' "$evidence_dir/carp-a-changed-interface.log"
cmp "$evidence_dir/carp-a-native-boot.json" "$evidence_dir/carp-a-native-change.json"

run_a failover "$empty_config" >"$evidence_dir/carp-a-stop-failover.log" 2>&1
a_started=0
observe_withdrawal a failover
wait_role "$jail_b" "$b_if" MASTER "$evidence_dir/carp-b-master-after-failover.log"
jexec "$jail_a" ping -S 198.18.232.1 -c 3 198.18.232.100 >"$evidence_dir/carp-failover-vip-ping.log"

a_started=1
run_a reacquire "$work/a.yaml" >"$evidence_dir/carp-a-reacquire.log" 2>&1
observe_actor a reacquire "$a_if"
run_b refresh "$work/b.yaml" >"$evidence_dir/carp-b-refresh.log" 2>&1
observe_actor b refresh "$b_if"

run_a withdraw "$empty_config" >"$evidence_dir/carp-a-stop.log" 2>&1
a_started=0
observe_withdrawal a withdraw
run_b withdraw "$empty_config" >"$evidence_dir/carp-b-stop.log" 2>&1
b_started=0
observe_withdrawal b withdraw
if jexec "$jail_a" ifconfig "$a_if" >"$evidence_dir/carp-a-after-stop.log" 2>&1 && grep -F 'vhid 232' "$evidence_dir/carp-a-after-stop.log" >/dev/null; then
  echo "routerd-owned CARP state survived stop" >&2; exit 1
fi

# Exercise both foreign collision predicates independently: desired address
# under another VHID, then the desired VHID with another address.
jexec "$jail_a" ifconfig "$a_if" inet vhid 233 advbase 1 advskew 100 pass lifecycle-carp-secret alias 198.18.232.100/32
if run_a foreign-address "$work/a.yaml" >"$evidence_dir/carp-foreign-address-refusal.log" 2>&1; then
  echo "CARP actor adopted a foreign desired address" >&2; exit 1
fi
jexec "$jail_a" ifconfig "$a_if" >"$evidence_dir/carp-foreign-address-preserved.log"
grep -F 'vhid 233' "$evidence_dir/carp-foreign-address-preserved.log"
jexec "$jail_a" ifconfig "$a_if" inet 198.18.232.100/32 -alias
jexec "$jail_a" ifconfig "$a_if" inet vhid 232 advbase 1 advskew 100 pass lifecycle-carp-secret alias 198.18.232.101/32
if run_a foreign-vhid "$work/a.yaml" >"$evidence_dir/carp-foreign-vhid-refusal.log" 2>&1; then
  echo "CARP actor adopted a foreign matching VHID" >&2; exit 1
fi
jexec "$jail_a" ifconfig "$a_if" >"$evidence_dir/carp-foreign-vhid-preserved.log"
grep -F 'vhid 232' "$evidence_dir/carp-foreign-vhid-preserved.log"
jexec "$jail_a" ifconfig "$a_if" inet 198.18.232.101/32 -alias

printf '%s\n' \
  'carp-actor-vnet-boot-master-backup-change-failover-restart-withdraw=ok' \
  'carp-vip-ping-before-after-failover=ok' \
  'carp-foreign-address-and-vhid=actor-refusal-preserved' \
  'carp-owned-cleanup=ok' >"$evidence_dir/summary.log"
printf 'freebsd-carp-runtime=ok\n' >"$evidence_dir/result"
