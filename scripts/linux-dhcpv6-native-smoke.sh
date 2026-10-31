#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Formal-package routerd IA_NA ownership versus independent Kea and Linux DAD.
# All network effects and fixed daemon paths are isolated inside a disposable VM.
set -euo pipefail
if [ "${1:-}" = --disposable-vm ]; then
 test "$#" = 4
 test "$(id -u)" = 0
 systemd-detect-virt --vm --quiet
 export ROUTERD_HOST_NETNS
 ROUTERD_HOST_NETNS=$(readlink /proc/self/ns/net)
 exec unshare -nm bash "$0" --inside "$2" "$3" "$4"
fi
if [ "${1:-}" != --inside ] || [ "$#" -ne 4 ] || [ -z "${ROUTERD_HOST_NETNS:-}" ]; then
 echo "usage: $0 --disposable-vm MAKE_DIST_ARCHIVE SHA256 KEA_PACKAGE_ROOT" >&2
 exit 2
fi
test "$(readlink /proc/self/ns/net)" != "$ROUTERD_HOST_NETNS"
archive=$(readlink -f "$2")
test "$(sha256sum "$archive" | cut -d ' ' -f1)" = "$3"
fixture=$(readlink -f "$4")
export LD_LIBRARY_PATH="$fixture/usr/lib/x86_64-linux-gnu"
kea=$fixture/usr/sbin/kea-dhcp6
for tool in ip nsenter jq curl python3 ping; do command -v "$tool" >/dev/null; done
"$kea" -v
mount --make-rprivate /
mount -t sysfs sysfs /sys
work=$(mktemp -d /var/tmp/routerd-dhcpv6-native.XXXXXX)
peer='' server='' client=''
finish() {
 rc=$?
 trap - EXIT
 for pid in "$client" "$server" "$peer"; do
  if [ -n "$pid" ]; then kill -CONT "$pid" 2>/dev/null || true; kill "$pid" 2>/dev/null || true; fi
 done
 for pid in "$client" "$server" "$peer"; do
  if [ -n "$pid" ]; then wait "$pid" 2>/dev/null || true; fi
 done
 if [ "$rc" = 0 ]; then rm -r -- "$work"; else echo "FAIL; diagnostics retained at $work" >&2; fi
 exit "$rc"
}
trap finish EXIT
unshare -n sleep 600 & peer=$!
for _ in {1..30}; do
 if [ "$(readlink /proc/"$peer"/ns/net)" != "$(readlink /proc/self/ns/net)" ]; then break; fi
 sleep .1
done
test "$(readlink /proc/"$peer"/ns/net)" != "$(readlink /proc/self/ns/net)"
peer_run() { nsenter -t "$peer" -n -- "$@"; }
ip link add wan0 type veth peer name peer0 netns "$peer"
ip link set lo up
ip link set wan0 up
peer_run ip link set lo up
peer_run ip link set peer0 up
peer_run ip -6 addr add 2001:db8:1620::1/64 dev peer0
for _ in {1..30}; do
 if ! ip -6 addr show dev wan0 | grep -q tentative && ! peer_run ip -6 addr show dev peer0 | grep -q tentative; then break; fi
 sleep .1
done
peer_ll=$(peer_run ip -6 -j addr show dev peer0 | jq -r '.[0].addr_info[] | select(.scope=="link") | .local')
ping -6 -I wan0 -c 1 -W 2 "$peer_ll" >/dev/null
export KEA_PIDFILE_DIR="$work" KEA_LOCKFILE_DIR="$work"
cat > "$work/kea.json" <<EOF
{"Dhcp6": {
 "interfaces-config":{"interfaces":["peer0"]},
 "lease-database":{"type":"memfile","persist":true,"name":"$work/leases.csv","lfc-interval":0},
 "data-directory":"$work", "renew-timer":15, "rebind-timer":30,
 "preferred-lifetime":30, "valid-lifetime":45,
 "subnet6":[{"id":16,"subnet":"2001:db8:1620::/64","interface":"peer0",
 "pools":[{"pool":"2001:db8:1620::100-2001:db8:1620::1ff"}],
 "pd-pools":[{"prefix":"2001:db8:1630::","prefix-len":56,"delegated-len":60}]}]
}}
EOF
peer_run "$kea" -t "$work/kea.json" > "$work/kea-config-check.log" 2>&1
nsenter -t "$peer" -n -- "$kea" -d -c "$work/kea.json" > "$work/kea.log" 2>&1 & server=$!
for _ in {1..30}; do
 if peer_run ss -unl | grep -q ':547 '; then break; fi
 kill -0 "$server"
 sleep .1
done
peer_run ss -unl | grep -q ':547 '
printf 'CERTIFIED: isolated IPv6 link, Kea configuration and UDP/547 listener\n'
# Isolate the supervisor's fixed paths without touching the installed service.
for path in /run/routerd /var/lib/routerd; do test -d "$path"; mount -t tmpfs tmpfs "$path"; done
tar -xzf "$archive" -C "$work" ./bin
export PATH="$work/bin:$PATH"
routerd version
cat > "$work/empty.yaml" <<'YAML'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: native-iana}
spec: {resources: []}
YAML
cat > "$work/candidate.yaml" <<'YAML'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: native-iana}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: {name: wan}
      spec: {ifname: wan0}
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv6Address
      metadata: {name: address}
      spec: {interface: wan}
YAML
cp "$work/empty.yaml" "$work/router.yaml"
routerd validate --config "$work/candidate.yaml"
routerd apply --config "$work/candidate.yaml" --once --dry-run \
 --state-file "$work/dry-state.db" --ledger-file "$work/dry-ledger.db" --status-file "$work/dry-status.json" > "$work/dry-run.log" 2>&1
start_router() {
 routerd serve --config "$work/router.yaml" --controllers daemon-supervisor,daemon-status,dhcpv6-address \
  --state-file "$work/state.db" --ledger-file "$work/ledger.db" --status-file "$work/status.json" \
  --socket "$work/control.sock" --status-socket "$work/status.sock" --graceful-stop-timeout 0 > "$work/router.log" 2>&1 & client=$!
 for _ in {1..30}; do if [ -S "$work/control.sock" ]; then return; fi; kill -0 "$client"; sleep .2; done
 return 1
}
stop_router() { kill "$client"; wait "$client"; client=''; }
apply_config() {
 routerctl plan -f "$1" --replace --socket "$work/control.sock" > "$work/plan.json"
 routerctl apply -f "$1" --replace --socket "$work/control.sock" > "$work/apply.json"
}
state() {
 python3 - "$work/state.db" <<'PYSQL'
import sqlite3,sys
with sqlite3.connect(sys.argv[1]) as c:
 row=c.execute("select status from objects where kind='DHCPv6Address' and name='address'").fetchone()
 print(row[0] if row else '{}')
PYSQL
}
await_applied() {
 for _ in {1..40}; do
  state > "$work/observed.json"
  if jq -e '.phase=="Applied" and (.addresses|length)>0' "$work/observed.json" >/dev/null; then return; fi
  kill -0 "$client"; sleep 1
 done
 return 1
}
await_withdrawn() {
 for _ in {1..30}; do
  if ! ip -6 -o addr show dev wan0 scope global | grep -q inet6 && state | jq -e 'length==0' >/dev/null; then return; fi
  kill -0 "$client"; sleep .5
 done
 return 1
}
start_router
apply_config "$work/candidate.yaml"
await_applied
address=$(jq -r '.nativeState.leases[0].address' "$work/observed.json")
acquired=$(jq -r '.nativeState.leases[0].acquiredAt' "$work/observed.json")
ip -6 -j addr show dev wan0 > "$work/kernel.json"
python3 - "$work/observed.json" "$work/kernel.json" "$work/leases.csv" <<'PYVERIFY'
import csv,json,sys
s=json.load(open(sys.argv[1]));native=json.load(open(sys.argv[2]));lease=s['nativeState']['leases'][0]
a=next(a for a in native[0]['addr_info'] if a['local']==lease['address'])
assert a['prefixlen']==128 and 0<a['valid_life_time']<=45 and a['preferred_life_time']<=30,a
assert not any(x in a.get('flags',[]) for x in ['tentative','dadfailed']),a
assert any(r['address']==lease['address'] and int(r['valid_lifetime'])==45 for r in csv.DictReader(open(sys.argv[3])))
PYVERIFY
peer_run ping -6 -c 3 -W 2 "$address"
ping -6 -I "$address" -c 3 -W 2 "$peer_ll%wan0"
printf 'PASS: independent IA_NA allocation, finite /128, DAD and bidirectional peer traffic\n'
# Stop and reopen SQLite while the original acquisition is still valid.
stop_router
ip -6 -o addr show dev wan0 | grep -F "$address/128" >/dev/null
start_router
await_applied
test "$(jq -r '.nativeState.leases[0].acquiredAt' "$work/observed.json")" = "$acquired"
printf 'PASS: service stop/restart retains finite kernel lifetime and SQLite acquisition\n'
# A real T1 renewal must refresh the kernel lifetime from the new transaction.
for _ in {1..30}; do
 state > "$work/observed.json"
 if [ "$(jq -r '.nativeState.leases[0].acquiredAt' "$work/observed.json")" != "$acquired" ]; then break; fi
 sleep 1
done
test "$(jq -r '.nativeState.leases[0].acquiredAt' "$work/observed.json")" != "$acquired"
printf 'PASS: timed Renew updates acquisition and native lifetime\n'
kill -STOP "$server"
for _ in {1..60}; do
 if ! ip -6 -o addr show dev wan0 scope global | grep -q inet6 && state | jq -e '.phase!="Applied" and (.nativeState.leases|length)==0' >/dev/null; then break; fi
 kill -0 "$client"; sleep 1
done
! ip -6 -o addr show dev wan0 scope global | grep -q inet6
state | jq -e '.phase!="Applied" and (.nativeState.leases|length)==0' >/dev/null
kill -CONT "$server"
await_applied
printf 'PASS: peer outage expires real kernel address; peer return reacquires autonomously\n'
address=$(jq -r '.nativeState.leases[0].address' "$work/observed.json")
apply_config "$work/empty.yaml"
await_withdrawn
printf 'PASS: configuration removal withdraws owned address and retires SQLite receipt\n'
# The helper restores the same live lease, but an independent host now owns it.
peer_run ip -6 addr add "$address/128" dev peer0 nodad
apply_config "$work/candidate.yaml"
saw_decline=false
for _ in {1..35}; do
 if grep -qi 'decline' "$work/kea.log" && ! ip -6 -o addr show dev wan0 | grep -F "$address/128" >/dev/null; then saw_decline=true; break; fi
 kill -0 "$client"; sleep 1
done
"$saw_decline"
printf 'PASS: real DAD conflict triggers server-observed Decline and native withdrawal\n'
apply_config "$work/empty.yaml"
await_withdrawn
peer_run ip -6 addr del "$address/128" dev peer0
cp "$work/candidate.yaml" "$work/combined.yaml"
cat >> "$work/combined.yaml" <<'YAML'
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv6PrefixDelegation
      metadata: {name: prefix}
      spec: {interface: wan, client: routerd-dhcpv6-client}
YAML
routerd validate --config "$work/combined.yaml"
apply_config "$work/combined.yaml"
await_applied
curl --silent --fail --max-time 2 --unix-socket /run/routerd/dhcpv6-client/prefix.sock http://localhost/v1/lease > "$work/combined-lease.json"
jq -e '.requestMode==2 and (.addresses|length)>0 and (.prefixes|length)>0' "$work/combined-lease.json" >/dev/null
state | jq -e '.nativeState.source.worker=="prefix" and .phase=="Applied"' >/dev/null
printf 'PASS: shared PD/NA worker retains independent associations and native /128 ownership\n'
apply_config "$work/empty.yaml"
await_withdrawn
stop_router
kill "$server"; wait "$server"; server=''
ip link del wan0
kill "$peer"; wait "$peer" || test "$?" = 143; peer=''
printf 'PASS: native IA_NA fixture cleaned; installed router service unchanged\n'
