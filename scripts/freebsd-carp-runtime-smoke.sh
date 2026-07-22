#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Exercise the current routerd CARP rc.d artifact on a disposable FreeBSD
# epair.  The peer is intentionally not configured with the same VHID: the
# one-node CARP role is observed locally, while foreign-interface collisions
# fail before any host mutation.
set -eu

routerd=
evidence_dir=
while [ "$#" -gt 0 ]; do
  case "$1" in
  --routerd) routerd=$2; shift 2 ;;
  --evidence-dir) evidence_dir=$2; shift 2 ;;
  *) echo "usage: $0 --routerd PATH --evidence-dir DIR" >&2; exit 2 ;;
  esac
done
[ -x "$routerd" ]
[ -n "$evidence_dir" ]
[ "$(uname -s)" = FreeBSD ]

mkdir -p "$evidence_dir"
work=$(mktemp -d /var/tmp/routerd-carp-runtime.XXXXXX)
owned_if=
rcd_script=
rcd_installed=0
module_preloaded=0

cleanup() {
  rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "freebsd-carp-runtime failure evidence:" >&2
    for log in "$evidence_dir"/*; do
      [ -f "$log" ] || continue
      echo "--- $log" >&2
      cat "$log" >&2 || true
    done
  fi
  if [ "$rcd_installed" -eq 1 ]; then
    env routerd_carp_enable=YES "$rcd_script" onestop >>"$evidence_dir/carp-cleanup-stop.log" 2>&1 || rc=1
    rm -f "$rcd_script"
  fi
  if [ -n "$owned_if" ] && ifconfig "$owned_if" >/dev/null 2>&1; then
    ifconfig "$owned_if" destroy >>"$evidence_dir/cleanup.log" 2>&1 || rc=1
  fi
  # CARP may be preloaded by the guest. This fixture never unloads it.
  printf 'carp_module_preloaded=%s\n' "$module_preloaded" >>"$evidence_dir/cleanup.log"
  rm -rf "$work"
  exit "$rc"
}
trap cleanup EXIT HUP INT TERM

if kldstat -m carp >/dev/null 2>&1; then
  module_preloaded=1
fi

owned_if=$(ifconfig epair create)
case "$owned_if" in epair*a) ;; *) echo "unexpected epair name: $owned_if" >&2; exit 1 ;; esac
peer_if=${owned_if%a}b
ifconfig "$owned_if" inet 198.18.232.1/24 up
ifconfig "$peer_if" inet 198.18.232.2/24 up

cat >"$work/router.yaml" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: lifecycle-carp
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lifecycle-lan
      spec:
        ifname: $owned_if
        managed: false
        owner: external
    - apiVersion: net.routerd.net/v1alpha1
      kind: VirtualAddress
      metadata:
        name: lifecycle-carp-vip
      spec:
        family: ipv4
        interface: lifecycle-lan
        address: 198.18.232.100/24
        mode: vrrp
        vrrp:
          virtualRouterID: 232
          priority: 150
          advertInterval: 1s
          authentication: lifecycle-carp-secret
EOF

"$routerd" render freebsd --config "$work/router.yaml" --out-dir "$work/rendered" >"$evidence_dir/render.log"
rcd_script="$work/rendered/rc.d-routerd_carp"
test -x "$rcd_script"
grep -F "kldload carp" "$rcd_script" >"$evidence_dir/render-carp.log"
grep -F "vhid' '232'" "$rcd_script" >>"$evidence_dir/render-carp.log"

# A pre-existing operator script is a collision, not a fixture-owned target.
if [ -e /usr/local/etc/rc.d/routerd_carp ]; then
  echo "foreign routerd_carp rc.d collision" >&2
  exit 1
fi
install -d -m 0755 /usr/local/etc/rc.d
install -m 0555 "$rcd_script" /usr/local/etc/rc.d/routerd_carp
rcd_installed=1
rcd_script=/usr/local/etc/rc.d/routerd_carp

env routerd_carp_enable=YES "$rcd_script" onestart >"$evidence_dir/carp-start.log" 2>&1
env routerd_carp_enable=YES "$rcd_script" onestatus >"$evidence_dir/carp-status-initial.log" 2>&1
ifconfig "$owned_if" >"$evidence_dir/carp-ifconfig-initial.log"
grep -F "vhid 232" "$evidence_dir/carp-ifconfig-initial.log" >/dev/null
grep -E 'carp: (MASTER|BACKUP|INIT)' "$evidence_dir/carp-ifconfig-initial.log" >"$evidence_dir/carp-role-initial.log"
ping -S 198.18.232.1 -c 3 198.18.232.2 >"$evidence_dir/carp-underlay-ping.log"

env routerd_carp_enable=YES "$rcd_script" onerestart >"$evidence_dir/carp-restart.log" 2>&1
env routerd_carp_enable=YES "$rcd_script" onestatus >"$evidence_dir/carp-status-restart.log" 2>&1
ifconfig "$owned_if" >"$evidence_dir/carp-ifconfig-restart.log"
grep -F "vhid 232" "$evidence_dir/carp-ifconfig-restart.log" >/dev/null

env routerd_carp_enable=YES "$rcd_script" onestop >"$evidence_dir/carp-stop.log" 2>&1
rcd_installed=0
rm -f "$rcd_script"
ifconfig "$owned_if" >"$evidence_dir/carp-ifconfig-stop.log"
if grep -F "vhid 232" "$evidence_dir/carp-ifconfig-stop.log" >/dev/null; then
  echo "routerd-owned CARP vhid survived stop" >&2
  exit 1
fi
if grep -F "198.18.232.100" "$evidence_dir/carp-ifconfig-stop.log" >/dev/null; then
  echo "routerd-owned CARP address survived stop" >&2
  exit 1
fi

printf '%s\n' \
  'carp-render-configure-observe-restart-stop=ok' \
  'carp-role=observed-on-real-ifconfig' \
  'foreign-rcd-collision=refuse-before-mutation' \
  'owned-carp-address-vhid-cleanup=ok' >"$evidence_dir/summary.log"
printf 'freebsd-carp-runtime=ok\n' >"$evidence_dir/result"
