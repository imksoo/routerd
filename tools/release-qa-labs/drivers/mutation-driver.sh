#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=common.sh
source "$script_dir/common.sh"

[ "$#" -eq 0 ] || die "Usage: $(basename "$0")"
load_contract "$default_contract_path"
release_repo="$(absolute_path "$(jq -er '.releaseRepo' "$run_env_path")")"
out="$evidence_root/mutation-result.json"
[ "$release_repo" = "$repo_root" ] || die "release repo is not the canonical checkout root"
[ "$(git_at_checkout_root rev-parse HEAD)" = "$artifact_commit" ] ||
  die "release repo HEAD does not equal exact artifact commit"
require_supervisor_mutating
touch_heartbeat

pve_certification="$evidence_root/certification/pve-certification.json"
pve_driver_result="$evidence_root/certification/pve-driver-result.json"
cloud_driver_result="$evidence_root/certification/cloud-driver-result.json"
full_certification="$evidence_root/certification/full-certification.json"
qualification_result="$evidence_root/qualification/release-qualification-result.json"

qualification_profile="$(jq -er '.qualification.profile' "$contract_path")"
qualification_scope="$(jq -er '.qualification.runScope' "$contract_path")"
provisioning_budget_seconds="$(jq -er '.qualification.provisioningBudgetSeconds' "$contract_path")"
qualification_budget_seconds="$(jq -er '.qualification.qualificationBudgetSeconds' "$contract_path")"
cleanup_reserve_seconds="$(jq -er '.qualification.minimumSupervisorReserveSeconds' "$contract_path")"
[ "$qualification_profile" = "representative-redundancy" ] ||
  die "unsupported release qualification profile: $qualification_profile"
case "$qualification_scope" in
  pve-certification-only|full-representative) ;;
  *) die "unsupported release qualification run scope: $qualification_scope" ;;
esac
for budget in "$provisioning_budget_seconds" "$qualification_budget_seconds" "$cleanup_reserve_seconds"; do
  case "$budget" in
    ''|*[!0-9]*) die "qualification budgets must be positive integer seconds" ;;
  esac
  [ "$budget" -gt 0 ] || die "qualification budgets must be positive"
done
ttl_seconds="$(jq -er '.lifecycle.ttl' "$contract_path" | sed -n 's/^\([0-9][0-9]*\)m$/\1/p' | awk '{print $1 * 60}')"
[ -n "$ttl_seconds" ] || die "release qualification requires a minute-based lifecycle TTL"
if [ $((provisioning_budget_seconds + qualification_budget_seconds + cleanup_reserve_seconds)) -gt "$ttl_seconds" ]; then
  die "qualification budgets exceed lifecycle TTL"
fi
command -v timeout >/dev/null 2>&1 || die "timeout is required for bounded release qualification"

budget_evidence="$evidence_root/qualification/budget-contract.json"
mkdir -p "$(dirname "$budget_evidence")"
jq -n \
  --arg profile "$qualification_profile" \
  --arg runScope "$qualification_scope" \
  --argjson provisioningBudgetSeconds "$provisioning_budget_seconds" \
  --argjson qualificationBudgetSeconds "$qualification_budget_seconds" \
  --argjson minimumSupervisorReserveSeconds "$cleanup_reserve_seconds" \
  --argjson lifecycleTTLSeconds "$ttl_seconds" \
  '{
    profile:$profile,
    runScope:$runScope,
    provisionAndCertification:{maxRuntimeSeconds:$provisioningBudgetSeconds},
    qualification:{maxRuntimeSeconds:$qualificationBudgetSeconds},
    supervisor:{minimumRemainingSeconds:$minimumSupervisorReserveSeconds, mutationTTLSeconds:$lifecycleTTLSeconds},
    cleanup:"supervisor-owned; each cleanup/inventory attempt uses the pinned lifecycle bounds"
  }' >"$budget_evidence"

provision_started="$SECONDS"
run_provision_stage() {
  local label="$1" remaining
  shift
  remaining=$((provisioning_budget_seconds - (SECONDS - provision_started)))
  [ "$remaining" -gt 0 ] || {
    echo "provision/certification budget exhausted before $label" >&2
    return 124
  }
  run_with_progress "$label" timeout --foreground --kill-after=30s "${remaining}s" "$@"
}

run_provision_stage pve-certification \
  "$release_repo/scripts/certify-pve-substrate.sh" \
  --environment "$(jq -er '.environment' "$contract_path")" \
  --topology "$(jq -er '.topology' "$contract_path")" \
  --providers pve --contract "$contract_path" \
  --driver "$script_dir/pve-certification-driver.sh" \
  --driver-out "$pve_driver_result" --out "$pve_certification" --valid-for 2h
touch_heartbeat
if [ "$qualification_scope" = "pve-certification-only" ]; then
  # This is a deliberately bounded substrate gate. The normal supervisor still
  # owns teardown, seven-scope zero inventory, and run-token revocation, but
  # no cloud resource or product qualification is allowed to begin.
  jq -n --arg status pve-certification-pass --arg runId "$run_id" \
    --arg runScope "$qualification_scope" --arg certification "$pve_certification" \
    --arg finishedAt "$(utc_now)" \
    '{status:$status,runId:$runId,runScope:$runScope,pveCertification:$certification,
      cloudProvisioning:false,productQualification:false,releaseQualification:"not-run",
      finishedAt:$finishedAt}' \
    >"$out"
  echo "release QA mutation: PVE certification gate pass; cloud qualification not run"
  exit 0
fi
run_provision_stage cloud-certification \
  "$release_repo/scripts/certify-cloud-substrate.sh" \
  --environment "$(jq -er '.environment' "$contract_path")" \
  --topology "$(jq -er '.topology' "$contract_path")" \
  --providers aws,azure,oci --contract "$contract_path" \
  --driver "$script_dir/cloud-certification-driver.sh" \
  --driver-out "$cloud_driver_result" --pve-certification "$pve_certification" \
  --out "$full_certification" --valid-for 2h
touch_heartbeat
"$script_dir/qualification-driver.sh" \
  --certification "$full_certification" --release "$artifact_version" \
  --out "$qualification_result" --heartbeat "$heartbeat"
jq -n --arg status pass --arg runId "$run_id" --arg runScope "$qualification_scope" \
  --arg certification "$full_certification" --arg qualification "$qualification_result" \
  --arg finishedAt "$(utc_now)" \
  '{status:$status,runId:$runId,runScope:$runScope,certification:$certification,qualification:$qualification,
    cloudProvisioning:true,productQualification:true,releaseQualification:"pass",finishedAt:$finishedAt}' \
  >"$out"
echo "release QA mutation: pass"
