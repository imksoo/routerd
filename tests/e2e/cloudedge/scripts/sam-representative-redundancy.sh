#!/usr/bin/env bash
set -euo pipefail

# This profile is deliberately narrower than sam-full-validation.sh. It makes
# one full baseline measurement, then proves the representative RR lifecycle
# A -> AB -> B-only -> AB. It does not repeat the symmetric B departure.
readonly profile_name="representative-redundancy"
readonly default_max_runtime_seconds=1920
readonly max_allowed_runtime_seconds=1920

usage() {
  cat <<'USAGE'
Usage:
  sam-representative-redundancy.sh --tofu-output tofu-output.json --artifact routerd.tar.gz \
    --evidence-root DIR [options]

Options:
  --tfvars FILE              Optional OpenTofu tfvars for provider inventory profiles
  --ssh-key FILE             Guest/cloud SSH key (required)
  --pve-ssh-key FILE         Exact root PVE SSH key for hypervisor bridge audit
  --pve-known-hosts FILE     Pinned known_hosts for the three PVE hypervisors
  --max-runtime-seconds N    Qualification wall-clock cap, 1..1920 (default: 1920)

Runs the representative, host-redundant PVE-RR qualification profile against
an already provisioned full CloudEdge topology. It installs the supplied
artifact in this order:

  1. all leaf routers;
  2. pve-rr-a;
  3. pve-rr-b;
  4. one complete 56-flow client and 42-flow cloud-ingress baseline;
  5. pve-rr-a stop, while pve-rr-b retains the all-leaf control/provider gate
     and four cross-site hostname canaries; and
  6. pve-rr-a rejoin with the same transition gates.

The symmetric pve-rr-b departure/rejoin is intentionally not repeated. This
is a representative redundancy qualification, not the exhaustive engineering
suite. It never provisions or destroys infrastructure; the supervising
lifecycle owns unconditional cleanup.
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

require_node() {
  local node="$1" role="$2" site="$3"
  jq -e --arg node "$node" --arg role "$role" --arg site "$site" \
    '.[$node] | .role == $role and .site == $site' "$nodes_json" >/dev/null || {
    echo "required full-topology node is missing or has the wrong role/site: $node expected=$role/$site" >&2
    return 1
  }
}

topology_scale="$(jq -r '.topology_scale // empty' "$fabric_json")"
[ "$topology_scale" = full ] || {
  echo "$profile_name requires fabric.topology_scale=full; got: ${topology_scale:-<empty>}" >&2
  exit 2
}
jq -e --slurpfile nodes "$nodes_json" '
  .pve as $pve
  | $nodes[0] as $nodes
  | ["pve-rr-a", "pve-rr-b"] as $rrs
  | ($pve.rr_fault_domain == "host-redundant")
  and ((($pve.rr_nodes // []) | sort) == $rrs)
  and ([$rrs[] | $nodes[.].pve_host] as $hosts
       | ($hosts | all(type == "string" and length > 0))
       and (($hosts | unique | length) == 2)
       and (($hosts | sort) == (($pve.rr_hosts // []) | sort)))
  and ([$rrs[] | $nodes[.].pve_ssh_host] as $sshHosts
       | ($sshHosts | all(type == "string" and length > 0))
       and (($sshHosts | unique | length) == 2)
       and (($sshHosts | sort) == (($pve.rr_ssh_hosts // []) | sort)))
  and ($pve.leaf_capture_bridge as $captureBridge
       | ($captureBridge | type == "string" and length > 0)
       and ([$rrs[] | $nodes[.].underlay_bridge] as $underlayBridges
            | ($underlayBridges | all(type == "string" and length > 0))
            and ((reduce $rrs[] as $node ({}; .[$node] = $nodes[$node].underlay_bridge))
                 == ($pve.rr_underlay_bridges // {}))
            and ($underlayBridges | all(. != $captureBridge))))
' "$fabric_json" >/dev/null || {
  echo "$profile_name requires two PVE RRs whose node host, SSH host, and underlay-only NIC values exactly match the host-redundant fabric" >&2
  exit 2
}

require_node pve-rr-a rr pve
require_node pve-rr-b rr pve
for site in aws azure oci pve; do
  require_node "$site-leaf-a" leaf "$site"
  require_node "$site-leaf-b" leaf "$site"
  require_node "$site-client-a" client "$site"
  require_node "$site-client-b" client "$site"
done

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
    --staged-rr-pair pve-rr-a pve-rr-b \
    --failover-node pve-rr-a \
    --rejoin-after-failover \
    --transition-canary \
    --skip-legacy-protocols \
    --skip-load-balance-report \
    --success-evidence-minimal \
  2>&1 | tee "$log"
e2e_rc=${PIPESTATUS[0]}
set -e

verify_baseline() {
  local matrix cloud_ingress convergence expected_matrix expected_cloud actual_matrix actual_cloud
  matrix="$qualification_dir/matrix/initial/summary.tsv"
  cloud_ingress="$qualification_dir/matrix/initial/cloud-ingress-summary.tsv"
  convergence="$qualification_dir/convergence/summary.tsv"
  expected_matrix=$((client_count * (client_count - 1)))
  expected_cloud=$((cloud_client_count * (client_count - 1)))
  [ -f "$matrix" ] && [ -f "$cloud_ingress" ] && [ -f "$convergence" ] || return 1
  actual_matrix="$(wc -l <"$matrix")"
  actual_cloud="$(wc -l <"$cloud_ingress")"
  [ "$actual_matrix" -eq "$expected_matrix" ] || return 1
  [ "$actual_cloud" -eq "$expected_cloud" ] || return 1
  awk -F '\t' '$3 != "PASS" { exit 1 }' "$matrix"
  awk -F '\t' '$3 != "PASS" { exit 1 }' "$cloud_ingress"
  awk -F '\t' '$1 == "initial-dataplane" && $2 == "PASS" { found = 1 } END { exit !found }' "$convergence"
  awk -F '\t' '$1 == "initial-provider" && $2 == "PASS" { found = 1 } END { exit !found }' "$convergence"
  for rr in pve-rr-a pve-rr-b; do
    awk -F '\t' -v rr="$rr" '$1 == "initial-rr-" rr && $2 == "PASS" { found = 1 } END { exit !found }' "$convergence"
  done
}

verify_staged_rr_pair() {
  local stages="$qualification_dir/deploy/rr-stage.tsv"
  [ -f "$stages" ] || return 1
  awk -F '\t' '
    $1 == "rr-a-started" && $2 == "pve-rr-a" && $3 == "PASS" { a = 1; a_line = NR }
    $1 == "rr-a-joined" && $2 == "pve-rr-a" && $3 == "PASS" { a_joined = 1; a_joined_line = NR }
    $1 == "rr-b-started" && $2 == "pve-rr-b" && $3 == "PASS" { b = 1; b_line = NR }
    $1 == "rr-b-joined" && $2 == "pve-rr-b" && $3 == "PASS" { b_joined = 1; b_joined_line = NR }
    $1 == "rr-pair-ready" && $2 == "pve-rr-a,pve-rr-b" && $3 == "PASS" { pair = 1; pair_line = NR }
    END { exit !(a && a_joined && b && b_joined && pair && a_line < a_joined_line && a_joined_line < b_line && b_line < b_joined_line && b_joined_line < pair_line) }
  ' "$stages"
}

verify_transition() {
  local label canary convergence rr
  label="$1"
  shift
  canary="$qualification_dir/matrix/$label/transition-canary-summary.tsv"
  convergence="$qualification_dir/convergence/summary.tsv"
  [ -f "$canary" ] && [ -f "$convergence" ] || return 1
  [ "$#" -gt 0 ] || return 1
  [ "$(wc -l <"$canary")" -eq 4 ] || return 1
  awk -F '\t' '$3 != "PASS" { exit 1 }' "$canary"
  awk -F '\t' -v label="$label" '$1 == label "-dataplane" && $2 == "PASS" { found = 1 } END { exit !found }' "$convergence"
  awk -F '\t' -v label="$label" '$1 == label "-provider" && $2 == "PASS" { found = 1 } END { exit !found }' "$convergence"
  for rr in "$@"; do
    awk -F '\t' -v label="$label" -v rr="$rr" '$1 == label "-rr-" rr && $2 == "PASS" { found = 1 } END { exit !found }' "$convergence"
  done
}

baseline_rc=0
staging_rc=0
failover_rc=0
rejoin_rc=0
if [ "$e2e_rc" -eq 0 ]; then
  verify_staged_rr_pair || staging_rc=1
  verify_baseline || baseline_rc=1
  verify_transition after-failover-pve-rr-a pve-rr-b || failover_rc=1
  verify_transition after-rejoin-pve-rr-a pve-rr-a pve-rr-b || rejoin_rc=1
fi

elapsed_seconds="$SECONDS"
finished_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
if [ "$e2e_rc" -eq 0 ] && [ "$staging_rc" -eq 0 ] && [ "$baseline_rc" -eq 0 ] && [ "$failover_rc" -eq 0 ] && [ "$rejoin_rc" -eq 0 ]; then
  result=pass
  status=PASS
  summary="staged PVE RR A/AB/B-only/AB representative redundancy gates passed"
  rc=0
else
  result=fail
  status=FAIL
  if [ "$e2e_rc" -eq 124 ]; then
    summary="qualification timed out after ${max_runtime_seconds}s"
  elif [ "$e2e_rc" -ne 0 ]; then
    summary="sam-e2e representative transition failed with exit=$e2e_rc"
  else
    summary="sam-e2e returned success but staged, baseline, failover, or rejoin evidence was incomplete"
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
  --argjson stagingEvidenceExit "$staging_rc" \
  --argjson baselineEvidenceExit "$baseline_rc" \
  --argjson failoverEvidenceExit "$failover_rc" \
  --argjson rejoinEvidenceExit "$rejoin_rc" \
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
    topology:{routerCount:$routerCount,clientCount:$clientCount,cloudClientCount:$cloudClientCount,rrFaultDomain:"host-redundant"},
    gates:{
      rrAStaged:true,
      rrAJoined:true,
      rrBStaged:true,
      rrBJoined:true,
      rrPairReady:true,
      fullBaseline:true,
      directedClientMatrix:true,
      directedCloudIngressMatrix:true,
      providerReadiness:true,
      rrAFailover:true,
      rrBControlPlaneContinuity:true,
      rrBContinuityCanary:true,
      rrARejoin:true,
      legacyProtocols:false,
      performance:false,
      symmetricBFailover:false,
      provisioning:false,
      destruction:false
    },
    outcomes:{
      samE2EExit:$samE2EExit,
      stagingEvidenceExit:$stagingEvidenceExit,
      baselineEvidenceExit:$baselineEvidenceExit,
      failoverEvidenceExit:$failoverEvidenceExit,
      rejoinEvidenceExit:$rejoinEvidenceExit
    }
  }' >"$evidence_root/profile-result.json"

echo "$profile_name qualification: $status ($summary)"
echo "evidence: $evidence_root"
exit "$rc"
