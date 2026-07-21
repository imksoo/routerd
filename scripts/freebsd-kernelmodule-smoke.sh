#!/bin/sh
set -eu

media=/var/tmp/g9media
root=/var/tmp/g9root
ev=/var/tmp/g9evidence
preloaded=0

cleanup() {
  rc=$?
  if [ "$preloaded" -eq 0 ] && kldstat -q -m pf; then
    kldunload pf >>"$ev/pf-cleanup.log" 2>&1 || rc=70
  fi
  mount | grep -F "on $media " >/dev/null 2>&1 && umount "$media" >>"$ev/media-cleanup.log" 2>&1 || true
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

status_row() {
  target=$1
  if command -v sqlite3 >/dev/null 2>&1; then
    sqlite3 "$root/state.db" "SELECT status FROM objects WHERE api_version='system.routerd.net/v1alpha1' AND kind='KernelModule' AND name='router-runtime';" >"$target"
  else
    strings "$root/state.db" | grep -F '"phase":"Applied"' | grep -F '"loaded":["pf"]' >"$target"
  fi
  grep -F '"phase":"Applied"' "$target"
  grep -F '"loaded":["pf"]' "$target"
}

mkdir -p "$media" "$root" "$ev"
mount -t cd9660 -o ro /dev/cd0 "$media"
(cd "$media" && sha256sum -c SHA256SUMS) >"$ev/checksum.log" 2>&1
cp "$media/routerd" "$media/fixture.yaml" "$ev/"

if kldstat -q -m pf; then
  printf 'preloaded\n' >"$ev/pf-before"
  preloaded=1
else
  printf 'absent\n' >"$ev/pf-before"
fi
[ "$preloaded" -eq 0 ] || {
  printf 'fixture requires an initially absent pf module; preserving pre-existing module\n' >>"$ev/runner.stderr"
  exit 75
}

"$ev/routerd" serve --once --controllers kernel-module \
  --config "$ev/fixture.yaml" \
  --state-file "$root/state.db" \
  --status-file "$root/status.json" \
  --socket "$root/api.sock" \
  --status-socket "$root/status.sock" >"$ev/serve-1.log" 2>&1
kldstat -q -m pf
kldstat -m pf >"$ev/pf-after"
status_row "$ev/kernel-status-1.json"
grep -F '"changed":true' "$ev/kernel-status-1.json"

"$ev/routerd" serve --once --controllers kernel-module \
  --config "$ev/fixture.yaml" \
  --state-file "$root/state.db" \
  --status-file "$root/status.json" \
  --socket "$root/api-2.sock" \
  --status-socket "$root/status-2.sock" >"$ev/serve-2.log" 2>&1
status_row "$ev/kernel-status-2.json"
grep -F '"changed":false' "$ev/kernel-status-2.json"
printf 'freebsd-kernelmodule=ok\n' >"$ev/result"
