#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
# Opt-in native acceptance for routerd-owned FreeBSD loader persistence.

set -eu

routerd=
while [ "$#" -gt 0 ]; do
  case "$1" in
  --routerd) routerd=$2; shift 2 ;;
  *) echo "usage: $0 --routerd PATH" >&2; exit 64 ;;
  esac
done
[ -n "$routerd" ] && [ -x "$routerd" ]

work=/var/tmp/routerd-kernelmodule-persistence
loader_dir=/boot/loader.conf.d
owned_loader="$loader_dir/90-routerd-router-runtime.conf"
foreign_loader="$loader_dir/99-routerd-fixture-foreign.conf"
rc_local=/etc/rc.local
rc_backup="$work/rc.local.before"
after_script="$work/after-reboot.sh"
marker="$work/reboot.complete"
scheduled=0
rc_local_existed=0
rc_local_mode=

restore_rc_local() {
  if [ "$rc_local_existed" -eq 1 ]; then
    cp "$rc_backup" "$rc_local"
    chmod "$rc_local_mode" "$rc_local"
  else
    rm -f "$rc_local"
  fi
}

cleanup_before_reboot() {
  rc=$?
  if [ "$scheduled" -eq 0 ]; then
    restore_rc_local 2>/dev/null || true
    rm -f "$foreign_loader" "$after_script" "$marker"
  fi
  exit "$rc"
}
trap cleanup_before_reboot EXIT HUP INT TERM

case "$work" in
/var/tmp/routerd-kernelmodule-persistence) rm -rf "$work" ;;
*) echo "refuse unexpected fixture work path $work" >&2; exit 64 ;;
esac
mkdir -p "$work" "$loader_dir"
cp "$routerd" "$work/routerd"
cat >"$work/router.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-kernelmodule-persistence}
spec:
  resources:
  - apiVersion: firewall.routerd.net/v1alpha1
    kind: FirewallEventLog
    metadata: {name: kernelmodule-persistence}
    spec: {enabled: true}
EOF

if [ -e "$rc_local" ]; then
	rc_local_existed=1
	cp "$rc_local" "$rc_backup"
	rc_local_mode=$(stat -f '%Lp' "$rc_local")
fi
printf '# fixture foreign loader entry\nforeign_fixture_load="YES"\n' >"$foreign_loader"

if kldstat -q -m pflog; then
  echo 'KernelModule persistence fixture requires initially absent pflog' >&2
  exit 75
fi

"$work/routerd" serve --once --controllers kernel-module \
  --config "$work/router.yaml" \
  --state-file "$work/state.db" --status-file "$work/status.json" \
  --socket "$work/api.sock" --status-socket "$work/status.sock" >"$work/apply-1.log" 2>&1
grep -F 'pf_load="YES"' "$owned_loader"
grep -F 'pflog_load="YES"' "$owned_loader"
kldstat -q -m pflog
kldunload pflog >"$work/pflog-unload-before-reboot.log" 2>&1
if kldstat -q -m pflog; then
  echo 'pflog remained loaded before reboot fixture boundary' >&2
  exit 1
fi

cat >"$after_script" <<EOF
#!/bin/sh
set -eu
status_row() {
  target=\$1
  if command -v sqlite3 >/dev/null 2>&1; then
    sqlite3 "$work/state.db" "SELECT status FROM objects WHERE api_version='system.routerd.net/v1alpha1' AND kind='KernelModule' AND name='router-runtime';" >"\$target"
  else
    strings "$work/state.db" | grep -F '"phase":"Applied"' | grep -F '"loaded"' >"\$target"
  fi
  grep -F '"phase":"Applied"' "\$target"
}
if ! kldstat -q -m pflog; then
  echo 'pflog was not loaded from routerd loader drop-in after reboot' >&2
  exit 1
fi
"$work/routerd" serve --once --controllers kernel-module --config "$work/router.yaml" --state-file "$work/state.db" --status-file "$work/status-2.json" --socket "$work/api-2.sock" --status-socket "$work/status-2.sock" >"$work/apply-2.log" 2>&1
status_row "$work/kernel-status-2.json"
grep -F '"changed":false' "$work/kernel-status-2.json"
cat >"$work/empty.yaml" <<'EMPTY'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-kernelmodule-persistence}
spec: {resources: []}
EMPTY
"$work/routerd" serve --once --controllers kernel-module --config "$work/empty.yaml" --state-file "$work/state.db" --status-file "$work/status-3.json" --socket "$work/api-3.sock" --status-socket "$work/status-3.sock" >"$work/remove.log" 2>&1
test ! -e "$owned_loader"
grep -F 'foreign_fixture_load="YES"' "$foreign_loader"
rm -f "$foreign_loader"
if [ -f "$work/rc.local.before" ]; then
  cp "$work/rc.local.before" /etc/rc.local
  chmod "$rc_local_mode" /etc/rc.local
else
  rm -f /etc/rc.local
fi
printf 'freebsd-kernelmodule-persistence=ok\n' >"$work/reboot.complete"
EOF
chmod 700 "$after_script"

{
  printf '\n# routerd-kernelmodule-persistence-fixture\n'
  printf 'if [ -x %s ]; then %s; fi\n' "$after_script" "$after_script"
} >>"$rc_local"
chmod 755 "$rc_local"

scheduled=1
nohup sh -c 'sleep 2; /sbin/reboot' >/dev/null 2>&1 &
printf 'freebsd-kernelmodule-persistence reboot-scheduled\n'
