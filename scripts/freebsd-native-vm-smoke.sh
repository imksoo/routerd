#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause

set -eu

expected_arch=${ROUTERD_FREEBSD_EXPECTED_ARCH:-x86_64}
freebsd_release=$(freebsd-version -u)
case "$(uname -s)" in
FreeBSD) ;;
*)
  echo "native FreeBSD smoke must run in FreeBSD" >&2
  exit 1
  ;;
esac
case "$freebsd_release" in
14.3-*) ;;
*)
  echo "expected FreeBSD 14.3, got $freebsd_release" >&2
  exit 1
  ;;
esac
[ "$(go env GOOS)" = "freebsd" ]
case "$expected_arch" in
x86_64)
  [ "$(uname -m)" = amd64 ]
  [ "$(go env GOARCH)" = amd64 ]
  ;;
aarch64)
  case "$(uname -m)" in arm64|aarch64) ;; *) exit 1 ;; esac
  [ "$(go env GOARCH)" = arm64 ]
  ;;
*)
  echo "unsupported expected FreeBSD architecture: $expected_arch" >&2
  exit 1
  ;;
esac
pkg info -e go
pkg info -e dnsmasq
pkg info -e git
pkg info -e hs-ShellCheck
pkg info -e curl
pkg info -e jq
printf 'freebsd-native-runtime expected=%s arch=%s release=%s goarch=%s\n' \
  "$expected_arch" "$(uname -m)" "$freebsd_release" "$(go env GOARCH)"
git config --global --add safe.directory "$(pwd)"
# The action shares a checkout into the guest. Test fixtures build temporary
# helper binaries, so suppress VCS stamping there without narrowing the gate.
export GOFLAGS="${GOFLAGS:+$GOFLAGS }-buildvcs=false"

# This is the native package gate. Do not narrow it to selected packages: a
# FreeBSD-only build or runtime dependency failure must be visible in CI.
go test ./...

work=$(mktemp -d /tmp/routerd-freebsd-native-smoke.XXXXXX)
root="$work/root"
render="$work/render"
config="$work/fixture.yaml"
routerd="$work/routerd"
routerctl="$work/routerctl"
own_pf=0

cleanup() {
  if [ -s "$work/routerd.pid" ]; then
    kill -TERM "$(cat "$work/routerd.pid")" 2>/dev/null || true
  fi
  if [ "$own_pf" -eq 1 ]; then
    kldunload pf
  fi
  rm -rf "$work"
}
trap cleanup EXIT HUP INT TERM

cat >"$config" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-native-ci-smoke}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: lan}
    spec: {ifname: vtnet0, managed: false, role: trust}
  - apiVersion: net.routerd.net/v1alpha1
    kind: IPv4StaticAddress
    metadata: {name: lan-v4}
    spec: {interface: lan, address: 192.0.2.1/24}
  - apiVersion: net.routerd.net/v1alpha1
    kind: DHCPv4Server
    metadata: {name: lan-dhcp}
    spec: {interface: lan, rangeStart: 192.0.2.10, rangeEnd: 192.0.2.20, router: 192.0.2.1, leaseTime: 1h}
  - apiVersion: firewall.routerd.net/v1alpha1
    kind: FirewallZone
    metadata: {name: lan}
    spec: {role: trust, interfaces: [lan]}
  - apiVersion: firewall.routerd.net/v1alpha1
    kind: FirewallPolicy
    metadata: {name: default}
    spec: {}
EOF

go build -o "$routerd" ./cmd/routerd
go build -o "$routerctl" ./cmd/routerctl

mkdir -p "$root/etc/routerd"
cp "$config" "$root/etc/routerd/router.yaml"
daemon -p "$work/routerd.pid" -o "$work/routerd.log" \
  "$routerd" serve --sandbox --root "$root" --config "$root/etc/routerd/router.yaml" \
  --state-file "$root/state.db" --status-file "$root/status.json" \
  --socket "$root/api.sock" --status-socket "$root/status.sock" --controllers all

ready=0
for _ in $(jot 20); do
  if [ -S "$root/api.sock" ] && [ -S "$root/status.sock" ]; then
    ready=1
    break
  fi
  sleep 1
done
[ "$ready" -eq 1 ] || {
  cat "$work/routerd.log" >&2
  exit 1
}

"$routerctl" validate --socket "$root/status.sock" -f "$config" --replace
"$routerctl" plan --socket "$root/status.sock" -f "$config" --replace
"$routerd" render freebsd --config "$config" --out-dir "$render"

if ! kldstat -q -m pf; then
  kldload pf
  own_pf=1
fi
pfctl -nf "$render/pf.conf"
dnsmasq --test --conf-file="$render/dnsmasq.conf"

set -- "$render"/rc.d-*
[ -f "$1" ]
for script in "$@"; do
  sh -n "$script"
  set +e
  sh "$script" status
  rc=$?
  set -e
  [ "$rc" -eq 0 ] || [ "$rc" -eq 1 ] || exit "$rc"
done

ndpi_agent="$work/routerd-ndpi-agent-libndpi"
CGO_ENABLED=1 go test -tags libndpi ./cmd/routerd-ndpi-agent
CGO_ENABLED=1 go build -tags libndpi -o "$ndpi_agent" ./cmd/routerd-ndpi-agent
"$ndpi_agent" selftest | tee "$work/ndpi-selftest.json"
jq -e '.ok == true and .libndpiLoaded == true and (.libndpiVersion | length > 0)' \
  "$work/ndpi-selftest.json" >/dev/null
echo "freebsd-native-libndpi=ok"

sh scripts/freebsd-native-observer-smoke.sh
sh scripts/freebsd-bgp-ipv6-fib-smoke.sh

if [ "${ROUTERD_FREEBSD_CLIENTPOLICY_IDENTITY_RUNTIME:-false}" = true ]; then
  clientpolicy_evidence="$work/clientpolicy-identity-vnet"
  sh scripts/freebsd-clientpolicy-ipv6-smoke.sh --routerd "$routerd" --evidence-dir "$clientpolicy_evidence"
  cat "$clientpolicy_evidence/summary.log"
  cat "$clientpolicy_evidence/result"
fi

firewall_evidence="$work/firewall-vnet"
sh scripts/freebsd-vnet-firewall-dataplane-smoke.sh --routerd "$routerd" --evidence-dir "$firewall_evidence"
cat "$firewall_evidence/summary.log"
cat "$firewall_evidence/result"

policyroute_evidence="$work/policyroute-vnet"
sh scripts/freebsd-vnet-policyroute-smoke.sh --routerd "$routerd" --evidence-dir "$policyroute_evidence"
cat "$policyroute_evidence/summary.log"
cat "$policyroute_evidence/result"

if [ "${ROUTERD_IPV6_ROUTE_TO_CONSOLE_CANDIDATE:-false}" = true ]; then
  candidate_evidence="$work/ipv6-route-to-console-candidate"
  sh scripts/freebsd-vnet-ipv6-route-to-console-candidate.sh \
    --source "$(pwd)" --evidence-dir "$candidate_evidence"
  cat "$candidate_evidence/summary.log"
  cat "$candidate_evidence/result"
fi

if [ "${ROUTERD_FREEBSD_TUNNELINTERFACE_RUNTIME:-false}" = true ]; then
  sh scripts/freebsd-tunnelinterface-smoke.sh --routerd "$routerd"
fi

ipsec_evidence="$work/ipsec-linux-peer"
sh scripts/freebsd-ipsec-linux-peer-smoke.sh --routerd "$routerd" --evidence-dir "$ipsec_evidence"
cat "$ipsec_evidence/summary.log"
cat "$ipsec_evidence/result"

if [ "${ROUTERD_FREEBSD_KERNELMODULE_PERSISTENCE_RUNTIME:-false}" = true ]; then
  sh scripts/freebsd-kernelmodule-persistence-smoke.sh --routerd "$routerd"
fi

if [ "${ROUTERD_FREEBSD_LIFECYCLE_RUNTIME:-false}" = true ]; then
  lifecycle_evidence="$work/lifecycle-runtime"
  dhcpv4_client="$work/routerd-dhcpv4-client"
  dhcpv6_client="$work/routerd-dhcpv6-client"
  dns_resolver="$work/routerd-dns-resolver"
  go build -o "$dhcpv4_client" ./cmd/routerd-dhcpv4-client
  go build -o "$dhcpv6_client" ./cmd/routerd-dhcpv6-client
  go build -o "$dns_resolver" ./cmd/routerd-dns-resolver
  sh scripts/freebsd-lifecycle-runtime-smoke.sh \
    --dhcpv4-client "$dhcpv4_client" --dhcpv6-client "$dhcpv6_client" --dns-resolver "$dns_resolver" \
    --routerd "$routerd" \
    --evidence-dir "$lifecycle_evidence"
  cat "$lifecycle_evidence/summary.log"
  cat "$lifecycle_evidence/result"
fi

if [ "${ROUTERD_FREEBSD_PPPOE_RUNTIME:-false}" = true ]; then
  pppoe_evidence="$work/pppoe-runtime"
  pppoe_client="$work/routerd-pppoe-client"
  go build -o "$pppoe_client" ./cmd/routerd-pppoe-client
  sh scripts/freebsd-pppoe-runtime-smoke.sh --pppoe-client "$pppoe_client" --evidence-dir "$pppoe_evidence"
  cat "$pppoe_evidence/summary.log"
  cat "$pppoe_evidence/result"
fi

if [ "${ROUTERD_FREEBSD_WIREGUARD_VXLAN_RUNTIME:-false}" = true ]; then
  wireguard_vxlan_evidence="$work/wireguard-vxlan-runtime"
  sh scripts/freebsd-wireguard-vxlan-runtime-smoke.sh --routerd "$routerd" --evidence-dir "$wireguard_vxlan_evidence"
  cat "$wireguard_vxlan_evidence/summary.log"
  cat "$wireguard_vxlan_evidence/result"
fi

if [ "${ROUTERD_FREEBSD_TAILSCALE_BOUNDARY_RUNTIME:-false}" = true ]; then
  tailscale_evidence="$work/tailscale-boundary"
  /usr/bin/timeout -k 2 180 sh scripts/freebsd-tailscale-boundary-smoke.sh --routerd "$routerd" --evidence-dir "$tailscale_evidence"
  cat "$tailscale_evidence/summary.log"
  cat "$tailscale_evidence/result"
fi

if [ "${ROUTERD_FREEBSD_CARP_RUNTIME:-false}" = true ]; then
  carp_evidence="$work/carp-runtime"
  sh scripts/freebsd-carp-runtime-smoke.sh --routerd "$routerd" --evidence-dir "$carp_evidence"
  cat "$carp_evidence/summary.log"
  cat "$carp_evidence/result"
fi

if [ "${ROUTERD_FREEBSD_SAM_DATAPLANE_RUNTIME:-false}" = true ]; then
  sam_evidence="$work/sam-dataplane-runtime"
  sh scripts/freebsd-sam-dataplane-smoke.sh --routerd "$routerd" --routerctl "$routerctl" --evidence-dir "$sam_evidence"
  cat "$sam_evidence/summary.log"
  cat "$sam_evidence/result"
fi

# Package lifecycle replaces binaries/services; run it last and never combine
# this opt-in input with the mutable lifecycle inputs in one native dispatch.
if [ "${ROUTERD_FREEBSD_PACKAGE_LIFECYCLE_RUNTIME:-false}" = true ]; then
  sh scripts/freebsd-package-lifecycle-smoke.sh --source "$(pwd)"
fi
