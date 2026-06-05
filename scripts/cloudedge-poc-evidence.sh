#!/usr/bin/env bash
#
# cloudedge-poc-evidence.sh - assemble a common CloudEdge SAM PoC evidence bundle.
#
# The bundle contract intentionally follows /home/imksoo/routerd-sam-gold:
# test-record.csv keeps the six gold columns
# TEST_ID,PHASE,TARGET,RESULT,EVIDENCE,NOTES. The manifest adds derived
# absolute timestamps and collector slot metadata for later live collectors.
#
set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)

die() { printf '%s: %s\n' "$SELF" "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

usage() {
  cat <<EOF
$SELF - assemble CloudEdge SAM PoC evidence bundle

USAGE:
  $SELF --date <YYYYMMDD> --gold-dir <dir> --out <dir> [--schema <file>]

DEFAULTS:
  --date      current UTC date
  --gold-dir /home/imksoo/routerd-sam-gold
  --out       poc-evidence-<date>
  --schema    scripts/cloudedge-poc-bundle-schema.json

OUTPUT:
  <out>/00-topology ... <out>/07-summary
  <out>/test-record.csv       gold six-column record plus explicit NOT-RUN slots
  <out>/manifest.json         normalized records with absolute observedAt values
  <out>/go-nogo.md
  <out>/known-risks.md
  <out>/open-issues.md
  <out>/rollback-diff.md
  <out>/source-evidence-refs.md

The assembler does not invent a new TEST_ID taxonomy. It preserves the gold
TEST_ID format and only appends explicit NOT-RUN records for collector slots
that are outside the current routerd SAM evidence bundle.
EOF
}

date_utc=$(date -u +%Y%m%d)
gold_dir="/home/imksoo/routerd-sam-gold"
out=""
schema="$REPO_ROOT/scripts/cloudedge-poc-bundle-schema.json"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --date) date_utc="${2:-}"; shift 2 ;;
    --gold-dir) gold_dir="${2:-}"; shift 2 ;;
    --out) out="${2:-}"; shift 2 ;;
    --schema) schema="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

[[ "$date_utc" =~ ^[0-9]{8}$ ]] || die "--date must be YYYYMMDD"
[[ -n "$out" ]] || out="poc-evidence-$date_utc"
[[ -d "$gold_dir" ]] || die "gold dir not found: $gold_dir"
[[ -f "$schema" ]] || die "schema not found: $schema"
have python3 || die "python3 is required"

python3 - "$gold_dir" "$date_utc" "$out" "$schema" <<'PY'
import csv
import datetime as dt
import json
import os
import re
import shutil
import sys
from pathlib import Path

gold_dir = Path(sys.argv[1]).resolve()
date_s = sys.argv[2]
out = Path(sys.argv[3]).resolve()
schema = Path(sys.argv[4]).resolve()

columns = ["TEST_ID", "PHASE", "TARGET", "RESULT", "EVIDENCE", "NOTES"]
result_enum = ["PASS", "FAIL", "PARTIAL", "NOT-RUN"]
layout = [
    "00-topology",
    "01-baseline",
    "02-config",
    "03-control-plane",
    "04-data-plane",
    "05-capture",
    "06-rollback",
    "07-summary",
]

def gold_file(prefix: str) -> Path:
    exact = gold_dir / f"{prefix}-{date_s}.md"
    if exact.exists():
        return exact
    exact_csv = gold_dir / f"{prefix}-{date_s}.csv"
    if exact_csv.exists():
        return exact_csv
    matches = sorted(gold_dir.glob(f"{prefix}-*"))
    if len(matches) == 1:
        return matches[0]
    raise SystemExit(f"cannot find unique gold {prefix} file in {gold_dir}")

coverage = gold_file("coverage-matrix")
test_record = gold_file("test-record")
open_issues = gold_file("open-issues")

def parse_observed_at(test_id: str) -> str:
    m = re.match(r"^([0-9]{8})-([0-9]{4})-", test_id)
    if not m:
        raise SystemExit(f"bad TEST_ID format: {test_id}")
    day, hm = m.groups()
    when = dt.datetime.strptime(day + hm, "%Y%m%d%H%M").replace(tzinfo=dt.timezone.utc)
    return when.isoformat().replace("+00:00", "Z")

def normalize_record(row: dict) -> dict:
    missing = [c for c in columns if c not in row]
    if missing:
        raise SystemExit(f"test record row missing columns: {missing}")
    rec = {c: (row.get(c) or "").strip() for c in columns}
    if rec["RESULT"] not in result_enum:
        raise SystemExit(f"{rec['TEST_ID']} has unsupported RESULT {rec['RESULT']!r}")
    if not rec["EVIDENCE"]:
        rec["EVIDENCE"] = "07-summary/known-risks.md"
    return rec

with test_record.open(newline="") as f:
    reader = csv.DictReader(f)
    if reader.fieldnames != columns:
        raise SystemExit(f"gold test-record header {reader.fieldnames!r}, want {columns!r}")
    records = [normalize_record(row) for row in reader]

supplemental = [
    {
        "TEST_ID": f"{date_s}-0000-C8K-01",
        "PHASE": "C8K",
        "TARGET": "Cisco C8000V LISP comparison evidence",
        "RESULT": "NOT-RUN",
        "EVIDENCE": "07-summary/known-risks.md",
        "NOTES": "#115 is scoped out for routerd SAM; keep Cisco comparison slot explicit",
    },
    {
        "TEST_ID": f"{date_s}-0000-SOAK-01",
        "PHASE": "SOAK",
        "TARGET": "24h soak run",
        "RESULT": "NOT-RUN",
        "EVIDENCE": "07-summary/known-risks.md",
        "NOTES": "Gold coverage marks 24h soak as not covered in this workspace",
    },
    {
        "TEST_ID": f"{date_s}-0000-SCALE-01",
        "PHASE": "SCALE",
        "TARGET": "100/1000-IP scale test",
        "RESULT": "NOT-RUN",
        "EVIDENCE": "07-summary/known-risks.md",
        "NOTES": "Gold coverage marks scale testing as not covered in this workspace",
    },
    {
        "TEST_ID": f"{date_s}-0000-RB-01",
        "PHASE": "RB",
        "TARGET": "common rollback diff artifact",
        "RESULT": "NOT-RUN",
        "EVIDENCE": "06-rollback/rollback-diff.md",
        "NOTES": "CloudEdge teardown evidence exists, but no common rollback diff artifact is in gold",
    },
]
seen = {r["TEST_ID"] for r in records}
for rec in supplemental:
    if rec["TEST_ID"] not in seen:
        records.append(rec)
        seen.add(rec["TEST_ID"])

if out.exists():
    shutil.rmtree(out)
for name in layout:
    (out / name).mkdir(parents=True, exist_ok=True)

def write(path: Path, text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")

def copy(src: Path, dst: Path) -> None:
    dst.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(src, dst)

copy(coverage, out / "00-topology" / "coverage-matrix.md")
copy(open_issues, out / "07-summary" / "open-issues.md")
copy(open_issues, out / "open-issues.md")

for path in (out / "test-record.csv", out / "01-baseline" / "test-record.csv"):
    with path.open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=columns)
        writer.writeheader()
        writer.writerows(records)

counts = {value: 0 for value in result_enum}
for rec in records:
    counts[rec["RESULT"]] += 1

known = [r for r in records if r["RESULT"] in ("PARTIAL", "NOT-RUN", "FAIL")]
known_lines = [
    "# Known Risks and Missing Evidence",
    "",
    "Generated from the gold coverage/test-record inputs. Missing evidence is explicit; records are not silently omitted.",
    "",
]
for rec in known:
    known_lines.append(f"- {rec['TEST_ID']} {rec['RESULT']} {rec['PHASE']} {rec['TARGET']}: {rec['NOTES']} (evidence: {rec['EVIDENCE']})")
known_lines.append("")
known_risks = "\n".join(known_lines)
write(out / "07-summary" / "known-risks.md", known_risks)
write(out / "known-risks.md", known_risks)

decision = "Conditional Go" if counts["FAIL"] == 0 else "No-Go"
go_nogo = f"""# Go / No-Go

Decision: {decision} for routerd SAM lab PoC.

Basis: gold coverage matrix from `{coverage}`.

| RESULT | Count |
| --- | ---: |
| PASS | {counts['PASS']} |
| FAIL | {counts['FAIL']} |
| PARTIAL | {counts['PARTIAL']} |
| NOT-RUN | {counts['NOT-RUN']} |

Conditions:

- PARTIAL and NOT-RUN records remain explicit in `test-record.csv`.
- Cisco C8000V/LISP is retained only as a NOT-RUN comparison slot for this routerd SAM bundle.
- Live pcap and cloud fabric collectors are expected to fill CAP/CF evidence in later issues.
"""
write(out / "07-summary" / "go-nogo.md", go_nogo)
write(out / "go-nogo.md", go_nogo)

rollback = """# Rollback Diff

Result: NOT-RUN for a common rollback diff artifact.

The gold coverage records CloudEdge teardown/rollback evidence from prior runs,
but it does not contain a single normalized rollback diff artifact. The bundle
therefore keeps the RB slot explicit rather than implying that the artifact was
collected.
"""
write(out / "06-rollback" / "rollback-diff.md", rollback)
write(out / "rollback-diff.md", rollback)

source_refs = [
    "# Source Evidence References",
    "",
    f"- gold coverage matrix: `{coverage}`",
    f"- gold test record: `{test_record}`",
    f"- gold open issues: `{open_issues}`",
    "",
    "## Referenced evidence paths",
    "",
]
for evidence in sorted({r["EVIDENCE"] for r in records if r["EVIDENCE"]}):
    source_refs.append(f"- `{evidence}`")
source_refs.append("")
source_text = "\n".join(source_refs)
write(out / "02-config" / "source-evidence-refs.md", source_text)
write(out / "source-evidence-refs.md", source_text)

collector_slots = [
    {
        "phase": "CAP",
        "issue": 112,
        "path": "05-capture",
        "status": "PARTIAL",
        "contract": "Four-point pcap collectors write TEST_ID-scoped source/router-inside/router-outside/remote files and update CAP records.",
    },
    {
        "phase": "APP",
        "issue": 113,
        "path": "04-data-plane",
        "status": "PARTIAL",
        "contract": "Protocol collector records concrete overlay_mtu, route_mtu, route_advmss, and effective MSS fields.",
    },
    {
        "phase": "CF",
        "issue": 114,
        "path": "03-control-plane",
        "status": "PARTIAL",
        "contract": "Cloud fabric collectors normalize AWS/Azure/OCI route, effective security, flow, and forwarding evidence.",
    },
    {
        "phase": "C8K",
        "issue": 115,
        "path": "03-control-plane",
        "status": "NOT-RUN",
        "contract": "Cisco C8000V/LISP collector is out of #111 scope; this bundle keeps only an explicit NOT-RUN comparison slot.",
    },
]

write(out / "03-control-plane" / "collector-contract.md", "\n".join([
    "# Control-plane Collector Contract",
    "",
    "- CF collectors for #114 write provider JSON and normalized summaries here.",
    "- C8K comparison evidence remains NOT-RUN in #111 because #115 is out of scope.",
    "",
]))
write(out / "04-data-plane" / "collector-contract.md", "\n".join([
    "# Data-plane Collector Contract",
    "",
    "- Protocol collectors for #113 update APP records with concrete MTU/MSS fields.",
    "- Existing PASS protocol evidence remains referenced from the gold record.",
    "",
]))
write(out / "05-capture" / "collector-contract.md", "\n".join([
    "# Capture Collector Contract",
    "",
    "- Four-point capture collectors for #112 write TEST_ID-node-role filenames here.",
    "- Missing capture points must mark CAP/DP evidence PARTIAL with a reason.",
    "",
]))

manifest_records = []
for rec in records:
    manifest_records.append({
        "testId": rec["TEST_ID"],
        "phase": rec["PHASE"],
        "target": rec["TARGET"],
        "result": rec["RESULT"],
        "evidence": rec["EVIDENCE"],
        "notes": rec["NOTES"],
        "observedAt": parse_observed_at(rec["TEST_ID"]),
    })

manifest = {
    "bundleVersion": 1,
    "date": date_s,
    "generatedAt": dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
    "gold": {
        "directory": str(gold_dir),
        "coverageMatrix": str(coverage),
        "testRecord": str(test_record),
        "openIssues": str(open_issues),
    },
    "layout": layout,
    "testRecordColumns": columns,
    "resultEnum": result_enum,
    "summary": {
        "pass": counts["PASS"],
        "fail": counts["FAIL"],
        "partial": counts["PARTIAL"],
        "notRun": counts["NOT-RUN"],
        "total": len(records),
    },
    "records": manifest_records,
    "collectorSlots": collector_slots,
}
write(out / "manifest.json", json.dumps(manifest, indent=2, sort_keys=True) + "\n")
write(out / "07-summary" / "manifest.json", json.dumps(manifest, indent=2, sort_keys=True) + "\n")

try:
    import jsonschema
except Exception:
    jsonschema = None
if jsonschema is not None:
    jsonschema.validate(instance=manifest, schema=json.loads(schema.read_text(encoding="utf-8")))

print(out)
PY
