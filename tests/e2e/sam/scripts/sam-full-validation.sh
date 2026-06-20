#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/sam-full-validation.sh --tofu-output tofu-output.json --artifact routerd.tar.gz --evidence-root DIR [options]

Options:
  --ssh-key FILE      Fixed lab SSH key (default: ~/.ssh/routerd-cloudedge-lab-20260529)
  --destroy-cmd CMD   Optional teardown command to run only after every scenario passes
  --list-scenarios    Validate tofu output has required nodes, print scenario list, and exit

Runs the standard full-topology SAM validation sequence against an already
applied OpenTofu environment:
  1. baseline full matrix + legacy + performance + load-balance report
  2. RR failover/rejoin for aws-rr-a and aws-rr-b
  3. leaf failover/rejoin for one leaf per site
  4. load-balance report rerun

If any scenario fails, the script stops and does not run destroy-cmd. Inspect
the live environment and the scenario evidence before retrying or destroying.
USAGE
}

tofu_output=
artifact=
evidence_root=
ssh_key="${HOME}/.ssh/routerd-cloudedge-lab-20260529"
destroy_cmd=
list_scenarios=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tofu-output) tofu_output="$2"; shift 2 ;;
    --artifact) artifact="$2"; shift 2 ;;
    --evidence-root) evidence_root="$2"; shift 2 ;;
    --ssh-key) ssh_key="$2"; shift 2 ;;
    --destroy-cmd) destroy_cmd="$2"; shift 2 ;;
    --list-scenarios) list_scenarios=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$tofu_output" ] || { echo "--tofu-output is required" >&2; exit 2; }
[ -n "$evidence_root" ] || { echo "--evidence-root is required" >&2; exit 2; }
[ -f "$tofu_output" ] || { echo "tofu output not found: $tofu_output" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
e2e_script="$script_dir/sam-e2e.sh"
summary_script="$script_dir/sam-e2e-summary.sh"
post_destroy_script="$script_dir/sam-post-destroy-inventory.sh"

mkdir -p "$evidence_root"

nodes_json="$evidence_root/nodes.json"
jq '.nodes.value' "$tofu_output" >"$nodes_json"

require_node() {
  local node="$1"
  jq -e --arg node "$node" 'has($node)' "$nodes_json" >/dev/null || {
    echo "required node missing from tofu output: $node" >&2
    return 1
  }
}

for node in aws-rr-a aws-rr-b aws-leaf-a azure-leaf-a oci-leaf-a pve-leaf-a; do
  require_node "$node"
done

scenario_names=(
  baseline
  rr-failover-aws-rr-a
  rr-failover-aws-rr-b
  leaf-failover-aws-leaf-a
  leaf-failover-azure-leaf-a
  leaf-failover-oci-leaf-a
  leaf-failover-pve-leaf-a
  load-balance
)

if [ "$list_scenarios" -eq 1 ]; then
  printf '%s\n' "${scenario_names[@]}"
  exit 0
fi

[ -n "$artifact" ] || { echo "--artifact is required" >&2; exit 2; }
[ -f "$artifact" ] || { echo "artifact not found: $artifact" >&2; exit 2; }
[ -f "$ssh_key" ] || { echo "ssh key not found: $ssh_key" >&2; exit 2; }

run_scenario() {
  local name="$1"; shift
  local dir="$evidence_root/$name"
  local rc=0
  mkdir -p "$dir"
  echo "== scenario $name =="
  set +e
  "$e2e_script" \
    --tofu-output "$tofu_output" \
    --artifact "$artifact" \
    --ssh-key "$ssh_key" \
    --evidence-dir "$dir" \
    "$@" 2>&1 | tee "$dir/sam-e2e.log"
  rc=${PIPESTATUS[0]}
  set -e
  "$summary_script" "$dir" >"$dir/summary.txt"
  sed -n '1,160p' "$dir/summary.txt"
  if [ "$rc" -eq 0 ]; then
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

{
  date -u '+timestamp=%Y-%m-%dT%H:%M:%SZ'
  echo "tofu_output=$tofu_output"
  echo "artifact=$artifact"
  sha256sum "$artifact"
  echo "ssh_key=$ssh_key"
  ssh-keygen -lf "${ssh_key}.pub" 2>/dev/null || ssh-keygen -y -f "$ssh_key" | ssh-keygen -lf -
  echo "destroy_cmd=${destroy_cmd:-}"
  echo "policy_read=Read ~/routerd-orchestration.md and cloudedge-mobility/LAB_POLICY.md before running this on real machines."
} >"$evidence_root/full-validation-note.txt"

printf 'scenario\tstatus\tevidence_dir\n' >"$evidence_root/scenario-status.tsv"
trap write_overall_summary EXIT

run_scenario baseline \
  --load-balance-report \
  --performance-tests

run_scenario rr-failover-aws-rr-a \
  --skip-deploy \
  --failover-node aws-rr-a \
  --rejoin-after-failover \
  --load-balance-report \
  --performance-tests \
  --failover-transfer-tests

run_scenario rr-failover-aws-rr-b \
  --skip-deploy \
  --failover-node aws-rr-b \
  --rejoin-after-failover \
  --load-balance-report \
  --performance-tests \
  --failover-transfer-tests

for leaf in aws-leaf-a azure-leaf-a oci-leaf-a pve-leaf-a; do
  run_scenario "leaf-failover-${leaf}" \
    --skip-deploy \
    --failover-node "$leaf" \
    --rejoin-after-failover \
    --load-balance-report \
    --performance-tests \
    --failover-transfer-tests
done

run_scenario load-balance \
  --skip-deploy \
  --load-balance-report \
  --skip-legacy-protocols \
  --performance-tests

if [ -n "$destroy_cmd" ]; then
  echo "== destroy =="
  bash -lc "$destroy_cmd" >"$evidence_root/destroy.log" 2>&1
  "$post_destroy_script" --tofu-output "$tofu_output" --evidence-dir "$evidence_root/post-destroy" >"$evidence_root/post-destroy-summary.txt"
  "$summary_script" "$evidence_root/load-balance" >"$evidence_root/final-summary.txt"
fi

echo "full validation evidence: $evidence_root"
