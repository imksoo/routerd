#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/sam-619-distribution-check.sh --tofu-output tofu-output.json --evidence-dir DIR [options]

Options:
  --ssh-key FILE      Fixed lab SSH key (default: ~/.ssh/routerd-cloudedge-lab-20260529)
  --settle-seconds N  Seconds to wait before reading live state (default: 90)

Collects the focused #619 signal only. It does not deploy, stop routers, run
client matrix, legacy protocols, performance tests, or rebalance actions.

PASS means every cloud leaf reports:
  - phase is not Degraded/Failed
  - ownershipResolverConflictCount is 0
  - providerActionPhase is not Blocked/Failed
  - providerActionFailedCount is 0
  - same-site cloud leaf peers agree on captureDistributionNodeCounts

PVE distribution skew is collected but not a load-balance failure.
USAGE
}

tofu_output=
evidence_dir=
ssh_key="${HOME}/.ssh/routerd-cloudedge-lab-20260529"
settle_seconds=90

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tofu-output) tofu_output="$2"; shift 2 ;;
    --evidence-dir) evidence_dir="$2"; shift 2 ;;
    --ssh-key) ssh_key="$2"; shift 2 ;;
    --settle-seconds) settle_seconds="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$tofu_output" ] || { echo "--tofu-output is required" >&2; exit 2; }
[ -n "$evidence_dir" ] || { echo "--evidence-dir is required" >&2; exit 2; }
[ -f "$tofu_output" ] || { echo "tofu output not found: $tofu_output" >&2; exit 2; }
[ -f "$ssh_key" ] || { echo "ssh key not found: $ssh_key" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }

mkdir -p "$evidence_dir"/{ssh,raw,doctor,status}
cp "$tofu_output" "$evidence_dir/tofu-output.json"
nodes_json="$evidence_dir/nodes.json"
jq '.nodes.value' "$tofu_output" >"$nodes_json"

known_hosts="$evidence_dir/ssh/known_hosts"
: >"$known_hosts"

mapfile -t leaf_routers < <(jq -r 'to_entries[] | select(.value.role == "leaf") | .key' "$nodes_json" | sort)
[ "${#leaf_routers[@]}" -gt 0 ] || { echo "no leaf routers found in tofu output" >&2; exit 2; }

node_field() {
  local node="$1" field="$2"
  jq -r --arg node "$node" --arg field "$field" '.[$node][$field]' "$nodes_json"
}

ssh_base=(-i "$ssh_key" -o UserKnownHostsFile="$known_hosts" -o StrictHostKeyChecking=yes -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=10 -o ServerAliveCountMax=3)

ssh_node() {
  local node="$1"; shift
  local user host
  user="$(node_field "$node" ssh_user)"
  host="$(node_field "$node" public_ip)"
  ssh -n "${ssh_base[@]}" "$user@$host" "$@"
}

{
  date -u '+timestamp=%Y-%m-%dT%H:%M:%SZ'
  echo "tofu_output=$tofu_output"
  echo "ssh_key=$ssh_key"
  echo "settle_seconds=$settle_seconds"
  echo "scope=#619 distribution/doctor only; no deploy, matrix, legacy, performance, failover, or rebalance"
} >"$evidence_dir/run-note.txt"

for node in "${leaf_routers[@]}"; do
  host="$(node_field "$node" public_ip)"
  ssh-keyscan -H "$host" >>"$known_hosts" 2>"$evidence_dir/ssh/${node}.keyscan.err" || true
done

if [ "$settle_seconds" -gt 0 ]; then
  sleep "$settle_seconds"
fi

overall=0
: >"$evidence_dir/summary.tsv"
: >"$evidence_dir/failures.txt"
printf 'node\tsite\tphase\townershipConflictCount\tproviderActionPhase\tproviderFailedCount\tnodeCounts\treasonCounts\n' >"$evidence_dir/summary.tsv"

for node in "${leaf_routers[@]}"; do
  site="$(node_field "$node" site)"
  if ! ssh_node "$node" 'sudo routerctl describe MobilityPool/cloudedge -o json' >"$evidence_dir/raw/${node}.pool.json" 2>"$evidence_dir/raw/${node}.pool.err"; then
    echo "$node: failed to read MobilityPool" >>"$evidence_dir/failures.txt"
    overall=1
    continue
  fi
  ssh_node "$node" 'sudo routerctl doctor sam' >"$evidence_dir/doctor/${node}.txt" 2>&1 || true
  ssh_node "$node" 'sudo routerctl get status -o json' >"$evidence_dir/status/${node}.json" 2>&1 || true

  jq -r --arg node "$node" --arg site "$site" '
    .resource.status as $s |
    [
      $node,
      $site,
      ($s.phase // ""),
      (($s.ownershipResolverConflictCount // 0) | tostring),
      ($s.providerActionPhase // ""),
      (($s.providerActionFailedCount // 0) | tostring),
      (($s.captureDistributionNodeCounts // {}) | tojson),
      (($s.captureDistributionReasonCounts // {}) | tojson)
    ] | @tsv
  ' "$evidence_dir/raw/${node}.pool.json" >>"$evidence_dir/summary.tsv"

  phase="$(jq -r '.resource.status.phase // ""' "$evidence_dir/raw/${node}.pool.json")"
  conflicts="$(jq -r '.resource.status.ownershipResolverConflictCount // 0' "$evidence_dir/raw/${node}.pool.json")"
  provider_phase="$(jq -r '.resource.status.providerActionPhase // ""' "$evidence_dir/raw/${node}.pool.json")"
  provider_failed="$(jq -r '.resource.status.providerActionFailedCount // 0' "$evidence_dir/raw/${node}.pool.json")"

  if [ "$site" != "pve" ]; then
    if [ "$phase" = "Degraded" ] || [ "$phase" = "Failed" ]; then
      echo "$node: phase=$phase" >>"$evidence_dir/failures.txt"
      overall=1
    fi
    if [ "$conflicts" != "0" ]; then
      echo "$node: ownershipResolverConflictCount=$conflicts" >>"$evidence_dir/failures.txt"
      overall=1
    fi
    if [ "$provider_phase" = "Blocked" ] || [ "$provider_phase" = "Failed" ]; then
      echo "$node: providerActionPhase=$provider_phase" >>"$evidence_dir/failures.txt"
      overall=1
    fi
    if [ "$provider_failed" != "0" ]; then
      echo "$node: providerActionFailedCount=$provider_failed" >>"$evidence_dir/failures.txt"
      overall=1
    fi
  fi
done

for site in aws azure oci; do
  mapfile -t site_nodes < <(jq -r --arg site "$site" 'to_entries[] | select(.value.role == "leaf" and .value.site == $site) | .key' "$nodes_json" | sort)
  [ "${#site_nodes[@]}" -gt 1 ] || continue
  first_counts=
  for node in "${site_nodes[@]}"; do
    file="$evidence_dir/raw/${node}.pool.json"
    [ -f "$file" ] || continue
    counts="$(jq -S -c '.resource.status.captureDistributionNodeCounts // {}' "$file")"
    if [ -z "$first_counts" ]; then
      first_counts="$counts"
    elif [ "$counts" != "$first_counts" ]; then
      echo "$site: inconsistent captureDistributionNodeCounts between leaf peers; ${site_nodes[*]}" >>"$evidence_dir/failures.txt"
      overall=1
      break
    fi
  done
done

if [ ! -s "$evidence_dir/failures.txt" ]; then
  echo "PASS" >"$evidence_dir/result.txt"
else
  echo "FAIL" >"$evidence_dir/result.txt"
fi

cat "$evidence_dir/result.txt"
exit "$overall"
