#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: sam-pve-qga-addresses.sh --tofu-output IN --out OUT [options]

Discover PVE guest management addresses, capture-interface MACs, and SSH host
keys from QGA, then patch tofu-output.json. Every PVE guest obtains its
management IPv4 from the existing PVE underlay DHCP service. This script copies
the QGA-reported address into each PVE node's management_ip and public_ip only
after proving QGA is enabled on that VM. For PVE leaf routers, it also records
the MAC of the declared capture interface, which is intentionally unaddressed
until routerd applies its MobilityPool-owned /32. It reads the guest's public
SSH host key through the authenticated PVE/QGA path and binds it to that
discovered address.

Options:
  --tofu-output FILE       Raw `tofu output -json` file.
  --out FILE               Patched output file for sam-e2e.sh.
  --pve-ssh-key FILE       Exact root private key for PVE node SSH.
  --pve-known-hosts FILE   Pinned known_hosts for the PVE hypervisor SSH hosts.
  --guest-known-hosts-out FILE
                           Write QGA-pinned PVE guest known_hosts (default: OUT.guest-known_hosts).
  --management-ifname NAME Management interface reported by QGA (default: ens18).
  --capture-ifname NAME    PVE leaf capture interface reported by QGA (default: ens19).
  --retries N              QGA/management-IPv4 readiness attempts per VM (default: 90).
  --retry-sleep SEC        Delay between QGA retries (default: 20).
  --evidence FILE          Write discovery evidence (default: OUT.qga-addresses.txt).
USAGE
}

tofu_output=
out=
pve_ssh_key=
pve_known_hosts=
guest_known_hosts_out=
management_ifname=ens18
capture_ifname=ens19
retries=90
retry_sleep=20
evidence=

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tofu-output) tofu_output=${2:?missing --tofu-output value}; shift 2 ;;
    --out) out=${2:?missing --out value}; shift 2 ;;
    --pve-ssh-key) pve_ssh_key=${2:?missing --pve-ssh-key value}; shift 2 ;;
    --pve-known-hosts) pve_known_hosts=${2:?missing --pve-known-hosts value}; shift 2 ;;
    --guest-known-hosts-out) guest_known_hosts_out=${2:?missing --guest-known-hosts-out value}; shift 2 ;;
    --management-ifname) management_ifname=${2:?missing --management-ifname value}; shift 2 ;;
    --capture-ifname) capture_ifname=${2:?missing --capture-ifname value}; shift 2 ;;
    --retries) retries=${2:?missing --retries value}; shift 2 ;;
    --retry-sleep) retry_sleep=${2:?missing --retry-sleep value}; shift 2 ;;
    --evidence) evidence=${2:?missing --evidence value}; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$tofu_output" ] || { usage >&2; exit 2; }
[ -n "$out" ] || { usage >&2; exit 2; }
[ -n "$pve_ssh_key" ] || { echo "--pve-ssh-key is required" >&2; exit 2; }
[ -f "$pve_ssh_key" ] || { echo "PVE SSH key not found: $pve_ssh_key" >&2; exit 2; }
[ -n "$pve_known_hosts" ] || { echo "--pve-known-hosts is required" >&2; exit 2; }
[ -f "$pve_known_hosts" ] || { echo "PVE known_hosts not found: $pve_known_hosts" >&2; exit 2; }
[ -f "$tofu_output" ] || { echo "tofu output not found: $tofu_output" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }
command -v ssh-keygen >/dev/null || { echo "ssh-keygen is required" >&2; exit 2; }
[[ "$capture_ifname" =~ ^[[:alnum:]_.:-]+$ ]] || {
  echo "--capture-ifname must be a non-empty interface name" >&2
  exit 2
}

evidence=${evidence:-"$out.qga-addresses.txt"}
guest_known_hosts_out=${guest_known_hosts_out:-"$out.guest-known_hosts"}
[ -d "$(dirname "$guest_known_hosts_out")" ] || {
  echo "guest known_hosts parent directory does not exist: $(dirname "$guest_known_hosts_out")" >&2
  exit 2
}
tmp="$(mktemp)"
guest_known_hosts_tmp="$(mktemp)"
trap 'rm -f "$tmp" "$tmp.next" "$tmp.ssh-stderr" "$guest_known_hosts_tmp"' EXIT
cp "$tofu_output" "$tmp"
: >"$evidence"
: >"$guest_known_hosts_tmp"

mapfile -t pve_nodes < <(jq -r '
  .nodes.value
  | to_entries[]
  | select(.value.site == "pve")
  | [.key, (.value.vm_id | tostring), (.value.pve_ssh_host // empty)]
  | @tsv
' "$tofu_output")
[ "${#pve_nodes[@]}" -gt 0 ] || { echo "no PVE nodes found in $tofu_output" >&2; exit 2; }

for entry in "${pve_nodes[@]}"; do
  IFS=$'\t' read -r node vmid pve_node_ssh_host <<<"$entry"
  role="$(jq -r --arg node "$node" '.nodes.value[$node].role // empty' "$tmp")"
  public_ip="$(jq -r --arg node "$node" '.nodes.value[$node].public_ip // empty' "$tmp")"
  management_ip="$(jq -r --arg node "$node" '.nodes.value[$node].management_ip // empty' "$tmp")"
  management_source="$(jq -r --arg node "$node" '.nodes.value[$node].pve_management_source // empty' "$tmp")"
  capture_mac="$(jq -r --arg node "$node" '.nodes.value[$node].capture_mac // empty' "$tmp")"
  case "$management_source" in
    pending-qga-dhcp)
      if [ -n "$public_ip" ] || [ -n "$management_ip" ]; then
        printf 'PVEQGAStaticManagementAddress: node=%s has management data before mandatory QGA discovery\n' "$node" >&2
        exit 2
      fi
      if [ "$role" = "leaf" ] && [ -n "$capture_mac" ]; then
        printf 'PVEQGAStaticCaptureMAC: node=%s has capture_mac before mandatory QGA discovery\n' "$node" >&2
        exit 2
      fi
      ;;
    qga-dhcp)
      # A rerun is allowed only when the previous value was itself QGA
      # attested. The observation below must reproduce it exactly before we
      # refresh the host-key and capture-MAC facts.
      if [ -z "$management_ip" ] || [ "$public_ip" != "$management_ip" ]; then
        printf 'PVEQGARecordedManagementAddress: node=%s has incomplete QGA-attested management data\n' "$node" >&2
        exit 2
      fi
      ;;
    *)
      printf 'PVEQGAManagementSource: node=%s must declare pve_management_source=pending-qga-dhcp or qga-dhcp\n' "$node" >&2
      exit 2
      ;;
  esac
  [ -n "$pve_node_ssh_host" ] || {
    printf 'PVEQGATransportUnavailable: node=%s has no declared PVE SSH host\n' "$node" >&2
    exit 2
  }
done

boot_source="$(jq -r '.fabric.value.pve.boot_source // empty' "$tofu_output")"
case "$boot_source" in
  template|iso) ;;
  *)
  printf 'PVEQGAUnsupportedBootSource: boot_source must be template or iso, got=%s\n' "${boot_source:-<empty>}" >&2
  exit 2
  ;;
esac

valid_unicast_ipv4() {
  local candidate="$1" octet first_octet
  [[ "$candidate" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || return 1
  IFS=. read -r -a octets <<<"$candidate"
  for octet in "${octets[@]}"; do
    # Do not accept an ambiguous octal-looking representation.  The address
    # becomes a bootstrap endpoint and must have one canonical dotted-decimal
    # spelling in evidence, generated configuration, and operator tooling.
    { [ "${#octet}" -eq 1 ] || [ "${octet:0:1}" != 0 ]; } || return 1
    [ "$((10#$octet))" -le 255 ] || return 1
  done
  first_octet="$((10#${octets[0]}))"
  # Only an ordinary unicast address may become a WireGuard bootstrap
  # endpoint. Link-local, loopback, this-network, multicast, the IANA
  # reserved Class-E block, and limited broadcast all fail closed even if QGA
  # reports them. (The pre-existing PVE underlay can legitimately use private
  # RFC1918 space, so that remains permitted.)
  [ "$first_octet" -gt 0 ] && [ "$first_octet" -lt 224 ] || return 1
  [ "$first_octet" -ne 127 ] || return 1
  [[ "$candidate" != 169.254.* ]] || return 1
}

canonical_ethernet_mac() {
  local candidate="$1" first_octet
  candidate="$(printf '%s' "$candidate" | tr '[:upper:]' '[:lower:]')"
  [[ "$candidate" =~ ^([[:xdigit:]]{2}:){5}[[:xdigit:]]{2}$ ]] || return 1
  [ "$candidate" != "00:00:00:00:00:00" ] || return 1
  first_octet="${candidate%%:*}"
  (( (16#$first_octet & 1) == 0 )) || return 1
  printf '%s\n' "$candidate"
}

ssh_qga=(ssh -n -i "$pve_ssh_key" -o BatchMode=yes -o StrictHostKeyChecking=yes \
  -o UserKnownHostsFile="$pve_known_hosts" -o GlobalKnownHostsFile=/dev/null \
  -o CanonicalizeHostname=no -o IdentitiesOnly=yes -o PasswordAuthentication=no \
  -o KbdInteractiveAuthentication=no -o ConnectTimeout=10)
declare -A discovered_management_ips=()
declare -A discovered_capture_macs=()
qga_host_keys=()

valid_guest_host_key_line() {
  local line="$1" key_type key_blob _comment canonical
  read -r key_type key_blob _comment <<<"$line"
  [ -n "$key_type" ] && [ -n "$key_blob" ] || return 1
  case "$key_type" in
    ssh-ed25519|ecdsa-sha2-nistp256|ecdsa-sha2-nistp384|ecdsa-sha2-nistp521|ssh-rsa) ;;
    *) return 1 ;;
  esac
  # The subsequent ssh-keygen check validates the decoded key material.  This
  # inexpensive lexical check keeps malformed QGA output out of known_hosts
  # before it reaches that parser.
  [[ "$key_blob" =~ ^[A-Za-z0-9+/]+={0,3}$ ]] || return 1
  canonical="$key_type $key_blob"
  printf '%s\n' "$canonical" | ssh-keygen -lf - >/dev/null 2>&1 || return 1
  printf '%s\n' "$canonical"
}

qga_guest_host_keys() {
  local node="$1" vmid="$2" pve_node_ssh_host="$3"
  local attempt raw exitcode out_data err_data guest_command guest_cmd remote_cmd key fingerprint
  local -a valid_keys=()

  qga_host_keys=()
  # shellcheck disable=SC2016 # $key must expand in the guest shell, not here.
  guest_command='for key in /etc/ssh/ssh_host_ed25519_key.pub /etc/ssh/ssh_host_ecdsa_key.pub /etc/ssh/ssh_host_rsa_key.pub; do [ -r "$key" ] && cat "$key"; done'
  printf -v guest_cmd '/bin/sh -lc %q' "$guest_command"
  # Request the completed result explicitly: host-key pinning must not accept a
  # launch PID and mistake an absent exitcode/output for a guest key.
  printf -v remote_cmd 'qm guest exec %q --synchronous 1 --timeout 60 -- %s' "$vmid" "$guest_cmd"

  for attempt in $(seq 1 "$retries"); do
    raw=
    exitcode=255
    if raw="$("${ssh_qga[@]}" "root@$pve_node_ssh_host" "$remote_cmd" 2>>"$tmp.ssh-stderr")"; then
      exitcode="$(jq -r '.exitcode // 255' <<<"$raw" 2>/dev/null || printf '255')"
      out_data="$(jq -r '."out-data" // empty' <<<"$raw" 2>/dev/null || true)"
      err_data="$(jq -r '."err-data" // empty' <<<"$raw" 2>/dev/null || true)"
      if [ "$exitcode" = "0" ]; then
        mapfile -t valid_keys < <(
          while IFS= read -r key; do
            valid_guest_host_key_line "$key" || true
          done <<<"$out_data" | LC_ALL=C sort -u
        )
        if [ "${#valid_keys[@]}" -gt 0 ]; then
          qga_host_keys=("${valid_keys[@]}")
          {
            printf 'host_key_attempt=%s\n' "$attempt"
            printf 'host_key_count=%s\n' "${#qga_host_keys[@]}"
            for key in "${qga_host_keys[@]}"; do
              fingerprint="$(printf '%s\n' "$key" | ssh-keygen -lf - | awk '{print $2}')"
              printf 'host_key_type=%s fingerprint=%s\n' "${key%% *}" "$fingerprint"
            done
          } >>"$evidence"
          return 0
        fi
      fi
    else
      err_data="$(tail -n 1 "$tmp.ssh-stderr" 2>/dev/null || true)"
    fi
    {
      printf 'host_key_attempt=%s exitcode=%s\n' "$attempt" "$exitcode"
      [ -z "${err_data:-}" ] || printf 'host_key_error=%s\n' "$err_data"
    } >>"$evidence"
    if [ "$attempt" -lt "$retries" ]; then
      sleep "$retry_sleep"
    fi
  done

  printf 'PVEQGAHostKeyUnavailable: no valid SSH host key from node=%s vmid=%s after %s attempts\n' \
    "$node" "$vmid" "$retries" >&2
  return 1
}

for entry in "${pve_nodes[@]}"; do
  IFS=$'\t' read -r node vmid pve_node_ssh_host <<<"$entry"
  role="$(jq -r --arg node "$node" '.nodes.value[$node].role // empty' "$tmp")"
  expected_management_ip="$(jq -r --arg node "$node" '.nodes.value[$node].management_ip // empty' "$tmp")"
  expected_management_source="$(jq -r --arg node "$node" '.nodes.value[$node].pve_management_source // empty' "$tmp")"
  expected_management_mac="$(jq -r --arg node "$node" '.nodes.value[$node].management_mac // empty' "$tmp")"
  if [ -z "$vmid" ] || [ "$vmid" = "null" ]; then
    echo "missing vm_id for $node" >&2
    exit 1
  fi

  # shellcheck disable=SC2029 # awk runs on the PVE host, not locally.
  if ! agent_config="$("${ssh_qga[@]}" "root@$pve_node_ssh_host" "qm config $vmid | awk -F: '\$1 == \"agent\" { gsub(/[[:space:]]/, \"\", \$2); print \$2; exit }'" 2>"$tmp.ssh-stderr")"; then
    cat "$tmp.ssh-stderr" >>"$evidence"
    printf 'PVEQGATransportUnavailable: cannot query QGA capability on PVE host %s\n' "$pve_node_ssh_host" >&2
    exit 2
  fi
  qga_configured=false
  case "$agent_config" in 1|1,*|yes|yes,*|enabled=1|enabled=1,*|enabled=yes|enabled=yes,*) qga_configured=true ;; esac
  {
    printf 'node=%s\nvmid=%s\npve_ssh_host=%s\nboot_source=%s\ntransport=ssh\nqga_configured=%s\n' "$node" "$vmid" "$pve_node_ssh_host" "$boot_source" "$qga_configured"
  } >>"$evidence"
  if [ "$qga_configured" != true ]; then
    printf 'PVEQGADisabled: QEMU guest agent is disabled for node %s vmid=%s\n' "$node" "$vmid" >&2
    exit 2
  fi

  if [ -n "$expected_management_mac" ]; then
    # Netplan removes stale files under its generated 10-netplan-*.network
    # namespace during the clone's first boot. Keep the reviewed source in
    # /etc/netplan and attest both that source and networkd's active rendering.
    dhcp_source_path=/etc/netplan/99-routerd-lab-dhcp.yaml
    dhcp_rendered_path=/run/systemd/network/10-netplan-eth0.network
    printf -v dhcp_source_command 'qm guest exec %q --synchronous 1 --timeout 30 -- /bin/cat %q' \
      "$vmid" "$dhcp_source_path"
    printf -v dhcp_rendered_command 'qm guest exec %q --synchronous 1 --timeout 30 -- /bin/cat %q' \
      "$vmid" "$dhcp_rendered_path"
    dhcp_source_raw_file="$evidence.dhcp-identity-source.$node.json"
    dhcp_rendered_raw_file="$evidence.dhcp-identity-rendered.$node.json"
    dhcp_source_raw=
    dhcp_rendered_raw=
    dhcp_identity_ready=false
    for dhcp_identity_attempt in $(seq 1 "$retries"); do
      dhcp_attempt_stderr="$evidence.dhcp-identity.$node.attempt-$dhcp_identity_attempt.stderr"
      : >"$tmp.ssh-stderr"
      if dhcp_source_raw="$("${ssh_qga[@]}" "root@$pve_node_ssh_host" "$dhcp_source_command" 2>>"$tmp.ssh-stderr")" && \
         dhcp_rendered_raw="$("${ssh_qga[@]}" "root@$pve_node_ssh_host" "$dhcp_rendered_command" 2>>"$tmp.ssh-stderr")"; then
        cp "$tmp.ssh-stderr" "$dhcp_attempt_stderr"
        dhcp_identity_ready=true
        break
      fi
      printf '%s\n' "$dhcp_source_raw" >"$evidence.dhcp-identity-source.$node.attempt-$dhcp_identity_attempt.json"
      printf '%s\n' "$dhcp_rendered_raw" >"$evidence.dhcp-identity-rendered.$node.attempt-$dhcp_identity_attempt.json"
      cp "$tmp.ssh-stderr" "$dhcp_attempt_stderr"
      if [ "$(LC_ALL=C sort -u "$tmp.ssh-stderr")" != "QEMU guest agent is not running" ]; then
        printf 'PVEQGADHCPIdentityUnavailable: node=%s vmid=%s returned an unknown identity-attestation transport error\n' \
          "$node" "$vmid" >&2
        exit 1
      fi
      printf 'dhcp_identity_attempt=%s state=agent-unavailable\n' "$dhcp_identity_attempt" >>"$evidence"
      if [ "$dhcp_identity_attempt" -lt "$retries" ]; then
        sleep "$retry_sleep"
      fi
    done
    if [ "$dhcp_identity_ready" != true ]; then
      printf 'PVEQGADHCPIdentityUnavailable: node=%s vmid=%s could not read identity settings after %s attempts\n' \
        "$node" "$vmid" "$retries" >&2
      exit 1
    fi
    printf '%s\n' "$dhcp_source_raw" >"$dhcp_source_raw_file"
    printf '%s\n' "$dhcp_rendered_raw" >"$dhcp_rendered_raw_file"
    dhcp_source="$(jq -r '."out-data" // empty' <<<"$dhcp_source_raw" 2>/dev/null)"
    dhcp_rendered="$(jq -r '."out-data" // empty' <<<"$dhcp_rendered_raw" 2>/dev/null)"
    if [ "$(jq -r '.exitcode // 255' <<<"$dhcp_source_raw" 2>/dev/null || printf 255)" != 0 ] || \
       [ "$(jq -r '.exitcode // 255' <<<"$dhcp_rendered_raw" 2>/dev/null || printf 255)" != 0 ] || \
       [ "$dhcp_source" != $'network:\n  version: 2\n  ethernets:\n    eth0:\n      dhcp-identifier: mac\n      dhcp4-overrides:\n        send-hostname: false' ] || \
       [ "$(grep -Fxc 'ClientIdentifier=mac' <<<"$dhcp_rendered")" != 1 ] || \
       [ "$(grep -Exc 'SendHostname=(no|false)' <<<"$dhcp_rendered")" != 1 ]; then
      printf 'PVEQGADHCPIdentityUnavailable: node=%s vmid=%s lacks the reviewed Netplan source or active rendering\n' \
        "$node" "$vmid" >&2
      exit 1
    fi
    printf 'dhcp_identity=stable-mac send_hostname=false source=netplan active_renderer=systemd-networkd\n' >>"$evidence"
  fi

  raw=
  for attempt in $(seq 1 "$retries"); do
    network_state=transport-unavailable
    management_link_count=0
    ips=()
    : >"$tmp.ssh-stderr"
    # shellcheck disable=SC2029 # the redirect is part of the remote QGA readiness check.
    if raw="$("${ssh_qga[@]}" "root@$pve_node_ssh_host" "qm agent $vmid ping >/dev/null && qm agent $vmid network-get-interfaces" 2>>"$tmp.ssh-stderr")"; then
      network_state=invalid-response
      if management_links="$(jq -ce --arg ifname "$management_ifname" '
        (if type == "object" and has("result") then .result else . end)
        | if type == "array" and all(.[]; type == "object" and (.name | type) == "string")
          then map(select(.name == $ifname)) else error("invalid QGA interface response") end
        | if all(.[]; ((if has("ip-addresses") then ."ip-addresses" else [] end) | type == "array" and all(.[];
            type == "object" and (."ip-address" | type) == "string" and
            (."ip-address-type" == "ipv4" or ."ip-address-type" == "ipv6"))))
          then . else error("invalid QGA management address response") end
      ' <<<"$raw" 2>>"$tmp.ssh-stderr")"; then
        management_link_count="$(jq 'length' <<<"$management_links")"
        network_state=invalid-management-interface
        if [ "$management_link_count" -eq 1 ]; then
          mapfile -t ips < <(jq -r '
            .[]."ip-addresses"[]? | select(."ip-address-type" == "ipv4") | ."ip-address"
          ' <<<"$management_links")
          network_state=invalid-management-ipv4
          if [ "${#ips[@]}" -eq 0 ]; then
            # QGA readiness precedes DHCP readiness. Retry only this precise
            # initial state; never hide a wrong NIC or bad/ambiguous address
            # behind a later successful observation.
            network_state=waiting-management-ipv4
          elif [ "${#ips[@]}" -eq 1 ] && valid_unicast_ipv4 "${ips[0]}"; then
            ip="${ips[0]}"
            network_state=ready
          fi
        fi
      fi
    fi
    {
      printf 'network_attempt=%s state=%s ifname=%s management_link_count=%s reported_ipv4_count=%s\n' \
        "$attempt" "$network_state" "$management_ifname" "$management_link_count" "${#ips[@]}"
      [ -z "$raw" ] || printf '%s\n' "$raw"
      [ ! -s "$tmp.ssh-stderr" ] || cat "$tmp.ssh-stderr"
    } >>"$evidence"
    case "$network_state" in
      ready) break ;;
      waiting-management-ipv4|transport-unavailable) ;;
      *)
        echo "QGA must report exactly one usable DHCP IPv4 for $node on $management_ifname ($network_state)" >&2
        exit 1 ;;
    esac
    if [ "$attempt" -eq "$retries" ]; then
      if [ "$network_state" = waiting-management-ipv4 ]; then
        echo "QGA must report exactly one usable DHCP IPv4 for $node on $management_ifname after $retries attempts" >&2
      else
        echo "QGA did not become ready for $node vmid=$vmid after $retries attempts" >&2
      fi
      exit 1
    fi
    printf 'QGA readiness pending node=%s attempt=%s/%s state=%s\n' "$node" "$attempt" "$retries" "$network_state"
    sleep "$retry_sleep"
  done
  if [ "$expected_management_source" = "qga-dhcp" ] && [ "$ip" != "$expected_management_ip" ]; then
    {
      printf 'FAIL node=%s vmid=%s expected_management_ip=%s observed_management_ip=%s\n' "$node" "$vmid" "$expected_management_ip" "$ip"
      echo "$raw"
    } >>"$evidence"
    printf 'PVEQGAManagementAddressMismatch: node=%s recorded %s but QGA now reports %s\n' "$node" "$expected_management_ip" "$ip" >&2
    exit 1
  fi
  observed_management_mac_raw="$(jq -r '.[0]."hardware-address" // empty' <<<"$management_links")"
  observed_management_mac="$(canonical_ethernet_mac "$observed_management_mac_raw")" || {
    printf 'PVEQGAManagementMACUnavailable: node=%s reported invalid management MAC %s\n' \
      "$node" "${observed_management_mac_raw:-<empty>}" >&2
    exit 1
  }
  if [ -n "$expected_management_mac" ] && \
     [ "$(printf '%s' "$expected_management_mac" | tr '[:upper:]' '[:lower:]')" != "$observed_management_mac" ]; then
    printf 'PVEQGAManagementMACMismatch: node=%s expected=%s observed=%s\n' \
      "$node" "$expected_management_mac" "$observed_management_mac" >&2
    exit 1
  fi
  if [ -n "${discovered_management_ips[$ip]:-}" ]; then
    printf 'PVEQGADuplicateManagementAddress: node=%s and node=%s report %s\n' \
      "$node" "${discovered_management_ips[$ip]}" "$ip" >&2
    exit 1
  fi
  discovered_management_ips["$ip"]="$node"

  capture_mac=""
  observed_capture_ifname=""
  if [ "$role" = "leaf" ]; then
    # capture.sourceAddress is a MobilityPool-owned /32.  It must not be
    # seeded into cloud-init merely so this bootstrap step can discover a
    # MAC: doing so introduces a conflicting /24 route before routerd starts.
    mapfile -t capture_links < <(jq -r --arg ifname "$capture_ifname" '
      (if type == "object" and has("result") then .result else . end)[]?
      | select((.name // "") == $ifname)
      | [(.name // ""), (."hardware-address" // "")] | @tsv
    ' <<<"$raw")
    if [ "${#capture_links[@]}" -ne 1 ]; then
      {
        printf 'FAIL node=%s vmid=%s capture_ifname=%s capture_link_count=%s\n' "$node" "$vmid" "$capture_ifname" "${#capture_links[@]}"
        printf 'capture_links=%s\n' "${capture_links[*]:-<empty>}"
        echo "$raw"
      } >>"$evidence"
      printf 'PVEQGACaptureMACUnavailable: QGA must report exactly one capture interface for node %s name %s\n' "$node" "$capture_ifname" >&2
      exit 1
    fi
    IFS=$'\t' read -r observed_capture_ifname capture_mac_raw <<<"${capture_links[0]}"
    if [ -z "$observed_capture_ifname" ] || ! capture_mac="$(canonical_ethernet_mac "$capture_mac_raw")"; then
      {
        printf 'FAIL node=%s vmid=%s capture_ifname=%s capture_mac=%s\n' "$node" "$vmid" "${observed_capture_ifname:-<empty>}" "${capture_mac_raw:-<empty>}"
        echo "$raw"
      } >>"$evidence"
      printf 'PVEQGACaptureMACUnavailable: QGA reported an invalid capture MAC for node %s interface %s\n' "$node" "$capture_ifname" >&2
      exit 1
    fi
    if [ -n "${discovered_capture_macs[$capture_mac]:-}" ]; then
      printf 'PVEQGADuplicateCaptureMAC: node=%s and node=%s report %s\n' \
        "$node" "${discovered_capture_macs[$capture_mac]}" "$capture_mac" >&2
      exit 1
    fi
    discovered_capture_macs["$capture_mac"]="$node"
  fi

  qga_guest_host_keys "$node" "$vmid" "$pve_node_ssh_host" || exit 1
  for key in "${qga_host_keys[@]}"; do
    printf '%s %s\n' "$ip" "$key" >>"$guest_known_hosts_tmp"
  done

  host_keys_json="$(printf '%s\n' "${qga_host_keys[@]}" | jq -R . | jq -s .)"
  jq --arg node "$node" --arg ip "$ip" --arg captureMAC "$capture_mac" --argjson hostKeys "$host_keys_json" '
    .nodes.value[$node].management_ip = $ip
    | .nodes.value[$node].public_ip = $ip
    | .nodes.value[$node].pve_management_source = "qga-dhcp"
    | .nodes.value[$node].ssh_host_keys = $hostKeys
    | .nodes.value[$node].ssh_host_key_source = "qga"
    | if $captureMAC == "" then . else .nodes.value[$node].capture_mac = $captureMAC end
  ' "$tmp" >"$tmp.next"
  mv "$tmp.next" "$tmp"

  {
    echo "PASS node=$node vmid=$vmid ifname=$management_ifname ip=$ip source=qga-dhcp"
    if [ -n "$capture_mac" ]; then
      echo "capture_ifname=$observed_capture_ifname capture_mac=$capture_mac"
    fi
    echo "$raw"
    echo
  } >>"$evidence"
done

install -m 0600 "$tmp" "$out"
install -m 0600 "$guest_known_hosts_tmp" "$guest_known_hosts_out"
echo "wrote patched tofu output: $out"
echo "wrote QGA-pinned guest known_hosts: $guest_known_hosts_out"
echo "wrote QGA evidence: $evidence"
