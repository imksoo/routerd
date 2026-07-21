#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# FreeBSD PF route-to is certified only for the IPv4 static source-affinity
# shape. FreeBSD 14.3 native IPv6 route-to dataplane evidence is unsafe to
# claim after the isolated native run lost control at dataplane start.
set -eu

usage() {
  echo 'usage: freebsd-vnet-policyroute-smoke.sh --routerd /absolute/routerd --evidence-dir /absolute/dir' >&2
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
mkdir -p "$evidence"

cat >"$evidence/ipv6-route-to.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-ipv6-route-to-rejection}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: wan-a}
    spec: {ifname: vtnet0, managed: false}
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: wan-b}
    spec: {ifname: vtnet1, managed: false}
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
EOF

if "$routerd" validate --config "$evidence/ipv6-route-to.yaml" >"$evidence/validate.log" 2>&1; then
  echo 'FreeBSD IPv6 route-to was unexpectedly accepted' >&2
  exit 1
fi
grep -F 'FreeBSD route-to supports only family ipv4' "$evidence/validate.log"
printf 'ipv6-route-to=explicitly-rejected\n' >"$evidence/summary.log"
printf 'freebsd-ipv6-route-to=explicitly-rejected\n' >"$evidence/result"
