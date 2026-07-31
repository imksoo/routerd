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
pve_host="$(jq -er '.pve.node' "$contract_path")"
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
proxy="${HTTPS_PROXY:-${https_proxy:-}}"
if [ -n "$proxy" ]; then
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
    timeout 10 bash -c "exec 3<>/dev/tcp/$host/443"
    timeout 15 openssl s_client -connect "$host:443" -servername "$host" \
      -verify_return_error </dev/null >"$out/tls-${host//[^A-Za-z0-9_.-]/_}.txt" 2>&1
  done
fi
timeout 10 bash -c "exec 3<>/dev/tcp/$pve_host/8006"

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
