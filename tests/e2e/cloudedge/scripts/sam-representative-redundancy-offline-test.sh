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
printf '%s\n' "$@" >"${SAM_REPRESENTATIVE_FAKE_INVOCATION:?}"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --evidence-dir) evidence_dir="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -n "$evidence_dir" ]
mkdir -p "$evidence_dir/deploy" "$evidence_dir/matrix/initial" \
  "$evidence_dir/matrix/after-failover-pve-rr-a" \
  "$evidence_dir/matrix/after-rejoin-pve-rr-a" "$evidence_dir/convergence"
printf 'stage\tnode\tstatus\telapsed_seconds\nrr-a-started\tpve-rr-a\tPASS\t1\nrr-a-joined\tpve-rr-a\tPASS\t1\nrr-b-started\tpve-rr-b\tPASS\t1\nrr-b-joined\tpve-rr-b\tPASS\t1\nrr-pair-ready\tpve-rr-a,pve-rr-b\tPASS\t1\n' \
  >"$evidence_dir/deploy/rr-stage.tsv"
for _ in $(seq 1 56); do printf 'client-a\tclient-b\tPASS\n'; done >"$evidence_dir/matrix/initial/summary.tsv"
for _ in $(seq 1 42); do printf 'cloud-client-a\tclient-b\tPASS\n'; done >"$evidence_dir/matrix/initial/cloud-ingress-summary.tsv"
canary_rows=4
[ "${SAM_REPRESENTATIVE_FAKE_INCOMPLETE_CANARY:-0}" = 1 ] && canary_rows=3
for label in after-failover-pve-rr-a after-rejoin-pve-rr-a; do
  for _ in $(seq 1 "$canary_rows"); do printf 'client-a\tclient-b\tPASS\n'; done >"$evidence_dir/matrix/$label/transition-canary-summary.tsv"
done
rr_status=PASS
[ "${SAM_REPRESENTATIVE_FAKE_INCOMPLETE_RR:-0}" = 1 ] && rr_status=TIMEOUT
printf 'label\tstatus\telapsed_seconds\ninitial-rr-pve-rr-a\t%s\t1\ninitial-rr-pve-rr-b\t%s\t1\ninitial-dataplane\tPASS\t1\ninitial-provider\tPASS\t1\nafter-failover-pve-rr-a-rr-pve-rr-b\t%s\t1\nafter-failover-pve-rr-a-dataplane\tPASS\t1\nafter-failover-pve-rr-a-provider\tPASS\t1\nafter-rejoin-pve-rr-a-rr-pve-rr-a\t%s\t1\nafter-rejoin-pve-rr-a-rr-pve-rr-b\t%s\t1\nafter-rejoin-pve-rr-a-dataplane\tPASS\t1\nafter-rejoin-pve-rr-a-provider\tPASS\t1\n' "$rr_status" "$rr_status" "$rr_status" "$rr_status" "$rr_status" \
  >"$evidence_dir/convergence/summary.tsv"
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
  --max-runtime-seconds 1920 >/dev/null

jq -e '
  .profile == "representative-redundancy"
  and .result == "pass"
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
    legacyProtocols:false,
    performance:false,
    symmetricBFailover:false,
    provisioning:false,
    destruction:false
  }
' "$evidence/profile-result.json" >/dev/null
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

if SAM_REPRESENTATIVE_FAKE_INVOCATION="$invocation" "$scripts/sam-representative-redundancy.sh" \
  --tofu-output "$tofu_output" --artifact "$artifact" --ssh-key "$guest_ssh_key" \
  --pve-ssh-key "$pve_ssh_key" \
  --pve-known-hosts "$pve_known_hosts" \
  --evidence-root "$work/too-long" --max-runtime-seconds 1921 >/dev/null 2>&1; then
  echo "representative profile accepted a runtime budget above its hard cap" >&2
  exit 1
fi

if SAM_REPRESENTATIVE_FAKE_INVOCATION="$invocation" SAM_REPRESENTATIVE_FAKE_INCOMPLETE_CANARY=1 \
  "$scripts/sam-representative-redundancy.sh" \
    --tofu-output "$tofu_output" --artifact "$artifact" --ssh-key "$guest_ssh_key" \
    --pve-ssh-key "$pve_ssh_key" \
    --pve-known-hosts "$pve_known_hosts" \
    --evidence-root "$work/incomplete" --max-runtime-seconds 1920 >/dev/null 2>&1; then
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
    --evidence-root "$work/incomplete-rr" --max-runtime-seconds 1920 >/dev/null 2>&1; then
  echo "representative profile accepted RR-B continuity without RR membership evidence" >&2
  exit 1
fi
jq -e '.result == "fail" and .outcomes.failoverEvidenceExit == 1' \
  "$work/incomplete-rr/profile-result.json" >/dev/null

mismatched_topology="$work/mismatched-topology.json"
jq '.nodes.value["pve-rr-b"].pve_host = "pve02"' "$tofu_output" >"$mismatched_topology"
if SAM_REPRESENTATIVE_FAKE_INVOCATION="$invocation" "$scripts/sam-representative-redundancy.sh" \
  --tofu-output "$mismatched_topology" --artifact "$artifact" --ssh-key "$guest_ssh_key" \
  --pve-ssh-key "$pve_ssh_key" \
  --pve-known-hosts "$pve_known_hosts" \
  --evidence-root "$work/mismatched" --max-runtime-seconds 1920 >/dev/null 2>&1; then
  echo "representative profile accepted node/fabric PVE RR host mismatch" >&2
  exit 1
fi

capture_nic_topology="$work/capture-nic-topology.json"
jq '.nodes.value["pve-rr-b"].underlay_bridge = .fabric.value.pve.leaf_capture_bridge' "$tofu_output" >"$capture_nic_topology"
if SAM_REPRESENTATIVE_FAKE_INVOCATION="$invocation" "$scripts/sam-representative-redundancy.sh" \
  --tofu-output "$capture_nic_topology" --artifact "$artifact" --ssh-key "$guest_ssh_key" \
  --pve-ssh-key "$pve_ssh_key" \
  --pve-known-hosts "$pve_known_hosts" \
  --evidence-root "$work/capture-nic" --max-runtime-seconds 1920 >/dev/null 2>&1; then
  echo "representative profile accepted a PVE RR capture-bridge NIC" >&2
  exit 1
fi

printf 'sam representative-redundancy offline OK\n'
