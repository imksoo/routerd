#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
# Formal-package routerd IA_NA versus independent Kea in private VNET jails.
# Preparation/cross-build is not native qualification. Run only on a disposable VM.
set -eu
export LC_ALL=C
PATH=/sbin:/bin:/usr/sbin:/usr/bin:/usr/local/sbin:/usr/local/bin
export PATH
if [ "${1:-}" != --disposable-vm ] || [ "$#" -ne 5 ]; then
  echo "usage: $0 --disposable-vm MAKE_DIST_ARCHIVE SHA256 KEA_BINARY WORK_PARENT" >&2
  exit 64
fi
case "$(uname -sr)" in 'FreeBSD 14.3-'*) ;; *) echo 'FreeBSD 14.3 required' >&2; exit 65 ;; esac
[ "$(id -u)" -eq 0 ]
[ "$(sysctl -n security.jail.jailed)" -eq 0 ]
[ "$(sysctl -n kern.vm_guest)" != none ]
archive=$(realpath "$2")
[ "$(sha256 -q "$archive")" = "$3" ]
kea=$(realpath "$4")
# Both roots expose the host's installed, certified Kea and its dependencies read-only.
case "$kea" in /usr/*) ;; *) echo 'Kea must be installed under /usr' >&2; exit 64 ;; esac
[ -x "$kea" ]
parent=$(realpath "$5")
case "$parent" in /tmp|/tmp/*|/var/tmp|/var/tmp/*|/usr|/usr/*|*[!A-Za-z0-9_./-]*) echo 'use a home directory outside /usr and temporary storage, e.g. /root/routerd-tests' >&2; exit 64 ;; esac
for tool in jail jexec jls ifconfig sysctl timeout sha256 mount_nullfs mount umount sqlite3 jq python3 ping6 curl sockstat kldstat kldload kldunload; do command -v "$tool" >/dev/null; done
umask 077
work=$(mktemp -d "$parent/freebsd-iana.XXXXXX")
client_root=$work/client
peer_root=$work/peer
client_jail=rd-iana-client-$$
peer_jail=rd-iana-peer-$$
client_jid='' peer_jid='' epair_a='' epair_b='' client='' server='' mounts=''
own_epair=0
stage=certification

client_run() { timeout 20 jexec "$client_jail" env PATH="/opt/routerd/bin:$PATH" "$@"; }
peer_run() { timeout 20 jexec "$peer_jail" "$@"; }
# Stop only children started by this shell; a saved pid file never grants ownership.
stop_child() {
  child=$1
  [ -n "$child" ] || return 0
  kill -CONT "$child" 2>/dev/null || true
  kill -TERM "$child" 2>/dev/null || true
  stop_deadline=$(( $(date +%s) + 10 ))
  while kill -0 "$child" 2>/dev/null; do
    if [ "$(date +%s)" -ge "$stop_deadline" ]; then
      kill -KILL "$child" 2>/dev/null || true
      wait "$child" 2>/dev/null || true
      return 1
    fi
    sleep .2
  done
  wait "$child" 2>/dev/null || true
}
remove_jail() {
  [ -n "$2" ] || return 0
  actual=$(jls -j "$1" -n jid 2>/dev/null) || return 1
  [ "$actual" = "jid=$2" ] || return 1
  timeout 10 jail -r "$1"
  ! jls -j "$1" >/dev/null 2>&1
}
cleanup() {
  result=$?
  trap - EXIT HUP INT TERM
  set +e
  if [ "$result" -ne 0 ]; then
    client_run ifconfig -L -a >"$work/failure-client-addresses.txt" 2>&1
    client_run netstat -rn -f inet6 >"$work/failure-client-routes.txt" 2>&1
    peer_run ifconfig -L -a >"$work/failure-peer-addresses.txt" 2>&1
  fi
  stop_child "$client" || result=1
  stop_child "$server" || result=1
  remove_jail "$client_jail" "$client_jid" || result=1
  remove_jail "$peer_jail" "$peer_jid" || result=1
  if [ -n "$epair_a" ] && ifconfig "$epair_a" >/dev/null 2>&1; then
    ifconfig "$epair_a" destroy || result=1
  elif [ -n "$epair_b" ] && ifconfig "$epair_b" >/dev/null 2>&1; then
    ifconfig "$epair_b" destroy || result=1
  fi
  if [ -n "$epair_a" ] && ifconfig "$epair_a" >/dev/null 2>&1; then result=1; fi
  if [ -n "$epair_b" ] && ifconfig "$epair_b" >/dev/null 2>&1; then result=1; fi
  # Entries are prepended only after a successful mount, so this is reverse order.
  for target in $mounts; do umount "$target" || result=1; done
  if [ "$own_epair" -eq 1 ]; then kldunload if_epair || result=1; fi
  if [ "$result" -eq 0 ]; then
    rm -r -- "$work"
    echo 'PASS: FreeBSD native IA_NA lifecycle and fixture cleanup'
  else
    echo "NOT PASS: stage=$stage; diagnostics retained at $work" >&2
  fi
  exit "$result"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
echo "FreeBSD IA_NA work=$work"
uname -a >"$work/uname.txt"
sha256 "$archive" "$0" "$kea" >"$work/inputs.sha256"
"$kea" -v >"$work/kea-version.txt"

for root in "$client_root" "$peer_root"; do
  mkdir -p "$root/dev" "$root/etc" "$root/tmp" "$root/work" "$root/opt/routerd" "$root/var/run/routerd" "$root/var/db/routerd"
  chmod 1777 "$root/tmp"
  # Utilities/shared libraries are read-only. No shared /var, /run, /etc or rc.d calls.
  for directory in bin sbin lib libexec usr; do
    mkdir "$root/$directory"
    mount_nullfs -o ro "/$directory" "$root/$directory"
    mounts="$root/$directory $mounts"
  done
  cp /etc/passwd /etc/pwd.db /etc/group /etc/nsswitch.conf "$root/etc/"
  if [ -f /var/run/ld-elf.so.hints ]; then cp /var/run/ld-elf.so.hints "$root/var/run/"; fi
  mount -t devfs devfs "$root/dev"
  mounts="$root/dev $mounts"
  mount -t fdescfs fdesc "$root/dev/fd"
  mounts="$root/dev/fd $mounts"
done
tar -xzf "$archive" -C "$client_root/opt/routerd" ./bin
for binary in routerd routerctl routerd-dhcpv6-client; do
  test -x "$client_root/opt/routerd/bin/$binary"
  sha256 "$client_root/opt/routerd/bin/$binary" >>"$work/inputs.sha256"
done
if ! kldstat -q -m if_epair; then kldload if_epair; own_epair=1; fi
epair_a=$(ifconfig epair create)
case "$epair_a" in epair*a) ;; *) exit 65 ;; esac
epair_b=${epair_a%a}b
client_jid=$(jail -i -c name="$client_jail" path="$client_root" host.hostname="$client_jail" persist vnet allow.raw_sockets=1 vnet.interface="$epair_a")
peer_jid=$(jail -i -c name="$peer_jail" path="$peer_root" host.hostname="$peer_jail" persist vnet allow.raw_sockets=1 vnet.interface="$epair_b")
client_run ifconfig lo0 up
peer_run ifconfig lo0 up
client_run ifconfig "$epair_a" inet6 -ifdisabled auto_linklocal up
peer_run ifconfig "$epair_b" inet6 -ifdisabled auto_linklocal up
peer_run ifconfig "$epair_b" inet6 2001:db8:1620::1 prefixlen 64 alias
sleep 3
client_run ifconfig -L "$epair_a" >"$work/cert-client.txt"
peer_run ifconfig -L "$epair_b" >"$work/cert-peer.txt"
if grep -Eq 'tentative|duplicated' "$work/cert-client.txt"; then exit 1; fi
if grep -Eq 'tentative|duplicated' "$work/cert-peer.txt"; then exit 1; fi
peer_ll=$(awk '$1=="inet6" && $2 ~ /^fe80:/ { split($2,a,"%"); print a[1]; exit }' "$work/cert-peer.txt")
[ -n "$peer_ll" ]
# IA_NA gives a /128, not a default/on-link route. Certify the scoped return path.
client_run ifconfig "$epair_a" inet6 2001:db8:1620::ffff prefixlen 128 pltime 20 vltime 20 alias
sleep 3
timeout 8 jexec "$peer_jail" ping6 -n -I "$epair_b" -S "$peer_ll" -c 1 2001:db8:1620::ffff >"$work/cert-peer-ping.txt"
timeout 8 jexec "$client_jail" ping6 -n -I "$epair_a" -S 2001:db8:1620::ffff -c 1 "$peer_ll%$epair_a" >"$work/cert-client-ping.txt"
client_run ifconfig "$epair_a" inet6 2001:db8:1620::ffff -alias
# Check the actual FreeBSD duplicate flag before testing the product's DAD path.
peer_run ifconfig "$epair_b" inet6 2001:db8:1620::fffe prefixlen 128 alias
sleep 3
client_run ifconfig "$epair_a" inet6 2001:db8:1620::fffe prefixlen 128 pltime 20 vltime 20 alias
sleep 3
client_run ifconfig -L "$epair_a" >"$work/cert-dad.txt"
grep -E 'inet6 2001:db8:1620::fffe .*duplicated' "$work/cert-dad.txt" >/dev/null
client_run ifconfig "$epair_a" inet6 2001:db8:1620::fffe -alias
peer_run ifconfig "$epair_b" inet6 2001:db8:1620::fffe -alias
cat >"$peer_root/work/kea.json" <<JSON
{"Dhcp6":{
 "interfaces-config":{"interfaces":["$epair_b"]},
 "lease-database":{"type":"memfile","persist":true,"name":"/work/leases.csv","lfc-interval":0},
 "data-directory":"/work", "renew-timer":45, "rebind-timer":75,
 "preferred-lifetime":60,"valid-lifetime":90,
 "subnet6":[{"id":16,"subnet":"2001:db8:1620::/64","interface":"$epair_b",
 "pools":[{"pool":"2001:db8:1620::100-2001:db8:1620::1ff"}],
 "pd-pools":[{"prefix":"2001:db8:1630::","prefix-len":56,"delegated-len":60}]}]
}}
JSON
peer_run "$kea" -t /work/kea.json >"$work/kea-check.log" 2>&1
jexec "$peer_jail" env KEA_PIDFILE_DIR=/work KEA_LOCKFILE_DIR=/work "$kea" -d -c /work/kea.json >"$work/kea.log" 2>&1 & server=$!
sleep 2
kill -0 "$server"
peer_run sockstat -6 -l >"$work/kea-sockets.txt"
grep ':547' "$work/kea-sockets.txt" >/dev/null
echo 'CERTIFIED: private filesystems/VNET, finite /128 traffic, FreeBSD duplicated flag, independent Kea'

stage=qualification
cat >"$client_root/work/empty.yaml" <<'YAML'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-native-iana}
spec: {resources: []}
YAML
cat >"$client_root/work/candidate.yaml" <<YAML
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-native-iana}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: {name: wan}
      spec: {ifname: $epair_a}
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv6Address
      metadata: {name: address}
      spec: {interface: wan}
YAML
cp "$client_root/work/empty.yaml" "$client_root/work/router.yaml"
client_run routerd version
client_run routerd validate --config /work/candidate.yaml
client_run routerd apply --config /work/candidate.yaml --once --dry-run --state-file /work/dry-state.db --ledger-file /work/dry-ledger.db --status-file /work/dry-status.json >"$work/dry-run.log" 2>&1
start_router() {
  jexec "$client_jail" env PATH="/opt/routerd/bin:$PATH" routerd serve --config /work/router.yaml \
    --controllers daemon-supervisor,daemon-status,dhcpv6-address --state-file /work/state.db --ledger-file /work/ledger.db \
    --status-file /work/status.json --socket /work/control.sock --status-socket /work/status.sock --graceful-stop-timeout 0 >>"$work/router.log" 2>&1 & client=$!
  deadline=$(( $(date +%s) + 15 ))
  while [ ! -S "$client_root/work/control.sock" ]; do
    kill -0 "$client"; [ "$(date +%s)" -lt "$deadline" ]; sleep .2
  done
}
stop_router() { stop_child "$client"; client=; }
apply_config() {
  client_run routerctl plan -f "/work/$1.yaml" --replace --socket /work/control.sock >"$work/plan.json"
  client_run routerctl apply -f "/work/$1.yaml" --replace --socket /work/control.sock >"$work/apply.json"
}
state() {
  row=$(sqlite3 -readonly -cmd '.timeout 2000' "$client_root/work/state.db" "SELECT status FROM objects WHERE kind='DHCPv6Address' AND name='address';") || return 1
  printf '%s\n' "$row" | jq -s '.[0] // {}'
}
await_applied() {
  deadline=$(( $(date +%s) + 45 ))
  while :; do
    state >"$work/observed.json"
    if jq -e '.phase=="Applied" and (.addresses|length)>0' "$work/observed.json" >/dev/null; then return; fi
    kill -0 "$client"; [ "$(date +%s)" -lt "$deadline" ]; sleep 1
  done
}
global_addresses() { addresses=$(client_run ifconfig -L "$epair_a") || return 1; printf '%s\n' "$addresses" | awk '$1=="inet6" && $2 !~ /^fe80:/'; }
await_withdrawn() {
  deadline=$(( $(date +%s) + 20 ))
  while :; do
    remaining=$(global_addresses)
    state >"$work/observed.json"
    if [ -z "$remaining" ] && jq -e 'length==0' "$work/observed.json" >/dev/null; then return; fi
    kill -0 "$client"; [ "$(date +%s)" -lt "$deadline" ]; sleep .5
  done
}
verify_native() {
  client_run ifconfig -L "$epair_a" >"$work/kernel.txt"
  python3 - "$work/observed.json" "$work/kernel.txt" "$peer_root/work/leases.csv" <<'PY'
import csv,ipaddress,json,sys
s=json.load(open(sys.argv[1])); lease=s['nativeState']['leases'][0]
line=next(line.split() for line in open(sys.argv[2]) if line.split()[:2]==['inet6',lease['address']])
assert line[line.index('prefixlen')+1]=='128',line
assert 0<int(line[line.index('vltime')+1])<=90 and 0<=int(line[line.index('pltime')+1])<=60,line
assert not {'duplicated','tentative'}.intersection(line),line
owned=next(a for a in s['nativeState']['native']['addresses'] if a['address']==lease['address']+'/128')
assert owned['held'] and owned['ready'] and not owned.get('pending'),owned
assert owned['acquiredAt']==lease['acquiredAt'],owned
assert any(ipaddress.ip_address(r['address'])==ipaddress.ip_address(lease['address']) and int(r['valid_lifetime'])==90 for r in csv.DictReader(open(sys.argv[3])))
PY
}
start_router
apply_config candidate
await_applied
address=$(jq -r '.nativeState.leases[0].address' "$work/observed.json")
acquired=$(jq -r '.nativeState.leases[0].acquiredAt' "$work/observed.json")
verify_native
timeout 10 jexec "$peer_jail" ping6 -n -I "$epair_b" -S "$peer_ll" -c 3 "$address" >"$work/peer-ping.txt"
timeout 10 jexec "$client_jail" ping6 -n -I "$epair_a" -S "$address" -c 3 "$peer_ll%$epair_a" >"$work/client-ping.txt"
grep -F '3 packets transmitted, 3 packets received, 0.0% packet loss' "$work/peer-ping.txt"
grep -F '3 packets transmitted, 3 packets received, 0.0% packet loss' "$work/client-ping.txt"
echo 'PASS: independent allocation, finite /128, completed DAD and bidirectional scoped traffic'
stop_router
global_addresses | grep -F "$address" >/dev/null
start_router
await_applied
[ "$(jq -r '.nativeState.leases[0].acquiredAt' "$work/observed.json")" = "$acquired" ]
echo 'PASS: stop/restart reopens SQLite without extending acquisition'
deadline=$(( $(date +%s) + 40 ))
while [ "$(jq -r '.nativeState.leases[0].acquiredAt' "$work/observed.json")" = "$acquired" ]; do
  [ "$(date +%s)" -lt "$deadline" ]; kill -0 "$client"; sleep 1; state >"$work/observed.json"
done
await_applied
verify_native
echo 'PASS: timed Renew refreshes the native lifetime from a new acquisition'
kill -STOP "$server"
deadline=$(( $(date +%s) + 115 ))
while :; do
  remaining=$(global_addresses)
  state >"$work/observed.json"
  if [ -z "$remaining" ] && jq -e '.phase!="Applied" and (.nativeState.leases|length)==0' "$work/observed.json" >/dev/null; then break; fi
  [ "$(date +%s)" -lt "$deadline" ]; kill -0 "$client"; sleep 1
done
kill -CONT "$server"
await_applied
echo 'PASS: actual kernel expiry and autonomous reacquisition after peer resumes'
address=$(jq -r '.nativeState.leases[0].address' "$work/observed.json")
apply_config empty
await_withdrawn
echo 'PASS: declaration removal withdraws native address and SQLite receipt'
peer_run ifconfig "$epair_b" inet6 "$address" prefixlen 128 alias
sleep 3
apply_config candidate
deadline=$(( $(date +%s) + 40 ))
while :; do
  remaining=$(global_addresses)
  # Require Kea's accepted-Decline event for this address, not a debug/config string.
  if grep -F DHCP6_DECLINE_LEASE "$work/kea.log" | grep -F "address $address " >/dev/null && ! printf '%s\n' "$remaining" | grep -F "$address" >/dev/null; then break; fi
  [ "$(date +%s)" -lt "$deadline" ]; kill -0 "$client"; sleep 1
done
echo 'PASS: duplicate causes server-observed Decline and native withdrawal'
apply_config empty
await_withdrawn
peer_run ifconfig "$epair_b" inet6 "$address" -alias
cp "$client_root/work/candidate.yaml" "$client_root/work/combined.yaml"
cat >>"$client_root/work/combined.yaml" <<'YAML'
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv6PrefixDelegation
      metadata: {name: prefix}
      spec: {interface: wan, client: routerd-dhcpv6-client}
YAML
apply_config combined
await_applied
client_run curl --silent --fail --max-time 2 --unix-socket /var/run/routerd/dhcpv6-client/prefix.sock http://localhost/v1/lease >"$work/combined-lease.json"
jq -e '.requestMode==2 and (.addresses|length)>0 and (.prefixes|length)>0' "$work/combined-lease.json" >/dev/null
jq -e '.nativeState.source.worker=="prefix" and .phase=="Applied"' "$work/observed.json" >/dev/null
echo 'PASS: shared PD/NA worker and applied native /128'
apply_config empty
await_withdrawn
stop_router
