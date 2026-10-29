#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Exercise the current routerd WireGuard actor and its persisted ownership
# journal against an if_wg peer, then configure VXLAN over that underlay.
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
command -v sqlite3 >/dev/null

mkdir -p "$evidence_dir"
work=$(mktemp -d /var/tmp/routerd-wireguard-runtime.XXXXXX)
client_if=rdwg0
peer_if=rdwg1
client_vx=rdvx0
peer_vx=rdvx1
client_bridge=rdbr0
client_started=0
foreign_client_created=0
peer_created=0
vx_started=0
foreign_vx_created=0
peer_vx_created=0
bridge_created=0
state="$work/state.db"
ledger="$work/ledger.db"
empty_config="$work/empty.yaml"
cat >"$empty_config" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: lifecycle-wireguard-vxlan}
spec: {resources: []}
EOF

apply_wireguard() {
  label=$1
  config=$2
  "$routerd" serve --once --controllers wireguard,ipv4-static-address \
    --config "$config" --state-file "$state" --ledger-file "$ledger" \
    --status-file "$work/status-$label.json" --socket "$work/api-$label.sock" \
    --status-socket "$work/status-$label.sock"
}

observe_wireguard() {
  label=$1
  sqlite3 "$state" "SELECT status FROM objects WHERE api_version='net.routerd.net/v1alpha1' AND kind='WireGuardInterface' AND name='$client_if';" >"$evidence_dir/wireguard-$label.json"
  jq -e --arg ifname "$client_if" '.phase == "Up" and .ifname == $ifname and .peerCount == 1' "$evidence_dir/wireguard-$label.json" >/dev/null
  sqlite3 "$state" "SELECT status FROM objects WHERE api_version='routerd.net/v1alpha1' AND kind='Router' AND name='wireguard-native';" >"$evidence_dir/wireguard-native-$label.json"
  jq -e --arg ifname "$client_if" '.interfaces | length == 1 and .[0].name == $ifname and .[0].resource == $ifname and .[0].os == "freebsd" and .[0].pending == false and (.[0].token | length == 32)' "$evidence_dir/wireguard-native-$label.json" >/dev/null
}

apply_vxlan() {
  label=$1
  config=$2
  "$routerd" serve --once --controllers vxlan-segment \
    --config "$config" --state-file "$state" --ledger-file "$ledger" \
    --status-file "$work/status-vxlan-$label.json" --socket "$work/api-vxlan-$label.sock" \
    --status-socket "$work/status-vxlan-$label.sock"
}

observe_vxlan() {
  label=$1
  sqlite3 "$state" "SELECT status FROM objects WHERE api_version='net.routerd.net/v1alpha1' AND kind='VXLANSegment' AND name='$client_vx';" >"$evidence_dir/vxlan-$label.json"
  jq -e --arg ifname "$client_vx" --arg bridge "$client_bridge" '.phase == "Healthy" and .ifname == $ifname and .bridgeIfname == $bridge and .vni == 899' "$evidence_dir/vxlan-$label.json" >/dev/null
  # Match the actual native cookie, so a receipt from another boot/VNET cannot
  # satisfy this namespace's ownership observation.
  token=$(ifconfig "$client_vx" | sed -n 's/^[[:space:]]*description: routerd.vxlan://p')
  [ "${#token}" -eq 32 ]
  find /var/db/routerd/vxlan-native -type f -name 'segment-*.json' -exec \
    jq -c --arg resource "$client_vx" --arg token "$token" 'select(.resource == $resource and .identity.token == $token)' {} + >"$evidence_dir/vxlan-native-$label.json"
  [ "$(wc -l <"$evidence_dir/vxlan-native-$label.json" | tr -d ' ')" -eq 1 ]
  jq -e --arg ifname "$client_vx" '.identity.pending == false and .identity.index > 0 and .identity.underlayIndex > 0 and .identity.bridgeIndex > 0 and .withdrawing == false and .plan.ifname == $ifname' "$evidence_dir/vxlan-native-$label.json" >/dev/null
}

capture_wireguard_traffic() {
  label=$1
  # wg dump includes the interface private key and peer PSK. Keep only the
  # public identity, endpoint, handshake and transfer observations as evidence.
  wg show "$client_if" dump | awk 'BEGIN { FS=OFS="\t" } NR==1 {$1="[REDACTED]"} NR>1 {$2="[REDACTED]"} {print}' >"$evidence_dir/wireguard-dump-$label.log"
  awk -F '\t' 'NR == 2 && $5 > 0 && ($6 + $7) > 0 { ok=1 } END { exit ok ? 0 : 1 }' "$evidence_dir/wireguard-dump-$label.log"
}

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
  if [ "$vx_started" -eq 1 ]; then
    apply_vxlan cleanup "$empty_config" >>"$evidence_dir/cleanup.log" 2>&1 || rc=1
  fi
  if [ "$foreign_vx_created" -eq 1 ]; then
    ifconfig "$client_vx" destroy >>"$evidence_dir/cleanup.log" 2>&1 || rc=1
  fi
  if [ "$peer_vx_created" -eq 1 ]; then
    ifconfig "$peer_vx" destroy >>"$evidence_dir/cleanup.log" 2>&1 || rc=1
  fi
  if [ "$bridge_created" -eq 1 ]; then
    ifconfig "$client_bridge" destroy >>"$evidence_dir/cleanup.log" 2>&1 || rc=1
  fi
  if [ "$client_started" -eq 1 ]; then
    apply_wireguard cleanup "$empty_config" >>"$evidence_dir/cleanup.log" 2>&1 || rc=1
  fi
  if [ "$foreign_client_created" -eq 1 ]; then
    ifconfig "$client_if" destroy >>"$evidence_dir/cleanup.log" 2>&1 || rc=1
  fi
  if [ "$peer_created" -eq 1 ]; then
    ifconfig "$peer_if" destroy >>"$evidence_dir/cleanup.log" 2>&1 || rc=1
  fi
  rm -rf "$work"
  exit "$rc"
}
trap cleanup EXIT HUP INT TERM

for ifname in "$client_if" "$peer_if" "$client_vx" "$peer_vx" "$client_bridge"; do
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

# if_wg only accepts its native clone prefix for a direct create. Match the
# generated routerd rc.d fallback for this disposable custom peer name.
ifconfig "$peer_if" create >"$evidence_dir/peer-create.log" 2>&1 || \
  ifconfig wg create name "$peer_if" >>"$evidence_dir/peer-create.log" 2>&1
peer_created=1
wg set "$peer_if" listen-port 51891 private-key "$peer_key" peer "$client_pub" \
  allowed-ips 10.250.89.1/32 endpoint 127.0.0.1:51890 persistent-keepalive 1 >>"$evidence_dir/peer-create.log" 2>&1
ifconfig "$peer_if" inet 10.250.89.2/24 alias
ifconfig "$peer_if" up

write_router() {
  mtu=$1
  vxlan_mtu=${2:-1300}
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
        ifname: $client_if
        privateKeyFile: $client_key
        listenPort: 51890
        mtu: $mtu
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
        ifname: $client_vx
        mtu: $vxlan_mtu
        vni: 899
        localAddress: 10.250.89.1
        remotes:
          - 10.250.89.2
        underlayInterface: $client_if
        udpPort: 4789
    - apiVersion: net.routerd.net/v1alpha1
      kind: Bridge
      metadata:
        name: $client_bridge
      spec:
        ifname: $client_bridge
        members:
          - $client_vx
EOF
}
write_router 1420
"$routerd" render freebsd --config "$work/router.yaml" --out-dir "$work/rendered" >"$evidence_dir/render.log"
test ! -e "$work/rendered/rc.d-routerd_wireguard_$client_if"
test ! -e "$work/rendered/rc.d-routerd_vxlan_$client_vx"
if grep -F "routerd_vxlan_${client_vx}_enable" "$work/rendered/rc.conf.d-routerd"; then
  echo "native Segment actor has a competing boot owner" >&2
  exit 1
fi

# Mark the attempt before mutation: the actor may have a pending receipt even
# if its first application fails, and cleanup must use that same SQLite owner.
client_started=1
apply_wireguard boot "$work/router.yaml" >"$evidence_dir/wireguard-boot.log" 2>&1
observe_wireguard boot
ping -S 10.250.89.1 -c 3 10.250.89.2 >"$evidence_dir/wireguard-ping-initial.log"
capture_wireguard_traffic initial
# Every invocation is a new routerd process. The persisted receipt must permit
# an unchanged restart to observe ownership without creating another owner.
apply_wireguard restart "$work/router.yaml" >"$evidence_dir/wireguard-restart.log" 2>&1
observe_wireguard restart
jq -e '.reason == "AlreadyConfigured"' "$evidence_dir/wireguard-restart.json" >/dev/null
cmp "$evidence_dir/wireguard-native-boot.json" "$evidence_dir/wireguard-native-restart.json"
write_router 1380
apply_wireguard reload "$work/router.yaml" >"$evidence_dir/wireguard-reload.log" 2>&1
observe_wireguard reload
ifconfig "$client_if" >"$evidence_dir/wireguard-interface-reload.log"
grep -E 'mtu 1380([[:space:]]|$)' "$evidence_dir/wireguard-interface-reload.log"
jq -e '.mtu == 1380' "$evidence_dir/wireguard-reload.json" >/dev/null
cmp "$evidence_dir/wireguard-native-boot.json" "$evidence_dir/wireguard-native-reload.json"
ping -S 10.250.89.1 -c 3 10.250.89.2 >"$evidence_dir/wireguard-ping-reload.log"
capture_wireguard_traffic reload

ifconfig "$peer_vx" create >>"$evidence_dir/vxlan-create.log" 2>&1 || \
  ifconfig vxlan create name "$peer_vx" >>"$evidence_dir/vxlan-create.log" 2>&1
peer_vx_created=1
ifconfig "$client_bridge" create >>"$evidence_dir/vxlan-create.log" 2>&1 || \
  ifconfig bridge create name "$client_bridge" >>"$evidence_dir/vxlan-create.log" 2>&1
ifconfig "$client_bridge" up >>"$evidence_dir/vxlan-create.log" 2>&1
bridge_created=1
ifconfig "$peer_vx" vxlanid 899 vxlanlocal 10.250.89.2 vxlanremote 10.250.89.1 vxlandev "$peer_if" vxlanlocalport 4789 vxlanremoteport 4789 up >>"$evidence_dir/vxlan-create.log" 2>&1
vx_started=1
apply_vxlan boot "$work/router.yaml" >"$evidence_dir/vxlan-boot.log" 2>&1
observe_vxlan boot
ifconfig "$client_bridge" >"$evidence_dir/vxlan-bridge-membership.log"
grep -F "$client_vx" "$evidence_dir/vxlan-bridge-membership.log"
ifconfig "$client_vx" inet 198.19.89.1/24 alias
ifconfig "$peer_vx" inet 198.19.89.2/24 alias
# Same-stack ping is diagnostic; local delivery cannot prove overlay forwarding.
ping -S 198.19.89.1 -c 3 198.19.89.2 >"$evidence_dir/vxlan-over-wireguard-ping.log" 2>&1 || true

apply_vxlan restart "$work/router.yaml" >"$evidence_dir/vxlan-restart.log" 2>&1
observe_vxlan restart
cmp "$evidence_dir/vxlan-native-boot.json" "$evidence_dir/vxlan-native-restart.json"
ping -S 198.19.89.1 -c 3 198.19.89.2 >"$evidence_dir/vxlan-over-wireguard-ping-restart.log" 2>&1 || true
write_router 1380 1280
apply_vxlan reload "$work/router.yaml" >"$evidence_dir/vxlan-reload.log" 2>&1
observe_vxlan reload
jq -e '.plan.mtu == 1280' "$evidence_dir/vxlan-native-reload.json" >/dev/null
ifconfig "$client_vx" >"$evidence_dir/vxlan-interface-reload.log"
grep -E 'mtu 1280([[:space:]]|$)' "$evidence_dir/vxlan-interface-reload.log"
old_token=$(jq -r '.identity.token' "$evidence_dir/vxlan-native-boot.json")
new_token=$(jq -r '.identity.token' "$evidence_dir/vxlan-native-reload.json")
[ "$old_token" != "$new_token" ]
apply_vxlan withdraw "$empty_config" >"$evidence_dir/vxlan-withdraw.log" 2>&1
vx_started=0
if ifconfig "$client_vx" >"$evidence_dir/vxlan-after-stop.log" 2>&1; then
  echo "routerd-owned VXLAN interface survived withdrawal" >&2
  exit 1
fi
sqlite3 "$state" "SELECT status FROM objects WHERE api_version='net.routerd.net/v1alpha1' AND kind='VXLANSegment' AND name='$client_vx';" >"$evidence_dir/vxlan-withdraw.json"
jq -e '.phase == "Removed"' "$evidence_dir/vxlan-withdraw.json" >/dev/null
find /var/db/routerd/vxlan-native -type f -name 'segment-*.json' -exec \
  jq -c --arg token "$new_token" 'select(.identity.token == $token)' {} + >"$evidence_dir/vxlan-native-withdraw.json"
test ! -s "$evidence_dir/vxlan-native-withdraw.json"

# Exercise the actual actor's same-name foreign-interface refusal path.
ifconfig "$client_vx" create >"$evidence_dir/vxlan-foreign-create.log" 2>&1 || \
  ifconfig vxlan create name "$client_vx" >>"$evidence_dir/vxlan-foreign-create.log" 2>&1
foreign_vx_created=1
ifconfig "$client_vx" >"$evidence_dir/vxlan-foreign-before.log"
if apply_vxlan foreign "$work/router.yaml" >"$evidence_dir/vxlan-foreign-refusal.log" 2>&1; then
  echo "VXLAN Segment actor adopted a foreign interface" >&2
  exit 1
fi
ifconfig "$client_vx" >"$evidence_dir/vxlan-foreign-preserved.log"
cmp "$evidence_dir/vxlan-foreign-before.log" "$evidence_dir/vxlan-foreign-preserved.log"
ifconfig "$client_vx" destroy >>"$evidence_dir/vxlan-foreign-preserved.log" 2>&1
foreign_vx_created=0

apply_wireguard withdraw "$empty_config" >"$evidence_dir/wireguard-withdraw.log" 2>&1
client_started=0
if ifconfig "$client_if" >/dev/null 2>&1; then
  echo "routerd-owned WireGuard interface survived stop" >&2
  exit 1
fi
sqlite3 "$state" "SELECT status FROM objects WHERE api_version='routerd.net/v1alpha1' AND kind='Router' AND name='wireguard-native';" >"$evidence_dir/wireguard-native-withdraw.json"
jq -e '.interfaces | length == 0' "$evidence_dir/wireguard-native-withdraw.json" >/dev/null
# A manually created same-name interface is foreign. The actor
# must reject it and leave it for the fixture owner to remove.
ifconfig "$client_if" create >"$evidence_dir/wireguard-foreign-create.log" 2>&1 || \
  ifconfig wg create name "$client_if" >>"$evidence_dir/wireguard-foreign-create.log" 2>&1
foreign_client_created=1
ifconfig "$client_if" >"$evidence_dir/wireguard-foreign-before.log"
if apply_wireguard foreign "$work/router.yaml" >"$evidence_dir/wireguard-foreign-refusal.log" 2>&1; then
  echo "WireGuard actor adopted a foreign interface" >&2
  exit 1
fi
ifconfig "$client_if" >"$evidence_dir/wireguard-foreign-preserved.log"
cmp "$evidence_dir/wireguard-foreign-before.log" "$evidence_dir/wireguard-foreign-preserved.log"
ifconfig "$client_if" destroy >>"$evidence_dir/wireguard-foreign-preserved.log" 2>&1
foreign_client_created=0
printf '%s\n' \
  'wireguard-actor-boot-handshake-observe-restart-reload-withdraw=ok' \
  'wireguard-persisted-native-owner=one-stable-token' \
  'wireguard-ping-forwarding=unverified-same-stack-fixture' \
  'wireguard-foreign-interface=actor-refusal-preserved' \
  'vxlan-actor-boot-observe-restart-mtu-reload-withdraw=ok' \
  'vxlan-over-wireguard-forwarding=unverified-same-stack-fixture' \
  'vxlan-native-cookie-index-flood-and-bridge-receipt=ok' \
  'vxlan-foreign-interface=actor-refusal-preserved' \
  'owned-interface-cleanup=pending-exit-trap' >"$evidence_dir/summary.log"
printf 'freebsd-wireguard-vxlan-runtime=ok\n' >"$evidence_dir/result"
