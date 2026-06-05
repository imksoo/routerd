#!/usr/bin/env bash
# Four-point pcap harness for CloudEdge SAM data-plane evidence.

set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/runners/cloudedge-runner-lib.sh
. "$SCRIPT_DIR/cloudedge-runner-lib.sh"

usage() {
  cat <<EOF
$SELF - CloudEdge four-point pcap runner

USAGE:
  $SELF start --test-id TEST_ID --out BUNDLE_DIR --source-site SITE --remote-site SITE --router-provider PROVIDER --target-ip IP [--ports PORTS]
  $SELF stop  --test-id TEST_ID --out BUNDLE_DIR --source-site SITE --remote-site SITE --router-provider PROVIDER --target-ip IP [--ports PORTS]

OUTPUT:
  BUNDLE_DIR/05-capture/TEST_ID-node-role-iface.pcap
  BUNDLE_DIR/05-capture/TEST_ID-capture-manifest.json
  BUNDLE_DIR/05-capture/capture-manifest.json

ENV:
  CE_CAPTURE_SOURCE_IFACE                 Source endpoint iface (default any).
  CE_CAPTURE_ROUTER_INSIDE_IFACE          Router inside/LAN iface (default br0).
  CE_CAPTURE_ROUTER_OUTSIDE_TUNNEL_IFACE  Router outside/tunnel iface (default wg-hybrid).
  CE_CAPTURE_REMOTE_IFACE                 Remote endpoint iface (default any).
  CE_CAPTURE_ROUTER_ROLE                  Router role for ce_router_ssh (default active).
  CE_CAPTURE_REMOTE_DIR                   Remote pcap dir prefix (default /tmp/routerd-cloudedge-capture).
  CE_CAPTURE_START_COMMAND                Optional local fake/override for one point start.
  CE_CAPTURE_STOP_COMMAND                 Optional local fake/override for one point stop.
  CE_CAPTURE_COPY_COMMAND                 Optional local fake/override to write CE_CAPTURE_PATH.
  CE_CAPTURE_<ROLE>_<START|STOP|COPY>_COMMAND
                                           Role-specific override, role uppercased with '-' as '_'.

The live implementation targets routerd Linux nodes through ce_client_ssh and
ce_router_ssh. Cisco EPC is intentionally out of scope for this runner.
EOF
}

op=""
test_id=""
out=""
source_site=""
remote_site=""
router_provider=""
target_ip=""
ports=""
router_role=${CE_CAPTURE_ROUTER_ROLE:-active}

while [[ $# -gt 0 ]]; do
  case "$1" in
    start|stop)
      op=$1
      shift
      ;;
    --test-id)
      test_id=${2:-}
      shift 2
      ;;
    --out)
      out=${2:-}
      shift 2
      ;;
    --source-site)
      source_site=${2:-}
      shift 2
      ;;
    --remote-site)
      remote_site=${2:-}
      shift 2
      ;;
    --router-provider)
      router_provider=${2:-}
      shift 2
      ;;
    --router-role)
      router_role=${2:-}
      shift 2
      ;;
    --target-ip)
      target_ip=${2:-}
      shift 2
      ;;
    --ports)
      ports=${2:-}
      shift 2
      ;;
    -h|--help|help)
      usage
      exit 0
      ;;
    *)
      ce_die "unknown argument: $1"
      ;;
  esac
done

[[ -n "$op" ]] || { usage; exit 0; }
[[ -n "$test_id" ]] || ce_die "--test-id is required"
[[ "$test_id" =~ ^[A-Za-z0-9_.:-]+$ ]] || ce_die "bad --test-id: $test_id"
[[ -n "$out" ]] || ce_die "--out is required"
[[ -n "$source_site" ]] || ce_die "--source-site is required"
[[ -n "$remote_site" ]] || ce_die "--remote-site is required"
[[ -n "$router_provider" ]] || ce_die "--router-provider is required"
[[ "$target_ip" =~ ^[0-9A-Fa-f:.]+$ ]] || ce_die "--target-ip must be an IPv4/IPv6 address"
if [[ -n "$ports" && ! "$ports" =~ ^[0-9,]+$ ]]; then
  ce_die "--ports must be a comma-separated list of numeric TCP/UDP ports"
fi

capture_dir() {
  local root=$1
  if [[ "$(basename "$root")" == "05-capture" ]]; then
    printf '%s' "$root"
  else
    printf '%s/05-capture' "$root"
  fi
}

sanitize_token() {
  printf '%s' "$1" | tr -c 'A-Za-z0-9_.-' '_'
}

build_filter() {
  local expr="host $target_ip or arp or icmp"
  local port
  if [[ -n "$ports" ]]; then
    IFS=',' read -ra port_list <<<"$ports"
    for port in "${port_list[@]}"; do
      [[ -n "$port" ]] || continue
      expr="$expr or port $port"
    done
  fi
  printf '(%s)' "$expr"
}

point_host() {
  local role=$1 node=$2 host=""
  case "$role" in
    source|remote)
      host=$(ce_client_host "$node" 2>/dev/null || true)
      ;;
    router-inside|router-outside-tunnel)
      host=$(ce_router_host "$node" "$router_role" 2>/dev/null || true)
      ;;
  esac
  printf '%s' "${host:-unresolved:$node}"
}

has_override() {
  local role=$1 phase=$2 role_upper phase_upper
  role_upper=$(ce_upper "$role")
  phase_upper=$(ce_upper "$phase")
  ce_env_first "CE_CAPTURE_${role_upper}_${phase_upper}_COMMAND" "CE_CAPTURE_${phase_upper}_COMMAND" >/dev/null 2>&1
}

run_override() {
  local role=$1 phase=$2 node=$3 host=$4 iface=$5 filename=$6 path=$7 remote_path=$8 pid_path=$9 filter=${10} command=${11}
  local role_upper phase_upper override
  role_upper=$(ce_upper "$role")
  phase_upper=$(ce_upper "$phase")
  override=$(ce_env_first "CE_CAPTURE_${role_upper}_${phase_upper}_COMMAND" "CE_CAPTURE_${phase_upper}_COMMAND" 2>/dev/null || true)
  [[ -n "$override" ]] || return 1
  CE_CAPTURE_PHASE=$phase \
  CE_CAPTURE_TEST_ID=$test_id \
  CE_CAPTURE_ROLE=$role \
  CE_CAPTURE_NODE=$node \
  CE_CAPTURE_HOST=$host \
  CE_CAPTURE_IFACE=$iface \
  CE_CAPTURE_FILENAME=$filename \
  CE_CAPTURE_PATH=$path \
  CE_CAPTURE_REMOTE_PATH=$remote_path \
  CE_CAPTURE_PID_PATH=$pid_path \
  CE_CAPTURE_FILTER=$filter \
  CE_CAPTURE_COMMAND=$command \
    bash -lc "$override"
}

ssh_for_point() {
  local role=$1 node=$2 command=$3
  case "$role" in
    source|remote)
      ce_client_ssh "$node" "$command"
      ;;
    router-inside|router-outside-tunnel)
      ce_router_ssh "$node" "$router_role" "$command"
      ;;
    *)
      ce_die "bad capture role: $role"
      ;;
  esac
}

start_command() {
  local iface=$1 remote_dir=$2 remote_path=$3 pid_path=$4 log_path=$5 filter=$6 inner
  inner="nohup tcpdump -U -i $(printf '%q' "$iface") -w $(printf '%q' "$remote_path") $filter >$(printf '%q' "$log_path") 2>&1 & echo \$! > $(printf '%q' "$pid_path")"
  printf 'sudo mkdir -p %q && command -v tcpdump >/dev/null 2>&1 && sudo sh -c %q' "$remote_dir" "$inner"
}

stop_command() {
  local remote_path=$1 pid_path=$2
  # The remote shell must expand pid after it reads the tcpdump pid file.
  # shellcheck disable=SC2016
  printf 'if [ -f %q ]; then pid=$(sudo cat %q 2>/dev/null || cat %q); sudo kill -INT "$pid" 2>/dev/null || kill -INT "$pid" 2>/dev/null || true; sleep 1; fi; sudo test -s %q' "$pid_path" "$pid_path" "$pid_path" "$remote_path"
}

copy_command() {
  local remote_path=$1
  printf 'sudo cat %q' "$remote_path"
}

append_json_line() {
  local jsonl=$1 phase=$2 role=$3 node=$4 host=$5 iface=$6 filename=$7 path=$8 remote_path=$9 pid_path=${10} filter=${11} command=${12} exit_status=${13} reason=${14} at=${15}
  python3 - "$jsonl" <<'PY'
import json
import os
import sys

path = sys.argv[1]
record = {
    "role": os.environ["ROLE"],
    "node": os.environ["NODE"],
    "host": os.environ["HOST"],
    "interface": os.environ["IFACE"],
    "filename": os.environ["FILENAME"],
    "path": os.environ["LOCAL_PATH"],
    "remotePath": os.environ["REMOTE_PATH"],
    "pidPath": os.environ["PID_PATH"],
    "filter": os.environ["FILTER"],
    f"{os.environ['PHASE']}Command": os.environ["COMMAND"],
    f"{os.environ['PHASE']}Exit": int(os.environ["EXIT_STATUS"]),
    f"{os.environ['PHASE']}At": os.environ["AT"],
}
if os.environ.get("REASON"):
    record[f"{os.environ['PHASE']}Reason"] = os.environ["REASON"]
with open(path, "a", encoding="utf-8") as f:
    f.write(json.dumps(record, sort_keys=True) + "\n")
PY
}

merge_stop_line() {
  local out_jsonl=$1 old_json=$2 phase=$3 command=$4 exit_status=$5 reason=$6 at=$7
  python3 - "$out_jsonl" "$old_json" <<'PY'
import json
import os
import sys

out, old = sys.argv[1:]
record = json.loads(old)
phase = os.environ["PHASE"]
record[f"{phase}Command"] = os.environ["COMMAND"]
record[f"{phase}Exit"] = int(os.environ["EXIT_STATUS"])
record[f"{phase}At"] = os.environ["AT"]
if os.environ.get("REASON"):
    record[f"{phase}Reason"] = os.environ["REASON"]
with open(out, "a", encoding="utf-8") as f:
    f.write(json.dumps(record, sort_keys=True) + "\n")
PY
}

write_manifest() {
  local jsonl=$1
  local per_test=$cap_dir/$test_id-capture-manifest.json aggregate=$cap_dir/capture-manifest.json
  python3 - "$jsonl" "$per_test" "$aggregate" <<'PY'
import json
import os
import sys
from pathlib import Path

jsonl, per_test, aggregate = map(Path, sys.argv[1:])
points = []
if jsonl.exists():
    points = [json.loads(line) for line in jsonl.read_text(encoding="utf-8").splitlines() if line.strip()]
run = {
    "testId": os.environ["TEST_ID"],
    "phase": "CAP",
    "evidencePhases": ["CAP", "DP"],
    "result": os.environ["RESULT"],
    "reason": os.environ.get("REASON", ""),
    "startedAt": os.environ["STARTED_AT"],
    "stoppedAt": os.environ.get("STOPPED_AT", ""),
    "captureDir": str(per_test.parent),
    "points": points,
}
per_test.write_text(json.dumps(run, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if aggregate.exists():
    try:
        manifest = json.loads(aggregate.read_text(encoding="utf-8"))
    except Exception:
        manifest = {"runs": []}
else:
    manifest = {"runs": []}
manifest["runs"] = [r for r in manifest.get("runs", []) if r.get("testId") != run["testId"]]
manifest["runs"].append(run)
aggregate.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")
PY
}

point_specs() {
  printf 'source\t%s\t%s\n' "$source_site" "${CE_CAPTURE_SOURCE_IFACE:-any}"
  printf 'router-inside\t%s\t%s\n' "$router_provider" "${CE_CAPTURE_ROUTER_INSIDE_IFACE:-br0}"
  printf 'router-outside-tunnel\t%s\t%s\n' "$router_provider" "${CE_CAPTURE_ROUTER_OUTSIDE_TUNNEL_IFACE:-wg-hybrid}"
  printf 'remote\t%s\t%s\n' "$remote_site" "${CE_CAPTURE_REMOTE_IFACE:-any}"
}

run_start() {
  local jsonl=$state_jsonl filter started_at failures=0 reasons=()
  local role node iface host node_s role_s iface_s filename local_path remote_dir remote_path pid_path log_path command exit_status reason at
  filter=$(build_filter)
  started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  : >"$jsonl"
  while IFS=$'\t' read -r role node iface; do
    host=$(point_host "$role" "$node")
    node_s=$(sanitize_token "$node")
    role_s=$(sanitize_token "$role")
    iface_s=$(sanitize_token "$iface")
    filename="$test_id-$node_s-$role_s-$iface_s.pcap"
    local_path="$cap_dir/$filename"
    remote_dir="${CE_CAPTURE_REMOTE_DIR:-/tmp/routerd-cloudedge-capture}/$test_id"
    remote_path="$remote_dir/$filename"
    pid_path="$remote_dir/$filename.pid"
    log_path="$remote_dir/$filename.log"
    command=$(start_command "$iface" "$remote_dir" "$remote_path" "$pid_path" "$log_path" "$filter")
    at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    reason=""
    exit_status=0
    if has_override "$role" start; then
      run_override "$role" start "$node" "$host" "$iface" "$filename" "$local_path" "$remote_path" "$pid_path" "$filter" "$command" || exit_status=$?
    else
      ssh_for_point "$role" "$node" "$command" || exit_status=$?
    fi
    if [[ "$exit_status" -ne 0 ]]; then
      reason="start_failed"
      reasons+=("$role:$reason")
      failures=$((failures + 1))
    fi
    ROLE=$role NODE=$node HOST=$host IFACE=$iface FILENAME=$filename LOCAL_PATH=$local_path \
      REMOTE_PATH=$remote_path PID_PATH=$pid_path FILTER=$filter PHASE=start COMMAND=$command \
      EXIT_STATUS=$exit_status REASON=$reason AT=$at \
      append_json_line "$jsonl" start "$role" "$node" "$host" "$iface" "$filename" "$local_path" "$remote_path" "$pid_path" "$filter" "$command" "$exit_status" "$reason" "$at"
  done < <(point_specs)
  if [[ "$failures" -gt 0 ]]; then
    TEST_ID=$test_id RESULT=PARTIAL REASON="capture start partial: ${reasons[*]}" STARTED_AT=$started_at STOPPED_AT="" \
      write_manifest "$jsonl" PARTIAL "capture start partial: ${reasons[*]}" "$started_at" ""
    printf 'result=PARTIAL\nreason=%s\nmanifest=%s\n' "capture start partial: ${reasons[*]}" "$cap_dir/$test_id-capture-manifest.json"
  else
    TEST_ID=$test_id RESULT=PASS REASON="" STARTED_AT=$started_at STOPPED_AT="" \
      write_manifest "$jsonl" PASS "" "$started_at" ""
    printf 'result=PASS\nmanifest=%s\n' "$cap_dir/$test_id-capture-manifest.json"
  fi
}

run_stop() {
  local jsonl=$state_jsonl stop_jsonl=$cap_dir/$test_id-capture-stop.jsonl stopped_at failures=0 reasons=()
  local old role node host iface filename local_path remote_path pid_path filter command exit_status reason at copy_cmd copy_exit
  [[ -f "$jsonl" ]] || ce_die "capture state not found: $jsonl"
  : >"$stop_jsonl"
  stopped_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  while IFS= read -r old; do
    [[ -n "$old" ]] || continue
    IFS=$'\t' read -r role node host iface filename local_path remote_path pid_path filter < <(
      python3 - "$old" <<'PY'
import json
import sys
r = json.loads(sys.argv[1])
print("\t".join(str(r.get(k, "")) for k in ("role", "node", "host", "interface", "filename", "path", "remotePath", "pidPath", "filter")))
PY
    )
    command=$(stop_command "$remote_path" "$pid_path")
    at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    reason=""
    exit_status=0
    if ! python3 - "$old" <<'PY'
import json
import sys
raise SystemExit(0 if json.loads(sys.argv[1]).get("startExit") == 0 else 1)
PY
    then
      exit_status=99
      reason="start_failed_skip_stop"
    elif has_override "$role" stop; then
      run_override "$role" stop "$node" "$host" "$iface" "$filename" "$local_path" "$remote_path" "$pid_path" "$filter" "$command" || exit_status=$?
    else
      ssh_for_point "$role" "$node" "$command" || exit_status=$?
    fi
    if [[ "$exit_status" -ne 0 && -z "$reason" ]]; then
      reason="stop_failed"
    fi
    PHASE=stop COMMAND=$command EXIT_STATUS=$exit_status REASON=$reason AT=$at \
      merge_stop_line "$stop_jsonl" "$old" stop "$command" "$exit_status" "$reason" "$at"

    old=$(tail -n1 "$stop_jsonl")
    copy_cmd=$(copy_command "$remote_path")
    copy_exit=0
    reason=""
    if [[ "$exit_status" -ne 0 ]]; then
      copy_exit=99
      reason="stop_failed_skip_copy"
    elif has_override "$role" copy; then
      run_override "$role" copy "$node" "$host" "$iface" "$filename" "$local_path" "$remote_path" "$pid_path" "$filter" "$copy_cmd" || copy_exit=$?
    else
      ssh_for_point "$role" "$node" "$copy_cmd" >"$local_path" || copy_exit=$?
    fi
    if [[ "$copy_exit" -ne 0 && -z "$reason" ]]; then
      reason="copy_failed"
    fi
    if [[ "$copy_exit" -eq 0 && ! -s "$local_path" ]]; then
      copy_exit=98
      reason="empty_pcap"
    fi
    if [[ "$exit_status" -ne 0 || "$copy_exit" -ne 0 ]]; then
      reasons+=("$role:${reason:-capture_failed}")
      failures=$((failures + 1))
    fi
    tmp_jsonl=$cap_dir/$test_id-capture-stop.tmp
    head -n -1 "$stop_jsonl" >"$tmp_jsonl" || true
    mv "$tmp_jsonl" "$stop_jsonl"
    PHASE=copy COMMAND=$copy_cmd EXIT_STATUS=$copy_exit REASON=$reason AT=$stopped_at \
      merge_stop_line "$stop_jsonl" "$old" copy "$copy_cmd" "$copy_exit" "$reason" "$stopped_at"
  done <"$jsonl"
  mv "$stop_jsonl" "$jsonl"
  started_at=$(python3 - "$jsonl" <<'PY'
import json
import sys
for line in open(sys.argv[1], encoding="utf-8"):
    if line.strip():
        print(json.loads(line).get("startAt", ""))
        break
PY
)
  if [[ "$failures" -gt 0 ]]; then
    TEST_ID=$test_id RESULT=PARTIAL REASON="capture partial: ${reasons[*]}" STARTED_AT=${started_at:-} STOPPED_AT=$stopped_at \
      write_manifest "$jsonl" PARTIAL "capture partial: ${reasons[*]}" "${started_at:-}" "$stopped_at"
    printf 'result=PARTIAL\nreason=%s\nmanifest=%s\n' "capture partial: ${reasons[*]}" "$cap_dir/$test_id-capture-manifest.json"
  else
    TEST_ID=$test_id RESULT=PASS REASON="" STARTED_AT=${started_at:-} STOPPED_AT=$stopped_at \
      write_manifest "$jsonl" PASS "" "${started_at:-}" "$stopped_at"
    printf 'result=PASS\nmanifest=%s\n' "$cap_dir/$test_id-capture-manifest.json"
  fi
}

cap_dir=$(capture_dir "$out")
mkdir -p "$cap_dir"
state_jsonl=$cap_dir/$test_id-capture-state.jsonl

case "$op" in
  start) run_start ;;
  stop) run_stop ;;
  *) ce_die "unknown op: $op" ;;
esac
