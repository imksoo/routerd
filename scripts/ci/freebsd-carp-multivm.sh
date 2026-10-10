#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause
# Two independent FreeBSD routers plus a client on a host-owned TAP bridge.
set -euo pipefail

run_id=${GITHUB_RUN_ID:?}
attempt=${GITHUB_RUN_ATTEMPT:?}
work=/tmp/routerd-carp-multivm-${run_id}-${attempt}
evidence=${RUNNER_TEMP:-/tmp}/routerd-carp-multivm-${run_id}-${attempt}
bridge=rd-carp-br
taps=(rd-carp-ta rd-carp-tb rd-carp-tc)
pids=()
owned_taps=()
bridge_owned=0
old_kvm_mode=
kvm_changed=0
cleanup() {
  rc=$?
  install -d -m 0700 "$evidence"
  cp "$work"/*.log "$evidence"/ 2>/dev/null || true
  find "$work" -type f -name '*.serial.log' -exec cp {} "$evidence"/ \; 2>/dev/null || true
  for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
  cleanup_failed=0
  # Evidence is owned by this user; only ip needs privilege.
  # shellcheck disable=SC2024
  for tap in "${owned_taps[@]}"; do sudo ip link del "$tap" >>"$evidence/cleanup.log" 2>&1 || cleanup_failed=1; done
  # shellcheck disable=SC2024
  if (( bridge_owned )); then sudo ip link del "$bridge" >>"$evidence/cleanup.log" 2>&1 || cleanup_failed=1; fi
  printf 'original_exit=%s cleanup_failed=%s\n' "$rc" "$cleanup_failed" >>"$evidence/cleanup.log"
  if (( cleanup_failed && rc == 0 )); then rc=2; fi
  if (( kvm_changed )); then sudo chmod "$old_kvm_mode" /dev/kvm || rc=1; fi
  exit "$rc"
}
trap cleanup EXIT
install -d -m 0700 "$work" "$evidence"
sudo apt-get update -qq
sudo apt-get install -y -qq qemu-utils qemu-system-x86 ovmf python3 rsync
[[ -c /dev/kvm ]] || exit 1
old_kvm_mode=$(stat -c '%a' /dev/kvm)
if [[ ! -w /dev/kvm ]]; then sudo chmod 666 /dev/kvm; kvm_changed=1; fi
for link in "$bridge" "${taps[@]}"; do if sudo ip link show "$link" >/dev/null 2>&1; then exit 2; fi; done
sudo ip link add "$bridge" type bridge
bridge_owned=1
sudo ip link set "$bridge" up
for tap in "${taps[@]}"; do
  sudo ip tuntap add dev "$tap" mode tap user "$(id -un)"
  owned_taps+=("$tap")
  sudo ip link set "$tap" master "$bridge"
  sudo ip link set "$tap" up
done
GOOS=freebsd GOARCH=amd64 go build -o "$work/routerd" ./cmd/routerd
printf 'step=topology bridge=%s taps=%s\n' "$bridge" "${taps[*]}" >"$evidence/steps.log"
curl -fsSL https://raw.githubusercontent.com/anyvm-org/anyvm/v0.5.1/anyvm.py >"$work/anyvm.py"
printf '%s  %s\n' '0b2e5b20879d83ff7d07fc09649e9b3576825b35c8106e2354e5cf3d0d78be06' "$work/anyvm.py" | sha256sum -c -
python3 - "$work/anyvm.py" <<'PY'
import pathlib
import sys
p = pathlib.Path(sys.argv[1])
s = p.read_text()
needle = '        "-netdev", netdev_args,\n    ])'
replacement = '''        "-netdev", netdev_args,
    ])
    tap = os.environ.get("ROUTERD_CARP_TAP")
    mac = os.environ.get("ROUTERD_CARP_MAC")
    if tap and mac:
        args_qemu.extend(["-netdev", "tap,id=net1,ifname={},script=no,downscript=no".format(tap), "-device", "virtio-net-pci,netdev=net1,mac={}".format(mac)])'''
if s.count(needle) != 1:
    raise SystemExit("anyvm net1 anchor mismatch")
p.write_text(s.replace(needle, replacement, 1))
PY
python3 -m py_compile "$work/anyvm.py"
cache="$work/cache"; install -d -m 0700 "$cache"
launch() {
  local role=$1 tap=$2 mac=$3 port=$4 serial=$5 mon=$6 mem=$7
  local data="$work/$role" pid
  ROUTERD_CARP_TAP=$tap ROUTERD_CARP_MAC=$mac python3 "$work/anyvm.py" --os freebsd --release 14.3 --arch x86_64 --mem "$mem" --snapshot --detach --vnc off --remote-vnc no --ssh-port "$port" --ssh-name "$role" --data-dir "$data" --cache-dir "$cache" --serial "$serial" --mon "$mon" >"$work/$role.log" 2>&1
  pid=$(pgrep -f "qemu.*$data" | tail -1)
  [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" && tr '\0' ' ' <"/proc/$pid/cmdline" | grep -Fq "$data" || exit 1
  pids+=("$pid")
}
launch router-a rd-carp-ta 52:54:00:c8:00:0a 2222 7101 7201 2048
launch router-b rd-carp-tb 52:54:00:c8:00:0b 2223 7102 7202 2048
launch client rd-carp-tc 52:54:00:c8:00:0c 2224 7103 7203 1024
key_for() {
  local key
  key=$(find "$work/$1" -type f -name 'freebsd-14.3-host.id_rsa' -print -quit)
  if test -n "$key"; then printf '%s\n' "$key"; return 0; fi
  find "$cache" -type f -name 'freebsd-14.3-host.id_rsa' -print -quit
}
for role in router-a router-b client; do
  for _ in $(seq 1 60); do test -n "$(key_for "$role")" && break; sleep 1; done
  test -n "$(key_for "$role")" || exit 1
done
sshvm() { local role=$1 port=$2 key; shift 2; key=$(key_for "$role"); test -n "$key"; timeout -k 2 30 ssh -o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$key" -p "$port" root@127.0.0.1 "$@"; }
scpvm() { local role=$1 port=$2 source=$3 target=$4 key; key=$(key_for "$role"); test -n "$key"; timeout -k 2 30 scp -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$key" -P "$port" "$source" "root@127.0.0.1:$target"; }
# Bind the bridge NIC by its assigned MAC; QEMU device insertion order is not
# an interface-name contract. Keep the actual guest inventory as evidence.
tap_interface() {
  local role=$1 port=$2 mac=$3 rc=0
  sshvm "$role" "$port" 'ifconfig -a' >"$evidence/$role-interfaces.log" 2>"$evidence/$role-interfaces.stderr" || rc=$?
  printf 'ssh_exit=%s\n' "$rc" >"$evidence/$role-interface-binding.log"
  (( rc == 0 )) || return 3
  python3 - "$evidence/$role-interfaces.log" "$mac" <<'PYMAC'
import pathlib, re, sys
current = None
matches = []
for line in pathlib.Path(sys.argv[1]).read_text().splitlines():
    header = re.match(r'^([a-zA-Z][a-zA-Z0-9_]*):', line)
    if header:
        current = header.group(1)
    ether = re.match(r'^\s+ether\s+([0-9a-f:]+)\s*$', line, re.I)
    if ether and ether.group(1).lower() == sys.argv[2].lower() and current:
        matches.append(current)
if len(matches) != 1:
    raise SystemExit(3)
print(matches[0])
PYMAC
}
iface_a=$(tap_interface router-a 2222 52:54:00:c8:00:0a)
iface_b=$(tap_interface router-b 2223 52:54:00:c8:00:0b)
iface_client=$(tap_interface client 2224 52:54:00:c8:00:0c)
printf 'router-a=%s router-b=%s client=%s\n' "$iface_a" "$iface_b" "$iface_client" >>"$evidence/interface-binding.log"
sshvm router-a 2222 "ifconfig $iface_a inet 198.18.232.11/24 up"
sshvm router-b 2223 "ifconfig $iface_b inet 198.18.232.12/24 up"
sshvm client 2224 "ifconfig $iface_client inet 198.18.232.20/24 up"
printf 'step=vm-management-ready\n' >>"$evidence/steps.log"
cat >"$work/a.yaml" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: carp-a}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: lan}
    spec: {ifname: $iface_a, managed: false, owner: external}
  - apiVersion: net.routerd.net/v1alpha1
    kind: VirtualAddress
    metadata: {name: vip}
    spec: {family: ipv4, interface: lan, address: 198.18.232.100/32, mode: vrrp, vrrp: {virtualRouterID: 232, priority: 150}}
EOF
sed -e 's/carp-a/carp-b/; s/priority: 150/priority: 100/' -e "s/ifname: $iface_a,/ifname: $iface_b,/" "$work/a.yaml" >"$work/b.yaml"
rcenv='env ROUTERD_RUNTIME_DIR=/var/tmp/routerd-carp-runtime routerd_carp_enable=YES'
for spec in a b; do role=router-$spec; port=$([ "$spec" = a ] && echo 2222 || echo 2223); scpvm "$role" "$port" "$work/routerd" /var/tmp/routerd; scpvm "$role" "$port" "$work/$spec.yaml" /var/tmp/router.yaml; sshvm "$role" "$port" "kldload carp || true; kldstat -m carp && chmod 755 /var/tmp/routerd && /var/tmp/routerd render freebsd --config /var/tmp/router.yaml --out-dir /var/tmp/carp && $rcenv sh /var/tmp/carp/rc.d-routerd_carp onestart" >"$evidence/$role-start.log"; done
role_interface() {
  case "$1" in router-a) printf '%s\n' "$iface_a";; router-b) printf '%s\n' "$iface_b";; *) return 3;; esac
}
read_role() {
  local role=$1 port=$2 iface rc=0 raw
  iface=$(role_interface "$role") || return 3
  raw=$(mktemp "$evidence/$role-role-XXXXXX")
  sshvm "$role" "$port" "ifconfig $iface" >"$raw.log" 2>"$raw.stderr" || rc=$?
  printf 'ssh_exit=%s\n' "$rc" >"$raw.exit"
  (( rc == 0 )) || return 3
  awk '/carp:.*vhid 232([[:space:]]|$)/ {
         if ($2 != "MASTER" && $2 != "BACKUP" && $2 != "INIT") exit 3
         n++; role=$2
       }
       END { if (n != 1) exit 3; print role }' "$raw.log"
}
wait_role() {
  local role=$1 port=$2 want=$3 observed
  for _ in $(seq 1 30); do
    if observed=$(read_role "$role" "$port") && [[ "$observed" == "$want" ]]; then return 0; fi
    sleep 1
  done
  return 3 # Bounded observation exhausted; no product timing measurement.
}
ping_vip() { sshvm client 2224 'ping -c 3 198.18.232.100'; }
exactly_one_master() {
  local a b
  a=$(read_role router-a 2222) || return 3
  b=$(read_role router-b 2223) || return 3
  [[ "$a/$b" == MASTER/BACKUP || "$a/$b" == BACKUP/MASTER ]]
}
wait_convergence() {
  for _ in $(seq 1 30); do
    if exactly_one_master; then return 0; fi
    sleep 1
  done
  return 3
}
wait_role router-a 2222 MASTER; sshvm router-a 2222 "ifconfig $iface_a" >"$evidence/router-a-initial-role.log"
wait_role router-b 2223 BACKUP; sshvm router-b 2223 "ifconfig $iface_b" >"$evidence/router-b-initial-role.log"
ping_vip >"$evidence/vip-initial.log"
sudo ip link set rd-carp-ta down; sshvm router-a 2222 true >"$evidence/router-a-management-after-tap-down.log"; wait_role router-b 2223 MASTER; sshvm router-b 2223 "ifconfig $iface_b" >"$evidence/router-b-takeover-role.log"; ping_vip >"$evidence/vip-after-takeover.log"
sudo ip link set rd-carp-ta up; wait_convergence; sshvm router-a 2222 "ifconfig $iface_a" >"$evidence/router-a-restored-role.log"; sshvm router-b 2223 "ifconfig $iface_b" >"$evidence/router-b-restored-role.log"; sshvm router-a 2222 "$rcenv sh /var/tmp/carp/rc.d-routerd_carp onestop && $rcenv sh /var/tmp/carp/rc.d-routerd_carp onestart" >"$evidence/router-a-restart.log"; wait_convergence; sshvm router-a 2222 "ifconfig $iface_a" >"$evidence/router-a-restarted-role.log"; sshvm router-b 2223 "ifconfig $iface_b" >"$evidence/router-b-restarted-role.log"; ping_vip >"$evidence/vip-after-restart.log"
for pair in 'router-a 2222' 'router-b 2223'; do read -r role port <<<"$pair"; iface=$(role_interface "$role"); sshvm "$role" "$port" "$rcenv sh /var/tmp/carp/rc.d-routerd_carp onestop && observed=\$(ifconfig $iface) && ! printf '%s\\n' \"\$observed\" | grep -q 'vhid 232'" >"$evidence/$role-cleanup.log"; done
sshvm router-a 2222 "ifconfig $iface_a inet vhid 233 advbase 1 pass foreign alias 198.18.232.100/32 && ifconfig $iface_a" >"$evidence/foreign-address-before.log"
rc=0; sshvm router-a 2222 "$rcenv sh /var/tmp/carp/rc.d-routerd_carp onestart" >"$evidence/foreign-address-refusal.log" 2>&1 || rc=$?
if (( rc == 0 )); then exit 1; fi
if (( rc != 1 )); then exit 3; fi
sshvm router-a 2222 "ifconfig $iface_a" >"$evidence/foreign-address-after.log"; cmp "$evidence/foreign-address-before.log" "$evidence/foreign-address-after.log"
sshvm router-a 2222 "ifconfig $iface_a inet 198.18.232.100/32 -alias"
sshvm router-a 2222 "ifconfig $iface_a inet vhid 232 advbase 1 pass foreign alias 198.18.232.101/32 && ifconfig $iface_a" >"$evidence/foreign-vhid-before.log"
rc=0; sshvm router-a 2222 "$rcenv sh /var/tmp/carp/rc.d-routerd_carp onestart" >"$evidence/foreign-vhid-refusal.log" 2>&1 || rc=$?
if (( rc == 0 )); then exit 1; fi
if (( rc != 1 )); then exit 3; fi
sshvm router-a 2222 "ifconfig $iface_a" >"$evidence/foreign-vhid-after.log"; cmp "$evidence/foreign-vhid-before.log" "$evidence/foreign-vhid-after.log"
sshvm router-a 2222 "ifconfig $iface_a inet 198.18.232.101/32 -alias"
printf '%s\n' 'carp-three-vm-master-backup=ok' 'carp-three-vm-vip-failover-recovery=ok' 'carp-three-vm-owned-cleanup=ok' 'carp-three-vm-foreign-preservation=ok' >"$evidence/summary.log"
printf 'freebsd-carp-multivm=ok\n' >"$evidence/result"
