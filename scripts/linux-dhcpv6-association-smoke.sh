#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Formal-package DHCPv6 helper versus independent Kea. Protocol leases only;
# this does not qualify routerd's kernel address controller or forwarding.
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
work=$(mktemp -d /var/tmp/routerd-dhcpv6-association.XXXXXX)
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
tar -xzf "$archive" -C "$work" ./bin/routerd-dhcpv6-client
binary=$work/bin/routerd-dhcpv6-client
sha256sum "$binary"
start_client() {
 "$binary" daemon --resource trial --interface wan0 --request-mode "$mode" \
  --client-duid "$duid" --renew-margin 0s --rebind-margin 0s \
  --socket "$trial/client.sock" --lease-file "$trial/lease.json" --event-file "$trial/events.jsonl" \
  > "$trial/client.log" 2>&1 & client=$!
}
lease() { curl --silent --fail --max-time 2 --unix-socket "$trial/client.sock" http://localhost/v1/lease; }
await_bound() {
 for _ in {1..30}; do
  if lease > "$trial/observed.json" && jq -e --argjson pd "$want_pd" '.state=="bound" and (.addresses|length)==1 and ((.prefixes//[])|length)==$pd' "$trial/observed.json" >/dev/null; then return; fi
  kill -0 "$client"
  sleep 1
 done
 return 1
}
for mode in address pd-address; do
 trial=$work/$mode
 mkdir "$trial"
 want_pd=0; duid=00030001020000000011
 if [ "$mode" = pd-address ]; then want_pd=1; duid=00030001020000000012; fi
 start_client
 await_bound
 python3 - "$trial/observed.json" "$work/leases.csv" <<'PY'
import csv, json, sys
s=json.load(open(sys.argv[1])); rows=list(csv.DictReader(open(sys.argv[2])))
for a in s['addresses']:
 assert a['validSeconds']==45 and a['preferredSeconds']==30, a
 assert any(r['address']==a['address'] and int(r['valid_lifetime'])==45 for r in rows), (a, rows)
for p in s.get('prefixes',[]):
 assert any(r['address']==p['prefix'].split('/')[0] and int(r['valid_lifetime'])==45 for r in rows), (p, rows)
PY
 acquired=$(jq -r '.addresses[0].acquiredAt' "$trial/observed.json")
 # Restart the helper while the peer stays up; restoration must not extend time.
 kill "$client"; wait "$client"; client=''
 start_client
 await_bound
 test "$(jq -r '.addresses[0].acquiredAt' "$trial/observed.json")" = "$acquired"
 # Observe a real timed renewal, rather than invoking an administrative command.
 for _ in {1..25}; do
  lease > "$trial/observed.json"
  if [ "$(jq -r '.addresses[0].acquiredAt' "$trial/observed.json")" != "$acquired" ]; then break; fi
  sleep 1
 done
 test "$(jq -r '.addresses[0].acquiredAt' "$trial/observed.json")" != "$acquired"
 test "$(jq -r .lastMessageType "$trial/observed.json")" = 5
 printf 'PASS %s: independent allocation, restart and timed Renew\n' "$mode"
 kill -STOP "$server"
 saw_renew=false; saw_rebind=false; expired=false
 for _ in {1..55}; do
  lease > "$trial/observed.json"
  state=$(jq -r .state "$trial/observed.json")
  [ "$state" != renewing ] || saw_renew=true
  [ "$state" != rebinding ] || saw_rebind=true
  if jq -e '((.addresses//[])|length)==0 and ((.prefixes//[])|length)==0' "$trial/observed.json" >/dev/null && jq -e '((.addresses//[])|length)==0 and ((.prefixes//[])|length)==0' "$trial/lease.json" >/dev/null; then expired=true; break; fi
  kill -0 "$client"
  sleep 1
 done
 "$saw_renew"; "$saw_rebind"; "$expired"
 kill -CONT "$server"
 await_bound
 printf 'PASS %s: timed Rebind, actual lease expiry and autonomous reacquisition\n' "$mode"
 curl --silent --fail --max-time 2 --unix-socket "$trial/client.sock" -X POST http://localhost/v1/commands/release > "$trial/release.json"
 jq -e '.accepted==true' "$trial/release.json" >/dev/null
 lease | jq -e '.state=="released" and ((.addresses//[])|length)==0 and ((.prefixes//[])|length)==0' >/dev/null
 kill "$client"; wait "$client"; client=''
 printf 'PASS %s: explicit release and clean helper shutdown\n' "$mode"
done
kill "$server"; wait "$server"; server=''
ip link del wan0
kill "$peer"; wait "$peer" || test "$?" = 143; peer=''
test -z "$(ip -6 -o addr show scope global)"
printf 'PASS: fixture processes reaped and trial links removed; no kernel IA_NA ownership claim\n'
