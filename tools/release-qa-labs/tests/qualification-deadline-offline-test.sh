#!/usr/bin/env bash
set -euo pipefail

framework_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d "${TMPDIR:-/tmp}/routerd-qualification-deadline.XXXXXX")"
cleanup() {
  # These are only the fake profile processes created by this test. The real
  # driver must leave live children to its durable supervisor, not kill them.
  local marker pid command_line
  for marker in "$work"/*/profile.pid; do
    [ -f "$marker" ] || continue
    read -r pid <"$marker"
    [ -r "/proc/$pid/cmdline" ] || continue
    command_line="$(tr '\0' ' ' <"/proc/$pid/cmdline")"
    case "$command_line" in
      *"$work/profile.sh"*) kill "$pid" 2>/dev/null || true ;;
    esac
  done
  find "$work" -depth -delete
}
trap cleanup EXIT
mkdir -p "$work/drivers"
cp "$framework_root/drivers/qualification-driver.sh" "$work/drivers/qualification-driver.sh"

# Execute the complete real driver, including its actual wait loop, with only
# common.sh's contract/environment boundary and the profile executable mocked.
# No real credential, provider, SSH, systemd, or network operation is available.
cat >"$work/drivers/common.sh" <<'COMMON'
set -euo pipefail
default_contract_path="$QUALIFICATION_TEST_CASE/contract.json"
load_contract() {
  contract_path="$1"
  artifact_version=test-release run_id=test-run
  evidence_root="$QUALIFICATION_TEST_CASE/evidence"
  active_pid_file="$QUALIFICATION_TEST_CASE/active.pid"
  tofu_output_path=unused artifact_path=unused tfvars_path=unused
  guest_ssh_private_key=unused pve_ssh_private_key=unused pve_ssh_known_hosts=unused
}
die() { echo "$*" >&2; exit 2; }
absolute_path() { printf '%s\n' "$1"; }
routerd_script() { printf '%s\n' "$QUALIFICATION_TEST_ROOT/profile.sh"; }
utc_now() { date -u +%Y-%m-%dT%H:%M:%SZ; }
COMMON
cat >"$work/profile.sh" <<'PROFILE'
#!/usr/bin/env bash
set -euo pipefail
while [ "$#" -gt 0 ]; do
  case "$1" in
    --evidence-root) evidence_root="$2" ;;
  esac
  shift 2
done
printf '%s\n' "$$" >"$QUALIFICATION_TEST_CASE/profile.pid"
cp "$QUALIFICATION_TEST_CASE/pass.json" "$evidence_root/profile-result.json"
case "$QUALIFICATION_TEST_MODE" in
  pass) exit 0 ;;
  fail) exit 9 ;;
  hang)
    # Progress keeps the heartbeat fresh, so a stale-heartbeat timeout cannot
    # substitute for the qualification deadline. A valid PASS file also cannot
    # substitute for a completed, timely profile process.
    while :; do printf 'still-running\n'; sleep 0.1; done ;;
esac
PROFILE
chmod +x "$work/profile.sh"
jq -n '{
  profile:"representative-redundancy",result:"pass",limits:{maxRuntimeSeconds:1},
  topology:{routerCount:10,clientCount:8,cloudClientCount:6,rrFaultDomain:"host-redundant"},
  gates:{rrAStaged:true,rrAJoined:true,rrBStaged:true,rrBJoined:true,rrPairReady:true,
    fullBaseline:true,directedClientMatrix:true,directedCloudIngressMatrix:true,providerReadiness:true,
    rrAFailover:true,rrBControlPlaneContinuity:true,rrBContinuityCanary:true,rrARejoin:true,
    edgeAFailover:true,edgeARejoin:true,edgeAClientMatrix:true,edgeACloudIngressMatrix:true,
    legacyProtocols:false,performance:false,symmetricBFailover:false,provisioning:false,destruction:false},
  edgeScenarios:(["aws-leaf-a","azure-leaf-a","oci-leaf-a","pve-leaf-a"] | map({key:.,value:{
    result:"pass",failoverEvidenceExit:0,rejoinEvidenceExit:0,e2eExit:0}}) | from_entries)
}' >"$work/pass.json"

run_case() {
  local mode="$1" started rc=0 budget=10 case_dir="$work/$1"
  [ "$mode" != hang ] || budget=1
  mkdir -p "$case_dir/evidence/commands"
  jq -n --argjson budget "$budget" '{qualification:{profile:"representative-redundancy",qualificationBudgetSeconds:$budget},
    safety:{pveManagementControlPlane:"none"}}' >"$case_dir/contract.json"
  jq --argjson budget "$budget" '.limits.maxRuntimeSeconds=$budget' "$work/pass.json" >"$case_dir/pass.json"
  jq -n '{run:{runId:"test-run"}}' >"$case_dir/certification.json"
  started="$SECONDS"
  QUALIFICATION_TEST_ROOT="$work" QUALIFICATION_TEST_CASE="$case_dir" QUALIFICATION_TEST_MODE="$mode" \
    timeout --kill-after=1s 7s bash "$work/drivers/qualification-driver.sh" \
      --certification "$case_dir/certification.json" --release test-release \
      --out "$case_dir/result.json" --heartbeat "$case_dir/heartbeat" \
      >"$case_dir/driver.log" 2>&1 || rc=$?
  if [ "$rc" -eq 124 ] || [ "$rc" -eq 137 ]; then
    echo "FAIL: actual qualification driver wait loop exceeded its deadline test bound ($mode, budget=$budget)" >&2
    return 1
  fi
  if [ "$mode" = pass ]; then
    [ "$rc" -eq 0 ]
    jq -e '.status == "pass" and .checks[0].result == "pass"' "$case_dir/result.json" >/dev/null
  else
    [ "$rc" -ne 0 ]
    jq -e '.status == "fail" and .checks[0].result == "fail"' "$case_dir/result.json" >/dev/null
  fi
  if [ "$mode" = hang ]; then
    [ "$((SECONDS - started))" -le 3 ] || {
      echo "FAIL: driver did not return promptly at qualification deadline" >&2; return 1;
    }
    jq -e '.checks[0].summary | contains("deadline")' "$case_dir/result.json" >/dev/null
    [ -s "$case_dir/active.pid" ]
    cmp "$case_dir/active.pid" "$case_dir/profile.pid"
    local pid
    read -r pid <"$case_dir/active.pid"
    kill -0 "$pid" # Still owned by the supervisor, not killed by the driver.
  else
    [ ! -e "$case_dir/active.pid" ]
  fi
}

run_case hang
run_case pass
run_case fail
echo "Qualification deadline and supervisor recovery handoff offline checks: PASS"
