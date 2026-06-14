#!/usr/bin/env sh
# SPDX-License-Identifier: BSD-3-Clause

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
check="${SCRIPT_DIR}/check-tar-safe-paths.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/routerd-tar-safe-test.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

mkdir -p "$work/src/bin"
printf 'ok\n' > "$work/src/bin/routerd"
tar -C "$work/src" -czf "$work/safe.tar.gz" .
"$check" "$work/safe.tar.gz"

mkdir -p "$work/regular-arrow"
printf 'ok\n' > "$work/regular-arrow/file -> tmp"
tar -C "$work/regular-arrow" -czf "$work/regular-arrow.tar.gz" .
"$check" "$work/regular-arrow.tar.gz"

mkdir -p "$work/top-tmp/tmp"
printf 'bad\n' > "$work/top-tmp/tmp/routerd"
tar -C "$work/top-tmp" -czf "$work/top-tmp.tar.gz" .
if "$check" "$work/top-tmp.tar.gz" >/dev/null 2>&1; then
	echo "expected top-level tmp archive to fail" >&2
	exit 1
fi

mkdir -p "$work/link-abs"
abs_tmp="/tmp"
ln -s "$abs_tmp" "$work/link-abs/stage"
tar -C "$work/link-abs" -czf "$work/link-abs.tar.gz" .
if "$check" "$work/link-abs.tar.gz" >/dev/null 2>&1; then
	echo "expected absolute symlink target archive to fail" >&2
	exit 1
fi

mkdir -p "$work/link-parent"
ln -s ../tmp "$work/link-parent/stage"
tar -C "$work/link-parent" -czf "$work/link-parent.tar.gz" .
if "$check" "$work/link-parent.tar.gz" >/dev/null 2>&1; then
	echo "expected parent-traversal symlink target archive to fail" >&2
	exit 1
fi

mkdir -p "$work/safe-hardlink"
printf 'ok\n' > "$work/safe-hardlink/original"
ln "$work/safe-hardlink/original" "$work/safe-hardlink/copy"
tar -C "$work/safe-hardlink" -czf "$work/safe-hardlink.tar.gz" .
"$check" "$work/safe-hardlink.tar.gz"

if command -v python3 >/dev/null 2>&1; then
	python3 - "$work/unsafe-hardlink-target.tar.gz" <<'PY'
import sys
import tarfile

with tarfile.open(sys.argv[1], "w:gz") as archive:
    info = tarfile.TarInfo("safe-hardlink-name")
    info.type = tarfile.LNKTYPE
    info.linkname = "/tmp/routerd-install"
    archive.addfile(info)
PY
	if "$check" "$work/unsafe-hardlink-target.tar.gz" >/dev/null 2>&1; then
		echo "expected unsafe hardlink target archive to fail" >&2
		exit 1
	fi
fi
