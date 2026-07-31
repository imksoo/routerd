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
[ "$(git -C "$release_repo" rev-parse HEAD)" = "$artifact_commit" ] ||
  die "release repo HEAD does not equal exact artifact commit"
require_supervisor_mutating
touch_heartbeat

cloud_certification="$evidence_root/certification/cloud-certification.json"
cloud_driver_result="$evidence_root/certification/cloud-driver-result.json"
pve_driver_result="$evidence_root/certification/pve-driver-result.json"
full_certification="$evidence_root/certification/full-certification.json"
qualification_result="$evidence_root/qualification/release-qualification-result.json"

"$release_repo/scripts/certify-cloud-substrate.sh" \
  --environment "$(jq -er '.environment' "$contract_path")" \
  --topology "$(jq -er '.topology' "$contract_path")" \
  --providers aws,azure,oci --contract "$contract_path" \
  --driver "$script_dir/cloud-certification-driver.sh" \
  --driver-out "$cloud_driver_result" --out "$cloud_certification" --valid-for 2h
touch_heartbeat
"$release_repo/scripts/certify-pve-substrate.sh" \
  --environment "$(jq -er '.environment' "$contract_path")" \
  --topology "$(jq -er '.topology' "$contract_path")" \
  --providers pve --contract "$contract_path" \
  --driver "$script_dir/pve-certification-driver.sh" \
  --driver-out "$pve_driver_result" --cloud-certification "$cloud_certification" \
  --out "$full_certification" --valid-for 2h
touch_heartbeat
"$script_dir/qualification-driver.sh" \
  --certification "$full_certification" --release "$artifact_version" \
  --out "$qualification_result" --heartbeat "$heartbeat"
jq -n --arg status pass --arg runId "$run_id" \
  --arg certification "$full_certification" --arg qualification "$qualification_result" \
  --arg finishedAt "$(utc_now)" \
  '{status:$status,runId:$runId,certification:$certification,qualification:$qualification,finishedAt:$finishedAt}' \
  >"$out"
echo "release QA mutation: pass"
