#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
profile_script="$repo_root/tests/e2e/cloudedge/scripts/sam-representative-redundancy.sh"
harness_script="$repo_root/tests/e2e/cloudedge/scripts/sam-e2e.sh"

bash -n "$profile_script"
bash -n "$harness_script"
"$profile_script" --help >/dev/null

work="$(mktemp -d "${TMPDIR:-/tmp}/routerd-sam-representative-redundancy.XXXXXX")"
cleanup() {
  find "$work" -depth -delete
}
trap cleanup EXIT

scripts="$work/scripts"
mkdir -p "$scripts"
cp "$profile_script" "$scripts/sam-representative-redundancy.sh"
cat >"$scripts/sam-e2e.sh" <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail

evidence_dir=
tofu_output=
failover_node=
failover_count=0
args=("$@")
while [ "$#" -gt 0 ]; do
  case "$1" in
    --evidence-dir) evidence_dir="$2"; shift 2 ;;
    --tofu-output) tofu_output="$2"; shift 2 ;;
    --failover-node) failover_node="$2"; failover_count=$((failover_count + 1)); shift 2 ;;
    *) shift ;;
  esac
done
[ -n "$evidence_dir" ]
[ "$failover_count" -eq 1 ] || { echo 'cumulative failover is forbidden' >&2; exit 97; }
case "$failover_node" in pve-rr-a|aws-leaf-a|azure-leaf-a|oci-leaf-a|pve-leaf-a) ;; *) exit 98 ;; esac
printf '%s\n' "${args[@]}" >"${SAM_REPRESENTATIVE_FAKE_INVOCATION:?}.$failover_node"
if [ "$failover_node" = pve-rr-a ]; then
  printf '%s\n' "${args[@]}" >"$SAM_REPRESENTATIVE_FAKE_INVOCATION"
fi
printf 'begin\t%s\n' "$failover_node" >>"$SAM_REPRESENTATIVE_FAKE_INVOCATION.timeline"
sleep "${SAM_REPRESENTATIVE_FAKE_DELAY:-0}"
[ "${SAM_REPRESENTATIVE_FAKE_FAIL_NODE:-}" != "$failover_node" ] || exit 42
mkdir -p "$evidence_dir/deploy" "$evidence_dir/matrix/initial" \
  "$evidence_dir/matrix/after-failover-pve-rr-a" \
  "$evidence_dir/matrix/after-rejoin-pve-rr-a" "$evidence_dir/convergence"
printf 'stage\tnode\tstatus\telapsed_seconds\nrr-a-started\tpve-rr-a\tPASS\t1\nrr-a-joined\tpve-rr-a\tPASS\t1\nrr-b-started\tpve-rr-b\tPASS\t1\nrr-b-joined\tpve-rr-b\tPASS\t1\nrr-pair-ready\tpve-rr-a,pve-rr-b\tPASS\t1\n' \
  >"$evidence_dir/deploy/rr-stage.tsv"
write_matrix() {
  local label="$1" cloud="$2" path="$3"
  jq -r --argjson cloud "$cloud" '
    .nodes.value | to_entries | map(select(.value.role == "client")) as $clients
    | $clients[] as $src | $clients[] as $dst
    | select($src.key != $dst.key)
    | select(($cloud | not) or $src.value.site != "pve")
    | [$src.key, $dst.key, "PASS"] | @tsv
  ' "$tofu_output" >"$path"
  if [ "${SAM_REPRESENTATIVE_FAKE_BAD_LABEL:-}" = "$label" ] && \
    { [ "${SAM_REPRESENTATIVE_FAKE_BAD_CLOUD:-both}" = both ] || [ "${SAM_REPRESENTATIVE_FAKE_BAD_CLOUD:-both}" = "$cloud" ]; }; then
    case "${SAM_REPRESENTATIVE_FAKE_BAD_MATRIX:-missing}" in
      missing) sed -i '$d' "$path" ;;
      duplicate) first="$(head -n 1 "$path")"; sed -i "\$c\\$first" "$path" ;;
      failed) sed -i '1s/PASS/FAIL/' "$path" ;;
    esac
  fi
}
write_matrix initial false "$evidence_dir/matrix/initial/summary.tsv"
write_matrix initial true "$evidence_dir/matrix/initial/cloud-ingress-summary.tsv"
canary_rows=4
[ "${SAM_REPRESENTATIVE_FAKE_INCOMPLETE_CANARY:-0}" = 1 ] && canary_rows=3
for label in after-failover-pve-rr-a after-rejoin-pve-rr-a; do
  for _ in $(seq 1 "$canary_rows"); do printf 'client-a\tclient-b\tPASS\n'; done >"$evidence_dir/matrix/$label/transition-canary-summary.tsv"
done
rr_status=PASS
[ "${SAM_REPRESENTATIVE_FAKE_INCOMPLETE_RR:-0}" = 1 ] && rr_status=TIMEOUT
printf 'label\tstatus\telapsed_seconds\ninitial-rr-pve-rr-a\t%s\t1\ninitial-rr-pve-rr-b\t%s\t1\ninitial-dataplane\tPASS\t1\ninitial-provider\tPASS\t1\nafter-failover-pve-rr-a-rr-pve-rr-b\t%s\t1\nafter-failover-pve-rr-a-dataplane\tPASS\t1\nafter-failover-pve-rr-a-provider\tPASS\t1\nafter-rejoin-pve-rr-a-rr-pve-rr-a\t%s\t1\nafter-rejoin-pve-rr-a-rr-pve-rr-b\t%s\t1\nafter-rejoin-pve-rr-a-dataplane\tPASS\t1\nafter-rejoin-pve-rr-a-provider\tPASS\t1\n' "$rr_status" "$rr_status" "$rr_status" "$rr_status" "$rr_status" \
  >"$evidence_dir/convergence/summary.tsv"
if [ "$failover_node" != pve-rr-a ]; then
  for label in "after-failover-$failover_node" "after-rejoin-$failover_node"; do
    mkdir -p "$evidence_dir/matrix/$label"
    write_matrix "$label" false "$evidence_dir/matrix/$label/summary.tsv"
    write_matrix "$label" true "$evidence_dir/matrix/$label/cloud-ingress-summary.tsv"
    for gate in dataplane provider rr-pve-rr-a rr-pve-rr-b; do
      [ "${SAM_REPRESENTATIVE_FAKE_MISSING_GATE:-}" != "$label-$gate" ] || continue
      printf '%s-%s\tPASS\t1\n' "$label" "$gate" >>"$evidence_dir/convergence/summary.tsv"
    done
  done
fi
for ack in "failover-stop-$failover_node" "rejoin-start-$failover_node"; do
  [ "${SAM_REPRESENTATIVE_FAKE_MISSING_GATE:-}" != "$ack" ] || continue
  ack_status=PASS
  [ "${SAM_REPRESENTATIVE_FAKE_FAILED_ACK:-}" != "$ack" ] || ack_status=FAIL
  printf '%s\t%s\t1\n' "$ack" "$ack_status" >>"$evidence_dir/convergence/summary.tsv"
done
printf 'end\t%s\n' "$failover_node" >>"$SAM_REPRESENTATIVE_FAKE_INVOCATION.timeline"
SCRIPT
chmod +x "$scripts/sam-e2e.sh"

tofu_output="$work/tofu-output.json"
jq -n '{
  nodes:{value:{
    "pve-rr-a":{role:"rr",site:"pve",pve_host:"pve02",pve_ssh_host:"pve02.example.test",underlay_bridge:"vmbr0"},
    "pve-rr-b":{role:"rr",site:"pve",pve_host:"pve03",pve_ssh_host:"pve03.example.test",underlay_bridge:"vmbr0"},
    "aws-leaf-a":{role:"leaf",site:"aws"}, "aws-leaf-b":{role:"leaf",site:"aws"},
    "azure-leaf-a":{role:"leaf",site:"azure"}, "azure-leaf-b":{role:"leaf",site:"azure"},
    "oci-leaf-a":{role:"leaf",site:"oci"}, "oci-leaf-b":{role:"leaf",site:"oci"},
    "pve-leaf-a":{role:"leaf",site:"pve"}, "pve-leaf-b":{role:"leaf",site:"pve"},
    "aws-client-a":{role:"client",site:"aws"}, "aws-client-b":{role:"client",site:"aws"},
    "azure-client-a":{role:"client",site:"azure"}, "azure-client-b":{role:"client",site:"azure"},
    "oci-client-a":{role:"client",site:"oci"}, "oci-client-b":{role:"client",site:"oci"},
    "pve-client-a":{role:"client",site:"pve"}, "pve-client-b":{role:"client",site:"pve"}
  }},
  fabric:{value:{
    topology_scale:"full",
    pve:{
      rr_fault_domain:"host-redundant",
      rr_nodes:["pve-rr-a","pve-rr-b"],
      rr_hosts:["pve02","pve03"],
      rr_ssh_hosts:["pve02.example.test","pve03.example.test"],
      leaf_capture_bridge:"rsamqa",
      rr_underlay_bridges:{"pve-rr-a":"vmbr0","pve-rr-b":"vmbr0"}
    }
  }}
}' >"$tofu_output"

artifact="$work/routerd.tar.gz"
guest_ssh_key="$work/id_ed25519"
pve_ssh_key="$work/pve_id_ed25519"
pve_known_hosts="$work/pve-known_hosts"
tfvars="$work/terraform.tfvars"
touch "$artifact" "$pve_known_hosts" "$tfvars"
ssh-keygen -q -t ed25519 -N '' -f "$guest_ssh_key"
ssh-keygen -q -t ed25519 -N '' -f "$pve_ssh_key"
invocation="$work/invocation.txt"
evidence="$work/evidence"

SAM_REPRESENTATIVE_FAKE_INVOCATION="$invocation" \
"$scripts/sam-representative-redundancy.sh" \
  --tofu-output "$tofu_output" \
  --artifact "$artifact" \
  --tfvars "$tfvars" \
  --ssh-key "$guest_ssh_key" \
  --pve-ssh-key "$pve_ssh_key" \
  --pve-known-hosts "$pve_known_hosts" \
  --evidence-root "$evidence" \
  --max-runtime-seconds 5400 >/dev/null

jq -e '
  .profile == "representative-redundancy"
  and .result == "pass"
  and .limits.maxRuntimeSeconds == 5400
  and .topology == {routerCount:10,clientCount:8,cloudClientCount:6,rrFaultDomain:"host-redundant"}
  and .gates == {
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
    edgeAFailover:true,
    edgeARejoin:true,
    edgeAClientMatrix:true,
    edgeACloudIngressMatrix:true,
    legacyProtocols:false,
    performance:false,
    symmetricBFailover:false,
    provisioning:false,
    destruction:false
  }
' "$evidence/profile-result.json" >/dev/null
jq -e '
  (.edgeScenarios | keys) == ["aws-leaf-a","azure-leaf-a","oci-leaf-a","pve-leaf-a"]
  and (.edgeScenarios | all(.result == "pass" and .e2eExit == 0 and .failoverEvidenceExit == 0 and .rejoinEvidenceExit == 0))
  and .symmetry == {testedSides:["a"], bSideEquivalent:"unproven"}
' "$evidence/profile-result.json" >/dev/null
expected_timeline="$work/expected-timeline"
for node in pve-rr-a aws-leaf-a azure-leaf-a oci-leaf-a pve-leaf-a; do
  printf 'begin\t%s\nend\t%s\n' "$node" "$node"
done >"$expected_timeline"
diff -u "$expected_timeline" "$invocation.timeline"
for node in aws-leaf-a azure-leaf-a oci-leaf-a pve-leaf-a; do
  for arg in --skip-deploy --skip-initial-validation --reuse-deployed-topology --staged-rr-pair --full-cloud-ingress \
    --rejoin-after-failover --skip-legacy-protocols; do
    grep -Fx -- "$arg" "$invocation.$node" >/dev/null
  done
  if grep -Eq -- '--transition-canary|--performance-tests|--destroy-cmd|--skip-matrix' "$invocation.$node"; then
    echo "edge transition did not request complete non-performance matrices" >&2
    exit 1
  fi
done
for arg in --staged-rr-pair --failover-node --rejoin-after-failover --transition-canary \
  --skip-legacy-protocols --skip-load-balance-report --success-evidence-minimal; do
  grep -Fx -- "$arg" "$invocation" >/dev/null
done
grep -A2 -Fx -- '--staged-rr-pair' "$invocation" | grep -Fx -- 'pve-rr-a' >/dev/null
grep -A2 -Fx -- '--staged-rr-pair' "$invocation" | grep -Fx -- 'pve-rr-b' >/dev/null
grep -A1 -Fx -- '--ssh-key' "$invocation" | grep -Fx -- "$guest_ssh_key" >/dev/null
grep -A1 -Fx -- '--pve-ssh-key' "$invocation" | grep -Fx -- "$pve_ssh_key" >/dev/null
if grep -Eq -- '--destroy-cmd|--performance-tests|--load-balance-report' "$invocation"; then
  echo "representative profile enabled a disallowed E2E phase" >&2
  exit 1
fi
grep -F 'transition_canary_matrix()' "$harness_script" >/dev/null
grep -F 'deploy_staged_rr' "$harness_script" >/dev/null
grep -F 'rr_membership_probe()' "$harness_script" >/dev/null
grep -F 'stage_staged_rr_membership' "$harness_script" >/dev/null
grep -F 'pin_pve_guest_host_keys()' "$harness_script" >/dev/null
grep -F 'append_pve_guest_keys_for_client()' "$harness_script" >/dev/null
grep -F 'PVEQGAHostKeyProvenance' "$harness_script" >/dev/null
if rg -q 'node_requires_qga|pve_qga_(preflight|exec|copy)|qm guest exec' "$harness_script"; then
  echo "sam-e2e retained an unsupported QGA command fallback" >&2
  exit 1
fi

for kind in DHCPv4Client DHCPv4Server DHCPv6Client DHCPv6PrefixDelegation DHCPv6Server IPv6RAAddress IPv6RouterAdvertisement; do
  safety_configs="$work/pve-control-plane-$kind"
  safety_evidence="$work/pve-control-plane-evidence-$kind"
  safety_stdout="$work/pve-control-plane-$kind.stdout"
  safety_stderr="$work/pve-control-plane-$kind.stderr"
  unsafe_node=pve-leaf-a
  # RR management NICs share the same PVE L2 as leaves. Exercise the RR path
  # explicitly so this gate cannot regress to inspecting leaf configs only.
  [ "$kind" = DHCPv4Client ] && unsafe_node=pve-rr-a
  rendered_kind="$kind"
  [ "$kind" = IPv6RAAddress ] && rendered_kind="\"$kind\""
  mkdir -p "$safety_configs"
  printf '%s\n' \
    'apiVersion: routerd.net/v1alpha1' \
    'kind: Router' \
    'metadata:' \
    "  name: $unsafe_node" \
    'spec:' \
    '  resources:' \
    '    - apiVersion: net.routerd.net/v1alpha1' \
    "      kind: $rendered_kind" \
    '      metadata: { name: forbidden-control-plane }' \
    >"$safety_configs/$unsafe_node.yaml"
  for safe_node in pve-rr-a pve-rr-b pve-leaf-a pve-leaf-b; do
    [ "$safe_node" = "$unsafe_node" ] && continue
    printf '%s\n' \
      'apiVersion: routerd.net/v1alpha1' \
      'kind: Router' \
      'metadata:' \
      "  name: $safe_node" \
      'spec: {}' \
      >"$safety_configs/$safe_node.yaml"
  done
  if "$harness_script" \
    --tofu-output "$tofu_output" \
    --artifact "$artifact" \
    --ssh-key "$guest_ssh_key" \
    --pve-ssh-key "$pve_ssh_key" \
    --pve-known-hosts "$pve_known_hosts" \
    --configs-dir "$safety_configs" \
    --skip-deploy \
    --evidence-dir "$safety_evidence" >"$safety_stdout" 2>"$safety_stderr"; then
    echo "sam-e2e accepted forbidden PVE router resource kind: $kind" >&2
    exit 1
  fi
  grep -F "forbidden resource kind $kind" "$safety_stderr" >/dev/null
  grep -F "node=$unsafe_node" "$safety_stderr" >/dev/null
  if [ -n "$(find "$safety_evidence/preflight" -type f -print -quit)" ]; then
    echo "sam-e2e reached PVE/cloud preflight after rejecting $kind" >&2
    exit 1
  fi
  if [ -e "$safety_evidence/config-validate" ]; then
    echo "sam-e2e validated unsafe PVE router config after rejecting $kind" >&2
    exit 1
  fi
done

if SAM_REPRESENTATIVE_FAKE_INVOCATION="$work/too-long-invocation" "$scripts/sam-representative-redundancy.sh" \
  --tofu-output "$tofu_output" --artifact "$artifact" --ssh-key "$guest_ssh_key" \
  --pve-ssh-key "$pve_ssh_key" \
  --pve-known-hosts "$pve_known_hosts" \
  --evidence-root "$work/too-long" --max-runtime-seconds 5401 >/dev/null 2>&1; then
  echo "representative profile accepted a runtime budget above its hard cap" >&2
  exit 1
fi
if [ -e "$work/too-long-invocation.timeline" ] || [ -e "$work/too-long" ]; then
  echo "over-budget profile reached its harness or evidence setup" >&2
  exit 1
fi

SAM_REPRESENTATIVE_FAKE_INVOCATION="$work/default-invocation" "$scripts/sam-representative-redundancy.sh" \
  --tofu-output "$tofu_output" --artifact "$artifact" --ssh-key "$guest_ssh_key" \
  --pve-ssh-key "$pve_ssh_key" --pve-known-hosts "$pve_known_hosts" \
  --evidence-root "$work/default-budget" >/dev/null
jq -e '.result == "pass" and .limits.maxRuntimeSeconds == 5400' \
  "$work/default-budget/profile-result.json" >/dev/null

if SAM_REPRESENTATIVE_FAKE_INVOCATION="$invocation" SAM_REPRESENTATIVE_FAKE_INCOMPLETE_CANARY=1 \
  "$scripts/sam-representative-redundancy.sh" \
    --tofu-output "$tofu_output" --artifact "$artifact" --ssh-key "$guest_ssh_key" \
    --pve-ssh-key "$pve_ssh_key" \
    --pve-known-hosts "$pve_known_hosts" \
    --evidence-root "$work/incomplete" --max-runtime-seconds 5400 >/dev/null 2>&1; then
  echo "representative profile accepted an incomplete transition canary" >&2
  exit 1
fi
jq -e '.result == "fail" and .outcomes.failoverEvidenceExit == 1' \
  "$work/incomplete/profile-result.json" >/dev/null

if SAM_REPRESENTATIVE_FAKE_INVOCATION="$invocation" SAM_REPRESENTATIVE_FAKE_INCOMPLETE_RR=1 \
  "$scripts/sam-representative-redundancy.sh" \
    --tofu-output "$tofu_output" --artifact "$artifact" --ssh-key "$guest_ssh_key" \
    --pve-ssh-key "$pve_ssh_key" \
    --pve-known-hosts "$pve_known_hosts" \
    --evidence-root "$work/incomplete-rr" --max-runtime-seconds 5400 >/dev/null 2>&1; then
  echo "representative profile accepted RR-B continuity without RR membership evidence" >&2
  exit 1
fi
jq -e '.result == "fail" and .outcomes.failoverEvidenceExit == 1' \
  "$work/incomplete-rr/profile-result.json" >/dev/null

run_edge_fault() {
  local label="$1"; shift
  local fault_invocation="$work/fault-$label"
  : >"$fault_invocation.timeline"
  if env SAM_REPRESENTATIVE_FAKE_INVOCATION="$fault_invocation" "$@" \
    "${fault_profile:-$scripts/sam-representative-redundancy.sh}" \
    --tofu-output "$tofu_output" --artifact "$artifact" --ssh-key "$guest_ssh_key" \
    --pve-ssh-key "$pve_ssh_key" --pve-known-hosts "$pve_known_hosts" \
    --evidence-root "$work/fault-evidence-$label" --max-runtime-seconds "${fault_budget:-5400}" >/dev/null 2>&1; then
    echo "representative profile accepted edge fault: $label" >&2
    exit 1
  fi
  jq -e '.result == "fail"' "$work/fault-evidence-$label/profile-result.json" >/dev/null
  if grep -q $'begin\tazure-leaf-a' "$fault_invocation.timeline"; then
    echo "representative profile proceeded after failed AWS edge scenario: $label" >&2
    exit 1
  fi
}
run_edge_fault e2e SAM_REPRESENTATIVE_FAKE_FAIL_NODE=aws-leaf-a
for node in pve-rr-a aws-leaf-a; do
  for phase in failover-stop rejoin-start; do
    run_edge_fault "missing-ack-$phase-$node" SAM_REPRESENTATIVE_FAKE_MISSING_GATE="$phase-$node"
    run_edge_fault "failed-ack-$phase-$node" SAM_REPRESENTATIVE_FAKE_FAILED_ACK="$phase-$node"
    if [ "$node" = pve-rr-a ]; then
      for kind in missing failed; do
        if grep -q $'begin\taws-leaf-a' "$work/fault-$kind-ack-$phase-$node.timeline"; then
          echo "profile started edge scenario without the preceding RR service ACK" >&2
          exit 1
        fi
      done
    fi
  done
done
for phase in after-failover after-rejoin; do
  for bad in missing duplicate failed; do
    for cloud in true false; do
      run_edge_fault "$phase-$bad-$cloud" SAM_REPRESENTATIVE_FAKE_BAD_LABEL="$phase-aws-leaf-a" \
        SAM_REPRESENTATIVE_FAKE_BAD_MATRIX="$bad" SAM_REPRESENTATIVE_FAKE_BAD_CLOUD="$cloud"
    done
  done
  for gate in dataplane provider rr-pve-rr-a rr-pve-rr-b; do
    run_edge_fault "$phase-$gate" SAM_REPRESENTATIVE_FAKE_MISSING_GATE="$phase-aws-leaf-a-$gate"
  done
done

# Replace clock reads in a test-only copy, never the production source. This
# exercises the real remaining-budget arithmetic without relying on the host
# completing all topology/jq checks inside a one-second scheduling margin.
clock_scripts="$work/clock-scripts"
mkdir -p "$clock_scripts" "$work/clock-bin"
# The replacement must be evaluated by the copied wrapper, not this test.
# shellcheck disable=SC2016
sed 's/\$SECONDS/$(cat "$SAM_REPRESENTATIVE_FAKE_CLOCK")/g;s/max_runtime_seconds - SECONDS/max_runtime_seconds - $(cat "$SAM_REPRESENTATIVE_FAKE_CLOCK")/g' \
  "$scripts/sam-representative-redundancy.sh" >"$clock_scripts/sam-representative-redundancy.sh"
cp "$scripts/sam-e2e.sh" "$clock_scripts/sam-e2e.sh"
chmod +x "$clock_scripts/sam-representative-redundancy.sh"
cat >"$work/clock-bin/timeout" <<'CLOCK_TIMEOUT'
#!/usr/bin/env bash
set -euo pipefail
[ "$1" = --foreground ]; shift
[ "$1" = --kill-after=30s ]; shift
budget="${1%s}"; shift
printf '%s\n' "$budget" >>"${SAM_REPRESENTATIVE_FAKE_TIMEOUT_BUDGETS:?}"
now="$(cat "${SAM_REPRESENTATIVE_FAKE_CLOCK:?}")"
# Each successful harness call costs six virtual seconds. A second call
# with only four seconds remaining times out instead of receiving a reset.
if [ "$budget" -le 6 ]; then
  printf '%s\n' "$((now + budget))" >"$SAM_REPRESENTATIVE_FAKE_CLOCK"
  exit 124
fi
"$@"
printf '%s\n' "$((now + 6))" >"$SAM_REPRESENTATIVE_FAKE_CLOCK"
CLOCK_TIMEOUT
chmod +x "$work/clock-bin/timeout"
fake_clock="$work/fake-clock"
fake_budgets="$work/fake-budgets"
printf '0\n' >"$fake_clock"
: >"$fake_budgets"
fault_profile="$clock_scripts/sam-representative-redundancy.sh"
fault_budget=10
run_edge_fault absolute-budget PATH="$work/clock-bin:$PATH" \
  SAM_REPRESENTATIVE_FAKE_CLOCK="$fake_clock" SAM_REPRESENTATIVE_FAKE_TIMEOUT_BUDGETS="$fake_budgets"
printf '10\n4\n' >"$work/expected-budgets"
diff -u "$work/expected-budgets" "$fake_budgets"
grep -q $'end\tpve-rr-a' "$work/fault-absolute-budget.timeline"
if grep -q $'end\taws-leaf-a' "$work/fault-absolute-budget.timeline"; then
  echo "edge scenario received a fresh timeout instead of the remaining budget" >&2
  exit 1
fi
jq -e '.outcomes.samE2EExit == 124 and .limits.maxRuntimeSeconds == 10 and .elapsedSeconds == 10' \
  "$work/fault-evidence-absolute-budget/profile-result.json" >/dev/null
printf '0\n' >"$fake_clock"
: >"$fake_budgets"
fault_budget=1
run_edge_fault first-timeout PATH="$work/clock-bin:$PATH" \
  SAM_REPRESENTATIVE_FAKE_CLOCK="$fake_clock" SAM_REPRESENTATIVE_FAKE_TIMEOUT_BUDGETS="$fake_budgets"
if grep -q $'begin\taws-leaf-a' "$work/fault-first-timeout.timeline"; then
  echo "profile started edge fault after baseline timeout" >&2
  exit 1
fi
unset fault_budget fault_profile

for ownership_gate in carp unknown; do
  ownership_invocation="$work/ownership-$ownership_gate"
  if SAM_REPRESENTATIVE_FAKE_INVOCATION="$ownership_invocation" PVE_OWNERSHIP_GATE="$ownership_gate" \
    "$scripts/sam-representative-redundancy.sh" \
      --tofu-output "$tofu_output" --artifact "$artifact" --ssh-key "$guest_ssh_key" \
      --pve-ssh-key "$pve_ssh_key" --pve-known-hosts "$pve_known_hosts" \
      --evidence-root "$work/ownership-evidence-$ownership_gate" >/dev/null 2>&1; then
    echo "A-only profile accepted an unqualified ownership gate: $ownership_gate" >&2
    exit 1
  fi
  [ ! -e "$ownership_invocation.timeline" ] || {
    echo "ownership-mode rejection occurred after invoking the mutating harness" >&2
    exit 1
  }
done

mismatched_topology="$work/mismatched-topology.json"
jq '.nodes.value["pve-rr-b"].pve_host = "pve02"' "$tofu_output" >"$mismatched_topology"
if SAM_REPRESENTATIVE_FAKE_INVOCATION="$invocation" "$scripts/sam-representative-redundancy.sh" \
  --tofu-output "$mismatched_topology" --artifact "$artifact" --ssh-key "$guest_ssh_key" \
  --pve-ssh-key "$pve_ssh_key" \
  --pve-known-hosts "$pve_known_hosts" \
  --evidence-root "$work/mismatched" --max-runtime-seconds 5400 >/dev/null 2>&1; then
  echo "representative profile accepted node/fabric PVE RR host mismatch" >&2
  exit 1
fi

capture_nic_topology="$work/capture-nic-topology.json"
jq '.nodes.value["pve-rr-b"].underlay_bridge = .fabric.value.pve.leaf_capture_bridge' "$tofu_output" >"$capture_nic_topology"
if SAM_REPRESENTATIVE_FAKE_INVOCATION="$invocation" "$scripts/sam-representative-redundancy.sh" \
  --tofu-output "$capture_nic_topology" --artifact "$artifact" --ssh-key "$guest_ssh_key" \
  --pve-ssh-key "$pve_ssh_key" \
  --pve-known-hosts "$pve_known_hosts" \
  --evidence-root "$work/capture-nic" --max-runtime-seconds 5400 >/dev/null 2>&1; then
  echo "representative profile accepted a PVE RR capture-bridge NIC" >&2
  exit 1
fi

printf 'sam representative-redundancy offline OK\n'
