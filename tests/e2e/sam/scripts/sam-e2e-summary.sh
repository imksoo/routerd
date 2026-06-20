#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/sam-e2e-summary.sh EVIDENCE_DIR

Summarizes a SAM E2E evidence directory. The raw pseudo-client E2E matrix
remains the PASS authority; this script only makes the evidence easier to audit.
USAGE
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi

evidence_dir="${1:-}"
[ -n "$evidence_dir" ] || { usage >&2; exit 2; }
[ -d "$evidence_dir" ] || { echo "evidence dir not found: $evidence_dir" >&2; exit 2; }

count_status_file() {
  local file="$1"
  awk -F '\t' '
    NF >= 3 { total++; status[$3]++ }
    END {
      if (total == 0) {
        print "total=0"
        exit
      }
      printf "total=%d", total
      for (s in status) {
        printf " %s=%d", s, status[s]
      }
      printf "\n"
    }
  ' "$file"
}

count_public_file() {
  local file="$1"
  awk -F '\t' '
    NF >= 3 { total++; status[$3]++ }
    END {
      if (total == 0) {
        print "total=0"
        exit
      }
      printf "total=%d", total
      for (s in status) {
        printf " %s=%d", s, status[s]
      }
      printf "\n"
    }
  ' "$file"
}

print_group_summaries() {
  local group="$1"
  local file
  if [ ! -d "$evidence_dir/$group" ]; then
    return 0
  fi
  find "$evidence_dir/$group" -mindepth 2 -maxdepth 2 -name summary.tsv -type f | sort | while read -r file; do
    label="${file%/summary.tsv}"
    label="${label#"$evidence_dir/$group/"}"
    printf '%s\t%s\t%s\n' "$group" "$label" "$(count_status_file "$file")"
  done
}

echo "evidence_dir=$evidence_dir"
if [ -f "$evidence_dir/run-note.txt" ]; then
  echo "== run-note =="
  sed -n '1,40p' "$evidence_dir/run-note.txt"
fi

echo "== e2e summaries =="
print_group_summaries matrix
print_group_summaries legacy
print_group_summaries performance

if [ -d "$evidence_dir/performance" ]; then
  find "$evidence_dir/performance" -mindepth 2 -maxdepth 2 -name public-summary.tsv -type f | sort | while read -r file; do
    label="${file%/public-summary.tsv}"
    label="${label#"$evidence_dir/performance/"}"
    printf 'public-performance\t%s\t%s\n' "$label" "$(count_public_file "$file")"
  done
fi

if [ -f "$evidence_dir/convergence/summary.tsv" ]; then
  echo "== convergence =="
  column -t -s $'\t' "$evidence_dir/convergence/summary.tsv" 2>/dev/null || cat "$evidence_dir/convergence/summary.tsv"
fi

if [ -d "$evidence_dir/failover-transfer" ]; then
  echo "== failover-transfer =="
  find "$evidence_dir/failover-transfer" -mindepth 2 -maxdepth 2 -name result.txt -type f | sort | while read -r file; do
    label="${file%/result.txt}"
    label="${label#"$evidence_dir/failover-transfer/"}"
    rc="$(awk -F= '/^rc=/ {print $2}' "$file" | tail -n 1)"
    bytes="$(awk '/routerd-.*\.bin$/ {print $5}' "$file" | tail -n 1)"
    [ -n "$rc" ] || rc=missing
    [ -n "$bytes" ] || bytes=missing
    printf '%s\trc=%s\tbytes=%s\n' "$label" "$rc" "$bytes"
  done
fi

if [ -d "$evidence_dir/provider" ]; then
  echo "== provider evidence =="
  find "$evidence_dir/provider" -mindepth 1 -maxdepth 1 -type d | sort | while read -r dir; do
    label="${dir#"$evidence_dir/provider/"}"
    files="$(find "$dir" -maxdepth 1 -type f | wc -l | tr -d ' ')"
    oci_name=
    if [ -f "$dir/oci.txt" ]; then
      oci_name="$(awk -F= '/^oci_compartment_display_name=/ {print $2}' "$dir/oci.txt" | tail -n 1)"
    fi
    printf '%s\tfiles=%s' "$label" "$files"
    if [ -n "$oci_name" ]; then
      printf '\toci_compartment_display_name=%s' "$oci_name"
    fi
    printf '\n'
  done
fi
