#!/usr/bin/env bash
#
# cloudedge-connectivity-matrix.sh - directed ping+ssh matrix for CloudEdge SAM labs.
#
# Given a list of site->client-ip pairs, runs every directed (src != dst) flow and
# asserts the three SAM data-plane invariants per flow:
#   - source-IP-preserved  : the dst sees the real src client IP (no NAT rewrite).
#   - default-gw-unchanged  : the src client's default gateway is unchanged.
#   - no-NAT                : ping reaches dst and the SSH peer address == src IP.
#
# Output: a single JSON object on stdout (flows[] + summary) consumable by
# `cloudedge-labctl.sh evidence collect`. Human-readable progress goes to stderr.
#
# Runner indirection: all ping/ssh execution goes through MATRIX_RUNNER so the
# matrix logic is unit-runnable WITHOUT a lab. Set MATRIX_RUNNER=<path> to a script
# that emulates a site (used by tests / dry runs). Default runner uses real ssh.
#
set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/runners/cloudedge-runner-lib.sh
. "$SCRIPT_DIR/runners/cloudedge-runner-lib.sh"

usage() {
  cat <<EOF
$SELF - directed ping+ssh connectivity matrix for CloudEdge SAM labs

USAGE:
  $SELF [--sites "site=ip,site=ip,..."] [--out <file>] [--expect-default-gw <cidr-or-ip>]
        [--parallel <n>] [--phase <name>]
  $SELF --help

ARGS:
  --sites STR        Comma list of <site>=<client-ip> pairs. If omitted, read from env:
                     CE_MATRIX_SITES, else the four demo defaults
                     (onprem,aws,azure,oci) from *_CLIENT_IP env vars.
  --out FILE         Write the JSON result to FILE (also echoed to stdout).
  --expect-default-gw VAL
                     Expected default gateway value used for the default-gw-unchanged
                     assertion (default: \$CE_EXPECT_DEFAULT_GW, else "unchanged").
  --parallel N       Run up to N directed flows at once (default:
                     \$CE_MATRIX_PARALLELISM, else 1). Output order stays stable.
  --phase NAME       Label this matrix phase in summary.phase (for example
                     normal or cloud-ingress).
  --help             Show this help and exit 0.

ENV:
  MATRIX_RUNNER      Path to a runner used for execution. Contract:
                       MATRIX_RUNNER ping  <src_site> <dst_ip>            -> exit 0/!0
                       MATRIX_RUNNER ssh   <src_site> <dst_ip>            -> prints:
                           peer_ip=<ip seen by dst>
                           default_gw=<src client default gw>
                     The default runner shells out to ssh/ping using the demo
                     env (SSH_KEY_FILE, *_CLIENT_SSH_HOST, jump hosts).
  CE_MATRIX_SITES    Same format as --sites.
  CE_MATRIX_PARALLELISM
                     Same as --parallel.
  CE_SSH_KNOWN_HOSTS Known-hosts file for SSH host-key verification.
  CE_<SITE>_CLIENT_EXPECT_HOSTNAME
                     Optional expected hostname for source/destination client
                     identity checks. A mismatch fails the flow and is recorded
                     in identityCheck/srcIdentityError/dstIdentityError.

OUTPUT (JSON):
  { "flows": [ {src,dst,dstIp,ping,sourceIpPreserved,defaultGwUnchanged,noNat,identityCheck,result} ],
    "summary": { "total", "passed", "failed", "result", "phase", "parallelism",
                 "elapsedSeconds" } }

EXIT: 0 if every flow passes, 1 if any flow fails, 2 on usage error.
EOF
}

SITES_ARG=""
OUT_FILE=""
EXPECT_GW="${CE_EXPECT_DEFAULT_GW:-unchanged}"
PARALLELISM="${CE_MATRIX_PARALLELISM:-1}"
PHASE="${CE_MATRIX_PHASE:-matrix}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --sites) SITES_ARG="${2:-}"; shift 2 ;;
    --out) OUT_FILE="${2:-}"; shift 2 ;;
    --expect-default-gw) EXPECT_GW="${2:-}"; shift 2 ;;
    --parallel) PARALLELISM="${2:-}"; shift 2 ;;
    --phase) PHASE="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "$SELF: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if ! [[ "$PARALLELISM" =~ ^[0-9]+$ ]] || [[ "$PARALLELISM" -lt 1 ]]; then
  echo "$SELF: bad --parallel: $PARALLELISM (want positive integer)" >&2
  exit 2
fi

# ---- resolve sites -> ip map -------------------------------------------------
declare -a SITE_NAMES=()
declare -a SITE_IPS=()

load_sites_from_spec() {
  local spec=$1 pair name ip
  IFS=',' read -ra _pairs <<<"$spec"
  for pair in "${_pairs[@]}"; do
    [[ -z "$pair" ]] && continue
    name=${pair%%=*}
    ip=${pair#*=}
    name=$(echo "$name" | tr -d '[:space:]')
    ip=$(echo "$ip" | tr -d '[:space:]')
    [[ -z "$name" || -z "$ip" || "$name" == "$ip" ]] && {
      echo "$SELF: bad site spec entry: '$pair' (want site=ip)" >&2; exit 2; }
    SITE_NAMES+=("$name")
    SITE_IPS+=("$ip")
  done
}

if [[ -n "$SITES_ARG" ]]; then
  load_sites_from_spec "$SITES_ARG"
elif [[ -n "${CE_MATRIX_SITES:-}" ]]; then
  load_sites_from_spec "$CE_MATRIX_SITES"
else
  # Demo defaults; absent env vars fall back to the documented logical addresses.
  for entry in \
    "onprem=${ONPREM_CLIENT_IP:-10.77.60.10}" \
    "aws=${AWS_CLIENT_IP:-10.77.60.11}" \
    "azure=${AZURE_CLIENT_IP:-10.77.60.12}" \
    "oci=${OCI_CLIENT_IP:-10.77.60.13}"; do
    SITE_NAMES+=("${entry%%=*}")
    SITE_IPS+=("${entry#*=}")
  done
fi

if [[ ${#SITE_NAMES[@]} -lt 2 ]]; then
  echo "$SELF: need at least 2 sites for a directed matrix" >&2
  exit 2
fi

# ---- default runner (real ssh/ping) ------------------------------------------
# Overridable via MATRIX_RUNNER for unit tests / dry runs.
default_runner() {
  local op=$1 src=$2 dst_ip=$3 dst_site=${4:-}
  local key="${SSH_KEY_FILE:-}" jump user="${CLIENT_SSH_USER:-ubuntu}"
  local known_hosts=${CE_SSH_KNOWN_HOSTS:-${CE_SSH_USER_KNOWN_HOSTS_FILE:-$HOME/.ssh/known_hosts}}
  local strict=${CE_SSH_STRICT_HOST_KEY_CHECKING:-yes}
  local nested_known_hosts=${CE_NESTED_SSH_KNOWN_HOSTS:-}
  [[ -n "$nested_known_hosts" ]] || nested_known_hosts='$HOME/.ssh/known_hosts'
  local nested_strict=${CE_NESTED_SSH_STRICT_HOST_KEY_CHECKING:-yes}
  local ssh_opts=(-o BatchMode=yes -o StrictHostKeyChecking="$strict"
                  -o UserKnownHostsFile="$known_hosts" -o ConnectTimeout=8)
  [[ -n "$key" ]] && ssh_opts=(-i "$key" "${ssh_opts[@]}")

  # Resolve jump host (router/client front door) for the src site from env.
  local jvar="${src^^}_CLIENT_SSH_JUMP"
  jump="${!jvar:-}"
  local srcvar="${src^^}_CLIENT_SSH_HOST"
  local src_host="${!srcvar:-}"
  if [[ -z "$src_host" ]]; then
    echo "$SELF: no ${srcvar} in env for default runner; set MATRIX_RUNNER for offline use" >&2
    return 3
  fi
  local target=("$user@$src_host")
  [[ -n "$jump" ]] && ssh_opts+=(-J "$jump")

  case "$op" in
    ping)
      # $dst_ip intentionally expands on the remote side (it is the ping target).
      # shellcheck disable=SC2029
      ssh "${ssh_opts[@]}" "${target[@]}" "ping -c3 -W2 $dst_ip" >/dev/null 2>&1
      ;;
    ssh)
      # From the src client, SSH to dst_ip and report what the peer (dst) sees and
      # the src client's default gateway. Inner command runs on the remote side.
      local src_expected dst_expected src_identity dst_identity
      src_expected=$(ce_expected_hostname client "$src")
      if [[ -n "$dst_site" ]]; then
        dst_expected=$(ce_expected_hostname client "$dst_site")
      else
        dst_expected=""
      fi
      src_identity=$(ce_remote_identity_command "$src_expected")
      dst_identity=$(ce_remote_identity_command "$dst_expected")
      # shellcheck disable=SC2029
      ssh "${ssh_opts[@]}" "${target[@]}" \
        "src_out=\$(bash -lc $src_identity); src_rc=\$?; printf '%s\n' \"\$src_out\" | sed 's/^/src_/'; ssh_rc=0; ssh -o BatchMode=yes -o StrictHostKeyChecking=$nested_strict -o UserKnownHostsFile=$nested_known_hosts -o ConnectTimeout=8 $user@$dst_ip \"dst_out=\\\$(bash -lc $dst_identity); dst_rc=\\\$?; printf '%s\n' \\\"\\\$dst_out\\\" | sed 's/^/dst_/'; echo peer_ip=\\\$(echo \\\$SSH_CONNECTION | awk '{print \\\$1}'); exit \\\$dst_rc\" || ssh_rc=\$?; echo default_gw=\$(ip route show default | awk '{print \$3; exit}'); exit \$((src_rc != 0 ? src_rc : ssh_rc))"
      ;;
    *) echo "$SELF: default runner: unknown op $op" >&2; return 3 ;;
  esac
}

run_op() {
  if [[ -n "${MATRIX_RUNNER:-}" ]]; then
    "$MATRIX_RUNNER" "$@"
  else
    default_runner "$@"
  fi
}

json_escape() { printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'; }

now_epoch() { date -u +%s; }

write_flow_result() {
  local out=$1 src=$2 src_ip=$3 dst=$4 dst_ip=$5
  local ping_res peer_ip default_gw ssh_out ssh_rc src_hostname dst_hostname
  local src_hostkey dst_hostkey src_identity_error dst_identity_error
  local src_pres no_nat gw_ok identity_ok src_expected dst_expected flow_res

  echo "matrix: $src ($src_ip) -> $dst ($dst_ip)" >&2

  ping_res="fail"
  if run_op ping "$src" "$dst_ip"; then ping_res="pass"; fi

  peer_ip=""
  default_gw=""
  ssh_out=""
  ssh_rc=0
  ssh_out=$(run_op ssh "$src" "$dst_ip" "$dst" 2>/dev/null) || ssh_rc=$?
  peer_ip=$(echo "$ssh_out" | sed -n 's/^peer_ip=//p' | head -n1)
  default_gw=$(echo "$ssh_out" | sed -n 's/^default_gw=//p' | head -n1)
  src_hostname=$(echo "$ssh_out" | sed -n 's/^src_hostname=//p' | head -n1)
  dst_hostname=$(echo "$ssh_out" | sed -n 's/^dst_hostname=//p' | head -n1)
  src_hostkey=$(echo "$ssh_out" | sed -n 's/^src_hostkey_sha256=//p' | head -n1)
  dst_hostkey=$(echo "$ssh_out" | sed -n 's/^dst_hostkey_sha256=//p' | head -n1)
  src_identity_error=$(echo "$ssh_out" | sed -n 's/^src_identity_error=//p' | head -n1)
  dst_identity_error=$(echo "$ssh_out" | sed -n 's/^dst_identity_error=//p' | head -n1)

  src_pres="fail"
  if [[ -n "$peer_ip" && "$peer_ip" == "$src_ip" ]]; then src_pres="pass"; fi
  no_nat="$src_pres"

  gw_ok="fail"
  if [[ "$EXPECT_GW" == "unchanged" ]]; then
    [[ -n "$default_gw" ]] && gw_ok="pass"
  else
    [[ "$default_gw" == "$EXPECT_GW" ]] && gw_ok="pass"
  fi

  identity_ok="pass"
  src_expected=$(ce_expected_hostname client "$src")
  dst_expected=$(ce_expected_hostname client "$dst")
  if [[ -n "$src_identity_error" || -n "$dst_identity_error" ]]; then
    identity_ok="fail"
  fi
  if [[ -n "$src_expected" && "$src_hostname" != "$src_expected" ]]; then
    identity_ok="fail"
    [[ -n "$src_identity_error" ]] || src_identity_error="hostname mismatch: got ${src_hostname:-<empty>} want $src_expected"
  fi
  if [[ -n "$dst_expected" && "$dst_hostname" != "$dst_expected" ]]; then
    identity_ok="fail"
    [[ -n "$dst_identity_error" ]] || dst_identity_error="hostname mismatch: got ${dst_hostname:-<empty>} want $dst_expected"
  fi

  flow_res="fail"
  if [[ "$ping_res" == "pass" && "$src_pres" == "pass" && "$gw_ok" == "pass" && "$no_nat" == "pass" && "$identity_ok" == "pass" && "$ssh_rc" -eq 0 ]]; then
    flow_res="pass"
  fi

  cat > "$out" <<EOF
{"src":"$(json_escape "$src")","dst":"$(json_escape "$dst")","dstIp":"$(json_escape "$dst_ip")","srcIp":"$(json_escape "$src_ip")","peerIp":"$(json_escape "$peer_ip")","defaultGw":"$(json_escape "$default_gw")","srcHostname":"$(json_escape "$src_hostname")","dstHostname":"$(json_escape "$dst_hostname")","srcHostKeySHA256":"$(json_escape "$src_hostkey")","dstHostKeySHA256":"$(json_escape "$dst_hostkey")","srcIdentityError":"$(json_escape "$src_identity_error")","dstIdentityError":"$(json_escape "$dst_identity_error")","ping":"$ping_res","sourceIpPreserved":"$src_pres","defaultGwUnchanged":"$gw_ok","noNat":"$no_nat","identityCheck":"$identity_ok","result":"$flow_res"}
EOF
}

wait_for_slot() {
  while [[ "$(jobs -rp | wc -l)" -ge "$PARALLELISM" ]]; do
    wait -n
  done
}

# ---- run the matrix ----------------------------------------------------------
start_epoch=$(now_epoch)
RESULT_DIR=$(mktemp -d "${TMPDIR:-/tmp}/cloudedge-matrix.XXXXXX")
cleanup_matrix() { rm -rf "$RESULT_DIR"; }
trap cleanup_matrix EXIT

total=0; passed=0; failed=0

for i in "${!SITE_NAMES[@]}"; do
  for j in "${!SITE_NAMES[@]}"; do
    [[ "$i" == "$j" ]] && continue
    src=${SITE_NAMES[$i]}
    src_ip=${SITE_IPS[$i]}
    dst=${SITE_NAMES[$j]}
    dst_ip=${SITE_IPS[$j]}
    total=$((total + 1))
    wait_for_slot
    write_flow_result "$RESULT_DIR/$total.json" "$src" "$src_ip" "$dst" "$dst_ip" &
  done
done

wait

FLOWS_JSON=""
for idx in $(seq 1 "$total"); do
  flow_json=$(cat "$RESULT_DIR/$idx.json")
  if echo "$flow_json" | grep -q '"result":"pass"'; then
    passed=$((passed + 1))
  else
    failed=$((failed + 1))
  fi
  [[ -n "$FLOWS_JSON" ]] && FLOWS_JSON+=","
  FLOWS_JSON+="$flow_json"
done

summary_res="pass"
[[ "$failed" -gt 0 ]] && summary_res="fail"
end_epoch=$(now_epoch)
elapsed=$((end_epoch - start_epoch))

RESULT_JSON=$(cat <<EOF
{"flows":[${FLOWS_JSON}],"summary":{"total":${total},"passed":${passed},"failed":${failed},"result":"${summary_res}","phase":"$(json_escape "$PHASE")","parallelism":${PARALLELISM},"elapsedSeconds":${elapsed}}}
EOF
)

if [[ -n "$OUT_FILE" ]]; then
  printf '%s\n' "$RESULT_JSON" > "$OUT_FILE"
fi
printf '%s\n' "$RESULT_JSON"

echo "matrix summary: $passed/$total passed, result=$summary_res, phase=$PHASE, parallelism=$PARALLELISM, elapsed=${elapsed}s" >&2
[[ "$summary_res" == "pass" ]] || exit 1
exit 0
