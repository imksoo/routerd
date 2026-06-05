#!/usr/bin/env bash
#
# Offline smoke test for the CloudEdge SAM PoC evidence bundle assembler.
#
set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

die() { printf '%s: %s\n' "$SELF" "$*" >&2; exit 1; }

tmp=$(mktemp -d "${TMPDIR:-/tmp}/cloudedge-poc-evidence.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

gold="$tmp/gold"
mkdir -p "$gold"

cat > "$gold/coverage-matrix-20260605.md" <<'MD'
# routerd SAM PoC Coverage - 2026-06-05

## Coverage Against New routerd SAM Plan

Strongly covered by existing routerd SAM evidence:

- P0-01 bidirectional communication
- P0-02 no NAT

Partially covered; needs better automated evidence bundling:

- P0-05 ARP / route / FIB consistency
- P0-06 cloud fabric

Not covered in this workspace:

- 24h soak and 100/1000-IP scale tests from the new plan.

## Go / Conditional Go / No-Go

routerd SAM status from existing evidence: Conditional Go for lab PoC.
MD

cat > "$gold/test-record-20260605.csv" <<'CSV'
TEST_ID,PHASE,TARGET,RESULT,EVIDENCE,NOTES
20260605-0225-LOCAL-01,LOCAL,make test,PASS,/tmp/gold,go test passed
20260605-0236-CAP-01,CAP,4-point simultaneous pcap bundle,PARTIAL,,pcap harness missing
CSV

cat > "$gold/open-issues-20260605.md" <<'MD'
# Open Issues From 2026-06-05 Revalidation

- Normalize the common PoC evidence bundle for routerd SAM.
MD

out="$tmp/poc-evidence-20260605"
"$SCRIPT_DIR/cloudedge-poc-evidence.sh" \
  --date 20260605 \
  --gold-dir "$gold" \
  --out "$out" \
  --schema "$SCRIPT_DIR/cloudedge-poc-bundle-schema.json" >/dev/null

python3 - "$out" "$SCRIPT_DIR/cloudedge-poc-bundle-schema.json" <<'PY'
import csv
import json
import sys
from pathlib import Path

out = Path(sys.argv[1])
schema_path = Path(sys.argv[2])
for name in (
    "00-topology",
    "01-baseline",
    "02-config",
    "03-control-plane",
    "04-data-plane",
    "05-capture",
    "06-rollback",
    "07-summary",
):
    if not (out / name).is_dir():
        raise SystemExit(f"missing bundle directory {name}")

manifest = json.loads((out / "manifest.json").read_text())
schema = json.loads(schema_path.read_text())
try:
    import jsonschema
except Exception:
    jsonschema = None
if jsonschema is not None:
    jsonschema.validate(instance=manifest, schema=schema)

if manifest["testRecordColumns"] != ["TEST_ID", "PHASE", "TARGET", "RESULT", "EVIDENCE", "NOTES"]:
    raise SystemExit("test-record columns drifted from gold")
if manifest["summary"]["notRun"] < 3:
    raise SystemExit("expected explicit NOT-RUN records")
if manifest["summary"]["partial"] < 1:
    raise SystemExit("expected explicit PARTIAL records")

records = {r["testId"]: r for r in manifest["records"]}
for test_id in (
    "20260605-0000-C8K-01",
    "20260605-0000-SOAK-01",
    "20260605-0000-SCALE-01",
    "20260605-0000-RB-01",
):
    rec = records.get(test_id)
    if not rec or rec["result"] != "NOT-RUN":
        raise SystemExit(f"{test_id} is not explicit NOT-RUN")
for rec in manifest["records"]:
    if not rec["evidence"]:
        raise SystemExit(f"{rec['testId']} has empty evidence")
    if not rec["observedAt"].endswith("Z"):
        raise SystemExit(f"{rec['testId']} observedAt is not UTC")

with (out / "test-record.csv").open(newline="") as f:
    reader = csv.DictReader(f)
    if reader.fieldnames != ["TEST_ID", "PHASE", "TARGET", "RESULT", "EVIDENCE", "NOTES"]:
        raise SystemExit("output CSV header drifted")
    rows = list(reader)
if not any(row["RESULT"] == "PARTIAL" for row in rows):
    raise SystemExit("PARTIAL row missing")
if not any(row["RESULT"] == "NOT-RUN" for row in rows):
    raise SystemExit("NOT-RUN row missing")
if any(not row["EVIDENCE"] for row in rows):
    raise SystemExit("CSV contains empty evidence path")

slots = {(slot["phase"], slot["issue"]) for slot in manifest["collectorSlots"]}
for want in (("CAP", 112), ("APP", 113), ("CF", 114), ("C8K", 115)):
    if want not in slots:
        raise SystemExit(f"collector slot missing {want}")
PY

printf 'cloudedge poc evidence offline OK\n'
