#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
# Disposable FreeBSD PF ClientPolicy IPv6 packet acceptance.  It refuses an
# enabled or non-empty PF baseline and removes every rule, state, jail, epair,
# and forwarding change before returning.
set -eu

routerd= evidence=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --routerd) routerd=${2:?}; shift 2 ;;
    --evidence-dir) evidence=${2:?}; shift 2 ;;
    *) exit 2 ;;
  esac
done
[ "$(uname -s)" = FreeBSD ] && [ "$(id -u)" -eq 0 ]
[ -x "$routerd" ] && [ -n "$evidence" ]
mkdir -p "$evidence"

tag="g8-$$"; work=$(mktemp -d /tmp/routerd-g8.XXXXXX)
src="${tag}-src"; denied="${tag}-deny"; allowed="${tag}-allow"
in_a= in_b= deny_a= deny_b= allow_a= allow_b=
cap_deny= cap_allow= forwarding= pf_enabled=0 pf_loaded=0
src_created=0; denied_created=0; allowed_created=0

cleanup() {
  rc=$?; cleanup_rc=0
  trap - EXIT INT TERM HUP
  set +e
  [ -n "$cap_deny" ] && kill "$cap_deny" 2>/dev/null
  [ -n "$cap_allow" ] && kill "$cap_allow" 2>/dev/null
  [ -n "$cap_deny" ] && wait "$cap_deny" 2>/dev/null
  [ -n "$cap_allow" ] && wait "$cap_allow" 2>/dev/null
  if [ "$pf_enabled" -eq 1 ]; then
    pfctl -F rules >"$evidence/pf-flush-rules.log" 2>&1 || cleanup_rc=70
    pfctl -F states >"$evidence/pf-flush-states.log" 2>&1 || cleanup_rc=70
    pfctl -sr >"$evidence/pf-rules-after-cleanup.log" 2>&1 || cleanup_rc=70
    pfctl -ss >"$evidence/pf-states-after-cleanup.log" 2>&1 || cleanup_rc=70
    [ ! -s "$evidence/pf-rules-after-cleanup.log" ] || cleanup_rc=70
    [ ! -s "$evidence/pf-states-after-cleanup.log" ] || cleanup_rc=70
    pfctl -d >"$evidence/pf-disable.log" 2>&1 || cleanup_rc=70
  fi
  [ "$src_created" -eq 1 ] && jail -r "$src" >>"$evidence/jail-cleanup.log" 2>&1
  [ "$denied_created" -eq 1 ] && jail -r "$denied" >>"$evidence/jail-cleanup.log" 2>&1
  [ "$allowed_created" -eq 1 ] && jail -r "$allowed" >>"$evidence/jail-cleanup.log" 2>&1
  [ -n "$in_a" ] && ifconfig "$in_a" destroy >>"$evidence/interface-cleanup.log" 2>&1
  [ -n "$deny_a" ] && ifconfig "$deny_a" destroy >>"$evidence/interface-cleanup.log" 2>&1
  [ -n "$allow_a" ] && ifconfig "$allow_a" destroy >>"$evidence/interface-cleanup.log" 2>&1
  [ -n "$forwarding" ] && sysctl net.inet6.ip6.forwarding="$forwarding" >>"$evidence/forwarding-restore.log" 2>&1
  [ "$pf_loaded" -eq 1 ] && kldunload pf >>"$evidence/pf-kldunload.log" 2>&1 || true
  rm -rf "$work"
  [ "$rc" -ne 0 ] || [ "$cleanup_rc" -eq 0 ] || rc=$cleanup_rc
  exit "$rc"
}
trap cleanup EXIT INT TERM HUP

if ! kldstat -m pf >/dev/null 2>&1; then kldload pf >"$evidence/pf-kldload.log" 2>&1; pf_loaded=1; else printf 'pf already loaded\n' >"$evidence/pf-kldload.log"; fi
test -c /dev/pf
[ "$(pfctl -s info 2>/dev/null | awk '/^Status:/ {print $2; exit}')" != Enabled ]
pfctl -sr >"$evidence/pf-rules-before.log" 2>&1; pfctl -ss >"$evidence/pf-states-before.log" 2>&1
[ ! -s "$evidence/pf-rules-before.log" ] && [ ! -s "$evidence/pf-states-before.log" ]

forwarding=$(sysctl -n net.inet6.ip6.forwarding); sysctl net.inet6.ip6.forwarding=1 >"$evidence/forwarding-enable.log"
jail -c name="$src" vnet persist; src_created=1
jail -c name="$denied" vnet persist; denied_created=1
jail -c name="$allowed" vnet persist; allowed_created=1
in_a=$(ifconfig epair create); in_b=${in_a%a}b
deny_a=$(ifconfig epair create); deny_b=${deny_a%a}b
allow_a=$(ifconfig epair create); allow_b=${allow_a%a}b
ifconfig "$in_b" vnet "$src"; ifconfig "$deny_b" vnet "$denied"; ifconfig "$allow_b" vnet "$allowed"
ifconfig "$in_a" inet6 fd00:1::1/64 up
ifconfig "$deny_a" inet6 fd00:2::1/64 up
ifconfig "$allow_a" inet6 2001:db8:3::1/64 up
jexec "$src" ifconfig lo0 up; jexec "$src" ifconfig "$in_b" inet6 fd00:1::10/64 up; jexec "$src" route -6 add default fd00:1::1
jexec "$denied" ifconfig lo0 up; jexec "$denied" ifconfig "$deny_b" inet6 fd00:2::2/64 up
jexec "$allowed" ifconfig lo0 up; jexec "$allowed" ifconfig "$allow_b" inet6 2001:db8:3::2/64 up

cat >"$work/router.yaml" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-clientpolicy-ipv6}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: lan-in}
    spec: {ifname: $in_a, managed: false}
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: deny-net}
    spec: {ifname: $deny_a, managed: false}
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: allow-net}
    spec: {ifname: $allow_a, managed: false}
  - apiVersion: firewall.routerd.net/v1alpha1
    kind: FirewallZone
    metadata: {name: lan}
    spec: {role: trust, interfaces: [lan-in, deny-net, allow-net]}
  - apiVersion: firewall.routerd.net/v1alpha1
    kind: ClientPolicy
    metadata: {name: guest-v6}
    spec:
      mode: include
      interfaces: [lan-in]
      guestEgressDeny: [fd00:2::/64]
      guestEgressAllow: [2001:db8:3::/64]
      classification:
      - name: explicit-dual-stack-guest
        mode: guest
        match: {macs: ['02:00:00:00:00:08']}
        ipv6Addresses: [fd00:1::10]
EOF

"$routerd" validate --config "$work/router.yaml" >"$evidence/validate.log" 2>&1
"$routerd" render freebsd --config "$work/router.yaml" --out-dir "$work/render" >"$evidence/render.log" 2>&1
find "$work/render" -maxdepth 1 -type f -exec basename {} \; | sort >"$evidence/render-files.log"
pfctl -nf "$work/render/pf.conf" >"$evidence/pf-nf.log" 2>&1
pfctl -e >"$evidence/pf-enable.log" 2>&1; pf_enabled=1
pfctl -f "$work/render/pf.conf" >"$evidence/pf-load.log" 2>&1
pfctl -sr -v >"$evidence/pf-rules.log" 2>&1
grep -F 'fd00:1::10' "$evidence/pf-rules.log"
grep -F 'fd00:2::/64' "$evidence/pf-rules.log"

jexec "$denied" tcpdump -n -l -i "$deny_b" icmp6 >"$evidence/denied-sink.log" 2>&1 & cap_deny=$!
jexec "$allowed" tcpdump -n -l -i "$allow_b" icmp6 >"$evidence/allowed-sink.log" 2>&1 & cap_allow=$!
sleep 1
jexec "$src" ping -6 -n -c 3 -W 1000 2001:db8:3::2 >"$evidence/source-allowed.log" 2>&1 || true
jexec "$src" ping -6 -n -c 3 -W 1000 fd00:2::2 >"$evidence/source-denied.log" 2>&1 || true
sleep 1
pfctl -ss -v >"$evidence/pf-states.log" 2>&1
kill "$cap_deny" "$cap_allow"; wait "$cap_deny" || true; cap_deny=; wait "$cap_allow" || true; cap_allow=
allowed_count=$(grep -c 'fd00:1::10' "$evidence/allowed-sink.log" || true)
denied_count=$(grep -c 'fd00:1::10' "$evidence/denied-sink.log" || true)
test "$allowed_count" -ge 3
test "$denied_count" -eq 0
grep -F 'fd00:1::10' "$evidence/pf-states.log"
grep -F 'routerd:client-policy:guest-v6:deny' "$evidence/pf-rules.log"
{
  printf 'allowed-source-fd00:1::10=%s\n' "$allowed_count"
  printf 'denied-source-fd00:1::10=%s\n' "$denied_count"
  printf 'ipv6-clientpolicy-deny-before-icmp6=1\n'
} >"$evidence/summary.log"
printf 'freebsd-clientpolicy-ipv6-dataplane=ok\n' >"$evidence/result"
