#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
set -eu

usage() {
  cat <<'USAGE'
usage: scripts/firewall-parity-smoke.sh [--linux-rules <file>] [--freebsd-rules <file>]

Checks that rendered Linux nftables and FreeBSD pf firewall rules contain the
semantic anchors routerd relies on for the 3-role firewall model. When a rules
file is omitted, the matching live command is used if available.
USAGE
}

linux_rules=""
freebsd_rules=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --linux-rules)
      linux_rules=$2
      shift 2
      ;;
    --freebsd-rules)
      freebsd_rules=$2
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

linux_out=$tmpdir/linux.rules
freebsd_out=$tmpdir/freebsd.rules

if [ -n "$linux_rules" ]; then
  cp "$linux_rules" "$linux_out"
elif command -v nft >/dev/null 2>&1; then
  nft list ruleset >"$linux_out" 2>/dev/null || : >"$linux_out"
else
  : >"$linux_out"
fi

if [ -n "$freebsd_rules" ]; then
  cp "$freebsd_rules" "$freebsd_out"
elif command -v pfctl >/dev/null 2>&1; then
  pfctl -sr >"$freebsd_out" 2>/dev/null || : >"$freebsd_out"
else
  : >"$freebsd_out"
fi

check_file() {
  label=$1
  file=$2
  shift 2
  if [ ! -s "$file" ]; then
    echo "skip $label: no rules supplied"
    return 0
  fi
  for pattern in "$@"; do
    if ! grep -Eq "$pattern" "$file"; then
      echo "$label missing semantic anchor: $pattern" >&2
      echo "--- $label rules ---" >&2
      sed -n '1,160p' "$file" >&2
      exit 1
    fi
  done
  echo "ok $label"
}

check_file linux "$linux_out" \
  'ct state.*(established|related)|ct state \{ established, related \}' \
  'iifname|meta iifname' \
  'drop|policy drop'

check_file freebsd "$freebsd_out" \
  '^block drop all' \
  'pass quick on lo0' \
  'pass out quick all' \
  'proto ipv6-icmp' \
  'routerd:.*(lan|mgmt|wan|client-policy)'
