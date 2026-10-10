#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/sam-full-validation.sh --tofu-output tofu-output.json --artifact routerd.tar.gz --evidence-root DIR [options]

Options:
  --ssh-key FILE      Guest/cloud SSH key (required)
  --pve-ssh-key FILE  Exact root PVE SSH key for hypervisor bridge audit
  --pve-known-hosts FILE
                      Pinned known_hosts for the PVE hypervisors
  --scenario NAME     Run only the named scenario; may be repeated. Use --list-scenarios for names
  --resume-status FILE
                      Resume the default ordered suite after a contiguous PASS prefix
  --destroy-cmd CMD   Optional teardown command to run only after every scenario passes
  --list-scenarios    Validate tofu output has required nodes, print scenario list, and exit

Runs the standard full-topology SAM validation sequence against an already
applied OpenTofu environment:
  1. baseline full matrix + legacy + performance + load-balance report
  2. RR failover/rejoin for pve-rr-a and pve-rr-b, with full traffic matrices
  3. leaf failover/rejoin for both leaf nodes at each site, with full matrices
  4. load-balance report rerun

The ordered default suite does not repeat legacy and throughput probes in
every failover phase: baseline and the final load-balance scenario retain
those expensive measurements, while each failover/rejoin retains control and
provider convergence, all directed client/cloud-ingress checks, transfer
observation, and owner-table evidence. A standalone --scenario remains
exhaustive.

If any scenario fails, the script stops and does not run destroy-cmd. Inspect
the live environment and the scenario evidence before retrying or destroying.
USAGE
}

tofu_output=
artifact=
evidence_root=
ssh_key=
pve_ssh_key=
pve_known_hosts=
resume_status=
destroy_cmd=
list_scenarios=0
selected_scenarios=()
scenario_filter=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tofu-output) tofu_output="$2"; shift 2 ;;
    --artifact) artifact="$2"; shift 2 ;;
    --evidence-root) evidence_root="$2"; shift 2 ;;
    --ssh-key) ssh_key="$2"; shift 2 ;;
    --pve-ssh-key) pve_ssh_key="$2"; shift 2 ;;
    --pve-known-hosts) pve_known_hosts="$2"; shift 2 ;;
    --scenario) selected_scenarios+=("$2"); scenario_filter=1; shift 2 ;;
    --resume-status) resume_status="$2"; shift 2 ;;
    --destroy-cmd) destroy_cmd="$2"; shift 2 ;;
    --list-scenarios) list_scenarios=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$tofu_output" ] || { echo "--tofu-output is required" >&2; exit 2; }
[ -n "$evidence_root" ] || { echo "--evidence-root is required" >&2; exit 2; }
[ -f "$tofu_output" ] || { echo "tofu output not found: $tofu_output" >&2; exit 2; }
[ -n "$ssh_key" ] || { echo "--ssh-key FILE is required" >&2; exit 2; }
[ -f "$ssh_key" ] || { echo "ssh key not found: $ssh_key" >&2; exit 2; }
[ -f "$pve_ssh_key" ] || { echo "--pve-ssh-key FILE is required" >&2; exit 2; }
[ -f "$pve_known_hosts" ] || { echo "--pve-known-hosts FILE is required" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
e2e_script="$script_dir/sam-e2e.sh"
summary_script="$script_dir/sam-e2e-summary.sh"
post_destroy_script="$script_dir/sam-post-destroy-inventory.sh"

mkdir -p "$evidence_root"

nodes_json="$evidence_root/nodes.json"
fabric_json="$evidence_root/fabric.json"
jq '.nodes.value' "$tofu_output" >"$nodes_json"
jq '.fabric.value' "$tofu_output" >"$fabric_json"

topology_scale="$(jq -r '.topology_scale // empty' "$fabric_json")"
if [ "$topology_scale" != "full" ]; then
  echo "sam-full-validation.sh requires fabric.topology_scale=full; got: ${topology_scale:-<empty>}" >&2
  echo "Use sam-e2e.sh directly for single-topology smoke tests." >&2
  exit 2
fi

require_node() {
  local node="$1"
  jq -e --arg node "$node" 'has($node)' "$nodes_json" >/dev/null || {
    echo "required node missing from tofu output: $node" >&2
    return 1
  }
}

for node in pve-rr-a pve-rr-b aws-leaf-a aws-leaf-b azure-leaf-a azure-leaf-b oci-leaf-a oci-leaf-b pve-leaf-a pve-leaf-b; do
  require_node "$node"
done

scenario_names=(
  baseline
  rr-failover-pve-rr-a
  rr-failover-pve-rr-b
  leaf-failover-aws-leaf-a
  leaf-failover-aws-leaf-b
  leaf-failover-azure-leaf-a
  leaf-failover-azure-leaf-b
  leaf-failover-oci-leaf-a
  leaf-failover-oci-leaf-b
  leaf-failover-pve-leaf-a
  leaf-failover-pve-leaf-b
  load-balance
)

if [ "$list_scenarios" -eq 1 ]; then
  printf '%s\n' "${scenario_names[@]}"
  exit 0
fi

if [ -n "$destroy_cmd" ] && [ "$scenario_filter" -eq 1 ]; then
  echo "--destroy-cmd is only allowed when running the full default scenario set" >&2
  exit 2
fi
if [ -n "$resume_status" ] && [ "$scenario_filter" -eq 1 ]; then
  echo "--resume-status cannot be combined with --scenario" >&2
  exit 2
fi

scenario_exists() {
  local want="$1" scenario
  for scenario in "${scenario_names[@]}"; do
    [ "$scenario" = "$want" ] && return 0
  done
  return 1
}

for scenario in "${selected_scenarios[@]}"; do
  scenario_exists "$scenario" || {
    echo "unknown scenario: $scenario" >&2
    echo "valid scenarios:" >&2
    printf '  %s\n' "${scenario_names[@]}" >&2
    exit 2
  }
done

# A PASS prefix is reusable only with the same payload, topology, harness and
# intact scenario outputs. This metadata records an acquired exit-0 result; it
# does not reinterpret diagnostic summaries as acceptance.
if [ -z "$artifact" ] || [ ! -f "$artifact" ]; then
  echo "--artifact FILE is required" >&2
  exit 2
fi
command -v python3 >/dev/null || { echo "python3 is required for resume provenance" >&2; exit 2; }

scenario_provenance() {
  local mode="$1" name="$2" dir="$3"
  python3 - "$mode" "$name" "$dir" "$artifact" "$tofu_output" "$e2e_script" "$script_dir/sam-full-validation.sh" <<'PYRESUME'
import hashlib, json, pathlib, sys
mode, name, directory, *inputs = sys.argv[1:]
root = pathlib.Path(directory)
marker = root / "scenario-provenance.json"
def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()
try:
    identity = dict(zip(("artifact", "topology", "e2eHarness", "suiteHarness"),
                        (digest(pathlib.Path(p)) for p in inputs)))
    files = {}
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise ValueError("symlink evidence cannot bind scenario outputs")
        if path.is_file() and path != marker:
            files[str(path.relative_to(root))] = digest(path)
    if not all(p in files for p in ("sam-e2e.log", "summary.txt")):
        raise ValueError("required scenario outputs missing")
    record = {"scenario": name, "result": "PASS", "inputs": identity, "files": files}
    if mode == "seal":
        if marker.exists():
            raise ValueError("existing scenario provenance cannot be overwritten")
        with marker.open("x") as out:
            json.dump(record, out, sort_keys=True, indent=2)
            out.write("\n")
    elif mode == "verify":
        if json.loads(marker.read_text()) != record:
            raise ValueError("stale inputs or changed/missing scenario outputs")
    else:
        raise ValueError("unknown provenance operation")
except (OSError, ValueError, TypeError) as exc:
    print("resume observation is not bound: " + str(exc), file=sys.stderr)
    raise SystemExit(3)
PYRESUME
}

resume_count=0
if [ -n "$resume_status" ]; then
  [ -f "$resume_status" ] || { echo "resume status not found: $resume_status" >&2; exit 2; }
  [ "$(head -n 1 "$resume_status")" = $'scenario\tstatus\tevidence_dir' ] || {
    echo "resume status has an invalid header: $resume_status" >&2
    exit 2
  }
  while IFS=$'\t' read -r name status dir; do
    [ -n "$name" ] || continue
    [ "$resume_count" -lt "${#scenario_names[@]}" ] || {
      echo "resume status contains too many scenarios" >&2
      exit 2
    }
    [ "$name" = "${scenario_names[$resume_count]}" ] || {
      echo "resume status is not a contiguous ordered prefix at: $name" >&2
      exit 2
    }
    [ "$status" = PASS ] || {
      echo "resume status contains non-PASS scenario: $name $status" >&2
      exit 2
    }
    [ -d "$dir" ] || {
      echo "resume evidence directory is missing: $dir" >&2
      exit 2
    }
    scenario_provenance verify "$name" "$dir" || exit 3
    resume_count=$((resume_count + 1))
  done < <(tail -n +2 "$resume_status")
  selected_scenarios=("${scenario_names[@]:$resume_count}")
elif [ "${#selected_scenarios[@]}" -eq 0 ]; then
  selected_scenarios=("${scenario_names[@]}")
fi

failover_suite_args=(--performance-tests)
if [ "$scenario_filter" -eq 0 ]; then
  failover_suite_args=(
    --skip-initial-validation
    --skip-legacy-protocols
    --success-evidence-minimal
  )
fi

[ -n "$artifact" ] || { echo "--artifact is required" >&2; exit 2; }
[ -f "$artifact" ] || { echo "artifact not found: $artifact" >&2; exit 2; }
[ -f "$ssh_key" ] || { echo "ssh key not found: $ssh_key" >&2; exit 2; }

run_scenario() {
  local name="$1"; shift
  local dir="$evidence_root/$name"
  local rc=0
  if [ -e "$dir" ]; then
    echo "scenario evidence already exists; refusing overwrite: $dir" >&2
    return 3
  fi
  mkdir -p "$dir"
  echo "== scenario $name =="
  set +e
  "$e2e_script" \
    --tofu-output "$tofu_output" \
    --artifact "$artifact" \
    --ssh-key "$ssh_key" \
    --pve-ssh-key "$pve_ssh_key" \
    --pve-known-hosts "$pve_known_hosts" \
    --evidence-dir "$dir" \
    "$@" 2>&1 | tee "$dir/sam-e2e.log"
  rc=${PIPESTATUS[0]}
  set -e
  "$summary_script" "$dir" >"$dir/summary.txt"
  sed -n '1,160p' "$dir/summary.txt"
  if [ "$rc" -eq 0 ]; then
    scenario_provenance seal "$name" "$dir" || return 3
    printf '%s\tPASS\t%s\n' "$name" "$dir" >>"$evidence_root/scenario-status.tsv"
  else
    printf '%s\tFAIL\t%s\n' "$name" "$dir" >>"$evidence_root/scenario-status.tsv"
  fi
  return "$rc"
}

write_overall_summary() {
  {
    echo "evidence_root=$evidence_root"
    echo "== scenario status =="
    if [ -f "$evidence_root/scenario-status.tsv" ]; then
      column -t -s $'\t' "$evidence_root/scenario-status.tsv" 2>/dev/null || cat "$evidence_root/scenario-status.tsv"
    fi
    echo "== scenario summaries =="
    if [ -f "$evidence_root/scenario-status.tsv" ]; then
      tail -n +2 "$evidence_root/scenario-status.tsv" | while IFS=$'\t' read -r name status dir; do
        echo "## $name $status"
        if [ -f "$dir/summary.txt" ]; then
          sed -n '1,120p' "$dir/summary.txt"
        else
          echo "summary missing: $dir/summary.txt"
        fi
      done
    fi
  } >"$evidence_root/overall-summary.txt"
}

run_named_scenario() {
  local scenario="$1"
  case "$scenario" in
    baseline)
      run_scenario baseline \
        --load-balance-report \
        --performance-tests
      ;;
    rr-failover-pve-rr-a)
      run_scenario rr-failover-pve-rr-a \
        --skip-deploy \
        --failover-node pve-rr-a \
        --rejoin-after-failover \
        --load-balance-report \
        "${failover_suite_args[@]}" \
        --failover-transfer-observe
      ;;
    rr-failover-pve-rr-b)
      run_scenario rr-failover-pve-rr-b \
        --skip-deploy \
        --failover-node pve-rr-b \
        --rejoin-after-failover \
        --load-balance-report \
        "${failover_suite_args[@]}" \
        --failover-transfer-observe
      ;;
    leaf-failover-aws-leaf-a)
      run_scenario leaf-failover-aws-leaf-a \
        --skip-deploy \
        --failover-node aws-leaf-a \
        --rejoin-after-failover \
        --load-balance-report \
        "${failover_suite_args[@]}" \
        --failover-transfer-observe
      ;;
    leaf-failover-aws-leaf-b)
      run_scenario leaf-failover-aws-leaf-b \
        --skip-deploy \
        --failover-node aws-leaf-b \
        --rejoin-after-failover \
        --load-balance-report \
        "${failover_suite_args[@]}" \
        --failover-transfer-observe
      ;;
    leaf-failover-azure-leaf-a)
      run_scenario leaf-failover-azure-leaf-a \
        --skip-deploy \
        --failover-node azure-leaf-a \
        --rejoin-after-failover \
        --load-balance-report \
        "${failover_suite_args[@]}" \
        --failover-transfer-observe
      ;;
    leaf-failover-azure-leaf-b)
      run_scenario leaf-failover-azure-leaf-b \
        --skip-deploy \
        --failover-node azure-leaf-b \
        --rejoin-after-failover \
        --load-balance-report \
        "${failover_suite_args[@]}" \
        --failover-transfer-observe
      ;;
    leaf-failover-oci-leaf-a)
      run_scenario leaf-failover-oci-leaf-a \
        --skip-deploy \
        --failover-node oci-leaf-a \
        --rejoin-after-failover \
        --load-balance-report \
        "${failover_suite_args[@]}" \
        --failover-transfer-observe
      ;;
    leaf-failover-oci-leaf-b)
      run_scenario leaf-failover-oci-leaf-b \
        --skip-deploy \
        --failover-node oci-leaf-b \
        --rejoin-after-failover \
        --load-balance-report \
        "${failover_suite_args[@]}" \
        --failover-transfer-observe
      ;;
    leaf-failover-pve-leaf-a)
      run_scenario leaf-failover-pve-leaf-a \
        --skip-deploy \
        --failover-node pve-leaf-a \
        --rejoin-after-failover \
        --load-balance-report \
        "${failover_suite_args[@]}" \
        --failover-transfer-observe
      ;;
    leaf-failover-pve-leaf-b)
      run_scenario leaf-failover-pve-leaf-b \
        --skip-deploy \
        --failover-node pve-leaf-b \
        --rejoin-after-failover \
        --load-balance-report \
        "${failover_suite_args[@]}" \
        --failover-transfer-observe
      ;;
    load-balance)
      run_scenario load-balance \
        --skip-deploy \
        --load-balance-report \
        --skip-legacy-protocols \
        --performance-tests
      ;;
    *)
      echo "unhandled scenario: $scenario" >&2
      return 2
      ;;
  esac
}

{
  date -u '+timestamp=%Y-%m-%dT%H:%M:%SZ'
  echo "tofu_output=$tofu_output"
  echo "artifact=$artifact"
  sha256sum "$artifact"
  echo "ssh_key=$ssh_key"
  ssh-keygen -lf "${ssh_key}.pub" 2>/dev/null || ssh-keygen -y -f "$ssh_key" | ssh-keygen -lf -
  echo "destroy_cmd=${destroy_cmd:-}"
  echo "resume_status=${resume_status:-}"
  echo "resume_count=$resume_count"
  printf 'selected_scenarios=%s\n' "$(IFS=,; echo "${selected_scenarios[*]}")"
  echo "policy_read=Read ~/routerd-orchestration.md and cloudedge-mobility/LAB_POLICY.md before running this on real machines."
} >"$evidence_root/full-validation-note.txt"

if [ -n "$resume_status" ]; then
  cp "$resume_status" "$evidence_root/scenario-status.tsv"
else
  printf 'scenario\tstatus\tevidence_dir\n' >"$evidence_root/scenario-status.tsv"
fi
trap write_overall_summary EXIT

for scenario in "${selected_scenarios[@]}"; do
  run_named_scenario "$scenario"
done

if [ -n "$destroy_cmd" ]; then
  echo "== destroy =="
  bash -lc "$destroy_cmd" >"$evidence_root/destroy.log" 2>&1
  "$post_destroy_script" --tofu-output "$tofu_output" --evidence-dir "$evidence_root/post-destroy" >"$evidence_root/post-destroy-summary.txt"
  "$summary_script" "$evidence_root/load-balance" >"$evidence_root/final-summary.txt"
fi

echo "full validation evidence: $evidence_root"
