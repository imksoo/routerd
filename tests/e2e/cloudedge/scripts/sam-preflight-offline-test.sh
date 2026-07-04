#!/usr/bin/env bash
set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

die() { printf '%s: %s\n' "$SELF" "$*" >&2; exit 1; }

tmp=$(mktemp -d "${TMPDIR:-/tmp}/sam-preflight-offline.XXXXXX")
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT

fake_bin="$tmp/bin"
mkdir -p "$fake_bin"
cat >"$fake_bin/oci" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '{"data":{"name":"routerd-lab"}}\n'
SH
chmod +x "$fake_bin/oci"

artifact_root="$tmp/artifact-root"
mkdir -p "$artifact_root/bin"
for name in routerd routerctl; do
  cat >"$artifact_root/bin/$name" <<SH
#!/usr/bin/env bash
set -euo pipefail
if [ "\${1:-}" = "version" ]; then
  printf '%s offline-test\\n' "$name"
fi
SH
  chmod 0700 "$artifact_root/bin/$name"
done
artifact="$tmp/routerd.tar.gz"
tar -C "$artifact_root" -czf "$artifact" .

write_tfvars() {
  local run_id="$1" out="$2"
  cat >"$out" <<EOF
run_id = "$run_id"
oci_profile = "offline"
oci_region = "ap-tokyo-1"
oci_compartment_id = "ocid1.compartment.oc1..offline"
EOF
}

run_45="$(printf 'r%.0s' $(seq 1 45))"
run_46="$(printf 'r%.0s' $(seq 1 46))"
good_tfvars="$tmp/good.tfvars"
bad_tfvars="$tmp/bad.tfvars"
write_tfvars "$run_45" "$good_tfvars"
write_tfvars "$run_46" "$bad_tfvars"

good_evidence="$tmp/good-evidence"
PATH="$fake_bin:$PATH" "$SCRIPT_DIR/sam-preflight.sh" \
  --tfvars "$good_tfvars" \
  --artifact "$artifact" \
  --evidence-dir "$good_evidence" >"$tmp/good.stdout" 2>"$tmp/good.stderr"

grep -q 'PASS: run_id name budget fits provider limits' "$tmp/good.stdout" || {
  cat "$tmp/good.stdout" >&2
  cat "$tmp/good.stderr" >&2
  die "45-char run_id did not pass name budget"
}
grep -q 'artifact_binary_routerd=' "$tmp/good.stdout" || die "artifact routerd binary was not checked"
grep -q $'azure virtual network\t64\t64\tvnet-routerd-' "$good_evidence/name-budget.tsv" || {
  cat "$good_evidence/name-budget.tsv" >&2
  die "45-char boundary did not produce a 64-char Azure VNet name"
}

bad_evidence="$tmp/bad-evidence"
if PATH="$fake_bin:$PATH" "$SCRIPT_DIR/sam-preflight.sh" \
    --tfvars "$bad_tfvars" \
    --artifact "$artifact" \
    --evidence-dir "$bad_evidence" >"$tmp/bad.stdout" 2>"$tmp/bad.stderr"; then
  cat "$tmp/bad.stdout" >&2
  die "46-char run_id unexpectedly passed name budget"
fi
grep -q 'FAIL: azure virtual network derived name is 65 chars, limit 64' "$tmp/bad.stderr" || {
  cat "$tmp/bad.stdout" >&2
  cat "$tmp/bad.stderr" >&2
  die "46-char run_id failure did not identify Azure VNet budget"
}

printf 'sam preflight offline OK\n'
