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

full_validation="$(routerd_script tests/e2e/cloudedge/scripts/sam-full-validation.sh)"
qualification_dir="$evidence_root/qualification/full"
mkdir -p "$qualification_dir"
log="$evidence_root/commands/full-qualification.log"
: >"$log"

# Do not create a nested session: the durable supervisor owns and quiesces the
# complete mutation process group before cleanup.
"$full_validation" \
  --tofu-output "$tofu_output_path" \
  --artifact "$artifact_path" \
  --ssh-key "$pve_ssh_private_key" \
  --evidence-root "$qualification_dir" \
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

scenario_status="$qualification_dir/scenario-status.tsv"
pass_count=0
fail_count=0
if [ -f "$scenario_status" ]; then
  pass_count="$(awk -F '\t' 'NR > 1 && $2 == "PASS" {count++} END {print count+0}' "$scenario_status")"
  fail_count="$(awk -F '\t' 'NR > 1 && $2 != "PASS" {count++} END {print count+0}' "$scenario_status")"
fi

if [ "$driver_rc" -eq 0 ] && [ "$pass_count" -eq 12 ] && [ "$fail_count" -eq 0 ]; then
  status=pass
  classification=none
  result=pass
  summary="all twelve ordered real-machine scenarios passed without repair"
  rc=0
else
  status=fail
  classification=product_failure
  result=fail
  summary="full validation exit=$driver_rc pass_scenarios=$pass_count failed_scenarios=$fail_count"
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
      name:"CloudEdge SAM full real-machine qualification",
      component:"cross-substrate",
      result:$result,
      checkedAt:$checkedAt,
      summary:$summary
    }]
  }' >"$out"
echo "qualification driver: $status ($summary)"
exit "$rc"
