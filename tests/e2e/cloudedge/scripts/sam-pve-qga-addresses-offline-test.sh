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
  *"pveversion && qm list"*) echo 'offline PVE inventory' ;;
  *"pvesh get /nodes/pve01/qemu/140/config"*)
    echo '{"template":1,"scsi0":"qnap:fixture-stage-disk"}'
    ;;
  *"qm config"*)
    [ "${QGA_TRANSPORT_FAIL:-0}" = 1 ] && { echo "permission denied" >&2; exit 255; }
    printf '%s\n' "${QGA_AGENT_MODE:-1}"
    ;;
  *"network-get-interfaces"*)
    if [ -n "${QGA_NETWORK_CASE_DIR:-}" ]; then
      call=0
      [ ! -f "$QGA_NETWORK_CASE_DIR/count" ] || read -r call <"$QGA_NETWORK_CASE_DIR/count"
      call=$((call + 1))
      printf '%s\n' "$call" >"$QGA_NETWORK_CASE_DIR/count"
      [ ! -f "$QGA_NETWORK_CASE_DIR/$call.stderr" ] || cat "$QGA_NETWORK_CASE_DIR/$call.stderr" >&2
      cat "$QGA_NETWORK_CASE_DIR/$call.json"
      [ ! -f "$QGA_NETWORK_CASE_DIR/$call.rc" ] || exit "$(cat "$QGA_NETWORK_CASE_DIR/$call.rc")"
      exit 0
    fi
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
      printf '[{"name":"%s","hardware-address":"02:00:00:00:18:00","ip-addresses":[{"ip-address-type":"ipv4","ip-address":"%s"}]},{"name":"%s","hardware-address":"%s","ip-addresses":[{"ip-address-type":"ipv4","ip-address":"%s"}]}]\n' "${QGA_MANAGEMENT_IFNAME:-ens18}" "$ip" "${QGA_CAPTURE_IFNAME:-ens19}" "$capture_mac" "$capture_ip"
    else
      printf '[{"name":"%s","hardware-address":"02:00:00:00:18:00","ip-addresses":[{"ip-address-type":"ipv4","ip-address":"%s"}]}]\n' "${QGA_MANAGEMENT_IFNAME:-ens18}" "$ip"
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

# Exercise successful QGA responses before DHCP has assigned management IPv4.
# The first snapshot reproduces the saved failure's shape (eth0 IPv6 only,
# eth1 capture IPv4), using documentation-only addresses and no live inputs.
network_cases="$tmp/network-cases"
mkdir -p "$network_cases"
jq -n '[
  {name:"eth0","ip-addresses":[{"ip-address-type":"ipv6","ip-address":"2001:db8::20"}]},
  {name:"eth1","ip-addresses":[{"ip-address-type":"ipv4","ip-address":"10.77.60.15"}]}
]' >"$network_cases/pending.json"
jq '.[0]["ip-addresses"] += [{"ip-address-type":"ipv4","ip-address":"192.0.2.20"}]' \
  "$network_cases/pending.json" >"$network_cases/ready.json"

run_network_case() {
  local name="$1" expected="$2" expected_calls="$3" first_filter="$4"
  local case_dir="$network_cases/$name" rc=0
  mkdir -p "$case_dir"
  jq "$first_filter" "$network_cases/ready.json" >"$case_dir/1.json"
  cp "$network_cases/ready.json" "$case_dir/2.json"
  cp "$network_cases/ready.json" "$case_dir/3.json"
  case "$name" in
    delayed-ipv4) cp "$network_cases/pending.json" "$case_dir/1.json" ;;
    exhausted)
      for call in 1 2 3; do cp "$network_cases/pending.json" "$case_dir/$call.json"; done ;;
    transport-then-ready)
      printf 'fixture QGA transport not ready\n' >"$case_dir/1.stderr"
      printf '17\n' >"$case_dir/1.rc" ;;
    malformed-json) printf '{broken\n' >"$case_dir/1.json" ;;
  esac
  write_output template
  PATH="$fake_bin:$PATH" QGA_SSH_LOG="$case_dir/ssh.log" QGA_NETWORK_CASE_DIR="$case_dir" \
    timeout 10s "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" \
      --tofu-output "$tmp/in.json" --out "$case_dir/out.json" --management-ifname eth0 \
      --guest-known-hosts-out "$case_dir/known_hosts" --retries 3 --retry-sleep 0 \
      --evidence "$case_dir/evidence" >"$case_dir/stdout" 2>"$case_dir/stderr" || rc=$?
  if [ "$expected" = pass ]; then
    if [ "$rc" -ne 0 ]; then
      cat "$case_dir/stderr" >&2
      die "$name: QGA command success must not bypass management IPv4 readiness"
    fi
    jq -e '.nodes.value["pve-client-a"].management_ip == "192.0.2.20" and
      .nodes.value["pve-client-a"].pve_management_source == "qga-dhcp"' "$case_dir/out.json" >/dev/null ||
      die "$name: readiness accepted capture IPv4 instead of the declared management address"
    grep -Fx "192.0.2.20 $QGA_HOST_KEY" "$case_dir/known_hosts" >/dev/null || die "$name: missing host-key binding"
  else
    [ "$rc" -eq 1 ] || die "$name: expected bounded rejection, got exit=$rc"
    [ ! -e "$case_dir/out.json" ] || die "$name: wrote partial output"
    [ ! -e "$case_dir/known_hosts" ] || die "$name: wrote partial known_hosts"
    if grep -q 'qm guest exec' "$case_dir/ssh.log"; then die "$name: invalid network observation reached host-key discovery"; fi
  fi
  [ "$(cat "$case_dir/count")" -eq "$expected_calls" ] || die "$name: wrong network observation count"
  if [ "$name" = delayed-ipv4 ] || [ "$name" = exhausted ]; then
    grep -q 'network_attempt=1.*state=waiting-management-ipv4' "$case_dir/evidence" || die "$name: lost initial pending evidence"
    grep -q '2001:db8::20' "$case_dir/evidence" || die "$name: lost raw pending snapshot"
  fi
  if [ "$name" = exhausted ]; then
    grep -q 'network_attempt=3.*state=waiting-management-ipv4' "$case_dir/evidence" || die "exhaustion lost final attempt evidence"
    grep -q 'exactly one usable DHCP IPv4' "$case_dir/stderr" || die "exhaustion lost IPv4 diagnostic"
  fi
  if [ "$name" = transport-then-ready ]; then
    grep -q 'network_attempt=1.*state=transport-unavailable' "$case_dir/evidence" || die "lost transport-attempt evidence"
    grep -q 'fixture QGA transport not ready' "$case_dir/evidence" || die "lost transport error evidence"
  fi
}

run_network_case delayed-ipv4 pass 2 '.'
run_network_case exhausted fail 3 '.'
run_network_case transport-then-ready pass 2 '.'
# Every invalid first snapshot is followed by a valid snapshot. It must fail
# immediately, rather than hide a bad identity/address behind a later retry.
run_network_case wrong-management fail 1 '.[0].name = "ens18"'
run_network_case duplicate-management fail 1 '. + [.[0]]'
run_network_case duplicate-empty-management fail 1 '.[0]["ip-addresses"] = [] | . + [.[0]]'
run_network_case multiple-ipv4 fail 1 '.[0]["ip-addresses"] += [{"ip-address-type":"ipv4","ip-address":"192.0.2.21"}]'
run_network_case invalid-ipv4 fail 1 '.[0]["ip-addresses"][1]["ip-address"] = "224.0.0.1"'
run_network_case mixed-invalid-ipv4 fail 1 '.[0]["ip-addresses"] += [{"ip-address-type":"ipv4","ip-address":"224.0.0.1"}]'
run_network_case missing-addresses pass 2 'del(.[0]["ip-addresses"])'
run_network_case false-addresses fail 1 '.[0]["ip-addresses"] = false'
run_network_case null-addresses fail 1 '.[0]["ip-addresses"] = null'
run_network_case object-addresses fail 1 '.[0]["ip-addresses"] = {}'
run_network_case string-addresses fail 1 '.[0]["ip-addresses"] = "pending"'
run_network_case malformed-json fail 1 '.'
run_network_case malformed-shape fail 1 '{result:{}}'

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

# A valid management address already owned by another guest is never a
# readiness condition. Even after one guest succeeds, publish no partial set.
: >"$tmp/duplicate-ssh.log"
if PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/duplicate-ssh.log" QGA_IP_OVERRIDE=192.0.2.20 \
  "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" \
    --tofu-output "$tmp/in.json" --out "$tmp/duplicate-output.json" \
    --guest-known-hosts-out "$tmp/duplicate-known_hosts" --retries 3 --retry-sleep 0 \
    >"$tmp/stdout" 2>"$tmp/stderr"; then die "different guests accepted the same management IPv4"; fi
grep -q PVEQGADuplicateManagementAddress "$tmp/stderr" || die "missing cross-guest duplicate diagnostic"
[ "$(grep -c network-get-interfaces "$tmp/duplicate-ssh.log")" -eq 2 ] || die "cross-guest duplicate was retried"
if [ -e "$tmp/duplicate-output.json" ] || [ -e "$tmp/duplicate-known_hosts" ]; then die "cross-guest duplicate published partial artifacts"; fi

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
: >"$tmp/mismatch-ssh.log"
if PATH="$fake_bin:$PATH" QGA_SSH_LOG="$tmp/mismatch-ssh.log" "$SCRIPT" --pve-ssh-key "$ssh_key" --pve-known-hosts "$pve_known_hosts" --tofu-output "$tmp/mismatch-input.json" --out "$tmp/mismatch-output.json" --guest-known-hosts-out "$tmp/mismatch-known_hosts" --retries 3 --retry-sleep 0 >"$tmp/stdout" 2>"$tmp/stderr"; then die "QGA rerun accepted a changed management address"; fi
grep -q PVEQGAManagementAddressMismatch "$tmp/stderr" || die "missing QGA management-address mismatch diagnostic"
[ "$(grep -c network-get-interfaces "$tmp/mismatch-ssh.log")" -eq 1 ] || die "changed attested management IPv4 was retried"
if [ -e "$tmp/mismatch-output.json" ] || [ -e "$tmp/mismatch-known_hosts" ]; then die "changed attested management IPv4 published artifacts"; fi

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

# Exercise the closed release driver with the real QGA script, not a copied
# argument list. Only provisioning/common.sh boundaries are fake, and the
# bridge audit deliberately stops immediately after QGA's successful handoff.
# The existing standalone cases above retain their ens18/ens19 defaults.
repo_root="$(cd "$SCRIPT_DIR/../../../.." && pwd)"
driver_root="$tmp/release-driver"
mkdir -p "$driver_root/drivers" "$driver_root/tf"
cp "$repo_root/tools/release-qa-labs/drivers/pve-certification-driver.sh" "$driver_root/drivers/"
write_multi_output
cp "$tmp/in.json" "$driver_root/pve-output.json"
jq -n '{pve:{sshHost:"pve01.example.test",templateStage:{sourceNode:"pve01",vmid:140,datastore:"qnap"},
  rrNodes:{"pve-rr-a":{sshHost:"pve05.example.test"},"pve-rr-b":{sshHost:"pve06.example.test"}}},
  limits:{maxEstimatedCostUsd:1.60}}' >"$driver_root/contract.json"
cat >"$driver_root/drivers/common.sh" <<'COMMON'
set -euo pipefail
contract_path="$QGA_DRIVER_ROOT/contract.json"
framework_root="$QGA_DRIVER_ROOT"
evidence_root="$QGA_DRIVER_CASE/evidence"
plan_root="$QGA_DRIVER_CASE/plans"
tf_dir="$QGA_DRIVER_ROOT/tf"
tofu_state_path="$QGA_DRIVER_CASE/state"
tfvars_path="$QGA_DRIVER_ROOT/unused.tfvars"
pve_ssh_private_key="$QGA_DRIVER_KEY"
pve_ssh_known_hosts="$QGA_DRIVER_KNOWN_HOSTS"
pve_ssh_host=pve01.example.test
parse_driver_args() { out_arg="$QGA_DRIVER_CASE/result.json"; }
reset_checks() { :; }
record_check() { :; }
write_driver_result() { printf '%s\n' "$3" >"$1"; }
require_command() { command -v "$1" >/dev/null; }
require_supervisor_mutating() { :; }
touch_heartbeat() { :; }
run_with_progress() {
  local label="$1"
  shift
  if [ "$label" = pve-qga-addresses ]; then
    printf '%s\n' "$@" >"$QGA_DRIVER_CASE/qga-argv"
  fi
  "$@"
}
routerd_script() {
  case "$1" in
    */sam-pve-qga-addresses.sh) printf '%s\n' "$QGA_DRIVER_SCRIPT" ;;
    */sam-pve-bridge-audit.sh) printf '%s\n' "$QGA_DRIVER_ROOT/stop-after-qga" ;;
    *) return 97 ;;
  esac
}
COMMON
cat >"$driver_root/drivers/pve-capture-bridge.sh" <<'BRIDGE'
#!/bin/sh
exit 0
BRIDGE
cat >"$driver_root/stop-after-qga" <<'STOP'
#!/bin/sh
touch "$QGA_DRIVER_CASE/qga-completed"
exit 42
STOP
cat >"$driver_root/qa_guard.py" <<'GUARD'
# Pure fixture provisioning boundary. The real QGA identity guard is not mocked.
raise SystemExit(0)
GUARD
cat >"$fake_bin/tofu" <<'TOFU'
#!/bin/sh
set -eu
case " $* " in
  *" show -json "*)
    echo '{"planned_values":{"root_module":{"resources":[{"type":"proxmox_virtual_environment_vm","name":"pve_shared_template_stage"}]}},"resource_changes":[{"type":"proxmox_virtual_environment_vm","name":"pve_shared_template_stage","change":{"actions":["create"]}}]}' ;;
  *" output -json pve_nodes "*) jq '.nodes.value' "$QGA_DRIVER_ROOT/pve-output.json" ;;
  *" output -json pve_fabric "*) jq '.fabric.value.pve' "$QGA_DRIVER_ROOT/pve-output.json" ;;
  *" init "*|*" plan "*|*" apply "*) : ;;
  *) echo 'unexpected fake tofu invocation' >&2; exit 97 ;;
esac
TOFU
chmod +x "$driver_root/drivers/pve-capture-bridge.sh" "$driver_root/stop-after-qga" "$fake_bin/tofu"

run_release_driver_case() {
  local name="$1" management="$2" capture="$3" expected="$4" diagnostic="${5:-}"
  local case_dir="$driver_root/$name" rc=0
  mkdir -p "$case_dir/evidence" "$case_dir/plans"
  PATH="$fake_bin:$PATH" QGA_SSH_LOG="$case_dir/ssh.log" \
    QGA_MANAGEMENT_IFNAME="$management" QGA_CAPTURE_IFNAME="$capture" \
    QGA_DRIVER_ROOT="$driver_root" QGA_DRIVER_CASE="$case_dir" QGA_DRIVER_SCRIPT="$SCRIPT" \
    QGA_DRIVER_KEY="$ssh_key" QGA_DRIVER_KNOWN_HOSTS="$pve_known_hosts" \
    timeout 20s bash "$driver_root/drivers/pve-certification-driver.sh" \
      >"$case_dir/driver.log" 2>&1 || rc=$?
  [ "$rc" -eq 1 ] || die "unexpected release-driver fixture exit=$rc ($name)"
  if [ "$expected" = pass ]; then
    if [ ! -e "$case_dir/qga-completed" ]; then
      cat "$case_dir/driver.log" >&2
      die "release driver did not attest PVE cloud-init eth0 management / ens19 leaf capture"
    fi
    [ "$(awk 'previous == "--management-ifname" {print} {previous=$0}' "$case_dir/qga-argv")" = eth0 ] ||
      die "release driver must explicitly select eth0 management"
    [ "$(awk 'previous == "--capture-ifname" {print} {previous=$0}' "$case_dir/qga-argv")" = ens19 ] ||
      die "release driver must explicitly select ens19 leaf capture"
    jq -e '[.nodes.value[] | select(.pve_management_source == "qga-dhcp" and .ssh_host_key_source == "qga")] | length == 6' \
      "$case_dir/evidence/certification/pve/tofu-output-pve-qga.json" >/dev/null ||
      die "release driver lost six-guest QGA/IP/host-key attestation"
    [ "$(wc -l <"$case_dir/evidence/certification/pve/guest-known_hosts")" -eq 6 ] ||
      die "release driver lost guest known-host bindings"
  else
    [ ! -e "$case_dir/qga-completed" ] || die "release driver accepted wrong $name interface identity"
    grep -Fq "$diagnostic" "$case_dir/driver.log" || die "release driver lost strict $name diagnostic"
  fi
}

run_release_driver_case cloud-init eth0 ens19 pass
run_release_driver_case wrong-management ens18 ens19 fail 'exactly one usable DHCP IPv4'
run_release_driver_case wrong-capture eth0 eth1 fail PVEQGACaptureMACUnavailable

echo "sam PVE QGA offline OK"
