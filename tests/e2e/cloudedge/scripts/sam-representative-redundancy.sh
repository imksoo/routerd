#!/usr/bin/env bash
set -euo pipefail

# This profile is deliberately narrower than sam-full-validation.sh. It makes
# one full baseline measurement, then proves the representative RR lifecycle
# A -> AB -> B-only -> AB, followed by one complete stop/rejoin of leaf A
# at each site. No B node is stopped; B-side equivalence remains unproven.
readonly profile_name="representative-redundancy"
readonly default_max_runtime_seconds=5400
readonly max_allowed_runtime_seconds=5400
# One surviving leaf owns at most three remote clients. The reviewed AWS
# t3.small ENI has four IPv4 slots: primary + these three secondary addresses.
# Scope this limit to the profile; other generator users keep their own policy.
export SAM_E2E_MAX_SECONDARY_IPS=3

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
  --max-runtime-seconds N    Qualification wall-clock cap, 1..5400 (default: 5400)

Runs the representative, host-redundant PVE-RR qualification profile against
an already provisioned full CloudEdge topology. It installs the supplied
artifact in this order:

  1. all leaf routers;
  2. pve-rr-a;
  3. pve-rr-b;
  4. one complete 12-flow client and 9-flow cloud-ingress baseline;
  5. pve-rr-a stop, while pve-rr-b retains the all-leaf control/provider gate
     and four cross-site hostname canaries; and
  6. pve-rr-a rejoin with the same transition gates; and
  7. aws, azure, oci, then pve leaf A: stop one service, verify all 12 client
     and 9 cloud-ingress flows, rejoin, and verify the same flows before
     proceeding to the next site. All transitions retain control/provider
     and surviving RR membership gates.

No RR-B or leaf-B departure is tested, and configuration equivalence between
A and B is not established by this profile. The entire sequence shares one
5400-second deadline; it does not grant a fresh budget to each leaf. This
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

# The generator's optional CARP mode deliberately uses different A/B
# priorities. This A-only profile does not qualify that separate mode.
if [ "${PVE_OWNERSHIP_GATE:-single-router}" != single-router ]; then
  echo "$profile_name requires PVE_OWNERSHIP_GATE=single-router; other ownership modes are unqualified" >&2
  exit 2
fi

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
jq -e '.clients_per_site == 1' "$fabric_json" >/dev/null || {
  echo "$profile_name requires fabric.clients_per_site=1 with both leaves retained" >&2
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
done

router_count="$(jq '[to_entries[] | select(.value.role == "rr" or .value.role == "leaf")] | length' "$nodes_json")"
client_count="$(jq '[to_entries[] | select(.value.role == "client")] | length' "$nodes_json")"
cloud_client_count="$(jq '[to_entries[] | select(.value.role == "client" and (.value.site == "aws" or .value.site == "azure" or .value.site == "oci"))] | length' "$nodes_json")"
[ "$router_count" -eq 10 ] || { echo "expected exactly 10 router nodes, got $router_count" >&2; exit 2; }
[ "$client_count" -eq 4 ] || { echo "expected exactly 4 client nodes, got $client_count" >&2; exit 2; }
[ "$cloud_client_count" -eq 3 ] || { echo "expected exactly 3 cloud client nodes, got $cloud_client_count" >&2; exit 2; }

qualification_dir="$evidence_root/$profile_name"
mkdir -p "$qualification_dir"
tfvars_args=()
if [ -n "$tfvars" ]; then
  tfvars_args=(--tfvars "$tfvars")
fi

started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

run_e2e() {
  local dir="$1" node="$2" remaining command_rc
  shift 2
  # SECONDS starts at script entry, so input checks and evidence validation
  # also spend this one budget. Never reset it between scenarios. The kill
  # grace only quiesces an expired child; it cannot authorize another fault.
  remaining=$((max_runtime_seconds - SECONDS))
  [ "$remaining" -gt 0 ] || return 124
  mkdir -p "$dir"
  set +e
  timeout --foreground --kill-after=30s "${remaining}s" \
    "$e2e_script" \
      --tofu-output "$tofu_output" \
      --artifact "$artifact" \
      --ssh-key "$ssh_key" \
      --pve-ssh-key "$pve_ssh_key" \
      --pve-known-hosts "$pve_known_hosts" \
      --evidence-dir "$dir" \
      "${tfvars_args[@]}" \
      --staged-rr-pair pve-rr-a pve-rr-b \
      --failover-node "$node" \
      --rejoin-after-failover \
      --skip-legacy-protocols \
      --skip-load-balance-report \
      --success-evidence-minimal \
      "$@" 2>&1 | tee "$dir/sam-e2e.log"
  command_rc=${PIPESTATUS[0]}
  set -e
  [ "$SECONDS" -lt "$max_runtime_seconds" ] || return 124
  return "$command_rc"
}

verify_gate() {
  local convergence="$1" label="$2"
  # Missing, duplicate, or conflicting rows are not a confirmed gate.
  awk -F '\t' -v label="$label" '
    $1 == label { count++; if ($2 != "PASS") failed = 1 }
    END { exit !(count == 1 && !failed) }
  ' "$convergence"
}

verify_matrix() {
  local path="$1" cloud="$2"
  [ -f "$path" ] || return 1
  # Row counts alone can accept duplicates that hide an untested client.
  # Require the exact directed pair set and PASS for every pair.
  jq -Rne --slurpfile nodes "$nodes_json" --argjson cloud "$cloud" '
    [inputs | split("\t")] as $rows
    | ($nodes[0] | to_entries | map(select(.value.role == "client"))) as $clients
    | [$clients[] as $src | $clients[] as $dst
       | select($src.key != $dst.key)
       | select(($cloud | not) or $src.value.site != "pve")
       | [$src.key, $dst.key]] as $expected
    | ($rows | all(length == 3 and .[2] == "PASS"))
      and (($rows | map(.[0:2]) | sort) == ($expected | sort))
  ' "$path" >/dev/null
}

verify_transition_ack() {
  local convergence="$1" label="$2"
  case "$label" in
    after-failover-*) verify_gate "$convergence" "failover-stop-${label#after-failover-}" ;;
    after-rejoin-*) verify_gate "$convergence" "rejoin-start-${label#after-rejoin-}" ;;
    initial) return 0 ;;
    *) return 1 ;;
  esac
}

verify_full_validation() {
  local dir="$1" label="$2" convergence rr
  convergence="$dir/convergence/summary.tsv"
  [ -f "$convergence" ] || return 1
  verify_transition_ack "$convergence" "$label" || return 1
  verify_matrix "$dir/matrix/$label/summary.tsv" false || return 1
  verify_matrix "$dir/matrix/$label/cloud-ingress-summary.tsv" true || return 1
  verify_gate "$convergence" "$label-dataplane" || return 1
  verify_gate "$convergence" "$label-provider" || return 1
  for rr in pve-rr-a pve-rr-b; do
    verify_gate "$convergence" "$label-rr-$rr" || return 1
  done
}

verify_baseline() {
  verify_full_validation "$qualification_dir" initial
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
  verify_transition_ack "$convergence" "$label" || return 1
  [ "$#" -gt 0 ] || return 1
  [ "$(wc -l <"$canary")" -eq 4 ] || return 1
  awk -F '\t' '$3 != "PASS" { exit 1 }' "$canary" || return 1
  verify_gate "$convergence" "$label-dataplane" || return 1
  verify_gate "$convergence" "$label-provider" || return 1
  for rr in "$@"; do
    verify_gate "$convergence" "$label-rr-$rr" || return 1
  done
}

e2e_rc=0
run_e2e "$qualification_dir" pve-rr-a --transition-canary || e2e_rc=$?
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

edge_nodes=(aws-leaf-a azure-leaf-a oci-leaf-a pve-leaf-a)
edge_scenarios='{}'
for node in "${edge_nodes[@]}"; do
  edge_scenarios="$(jq -c --arg node "$node" \
    '. + {($node):{result:"not-run",e2eExit:null,failoverEvidenceExit:null,rejoinEvidenceExit:null}}' <<<"$edge_scenarios")"
done
edge_rc=1
if [ "$e2e_rc" -eq 0 ] && [ "$staging_rc" -eq 0 ] && [ "$baseline_rc" -eq 0 ] && [ "$failover_rc" -eq 0 ] && [ "$rejoin_rc" -eq 0 ]; then
  edge_rc=0
  for node in "${edge_nodes[@]}"; do
    edge_dir="$evidence_root/edge-$node"
    node_e2e_rc=0
    node_failover_rc=1
    node_rejoin_rc=1
    # sam-e2e's multiple --failover-node values accumulate stops before any
    # rejoin. Use exactly one node per invocation, and require complete PASS
    # evidence before invoking the next. Reuse the actual deployed configs.
    run_e2e "$edge_dir" "$node" \
      --skip-deploy --skip-initial-validation --reuse-deployed-topology \
      --configs-dir "$qualification_dir/config-gen/configs" \
      --full-cloud-ingress || node_e2e_rc=$?
    if [ "$node_e2e_rc" -eq 0 ]; then
      verify_full_validation "$edge_dir" "after-failover-$node" && node_failover_rc=0
      verify_full_validation "$edge_dir" "after-rejoin-$node" && node_rejoin_rc=0
    fi
    [ "$SECONDS" -lt "$max_runtime_seconds" ] || node_e2e_rc=124
    node_result=fail
    if [ "$node_e2e_rc" -eq 0 ] && [ "$node_failover_rc" -eq 0 ] && [ "$node_rejoin_rc" -eq 0 ]; then
      node_result=pass
    fi
    edge_scenarios="$(jq -c --arg node "$node" --arg result "$node_result" \
      --argjson e2e "$node_e2e_rc" --argjson failover "$node_failover_rc" --argjson rejoin "$node_rejoin_rc" \
      '.[$node] = {result:$result,e2eExit:$e2e,failoverEvidenceExit:$failover,rejoinEvidenceExit:$rejoin}' <<<"$edge_scenarios")"
    if [ "$node_result" != pass ]; then
      edge_rc=1
      e2e_rc="$node_e2e_rc"
      break
    fi
  done
fi
[ "$SECONDS" -lt "$max_runtime_seconds" ] || e2e_rc=124

elapsed_seconds="$SECONDS"
finished_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
if [ "$e2e_rc" -eq 0 ] && [ "$staging_rc" -eq 0 ] && [ "$baseline_rc" -eq 0 ] && [ "$failover_rc" -eq 0 ] && [ "$rejoin_rc" -eq 0 ] && [ "$edge_rc" -eq 0 ]; then
  result=pass
  status=PASS
  summary="staged PVE RR A/AB/B-only/AB and sequential AWS/Azure/OCI/PVE leaf-A stop/rejoin gates passed"
  rc=0
else
  result=fail
  status=FAIL
  if [ "$e2e_rc" -eq 124 ]; then
    summary="qualification timed out after ${max_runtime_seconds}s"
  elif [ "$e2e_rc" -ne 0 ]; then
    summary="sam-e2e representative transition failed with exit=$e2e_rc"
  else
    summary="sam-e2e returned success but staged, baseline, RR or leaf-A transition evidence was incomplete"
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
  --argjson edgeScenarios "$edge_scenarios" \
  --argjson edgePassed "$([ "$edge_rc" -eq 0 ] && echo true || echo false)" \
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
    edgeScenarios:$edgeScenarios,
    symmetry:{testedSides:["a"],bSideEquivalent:"unproven"},
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
      edgeAFailover:$edgePassed,
      edgeARejoin:$edgePassed,
      edgeAClientMatrix:$edgePassed,
      edgeACloudIngressMatrix:$edgePassed,
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
