#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Exercise the supported FreeBSD IPv6 static EgressRoutePolicy shape against
# the production PF renderer.  Everything below is disposable: PF must begin
# disabled and empty; all jails, epairs, states, rules, forwarding settings,
# and a runner-loaded pf module are restored by cleanup.
set -eu

usage() {
  cat <<'USAGE'
usage: freebsd-vnet-policyroute-smoke.sh --routerd /absolute/routerd --evidence-dir /absolute/dir
USAGE
}

routerd=
evidence=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --routerd) routerd=${2:?missing routerd path}; shift 2 ;;
    --evidence-dir) evidence=${2:?missing evidence directory}; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) usage >&2; exit 2 ;;
  esac
done

[ "$(uname -s)" = FreeBSD ] || { echo 'FreeBSD is required' >&2; exit 2; }
[ "$(id -u)" -eq 0 ] || { echo 'root is required' >&2; exit 2; }
[ -x "$routerd" ] || { echo 'an executable --routerd is required' >&2; exit 2; }
[ -n "$evidence" ] || { echo '--evidence-dir is required' >&2; exit 2; }
case "$routerd:$evidence" in /*:/*) ;; *) echo 'paths must be absolute' >&2; exit 2;; esac
for command in ifconfig jail jexec kldload kldstat kldunload pfctl tcpdump ping6 route sysctl; do
  command -v "$command" >/dev/null 2>&1 || { echo "missing command: $command" >&2; exit 2; }
done

mkdir -p "$evidence"
if [ ! -d "$evidence" ] || [ -L "$evidence" ]; then
  echo 'unsafe evidence directory' >&2
  exit 2
fi

tag="g4v6-$$"
work=$(mktemp -d "${TMPDIR:-/tmp}/routerd-g4v6.XXXXXX")
src_jail="${tag}-src"; sink_a_jail="${tag}-sinka"; sink_b_jail="${tag}-sinkb"
in_a=''; in_b=''; out_a=''; out_b=''; out2_a=''; out2_b=''
capture_a=''; capture_b=''; blocked_capture=''
forwarding4=''; forwarding6=''
pf_enabled=0; pf_loaded_by_runner=0
src_created=0; sink_a_created=0; sink_b_created=0

cleanup() {
  rc=$?
  cleanup_rc=0
  trap - EXIT INT TERM HUP
  set +e
  for pid in "$capture_a" "$capture_b" "$blocked_capture"; do
    [ -n "$pid" ] && kill "$pid" 2>/dev/null
  done
  for pid in "$capture_a" "$capture_b" "$blocked_capture"; do
    [ -n "$pid" ] && wait "$pid" 2>/dev/null
  done
  if [ "$pf_enabled" -eq 1 ]; then
    pfctl -F rules >"$evidence/pf-flush-rules.log" 2>&1 || cleanup_rc=70
    pfctl -F states >"$evidence/pf-flush-states.log" 2>&1 || cleanup_rc=70
    pfctl -sr >"$evidence/pf-rules-after-cleanup.log" 2>&1 || cleanup_rc=70
    pfctl -ss >"$evidence/pf-states-after-cleanup.log" 2>&1 || cleanup_rc=70
    [ ! -s "$evidence/pf-rules-after-cleanup.log" ] || cleanup_rc=70
    [ ! -s "$evidence/pf-states-after-cleanup.log" ] || cleanup_rc=70
    pfctl -d >"$evidence/pf-disable.log" 2>&1 || cleanup_rc=70
  fi
  [ "$src_created" -eq 1 ] && jail -r "$src_jail" >"$evidence/jail-cleanup.log" 2>&1
  [ "$sink_a_created" -eq 1 ] && jail -r "$sink_a_jail" >>"$evidence/jail-cleanup.log" 2>&1
  [ "$sink_b_created" -eq 1 ] && jail -r "$sink_b_jail" >>"$evidence/jail-cleanup.log" 2>&1
  [ -n "$in_a" ] && ifconfig "$in_a" destroy >"$evidence/interface-cleanup.log" 2>&1
  [ -n "$out_a" ] && ifconfig "$out_a" destroy >>"$evidence/interface-cleanup.log" 2>&1
  [ -n "$out2_a" ] && ifconfig "$out2_a" destroy >>"$evidence/interface-cleanup.log" 2>&1
  [ -n "$forwarding4" ] && sysctl net.inet.ip.forwarding="$forwarding4" >"$evidence/forwarding4-restore.log" 2>&1
  [ -n "$forwarding6" ] && sysctl net.inet6.ip6.forwarding="$forwarding6" >"$evidence/forwarding6-restore.log" 2>&1
  if [ "$pf_loaded_by_runner" -eq 1 ]; then
    kldunload pf >"$evidence/pf-kldunload.log" 2>&1 || true
  fi
  rm -rf "$work"
  [ "$rc" -ne 0 ] && exit "$rc"
  exit "$cleanup_rc"
}
trap cleanup EXIT INT TERM HUP

if ! kldstat -m pf >/dev/null 2>&1; then
  kldload pf >"$evidence/pf-kldload.log" 2>&1
  pf_loaded_by_runner=1
else
  printf 'pf already loaded\n' >"$evidence/pf-kldload.log"
fi
test -c /dev/pf
[ "$(pfctl -s info 2>/dev/null | awk '/^Status:/ {print $2; exit}')" != Enabled ] || { echo 'PF is already enabled' >&2; exit 2; }
pfctl -sr >"$evidence/pf-rules-before.log" 2>&1
pfctl -ss >"$evidence/pf-states-before.log" 2>&1
if [ -s "$evidence/pf-rules-before.log" ] || [ -s "$evidence/pf-states-before.log" ]; then
  echo 'PF rules or states already exist' >&2
  exit 2
fi

forwarding4=$(sysctl -n net.inet.ip.forwarding)
forwarding6=$(sysctl -n net.inet6.ip6.forwarding)
sysctl net.inet.ip.forwarding=1 >"$evidence/forwarding4-enable.log"
sysctl net.inet6.ip6.forwarding=1 >"$evidence/forwarding6-enable.log"
jail -c name="$src_jail" vnet persist; src_created=1
jail -c name="$sink_a_jail" vnet persist; sink_a_created=1
jail -c name="$sink_b_jail" vnet persist; sink_b_created=1
in_a=$(ifconfig epair create); in_b=${in_a%a}b
out_a=$(ifconfig epair create); out_b=${out_a%a}b
out2_a=$(ifconfig epair create); out2_b=${out2_a%a}b
ifconfig "$in_b" vnet "$src_jail"; ifconfig "$out_b" vnet "$sink_a_jail"; ifconfig "$out2_b" vnet "$sink_b_jail"
ifconfig "$in_a" inet6 2001:db8:10::1/64 up
ifconfig "$out_a" inet6 2001:db8:100::1/64 up
ifconfig "$out2_a" inet6 2001:db8:200::1/64 up
jexec "$src_jail" ifconfig lo0 up
jexec "$src_jail" ifconfig "$in_b" inet6 2001:db8:10::10/64 up
jexec "$src_jail" ifconfig "$in_b" inet6 2001:db8:10::11/64 alias
jexec "$src_jail" route -6 add default 2001:db8:10::1
jexec "$sink_a_jail" ifconfig lo0 up; jexec "$sink_a_jail" ifconfig "$out_b" inet6 2001:db8:100::2/64 up
jexec "$sink_b_jail" ifconfig lo0 up; jexec "$sink_b_jail" ifconfig "$out2_b" inet6 2001:db8:200::2/64 up
jexec "$sink_a_jail" route -6 add 2001:db8:10::/64 2001:db8:100::1

cat >"$work/router.yaml" <<EOF_CONFIG
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-vnet-policyroute}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: lan}
    spec: {ifname: $in_a, managed: false}
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: wan-a}
    spec: {ifname: $out_a, managed: false}
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: wan-b}
    spec: {ifname: $out2_a, managed: false}
  - apiVersion: firewall.routerd.net/v1alpha1
    kind: FirewallZone
    metadata: {name: lan}
    spec: {role: trust, interfaces: [lan]}
  - apiVersion: firewall.routerd.net/v1alpha1
    kind: FirewallZone
    metadata: {name: wan-a}
    spec: {role: untrust, interfaces: [wan-a]}
  - apiVersion: firewall.routerd.net/v1alpha1
    kind: FirewallZone
    metadata: {name: wan-b}
    spec: {role: untrust, interfaces: [wan-b]}
  - apiVersion: net.routerd.net/v1alpha1
    kind: EgressRoutePolicy
    metadata: {name: source-affinity-v6}
    spec:
      family: ipv6
      mode: hash
      hashFields: [sourceAddress]
      sourceCIDRs: [2001:db8:10::/64]
      candidates:
      - targets:
        - interface: wan-a
          gatewaySource: static
          gateway: 2001:db8:100::2
        - interface: wan-b
          gatewaySource: static
          gateway: 2001:db8:200::2
EOF_CONFIG

"$routerd" validate --config "$work/router.yaml" >"$evidence/validate.log" 2>&1
"$routerd" render freebsd --config "$work/router.yaml" --out-dir "$work/render" >"$evidence/render.log" 2>&1
sed 's/gatewaySource: static/gatewaySource: dhcpv6/' "$work/router.yaml" >"$work/dynamic.yaml"
if "$routerd" validate --config "$work/dynamic.yaml" >"$evidence/reject-dynamic.log" 2>&1; then
  echo 'dynamic gateway unexpectedly accepted' >&2; exit 1
fi
grep -F 'static gateway' "$evidence/reject-dynamic.log"
pfctl -nf "$work/render/pf.conf" >"$evidence/pf-nf.log" 2>&1
awk '/route-to-self/{self=NR} /route-to-connected/{connected=NR} /route-to \{/{route=NR} /lan-to-wan-a/{broad=NR} END { exit !(self && connected && route && broad && self < connected && connected < route && route < broad) }' "$work/render/pf.conf"
pfctl -e >"$evidence/pf-enable.log" 2>&1; pf_enabled=1
pfctl -f "$work/render/pf.conf" >"$evidence/pf-load.log" 2>&1
pfctl -sr >"$evidence/pf-rules.log" 2>&1
grep -F 'inet6' "$evidence/pf-rules.log"
grep -F 'round-robin sticky-address' "$evidence/pf-rules.log"

jexec "$sink_a_jail" tcpdump -n -l -i "$out_b" 'ip6 and icmp6' >"$evidence/sink-a.packets.log" 2>&1 & capture_a=$!
jexec "$sink_b_jail" tcpdump -n -l -i "$out2_b" 'ip6 and icmp6' >"$evidence/sink-b.packets.log" 2>&1 & capture_b=$!
sleep 1
jexec "$src_jail" ping6 -n -S 2001:db8:10::10 -c 3 -W 1 2001:db8:ffff::1 >"$evidence/source-10.first.log" 2>&1 || true
jexec "$src_jail" ping6 -n -S 2001:db8:10::11 -c 3 -W 1 2001:db8:ffff::1 >"$evidence/source-11.first.log" 2>&1 || true
jexec "$src_jail" ping6 -n -S 2001:db8:10::10 -c 3 -W 1 2001:db8:ffff::1 >"$evidence/source-10.repeat.log" 2>&1 || true
jexec "$src_jail" ping6 -n -S 2001:db8:10::11 -c 3 -W 1 2001:db8:ffff::1 >"$evidence/source-11.repeat.log" 2>&1 || true
pfctl -ss -v >"$evidence/pf-states.log" 2>&1
kill "$capture_a" "$capture_b"; wait "$capture_a" || true; capture_a=; wait "$capture_b" || true; capture_b=

sed -i '' 's/^/sink-a /' "$evidence/sink-a.packets.log"; sed -i '' 's/^/sink-b /' "$evidence/sink-b.packets.log"
s10a=$(grep -c '^sink-a .*2001:db8:10::10' "$evidence/sink-a.packets.log" || true); s10b=$(grep -c '^sink-b .*2001:db8:10::10' "$evidence/sink-b.packets.log" || true)
s11a=$(grep -c '^sink-a .*2001:db8:10::11' "$evidence/sink-a.packets.log" || true); s11b=$(grep -c '^sink-b .*2001:db8:10::11' "$evidence/sink-b.packets.log" || true)
test $((s10a + s10b)) -ge 6; test $((s11a + s11b)) -ge 6
test "$s10a" -eq 0 -o "$s10b" -eq 0; test "$s11a" -eq 0 -o "$s11b" -eq 0
grep -q '^sink-a .*2001:db8:10::' "$evidence/sink-a.packets.log"; grep -q '^sink-b .*2001:db8:10::' "$evidence/sink-b.packets.log"
grep -F '2001:db8:10::10' "$evidence/pf-states.log"; grep -F '2001:db8:10::11' "$evidence/pf-states.log"; grep -F 'rule 1' "$evidence/pf-states.log"

# The untrust sink must not reach the trust connected network.  This proves
# the connected-zone deny precedes the broad forward/route-to pass.
jexec "$src_jail" tcpdump -n -l -i "$in_b" 'ip6 and icmp6' >"$evidence/blocked.packets.log" 2>&1 & blocked_capture=$!
sleep 1
jexec "$sink_a_jail" ping6 -n -S 2001:db8:100::2 -c 1 -W 1 2001:db8:10::10 >"$evidence/untrust-to-lan.log" 2>&1 || true
sleep 1
kill "$blocked_capture"; wait "$blocked_capture" || true; blocked_capture=
if grep -F '2001:db8:100::2 > 2001:db8:10::10' "$evidence/blocked.packets.log"; then
  echo 'untrust packet reached trust network' >&2; exit 1
fi
{
  printf 'source10 sink-a=%s sink-b=%s\n' "$s10a" "$s10b"
  printf 'source11 sink-a=%s sink-b=%s\n' "$s11a" "$s11b"
  printf 'both-routehosts=1\n'
  printf 'pf-states-source10-source11-rule1=1\n'
  printf 'untrust-to-lan-blocked=1\n'
} >"$evidence/summary.log"
printf 'freebsd-vnet-policyroute=ok\n' >"$evidence/result"
