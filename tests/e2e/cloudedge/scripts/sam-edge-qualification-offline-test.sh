#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
work="$(mktemp -d "${TMPDIR:-/tmp}/routerd-sam-edge-qualification.XXXXXX")"
trap 'find "$work" -depth -delete' EXIT
evidence_dir="$work/evidence"
nodes_json="$work/nodes.json"
clients=()
for site in aws azure oci pve; do
  clients+=("$site-client-a" "$site-client-b")
done
jq -n --args '
  reduce range(0; $ARGS.positional | length) as $i
    ({}; .[$ARGS.positional[$i]] = {
      role:"client", site:($ARGS.positional[$i] | split("-")[0]),
      private_ip:("192.0.2." + (($i + 10) | tostring)),
      name:$ARGS.positional[$i], ssh_user:"test"
    })
  + {"aws-leaf-a":{role:"leaf",site:"aws"}, "pve-leaf-a":{role:"leaf",site:"pve"}}
' "${clients[@]}" >"$nodes_json"

node_field() { jq -r --arg node "$1" --arg key "$2" '.[$node][$key]' "$nodes_json"; }
is_cloud_site() { [[ "$1" == aws || "$1" == azure || "$1" == oci ]]; }
# Invoked by the production function loaded with eval below.
# shellcheck disable=SC2317
ssh_node() {
  local destination
  case "$2" in
    ssh\ *)
      destination="$(sed -n "s/.*'test@\([^']*\)' hostname.*/\1/p" <<<"$2")"
      jq -r --arg ip "$destination" 'to_entries[] | select(.value.private_ip == $ip) | .value.name' "$nodes_json"
      ;;
    *) return 0 ;;
  esac
}
# Execute only the actual matrix function with a fake SSH boundary. Never
# source the executable harness or run its preflight/deployment entrypoint.
eval "$(sed -n '/^cloud_ingress_matrix() {/,/^}/p' "$repo_root/tests/e2e/cloudedge/scripts/sam-e2e.sh")"
check_matrix() {
  local label="$1" expected="$2"
  cloud_ingress_matrix "$label"
  local actual
  actual="$(wc -l <"$evidence_dir/matrix/$label/cloud-ingress-summary.tsv")"
  [ "$actual" -eq "$expected" ] || {
    echo "$label expected $expected cloud-ingress flows, got $actual" >&2
    return 1
  }
  awk -F '\t' '$3 != "PASS" {exit 1}' "$evidence_dir/matrix/$label/cloud-ingress-summary.tsv"
}
full_cloud_ingress=0
check_matrix initial 42
check_matrix after-failover-aws-leaf-a 10
# Read by the production function loaded above.
# shellcheck disable=SC2034
full_cloud_ingress=1
check_matrix after-failover-aws-leaf-a 42
check_matrix after-rejoin-aws-leaf-a 42
check_matrix after-failover-pve-leaf-a 42

# Use the production acceptance predicate, but no contract/provider drivers.
eval "$(sed -n '/^verify_profile_result() {/,/^}/p' "$repo_root/tools/release-qa-labs/drivers/qualification-driver.sh")"
# Read by the production predicate loaded above.
# shellcheck disable=SC2034
qualification_profile=representative-redundancy qualification_budget_seconds=5400
profile_result="$work/profile-result.json"
jq -n '{
  profile:"representative-redundancy", result:"pass",
  limits:{maxRuntimeSeconds:5400},
  topology:{routerCount:10,clientCount:8,cloudClientCount:6,rrFaultDomain:"host-redundant"},
  gates:{rrAStaged:true,rrAJoined:true,rrBStaged:true,rrBJoined:true,rrPairReady:true,
    fullBaseline:true,directedClientMatrix:true,directedCloudIngressMatrix:true,providerReadiness:true,
    rrAFailover:true,rrBControlPlaneContinuity:true,rrBContinuityCanary:true,rrARejoin:true,
    edgeAFailover:true,edgeARejoin:true,edgeAClientMatrix:true,edgeACloudIngressMatrix:true,
    legacyProtocols:false,performance:false,symmetricBFailover:false,provisioning:false,destruction:false},
  edgeScenarios:(["aws-leaf-a","azure-leaf-a","oci-leaf-a","pve-leaf-a"] | map({key:.,value:{
    result:"pass",failoverEvidenceExit:0,rejoinEvidenceExit:0,e2eExit:0}}) | from_entries)
}' >"$profile_result"
verify_profile_result
for bad in \
  '.limits.maxRuntimeSeconds=5401' \
  '.limits.maxRuntimeSeconds=1920' \
  'del(.edgeScenarios)' \
  'del(.edgeScenarios["oci-leaf-a"])' \
  '.edgeScenarios["aws-leaf-a"].result="not-run"' \
  '.edgeScenarios["azure-leaf-a"].rejoinEvidenceExit=1' \
  '.edgeScenarios["pve-leaf-b"] = .edgeScenarios["pve-leaf-a"]' \
  '.gates.edgeAClientMatrix=false'; do
  jq "$bad" "$profile_result" >"$work/bad.json"
  if (profile_result="$work/bad.json"; verify_profile_result) >/dev/null 2>&1; then
    echo "profile acceptance admitted incomplete or wrong edge evidence: $bad" >&2
    exit 1
  fi
done

# Reusing a deployed topology must not stop the other nine routers during
# preflight. Exercise the production function with only its I/O mocked.
eval "$(sed -n '/^preflight() {/,/^}/p' "$repo_root/tests/e2e/cloudedge/scripts/sam-e2e.sh")"
tofu_output="$work/preflight-tofu.json"
jq -n '{}' >"$tofu_output"
# Read by the production preflight function loaded above.
# shellcheck disable=SC2034
routers=(aws-leaf-a pve-leaf-a)
pin_pve_guest_host_keys() { :; }
scan_host_key() { :; }
run_preflight_probe() { probe_calls=$((probe_calls + 1)); }
quiesce_existing_routerd_units() { quiesce_calls=$((quiesce_calls + 1)); return "$quiesce_result"; }
# Read by the production preflight function loaded above.
# shellcheck disable=SC2034
skip_deploy=1
quiesce_calls=0 probe_calls=0 quiesce_result=0
preflight
if [ "$quiesce_calls" -ne 0 ] || [ "$probe_calls" -ne 10 ]; then
  echo "skip-deploy preflight stopped the topology or skipped read-only probes" >&2; exit 1
fi
# shellcheck disable=SC2034
skip_deploy=0
preflight
[ "$quiesce_calls" -eq 1 ] || { echo "initial deployment did not quiesce guest services" >&2; exit 1; }
quiesce_result=1 probe_calls=0
if preflight; then
  echo "failed initial quiesce became a PASS" >&2; exit 1
fi
[ "$probe_calls" -eq 0 ] || { echo "failed quiesce continued to probes" >&2; exit 1; }

# Stop acknowledgement is an independent fact from later traffic success.
# Bash disables errexit inside a function called in an OR-list; inject the
# failure in that exact calling shape and never execute a real SSH command.
eval "$(sed -n '/^run_failover() {/,/^}/p' "$repo_root/tests/e2e/cloudedge/scripts/sam-e2e.sh")"
eval "$(sed -n '/^run_rejoin() {/,/^}/p' "$repo_root/tests/e2e/cloudedge/scripts/sam-e2e.sh")"
collect_success_optional_diagnostics() { :; }
collect_success_optional_provider_inventory() { :; }
collect_diagnostics() { :; }
collect_provider_inventory() { :; }
merge_validation_status() { [ "$1" -eq 0 ] && printf '%s\n' "$2" || printf '%s\n' "$1"; }
run_validation_set() { validation_calls=$((validation_calls + 1)); }
mark_node_running() { running_calls=$((running_calls + 1)); }
ssh_node() { ssh_calls=$((ssh_calls + 1)); return "$ssh_result"; }
# Read by the production function loaded above.
# shellcheck disable=SC2034
failover_nodes=(aws-leaf-a azure-leaf-a)
stopped_routers=()
# shellcheck disable=SC2034
failover_transfer_tests=0 rejoin_after_failover=1
validation_calls=0 running_calls=0 ssh_calls=0 ssh_result=1
mkdir -p "$evidence_dir/convergence"
: >"$evidence_dir/convergence/summary.tsv"
stop_rc=0
run_failover || stop_rc=$?
if [ "$stop_rc" -eq 0 ] || [ "$ssh_calls" -ne 1 ] || [ "$validation_calls" -ne 0 ]; then
  echo "unacknowledged stop continued or became a PASS" >&2; exit 1;
fi
[ "${stopped_routers[*]}" = aws-leaf-a ] || { echo "unattempted nodes marked stopped" >&2; exit 1; }
rejoin_rc=0
run_rejoin || rejoin_rc=$?
if [ "$rejoin_rc" -eq 0 ] || [ "$running_calls" -ne 0 ] || [ "$validation_calls" -ne 0 ]; then
  echo "unacknowledged rejoin was published as running or validated" >&2; exit 1;
fi
ssh_result=0 ssh_calls=0
run_rejoin
[ "$ssh_calls" -eq 1 ] && [ "$running_calls" -eq 1 ] && [ "$validation_calls" -eq 1 ]
echo "SAM edge qualification matrix and acceptance offline checks: PASS"
