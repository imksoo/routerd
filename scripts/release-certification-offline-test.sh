#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/routerd-release-certification-test.XXXXXX")
cleanup_work() {
  find "$work" -depth -delete
}
trap cleanup_work EXIT

artifact="$work/routerd-test-linux-amd64.tar.gz"
printf 'exact release artifact\n' >"$artifact"
artifact_sha=$(sha256sum "$artifact" | awk '{print $1}')

cat >"$work/contract.json" <<EOF
{
  "schemaVersion": "release-environment-contract/v1",
  "runId": "release-contract-offline-test",
  "environment": "offline",
  "topology": "full",
  "stateMode": "fresh-fabric-fresh-state",
  "routerdArtifact": {
    "version": "v20990101.0000",
    "commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "path": "$artifact",
    "sha256": "$artifact_sha",
    "target": "linux-amd64"
  },
  "labsCommit": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "providers": [
    {"name": "pve", "profile": "offline", "region": "local", "authMode": "fixture"},
    {"name": "aws", "profile": "offline", "region": "fixture-1", "authMode": "fixture"},
    {"name": "azure", "profile": "offline", "region": "fixture-2", "authMode": "fixture"},
    {"name": "oci", "profile": "offline", "region": "fixture-3", "authMode": "fixture"}
  ],
  "tofu": {
    "workingDirectory": "$work/tofu",
    "statePath": "$work/tofu/terraform.tfstate",
    "variablesPath": "$work/tofu/terraform.tfvars",
    "lockPath": "$work/tofu/.terraform.lock.hcl",
    "outputPath": "$work/tofu-output.json"
  },
  "pve": {
    "node": "offline-pve",
    "datastore": "offline-store",
    "bootSource": "offline-image",
    "underlayBridge": "offline-underlay",
    "captureBridge": "offline-capture",
    "managementAddressSource": "qga-dhcp",
    "vmids": [9001, 9002, 9003, 9004]
  },
  "lifecycle": {
    "ttl": "75m",
    "heartbeatStale": "5m",
    "cleanupScope": "run-id"
  }
}
EOF

cat >"$work/pve-driver" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
out=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --contract) shift 2 ;;
    --out) out=$2; shift 2 ;;
    --repair) shift ;;
    *) exit 2 ;;
  esac
done
cat >"$out" <<JSON
{
  "status": "pass",
  "checks": [
    {
      "name": "pve-offline-contract",
      "component": "pve",
      "provider": "pve",
      "result": "pass",
      "checkedAt": "2099-01-01T00:00:00Z",
      "summary": "offline PVE driver contract"
    }
  ],
  "repairs": [],
  "toolVersions": {"pve-fixture": "1"}
}
JSON
EOF

cat >"$work/cloud-driver" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
out=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --contract) shift 2 ;;
    --out) out=$2; shift 2 ;;
    --repair) shift ;;
    *) exit 2 ;;
  esac
done
cat >"$out" <<JSON
{
  "status": "pass",
  "checks": [
    {"name": "aws-offline-contract", "component": "cloud", "provider": "aws", "result": "pass", "checkedAt": "2099-01-01T00:00:00Z"},
    {"name": "azure-offline-contract", "component": "cloud", "provider": "azure", "result": "pass", "checkedAt": "2099-01-01T00:00:00Z"},
    {"name": "oci-offline-contract", "component": "cloud", "provider": "oci", "result": "pass", "checkedAt": "2099-01-01T00:00:00Z"}
  ],
  "repairs": [],
  "toolVersions": {"cloud-fixture": "1"}
}
JSON
EOF

cat >"$work/qualification-pass" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
out=
heartbeat=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --certification|--release) shift 2 ;;
    --out) out=$2; shift 2 ;;
    --heartbeat) heartbeat=$2; shift 2 ;;
    *) exit 2 ;;
  esac
done
touch "$heartbeat"
cat >"$out" <<JSON
{
  "status": "pass",
  "classification": "none",
  "checks": [{"name": "offline qualification", "result": "pass"}]
}
JSON
EOF

cat >"$work/qualification-stale" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
while [ "$#" -gt 0 ]; do
  case "$1" in
    --certification|--release|--out|--heartbeat) shift 2 ;;
    *) exit 2 ;;
  esac
done
sleep 30
EOF

cat >"$work/cleanup" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
evidence_dir=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --run-id) shift 2 ;;
    --evidence-dir) evidence_dir=$2; shift 2 ;;
    *) exit 2 ;;
  esac
done
touch "$evidence_dir/cleanup-called"
EOF

cat >"$work/inventory" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
evidence_dir=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --run-id) shift 2 ;;
    --evidence-dir) evidence_dir=$2; shift 2 ;;
    *) exit 2 ;;
  esac
done
test -f "$evidence_dir/cleanup-called"
touch "$evidence_dir/inventory-zero"
EOF

chmod +x \
  "$work/pve-driver" \
  "$work/cloud-driver" \
  "$work/qualification-pass" \
  "$work/qualification-stale" \
  "$work/cleanup" \
  "$work/inventory"

"$repo_root/scripts/certify-pve-substrate.sh" \
  --environment offline \
  --topology full \
  --contract "$work/contract.json" \
  --driver "$work/pve-driver" \
  --out "$work/pve-certification.json" \
  --valid-for 24h

"$repo_root/scripts/certify-cloud-substrate.sh" \
  --environment offline \
  --topology full \
  --providers aws,azure,oci \
  --contract "$work/contract.json" \
  --driver "$work/cloud-driver" \
  --pve-certification "$work/pve-certification.json" \
  --out "$work/certification.json" \
  --valid-for 24h

"$repo_root/scripts/release-environment-preflight.sh" \
  --certification "$work/certification.json" \
  --environment offline \
  --topology full \
  --providers pve,aws,azure,oci \
  --release v20990101.0000

"$repo_root/scripts/certify-cloud-substrate.sh" \
  --environment offline \
  --topology full \
  --providers aws,azure,oci \
  --contract "$work/contract.json" \
  --driver "$work/cloud-driver" \
  --out "$work/cloud-only-certification.json" \
  --valid-for 24h

"$repo_root/scripts/certify-pve-substrate.sh" \
  --environment offline \
  --topology full \
  --contract "$work/contract.json" \
  --driver "$work/pve-driver" \
  --cloud-certification "$work/cloud-only-certification.json" \
  --out "$work/reverse-order-certification.json" \
  --valid-for 24h

"$repo_root/scripts/release-environment-preflight.sh" \
  --certification "$work/reverse-order-certification.json" \
  --environment offline \
  --topology full \
  --providers pve,aws,azure,oci \
  --release v20990101.0000

if "$repo_root/scripts/release-environment-preflight.sh" \
  --certification "$work/certification.json" \
  --environment wrong \
  --topology full \
  --providers pve,aws,azure,oci >/dev/null 2>&1; then
  echo "mismatched environment unexpectedly passed" >&2
  exit 1
fi

cp "$work/certification.json" "$work/expired.json"
python3 - "$work/expired.json" <<'PY'
import json
import sys
path = sys.argv[1]
value = json.load(open(path, encoding="utf-8"))
value["expiresAt"] = "2000-01-01T00:00:00Z"
with open(path, "w", encoding="utf-8") as stream:
    json.dump(value, stream)
PY
if "$repo_root/scripts/release-environment-preflight.sh" \
  --certification "$work/expired.json" \
  --environment offline \
  --topology full \
  --providers pve,aws,azure,oci >/dev/null 2>&1; then
  echo "expired certification unexpectedly passed" >&2
  exit 1
fi
if "$repo_root/scripts/release-qualification-smoke.sh" \
  --certification "$work/expired.json" \
  --release v20990101.0000 \
  --out "$work/preflight-failure-result.json" \
  --qualification-command "$work/qualification-pass" \
  --cleanup-command "$work/cleanup" \
  --inventory-command "$work/inventory" \
  --evidence-dir "$work/preflight-failure-evidence" \
  --ttl 15s \
  --heartbeat-stale 5s; then
  echo "qualification unexpectedly started with expired certification" >&2
  exit 1
fi
python3 - "$work/preflight-failure-result.json" <<'PY'
import json
import sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["status"] == "fail", value
assert value["classification"] == "preflight_failure", value
assert value["watchdogPid"] is None, value
PY
test ! -e "$work/preflight-failure-evidence/cleanup-called"

mkdir -p "$work/pass-evidence"
"$repo_root/scripts/release-qualification-smoke.sh" \
  --certification "$work/certification.json" \
  --release v20990101.0000 \
  --out "$work/pass-result.json" \
  --qualification-command "$work/qualification-pass" \
  --cleanup-command "$work/cleanup" \
  --inventory-command "$work/inventory" \
  --evidence-dir "$work/pass-evidence" \
  --ttl 15s \
  --heartbeat-stale 5s
test -f "$work/pass-evidence/cleanup-called"
test -f "$work/pass-evidence/inventory-zero"

mkdir -p "$work/stale-evidence"
if "$repo_root/scripts/release-qualification-smoke.sh" \
  --certification "$work/certification.json" \
  --release v20990101.0000 \
  --out "$work/stale-result.json" \
  --qualification-command "$work/qualification-stale" \
  --cleanup-command "$work/cleanup" \
  --inventory-command "$work/inventory" \
  --evidence-dir "$work/stale-evidence" \
  --ttl 10s \
  --heartbeat-stale 2s; then
  echo "stale qualification unexpectedly passed" >&2
  exit 1
fi
test -f "$work/stale-evidence/watchdog-abort.json"
test -f "$work/stale-evidence/cleanup-called"
test -f "$work/stale-evidence/inventory-zero"
python3 - "$work/stale-result.json" <<'PY'
import json
import sys
value = json.load(open(sys.argv[1], encoding="utf-8"))
assert value["status"] == "fail", value
assert value["classification"] == "infra_failure", value
assert value["watchdogAbort"]["reason"] == "heartbeat-stale", value
assert value["cleanupExit"] == 0, value
assert value["inventoryExit"] == 0, value
PY

mkdir -p "$work/orphan-evidence"
"$repo_root/scripts/release-qualification-smoke.sh" \
  --certification "$work/certification.json" \
  --release v20990101.0000 \
  --out "$work/orphan-result.json" \
  --qualification-command "$work/qualification-stale" \
  --cleanup-command "$work/cleanup" \
  --inventory-command "$work/inventory" \
  --evidence-dir "$work/orphan-evidence" \
  --ttl 10s \
  --heartbeat-stale 2s >"$work/orphan-parent.log" 2>&1 &
orphan_parent=$!
sleep 1
kill -KILL "$orphan_parent"
wait "$orphan_parent" 2>/dev/null || true
orphan_inventory_ready=0
for _ in $(seq 1 15); do
  if [ -f "$work/orphan-evidence/inventory-zero" ]; then
    orphan_inventory_ready=1
    break
  fi
  sleep 1
done
test "$orphan_inventory_ready" -eq 1
test -f "$work/orphan-evidence/watchdog-abort.json"
test -f "$work/orphan-evidence/cleanup-result.json"

echo "release certification offline OK"
