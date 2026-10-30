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

work=$(mktemp -d /tmp/routerd-freebsd-native-smoke.XXXXXX)
root="$work/root"
render="$work/render"
config="$work/fixture.yaml"
routerd="$work/routerd"
routerctl="$work/routerctl"
own_pf=0
native_fd_dir=/dev/fd
lifecycle_relay_parent=/usr/local/libexec
lifecycle_relay_dir=$lifecycle_relay_parent/routerd
lifecycle_relay=$lifecycle_relay_dir/dhcp-event-relay
native_own_fdescfs=0
lifecycle_own_relay_parent=0
lifecycle_own_relay_dir=0
lifecycle_relay_stage=

prepare_native_descriptors() {
  mount_table=$(mount -p) || return 1
  descriptor_mount=$(printf '%s\n' "$mount_table" | awk -v target="$native_fd_dir" '$2 == target { print $3 }')
  case "$descriptor_mount" in
    ''|fdescfs) ;;
    *) echo "refusing native runtime with a different filesystem on $native_fd_dir: $descriptor_mount" >&2; return 1 ;;
  esac
  [ -d "$native_fd_dir" ] && [ ! -L "$native_fd_dir" ] || return 1
  if [ -z "$descriptor_mount" ]; then
    mount -t fdescfs fdesc "$native_fd_dir" || return 1
    native_own_fdescfs=1
  fi
  (exec 9</dev/null; [ -e "$native_fd_dir/9" ]) || {
    echo "native runtime requires a complete descriptor view at $native_fd_dir" >&2
    return 1
  }
}

prepare_lifecycle_relay() {
  relay_binary=$1
  # Inspect every existing canonical path before creating the relay file.
  for directory in "$lifecycle_relay_parent" "$lifecycle_relay_dir"; do
    if [ -e "$directory" ] || [ -L "$directory" ]; then
      if [ ! -d "$directory" ] || [ -L "$directory" ]; then
        echo "refusing lifecycle runtime with a non-directory relay parent: $directory" >&2
        return 1
      fi
    fi
  done
  if [ -e "$lifecycle_relay" ] || [ -L "$lifecycle_relay" ]; then
    if [ ! -f "$lifecycle_relay" ] || [ -L "$lifecycle_relay" ] || [ ! -x "$lifecycle_relay" ] || ! cmp -s "$relay_binary" "$lifecycle_relay"; then
      echo "refusing to replace an existing DHCP event relay: $lifecycle_relay" >&2
      return 1
    fi
  fi
  if [ ! -e "$lifecycle_relay" ]; then
    if [ ! -d "$lifecycle_relay_parent" ]; then
      mkdir "$lifecycle_relay_parent" || return 1
      lifecycle_own_relay_parent=1
    fi
    if [ ! -d "$lifecycle_relay_dir" ]; then
      mkdir "$lifecycle_relay_dir" || return 1
      lifecycle_own_relay_dir=1
    fi
    lifecycle_relay_stage=$(mktemp "$lifecycle_relay_dir/.dhcp-event-relay.native.XXXXXX") || return 1
    install -m 0755 "$relay_binary" "$lifecycle_relay_stage" || return 1
    # A hard link publishes the completed binary without replacing a collision.
    ln "$lifecycle_relay_stage" "$lifecycle_relay" || return 1
  fi
}

restore_lifecycle_relay() {
  restore_rc=0
  if [ -n "$lifecycle_relay_stage" ]; then
    # FreeBSD sh supports -ef; compare the published file with its private link.
    # shellcheck disable=SC3013
    if [ -e "$lifecycle_relay" ] && [ ! -L "$lifecycle_relay" ] && [ "$lifecycle_relay" -ef "$lifecycle_relay_stage" ] && cmp -s "$relay_binary" "$lifecycle_relay"; then
      rm "$lifecycle_relay" || restore_rc=1
    elif [ -e "$lifecycle_relay" ] || [ -L "$lifecycle_relay" ]; then
      echo "preserving a changed DHCP event relay: $lifecycle_relay" >&2
      restore_rc=1
    fi
    rm "$lifecycle_relay_stage" || restore_rc=1
    lifecycle_relay_stage=
  fi
  if [ "$lifecycle_own_relay_dir" -eq 1 ]; then
    rmdir "$lifecycle_relay_dir" || restore_rc=1
    lifecycle_own_relay_dir=0
  fi
  if [ "$lifecycle_own_relay_parent" -eq 1 ]; then
    rmdir "$lifecycle_relay_parent" || restore_rc=1
    lifecycle_own_relay_parent=0
  fi
  return "$restore_rc"
}

restore_native_descriptors() {
  restore_rc=0
  if [ "$native_own_fdescfs" -eq 1 ]; then
    current_mounts=$(mount -p) || return 1
    current_descriptor_mount=$(printf '%s\n' "$current_mounts" | awk -v target="$native_fd_dir" '$2 == target { print $3 }')
    if [ "$current_descriptor_mount" = fdescfs ]; then
      umount "$native_fd_dir" || restore_rc=1
    else
      echo "preserving a changed descriptor mount: $native_fd_dir" >&2
      restore_rc=1
    fi
    native_own_fdescfs=0
  fi
  return "$restore_rc"
}

cleanup_started=0
cleanup() {
  test_rc=$?
  [ "$cleanup_started" -eq 0 ] || return "$test_rc"
  cleanup_started=1
  set +e
  cleanup_rc=0
  restore_lifecycle_relay >>"$work/cleanup.log" 2>&1 || cleanup_rc=2
  if [ -s "$work/routerd.pid" ]; then
    owned_pid=$(cat "$work/routerd.pid")
    if kill -0 "$owned_pid" 2>/dev/null; then
      kill -TERM "$owned_pid" >>"$work/cleanup.log" 2>&1
      stop_rc=$?
      printf 'routerd_stop_exit=%s\n' "$stop_rc" >>"$work/cleanup.log"
      [ "$stop_rc" -eq 0 ] || cleanup_rc=2
    else
      printf 'routerd_already_stopped=true\n' >>"$work/cleanup.log"
    fi
  fi
  if [ "$own_pf" -eq 1 ]; then
    kldunload pf >>"$work/cleanup.log" 2>&1
    unload_rc=$?
    printf 'pf_unload_exit=%s\n' "$unload_rc" >>"$work/cleanup.log"
    [ "$unload_rc" -eq 0 ] || cleanup_rc=2
  fi
  restore_native_descriptors >>"$work/cleanup.log" 2>&1 || cleanup_rc=2
  printf 'test_exit=%s\ncleanup_exit=%s\n' "$test_rc" "$cleanup_rc" >>"$work/cleanup.log"
  # Save child JSON/logs and cleanup evidence, without archiving helper binaries
  # or fixture credential/config files. The existing CI console artifact collects it.
  archive=/var/tmp/routerd-native-evidence.tar.gz
  list="$work/evidence-files"
  (cd "$work" && find . -type f \( -name '*.json' -o -name '*.jsonl' -o -name '*.log' -o -name '*.meta' -o -name '*.result' -o -name result -o -name cleanup.log \) -print) >"$list"
  archive_rc=$?
  if [ "$archive_rc" -eq 0 ]; then
    tar -czf "$archive" -C "$work" -T "$list"
    archive_rc=$?
  fi
  printf 'evidence_archive=%s archive_exit=%s\n' "$archive" "$archive_rc" >&2
  if [ "$archive_rc" -eq 0 ] && [ "$cleanup_rc" -eq 0 ]; then
    rm -rf "$work"
    remove_rc=$?
    printf 'workdir_remove_exit=%s\n' "$remove_rc" >&2
    [ "$remove_rc" -eq 0 ] || cleanup_rc=2
  else
    echo "native evidence retained at $work" >&2
    cleanup_rc=2
  fi
  [ "$test_rc" -ne 0 ] || test_rc=$cleanup_rc
  exit "$test_rc"
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

prepare_native_descriptors
# This is the native package gate. Do not narrow it to selected packages: a
# FreeBSD-only build or runtime dependency failure must be visible in CI.
go test ./...
go test -tags routerd_native ./pkg/routeradvertisement -run '^TestSolicitationWildcardReceiverOnActualSocket$' -count=1

cat >"$config" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-native-ci-smoke}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: lan}
    spec: {ifname: vtnet0, managed: false}
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
  lifecycle_helper="$work/freebsd-lifecycle-runtime"
  dhcp_event_relay="$work/routerd-dhcp-event-relay"
  go build -o "$dhcpv4_client" ./cmd/routerd-dhcpv4-client
  go build -o "$dhcpv6_client" ./cmd/routerd-dhcpv6-client
  go build -o "$dns_resolver" ./cmd/routerd-dns-resolver
  go build -o "$lifecycle_helper" ./tools/freebsd-lifecycle-runtime
  go build -o "$dhcp_event_relay" ./cmd/routerd-dhcp-event-relay
  prepare_lifecycle_relay "$dhcp_event_relay"
  sh scripts/freebsd-lifecycle-runtime-smoke.sh \
    --dhcpv4-client "$dhcpv4_client" --dhcpv6-client "$dhcpv6_client" --dns-resolver "$dns_resolver" \
    --routerd "$routerd" --lifecycle-helper "$lifecycle_helper" \
    --evidence-dir "$lifecycle_evidence"
  cat "$lifecycle_evidence/summary.log"
  cat "$lifecycle_evidence/result"
  sh scripts/freebsd-dns-resolver-smoke.sh --dns-resolver "$dns_resolver" \
    --lifecycle-helper "$lifecycle_helper" --evidence-dir "$work/dns-resolver-peer"
  cat "$work/dns-resolver-peer/result"
  restore_lifecycle_relay
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

# Package lifecycle replaces binaries/services; run it last and never combine
# this opt-in input with the mutable lifecycle inputs in one native dispatch.
if [ "${ROUTERD_FREEBSD_PACKAGE_LIFECYCLE_RUNTIME:-false}" = true ]; then
  sh scripts/freebsd-package-lifecycle-smoke.sh --source "$(pwd)"
fi
