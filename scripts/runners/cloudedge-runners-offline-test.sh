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
  cloudedge-l2-runner.sh; do
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

# Keep this single-quoted so the child runner expands its own CE_PROTOCOL_* env.
# shellcheck disable=SC2016
protocol_ok='printf "bytes=${CE_PROTOCOL_BYTES:-0}\ndetail=${CE_PROTOCOL_OP}_ok\n"'
CE_PROTOCOL_SETUP_COMMAND='printf "detail=setup_ok\n"' \
CE_PROTOCOL_FTP_ACTIVE_COMMAND="$protocol_ok" \
CE_PROTOCOL_FTP_PASSIVE_COMMAND="$protocol_ok" \
CE_PROTOCOL_NFS_COMMAND="$protocol_ok" \
CE_PROTOCOL_RPC_COMMAND='printf "dynamic_port=32768\ndetail=rpc_ok\n"' \
CE_PROTOCOL_BULK_COMMAND="$protocol_ok" \
CE_PROTOCOL_PMTU_COMMAND='printf "overlay_mtu=1380\nmss_clamp=1340\ndetail=pmtu_ok\n"' \
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
    CE_PROTOCOL_PMTU_COMMAND='printf "overlay_mtu=1380\nmss_clamp=1340\ndetail=pmtu_ok\n"' \
    CE_PROTOCOL_SOURCE_PRESERVED_COMMAND='printf "peer_ip=10.77.60.11\ndetail=source_ok\n"' \
    CE_PROTOCOL_NO_NAT_COMMAND='printf "detail=no_nat_ok\n"' \
    "$SCRIPT_DIR/cloudedge-protocol-runner.sh" "$op" aws azure 1024 >/dev/null
done

protocol_json="$tmp/protocol-probe.json"
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
PY

printf 'cloudedge runners offline OK\n'
