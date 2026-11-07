#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause

# Concrete independent-peer acceptance for the production BGP daemon and FIB.
# All native network changes occur inside the new namespaces owned below.
set -eu

[ "$(uname -s)" = Linux ] || { echo "BGP FRR smoke requires Linux" >&2; exit 1; }
go_bin=${GO:-go}
bgpd=${ROUTERD_FRR_BGPD:-/usr/lib/frr/bgpd}
for tool in "$go_bin" ip ping unshare nsenter mount timeout stat readlink vtysh; do
    command -v "$tool" >/dev/null || { echo "required command is unavailable: $tool" >&2; exit 1; }
done
[ -x "$bgpd" ] || { echo "independent FRR bgpd is unavailable: $bgpd" >&2; exit 1; }
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"
evidence=$(mktemp -d "${TMPDIR:-/tmp}/routerd15-bgp-frr.XXXXXX")
evidence=$(CDPATH= cd -- "$evidence" && pwd)
printf 'BGP evidence: %s\n' "$evidence"
git rev-parse HEAD > "$evidence/revision"
"$go_bin" version > "$evidence/go-version"
"$bgpd" --version > "$evidence/frr-version"
sha256sum pkg/bgp/fib.go pkg/controller/bgp/fib.go pkg/controller/bgp/fib_linux.go \
    pkg/controller/bgp/fib_linux_test.go pkg/controller/bgp/frr_native_linux_test.go \
    pkg/bgpdaemon/policy.go cmd/routerd-bgp/policy_mixed_family_native_test.go \
    cmd/routerd-bgp/main_test.go pkg/controller/bgp/controller_test.go \
    scripts/linux-bgp-frr-smoke.sh > "$evidence/source.sha256"

CGO_ENABLED=0 "$go_bin" build -o "$evidence/routerd-bgp" ./cmd/routerd-bgp > "$evidence/build.log" 2>&1 || {
    status=$?; printf '%s\n' "$status" > "$evidence/build.exit"; cat "$evidence/build.log" >&2; exit "$status";
}
printf '0\n' > "$evidence/build.exit"
CGO_ENABLED=0 "$go_bin" test -c -o "$evidence/bgp.test" ./pkg/controller/bgp > "$evidence/compile.log" 2>&1 || {
    status=$?; printf '%s\n' "$status" > "$evidence/compile.exit"; cat "$evidence/compile.log" >&2; exit "$status";
}
printf '0\n' > "$evidence/compile.exit"
host_netns=$(readlink /proc/self/ns/net)
host_run_device=$(stat -c %d /run)
set +e
ROUTERD_RUN_NETNS=1 ROUTERD_HOST_NETNS="$host_netns" \
ROUTERD_HOST_RUN_DEVICE="$host_run_device" ROUTERD_BGP_BIN="$evidence/routerd-bgp" \
ROUTERD_FRR_BGPD="$bgpd" ROUTERD_NATIVE_EVIDENCE="$evidence" \
timeout 115s unshare -Urnm sh -c '
    mount --make-rprivate / && mount -t sysfs sysfs /sys &&
    mount -t tmpfs -o mode=0755 tmpfs /run &&
    exec "$1/bgp.test" -test.run "^TestBGPIndependentFRRNativeLinuxLifecycle$" -test.count=1 -test.v -test.timeout=110s
' sh "$evidence" > "$evidence/native.log" 2>&1
status=$?
set -e
printf '%s\n' "$status" > "$evidence/native.exit"
cat "$evidence/native.log"
[ "$status" -eq 0 ] || exit "$status"
if ! grep -Fq -- '--- PASS: TestBGPIndependentFRRNativeLinuxLifecycle' "$evidence/native.log"; then
    echo "native BGP acceptance did not execute successfully" >&2
    exit 1
fi
printf '1\n' > "$evidence/executed-pass"
