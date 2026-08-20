#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=common.sh
source "$script_dir/common.sh"

certification=
release=
out=
heartbeat_arg=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --certification) certification="${2:?missing --certification value}"; shift 2 ;;
    --release) release="${2:?missing --release value}"; shift 2 ;;
    --out) out="${2:?missing --out value}"; shift 2 ;;
    --heartbeat) heartbeat_arg="${2:?missing --heartbeat value}"; shift 2 ;;
    -h|--help)
      echo "Usage: $(basename "$0") --certification FILE --release VERSION --out FILE --heartbeat FILE"
      exit 0
      ;;
    *) die "unknown argument: $1" ;;
  esac
done
[ -n "$certification" ] || die "--certification is required"
[ -n "$release" ] || die "--release is required"
[ -n "$out" ] || die "--out is required"
[ -n "$heartbeat_arg" ] || die "--heartbeat is required"
load_contract "$default_contract_path"
[ "$release" = "$artifact_version" ] ||
  die "qualification release does not equal exact artifact version"
[ "$(jq -er '.run.runId' "$certification")" = "$run_id" ] ||
  die "certification run ID does not equal contract"
heartbeat="$(absolute_path "$heartbeat_arg")"

qualification_profile="$(jq -er '.qualification.profile' "$contract_path")"
qualification_budget_seconds="$(jq -er '.qualification.qualificationBudgetSeconds' "$contract_path")"
safety_pve_management_control_plane="$(jq -er '.safety.pveManagementControlPlane' "$contract_path")"
[ "$qualification_profile" = "representative-redundancy" ] ||
  die "unsupported release qualification profile: $qualification_profile"
[ "$safety_pve_management_control_plane" = "none" ] ||
  die "release qualification requires passive PVE management control plane"
case "$qualification_budget_seconds" in
  ''|*[!0-9]*) die "qualification budget must be a positive integer" ;;
esac
[ "$qualification_budget_seconds" -gt 0 ] || die "qualification budget must be positive"

representative_validation="$(routerd_script tests/e2e/cloudedge/scripts/sam-representative-redundancy.sh)"
qualification_dir="$evidence_root/qualification/$qualification_profile"
mkdir -p "$qualification_dir"
log="$evidence_root/commands/$qualification_profile-qualification.log"
: >"$log"

# Do not create a nested session: the durable supervisor owns and quiesces the
# complete mutation process group before cleanup.
# The production profile requires the generator's QGA-DHCP management safety
# policy. The representative wrapper validates generated PVE configs before
# deployment; routerd never owns DHCP, DHCPv6, or RA on that shared underlay.
# The representative wrapper validates generated PVE configs before deploy.
"$representative_validation" \
  --tofu-output "$tofu_output_path" \
  --artifact "$artifact_path" \
  --tfvars "$tfvars_path" \
  --ssh-key "$guest_ssh_private_key" \
  --pve-ssh-key "$pve_ssh_private_key" \
  --pve-known-hosts "$pve_ssh_known_hosts" \
  --evidence-root "$qualification_dir" \
  --max-runtime-seconds "$qualification_budget_seconds" \
  >"$log" 2>&1 &
pid=$!
printf '%s\n' "$pid" >"$active_pid_file"
previous_signature=
while kill -0 "$pid" 2>/dev/null; do
  signature="$(
    find "$qualification_dir" "$log" -type f -printf '%T@:%s:%p\n' 2>/dev/null |
      sort | tail -n 1
  )"
  if [ -n "$signature" ] && [ "$signature" != "$previous_signature" ]; then
    touch "$heartbeat"
    previous_signature="$signature"
  fi
  sleep 5
done
set +e
wait "$pid"
driver_rc=$?
set -e
rm -f "$active_pid_file"
touch "$heartbeat"

profile_result="$qualification_dir/profile-result.json"

if [ "$driver_rc" -eq 0 ] && jq -e \
  --arg profile "$qualification_profile" \
  --argjson budget "$qualification_budget_seconds" \
  '.profile == $profile and .result == "pass" and .gates == {
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
  } and .limits.maxRuntimeSeconds == $budget
    and .topology == {routerCount:10,clientCount:8,cloudClientCount:6,rrFaultDomain:"host-redundant"}' \
  "$profile_result" >/dev/null 2>&1; then
  status=pass
  classification=none
  result=pass
  summary="representative PVE-RR A/AB/B-only/AB full-topology qualification passed without repair"
  rc=0
else
  status=fail
  classification=product_failure
  result=fail
  summary="$qualification_profile validation exit=$driver_rc; inspect $profile_result"
  rc=1
fi

jq -n \
  --arg status "$status" \
  --arg classification "$classification" \
  --arg result "$result" \
  --arg checkedAt "$(utc_now)" \
  --arg summary "$summary" \
  '{
    status:$status,
    classification:$classification,
    checks:[{
      name:"CloudEdge SAM representative-redundancy real-machine qualification",
      component:"cross-substrate",
      result:$result,
      checkedAt:$checkedAt,
      summary:$summary
    }]
  }' >"$out"
echo "qualification driver: $status ($summary)"
exit "$rc"
