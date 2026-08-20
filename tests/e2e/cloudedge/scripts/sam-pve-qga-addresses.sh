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
  --retries N              QGA retry attempts per VM (default: 90).
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

  raw=
  for attempt in $(seq 1 "$retries"); do
    # shellcheck disable=SC2029 # the redirect is part of the remote QGA readiness check.
    if raw="$("${ssh_qga[@]}" "root@$pve_node_ssh_host" "qm agent $vmid ping >/dev/null && qm agent $vmid network-get-interfaces" 2>>"$tmp.ssh-stderr")"; then
      break
    fi
    if [ "$attempt" -eq "$retries" ]; then
      echo "QGA did not become ready for $node vmid=$vmid after $retries attempts" >&2
      exit 1
    fi
    sleep "$retry_sleep"
  done
  mapfile -t ips < <(jq -r --arg ifname "$management_ifname" '
    (if type == "object" and has("result") then .result else . end)[]?
    | select(.name == $ifname)
    | ."ip-addresses"[]?
    | select(."ip-address-type" == "ipv4")
    | ."ip-address"
  ' <<<"$raw")
  valid_ips=()
  for ip in "${ips[@]}"; do
    valid_unicast_ipv4 "$ip" && valid_ips+=("$ip")
  done
  if [ "${#valid_ips[@]}" -ne 1 ]; then
    {
      echo "FAIL node=$node vmid=$vmid ifname=$management_ifname valid_ipv4_count=${#valid_ips[@]}"
      printf 'reported_ipv4=%s\n' "${ips[*]:-<empty>}"
      echo "$raw"
    } >>"$evidence"
    echo "QGA must report exactly one usable DHCP IPv4 for $node on $management_ifname" >&2
    exit 1
  fi
  ip="${valid_ips[0]}"
  if [ "$expected_management_source" = "qga-dhcp" ] && [ "$ip" != "$expected_management_ip" ]; then
    {
      printf 'FAIL node=%s vmid=%s expected_management_ip=%s observed_management_ip=%s\n' "$node" "$vmid" "$expected_management_ip" "$ip"
      echo "$raw"
    } >>"$evidence"
    printf 'PVEQGAManagementAddressMismatch: node=%s recorded %s but QGA now reports %s\n' "$node" "$expected_management_ip" "$ip" >&2
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
