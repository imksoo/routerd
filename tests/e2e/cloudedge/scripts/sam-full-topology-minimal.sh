#!/usr/bin/env bash
set -euo pipefail

# The release qualification profile deliberately has one baseline only.  It is
# not a shorthand for sam-full-validation.sh: that script remains the long
# engineering/failover suite, while this entry point has a bounded paid-run
# contract and exactly one functional gate set.
readonly profile_name="full-topology-minimal"
readonly default_max_runtime_seconds=1200
readonly max_allowed_runtime_seconds=1200

usage() {
  cat <<'USAGE'
Usage:
  sam-full-topology-minimal.sh --tofu-output tofu-output.json --artifact routerd.tar.gz \
    --evidence-root DIR [options]

Options:
  --tfvars FILE              Optional OpenTofu tfvars for provider inventory profiles
  --ssh-key FILE             Guest/cloud SSH key (required)
  --pve-ssh-key FILE         Exact root PVE SSH key for hypervisor bridge audit
  --pve-known-hosts FILE     Pinned known_hosts for the PVE hypervisors
  --max-runtime-seconds N    Qualification wall-clock cap, 1..1200 (default: 1200)

Runs the explicit full-topology-minimal qualification profile against an
already provisioned full CloudEdge topology. It installs the supplied artifact
on all ten router nodes and runs exactly one baseline:

  * control-plane/dataplane readiness;
  * every directed client-to-client hostname flow;
  * every directed cloud-origin cloud-ingress hostname flow; and
  * the MobilityPool provider readiness gate.

It deliberately does not run legacy protocol probes, throughput/performance
probes, load-balance reporting, transfer probes, failover, rejoin, provisioning,
or destruction. The supervising release-QA lifecycle owns provisioning and
unconditional cleanup. A timeout is a failure and leaves partial evidence for
that lifecycle to collect before cleanup.
USAGE
}

tofu_output=
artifact=
evidence_root=
tfvars=
ssh_key=
pve_ssh_key=
pve_known_hosts=
max_runtime_seconds="$default_max_runtime_seconds"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tofu-output) tofu_output="${2:?missing --tofu-output value}"; shift 2 ;;
    --artifact) artifact="${2:?missing --artifact value}"; shift 2 ;;
    --evidence-root) evidence_root="${2:?missing --evidence-root value}"; shift 2 ;;
    --tfvars) tfvars="${2:?missing --tfvars value}"; shift 2 ;;
    --ssh-key) ssh_key="${2:?missing --ssh-key value}"; shift 2 ;;
    --pve-ssh-key) pve_ssh_key="${2:?missing --pve-ssh-key value}"; shift 2 ;;
    --pve-known-hosts) pve_known_hosts="${2:?missing --pve-known-hosts value}"; shift 2 ;;
    --max-runtime-seconds) max_runtime_seconds="${2:?missing --max-runtime-seconds value}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$tofu_output" ] || { echo "--tofu-output is required" >&2; exit 2; }
[ -n "$artifact" ] || { echo "--artifact is required" >&2; exit 2; }
[ -n "$evidence_root" ] || { echo "--evidence-root is required" >&2; exit 2; }
[ -f "$tofu_output" ] || { echo "tofu output not found: $tofu_output" >&2; exit 2; }
[ -f "$artifact" ] || { echo "artifact not found: $artifact" >&2; exit 2; }
[ -z "$tfvars" ] || [ -f "$tfvars" ] || { echo "tfvars not found: $tfvars" >&2; exit 2; }
[ -n "$ssh_key" ] || { echo "--ssh-key FILE is required" >&2; exit 2; }
[ -f "$ssh_key" ] || { echo "ssh key not found: $ssh_key" >&2; exit 2; }
[ -f "$pve_ssh_key" ] || { echo "--pve-ssh-key FILE is required" >&2; exit 2; }
[ -f "$pve_known_hosts" ] || { echo "--pve-known-hosts FILE is required" >&2; exit 2; }
case "$max_runtime_seconds" in
  ''|*[!0-9]*) echo "--max-runtime-seconds must be an integer" >&2; exit 2 ;;
esac
if [ "$max_runtime_seconds" -le 0 ] || [ "$max_runtime_seconds" -gt "$max_allowed_runtime_seconds" ]; then
  echo "--max-runtime-seconds must be in 1..$max_allowed_runtime_seconds" >&2
  exit 2
fi
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }
command -v timeout >/dev/null || { echo "timeout is required" >&2; exit 2; }

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
e2e_script="$script_dir/sam-e2e.sh"
[ -x "$e2e_script" ] || { echo "sam-e2e entrypoint not found or not executable: $e2e_script" >&2; exit 2; }

mkdir -p "$evidence_root"
nodes_json="$evidence_root/nodes.json"
fabric_json="$evidence_root/fabric.json"
jq '.nodes.value' "$tofu_output" >"$nodes_json"
jq '.fabric.value' "$tofu_output" >"$fabric_json"

topology_scale="$(jq -r '.topology_scale // empty' "$fabric_json")"
if [ "$topology_scale" != "full" ]; then
  echo "$profile_name requires fabric.topology_scale=full; got: ${topology_scale:-<empty>}" >&2
  exit 2
fi

require_node() {
  local node="$1" role="$2" site="$3"
  jq -e --arg node "$node" --arg role "$role" --arg site "$site" \
    '.[$node] | .role == $role and .site == $site' "$nodes_json" >/dev/null || {
    echo "required full-topology node is missing or has the wrong role/site: $node expected=$role/$site" >&2
    return 1
  }
}

# The release topology keeps its redundant RR pair on PVE and has two
# client-bearing leaves at each of the four sites. Checking all eighteen nodes
# prevents a reduced fixture from silently passing a smaller directed matrix
# while claiming full topology.
require_node pve-rr-a rr pve
require_node pve-rr-b rr pve
require_node aws-leaf-a leaf aws
require_node aws-leaf-b leaf aws
require_node azure-leaf-a leaf azure
require_node azure-leaf-b leaf azure
require_node oci-leaf-a leaf oci
require_node oci-leaf-b leaf oci
require_node pve-leaf-a leaf pve
require_node pve-leaf-b leaf pve
require_node aws-client-a client aws
require_node aws-client-b client aws
require_node azure-client-a client azure
require_node azure-client-b client azure
require_node oci-client-a client oci
require_node oci-client-b client oci
require_node pve-client-a client pve
require_node pve-client-b client pve

router_count="$(jq '[to_entries[] | select(.value.role == "rr" or .value.role == "leaf")] | length' "$nodes_json")"
client_count="$(jq '[to_entries[] | select(.value.role == "client")] | length' "$nodes_json")"
cloud_client_count="$(jq '[to_entries[] | select(.value.role == "client" and (.value.site == "aws" or .value.site == "azure" or .value.site == "oci"))] | length' "$nodes_json")"
[ "$router_count" -eq 10 ] || { echo "expected exactly 10 router nodes, got $router_count" >&2; exit 2; }
[ "$client_count" -eq 8 ] || { echo "expected exactly 8 client nodes, got $client_count" >&2; exit 2; }
[ "$cloud_client_count" -eq 6 ] || { echo "expected exactly 6 cloud client nodes, got $cloud_client_count" >&2; exit 2; }

qualification_dir="$evidence_root/$profile_name"
mkdir -p "$qualification_dir"
log="$qualification_dir/sam-e2e.log"
tfvars_args=()
if [ -n "$tfvars" ]; then
  tfvars_args=(--tfvars "$tfvars")
fi

started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
set +e
timeout --foreground --kill-after=30s "${max_runtime_seconds}s" \
  "$e2e_script" \
    --tofu-output "$tofu_output" \
    --artifact "$artifact" \
    --ssh-key "$ssh_key" \
    --pve-ssh-key "$pve_ssh_key" \
    --pve-known-hosts "$pve_known_hosts" \
    --evidence-dir "$qualification_dir" \
    "${tfvars_args[@]}" \
    --skip-legacy-protocols \
    --skip-load-balance-report \
    --success-evidence-minimal \
  2>&1 | tee "$log"
e2e_rc=${PIPESTATUS[0]}
set -e

verify_full_gate_evidence() {
  local matrix cloud_ingress convergence expected_matrix expected_cloud actual_matrix actual_cloud
  matrix="$qualification_dir/matrix/initial/summary.tsv"
  cloud_ingress="$qualification_dir/matrix/initial/cloud-ingress-summary.tsv"
  convergence="$qualification_dir/convergence/summary.tsv"
  expected_matrix=$((client_count * (client_count - 1)))
  expected_cloud=$((cloud_client_count * (client_count - 1)))

  if [ ! -f "$matrix" ] || [ ! -f "$cloud_ingress" ] || [ ! -f "$convergence" ]; then
    echo "minimal profile is missing required gate evidence" >&2
    return 1
  fi
  actual_matrix="$(wc -l <"$matrix")"
  actual_cloud="$(wc -l <"$cloud_ingress")"
  [ "$actual_matrix" -eq "$expected_matrix" ] || {
    echo "directed client matrix was incomplete: got $actual_matrix expected $expected_matrix" >&2
    return 1
  }
  [ "$actual_cloud" -eq "$expected_cloud" ] || {
    echo "directed cloud-ingress matrix was incomplete: got $actual_cloud expected $expected_cloud" >&2
    return 1
  }
  awk -F '\t' '$3 != "PASS" { exit 1 }' "$matrix" || {
    echo "directed client matrix contains a non-PASS result" >&2
    return 1
  }
  awk -F '\t' '$3 != "PASS" { exit 1 }' "$cloud_ingress" || {
    echo "directed cloud-ingress matrix contains a non-PASS result" >&2
    return 1
  }
  awk -F '\t' '$1 == "initial-dataplane" && $2 == "PASS" { found = 1 } END { exit !found }' "$convergence" || {
    echo "control/dataplane gate did not pass" >&2
    return 1
  }
  awk -F '\t' '$1 == "initial-provider" && $2 == "PASS" { found = 1 } END { exit !found }' "$convergence" || {
    echo "provider gate did not pass" >&2
    return 1
  }
}

gate_rc=0
if [ "$e2e_rc" -eq 0 ]; then
  verify_full_gate_evidence || gate_rc=1
fi

elapsed_seconds="$SECONDS"
finished_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
if [ "$e2e_rc" -eq 0 ] && [ "$gate_rc" -eq 0 ]; then
  result=pass
  status=PASS
  summary="all non-legacy/non-performance full-topology baseline gates passed"
  rc=0
else
  result=fail
  status=FAIL
  if [ "$e2e_rc" -eq 124 ]; then
    summary="qualification timed out after ${max_runtime_seconds}s"
  elif [ "$e2e_rc" -ne 0 ]; then
    summary="sam-e2e baseline failed with exit=$e2e_rc"
  else
    summary="sam-e2e returned success but full gate evidence was incomplete or non-PASS"
  fi
  rc=1
fi

printf 'profile\tstatus\tevidence_dir\n%s\t%s\t%s\n' \
  "$profile_name" "$status" "$qualification_dir" >"$evidence_root/profile-status.tsv"
jq -n \
  --arg profile "$profile_name" \
  --arg result "$result" \
  --arg summary "$summary" \
  --arg startedAt "$started_at" \
  --arg finishedAt "$finished_at" \
  --arg tofuOutput "$tofu_output" \
  --arg artifact "$artifact" \
  --arg tfvars "$tfvars" \
  --arg sshKey "$ssh_key" \
  --argjson maxRuntimeSeconds "$max_runtime_seconds" \
  --argjson elapsedSeconds "$elapsed_seconds" \
  --argjson samE2EExit "$e2e_rc" \
  --argjson gateEvidenceExit "$gate_rc" \
  --argjson routerCount "$router_count" \
  --argjson clientCount "$client_count" \
  --argjson cloudClientCount "$cloud_client_count" \
  '{
    profile:$profile,
    result:$result,
    summary:$summary,
    startedAt:$startedAt,
    finishedAt:$finishedAt,
    limits:{maxRuntimeSeconds:$maxRuntimeSeconds},
    elapsedSeconds:$elapsedSeconds,
    inputs:{tofuOutput:$tofuOutput, artifact:$artifact, tfvars:$tfvars, sshKey:$sshKey},
    topology:{routerCount:$routerCount, clientCount:$clientCount, cloudClientCount:$cloudClientCount},
    gates:{
      controlDataplane:true,
      directedClientMatrix:true,
      directedCloudIngressMatrix:true,
      providerReadiness:true,
      legacyProtocols:false,
      performance:false,
      failover:false,
      rejoin:false,
      provisioning:false,
      destruction:false
    },
    outcomes:{samE2EExit:$samE2EExit, gateEvidenceExit:$gateEvidenceExit}
  }' >"$evidence_root/profile-result.json"

echo "$profile_name qualification: $status ($summary)"
echo "evidence: $evidence_root"
exit "$rc"
