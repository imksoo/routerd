#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
harness="$script_dir/sam-e2e.sh"
work="$(mktemp -d "${TMPDIR:-/tmp}/routerd-sam-reuse-topology.XXXXXX")"
cleanup() { find "$work" -depth -delete; }
trap cleanup EXIT

# Extract the actual top-level functions without executing the harness main.
# Explicit next-function boundaries preserve the validation function's nested
# remote-script heredoc (which itself contains shell function definitions).
extract_function() {
  awk -v start="$1() {" -v stop="$2() {" '
    $0 == stop { exit }
    $0 == start { printing = 1 }
    printing { print }
  ' "$harness"
}
eval "$(extract_function setup_pve_dataplane warm_onprem_discovery)"
eval "$(extract_function validate_generated_configs deploy)"
eval "$(extract_function setup_client_ssh setup_legacy_protocol_services)"

for function_name in setup_pve_dataplane validate_generated_configs setup_client_ssh; do
  for mode in 1 0; do
    test_dir="$work/$function_name-$mode"
    mkdir -p "$test_dir/ssh" "$test_dir/preflight"
    rc=0
    # These globals and doubles are consumed by the evaluated real functions.
    # shellcheck disable=SC2034,SC2317
    (
      reuse_deployed_topology="$mode"
      evidence_dir="$test_dir"
      pve_leaf_routers=(fixture-leaf)
      pve_clients=()
      pve_capture_interface=fixture0
      pve_client_capture_interface=fixture0
      clients=(fixture-client)
      nodes_json="$work/unused-nodes.json"
      io_log="$test_dir/io.log"
      fail_io() { printf '%s\n' "$1" >>"$io_log"; exit 71; }
      node_field() { printf '%s\n' 192.0.2.1; }
      ssh_node() { fail_io ssh; }
      scp_node() { fail_io scp; }
      scp_from_node() { fail_io scp; }
      mkdir() { fail_io mkdir; }
      chmod() { fail_io chmod; }
      jq() { fail_io jq; }
      tar() { fail_io tar; }
      "$function_name" "$work/unused-configs"
    ) >"$test_dir/stdout" 2>"$test_dir/stderr" || rc=$?
    if [ "$mode" -eq 1 ]; then
      if [ "$rc" -ne 0 ] || [ -e "$test_dir/io.log" ] || [ -e "$test_dir/ssh/client_known_hosts" ]; then
        echo "reuse mode entered setup I/O: $function_name (exit=$rc)" >&2
        exit 1
      fi
    elif [ "$rc" -ne 71 ] || [ ! -s "$test_dir/io.log" ]; then
      echo "default mode no longer performs its setup: $function_name (exit=$rc)" >&2
      exit 1
    fi
  done
done

mkdir -p "$work/configs"
assert_argument_error() {
  local name="$1" expected="$2"; shift 2
  local rc=0
  "$harness" "$@" >"$work/$name.stdout" 2>"$work/$name.stderr" || rc=$?
  [ "$rc" -eq 2 ] || { echo "unexpected argument-validation exit: $name=$rc" >&2; exit 1; }
  grep -F -- "$expected" "$work/$name.stderr" >/dev/null
}
assert_argument_error no-skip '--reuse-deployed-topology requires --skip-deploy' \
  --reuse-deployed-topology --configs-dir "$work/configs"
assert_argument_error no-config '--reuse-deployed-topology requires an existing --configs-dir' \
  --reuse-deployed-topology --skip-deploy
assert_argument_error missing-config '--reuse-deployed-topology requires an existing --configs-dir' \
  --reuse-deployed-topology --skip-deploy --configs-dir "$work/missing"
# Valid reuse arguments pass their own guard and reach the normal required-
# input checks, without enough inputs to perform any network operation.
assert_argument_error accepted-reuse '--tofu-output is required' \
  --reuse-deployed-topology --skip-deploy --configs-dir "$work/configs"

echo 'sam-e2e reuse deployed topology offline OK'
