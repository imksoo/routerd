#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause
# Three independent FreeBSD routers plus a client on a host-owned TAP bridge.
set -euo pipefail

run_id=${GITHUB_RUN_ID:?}
attempt=${GITHUB_RUN_ATTEMPT:?}
work=/tmp/routerd-carp-multivm-${run_id}-${attempt}
evidence=${RUNNER_TEMP:-/tmp}/routerd-carp-multivm-${run_id}-${attempt}
bridge=rd-carp-br
taps=(rd-carp-ta rd-carp-tb rd-carp-tc)
pids=()
old_kvm_mode=
kvm_changed=0
cleanup() {
  rc=$?
  install -d -m 0700 "$evidence"
  cp "$work"/*.log "$evidence"/ 2>/dev/null || true
  find "$work" -type f -name '*.serial.log' -exec cp {} "$evidence"/ \; 2>/dev/null || true
  for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
  for tap in "${taps[@]}"; do sudo ip link del "$tap" 2>/dev/null || true; done
  sudo ip link del "$bridge" 2>/dev/null || true
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
sudo ip link add "$bridge" type bridge; sudo ip link set "$bridge" up
for tap in "${taps[@]}"; do sudo ip tuntap add dev "$tap" mode tap user "$(id -un)"; sudo ip link set "$tap" master "$bridge"; sudo ip link set "$tap" up; done
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
  for n in $(seq 1 60); do test -n "$(key_for "$role")" && break; sleep 1; done
  test -n "$(key_for "$role")" || exit 1
done
sshvm() { local role=$1 port=$2 key; shift 2; key=$(key_for "$role"); test -n "$key"; timeout -k 2 30 ssh -o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$key" -p "$port" root@127.0.0.1 "$@"; }
scpvm() { local role=$1 port=$2 source=$3 target=$4 key; key=$(key_for "$role"); test -n "$key"; timeout -k 2 30 scp -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$key" -P "$port" "$source" "root@127.0.0.1:$target"; }
sshvm router-a 2222 'ifconfig vtnet0 inet 198.18.232.11/24 up'
sshvm router-b 2223 'ifconfig vtnet0 inet 198.18.232.12/24 up'
sshvm client 2224 'ifconfig vtnet0 inet 198.18.232.20/24 up'
printf 'step=vm-management-ready\n' >>"$evidence/steps.log"
cat >"$work/a.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: carp-a}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: Interface
    metadata: {name: lan}
    spec: {ifname: vtnet0, managed: false, owner: external}
  - apiVersion: net.routerd.net/v1alpha1
    kind: VirtualAddress
    metadata: {name: vip}
    spec: {family: ipv4, interface: lan, address: 198.18.232.100/32, mode: vrrp, vrrp: {virtualRouterID: 232, priority: 150}}
EOF
sed 's/carp-a/carp-b/; s/priority: 150/priority: 100/' "$work/a.yaml" >"$work/b.yaml"
rcenv='env ROUTERD_RUNTIME_DIR=/var/tmp/routerd-carp-runtime routerd_carp_enable=YES'
for spec in a b; do role=router-$spec; port=$([ "$spec" = a ] && echo 2222 || echo 2223); scpvm "$role" "$port" "$work/routerd" /var/tmp/routerd; scpvm "$role" "$port" "$work/$spec.yaml" /var/tmp/router.yaml; sshvm "$role" "$port" "kldload carp || true; kldstat -m carp; chmod 755 /var/tmp/routerd; /var/tmp/routerd render freebsd --config /var/tmp/router.yaml --out-dir /var/tmp/carp; $rcenv sh /var/tmp/carp/rc.d-routerd_carp onestart" >"$evidence/$role-start.log"; done
wait_role() { local role=$1 port=$2 want=$3; for n in $(seq 1 30); do sshvm "$role" "$port" "ifconfig vtnet0 | grep -q 'carp:.*${want}'" && return 0; sleep 1; done; return 1; }
ping_vip() { sshvm client 2224 'ping -c 3 198.18.232.100'; }
exactly_one_master() { local n=0; if sshvm router-a 2222 'ifconfig vtnet0' | grep -q 'carp:.*MASTER'; then n=$((n+1)); fi; if sshvm router-b 2223 'ifconfig vtnet0' | grep -q 'carp:.*MASTER'; then n=$((n+1)); fi; [ "$n" -eq 1 ]; }
wait_role router-a 2222 MASTER; sshvm router-a 2222 'ifconfig vtnet0' >"$evidence/router-a-initial-role.log"
wait_role router-b 2223 BACKUP; sshvm router-b 2223 'ifconfig vtnet0' >"$evidence/router-b-initial-role.log"
ping_vip >"$evidence/vip-initial.log"
sudo ip link set rd-carp-ta down; sshvm router-a 2222 true >"$evidence/router-a-management-after-tap-down.log"; wait_role router-b 2223 MASTER; sshvm router-b 2223 'ifconfig vtnet0' >"$evidence/router-b-takeover-role.log"; ping_vip >"$evidence/vip-after-takeover.log"
sudo ip link set rd-carp-ta up; converged=0; for n in $(seq 1 30); do if exactly_one_master; then converged=1; break; fi; sleep 1; done; [ "$converged" -eq 1 ]; sshvm router-a 2222 'ifconfig vtnet0' >"$evidence/router-a-restored-role.log"; sshvm router-b 2223 'ifconfig vtnet0' >"$evidence/router-b-restored-role.log"; sshvm router-a 2222 "$rcenv sh /var/tmp/carp/rc.d-routerd_carp onestop; $rcenv sh /var/tmp/carp/rc.d-routerd_carp onestart" >"$evidence/router-a-restart.log"; converged=0; for n in $(seq 1 30); do if exactly_one_master; then converged=1; break; fi; sleep 1; done; [ "$converged" -eq 1 ]; sshvm router-a 2222 'ifconfig vtnet0' >"$evidence/router-a-restarted-role.log"; sshvm router-b 2223 'ifconfig vtnet0' >"$evidence/router-b-restarted-role.log"; ping_vip >"$evidence/vip-after-restart.log"
for pair in 'router-a 2222' 'router-b 2223'; do read -r role port <<<"$pair"; sshvm "$role" "$port" "$rcenv sh /var/tmp/carp/rc.d-routerd_carp onestop; ! ifconfig vtnet0 | grep -q 'vhid 232'" >"$evidence/$role-cleanup.log"; done
sshvm router-a 2222 'ifconfig vtnet0 inet vhid 233 advbase 1 pass foreign alias 198.18.232.100/32; ifconfig vtnet0' >"$evidence/foreign-address-before.log"
if sshvm router-a 2222 "$rcenv sh /var/tmp/carp/rc.d-routerd_carp onestart" >"$evidence/foreign-address-refusal.log" 2>&1; then exit 1; fi
sshvm router-a 2222 'ifconfig vtnet0' >"$evidence/foreign-address-after.log"; cmp "$evidence/foreign-address-before.log" "$evidence/foreign-address-after.log"
sshvm router-a 2222 'ifconfig vtnet0 inet 198.18.232.100/32 -alias'
sshvm router-a 2222 'ifconfig vtnet0 inet vhid 232 advbase 1 pass foreign alias 198.18.232.101/32; ifconfig vtnet0' >"$evidence/foreign-vhid-before.log"
if sshvm router-a 2222 "$rcenv sh /var/tmp/carp/rc.d-routerd_carp onestart" >"$evidence/foreign-vhid-refusal.log" 2>&1; then exit 1; fi
sshvm router-a 2222 'ifconfig vtnet0' >"$evidence/foreign-vhid-after.log"; cmp "$evidence/foreign-vhid-before.log" "$evidence/foreign-vhid-after.log"
sshvm router-a 2222 'ifconfig vtnet0 inet 198.18.232.101/32 -alias'
printf '%s\n' 'carp-three-vm-master-backup=ok' 'carp-three-vm-vip-failover-recovery=ok' 'carp-three-vm-owned-cleanup=ok' 'carp-three-vm-foreign-preservation=ok' >"$evidence/summary.log"
printf 'freebsd-carp-multivm=ok\n' >"$evidence/result"
