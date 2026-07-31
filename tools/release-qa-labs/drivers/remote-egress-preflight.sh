#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=common.sh
source "$script_dir/common.sh"

if [ "$#" -ne 2 ] || [ "$1" != --contract ]; then
  die "Usage: $(basename "$0") --contract FILE"
fi
load_contract "$2"
out="$evidence_root/preflight/remote-egress"
mkdir -p "$out"
chmod 700 "$out"
rm -f "$out/result.json"
auth_complete=0
cleanup_partial_auth() {
  if [ "$auth_complete" -ne 1 ]; then
    rm -f "$out/aws-auth.json" "$out/azure-auth.json" \
      "$out/oci-auth.json" "$out/pve-auth.txt"
  fi
}
trap cleanup_partial_auth EXIT INT TERM

expected="$(jq -er '.execution.host' "$contract_path")"
actual="$(hostname -f 2>/dev/null || hostname)"
case "$actual" in
  "$expected"|"${expected%%.*}"|"${expected%%.*}."*) ;;
  *) die "egress preflight is running on $actual, expected $expected" ;;
esac

aws_region="$(extract_tfvars_string "$tfvars_path" aws_region)"
oci_region="$(extract_tfvars_string "$tfvars_path" oci_region)"
pve_host="$pve_ssh_host"
hosts=(
  "sts.${aws_region}.amazonaws.com"
  "management.azure.com"
  "identity.${oci_region}.oci.oraclecloud.com"
  "github.com"
  "$pve_host"
)
for host in "${hosts[@]}"; do
  getent ahosts "$host" >"$out/dns-${host//[^A-Za-z0-9_.-]/_}.txt"
done

resolved_addresses() {
  local family="$1" host="$2"
  if [ "$family" = 6 ]; then
    getent ahostsv6 "$host" 2>/dev/null || true
  else
    # Some glibc/NSS configurations return IPv4-mapped IPv6 literals from
    # ahostsv6. Feed both sources to one semantic classifier so mapped forms
    # become canonical dotted IPv4 and deduplicate against the native A result.
    getent ahostsv6 "$host" 2>/dev/null || true
    getent ahostsv4 "$host" 2>/dev/null || true
  fi | python3 -c '
import ipaddress
import sys

family = int(sys.argv[1])
seen = set()
for line in sys.stdin:
    fields = line.split()
    if len(fields) < 2 or fields[1] != "STREAM":
        continue
    try:
        address = ipaddress.ip_address(fields[0])
    except ValueError:
        continue
    if family == 6:
        if address.version != 6 or address.ipv4_mapped is not None:
            continue
        candidate = address.compressed
    else:
        if address.version == 6 and address.ipv4_mapped is not None:
            candidate = str(address.ipv4_mapped)
        elif address.version == 4:
            candidate = str(address)
        else:
            continue
    if candidate not in seen:
        seen.add(candidate)
        print(candidate)
' "$family"
}

tcp_literal() {
  local address="$1" port="$2"
  # Expansion is intentionally deferred to the inner Bash positional args.
  # shellcheck disable=SC2016
  timeout 10 bash -c 'exec 3<>"/dev/tcp/$1/$2"' bash "$address" "$port"
}

direct_tcp_tls() {
  local host="$1" port="$2" result="$3"
  local family address connect selected=
  : >"$result.attempts"
  for family in 6 4; do
    while IFS= read -r address; do
      [ -n "$address" ] || continue
      printf 'family=ipv%s address=%s tcp=' "$family" "$address" >>"$result.attempts"
      if ! tcp_literal "$address" "$port"; then
        printf 'fail tls=not-run\n' >>"$result.attempts"
        continue
      fi
      printf 'pass tls=' >>"$result.attempts"
      if [ "$family" = 6 ]; then
        connect="[$address]:$port"
      else
        connect="$address:$port"
      fi
      if timeout 15 openssl s_client "-$family" -connect "$connect" \
          -servername "$host" -verify_hostname "$host" -verify_return_error \
          </dev/null >"$result" 2>&1; then
        printf 'pass\n' >>"$result.attempts"
        selected="family=ipv$family address=$address host=$host port=$port"
        printf '%s\n' "$selected" >"$result.selected"
        return 0
      fi
      printf 'fail\n' >>"$result.attempts"
    done < <(resolved_addresses "$family" "$host")
  done
  echo "release lab driver: no TCP+TLS-capable address for $host:$port" >&2
  return 2
}

direct_tcp_any_family() {
  local host="$1" port="$2" result="$3"
  local family address
  : >"$result.attempts"
  for family in 6 4; do
    while IFS= read -r address; do
      [ -n "$address" ] || continue
      if tcp_literal "$address" "$port"; then
        printf 'family=ipv%s address=%s tcp=pass\n' "$family" "$address" >>"$result.attempts"
        printf 'family=ipv%s address=%s host=%s port=%s\n' \
          "$family" "$address" "$host" "$port" >"$result.selected"
        return 0
      fi
      printf 'family=ipv%s address=%s tcp=fail\n' "$family" "$address" >>"$result.attempts"
    done < <(resolved_addresses "$family" "$host")
  done
  echo "release lab driver: no TCP-capable address for $host:$port" >&2
  return 2
}

proxy="${HTTPS_PROXY:-${https_proxy:-}}"
if [ -n "$proxy" ]; then
  # In proxy mode the explicit TCP gate is for the proxy endpoint. curl then
  # performs the origin TLS exchange through that proxy; this path does not
  # claim that the proxy and origin use the same address family.
  proxy_authority="${proxy#*://}"
  proxy_authority="${proxy_authority%%/*}"
  proxy_host="${proxy_authority%%:*}"
  proxy_port="${proxy_authority##*:}"
  [ "$proxy_port" != "$proxy_authority" ] || proxy_port=443
  getent ahosts "$proxy_host" >"$out/dns-proxy.txt"
  timeout 10 bash -c "exec 3<>/dev/tcp/$proxy_host/$proxy_port"
  for host in "${hosts[@]:0:4}"; do
    curl --silent --show-error --head --connect-timeout 10 \
      --proxy "$proxy" "https://$host/" >"$out/proxy-connect-tls-${host//[^A-Za-z0-9_.-]/_}.txt"
  done
else
  for host in "${hosts[@]:0:4}"; do
    direct_tcp_tls "$host" 443 "$out/tls-${host//[^A-Za-z0-9_.-]/_}.txt"
  done
fi
# PVE's API endpoint is only a TCP reachability gate here. Its authenticated
# API/SSH checks below are separate and do not claim a shared selected family.
direct_tcp_any_family "$pve_host" 8006 "$out/tcp-${pve_host//[^A-Za-z0-9_.-]/_}-8006"

# Authenticated reads are deliberately redirected to private evidence files.
aws --profile "$(extract_tfvars_string "$tfvars_path" aws_profile)" \
  --region "$aws_region" sts get-caller-identity >"$out/aws-auth.json"
az account show --output json >"$out/azure-auth.json"
oci --profile "$(extract_tfvars_string "$tfvars_path" oci_profile)" \
  --region "$oci_region" iam region-subscription list >"$out/oci-auth.json"
ssh -i "$pve_ssh_private_key" -o BatchMode=yes -o ConnectTimeout=10 "root@$pve_host" pveversion \
  >"$out/pve-auth.txt"

mirror="$(jq -er '.execution.providerMirror' "$contract_path")"
[ -d "$mirror" ] || die "provider mirror is missing"
while read -r provider version; do
  path="$mirror/registry.opentofu.org/$provider/$version/linux_amd64"
  [ -d "$path" ] || die "provider mirror entry missing: $provider $version"
done < <(jq -r '.execution.providerVersions | to_entries[] | [.key,.value] | @tsv' "$contract_path")

chmod 600 "$out"/*
result_tmp="$(mktemp "$out/.result.XXXXXX")"
jq -n --arg host "$actual" --arg runId "$run_id" \
  --arg contractSha256 "$(sha256sum "$contract_path" | awk '{print $1}')" \
  --arg checkedAt "$(utc_now)" \
  '{status:"pass",runId:$runId,executionHost:$host,contractSha256:$contractSha256,checkedAt:$checkedAt}' \
  >"$result_tmp"
install -m 0600 "$result_tmp" "$out/result.json"
rm -f "$result_tmp"
auth_complete=1
trap - EXIT INT TERM
echo "remote egress preflight: pass on $actual"
