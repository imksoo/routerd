#!/usr/bin/env bash
#
# cloudedge-protocol-probe.sh - FTP/NFS/RPC/bulk/PMTU transparency probe.
#
# Live labs provide PROTOCOL_PROBE_RUNNER to set up services on site VMs and run
# protocol-level checks. This wrapper keeps the acceptance contract stable and
# makes the logic testable offline.
#
set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")

die() { printf '%s: %s\n' "$SELF" "$*" >&2; exit 1; }
log() { printf '%s %s\n' "[$(date -u +%H:%M:%SZ)]" "$*" >&2; }
have() { command -v "$1" >/dev/null 2>&1; }

usage() {
  cat <<EOF
$SELF - CloudEdge protocol transparency probe

USAGE:
  $SELF --out <file> [--pairs aws:azure,aws:onprem] [--bytes <n>]

ENV:
  PROTOCOL_PROBE_RUNNER  Required runner. Contract:
    \$RUNNER setup           <client-site> <server-site>
    \$RUNNER ftp-active      <client-site> <server-site> <bytes>
    \$RUNNER ftp-passive     <client-site> <server-site> <bytes>
    \$RUNNER nfs             <client-site> <server-site> <bytes>
    \$RUNNER rpc             <client-site> <server-site>
    \$RUNNER bulk            <client-site> <server-site> <bytes>
    \$RUNNER pmtu            <client-site> <server-site>
    \$RUNNER source-preserved <client-site> <server-site>
    \$RUNNER no-nat          <client-site> <server-site>

The runner is responsible for package install/service setup on lab VMs
(vsftpd, nfs-kernel-server, rpcbind, iperf3/dd/ssh as appropriate). It may print
key=value lines such as bytes=104857600, overlay_mtu=1380, mss_clamp=1340,
dynamic_port=32768, detail=...; this script records them in the JSON result.
The result shape is documented in scripts/cloudedge-protocol-result-schema.json.
EOF
}

json_map_from_kv() {
  local kv=$1
  python3 - "$kv" <<'PY'
import json, sys
out = {}
for line in sys.argv[1].splitlines():
    if "=" not in line:
        continue
    k, v = line.split("=", 1)
    k = k.strip().replace("-", "_")
    v = v.strip()
    if not k:
        continue
    try:
        if v.isdigit():
            out[k] = int(v)
        else:
            out[k] = float(v)
    except ValueError:
        out[k] = v
print(json.dumps(out, sort_keys=True))
PY
}

run_check() {
  local op=$1 client=$2 server=$3 bytes=${4:-}
  local output result="pass"
  if ! output=$("$PROTOCOL_PROBE_RUNNER" "$op" "$client" "$server" "$bytes" 2>&1); then
    result="fail"
  fi
  printf '%s\t%s\n' "$result" "$(json_map_from_kv "$output")"
}

out=""
pairs="aws:azure,aws:onprem"
bytes=104857600

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out) out="${2:-}"; shift 2 ;;
    --pairs) pairs="${2:-}"; shift 2 ;;
    --bytes) bytes="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

[[ -n "$out" ]] || die "--out is required"
[[ "$bytes" =~ ^[0-9]+$ && "$bytes" -gt 0 ]] || die "bad --bytes"
have python3 || die "python3 is required"
[[ -n "${PROTOCOL_PROBE_RUNNER:-}" && -x "${PROTOCOL_PROBE_RUNNER:-}" ]] \
  || die "PROTOCOL_PROBE_RUNNER must point to an executable runner"

mkdir -p "$(dirname "$out")"

tmp=$(mktemp "${TMPDIR:-/tmp}/cloudedge-protocol.XXXXXX")
trap 'rm -f "$tmp"' EXIT

IFS=',' read -ra pair_list <<<"$pairs"
for pair in "${pair_list[@]}"; do
  [[ -z "$pair" ]] && continue
  client=${pair%%:*}
  server=${pair#*:}
  [[ -n "$client" && -n "$server" && "$client" != "$server" ]] || die "bad pair: $pair"
  log "protocol: $client -> $server"

  setup_result="pass"
  setup_output=""
  if ! setup_output=$("$PROTOCOL_PROBE_RUNNER" setup "$client" "$server" "$bytes" 2>&1); then
    setup_result="fail"
  fi
  setup_detail=$(json_map_from_kv "$setup_output")

  declare -A results=()
  declare -A details=()
  for op in ftp-active ftp-passive nfs rpc bulk pmtu source-preserved no-nat; do
    IFS=$'\t' read -r results[$op] details[$op] < <(run_check "$op" "$client" "$server" "$bytes")
  done

  python3 - "$tmp" "$client" "$server" "$bytes" "$setup_result" "$setup_detail" \
    "${results[ftp-active]}" "${details[ftp-active]}" \
    "${results[ftp-passive]}" "${details[ftp-passive]}" \
    "${results[nfs]}" "${details[nfs]}" \
    "${results[rpc]}" "${details[rpc]}" \
    "${results[bulk]}" "${details[bulk]}" \
    "${results[pmtu]}" "${details[pmtu]}" \
    "${results[source-preserved]}" "${details[source-preserved]}" \
    "${results[no-nat]}" "${details[no-nat]}" <<'PY'
import json, sys
(
    path, client, server, bytes_s, setup_result, setup_detail,
    ftp_active, ftp_active_detail,
    ftp_passive, ftp_passive_detail,
    nfs, nfs_detail,
    rpc, rpc_detail,
    bulk, bulk_detail,
    pmtu, pmtu_detail,
    source_preserved, source_preserved_detail,
    no_nat, no_nat_detail,
) = sys.argv[1:]
try:
    existing = json.load(open(path))
except Exception:
    existing = []
def obj(s):
    try:
        return json.loads(s)
    except Exception:
        return {}
checks = {
    "setup": setup_result,
    "ftpActive": ftp_active,
    "ftpPassive": ftp_passive,
    "nfs": nfs,
    "rpc": rpc,
    "bulkTransfer": bulk,
    "pmtu": pmtu,
    "sourceIpPreserved": source_preserved,
    "noNat": no_nat,
}
details = {
    "setup": obj(setup_detail),
    "ftpActive": obj(ftp_active_detail),
    "ftpPassive": obj(ftp_passive_detail),
    "nfs": obj(nfs_detail),
    "rpc": obj(rpc_detail),
    "bulkTransfer": obj(bulk_detail),
    "pmtu": obj(pmtu_detail),
    "sourceIpPreserved": obj(source_preserved_detail),
    "noNat": obj(no_nat_detail),
}
existing.append({
    "client": client,
    "server": server,
    "bytesRequested": int(bytes_s),
    "checks": checks,
    "details": details,
    "result": "pass" if all(v == "pass" for v in checks.values()) else "fail",
})
with open(path, "w") as f:
    json.dump(existing, f, sort_keys=True)
PY
done

python3 - "$tmp" "$out" <<'PY'
import json, sys
pairs = json.load(open(sys.argv[1]))
total = len(pairs)
passed = sum(1 for p in pairs if p.get("result") == "pass")
checks = {}
for name in ("ftpActive", "ftpPassive", "nfs", "rpc", "bulkTransfer", "pmtu", "sourceIpPreserved", "noNat"):
    values = [p.get("checks", {}).get(name, "fail") for p in pairs]
    checks[name] = "pass" if values and all(v == "pass" for v in values) else "fail"
data = {
    "status": "pass" if total and passed == total else "fail",
    "pairs": pairs,
    "summary": {
        "total": total,
        "passed": passed,
        "failed": total - passed,
        "checks": checks,
    },
}
with open(sys.argv[2], "w") as f:
    json.dump(data, f, indent=2, sort_keys=True)
    f.write("\n")
print(sys.argv[2])
PY

result=$(python3 - "$out" <<'PY'
import json, sys
print(json.load(open(sys.argv[1])).get("status", "fail"))
PY
)
[[ "$result" == "pass" ]]
