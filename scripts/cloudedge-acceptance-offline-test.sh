#!/usr/bin/env bash
#
# Offline acceptance harness test for CloudEdge 4-site scaffolding.
#
# This intentionally does not touch cloud APIs. It stubs MATRIX_RUNNER and
# verifies the scenario catalog, directed matrix JSON, and evidence result shape.
#
set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)

die() { printf '%s: %s\n' "$SELF" "$*" >&2; exit 1; }

tmp=$(mktemp -d "${TMPDIR:-/tmp}/cloudedge-acceptance.XXXXXX")
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT

runner="$tmp/matrix-runner"
cat > "$runner" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
op=$1
src=$2
case "$src" in
  onprem) src_ip=10.77.60.10 ;;
  aws) src_ip=10.77.60.11 ;;
  azure) src_ip=10.77.60.12 ;;
  oci) src_ip=10.77.60.13 ;;
  *) exit 2 ;;
esac
case "$op" in
  ping) exit 0 ;;
  ssh)
    printf 'peer_ip=%s\n' "$src_ip"
    printf 'default_gw=10.77.60.1\n'
    ;;
  *) exit 2 ;;
esac
SH
chmod +x "$runner"

timing_runner="$tmp/failover-timing-runner"
cat > "$timing_runner" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
op=$1
provider=$2
stage=${3:-}
case "$op" in
  inject)
    printf 'stopped=%s\n' "$provider"
    ;;
  observe)
    case "$stage" in detection|switchover|recovery) exit 0 ;; *) exit 2 ;; esac
    ;;
  detail)
    printf 'stage=%s provider=%s\n' "$stage" "$provider"
    ;;
  *) exit 2 ;;
esac
SH
chmod +x "$timing_runner"

protocol_runner="$tmp/protocol-probe-runner"
cat > "$protocol_runner" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
op=$1
client=$2
server=$3
bytes=${4:-104857600}
case "$op" in
  setup)
    printf 'detail=packages-ready-%s-%s\n' "$client" "$server"
    ;;
  ftp-active|ftp-passive)
    printf 'bytes=%s\n' "$bytes"
    printf 'detail=%s-ok\n' "$op"
    ;;
  nfs)
    printf 'bytes=%s\n' "$bytes"
    printf 'detail=nfs-rw-ok\n'
    ;;
  rpc)
    printf 'dynamic_port=32768\n'
    printf 'detail=rpcbind-ok\n'
    ;;
  bulk)
    printf 'bytes=%s\n' "$bytes"
    printf 'detail=bulk-ok\n'
    ;;
  pmtu)
    printf 'overlay_mtu=1380\n'
    printf 'route_mtu=1380\n'
    printf 'route_advmss=1340\n'
    printf 'mss_clamp=1340\n'
    printf 'effective_mss_clamp=1340\n'
    printf 'detail=df-and-clamp-ok\n'
    ;;
  source-preserved|no-nat)
    printf 'detail=%s-ok\n' "$op"
    ;;
  *) exit 2 ;;
esac
SH
chmod +x "$protocol_runner"

l2_runner="$tmp/l2-loop-runner"
cat > "$l2_runner" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
op=$1
phase=$2
provider=$3
case "$op" in
  observe)
    printf 'broadcast_pps=12\n'
    printf 'stp_tcn_delta=%s\n' "$([[ "$phase" == "after" ]] && echo 1 || echo 0)"
    printf 'mac_flap_count=0\n'
    printf 'ping_loss_percent=0\n'
    printf 'blocked_ports=1\n'
    printf 'bpdu_seen=true\n'
    printf 'mechanism=vrrp-single-master+non-master-fail-closed+stp-blocking\n'
    printf 'detail=%s-%s-loop-free\n' "$provider" "$phase"
    ;;
  *) exit 2 ;;
esac
SH
chmod +x "$l2_runner"

"$SCRIPT_DIR/cloudedge-acceptance.sh" lint >/dev/null

matrix_json="$tmp/connectivity-matrix.json"
MATRIX_RUNNER="$runner" "$SCRIPT_DIR/cloudedge-connectivity-matrix.sh" --out "$matrix_json" >/dev/null

python3 - "$matrix_json" <<'PY'
import json, sys
data = json.load(open(sys.argv[1]))
summary = data.get("summary", {})
if summary.get("total") != 12:
    raise SystemExit(f"matrix total = {summary.get('total')}, want 12")
if summary.get("passed") != 12 or summary.get("result") != "pass":
    raise SystemExit(f"matrix did not pass: {summary!r}")
for flow in data.get("flows", []):
    for field in ("ping", "sourceIpPreserved", "defaultGwUnchanged", "noNat", "result"):
        if flow.get(field) != "pass":
            raise SystemExit(f"flow {flow.get('src')}->{flow.get('dst')} {field}={flow.get(field)!r}")
PY

parallel_matrix_json="$tmp/connectivity-matrix-parallel.json"
MATRIX_RUNNER="$runner" "$SCRIPT_DIR/cloudedge-connectivity-matrix.sh" \
  --parallel 3 \
  --phase normal \
  --out "$parallel_matrix_json" >/dev/null
python3 - "$matrix_json" "$parallel_matrix_json" <<'PY'
import json, sys
serial = json.load(open(sys.argv[1]))
parallel = json.load(open(sys.argv[2]))
if parallel["summary"].get("parallelism") != 3:
    raise SystemExit(f"parallelism={parallel['summary'].get('parallelism')!r}, want 3")
if parallel["summary"].get("phase") != "normal":
    raise SystemExit(f"phase={parallel['summary'].get('phase')!r}, want normal")
serial_pairs = [(f["src"], f["dst"]) for f in serial["flows"]]
parallel_pairs = [(f["src"], f["dst"]) for f in parallel["flows"]]
if parallel_pairs != serial_pairs:
    raise SystemExit("parallel matrix output order changed")
if parallel["summary"]["result"] != "pass" or parallel["summary"]["passed"] != 12:
    raise SystemExit(f"parallel matrix did not pass: {parallel['summary']!r}")
PY

cloud_ingress_json="$tmp/connectivity-matrix-cloud-ingress.json"
MATRIX_RUNNER="$runner" "$SCRIPT_DIR/cloudedge-connectivity-matrix.sh" \
  --parallel 2 \
  --phase cloud-ingress \
  --out "$cloud_ingress_json" >/dev/null

order_state="$tmp/state-order"
run_id_order=$(CE_STATE_DIR="$order_state" CE_DRY_RUN=1 \
  "$SCRIPT_DIR/cloudedge-labctl.sh" up \
    --profile full \
    --provider-order cost-optimized \
    --ttl 1h 2>/dev/null)
providers_order=$(sed -n 's/^providers=//p' "$order_state/$run_id_order.manifest")
provider_order_mode=$(sed -n 's/^provider_order=//p' "$order_state/$run_id_order.manifest")
if [[ "$providers_order" != "onprem,oci,aws,azure" ]]; then
  die "cost-optimized provider order = $providers_order, want onprem,oci,aws,azure"
fi
if [[ "$provider_order_mode" != "cost-optimized" ]]; then
  die "provider_order manifest value = $provider_order_mode, want cost-optimized"
fi
if CE_STATE_DIR="$order_state" CE_DRY_RUN=1 \
   "$SCRIPT_DIR/cloudedge-labctl.sh" up \
     --profile full \
     --provider-order unknown \
     --ttl 1h >/dev/null 2>&1; then
  die "invalid provider-order unexpectedly passed"
fi

identity_runner="$tmp/identity-matrix-runner"
cat > "$identity_runner" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
op=$1
src=$2
dst_site=${4:-}
case "$src" in
  onprem) src_ip=10.77.60.10; src_host=onprem-client ;;
  aws) src_ip=10.77.60.11; src_host=aws-client ;;
  *) exit 2 ;;
esac
case "$dst_site" in
  onprem) dst_host=onprem-client ;;
  aws) dst_host=aws-client ;;
  *) dst_host=unknown ;;
esac
case "$op" in
  ping) exit 0 ;;
  ssh)
    printf 'src_hostname=%s\n' "$src_host"
    printf 'src_hostkey_sha256=SHA256:source\n'
    printf 'dst_hostname=%s\n' "$dst_host"
    printf 'dst_hostkey_sha256=SHA256:dest\n'
    printf 'peer_ip=%s\n' "$src_ip"
    printf 'default_gw=10.77.60.1\n'
    ;;
  *) exit 2 ;;
esac
SH
chmod +x "$identity_runner"

identity_json="$tmp/connectivity-matrix-identity.json"
CE_ONPREM_CLIENT_EXPECT_HOSTNAME=onprem-client \
CE_AWS_CLIENT_EXPECT_HOSTNAME=aws-client \
MATRIX_RUNNER="$identity_runner" \
  "$SCRIPT_DIR/cloudedge-connectivity-matrix.sh" \
    --sites "onprem=10.77.60.10,aws=10.77.60.11" \
    --out "$identity_json" >/dev/null
python3 - "$identity_json" <<'PY'
import json, sys
data = json.load(open(sys.argv[1]))
if data["summary"]["result"] != "pass":
    raise SystemExit(f"identity matrix did not pass: {data['summary']!r}")
for flow in data["flows"]:
    if flow.get("identityCheck") != "pass":
        raise SystemExit(f"identityCheck = {flow.get('identityCheck')!r}")
    if not flow.get("srcHostname") or not flow.get("dstHostname"):
        raise SystemExit(f"missing identity evidence: {flow!r}")
PY

identity_fail_json="$tmp/connectivity-matrix-identity-fail.json"
if CE_ONPREM_CLIENT_EXPECT_HOSTNAME=onprem-client \
   CE_AWS_CLIENT_EXPECT_HOSTNAME=wrong-aws-client \
   MATRIX_RUNNER="$identity_runner" \
     "$SCRIPT_DIR/cloudedge-connectivity-matrix.sh" \
       --sites "onprem=10.77.60.10,aws=10.77.60.11" \
       --out "$identity_fail_json" >/dev/null 2>&1; then
  die "identity mismatch matrix unexpectedly passed"
fi
python3 - "$identity_fail_json" <<'PY'
import json, sys
data = json.load(open(sys.argv[1]))
if data["summary"]["result"] != "fail":
    raise SystemExit(f"identity mismatch did not fail: {data['summary']!r}")
if not any(flow.get("identityCheck") == "fail" and "wrong-aws-client" in flow.get("dstIdentityError", "") for flow in data["flows"]):
    raise SystemExit(f"identity mismatch was not recorded: {data['flows']!r}")
PY

out="$tmp/evidence"
run_id="20260601T000000Z-cloudedge-d3-4site-directed-matrix"
MATRIX_RUNNER="$runner" CE_STATE_DIR="$tmp/state" \
  "$SCRIPT_DIR/cloudedge-acceptance.sh" run \
    --scenario d3-4site-directed-matrix \
    --out "$out" \
    --run-id "$run_id" \
    --commit offline \
    --result pass >/dev/null

python3 - "$out/result.json" "$REPO_ROOT/scripts/cloudedge-evidence-schema.json" <<'PY'
import json, sys
result = json.load(open(sys.argv[1]))
schema = json.load(open(sys.argv[2]))
try:
    import jsonschema
except Exception:
    jsonschema = None
if jsonschema is not None:
    jsonschema.validate(instance=result, schema=schema)
if set(result.get("providers", {})) != {"aws", "oci", "azure", "onprem"}:
    raise SystemExit("providers object is not normalized")
assertions = {a.get("name"): a.get("result") for a in result.get("assertions", [])}
for name in (
    "directed_ping_ssh_matrix",
    "source_ip_preserved",
    "default_gateway_unchanged",
    "no_nat",
):
    if assertions.get(name) != "pass":
        raise SystemExit(f"{name}={assertions.get(name)!r}, want pass")
for name in (
    "ownership_epoch_bumped",
    "allow_reassignment_maintained_until_success",
    "old_holder_residue_absent",
    "stale_action_fenced",
):
    if name not in assertions:
        raise SystemExit(f"missing assertion {name}")
PY

timing_decomp_out="$tmp/evidence-timing-decomp"
"$SCRIPT_DIR/cloudedge-labctl.sh" evidence collect \
  --out "$timing_decomp_out" \
  --run-id "$run_id" \
  --scenario d3-4site-directed-matrix \
  --commit offline \
  --matrix-json "$parallel_matrix_json" \
  --cloud-ingress-matrix-json "$cloud_ingress_json" >/dev/null
python3 - "$timing_decomp_out/result.json" <<'PY'
import json, sys
result = json.load(open(sys.argv[1]))
qt = result.get("qualificationTimings", {})
normal = qt.get("normalMatrix", {})
cloud = qt.get("cloudIngressMatrix", {})
if normal.get("phase") != "normal" or normal.get("parallelism") != 3:
    raise SystemExit(f"normal matrix timing not preserved: {normal!r}")
if cloud.get("phase") != "cloud-ingress" or cloud.get("parallelism") != 2:
    raise SystemExit(f"cloud-ingress timing not preserved: {cloud!r}")
if not isinstance(qt.get("dataplaneProbeSeconds"), (int, float)):
    raise SystemExit(f"dataplaneProbeSeconds missing: {qt!r}")
PY

timing_out="$tmp/evidence-timing"
timing_run_id="20260601T000001Z-cloudedge-d6-azure-active-stop-seize"
MATRIX_RUNNER="$runner" FAILOVER_TIMING_RUNNER="$timing_runner" CE_STATE_DIR="$tmp/state-timing" \
  "$SCRIPT_DIR/cloudedge-acceptance.sh" run \
    --scenario d6-azure-active-stop-seize \
    --out "$timing_out" \
    --run-id "$timing_run_id" \
    --commit offline >/dev/null

python3 - "$timing_out/result.json" <<'PY'
import json, sys
result = json.load(open(sys.argv[1]))
assertions = {a.get("name"): a.get("result") for a in result.get("assertions", [])}
if assertions.get("failover_recovery_under_60s") != "pass":
    raise SystemExit("timing assertion did not pass")
events = result.get("timings", {}).get("events", [])
if not events:
    raise SystemExit("timing events missing")
event = events[0]
for field in ("detectionSeconds", "switchoverSeconds", "recoverySeconds"):
    if not isinstance(event.get(field), (int, float)):
        raise SystemExit(f"{field} is not numeric")
if event.get("recoverySeconds") >= 60:
    raise SystemExit("offline recovery unexpectedly exceeded threshold")
PY

protocol_out="$tmp/evidence-protocol"
protocol_run_id="20260601T000002Z-cloudedge-d11-protocol-transparency"
MATRIX_RUNNER="$runner" PROTOCOL_PROBE_RUNNER="$protocol_runner" CE_STATE_DIR="$tmp/state-protocol" \
  "$SCRIPT_DIR/cloudedge-acceptance.sh" run \
    --scenario d11-protocol-transparency \
    --out "$protocol_out" \
    --run-id "$protocol_run_id" \
    --commit offline >/dev/null

python3 - "$protocol_out/result.json" <<'PY'
import json, sys
result = json.load(open(sys.argv[1]))
assertions = {a.get("name"): a.get("result") for a in result.get("assertions", [])}
for name in (
    "protocol_transparency",
    "ftp_active_passive",
    "nfs_rpc",
    "bulk_transfer_pmtu",
    "protocol_source_ip_preserved",
    "protocol_no_nat",
):
    if assertions.get(name) != "pass":
        raise SystemExit(f"{name}={assertions.get(name)!r}, want pass")
protocols = result.get("protocols", {})
if protocols.get("status") != "pass" or len(protocols.get("pairs", [])) != 2:
    raise SystemExit("protocol probe summary missing or failed")
PY

l2_out="$tmp/evidence-l2"
l2_run_id="20260601T000003Z-cloudedge-d12-l2-loop-stp-stability"
MATRIX_RUNNER="$runner" FAILOVER_TIMING_RUNNER="$timing_runner" L2_LOOP_RUNNER="$l2_runner" CE_STATE_DIR="$tmp/state-l2" \
  "$SCRIPT_DIR/cloudedge-acceptance.sh" run \
    --scenario d12-l2-loop-stp-stability \
    --out "$l2_out" \
    --run-id "$l2_run_id" \
    --commit offline >/dev/null

python3 - "$l2_out/result.json" <<'PY'
import json, sys
result = json.load(open(sys.argv[1]))
assertions = {a.get("name"): a.get("result") for a in result.get("assertions", [])}
for name in (
    "l2_loop_free",
    "broadcast_storm_absent",
    "stp_rstp_stable",
    "mac_flap_absent",
    "failover_ping_stable",
    "l2_suppression_mechanism_recorded",
):
    if assertions.get(name) != "pass":
        raise SystemExit(f"{name}={assertions.get(name)!r}, want pass")
l2 = result.get("l2Loop", {})
if l2.get("status") != "pass" or len(l2.get("phases", [])) != 2:
    raise SystemExit("L2 loop probe summary missing or failed")
if "vrrp-single-master" not in l2.get("mechanism", ""):
    raise SystemExit("L2 suppression mechanism was not recorded")
PY

fail_runner="$tmp/matrix-runner-fail"
cat > "$fail_runner" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
op=$1
src=$2
dst_ip=$3
case "$src" in
  onprem) src_ip=10.77.60.10 ;;
  aws) src_ip=10.77.60.11 ;;
  azure) src_ip=10.77.60.12 ;;
  oci) src_ip=10.77.60.13 ;;
  *) exit 2 ;;
esac
if [[ "$op" == "ping" && "$src" == "onprem" && "$dst_ip" == "10.77.60.11" ]]; then
  exit 1
fi
case "$op" in
  ping) exit 0 ;;
  ssh)
    printf 'peer_ip=%s\n' "$src_ip"
    printf 'default_gw=10.77.60.1\n'
    ;;
  *) exit 2 ;;
esac
SH
chmod +x "$fail_runner"

fail_out="$tmp/evidence-fail"
set +e
MATRIX_RUNNER="$fail_runner" CE_STATE_DIR="$tmp/state-fail" \
  "$SCRIPT_DIR/cloudedge-acceptance.sh" run \
    --scenario d3-4site-directed-matrix \
    --out "$fail_out" \
    --run-id "$run_id" \
    --commit offline >/dev/null
fail_status=$?
set -e
if [[ "$fail_status" -eq 0 ]]; then
  die "failing matrix unexpectedly returned success"
fi
python3 - "$fail_out/result.json" <<'PY'
import json, sys
result = json.load(open(sys.argv[1]))
if result.get("result") != "fail":
    raise SystemExit(f"result={result.get('result')!r}, want fail")
assertions = {a.get("name"): a.get("result") for a in result.get("assertions", [])}
if assertions.get("directed_ping_ssh_matrix") != "fail":
    raise SystemExit("directed matrix failure was not preserved in evidence")
PY

printf 'cloudedge acceptance offline OK\n'
