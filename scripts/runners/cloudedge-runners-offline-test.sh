#!/usr/bin/env bash
# Offline contract test for CloudEdge live runners. No cloud or SSH access.

set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

die() { printf '%s: %s\n' "$SELF" "$*" >&2; exit 1; }

for script in \
  cloudedge-matrix-runner.sh \
  cloudedge-failover-runner.sh \
  cloudedge-protocol-runner.sh \
  cloudedge-l2-runner.sh \
  cloudedge-capture-runner.sh; do
  "$SCRIPT_DIR/$script" --help >/dev/null
done

tmp=$(mktemp -d "${TMPDIR:-/tmp}/cloudedge-runners.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

fake_matrix="$tmp/fake-matrix"
cat > "$fake_matrix" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  ping) exit 0 ;;
  ssh)
    src=$2
    case "$src" in
      onprem) ip=10.77.60.10 ;;
      aws) ip=10.77.60.11 ;;
      azure) ip=10.77.60.12 ;;
      oci) ip=10.77.60.13 ;;
      *) ip=10.77.60.254 ;;
    esac
    printf 'peer_ip=%s\n' "$ip"
    printf 'default_gw=10.77.60.1\n'
    ;;
  *) exit 2 ;;
esac
SH
chmod +x "$fake_matrix"

CE_AWS_INJECT_COMMAND='printf "injected=aws\n"' \
CE_AWS_DETECTION_COMMAND='printf "detected=1\n"' \
CE_AWS_SWITCHOVER_COMMAND='printf "switched=1\n"' \
CE_AWS_RECOVERY_COMMAND='printf "recovered=1\n"' \
  "$SCRIPT_DIR/cloudedge-failover-runner.sh" inject aws stop-active >/dev/null
CE_AWS_DETECTION_COMMAND='printf "detected=1\n"' \
  "$SCRIPT_DIR/cloudedge-failover-runner.sh" observe aws detection >/dev/null
CE_AWS_SWITCHOVER_COMMAND='printf "switched=1\n"' \
  "$SCRIPT_DIR/cloudedge-failover-runner.sh" observe aws switchover >/dev/null
CE_AWS_RECOVERY_COMMAND='printf "recovered=1\n"' \
  "$SCRIPT_DIR/cloudedge-failover-runner.sh" observe aws recovery >/dev/null

CE_L2_METRICS_COMMAND='printf "broadcast_pps=1\nstp_tcn_delta=0\nmac_flap_count=0\nping_loss_percent=0\nblocked_ports=1\nbpdu_seen=true\nmechanism=offline\n"' \
  "$SCRIPT_DIR/cloudedge-l2-runner.sh" observe before onprem >/dev/null

capture_bundle="$tmp/capture-bundle"
capture_id="20260605-0236-CAP-01"
# Keep these single-quoted so the capture runner expands its own CE_CAPTURE_* env.
# shellcheck disable=SC2016
fake_capture_copy='printf "fake pcap role=%s iface=%s\n" "$CE_CAPTURE_ROLE" "$CE_CAPTURE_IFACE" > "$CE_CAPTURE_PATH"'
# shellcheck disable=SC2016
fake_capture_partial_start='if [ "$CE_CAPTURE_ROLE" = "remote" ]; then exit 42; fi'
CE_CAPTURE_SOURCE_IFACE=eth0 \
CE_CAPTURE_ROUTER_INSIDE_IFACE=br0 \
CE_CAPTURE_ROUTER_OUTSIDE_TUNNEL_IFACE=wg-hybrid \
CE_CAPTURE_REMOTE_IFACE=eth0 \
CE_CAPTURE_START_COMMAND=':' \
CE_CAPTURE_STOP_COMMAND=':' \
CE_CAPTURE_COPY_COMMAND="$fake_capture_copy" \
  "$SCRIPT_DIR/cloudedge-capture-runner.sh" start \
    --test-id "$capture_id" \
    --out "$capture_bundle" \
    --source-site aws \
    --remote-site azure \
    --router-provider onprem \
    --target-ip 10.77.60.9 \
    --ports 22,2049 >/dev/null
CE_CAPTURE_SOURCE_IFACE=eth0 \
CE_CAPTURE_ROUTER_INSIDE_IFACE=br0 \
CE_CAPTURE_ROUTER_OUTSIDE_TUNNEL_IFACE=wg-hybrid \
CE_CAPTURE_REMOTE_IFACE=eth0 \
CE_CAPTURE_START_COMMAND=':' \
CE_CAPTURE_STOP_COMMAND=':' \
CE_CAPTURE_COPY_COMMAND="$fake_capture_copy" \
  "$SCRIPT_DIR/cloudedge-capture-runner.sh" stop \
    --test-id "$capture_id" \
    --out "$capture_bundle" \
    --source-site aws \
    --remote-site azure \
    --router-provider onprem \
    --target-ip 10.77.60.9 \
    --ports 22,2049 >/dev/null

partial_id="20260605-0236-CAP-02"
CE_CAPTURE_SOURCE_IFACE=eth0 \
CE_CAPTURE_ROUTER_INSIDE_IFACE=br0 \
CE_CAPTURE_ROUTER_OUTSIDE_TUNNEL_IFACE=wg-hybrid \
CE_CAPTURE_REMOTE_IFACE=eth0 \
CE_CAPTURE_START_COMMAND="$fake_capture_partial_start" \
CE_CAPTURE_STOP_COMMAND=':' \
CE_CAPTURE_COPY_COMMAND="$fake_capture_copy" \
  "$SCRIPT_DIR/cloudedge-capture-runner.sh" start \
    --test-id "$partial_id" \
    --out "$capture_bundle" \
    --source-site aws \
    --remote-site azure \
    --router-provider onprem \
    --target-ip 10.77.60.9 >/dev/null
CE_CAPTURE_SOURCE_IFACE=eth0 \
CE_CAPTURE_ROUTER_INSIDE_IFACE=br0 \
CE_CAPTURE_ROUTER_OUTSIDE_TUNNEL_IFACE=wg-hybrid \
CE_CAPTURE_REMOTE_IFACE=eth0 \
CE_CAPTURE_START_COMMAND="$fake_capture_partial_start" \
CE_CAPTURE_STOP_COMMAND=':' \
CE_CAPTURE_COPY_COMMAND="$fake_capture_copy" \
  "$SCRIPT_DIR/cloudedge-capture-runner.sh" stop \
    --test-id "$partial_id" \
    --out "$capture_bundle" \
    --source-site aws \
    --remote-site azure \
    --router-provider onprem \
    --target-ip 10.77.60.9 >/dev/null

python3 - "$capture_bundle/05-capture" "$capture_id" "$partial_id" <<'PY'
import json
import sys
from pathlib import Path

cap_dir = Path(sys.argv[1])
capture_id = sys.argv[2]
partial_id = sys.argv[3]
manifest = json.loads((cap_dir / "capture-manifest.json").read_text())
runs = {run["testId"]: run for run in manifest["runs"]}
run = runs[capture_id]
if run["result"] != "PASS":
    raise SystemExit(f"capture run result={run['result']}, want PASS")
if run.get("evidencePhases") != ["CAP", "DP"]:
    raise SystemExit("capture evidence phases missing")
if len(run["points"]) != 4:
    raise SystemExit("capture run did not record four points")
roles = {point["role"] for point in run["points"]}
if roles != {"source", "router-inside", "router-outside-tunnel", "remote"}:
    raise SystemExit(f"bad capture roles: {roles}")
for point in run["points"]:
    filename = point["filename"]
    for token in (capture_id, point["node"], point["role"], point["interface"].replace("/", "_")):
        if token not in filename:
            raise SystemExit(f"{filename} missing token {token}")
    if "10.77.60.9" not in point["filter"] or "arp" not in point["filter"] or "icmp" not in point["filter"] or "port 2049" not in point["filter"]:
        raise SystemExit("capture filter missing target/ARP/ICMP/port evidence")
    if point.get("startExit") != 0 or point.get("stopExit") != 0 or point.get("copyExit") != 0:
        raise SystemExit(f"capture point did not complete cleanly: {point}")
    if not (cap_dir / filename).is_file():
        raise SystemExit(f"missing pcap file {filename}")

partial = runs[partial_id]
if partial["result"] != "PARTIAL":
    raise SystemExit(f"partial run result={partial['result']}, want PARTIAL")
if "remote" not in partial.get("reason", ""):
    raise SystemExit("partial run reason does not identify remote failure")
remote = [point for point in partial["points"] if point["role"] == "remote"][0]
if remote.get("startExit") != 42 or remote.get("copyExit") != 99:
    raise SystemExit(f"partial remote exits not preserved: {remote}")
if (cap_dir / remote["filename"]).exists():
    raise SystemExit("failed remote capture unexpectedly produced a pcap")
PY

# Keep this single-quoted so the child runner expands its own CE_PROTOCOL_* env.
# shellcheck disable=SC2016
protocol_ok='printf "bytes=${CE_PROTOCOL_BYTES:-0}\ndetail=${CE_PROTOCOL_OP}_ok\n"'
CE_PROTOCOL_SETUP_COMMAND='printf "detail=setup_ok\n"' \
CE_PROTOCOL_FTP_ACTIVE_COMMAND="$protocol_ok" \
CE_PROTOCOL_FTP_PASSIVE_COMMAND="$protocol_ok" \
CE_PROTOCOL_NFS_COMMAND="$protocol_ok" \
CE_PROTOCOL_RPC_COMMAND='printf "dynamic_port=32768\ndetail=rpc_ok\n"' \
CE_PROTOCOL_BULK_COMMAND="$protocol_ok" \
CE_PROTOCOL_PMTU_COMMAND='printf "overlay_mtu=1380\nroute_mtu=1380\nroute_advmss=1340\nmss_clamp=1340\ndetail=pmtu_ok\n"' \
CE_PROTOCOL_SOURCE_PRESERVED_COMMAND='printf "peer_ip=10.77.60.11\ndetail=source_ok\n"' \
CE_PROTOCOL_NO_NAT_COMMAND='printf "detail=no_nat_ok\n"' \
  "$SCRIPT_DIR/cloudedge-protocol-runner.sh" setup aws azure 1024 >/dev/null

for op in ftp-active ftp-passive nfs rpc bulk pmtu source-preserved no-nat; do
  env \
    CE_PROTOCOL_FTP_ACTIVE_COMMAND="$protocol_ok" \
    CE_PROTOCOL_FTP_PASSIVE_COMMAND="$protocol_ok" \
    CE_PROTOCOL_NFS_COMMAND="$protocol_ok" \
    CE_PROTOCOL_RPC_COMMAND='printf "dynamic_port=32768\ndetail=rpc_ok\n"' \
    CE_PROTOCOL_BULK_COMMAND="$protocol_ok" \
    CE_PROTOCOL_PMTU_COMMAND='printf "overlay_mtu=1380\nroute_mtu=1380\nroute_advmss=1340\nmss_clamp=1340\ndetail=pmtu_ok\n"' \
    CE_PROTOCOL_SOURCE_PRESERVED_COMMAND='printf "peer_ip=10.77.60.11\ndetail=source_ok\n"' \
    CE_PROTOCOL_NO_NAT_COMMAND='printf "detail=no_nat_ok\n"' \
    "$SCRIPT_DIR/cloudedge-protocol-runner.sh" "$op" aws azure 1024 >/dev/null
done

protocol_json="$tmp/protocol-probe.json"
# Keep this single-quoted so the child runner expands its own CE_PROTOCOL_* env.
# shellcheck disable=SC2016
PROTOCOL_PROBE_RUNNER="$SCRIPT_DIR/cloudedge-protocol-runner.sh" \
CE_PROTOCOL_SETUP_COMMAND='printf "detail=setup_ok\nftp_passive_min=40000\nftp_passive_max=40100\n"' \
CE_PROTOCOL_FTP_ACTIVE_COMMAND="$protocol_ok" \
CE_PROTOCOL_FTP_PASSIVE_COMMAND="$protocol_ok" \
CE_PROTOCOL_NFS_COMMAND="$protocol_ok" \
CE_PROTOCOL_RPC_COMMAND='printf "dynamic_port=32768\ndetail=rpc_ok\n"' \
CE_PROTOCOL_BULK_COMMAND='printf "bytes=${CE_PROTOCOL_BYTES:-0}\nbytes_sent=${CE_PROTOCOL_BYTES:-0}\nretransmits=0\ndetail=bulk_ok\n"' \
CE_PROTOCOL_PMTU_COMMAND='printf "overlay_mtu=1380\nroute_mtu=1380\nroute_advmss=1340\nmss_clamp=1340\ndf_payload_bytes=1300\ndetail=pmtu_ok\n"' \
CE_PROTOCOL_SOURCE_PRESERVED_COMMAND='printf "peer_ip=10.77.60.11\ndetail=source_ok\n"' \
CE_PROTOCOL_NO_NAT_COMMAND='printf "detail=no_nat_ok\n"' \
  "$SCRIPT_DIR/../cloudedge-protocol-probe.sh" \
    --pairs aws:azure \
    --bytes 1024 \
    --out "$protocol_json" >/dev/null

python3 - "$protocol_json" "$SCRIPT_DIR/../cloudedge-protocol-result-schema.json" <<'PY'
import json, sys
data = json.load(open(sys.argv[1]))
schema = json.load(open(sys.argv[2]))
try:
    import jsonschema
except Exception:
    jsonschema = None
if jsonschema is not None:
    jsonschema.validate(instance=data, schema=schema)
if data.get("status") != "pass":
    raise SystemExit("protocol probe did not pass")
pair = data["pairs"][0]
if pair["details"]["rpc"].get("dynamic_port") != 32768:
    raise SystemExit("rpc dynamic port evidence missing")
if pair["details"]["bulkTransfer"].get("retransmits") != 0:
    raise SystemExit("bulk retransmit evidence missing")
if pair["details"]["pmtu"].get("route_advmss") != 1340:
    raise SystemExit("PMTU/advmss evidence missing")
if pair["details"]["pmtu"].get("effective_mss_clamp") != 1340:
    raise SystemExit("effective MSS clamp evidence missing")
if pair["details"]["pmtu"].get("overlay_mtu") != 1380 or pair["details"]["pmtu"].get("route_mtu") != 1380:
    raise SystemExit("MTU evidence missing")
PY

protocol_unknown_json="$tmp/protocol-probe-unknown.json"
PROTOCOL_PROBE_RUNNER="$SCRIPT_DIR/cloudedge-protocol-runner.sh" \
CE_PROTOCOL_SETUP_COMMAND='printf "detail=setup_ok\n"' \
CE_PROTOCOL_FTP_ACTIVE_COMMAND="$protocol_ok" \
CE_PROTOCOL_FTP_PASSIVE_COMMAND="$protocol_ok" \
CE_PROTOCOL_NFS_COMMAND="$protocol_ok" \
CE_PROTOCOL_RPC_COMMAND='printf "dynamic_port=32768\ndetail=rpc_ok\n"' \
CE_PROTOCOL_BULK_COMMAND="$protocol_ok" \
CE_PROTOCOL_PMTU_COMMAND='printf "overlay_mtu=unknown\noverlay_mtu_reason=offline_iface_missing\nroute_mtu=unknown\nroute_mtu_reason=offline_route_no_mtu\nroute_advmss=unknown\nroute_advmss_reason=offline_route_no_advmss\nmss_clamp=unknown\nmss_clamp_reason=offline_no_mss_rule\neffective_mss_clamp=unknown\neffective_mss_clamp_reason=offline_no_mss_rule\ndetail=pmtu_unknown_ok\n"' \
CE_PROTOCOL_SOURCE_PRESERVED_COMMAND='printf "peer_ip=10.77.60.11\ndetail=source_ok\n"' \
CE_PROTOCOL_NO_NAT_COMMAND='printf "detail=no_nat_ok\n"' \
  "$SCRIPT_DIR/../cloudedge-protocol-probe.sh" \
    --pairs aws:azure \
    --bytes 1024 \
    --out "$protocol_unknown_json" >/dev/null

python3 - "$protocol_unknown_json" "$SCRIPT_DIR/../cloudedge-protocol-result-schema.json" <<'PY'
import json, sys
data = json.load(open(sys.argv[1]))
schema = json.load(open(sys.argv[2]))
try:
    import jsonschema
except Exception:
    jsonschema = None
if jsonschema is not None:
    jsonschema.validate(instance=data, schema=schema)
pmtu = data["pairs"][0]["details"]["pmtu"]
for field in ("overlay_mtu", "route_mtu", "route_advmss", "mss_clamp", "effective_mss_clamp"):
    if pmtu.get(field) != "unknown":
        raise SystemExit(f"{field}={pmtu.get(field)!r}, want unknown")
    if not pmtu.get(f"{field}_reason"):
        raise SystemExit(f"{field} unknown without reason")
PY

printf 'cloudedge runners offline OK\n'
