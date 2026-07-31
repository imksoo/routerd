#!/usr/bin/env bash
set -euo pipefail
umask 077

framework_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repo_root="$(git -C "$framework_root" rev-parse --show-toplevel)"
run_root="$(dirname "$repo_root")"
runtime_root="$run_root/runtime"
# Exported to scripts that source this library; ShellCheck analyzes this file alone.
# shellcheck disable=SC2034
default_contract_path="${ROUTERD_RELEASE_QA_PINNED_CONTRACT:-$runtime_root/contract.json}"
run_env_path="${ROUTERD_RELEASE_QA_PINNED_RUN_ENV:-$runtime_root/run.env.json}"

die() {
  echo "release lab driver: $*" >&2
  exit 2
}

utc_now() {
  date -u +%Y-%m-%dT%H:%M:%SZ
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

require_private_file() {
  local file="$1" label="$2" mode
  [ -f "$file" ] || die "$label not found"
  mode="$(stat -c %a "$file")"
  [ "$mode" = 600 ] || die "$label must have mode 0600 (got $mode)"
  [ "$(stat -c %u "$file")" = "$(id -u)" ] || die "$label must be owned by the executing UID"
}

absolute_path() {
  readlink -m "$1"
}

require_run_confined() {
  local candidate resolved
  candidate="$1"
  resolved="$(readlink -m "$candidate")"
  case "$resolved" in
    "$run_root"|"$run_root"/*) ;;
    *) die "runtime path escapes canonical run root: $candidate" ;;
  esac
}

extract_tfvars_string() {
  local file="$1" key="$2"
  awk -v key="$key" '
    $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
      sub(/#.*/, "")
      sub("^[^=]*=", "")
      gsub(/^[[:space:]]+|[[:space:]]+$/, "")
      if ($0 ~ /^".*"$/) {
        sub(/^"/, "")
        sub(/"$/, "")
      }
      print
      exit
    }
  ' "$file"
}

load_contract() {
  local supplied="$1"
  require_command jq
  contract_path="$(absolute_path "$supplied")"
  require_private_file "$contract_path" contract

  run_id="$(jq -er '.runId' "$contract_path")"
  artifact_path="$(jq -er '.routerdArtifact.path' "$contract_path")"
  artifact_path="$(absolute_path "$artifact_path")"
  export artifact_version
  artifact_version="$(jq -er '.routerdArtifact.version' "$contract_path")"
  artifact_commit="$(jq -er '.routerdArtifact.commit' "$contract_path")"
  labs_commit="$(jq -er '.qaImplementation.commit // .labsCommit' "$contract_path")"
  tf_dir="$(jq -er '.tofu.workingDirectory' "$contract_path")"
  tf_dir="$(absolute_path "$tf_dir")"
  tofu_state_path="$(jq -er '.tofu.statePath' "$contract_path")"
  tofu_state_path="$(absolute_path "$tofu_state_path")"
  tfvars_path="$(jq -er '.tofu.variablesPath' "$contract_path")"
  tfvars_path="$(absolute_path "$tfvars_path")"
  tfvars_path="${ROUTERD_RELEASE_QA_PINNED_TFVARS:-$tfvars_path}"
  tofu_output_path="$(jq -er '.tofu.outputPath' "$contract_path")"
  tofu_output_path="$(absolute_path "$tofu_output_path")"
  export contract_ttl contract_stale
  contract_ttl="$(jq -er '.lifecycle.ttl' "$contract_path")"
  contract_stale="$(jq -er '.lifecycle.heartbeatStale' "$contract_path")"

  require_run_confined "$contract_path"
  require_run_confined "$artifact_path"
  require_run_confined "$tf_dir"
  require_run_confined "$tofu_state_path"
  require_run_confined "$tfvars_path"
  require_run_confined "$tofu_output_path"
  require_run_confined "$run_env_path"

  [ -f "$artifact_path" ] || die "artifact not found: $artifact_path"
  [ -d "$tf_dir" ] || die "OpenTofu directory not found: $tf_dir"
  require_private_file "$tfvars_path" "OpenTofu variables"
  [ "$(extract_tfvars_string "$tfvars_path" run_id)" = "$run_id" ] ||
    die "tfvars run_id does not equal contract runId"
  [ "$(extract_tfvars_string "$tfvars_path" commit)" = "$artifact_commit" ] ||
    die "tfvars commit does not equal exact artifact commit"
  pve_node="$(jq -er '.pve.node' "$contract_path")"
  pve_ssh_host="$(jq -er '.pve.sshHost' "$contract_path")"
  [ "$(extract_tfvars_string "$tfvars_path" pve_node_name)" = "$pve_node" ] ||
    die "tfvars pve_node_name does not equal contract pve.node"
  [ "$(extract_tfvars_string "$tfvars_path" pve_ssh_host)" = "$pve_ssh_host" ] ||
    die "tfvars pve_ssh_host does not equal contract pve.sshHost"
  [ "$(extract_tfvars_string "$tfvars_path" pve_endpoint)" = "https://$pve_ssh_host:8006/" ] ||
    die "tfvars pve_endpoint does not use contract pve.sshHost"

  local actual_labs_commit
  actual_labs_commit="$(git -C "$framework_root" rev-parse HEAD)"
  [ "$actual_labs_commit" = "$labs_commit" ] ||
    die "labsCommit does not equal framework HEAD"
  [ -z "$(git -C "$framework_root" status --short --untracked-files=no)" ] ||
    die "tracked framework files are dirty"

  require_private_file "$run_env_path" "run environment"
  local https_proxy_endpoint no_proxy_hosts pve_token_tfvars
  https_proxy_endpoint="$(jq -r '.httpsProxy // empty' "$run_env_path")"
  no_proxy_hosts="$(jq -r '.noProxy // "127.0.0.1,localhost,pve01"' "$run_env_path")"
  pve_token_tfvars="$(jq -r '.pveTokenTfvars // empty' "$run_env_path")"
  pve_ssh_private_key="${ROUTERD_RELEASE_QA_PINNED_PVE_SSH_PRIVATE_KEY:-$(jq -er '.pveSshPrivateKey' "$run_env_path")}"
  azure_auth_source="$(jq -r '.azureAuthSource // empty' "$run_env_path")"
  if [ -n "$azure_auth_source" ]; then
    azure_config_dir="$runtime_root/provider-state/azure"
    require_run_confined "$azure_auth_source"
    require_run_confined "$azure_config_dir"
    [ "$azure_auth_source" = "$runtime_root/secrets/azure-auth-source" ] ||
      die "Azure authentication source is not canonical"
    [ -d "$azure_config_dir" ] || die "run-confined Azure configuration is missing"
    [ "$(stat -c %a "$azure_config_dir")" = 700 ] ||
      die "run-confined Azure configuration must be mode 0700"
    [ "$(stat -c %u "$azure_config_dir")" = "$(id -u)" ] ||
      die "run-confined Azure configuration must be owned by the service UID"
    export AZURE_CONFIG_DIR="$azure_config_dir"
  fi
  pve_ssh_private_key="$(absolute_path "$pve_ssh_private_key")"
  require_run_confined "$pve_ssh_private_key"
  require_private_file "$pve_ssh_private_key" "PVE SSH private key"
  release_repo_path="$(jq -r '.releaseRepo // empty' "$run_env_path")"
  if [ -n "$release_repo_path" ]; then
    require_run_confined "$release_repo_path"
  fi
  if [ -n "$https_proxy_endpoint" ]; then
    export HTTPS_PROXY="$https_proxy_endpoint"
    export HTTP_PROXY="$https_proxy_endpoint"
    export https_proxy="$https_proxy_endpoint"
    export http_proxy="$https_proxy_endpoint"
    unset ALL_PROXY all_proxy
  else
    unset HTTPS_PROXY HTTP_PROXY ALL_PROXY https_proxy http_proxy all_proxy
  fi
  export NO_PROXY="$no_proxy_hosts"
  export no_proxy="$no_proxy_hosts"
  if [ -n "$pve_token_tfvars" ]; then
    require_run_confined "$pve_token_tfvars"
    require_private_file "$pve_token_tfvars" "PVE token source"
    TF_VAR_pve_api_token="$(extract_tfvars_string "$pve_token_tfvars" pve_api_token)"
    [ -n "$TF_VAR_pve_api_token" ] || die "PVE token source has no pve_api_token"
  fi
  tofu_binary="$(command -v tofu 2>/dev/null || true)"
  # shellcheck disable=SC2317
  tofu() {
    [ -n "$tofu_binary" ] || die "required command not found: tofu"
    if [ -n "${TF_VAR_pve_api_token:-}" ]; then
      env TF_DATA_DIR="$runtime_root/tofu-data" TF_VAR_pve_api_token="$TF_VAR_pve_api_token" "$tofu_binary" "$@"
    else
      env TF_DATA_DIR="$runtime_root/tofu-data" "$tofu_binary" "$@"
    fi
  }

  evidence_root="$runtime_root/evidence"
  plan_root="$runtime_root/plans"
  lifecycle_dir="$evidence_root/lifecycle"
  command_log_dir="$evidence_root/commands"
  heartbeat="$lifecycle_dir/heartbeat"
  active_pid_file="$lifecycle_dir/active.pid"
  checks_file="$lifecycle_dir/current-checks.ndjson"
  supervisor_state="$lifecycle_dir/supervisor-state.json"
  mkdir -p "$lifecycle_dir" "$command_log_dir" "$plan_root"
  chmod 700 "$plan_root"
}

require_supervisor_mutating() {
  [ -f "$supervisor_state" ] || die "durable supervisor state is missing"
  jq -e --arg runId "$run_id" \
    '.runId == $runId and .phase == "MUTATING" and (.mutationPgid | type == "number")' \
    "$supervisor_state" >/dev/null ||
    die "durable supervisor does not own the active mutation phase"
}

touch_heartbeat() {
  mkdir -p "$(dirname "$heartbeat")"
  touch "$heartbeat"
}

run_with_progress() {
  local label="$1"
  shift
  local log="$command_log_dir/$label.log"
  local pid previous_size=-1 size rc
  : >"$log"
  # Keep every mutation descendant inside the durable supervisor's process
  # group. A nested session would escape quiesce-before-cleanup ordering.
  "$@" >"$log" 2>&1 &
  pid=$!
  printf '%s\n' "$pid" >"$active_pid_file"
  touch_heartbeat
  while kill -0 "$pid" 2>/dev/null; do
    size="$(stat -c %s "$log" 2>/dev/null || echo 0)"
    if [ "$size" != "$previous_size" ]; then
      touch_heartbeat
      previous_size="$size"
    fi
    sleep 5
  done
  set +e
  wait "$pid"
  rc=$?
  set -e
  if [ "$(cat "$active_pid_file" 2>/dev/null || true)" = "$pid" ]; then
    rm -f "$active_pid_file"
  fi
  touch_heartbeat
  return "$rc"
}

reset_checks() {
  : >"$checks_file"
}

record_check() {
  local component="$1" provider="$2" name="$3" result="$4" summary="$5"
  if [ -n "$provider" ]; then
    jq -cn \
      --arg component "$component" \
      --arg provider "$provider" \
      --arg name "$name" \
      --arg result "$result" \
      --arg checkedAt "$(utc_now)" \
      --arg summary "$summary" \
      '{name:$name,component:$component,provider:$provider,result:$result,checkedAt:$checkedAt,summary:$summary}' \
      >>"$checks_file"
  else
    jq -cn \
      --arg component "$component" \
      --arg name "$name" \
      --arg result "$result" \
      --arg checkedAt "$(utc_now)" \
      --arg summary "$summary" \
      '{name:$name,component:$component,result:$result,checkedAt:$checkedAt,summary:$summary}' \
      >>"$checks_file"
  fi
}

write_driver_result() {
  local out="$1" status="$2" notes="$3"
  jq -s \
    --arg status "$status" \
    --arg notes "$notes" \
    --arg tofu "$(tofu version | sed -n '1p')" \
    --arg aws "$(aws --version 2>&1 | sed -n '1p')" \
    --arg azure "$(az version --output json 2>/dev/null | jq -r '.["azure-cli"] // "unknown"')" \
    --arg oci "$(oci --version 2>&1 | sed -n '1p')" \
    '{
      status:$status,
      checks: .,
      repairs: [],
      toolVersions:{tofu:$tofu,aws:$aws,azure:$azure,oci:$oci},
      notes:$notes
    }' "$checks_file" >"$out"
}

parse_driver_args() {
  contract_arg=
  out_arg=
  repair=0
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --contract) contract_arg="${2:?missing --contract value}"; shift 2 ;;
      --out) out_arg="${2:?missing --out value}"; shift 2 ;;
      --repair) repair=1; shift ;;
      -h|--help)
        echo "Usage: $(basename "$0") --contract FILE --out FILE"
        exit 0
        ;;
      *) die "unknown argument: $1" ;;
    esac
  done
  [ -n "$contract_arg" ] || die "--contract is required"
  [ -n "$out_arg" ] || die "--out is required"
  [ "$repair" -eq 0 ] || die "repair is forbidden for release certification"
  load_contract "$contract_arg"
  out_arg="$(absolute_path "$out_arg")"
  mkdir -p "$(dirname "$out_arg")"
}

routerd_script() {
  local relative="$1"
  local release_repo
  release_repo="$(jq -er '.releaseRepo' "$run_env_path")"
  release_repo="$(absolute_path "$release_repo")"
  [ "$(git -C "$release_repo" rev-parse HEAD)" = "$artifact_commit" ] ||
    die "releaseRepo HEAD does not equal exact artifact commit"
  [ -x "$release_repo/$relative" ] ||
    die "required exact-RC script is not executable: $release_repo/$relative"
  printf '%s\n' "$release_repo/$relative"
}
