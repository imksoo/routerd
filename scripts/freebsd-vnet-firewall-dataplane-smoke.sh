#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Run a production-rendered FreeBSD PF dataplane smoke in disposable VNET
# jails.  This is intentionally FreeBSD-only and root-only: it creates jails,
# epairs, enables PF temporarily, and restores every changed host setting.
set -eu

usage() {
  cat <<'USAGE'
usage: freebsd-vnet-firewall-dataplane-smoke.sh --routerd /absolute/routerd --evidence-dir /absolute/dir

Renders an EgressRoutePolicy using routerd, loads the generated PF rules, and
proves source-affinity with two VNET sources and two egress sinks.  The runner
refuses to run when PF is already enabled because it must not replace a live
host firewall.
USAGE
}

routerd=
evidence=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --routerd)
      routerd=${2:?missing routerd path}
      shift 2
      ;;
    --evidence-dir)
      evidence=${2:?missing evidence directory}
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
done

[ "$(uname -s)" = "FreeBSD" ] || { echo 'FreeBSD is required' >&2; exit 2; }
[ "$(id -u)" -eq 0 ] || { echo 'root is required' >&2; exit 2; }
if [ -z "$routerd" ] || [ ! -x "$routerd" ]; then
  echo 'an executable --routerd is required' >&2
  exit 2
fi
[ -n "$evidence" ] || { echo '--evidence-dir is required' >&2; exit 2; }
case "$routerd:$evidence" in
  /*:/*) ;;
  *) echo 'runner and evidence paths must be absolute' >&2; exit 2 ;;
esac

for command in ifconfig jail jls jexec kldload kldstat kldunload pfctl tcpdump ping sysctl; do
  command -v "$command" >/dev/null 2>&1 || { echo "missing command: $command" >&2; exit 2; }
done

mkdir -p "$evidence"
if [ ! -d "$evidence" ] || [ -L "$evidence" ]; then
  echo 'unsafe evidence directory' >&2
  exit 2
fi

tag="g7-$$"
work=$(mktemp -d "${TMPDIR:-/tmp}/routerd-g7.XXXXXX")
src_jail="${tag}-src"
sink_a_jail="${tag}-sinka"
sink_b_jail="${tag}-sinkb"
in_a=''; in_b=''; out_a=''; out_b=''; out2_a=''; out2_b=''
capture_a=''; capture_b=''
forwarding=
pf_enabled=0
pf_loaded_by_runner=0
src_created=0
sink_a_created=0
sink_b_created=0

cleanup() {
  rc=$?
  cleanup_rc=0
  trap - EXIT INT TERM HUP
  set +e
  [ -n "$capture_a" ] && kill "$capture_a" 2>/dev/null
  [ -n "$capture_b" ] && kill "$capture_b" 2>/dev/null
  [ -n "$capture_a" ] && wait "$capture_a" 2>/dev/null
  [ -n "$capture_b" ] && wait "$capture_b" 2>/dev/null
  if [ "$pf_enabled" -eq 1 ]; then
    pfctl -F rules >>"$evidence/pf-flush-rules.log" 2>&1 || cleanup_rc=70
    pfctl -F states >>"$evidence/pf-flush-states.log" 2>&1 || cleanup_rc=70
    pfctl -sr >>"$evidence/pf-rules-after-cleanup.log" 2>&1 || cleanup_rc=70
    pfctl -ss >>"$evidence/pf-states-after-cleanup.log" 2>&1 || cleanup_rc=70
    [ ! -s "$evidence/pf-rules-after-cleanup.log" ] || cleanup_rc=70
    [ ! -s "$evidence/pf-states-after-cleanup.log" ] || cleanup_rc=70
    pfctl -d >>"$evidence/pf-disable.log" 2>&1 || cleanup_rc=70
    pf_enabled=0
  fi
  if [ "$src_created" -eq 1 ]; then
    jail -r "$src_jail" >>"$evidence/jail-cleanup.log" 2>&1 || cleanup_rc=70
  fi
  if [ "$sink_a_created" -eq 1 ]; then
    jail -r "$sink_a_jail" >>"$evidence/jail-cleanup.log" 2>&1 || cleanup_rc=70
  fi
  if [ "$sink_b_created" -eq 1 ]; then
    jail -r "$sink_b_jail" >>"$evidence/jail-cleanup.log" 2>&1 || cleanup_rc=70
  fi
  if [ -n "$in_a" ]; then
    ifconfig "$in_a" destroy >>"$evidence/interface-cleanup.log" 2>&1 || cleanup_rc=70
  fi
  if [ -n "$out_a" ]; then
    ifconfig "$out_a" destroy >>"$evidence/interface-cleanup.log" 2>&1 || cleanup_rc=70
  fi
  if [ -n "$out2_a" ]; then
    ifconfig "$out2_a" destroy >>"$evidence/interface-cleanup.log" 2>&1 || cleanup_rc=70
  fi
  if [ -n "$forwarding" ]; then
    sysctl net.inet.ip.forwarding="$forwarding" >>"$evidence/forwarding-restore.log" 2>&1 || cleanup_rc=70
  fi
  for name in "$src_jail" "$sink_a_jail" "$sink_b_jail"; do
    if jls -j "$name" >/dev/null 2>&1; then cleanup_rc=70; fi
  done
  for name in "$in_a" "$out_a" "$out2_a"; do
    if [ -n "$name" ] && ifconfig "$name" >/dev/null 2>&1; then cleanup_rc=70; fi
  done
  if [ -n "$forwarding" ]; then
    restored=$(sysctl -n net.inet.ip.forwarding 2>>"$evidence/forwarding-restore.log") || cleanup_rc=70
    [ "$restored" = "$forwarding" ] || cleanup_rc=70
  fi
  if [ "$pf_loaded_by_runner" -eq 1 ]; then
    kldunload pf >>"$evidence/pf-kldunload.log" 2>&1 || cleanup_rc=70
  fi
  if [ "$cleanup_rc" -ne 0 ]; then
    printf 'fixture cleanup or restoration failed\n' >>"$evidence/cleanup-failure.log"
  fi
  rm -rf "$work"
  if [ "$rc" -eq 0 ] && [ "$cleanup_rc" -ne 0 ]; then
    exit "$cleanup_rc"
  fi
  if [ "$rc" -eq 0 ]; then
    printf 'freebsd-vnet-firewall-dataplane=ok\n' >"$evidence/result"
  fi
  exit "$rc"
}
trap cleanup EXIT INT TERM HUP

if ! kldstat -m pf >/dev/null 2>&1; then
  kldload pf >"$evidence/pf-kldload.log" 2>&1
  pf_loaded_by_runner=1
else
  printf 'pf already loaded\n' >"$evidence/pf-kldload.log"
fi
test -c /dev/pf

pf_status=$(pfctl -s info 2>/dev/null | awk '/^Status:/ {print $2; exit}')
[ "$pf_status" != 'Enabled' ] || {
  echo 'refusing to replace an enabled PF ruleset' >&2
  exit 2
}
pfctl -sr >"$evidence/pf-rules-before.log" 2>&1
pfctl -ss >"$evidence/pf-states-before.log" 2>&1
if [ -s "$evidence/pf-rules-before.log" ] || [ -s "$evidence/pf-states-before.log" ]; then
  echo 'refusing to replace a disabled PF ruleset with existing rules or states' >&2
  exit 2
fi

forwarding=$(sysctl -n net.inet.ip.forwarding)
sysctl net.inet.ip.forwarding=1 >"$evidence/forwarding-enable.log"
jail -c name="$src_jail" vnet persist; src_created=1
jail -c name="$sink_a_jail" vnet persist; sink_a_created=1
jail -c name="$sink_b_jail" vnet persist; sink_b_created=1

in_a=$(ifconfig epair create); in_b=${in_a%a}b
out_a=$(ifconfig epair create); out_b=${out_a%a}b
out2_a=$(ifconfig epair create); out2_b=${out2_a%a}b
ifconfig "$in_b" vnet "$src_jail"
ifconfig "$out_b" vnet "$sink_a_jail"
ifconfig "$out2_b" vnet "$sink_b_jail"
ifconfig "$in_a" inet 192.0.2.1/24 up
ifconfig "$out_a" inet 198.51.100.1/24 up
ifconfig "$out2_a" inet 203.0.113.1/24 up
jexec "$src_jail" ifconfig lo0 up
jexec "$src_jail" ifconfig "$in_b" inet 192.0.2.10/24 up
jexec "$src_jail" ifconfig "$in_b" alias 192.0.2.11/24
jexec "$src_jail" route add default 192.0.2.1
jexec "$sink_a_jail" ifconfig lo0 up
jexec "$sink_a_jail" ifconfig "$out_b" inet 198.51.100.2/24 up
jexec "$sink_b_jail" ifconfig lo0 up
jexec "$sink_b_jail" ifconfig "$out2_b" inet 203.0.113.2/24 up
# Both independent VNET peers own the routed destination, so each selected
# egress must return an echo reply through the actual PF/NAT state.
jexec "$sink_a_jail" ifconfig lo0 inet 10.10.10.1/32 alias
jexec "$sink_b_jail" ifconfig lo0 inet 10.10.10.1/32 alias

cat >"$work/router.yaml" <<EOF_CONFIG
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-vnet-firewall-dataplane}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: wan-a}
    spec: {ifname: $out_a, managed: false}
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: wan-b}
    spec: {ifname: $out2_a, managed: false}
  - apiVersion: net.routerd.net/v1alpha1
    kind: EgressRoutePolicy
    metadata: {name: source-affinity}
    spec:
      family: ipv4
      mode: hash
      hashFields: [sourceAddress]
      sourceCIDRs: [192.0.2.0/24]
      excludeDestinationCIDRs: [198.51.100.0/24]
      candidates:
      - targets:
        - interface: wan-a
          gatewaySource: static
          gateway: 198.51.100.2
        - interface: wan-b
          gatewaySource: static
          gateway: 203.0.113.2
  - apiVersion: net.routerd.net/v1alpha1
    kind: NAT44Rule
    metadata: {name: source-nat-wan-a}
    spec:
      outboundInterface: wan-a
      sourceCIDRs: [192.0.2.0/24]
      translation: {type: interfaceAddress}
  - apiVersion: net.routerd.net/v1alpha1
    kind: NAT44Rule
    metadata: {name: source-nat-wan-b}
    spec:
      outboundInterface: wan-b
      sourceCIDRs: [192.0.2.0/24]
      translation: {type: interfaceAddress}
EOF_CONFIG

"$routerd" validate --config "$work/router.yaml" >"$evidence/validate.log" 2>&1
"$routerd" render freebsd --config "$work/router.yaml" --out-dir "$work/render" >"$evidence/render.log" 2>&1
pfctl -nf "$work/render/pf.conf" >"$evidence/pf-nf.log" 2>&1
pfctl -e >"$evidence/pf-enable.log" 2>&1; pf_enabled=1
pfctl -f "$work/render/pf.conf" >"$evidence/pf-load.log" 2>&1
pfctl -sr >"$evidence/pf-rules.log" 2>&1
pfctl -sn >"$evidence/pf-nat-rules.log" 2>&1
grep -F 'route-to {' "$evidence/pf-rules.log"
grep -F 'round-robin sticky-address' "$evidence/pf-rules.log"
cat "$evidence/pf-nat-rules.log"
require_nat44_interface_translation() {
  interface=$1
  awk -v interface="$interface" '
    $1 == "nat" && $2 == "on" && $3 == interface {
      source = 0
      target = 0
      for (i = 4; i <= NF; i++) {
        if ($i == "from" && i < NF && $(i + 1) == "192.0.2.0/24") {
          source = 1
        }
        if ($i == "->") {
          for (j = i + 1; j <= NF; j++) {
            if (index($j, interface) != 0) {
              target = 1
            }
          }
        }
      }
      if (source && target) {
        found = 1
      }
    }
    END { exit(found ? 0 : 1) }
  ' "$evidence/pf-nat-rules.log"
}
require_nat44_interface_translation "$out_a"
require_nat44_interface_translation "$out2_a"

jexec "$sink_a_jail" tcpdump -n -l -i "$out_b" 'icmp and icmp[icmptype] = icmp-echo' >"$evidence/sink-a.packets.log" 2>&1 & capture_a=$!
jexec "$sink_b_jail" tcpdump -n -l -i "$out2_b" 'icmp and icmp[icmptype] = icmp-echo' >"$evidence/sink-b.packets.log" 2>&1 & capture_b=$!
sleep 1
jexec "$src_jail" ping -n -S 192.0.2.10 -c 3 -i 0.2 -W 1000 10.10.10.1 >"$evidence/source-10.first.log" 2>&1
jexec "$src_jail" ping -n -S 192.0.2.11 -c 3 -i 0.2 -W 1000 10.10.10.1 >"$evidence/source-11.first.log" 2>&1
jexec "$src_jail" ping -n -S 192.0.2.10 -c 3 -i 0.2 -W 1000 10.10.10.1 >"$evidence/source-10.repeat.log" 2>&1
jexec "$src_jail" ping -n -S 192.0.2.11 -c 3 -i 0.2 -W 1000 10.10.10.1 >"$evidence/source-11.repeat.log" 2>&1
pfctl -ss -v >"$evidence/pf-states.log" 2>&1
for log in source-10.first source-11.first source-10.repeat source-11.repeat; do
  grep -F '3 packets transmitted, 3 packets received' "$evidence/$log.log"
done
# A successful echo may arrive before BPF releases its final capture buffer.
# Observe the required peer evidence before stopping the capture processes.
for _ in 1 2 3 4 5; do
  a=$(grep -c '198\.51\.100\.1 > 10\.10\.10\.1: ICMP echo request' "$evidence/sink-a.packets.log" || true)
  b=$(grep -c '203\.0\.113\.1 > 10\.10\.10\.1: ICMP echo request' "$evidence/sink-b.packets.log" || true)
  [ "$a" -ge 6 ] && [ "$b" -ge 6 ] && break
  kill -0 "$capture_a"; kill -0 "$capture_b"
  sleep 1
done
kill "$capture_a" "$capture_b"
wait "$capture_a" || true; capture_a=
wait "$capture_b" || true; capture_b=

sed -i '' 's/^/sink-a /' "$evidence/sink-a.packets.log"
sed -i '' 's/^/sink-b /' "$evidence/sink-b.packets.log"
nat_a=$(grep -c '^sink-a .*198\.51\.100\.1' "$evidence/sink-a.packets.log" || true)
nat_b=$(grep -c '^sink-b .*203\.0\.113\.1' "$evidence/sink-b.packets.log" || true)
test "$nat_a" -ge 6
test "$nat_b" -ge 6
if grep -q '^sink-[ab] .*192\.0\.2\.' "$evidence/sink-a.packets.log" "$evidence/sink-b.packets.log"; then
  echo 'NAT44 source address leaked to egress sink' >&2
  exit 1
fi
grep -F '192.0.2.10' "$evidence/pf-states.log"
grep -F '192.0.2.11' "$evidence/pf-states.log"
grep -F 'rule 1' "$evidence/pf-states.log"
# Two separate requests from each original source must retain one translated
# egress, while the two sources exercise different egress peers.
awk '
  $1 == "all" && $2 == "icmp" && $4 ~ /^\(192[.]0[.]2[.](10|11):/ {
    source = $4
    sub(/^\(/, "", source)
    sub(/:[0-9]+\)$/, "", source)
    egress = $3
    sub(/:.*/, "", egress)
    if (selected[source] != "" && selected[source] != egress) { bad = 1 }
    selected[source] = egress
    count[source]++
  }
  END {
    exit (bad || count["192.0.2.10"] != 2 || count["192.0.2.11"] != 2 ||
      selected["192.0.2.10"] == selected["192.0.2.11"]) ? 1 : 0
  }
' "$evidence/pf-states.log"
{
  printf 'sink-a translated-source=%s\n' "$nat_a"
  printf 'sink-b translated-source=%s\n' "$nat_b"
  printf 'both-routehosts=1\n'
  printf 'nat44-egress-source-translation=1\n'
  printf 'pf-states-source10-source11-rule1=1\n'
  printf 'independent-peer-echo-replies=12\n'
  printf 'source-affinity-across-repeated-requests=1\n'
} >"$evidence/summary.log"
