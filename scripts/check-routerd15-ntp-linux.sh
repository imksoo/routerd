#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
# Opt-in actual-controller NTP checks; chronyd -x never controls the shared clock.
set -eu
repo=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo"
PATH="$PATH:/usr/sbin:/sbin"
export PATH
for tool in go chronyd chronyc unshare mount ip sha256sum; do command -v "$tool" >/dev/null; done
work=$(mktemp -d "${TMPDIR:-/tmp}/routerd15-ntp-native.XXXXXX")
printf 'native NTP evidence: %s\n' "$work"
go version >"$work/go-version.txt"
git rev-parse HEAD >"$work/revision.txt"
git status --porcelain >"$work/working-tree.txt"
sources='pkg/controller/chain/ntp.go pkg/controller/chain/ntp_config_owner.go pkg/controller/chain/ntp_test.go pkg/controller/chain/ntp_lifecycle_test.go pkg/controller/chain/ntp_config_review_test.go pkg/controller/chain/ntp_native_linux_test.go scripts/check-routerd15-ntp-linux.sh'
# The literal list contains only repository-owned paths without whitespace.
# shellcheck disable=SC2086
sha256sum $sources >"$work/source-before.sha256"
go test -c -o "$work/chain.test" ./pkg/controller/chain >"$work/build.log" 2>&1
host_ns=$(readlink /proc/self/ns/net)
host_mnt=$(readlink /proc/self/ns/mnt)
result=0
ROUTERD_RUN_NETNS=1 ROUTERD_NATIVE_HOST_NS="$host_ns" ROUTERD_NATIVE_HOST_MNT="$host_mnt" unshare -Urnm "$work/chain.test" -test.run '^TestNTPChronyNativeLinuxControllerLifecycle$' -test.v -test.count=1 -test.timeout=2m >"$work/native.log" 2>&1 || result=$?
printf '%s\n' "$result" >"$work/native.exit"
# shellcheck disable=SC2086
sha256sum $sources >"$work/source-after.sha256"
cmp "$work/source-before.sha256" "$work/source-after.sha256"
cat "$work/native.log"
exit "$result"
