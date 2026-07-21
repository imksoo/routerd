#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause
# Run the pinned anyvm source with its normal SLIRP net0 plus one disposable
# TAP-backed net1.  net0 stays responsible for SSH/rsync; net1 is the isolated
# IPsec underlay created by freebsd-ipsec-linux-peer.sh.
set -euo pipefail

tap=${ROUTERD_IPSEC_TAP:?ROUTERD_IPSEC_TAP is required}
peer_addr=${ROUTERD_IPSEC_PEER_ADDR:?ROUTERD_IPSEC_PEER_ADDR is required}
guest_addr=${ROUTERD_IPSEC_GUEST_ADDR:?ROUTERD_IPSEC_GUEST_ADDR is required}
workspace=${GITHUB_WORKSPACE:?GITHUB_WORKSPACE is required}
run_id=${GITHUB_RUN_ID:?GITHUB_RUN_ID is required}
attempt=${GITHUB_RUN_ATTEMPT:?GITHUB_RUN_ATTEMPT is required}
ipv6_candidate=${ROUTERD_IPV6_ROUTE_TO_CONSOLE_CANDIDATE:-false}

[[ "$tap" =~ ^[A-Za-z0-9_.-]+$ ]] || exit 2
[[ "$peer_addr" =~ ^198\.18\.[0-9]{1,3}\.[0-9]{1,3}$ && "$guest_addr" =~ ^198\.18\.[0-9]{1,3}\.[0-9]{1,3}$ ]] || exit 2
work="/tmp/routerd-anyvm-tap-${run_id}-${attempt}"
case "$work" in /tmp/routerd-anyvm-tap-"$run_id"-"$attempt") ;; *) exit 2;; esac
artifact_dir="${RUNNER_TEMP:-/tmp}/routerd-anyvm-console-${run_id}-${attempt}"
kvm_mode=
kvm_changed=0
cleanup() {
  local rc=$?
  install -d -m 0700 "$artifact_dir"
  find "$work" -type f -name '*.serial.log' -exec cp {} "$artifact_dir/" \; 2>/dev/null || true
  printf 'anyvm-exit=%s\n' "$rc" >"$artifact_dir/anyvm-exit.log"
  if (( kvm_changed )); then
    if ! sudo chmod "$kvm_mode" /dev/kvm; then
      echo "anyvm-tap: failed to restore /dev/kvm mode" >&2
      rc=1
    fi
  fi
  rm -rf -- "$work"
  exit "$rc"
}
trap cleanup EXIT
install -d -m 0700 "$work" "$artifact_dir"
sudo apt-get update -qq
sudo apt-get install -y -qq qemu-utils qemu-system-x86 ovmf python3 rsync

# The pinned vmactions action makes KVM writable before invoking anyvm.  Keep
# the same prerequisite bounded to this fixture and restore the exact mode on
# exit; TCG fallback makes the FreeBSD boot timeout rather than qualifying it.
[[ -c /dev/kvm ]] || { echo 'anyvm-tap: /dev/kvm is absent' >&2; exit 1; }
kvm_mode=$(stat -c '%a' /dev/kvm)
[[ "$kvm_mode" =~ ^[0-7]{3,4}$ ]] || { echo 'anyvm-tap: invalid /dev/kvm mode' >&2; exit 1; }
if [[ ! -w /dev/kvm ]]; then
  sudo chmod 666 /dev/kvm
  kvm_changed=1
fi
[[ -w /dev/kvm ]] || { echo 'anyvm-tap: /dev/kvm is not writable after chmod' >&2; exit 1; }

curl -fsSL https://raw.githubusercontent.com/anyvm-org/anyvm/v0.5.1/anyvm.py >"$work/anyvm.py"
printf '%s  %s\n' \
  '0b2e5b20879d83ff7d07fc09649e9b3576825b35c8106e2354e5cf3d0d78be06' \
  "$work/anyvm.py" | sha256sum -c -

# Keep the pinned net0 string untouched.  This is the smallest source patch:
# append a net1 TAP netdev and the matching x86 virtio device.
python3 - "$work/anyvm.py" "$tap" <<'PY'
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
tap = sys.argv[2]
source = path.read_text()
needle = '        "-netdev", netdev_args,\n    ])'
replacement = '''        "-netdev", netdev_args,
    ])
    args_qemu.extend(["-netdev", "tap,id=net1,ifname={},script=no,downscript=no".format({!r})])'''.format(tap, tap)
if source.count(needle) != 1:
    raise SystemExit("pinned anyvm netdev anchor mismatch")
source = source.replace(needle, replacement, 1)
needle = '            "-device", "{},netdev=net0".format(net_card),\n            "-device", "virtio-balloon-pci",'
replacement = '''            "-device", "{},netdev=net0".format(net_card),
            "-device", "virtio-net-pci,netdev=net1",
            "-device", "virtio-balloon-pci",'''
if source.count(needle) != 1:
    raise SystemExit("pinned anyvm x86 NIC anchor mismatch")
source = source.replace(needle, replacement, 1)

# Pinned anyvm logs the final SSH status but otherwise exits zero.  CI must
# fail when the guest smoke command fails, so preserve that remote status.
needle = '                    debuglog(config[\'debug\'], "[trace] final-SSH returned rc={}".format(rc))'
replacement = '''                    debuglog(config['debug'], "[trace] final-SSH returned rc={}".format(rc))
                    if rc != 0:
                        raise SystemExit(rc)'''
if source.count(needle) != 1:
    raise SystemExit("pinned anyvm final-SSH anchor mismatch")
source = source.replace(needle, replacement, 1)
path.write_text(source)
PY
python3 -m py_compile "$work/anyvm.py"

python3 "$work/anyvm.py" \
  --os freebsd --release 14.3 --arch x86_64 --mem 6144 --snapshot \
  --sync rsync -v "$workspace:/home/runner/work/routerd/routerd" \
  -- "cd /home/runner/work/routerd/routerd && pkg install -y go dnsmasq git hs-ShellCheck curl jq ndpi pkgconf strongswan && ROUTERD_IPSEC_TOPOLOGY=tap ROUTERD_IPSEC_UNDERLAY_IF=vtnet1 ROUTERD_IPSEC_PEER_ADDR=$peer_addr ROUTERD_IPSEC_GUEST_ADDR=$guest_addr ROUTERD_IPV6_ROUTE_TO_CONSOLE_CANDIDATE=$ipv6_candidate sh scripts/freebsd-native-vm-smoke.sh"
