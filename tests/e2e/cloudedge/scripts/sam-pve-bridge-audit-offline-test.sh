#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="$script_dir/sam-pve-bridge-audit.sh"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/sam-pve-bridge-audit.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

die() { echo "sam PVE bridge-audit offline: $*" >&2; exit 1; }

capture_bridge=rsamabc123
ssh_key="$tmp/pve_ssh"
: >"$ssh_key"
chmod 0600 "$ssh_key"
pve_known_hosts="$tmp/pve-known_hosts"
printf '%s\n' 'pve01.local ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEZpeHR1cmUtcHZlLWhvc3Qta2V5LTE=' \
  'pve02.local ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEZpeHR1cmUtcHZlLWhvc3Qta2V5LTI=' \
  'pve03.local ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEZpeHR1cmUtcHZlLWhvc3Qta2V5LTM=' >"$pve_known_hosts"
chmod 0600 "$pve_known_hosts"

jq -n --arg capture "$capture_bridge" '
  {
    fabric: {value: {pve: {leaf_capture_bridge: $capture}}},
    nodes: {value: {
      "pve-leaf-a": {site: "pve", role: "leaf", vm_id: 131, capture_bridge: $capture},
      "pve-client-a": {site: "pve", role: "client", vm_id: 141, capture_bridge: $capture},
      "pve-leaf-b": {site: "pve", role: "leaf", vm_id: 181, capture_bridge: $capture},
      "pve-client-b": {site: "pve", role: "client", vm_id: 182, capture_bridge: $capture},
      "pve-rr-a": {site: "pve", role: "rr", vm_id: 171, pve_ssh_host: "pve02.local", underlay_bridge: "svnet1"},
      "pve-rr-b": {site: "pve", role: "rr", vm_id: 172, pve_ssh_host: "pve03.local", underlay_bridge: "svnet1"}
    }}
  }
' >"$tmp/tofu-output.json"

fake_bin="$tmp/bin"
mkdir -p "$fake_bin"
# shellcheck disable=SC2016 # The quoted lines are the fake ssh program, not this shell.
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'printf "%s\n" "$*" >>"$BRIDGE_AUDIT_SSH_LOG"' \
  'case "$*" in' \
  '  *root@pve01.local*)' \
  '    printf "%b\n" "131\tpve-leaf-a\tnet1: virtio=aa:bb:cc:00:00:01,bridge=rsamabc123" "141\tpve-client-a\tnet1: virtio=aa:bb:cc:00:00:02,bridge=rsamabc123" "181\tpve-leaf-b\tnet1: virtio=aa:bb:cc:00:00:03,bridge=rsamabc123" "182\tpve-client-b\tnet1: virtio=aa:bb:cc:00:00:04,bridge=rsamabc123"' \
  '    ;;' \
  '  *root@pve02.local*)' \
  '    printf "%s\n" "name: routerd-run-pve-rr-a" "net0: virtio=aa:bb:cc:00:01:71,bridge=svnet1,firewall=1"' \
  '    if [ "${BRIDGE_AUDIT_RR_A_EXTRA_NIC:-0}" = 1 ]; then printf "%s\n" "net1: virtio=aa:bb:cc:00:11:71,bridge=rsamabc123"; fi' \
  '    ;;' \
  '  *root@pve03.local*)' \
  '    printf "%s\n" "name: routerd-run-pve-rr-b" "net0: virtio=aa:bb:cc:00:01:72,bridge=svnet1,firewall=1"' \
  '    ;;' \
  '  *) echo "unexpected fake SSH invocation: $*" >&2; exit 2 ;;' \
  'esac' >"$fake_bin/ssh"
chmod +x "$fake_bin/ssh"

: >"$tmp/ssh.log"
if ! PATH="$fake_bin:$PATH" BRIDGE_AUDIT_SSH_LOG="$tmp/ssh.log" \
  "$script" --tofu-output "$tmp/tofu-output.json" --pve-node-ssh-host pve01.local \
  --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --evidence "$tmp/pass-evidence.txt" >/dev/null; then
  cat "$tmp/pass-evidence.txt" >&2 || true
  die "valid PVE topology unexpectedly failed"
fi
grep -q 'pve_rr_nic_audit' "$tmp/pass-evidence.txt" || die "RR audit evidence is missing"
grep -q $'pve-rr-a\tpve02.local\t171\tsvnet1\trsamabc123\t1\tPASS' "$tmp/pass-evidence.txt" || die "RR A was not verified"
grep -q $'pve-rr-b\tpve03.local\t172\tsvnet1\trsamabc123\t1\tPASS' "$tmp/pass-evidence.txt" || die "RR B was not verified"
grep -q 'qm config 171' "$tmp/ssh.log" || die "RR A qm config was not inspected"
grep -q 'qm config 172' "$tmp/ssh.log" || die "RR B qm config was not inspected"
grep -q 'BatchMode=yes' "$tmp/ssh.log" || die "PVE audit SSH was not non-interactive"
grep -q 'StrictHostKeyChecking=yes' "$tmp/ssh.log" || die "PVE audit SSH did not require pinned PVE host keys"
grep -Fq "UserKnownHostsFile=$pve_known_hosts" "$tmp/ssh.log" || die "PVE audit SSH did not use the supplied PVE known_hosts"
grep -Fq 'GlobalKnownHostsFile=/dev/null' "$tmp/ssh.log" || die "PVE audit SSH consulted ambient global known_hosts"
grep -q 'ConnectTimeout=10' "$tmp/ssh.log" || die "PVE audit SSH did not bound connection setup"

if PATH="$fake_bin:$PATH" BRIDGE_AUDIT_SSH_LOG="$tmp/ssh.log" BRIDGE_AUDIT_RR_A_EXTRA_NIC=1 \
  "$script" --tofu-output "$tmp/tofu-output.json" --pve-node-ssh-host pve01.local \
  --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --evidence "$tmp/fail-evidence.txt" >/dev/null 2>&1; then
  die "RR capture NIC unexpectedly passed"
fi
grep -q 'FAIL_NIC_COUNT' "$tmp/fail-evidence.txt" || die "RR NIC-count failure was not recorded"

echo "sam PVE bridge-audit offline OK"
