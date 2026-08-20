#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
profile_script="$repo_root/tests/e2e/cloudedge/scripts/sam-full-topology-minimal.sh"
harness_script="$repo_root/tests/e2e/cloudedge/scripts/sam-e2e.sh"

bash -n "$profile_script"
bash -n "$harness_script"
"$profile_script" --help >/dev/null
if "$harness_script" --load-balance-report --skip-load-balance-report >/dev/null 2>&1; then
  echo "sam-e2e accepted conflicting load-balance options" >&2
  exit 1
fi

work="$(mktemp -d "${TMPDIR:-/tmp}/routerd-sam-full-topology-minimal.XXXXXX")"
cleanup() {
  find "$work" -depth -delete
}
trap cleanup EXIT

scripts="$work/scripts"
mkdir -p "$scripts"
cp "$profile_script" "$scripts/sam-full-topology-minimal.sh"
cat >"$scripts/sam-e2e.sh" <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail

evidence_dir=
printf '%s\n' "$@" >"${SAM_MINIMAL_FAKE_INVOCATION:?}"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --evidence-dir) evidence_dir="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -n "$evidence_dir" ]
mkdir -p "$evidence_dir/matrix/initial" "$evidence_dir/convergence"
matrix_rows=56
[ "${SAM_MINIMAL_FAKE_INCOMPLETE:-0}" = 1 ] && matrix_rows=55
for _ in $(seq 1 "$matrix_rows"); do printf 'client-a\tclient-b\tPASS\n'; done >"$evidence_dir/matrix/initial/summary.tsv"
for _ in $(seq 1 42); do printf 'cloud-client-a\tclient-b\tPASS\n'; done >"$evidence_dir/matrix/initial/cloud-ingress-summary.tsv"
printf 'label\tstatus\telapsed_seconds\ninitial-dataplane\tPASS\t1\ninitial-provider\tPASS\t1\n' >"$evidence_dir/convergence/summary.tsv"
SCRIPT
chmod +x "$scripts/sam-e2e.sh"

nodes_json="$work/tofu-output.json"
jq -n '{
  nodes:{value:{
    "pve-rr-a":{role:"rr",site:"pve"},
    "pve-rr-b":{role:"rr",site:"pve"},
    "aws-leaf-a":{role:"leaf",site:"aws"}, "aws-leaf-b":{role:"leaf",site:"aws"},
    "azure-leaf-a":{role:"leaf",site:"azure"}, "azure-leaf-b":{role:"leaf",site:"azure"},
    "oci-leaf-a":{role:"leaf",site:"oci"}, "oci-leaf-b":{role:"leaf",site:"oci"},
    "pve-leaf-a":{role:"leaf",site:"pve"}, "pve-leaf-b":{role:"leaf",site:"pve"},
    "aws-client-a":{role:"client",site:"aws"}, "aws-client-b":{role:"client",site:"aws"},
    "azure-client-a":{role:"client",site:"azure"}, "azure-client-b":{role:"client",site:"azure"},
    "oci-client-a":{role:"client",site:"oci"}, "oci-client-b":{role:"client",site:"oci"},
    "pve-client-a":{role:"client",site:"pve"}, "pve-client-b":{role:"client",site:"pve"}
  }},
  fabric:{value:{topology_scale:"full"}}
}' >"$nodes_json"

artifact="$work/routerd.tar.gz"
guest_ssh_key="$work/id_ed25519"
pve_ssh_key="$work/pve_id_ed25519"
pve_known_hosts="$work/pve-known_hosts"
tfvars="$work/terraform.tfvars"
touch "$artifact" "$guest_ssh_key" "$pve_ssh_key" "$pve_known_hosts" "$tfvars"
invocation="$work/invocation.txt"
evidence="$work/evidence"

SAM_MINIMAL_FAKE_INVOCATION="$invocation" "$scripts/sam-full-topology-minimal.sh" \
  --tofu-output "$nodes_json" \
  --artifact "$artifact" \
  --tfvars "$tfvars" \
  --ssh-key "$guest_ssh_key" \
  --pve-ssh-key "$pve_ssh_key" \
  --pve-known-hosts "$pve_known_hosts" \
  --evidence-root "$evidence" \
  --max-runtime-seconds 1200 >/dev/null

jq -e '
  .profile == "full-topology-minimal"
  and .result == "pass"
  and .topology == {routerCount:10,clientCount:8,cloudClientCount:6}
  and .gates == {
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
  }
' "$evidence/profile-result.json" >/dev/null
grep -Fx -- '--skip-legacy-protocols' "$invocation" >/dev/null
grep -Fx -- '--skip-load-balance-report' "$invocation" >/dev/null
grep -Fx -- '--success-evidence-minimal' "$invocation" >/dev/null
grep -A1 -Fx -- '--ssh-key' "$invocation" | grep -Fx -- "$guest_ssh_key" >/dev/null
grep -A1 -Fx -- '--pve-ssh-key' "$invocation" | grep -Fx -- "$pve_ssh_key" >/dev/null
if grep -Eq -- '--performance-tests|--failover-node|--load-balance-report|--destroy-cmd' "$invocation"; then
  echo "minimal profile enabled a disallowed E2E phase" >&2
  exit 1
fi
# shellcheck disable=SC2016 # Match the literal guard in the harness source.
grep -F '[ "$skip_load_balance_report" -eq 0 ] || return 0' "$harness_script" >/dev/null

if SAM_MINIMAL_FAKE_INVOCATION="$invocation" "$scripts/sam-full-topology-minimal.sh" \
  --tofu-output "$nodes_json" --artifact "$artifact" --ssh-key "$guest_ssh_key" \
  --pve-ssh-key "$pve_ssh_key" \
  --pve-known-hosts "$pve_known_hosts" \
  --evidence-root "$work/too-long" --max-runtime-seconds 1201 >/dev/null 2>&1; then
  echo "minimal profile accepted a runtime budget above its hard cap" >&2
  exit 1
fi

if SAM_MINIMAL_FAKE_INVOCATION="$invocation" SAM_MINIMAL_FAKE_INCOMPLETE=1 \
  "$scripts/sam-full-topology-minimal.sh" \
    --tofu-output "$nodes_json" --artifact "$artifact" --ssh-key "$guest_ssh_key" \
    --pve-ssh-key "$pve_ssh_key" \
    --pve-known-hosts "$pve_known_hosts" \
    --evidence-root "$work/incomplete" --max-runtime-seconds 1200 >/dev/null 2>&1; then
  echo "minimal profile accepted an incomplete directed client matrix" >&2
  exit 1
fi
jq -e '.result == "fail" and .outcomes.gateEvidenceExit == 1' \
  "$work/incomplete/profile-result.json" >/dev/null

printf 'sam full-topology-minimal offline OK\n'
