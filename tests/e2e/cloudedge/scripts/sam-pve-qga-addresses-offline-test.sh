#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
SCRIPT="$SCRIPT_DIR/sam-pve-qga-addresses.sh"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/sam-pve-qga.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

die() { echo "sam-pve-qga offline: $*" >&2; exit 1; }

write_output() {
  local boot=$1 public=${2:-} private=${3:-} host=${4:-pve06} vmid=${5:-141}
  jq -n --arg boot "$boot" --arg public "$public" --arg private "$private" --arg host "$host" --argjson vmid "$vmid" \
    '{fabric:{value:{pve:{node_ssh_host:$host,boot_source:$boot}}},nodes:{value:{"pve-client-a":{site:"pve",vm_id:$vmid,public_ip:$public,private_ip:$private}}}}' >"$tmp/in.json"
}

fake_bin="$tmp/bin"; mkdir -p "$fake_bin"
ssh_key="$tmp/pve_ssh"
: >"$ssh_key"
chmod 0600 "$ssh_key"
cat >"$fake_bin/ssh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
echo "$*" >>"$QGA_SSH_LOG"
case "$*" in
  *"qm config"*)
    [ "${QGA_TRANSPORT_FAIL:-0}" = 1 ] && { echo "permission denied" >&2; exit 255; }
    printf '%s\n' "${QGA_AGENT_MODE:-1}" ;;
  *"network-get-interfaces"*) printf '[{"name":"ens18","ip-addresses":[{"ip-address-type":"ipv4","ip-address":"192.0.2.20"}]}]\n' ;;
  *) exit 1 ;;
esac
SH
chmod +x "$fake_bin/ssh"

write_output template
: >"$tmp/ssh.log"
if PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" "$SCRIPT" --ssh-key "$ssh_key" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --retries 1 >"$tmp/stdout" 2>"$tmp/stderr"; then die "template unexpectedly passed"; fi
[ "$(wc -l <"$tmp/ssh.log")" -eq 0 ] || die "template used SSH"
grep -q PVEQGAUnsupportedBootSource "$tmp/stderr" || die "missing boot-source diagnostic"

write_output iso
: >"$tmp/ssh.log"
PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" "$SCRIPT" --ssh-key "$ssh_key" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --retries 1 --retry-sleep 0 --evidence "$tmp/evidence" >/dev/null
grep -q 'qga_configured=true' "$tmp/evidence" || die "missing safe capability evidence"
grep -q '192.0.2.20' "$tmp/out.json" || die "QGA address was not patched"

write_output iso
: >"$tmp/ssh.log"
if PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" QGA_AGENT_MODE=0 "$SCRIPT" --ssh-key "$ssh_key" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --retries 1 >"$tmp/stdout" 2>"$tmp/stderr"; then die "disabled agent unexpectedly passed"; fi
[ "$(wc -l <"$tmp/ssh.log")" -eq 1 ] || die "disabled agent entered readiness retry"
grep -q PVEQGADisabled "$tmp/stderr" || die "missing disabled-agent diagnostic"

write_output iso
: >"$tmp/ssh.log"
if PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" QGA_TRANSPORT_FAIL=1 "$SCRIPT" --ssh-key "$ssh_key" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --retries 1 --evidence "$tmp/evidence" >"$tmp/stdout" 2>"$tmp/stderr"; then die "unavailable transport unexpectedly passed"; fi
grep -q PVEQGATransportUnavailable "$tmp/stderr" || die "missing transport diagnostic"
grep -q 'permission denied' "$tmp/evidence" || die "transport stderr was not preserved in evidence"

write_output template '' 10.0.0.20
: >"$tmp/ssh.log"
PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" "$SCRIPT" --ssh-key "$ssh_key" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --retries 1 >/dev/null
[ "$(wc -l <"$tmp/ssh.log")" -eq 0 ] || die "private address should bypass QGA"

write_output template '' 10.0.0.20 '' 141
: >"$tmp/ssh.log"
PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" "$SCRIPT" --ssh-key "$ssh_key" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --retries 1 >/dev/null
[ "$(wc -l <"$tmp/ssh.log")" -eq 0 ] || die "private address should not require a PVE SSH host"

write_output template '' 10.0.0.20 pve06 null
: >"$tmp/ssh.log"
PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" "$SCRIPT" --ssh-key "$ssh_key" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --retries 1 >/dev/null
[ "$(wc -l <"$tmp/ssh.log")" -eq 0 ] || die "private address should not require a VM ID"

write_output iso
: >"$tmp/ssh.log"
PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" QGA_AGENT_MODE='enabled=1,fstrim_cloned_disks=1' "$SCRIPT" --ssh-key "$ssh_key" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --retries 1 --retry-sleep 0 >/dev/null
grep -q '192.0.2.20' "$tmp/out.json" || die "enabled=1 QGA agent was not accepted"

echo "sam PVE QGA offline OK"
