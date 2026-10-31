#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause
# Dedicated Linux VM only. No package installation, service or module commands.
set -Eeuo pipefail
routerd='' dependency_root='' evidence='' mode=esp inside=0
while (($#)); do
  case "$1" in
    --routerd) routerd=$2; shift 2 ;;
    --dependency-root) dependency_root=$2; shift 2 ;;
    --evidence-dir) evidence=$2; shift 2 ;;
    --mode) mode=$2; shift 2 ;;
    --inside) inside=1; shift ;;
    --help) echo "usage: $0 --routerd FILE --dependency-root EXTRACTED_DEBS --evidence-dir DIR [--mode ike|esp]"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 64 ;;
  esac
done
[[ $(uname -s) == Linux && -n $routerd && -n $dependency_root && -n $evidence ]]
[[ $mode == ike || $mode == esp ]]
[[ ${ROUTERD_RUN_IPSEC_DEDICATED_VM:-} == 1 ]] || { echo "requires explicit dedicated VM gate" >&2; exit 64; }
routerd=$(realpath "$routerd")
dependency_root=$(realpath "$dependency_root")
mkdir -p "$evidence"
evidence=$(realpath "$evidence")
charon=$dependency_root/usr/lib/ipsec/charon
swanctl=$dependency_root/usr/sbin/swanctl
[[ -x $routerd && -x $charon && -x $swanctl ]]
for command in ip nsenter unshare mount timeout python3 sha256sum; do command -v "$command" >/dev/null; done
if ((inside == 0)); then
  # kernel-netlink probes XFRM interfaces even before an IKE request. A network
  # namespace does not contain automatic module loading, so require prerequisites.
  require_module_or_builtin() {
    local module=$1 option=$2
    if [[ -d /sys/module/$module ]]; then return; fi
    if [[ -r /boot/config-$(uname -r) ]] && grep -qx "$option=y" "/boot/config-$(uname -r)"; then return; fi
    echo "VM prerequisite is not loaded/builtin: $module ($option); no automatic module loading is authorized by this script" >&2
    exit 69
  }
  require_module_or_builtin xfrm_user CONFIG_XFRM_USER
  require_module_or_builtin xfrm_interface CONFIG_XFRM_INTERFACE
  if [[ $mode == esp ]]; then
    require_module_or_builtin esp4 CONFIG_INET_ESP
    require_module_or_builtin authenc CONFIG_CRYPTO_AUTHENC
    require_module_or_builtin cbc CONFIG_CRYPTO_CBC
  fi
  export ROUTERD_HOST_USERNS ROUTERD_HOST_NETNS ROUTERD_HOST_MNTNS
  ROUTERD_HOST_USERNS=$(readlink /proc/self/ns/user)
  ROUTERD_HOST_NETNS=$(readlink /proc/self/ns/net)
  ROUTERD_HOST_MNTNS=$(readlink /proc/self/ns/mnt)
  cat /proc/modules >"$evidence/modules.before"
  ip -j link show >"$evidence/links.before.json"
  sha256sum "$routerd" "$charon" "$swanctl" >"$evidence/binaries.sha256"
  set +e
  unshare -Urnm -- "$0" --inside --routerd "$routerd" --dependency-root "$dependency_root" --evidence-dir "$evidence" --mode "$mode" >"$evidence/runtime.log" 2>&1
  rc=$?
  set -e
  cat /proc/modules >"$evidence/modules.after"
  ip -j link show >"$evidence/links.after.json"
  if ! cmp -s "$evidence/links.before.json" "$evidence/links.after.json"; then
    echo "outer link inventory changed; retain evidence" >&2
    rc=1
  fi
  printf '%s\n' "$rc" >"$evidence/runtime.exit"
  cat "$evidence/runtime.log"
  exit "$rc"
fi
for kind in user net mnt; do
  current=$(readlink "/proc/self/ns/$kind")
  case $kind in user) outer=$ROUTERD_HOST_USERNS;; net) outer=$ROUTERD_HOST_NETNS;; mnt) outer=$ROUTERD_HOST_MNTNS;; esac
  [[ -n $outer && $outer != "$current" ]] || { echo "unverified private $kind namespace" >&2; exit 1; }
  printf 'namespace=%s outer=%s private=%s\n' "$kind" "$outer" "$current"
done
mount --make-rprivate /
mount -t sysfs sysfs /sys
work=$(mktemp -d "$evidence/run.XXXXXXXX")
mkdir "$work/run" "$work/peer-run"
mount --bind "$work/run" /run
# Linux defaults use /usr/local/etc/routerd. Mask that hierarchy privately.
mount -t tmpfs tmpfs /usr/local
mkdir -p /usr/local/etc/routerd/swanctl
mount --bind "$dependency_root/usr/lib/ipsec/plugins" /usr/lib/ipsec/plugins
export LD_LIBRARY_PATH=$dependency_root/usr/lib/ipsec
export STRONGSWAN_CONF=$work/router-strongswan.conf
write_daemon_config() {
  cat >"$1" <<'EOF'
charon {
  load_modular = no
  load = aes sha1 sha2 random nonce gmp hmac xcbc kdf kernel-netlink socket-default vici
  plugins {
    vici {
      socket = unix:///run/charon.vici
    }
  }
}
swanctl {
  load = random nonce vici
}
EOF
}
write_daemon_config "$STRONGSWAN_CONF"
write_daemon_config "$work/peer-strongswan.conf"
router_pid='' peer_pid='' peer_holder='' router_birth='' peer_birth='' holder_birth=''
birth() { awk '{print $22}' "/proc/$1/stat" 2>/dev/null; }
stop_child() {
  local pid=$1 expected=$2 label=$3 rc=0
  [[ -n $pid ]] || return 0
  if [[ -e /proc/$pid/stat && $(birth "$pid") == "$expected" ]]; then
    kill -TERM "$pid"
    for ((i=0;i<50;i++)); do
      [[ ! -e /proc/$pid/stat || $(awk '{print $3}' "/proc/$pid/stat") == Z ]] && break
      sleep .1
    done
    if [[ -e /proc/$pid/stat && $(birth "$pid") == "$expected" && $(awk '{print $3}' "/proc/$pid/stat") != Z ]]; then
      kill -KILL "$pid"; rc=1
    fi
  fi
  wait "$pid" || true
  if [[ -e /proc/$pid/stat && $(birth "$pid") == "$expected" ]]; then rc=1; fi
  printf 'reaped=%s pid=%s result=%s\n' "$label" "$pid" "$rc" | tee -a "$evidence/process-cleanup.log"
  return "$rc"
}
cleanup() {
  local original=$? failed=0
  trap - EXIT
  stop_child "$router_pid" "$router_birth" router || failed=1
  stop_child "$peer_pid" "$peer_birth" peer || failed=1
  stop_child "$peer_holder" "$holder_birth" peer-namespace || failed=1
  if [[ $original == 0 && $failed != 0 ]]; then original=1; fi
  exit "$original"
}
trap cleanup EXIT
run_stage() {
  local label=$1; shift
  printf 'stage=%s begin\n' "$label"
  if "$@" >"$evidence/$label.log" 2>&1; then
    printf 'stage=%s exit=0\n' "$label"
  else
    local rc=$?
    printf 'stage=%s exit=%s\n' "$label" "$rc"
    cat "$evidence/$label.log"
    return "$rc"
  fi
}
unshare -n -- sleep 600 & peer_holder=$!
holder_birth=$(birth "$peer_holder")
for ((i=0;i<100;i++)); do
  [[ -e /proc/$peer_holder/ns/net && $(readlink "/proc/$peer_holder/ns/net") != "$(readlink /proc/self/ns/net)" ]] && break
  sleep .02
done
[[ $(readlink "/proc/$peer_holder/ns/net") != "$(readlink /proc/self/ns/net)" ]]
peer_exec() { timeout -k 2 20 nsenter --net="/proc/$peer_holder/ns/net" -- "$@"; }
ip link add rd-ike-router type veth peer name rd-ike-peer
ip link set rd-ike-peer netns "$peer_holder"
ip address add 198.18.94.1/30 dev rd-ike-router
ip link set rd-ike-router up
ip link set lo up
ip address add 10.254.94.1/32 dev lo
peer_exec ip address add 198.18.94.2/30 dev rd-ike-peer
peer_exec ip link set rd-ike-peer up
peer_exec ip link set lo up
peer_exec ip address add 10.254.94.2/32 dev lo
ip route add 10.254.94.2/32 via 198.18.94.2
peer_exec ip route add 10.254.94.1/32 via 198.18.94.1
start_router() {
  "$charon" >"$evidence/router-charon.$1.log" 2>&1 & router_pid=$!
  router_birth=$(birth "$router_pid")
}
# shellcheck disable=SC2016 # Expanded inside the private mount namespace.
nsenter --net="/proc/$peer_holder/ns/net" -- unshare -m -- bash -c '
  mount --make-rprivate / &&
  mount -t sysfs sysfs /sys &&
  mount --bind "$1" /run &&
  export STRONGSWAN_CONF=$2 &&
  exec "$3"
' peer-charon "$work/peer-run" "$work/peer-strongswan.conf" "$charon" >"$evidence/peer-charon.log" 2>&1 & peer_pid=$!
peer_birth=$(birth "$peer_pid")
peer_control() {
  timeout -k 2 20 nsenter --net="/proc/$peer_pid/ns/net" --mount="/proc/$peer_pid/ns/mnt" -- "$swanctl" "$@" --uri unix:///run/charon.vici
}
router_control() { timeout -k 2 20 "$swanctl" "$@" --uri unix:///run/charon.vici; }
wait_ready() {
  local side=$1
  for ((i=0;i<100;i++)); do
    if "$side" --stats >"$evidence/$side.stats.log" 2>&1; then return; fi
    sleep .05
  done
  cat "$evidence/$side.stats.log"; return 1
}
start_router initial
wait_ready router_control
wait_ready peer_control
cat >"$work/peer.conf" <<'EOF'
connections {
  native-peer {
    version = 2
    local_addrs = 198.18.94.2
    remote_addrs = 198.18.94.1
    proposals = aes256-sha256-modp2048
    local { auth = psk }
    remote { auth = psk }
    children {
      net {
        local_ts = 10.254.94.2/32
        remote_ts = 10.254.94.1/32
        esp_proposals = aes256-sha256
        start_action = trap
      }
    }
  }
}
secrets {
  ike-peer {
    id-1 = 198.18.94.1
    id-2 = 198.18.94.2
    secret = "routerd-disposable-private-native-psk"
  }
}
EOF
mkdir -p "$work"/{x509,x509ca,x509ocsp,x509aa,x509ac,x509crl,pubkey,private,rsa,ecdsa,pkcs8,pkcs12}
run_stage peer-load peer_control --load-all --file "$work/peer.conf"
cat >"$work/router.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: private-ipsec-native}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: IPsecConnection
    metadata: {name: native-tunnel}
    spec: {localAddress: 198.18.94.1, remoteAddress: 198.18.94.2, preSharedKey: routerd-disposable-private-native-psk, leftSubnet: 10.254.94.1/32, rightSubnet: 10.254.94.2/32}
EOF
cat >"$work/empty.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: private-ipsec-native}
spec: {}
EOF
product() {
  timeout -k 2 30 "$routerd" serve --once --controllers ipsec --config "$1" --state-file "$work/state.db" --ledger-file "$work/ledger.db" --status-file "$work/status.json"
}
receipt=/usr/local/etc/routerd/swanctl/routerd-ipsec.json
check_receipt() {
  python3 - "$receipt" "$1" <<'PY'
import json,sys
r=json.load(open(sys.argv[1]))
count=int(sys.argv[2])
assert r['version']==1 and r['pending'] is False, r
assert len(r['connections'])==count and len(r['sharedKeys'])==count, r
PY
}
check_phase() {
  local label=$1
  if [[ $mode == esp ]]; then
    run_stage "$label.initiate" router_control --initiate --ike routerd-native-tunnel --child net --timeout 15
    run_stage "$label.forward" ping -n -I 10.254.94.1 -c 2 -W 2 10.254.94.2
    run_stage "$label.reverse" peer_exec ping -n -I 10.254.94.2 -c 2 -W 2 10.254.94.1
    run_stage "$label.router-xfrm" ip -s xfrm state
    run_stage "$label.peer-xfrm" peer_exec ip -s xfrm state
    grep -Eq '[1-9][0-9]*\(packets\)' "$evidence/$label.router-xfrm.log"
    grep -Eq '[1-9][0-9]*\(packets\)' "$evidence/$label.peer-xfrm.log"
  else
    run_stage "$label.initiate" router_control --initiate --ike routerd-native-tunnel --timeout 15
  fi
  run_stage "$label.router-sas" router_control --list-sas
  run_stage "$label.peer-sas" peer_control --list-sas
  grep -q ESTABLISHED "$evidence/$label.router-sas.log"
  grep -q ESTABLISHED "$evidence/$label.peer-sas.log"
}
run_stage acquire product "$work/router.yaml"
check_receipt 1
check_phase initial
run_stage unchanged product "$work/router.yaml"
check_receipt 1
run_stage withdraw product "$work/empty.yaml"
check_receipt 0
run_stage withdrawn-connections router_control --list-conns
run_stage withdrawn-sas router_control --list-sas
if grep -q routerd-native-tunnel "$evidence/withdrawn-connections.log"; then echo "owned native state remains after withdrawal" >&2; exit 1; fi
if grep -q ESTABLISHED "$evidence/withdrawn-sas.log"; then echo "owned native state remains after withdrawal" >&2; exit 1; fi
run_stage reacquire product "$work/router.yaml"
check_receipt 1
check_phase reacquired
stop_child "$router_pid" "$router_birth" router-before-restart
router_pid=
start_router restarted
wait_ready router_control
run_stage restart-reconcile product "$work/router.yaml"
check_receipt 1
check_phase restarted
run_stage final-withdraw product "$work/empty.yaml"
check_receipt 0
run_stage final-connections router_control --list-conns
run_stage final-sas router_control --list-sas
if grep -q routerd-native-tunnel "$evidence/final-connections.log"; then echo "owned native state remains after withdrawal" >&2; exit 1; fi
if grep -q ESTABLISHED "$evidence/final-sas.log"; then echo "owned native state remains after withdrawal" >&2; exit 1; fi
cp "$receipt" "$evidence/receipt.final.json"
cp "$work/state.db" "$evidence/state.final.db"
printf 'ipsec-native=%s-ok\n' "$mode"
