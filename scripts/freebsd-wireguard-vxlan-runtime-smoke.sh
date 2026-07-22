#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Exercise current routerd FreeBSD WireGuard rc.d rendering against a real
# if_wg peer, then carry a unicast VXLAN over that encrypted underlay.
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
command -v wg >/dev/null
command -v jq >/dev/null

mkdir -p "$evidence_dir"
work=$(mktemp -d /var/tmp/routerd-wireguard-runtime.XXXXXX)
client_if=rdwg0
peer_if=rdwg1
client_vx=rdvx0
peer_vx=rdvx1
client_script=
client_started=0
peer_created=0
vx_created=0

cleanup() {
  rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "freebsd-wireguard-vxlan-runtime failure evidence:" >&2
    for log in "$evidence_dir"/*; do
      [ -f "$log" ] || continue
      echo "--- $log" >&2
      cat "$log" >&2 || true
    done
  fi
  if [ "$vx_created" -eq 1 ]; then
    ifconfig "$client_vx" destroy >>"$evidence_dir/cleanup.log" 2>&1 || rc=1
    ifconfig "$peer_vx" destroy >>"$evidence_dir/cleanup.log" 2>&1 || rc=1
  fi
  if [ "$client_started" -eq 1 ]; then
    "$client_script" onestop >>"$evidence_dir/cleanup.log" 2>&1 || rc=1
  fi
  if [ "$peer_created" -eq 1 ]; then
    ifconfig "$peer_if" destroy >>"$evidence_dir/cleanup.log" 2>&1 || rc=1
  fi
  rm -rf "$work"
  exit "$rc"
}
trap cleanup EXIT HUP INT TERM

for ifname in "$client_if" "$peer_if" "$client_vx" "$peer_vx"; do
  if ifconfig "$ifname" >/dev/null 2>&1; then
    echo "foreign interface collision: $ifname" >&2
    exit 1
  fi
done
kldload if_wg >"$evidence_dir/kldload-if_wg.log" 2>&1 || true
client_key="$work/client.key"
peer_key="$work/peer.key"
umask 077
wg genkey >"$client_key"
wg genkey >"$peer_key"
client_pub=$(wg pubkey <"$client_key")
peer_pub=$(wg pubkey <"$peer_key")

ifconfig "$peer_if" create >"$evidence_dir/peer-create.log" 2>&1
peer_created=1
wg set "$peer_if" listen-port 51891 private-key "$peer_key" peer "$client_pub" \
  allowed-ips 10.250.89.1/32 endpoint 127.0.0.1:51890 persistent-keepalive 1 >>"$evidence_dir/peer-create.log" 2>&1
ifconfig "$peer_if" inet 10.250.89.2/24 alias
ifconfig "$peer_if" up

cat >"$work/router.yaml" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: lifecycle-wireguard-vxlan
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata:
        name: $client_if
      spec:
        ifName: $client_if
        privateKeyFile: $client_key
        listenPort: 51890
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardPeer
      metadata:
        name: lifecycle-peer
      spec:
        interface: $client_if
        publicKey: $peer_pub
        allowedIPs:
          - 10.250.89.2/32
        endpoint: 127.0.0.1:51891
        persistentKeepalive: 1
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata:
        name: $client_if-address
      spec:
        interface: $client_if
        address: 10.250.89.1/24
        exclusive: false
    - apiVersion: net.routerd.net/v1alpha1
      kind: VXLANSegment
      metadata:
        name: $client_vx
      spec:
        ifName: $client_vx
        vni: 899
        localAddress: 10.250.89.1
        remotes:
          - 10.250.89.2
        underlayInterface: $client_if
        udpPort: 4789
EOF
"$routerd" render freebsd --config "$work/router.yaml" --out-dir "$work/rendered" >"$evidence_dir/render.log"
client_script="$work/rendered/rc.d-routerd_wireguard_$client_if"
test -x "$client_script"
grep -F "kldload if_wg" "$client_script" >"$evidence_dir/render-wireguard.log"
grep -F "ifconfig_${client_vx}=\"vxlanid 899 vxlanlocal 10.250.89.1 vxlanremote 10.250.89.2 vxlandev $client_if vxlanport 4789 up\"" "$work/rendered/rc.conf.d-routerd" >"$evidence_dir/render-vxlan.log"

"$client_script" onestart >"$evidence_dir/wireguard-start.log" 2>&1
client_started=1
"$client_script" onestatus >"$evidence_dir/wireguard-status-initial.log" 2>&1
ping -S 10.250.89.1 -c 3 10.250.89.2 >"$evidence_dir/wireguard-ping-initial.log"
wg show "$client_if" dump >"$evidence_dir/wireguard-dump-initial.log"
awk -F '\t' 'NR == 2 && $5 > 0 && ($6 + $7) > 0 { ok=1 } END { exit ok ? 0 : 1 }' "$evidence_dir/wireguard-dump-initial.log"

ifconfig "$client_vx" create >"$evidence_dir/vxlan-create.log" 2>&1
ifconfig "$peer_vx" create >>"$evidence_dir/vxlan-create.log" 2>&1
vx_created=1
ifconfig "$client_vx" vxlanid 899 vxlanlocal 10.250.89.1 vxlanremote 10.250.89.2 vxlandev "$client_if" vxlanport 4789 up >>"$evidence_dir/vxlan-create.log" 2>&1
ifconfig "$peer_vx" vxlanid 899 vxlanlocal 10.250.89.2 vxlanremote 10.250.89.1 vxlandev "$peer_if" vxlanport 4789 up >>"$evidence_dir/vxlan-create.log" 2>&1
ifconfig "$client_vx" inet 198.19.89.1/24 alias
ifconfig "$peer_vx" inet 198.19.89.2/24 alias
ping -S 198.19.89.1 -c 3 198.19.89.2 >"$evidence_dir/vxlan-over-wireguard-ping.log"

"$client_script" onerestart >"$evidence_dir/wireguard-restart.log" 2>&1
ping -S 10.250.89.1 -c 3 10.250.89.2 >"$evidence_dir/wireguard-ping-restart.log"
wg show "$client_if" dump >"$evidence_dir/wireguard-dump-restart.log"
awk -F '\t' 'NR == 2 && $5 > 0 && ($6 + $7) > 0 { ok=1 } END { exit ok ? 0 : 1 }' "$evidence_dir/wireguard-dump-restart.log"
"$client_script" onestop >"$evidence_dir/wireguard-stop.log" 2>&1
client_started=0
if ifconfig "$client_if" >/dev/null 2>&1; then
  echo "routerd-owned WireGuard interface survived stop" >&2
  exit 1
fi
printf '%s\n' \
  'wireguard-render-configure-handshake-observe-restart-stop=ok' \
  'vxlan-over-wireguard-render-configure-packet=ok' \
  'foreign-interface-collision=refuse-before-mutation' \
  'owned-interface-cleanup=pending-exit-trap' >"$evidence_dir/summary.log"
printf 'freebsd-wireguard-vxlan-runtime=ok\n' >"$evidence_dir/result"
