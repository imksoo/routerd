#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause

# The product bridges an existing FRR bfdd service; these two foreground daemons
# are owned only by this isolated acceptance test.
set -eu
[ "$(uname -s)" = Linux ] || { echo "BFD FRR smoke requires Linux" >&2; exit 1; }
go_bin=${GO:-go}
bfdd=${ROUTERD_FRR_BFDD:-/usr/lib/frr/bfdd}
bgpd=${ROUTERD_FRR_BGPD:-/usr/lib/frr/bgpd}
for tool in "$go_bin" ip ping unshare nsenter mount timeout readlink stat vtysh; do
    command -v "$tool" >/dev/null || { echo "required command is unavailable: $tool" >&2; exit 1; }
done
[ -x "$bfdd" ] || { echo "independent FRR bfdd is unavailable: $bfdd" >&2; exit 1; }
[ -x "$bgpd" ] || { echo "independent FRR bgpd is unavailable: $bgpd" >&2; exit 1; }
repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"
evidence=$(mktemp -d "${TMPDIR:-/tmp}/routerd15-bfd-frr.XXXXXX")
evidence=$(CDPATH='' cd -- "$evidence" && pwd)
printf 'BFD evidence: %s\n' "$evidence"
git rev-parse HEAD > "$evidence/revision"
"$go_bin" version > "$evidence/go-version"
"$bfdd" --version > "$evidence/frr-version"
"$bgpd" --version > "$evidence/bgpd-version"
sha256sum pkg/controller/bfd/controller.go pkg/controller/bfd/native.go \
    pkg/controller/bfd/controller_test.go pkg/controller/bfd/native_recovery_test.go \
    pkg/controller/bfd/native_observation_test.go pkg/controller/bfd/native_review_test.go \
    pkg/controller/bfd/frr_native_linux_test.go \
    pkg/controller/bgp/frr_native_linux_test.go scripts/linux-bfd-frr-smoke.sh > "$evidence/source.sha256"
CGO_ENABLED=0 "$go_bin" test -c -o "$evidence/bfd.test" ./pkg/controller/bfd > "$evidence/compile.log" 2>&1 || {
    status=$?; printf '%s\n' "$status" > "$evidence/compile.exit"; cat "$evidence/compile.log" >&2; exit "$status";
}
printf '0\n' > "$evidence/compile.exit"
CGO_ENABLED=0 "$go_bin" build -o "$evidence/routerd-bgp" ./cmd/routerd-bgp > "$evidence/bgp-build.log" 2>&1 || {
    status=$?; printf '%s\n' "$status" > "$evidence/bgp-build.exit"; cat "$evidence/bgp-build.log" >&2; exit "$status";
}
printf '0\n' > "$evidence/bgp-build.exit"
CGO_ENABLED=0 "$go_bin" test -c -o "$evidence/bgp.test" ./pkg/controller/bgp > "$evidence/bgp-compile.log" 2>&1 || {
    status=$?; printf '%s\n' "$status" > "$evidence/bgp-compile.exit"; cat "$evidence/bgp-compile.log" >&2; exit "$status";
}
printf '0\n' > "$evidence/bgp-compile.exit"
host_netns=$(readlink /proc/self/ns/net)
host_run_device=$(stat -c %d /run)
set +e
# Variables in the script below belong to the isolated child shell.
# shellcheck disable=SC2016
ROUTERD_RUN_NETNS=1 ROUTERD_HOST_NETNS="$host_netns" ROUTERD_FRR_BFDD="$bfdd" \
ROUTERD_FRR_BGPD="$bgpd" \
ROUTERD_HOST_RUN_DEVICE="$host_run_device" ROUTERD_BGP_BIN="$evidence/routerd-bgp" \
ROUTERD_NATIVE_EVIDENCE="$evidence" timeout 270s unshare -Urnm sh -c '
    set -eu
    mount --make-rprivate /
    mount -t sysfs sysfs /sys
    mount -t tmpfs -o mode=0755 tmpfs /run
    mount -t tmpfs -o mode=1777 tmpfs /var/tmp
    mkdir /var/tmp/frr
    # bfdd has no skip_runas option. Keep its real privilege initialization,
    # resolving its VTY group to the only mapped GID in this private mount view.
    printf "root:x:0:\nfrrvty:x:0:root\n" > "$1/group"
    mount --bind "$1/group" /etc/group
    "$1/bfd.test" -test.run "^TestBFDIndependentFRRNativeLinuxLifecycle$" -test.count=1 -test.v -test.timeout=140s
    ROUTERD_BFD_INTEROP=1 "$1/bgp.test" -test.run "^TestBGPIndependentFRRNativeLinuxLifecycle$" -test.count=1 -test.v -test.timeout=110s
' sh "$evidence" > "$evidence/native.log" 2>&1
status=$?
set -e
printf '%s\n' "$status" > "$evidence/native.exit"
cat "$evidence/native.log"
[ "$status" -eq 0 ] || exit "$status"
if ! grep -Fq -- '--- PASS: TestBFDIndependentFRRNativeLinuxLifecycle' "$evidence/native.log"; then
    echo "native BFD acceptance did not execute successfully" >&2
    exit 1
fi
if ! grep -Fq -- '--- PASS: TestBGPIndependentFRRNativeLinuxLifecycle' "$evidence/native.log"; then
    echo "BFD/BGP native acceptance did not execute successfully" >&2
    exit 1
fi
for mode in multihop single-hop; do
    if ! grep -Fq -- "--- PASS: TestBFDIndependentFRRNativeLinuxLifecycle/$mode" "$evidence/native.log"; then
        echo "BFD $mode acceptance did not execute successfully" >&2
        exit 1
    fi
done
printf '1\n' > "$evidence/executed-pass"
