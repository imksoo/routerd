#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
SCRIPT="$SCRIPT_DIR/sam-pve-qga-addresses.sh"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/sam-pve-qga.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

die() { echo "sam-pve-qga offline: $*" >&2; exit 1; }

write_output() {
  local boot=${1:-template} host=${2:-pve04.example.test} vmid=${3:-140}
  local public=${4:-} management=${5:-} source=${6:-pending-qga-dhcp}
  [ "$host" = "__missing__" ] && host=
  jq -n \
    --arg boot "$boot" --arg host "$host" --arg public "$public" \
    --arg management "$management" --arg source "$source" --argjson vmid "$vmid" \
    '{fabric:{value:{pve:{boot_source:$boot}}},nodes:{value:{"pve-client-a":{
      site:"pve",vm_id:$vmid,pve_ssh_host:$host,private_ip:"10.77.60.15",
      public_ip:$public,management_ip:$management,pve_management_source:$source
    }}}}' >"$tmp/in.json"
}

write_multi_output() {
  jq -n '{
    fabric:{value:{pve:{boot_source:"template"}}},
    nodes:{value:{
      "pve-leaf-a": {site:"pve",role:"leaf",vm_id:141,pve_ssh_host:"pve01.example.test",private_ip:"10.77.60.34",public_ip:"",management_ip:"",pve_management_source:"pending-qga-dhcp"},
      "pve-client-a": {site:"pve",role:"client",vm_id:144,pve_ssh_host:"pve01.example.test",private_ip:"10.77.60.15",public_ip:"",management_ip:"",pve_management_source:"pending-qga-dhcp"},
      "pve-leaf-b": {site:"pve",role:"leaf",vm_id:145,pve_ssh_host:"pve01.example.test",private_ip:"10.77.60.35",public_ip:"",management_ip:"",pve_management_source:"pending-qga-dhcp"},
      "pve-client-b": {site:"pve",role:"client",vm_id:146,pve_ssh_host:"pve01.example.test",private_ip:"10.77.60.19",public_ip:"",management_ip:"",pve_management_source:"pending-qga-dhcp"},
      "pve-rr-a": {site:"pve",role:"rr",vm_id:142,pve_ssh_host:"pve05.example.test",private_ip:null,public_ip:"",management_ip:"",pve_management_source:"pending-qga-dhcp"},
      "pve-rr-b": {site:"pve",role:"rr",vm_id:143,pve_ssh_host:"pve06.example.test",private_ip:null,public_ip:"",management_ip:"",pve_management_source:"pending-qga-dhcp"}
    }}
  }' >"$tmp/in.json"
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
    printf '%s\n' "${QGA_AGENT_MODE:-1}"
    ;;
  *"network-get-interfaces"*)
    if [ -n "${QGA_IP_OVERRIDE:-}" ]; then
      ip=$QGA_IP_OVERRIDE
    else
      case "$*" in
        *"qm agent 141 "*) ip=192.0.2.21 ;;
        *"qm agent 144 "*) ip=192.0.2.22 ;;
        *"qm agent 145 "*) ip=192.0.2.23 ;;
        *"qm agent 146 "*) ip=192.0.2.24 ;;
        *"root@pve05.example.test"*) ip=192.0.2.25 ;;
        *"root@pve06.example.test"*) ip=192.0.2.26 ;;
        *) ip=192.0.2.20 ;;
      esac
    fi
    capture_ip=
    capture_mac=
    case "$*" in
      *"qm agent 141 "*) capture_ip=10.77.60.34; capture_mac=02:00:00:00:00:41 ;;
      *"qm agent 145 "*) capture_ip=10.77.60.35; capture_mac=02:00:00:00:00:45 ;;
    esac
    if [ -n "${QGA_CAPTURE_MAC_OVERRIDE:-}" ] && [ -n "$capture_mac" ]; then
      capture_mac=$QGA_CAPTURE_MAC_OVERRIDE
    fi
    if [ -n "$capture_ip" ]; then
      printf '[{"name":"ens18","hardware-address":"02:00:00:00:18:00","ip-addresses":[{"ip-address-type":"ipv4","ip-address":"%s"}]},{"name":"ens19","hardware-address":"%s","ip-addresses":[{"ip-address-type":"ipv4","ip-address":"%s"}]}]\n' "$ip" "$capture_mac" "$capture_ip"
    else
      printf '[{"name":"ens18","hardware-address":"02:00:00:00:18:00","ip-addresses":[{"ip-address-type":"ipv4","ip-address":"%s"}]}]\n' "$ip"
    fi
    ;;
  *"qm guest exec"*)
    printf '{"exitcode":0,"out-data":"%s\\n"}\n' "$QGA_HOST_KEY"
    ;;
  *) exit 1 ;;
esac
SH
chmod +x "$fake_bin/ssh"

ssh-keygen -q -t ed25519 -N '' -f "$tmp/guest-host-key"
QGA_HOST_KEY="$(awk '{print $1 " " $2}' "$tmp/guest-host-key.pub")"
export QGA_HOST_KEY
pve_known_hosts="$tmp/pve-known_hosts"
for host in pve01.example.test pve04.example.test pve05.example.test pve06.example.test; do
  printf '%s %s\n' "$host" "$QGA_HOST_KEY" >>"$pve_known_hosts"
done
chmod 600 "$pve_known_hosts"

write_output template
: >"$tmp/ssh.log"
guest_known_hosts="$tmp/guest-known_hosts"
PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --guest-known-hosts-out "$guest_known_hosts" --retries 1 --retry-sleep 0 --evidence "$tmp/evidence" >/dev/null
grep -q 'qga_configured=true' "$tmp/evidence" || die "missing QGA capability evidence"
grep -q 'host_key_type=ssh-ed25519 fingerprint=SHA256:' "$tmp/evidence" || die "missing QGA host-key fingerprint evidence"
grep -q '192.0.2.20' "$tmp/out.json" || die "template QGA address was not patched"
jq -e '.nodes.value["pve-client-a"].management_ip == "192.0.2.20" and .nodes.value["pve-client-a"].public_ip == "192.0.2.20" and .nodes.value["pve-client-a"].pve_management_source == "qga-dhcp"' "$tmp/out.json" >/dev/null || die "QGA result did not become the only management source"
jq -e --arg key "$QGA_HOST_KEY" '.nodes.value["pve-client-a"].ssh_host_key_source == "qga" and .nodes.value["pve-client-a"].ssh_host_keys == [$key]' "$tmp/out.json" >/dev/null || die "QGA result did not record the pinned host key"
[ "$(stat -c %a "$guest_known_hosts")" = 600 ] || die "guest known_hosts must be mode 0600"
grep -Fx "192.0.2.20 $QGA_HOST_KEY" "$guest_known_hosts" >/dev/null || die "QGA host key was not bound to the discovered management address"

write_output iso
: >"$tmp/ssh.log"
PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --retries 1 --retry-sleep 0 >/dev/null
grep -q '192.0.2.20' "$tmp/out.json" || die "ISO QGA address was not patched"

write_output template pve06.example.test 141 192.0.2.55
: >"$tmp/ssh.log"
if PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --retries 1 >"$tmp/stdout" 2>"$tmp/stderr"; then die "static management address unexpectedly passed"; fi
[ "$(wc -l <"$tmp/ssh.log")" -eq 0 ] || die "static management address reached QGA"
grep -q PVEQGAStaticManagementAddress "$tmp/stderr" || die "missing static-management diagnostic"

write_output template pve06.example.test 141 '' '' qga-dhcp
: >"$tmp/ssh.log"
if PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --retries 1 >"$tmp/stdout" 2>"$tmp/stderr"; then die "predeclared QGA source unexpectedly passed"; fi
[ "$(wc -l <"$tmp/ssh.log")" -eq 0 ] || die "predeclared management source reached QGA"
grep -q PVEQGARecordedManagementAddress "$tmp/stderr" || die "missing incomplete-QGA-attestation diagnostic"

write_output template __missing__
: >"$tmp/ssh.log"
if PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --retries 1 >"$tmp/stdout" 2>"$tmp/stderr"; then die "missing PVE host unexpectedly passed"; fi
[ "$(wc -l <"$tmp/ssh.log")" -eq 0 ] || die "missing PVE host reached SSH"
grep -q PVEQGATransportUnavailable "$tmp/stderr" || die "missing PVE-host diagnostic"

write_output template
: >"$tmp/ssh.log"
if PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" QGA_AGENT_MODE=0 "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --retries 1 >"$tmp/stdout" 2>"$tmp/stderr"; then die "disabled agent unexpectedly passed"; fi
[ "$(wc -l <"$tmp/ssh.log")" -eq 1 ] || die "disabled agent entered readiness retry"
grep -q PVEQGADisabled "$tmp/stderr" || die "missing disabled-agent diagnostic"

write_output template
: >"$tmp/ssh.log"
if PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" QGA_TRANSPORT_FAIL=1 "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --retries 1 --evidence "$tmp/evidence" >"$tmp/stdout" 2>"$tmp/stderr"; then die "unavailable transport unexpectedly passed"; fi
grep -q PVEQGATransportUnavailable "$tmp/stderr" || die "missing transport diagnostic"
grep -q 'permission denied' "$tmp/evidence" || die "transport stderr was not preserved in evidence"

write_output template
: >"$tmp/ssh.log"
if PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" QGA_IP_OVERRIDE=224.0.0.1 "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --retries 1 --retry-sleep 0 >"$tmp/stdout" 2>"$tmp/stderr"; then die "multicast QGA address unexpectedly passed"; fi
grep -q 'exactly one usable DHCP IPv4' "$tmp/stderr" || die "missing unicast-only QGA diagnostic"

write_output template
: >"$tmp/ssh.log"
if PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" QGA_IP_OVERRIDE=240.0.0.1 "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --retries 1 --retry-sleep 0 >"$tmp/stdout" 2>"$tmp/stderr"; then die "reserved QGA address unexpectedly passed"; fi
grep -q 'exactly one usable DHCP IPv4' "$tmp/stderr" || die "missing reserved-address QGA diagnostic"

write_output template
: >"$tmp/ssh.log"
if PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" QGA_IP_OVERRIDE=192.168.001.20 "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --retries 1 --retry-sleep 0 >"$tmp/stdout" 2>"$tmp/stderr"; then die "non-canonical QGA address unexpectedly passed"; fi
grep -q 'exactly one usable DHCP IPv4' "$tmp/stderr" || die "missing canonical-address QGA diagnostic"

write_output template
: >"$tmp/ssh.log"
if PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" QGA_HOST_KEY='not-a-valid-host-key' "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --tofu-output "$tmp/in.json" --out "$tmp/host-key-fail-output.json" --guest-known-hosts-out "$tmp/host-key-fail-known_hosts" --retries 1 --retry-sleep 0 >"$tmp/stdout" 2>"$tmp/stderr"; then die "invalid QGA SSH host key unexpectedly passed"; fi
grep -q PVEQGAHostKeyUnavailable "$tmp/stderr" || die "missing invalid-host-key diagnostic"
[ ! -e "$tmp/host-key-fail-output.json" ] || die "invalid QGA host key wrote a patched output"
[ ! -e "$tmp/host-key-fail-known_hosts" ] || die "invalid QGA host key wrote known_hosts"

write_multi_output
: >"$tmp/ssh.log"
guest_known_hosts="$tmp/multi-guest-known_hosts"
PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --tofu-output "$tmp/in.json" --out "$tmp/out.json" --guest-known-hosts-out "$guest_known_hosts" --retries 1 --retry-sleep 0 --evidence "$tmp/multi-evidence" >/dev/null
for pair in 'pve-leaf-a:192.0.2.21' 'pve-client-a:192.0.2.22' 'pve-leaf-b:192.0.2.23' 'pve-client-b:192.0.2.24' 'pve-rr-a:192.0.2.25' 'pve-rr-b:192.0.2.26'; do
  node=${pair%%:*}; ip=${pair#*:}
  jq -e --arg node "$node" --arg ip "$ip" --arg key "$QGA_HOST_KEY" '.nodes.value[$node].management_ip == $ip and .nodes.value[$node].pve_management_source == "qga-dhcp" and .nodes.value[$node].ssh_host_key_source == "qga" and .nodes.value[$node].ssh_host_keys == [$key]' "$tmp/out.json" >/dev/null || die "missing per-host QGA result for $node"
  grep -Fx "$ip $QGA_HOST_KEY" "$guest_known_hosts" >/dev/null || die "missing QGA host-key binding for $node"
done
jq -e '
  .nodes.value["pve-leaf-a"].capture_mac == "02:00:00:00:00:41" and
  .nodes.value["pve-leaf-b"].capture_mac == "02:00:00:00:00:45" and
  (.nodes.value["pve-client-a"] | has("capture_mac") | not) and
  (.nodes.value["pve-rr-a"] | has("capture_mac") | not)
' "$tmp/out.json" >/dev/null || die "QGA did not record only PVE leaf capture MACs"
[ "$(grep -c 'capture_ifname=ens19' "$tmp/multi-evidence")" -eq 2 ] || die "capture-interface evidence is missing for a PVE leaf"

# A QGA-attested output is deliberately rerunnable: it must re-query the
# guest, retain the same management address, and replace any stale capture MAC
# with the observed interface MAC rather than trusting JSON carried forward.
jq '.nodes.value["pve-leaf-a"].capture_mac = "02:00:00:00:00:ff"' "$tmp/out.json" >"$tmp/rerun-input.json"
PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --tofu-output "$tmp/rerun-input.json" --out "$tmp/rerun-output.json" --guest-known-hosts-out "$tmp/rerun-known_hosts" --retries 1 --retry-sleep 0 >/dev/null
jq -e '.nodes.value["pve-leaf-a"].management_ip == "192.0.2.21" and .nodes.value["pve-leaf-a"].capture_mac == "02:00:00:00:00:41"' "$tmp/rerun-output.json" >/dev/null || die "QGA rerun did not re-attest management IP and replace capture MAC"

# A previously QGA-attested management address is not silently rewritten if
# the guest changes underneath it. The operator must investigate the identity
# change rather than carry it forward as an ordinary refresh.
jq '.nodes.value["pve-leaf-a"].management_ip = "192.0.2.99" | .nodes.value["pve-leaf-a"].public_ip = "192.0.2.99"' "$tmp/out.json" >"$tmp/mismatch-input.json"
if PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --tofu-output "$tmp/mismatch-input.json" --out "$tmp/mismatch-output.json" --guest-known-hosts-out "$tmp/mismatch-known_hosts" --retries 1 --retry-sleep 0 >"$tmp/stdout" 2>"$tmp/stderr"; then die "QGA rerun accepted a changed management address"; fi
grep -q PVEQGAManagementAddressMismatch "$tmp/stderr" || die "missing QGA management-address mismatch diagnostic"

# A unicast router interface MAC is required before it can enter the shared
# ARP-observer ignore set.
if PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/ssh.log" QGA_CAPTURE_MAC_OVERRIDE=01:00:00:00:00:41 "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --tofu-output "$tmp/out.json" --out "$tmp/invalid-capture-output.json" --guest-known-hosts-out "$tmp/invalid-capture-known_hosts" --retries 1 --retry-sleep 0 >"$tmp/stdout" 2>"$tmp/stderr"; then die "invalid QGA capture MAC unexpectedly passed"; fi
grep -q PVEQGACaptureMACUnavailable "$tmp/stderr" || die "missing invalid capture-MAC diagnostic"
[ "$(wc -l <"$guest_known_hosts")" -eq 6 ] || die "expected a QGA-pinned known_hosts entry for every PVE guest"
grep -q 'root@pve01.example.test' "$tmp/ssh.log" || die "leaf QGA did not use its PVE host"
grep -q 'root@pve05.example.test' "$tmp/ssh.log" || die "RR A QGA did not use its own PVE host"
grep -q 'root@pve06.example.test' "$tmp/ssh.log" || die "RR B QGA did not use its own PVE host"
grep -q 'StrictHostKeyChecking=yes' "$tmp/ssh.log" || die "QGA SSH did not require a known PVE host key"
grep -Fq "UserKnownHostsFile=$pve_known_hosts" "$tmp/ssh.log" || die "QGA SSH did not use the supplied PVE known_hosts"
grep -Fq 'GlobalKnownHostsFile=/dev/null' "$tmp/ssh.log" || die "QGA SSH consulted ambient global known_hosts"

echo "sam PVE QGA offline OK"
