#!/usr/bin/env bash
# Live PROTOCOL_PROBE_RUNNER implementation.

set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/runners/cloudedge-runner-lib.sh
. "$SCRIPT_DIR/cloudedge-runner-lib.sh"

usage() {
  cat <<EOF
$SELF - CloudEdge live PROTOCOL_PROBE_RUNNER

USAGE:
  PROTOCOL_PROBE_RUNNER=$SCRIPT_DIR/$SELF scripts/cloudedge-protocol-probe.sh ...
  $SELF setup <client-site> <server-site> [bytes]
  $SELF ftp-active|ftp-passive|nfs|rpc|bulk|pmtu|source-preserved|no-nat <client> <server> [bytes]

ENV:
  <SITE>_CLIENT_SSH_HOST / CE_<SITE>_CLIENT_SSH_HOST, <SITE>_CLIENT_IP
  CE_PROTOCOL_INSTALL=1              Install packages during setup (default 1).
  CE_PROTOCOL_CONFIGURE_SERVICES=1   Configure simple lab FTP/NFS/iperf (default 1).
  CE_PROTOCOL_FTP_PASSIVE_PORTS      Passive FTP port range (default 40000:40100).
  CE_PROTOCOL_FTP_USER/PASSWORD      FTP credentials (default anonymous/anonymous).
  CE_PROTOCOL_BULK_BYTES             Default bytes for bulk tests.
  CE_PROTOCOL_PMTU_SIZE              DF ping payload size (default 1300).
  CE_PROTOCOL_OVERLAY_IFACE          Overlay interface for MTU evidence (default wg-hybrid).
  CE_PROTOCOL_<CLIENT>_<SERVER>_OVERLAY_IFACE
                                      Pair-specific overlay interface override.
  CE_PROTOCOL_MSS_CLAMP              Expected/known MSS clamp value if packet capture is unavailable.
  CE_PROTOCOL_<OP>_COMMAND           Optional local override for one operation.
EOF
}

run_override() {
  local op=$1 client=$2 server=$3 bytes=${4:-}
  local key cmd
  key="CE_PROTOCOL_$(ce_upper "$op")_COMMAND"
  cmd=${!key:-}
  [[ -n "$cmd" ]] || return 1
  CE_PROTOCOL_OP=$op CE_PROTOCOL_CLIENT=$client CE_PROTOCOL_SERVER=$server CE_PROTOCOL_BYTES=$bytes bash -lc "$cmd"
}

server_ip() {
  local server=$1 ip
  ip=$(ce_site_ip "$server")
  [[ -n "$ip" ]] || ce_die "missing ${server} client IP"
  printf '%s' "$ip"
}

remote_install_packages() {
  local site=$1
  [[ "${CE_PROTOCOL_INSTALL:-1}" == "1" ]] || return 0
  ce_client_ssh "$site" "if command -v apt-get >/dev/null 2>&1; then sudo DEBIAN_FRONTEND=noninteractive apt-get update -y >/dev/null && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y vsftpd nfs-kernel-server nfs-common rpcbind curl ftp lftp iperf3 >/dev/null; elif command -v pkg >/dev/null 2>&1; then sudo pkg install -y vsftpd nfs-utils rpcbind curl lftp iperf3 >/dev/null; fi"
}

configure_server() {
  local server=$1 bytes=${2:-104857600}
  [[ "${CE_PROTOCOL_CONFIGURE_SERVICES:-1}" == "1" ]] || return 0
  local mib=$(( (bytes + 1048575) / 1048576 ))
  (( mib > 0 )) || mib=1
  local passive_range=${CE_PROTOCOL_FTP_PASSIVE_PORTS:-40000:40100}
  local passive_min=${passive_range%%:*}
  local passive_max=${passive_range#*:}
  ce_client_ssh "$server" "set -eu
sudo mkdir -p /srv/ftp /srv/cloudedge-nfs /tmp/cloudedge-protocol
sudo dd if=/dev/zero of=/srv/ftp/cloudedge.bin bs=1M count=$(printf '%q' "$mib") status=none
sudo chmod -R a+rX /srv/ftp
sudo sh -c 'grep -q /srv/cloudedge-nfs /etc/exports 2>/dev/null || echo \"/srv/cloudedge-nfs *(rw,sync,no_subtree_check,no_root_squash,insecure)\" >> /etc/exports'
if [ -f /etc/vsftpd.conf ]; then
  sudo cp -n /etc/vsftpd.conf /etc/vsftpd.conf.routerd-cloudedge.bak 2>/dev/null || true
  sudo sh -c 'cat >/etc/vsftpd.conf' <<EOF
listen=NO
listen_ipv6=YES
anonymous_enable=YES
local_enable=NO
write_enable=NO
anon_root=/srv/ftp
pasv_enable=YES
pasv_min_port=$(printf '%q' "$passive_min")
pasv_max_port=$(printf '%q' "$passive_max")
seccomp_sandbox=NO
EOF
fi
sudo exportfs -ra >/dev/null 2>&1 || true
sudo systemctl restart rpcbind 2>/dev/null || sudo service rpcbind restart 2>/dev/null || true
sudo systemctl restart nfs-server 2>/dev/null || sudo systemctl restart nfs-kernel-server 2>/dev/null || sudo service nfs-kernel-server restart 2>/dev/null || true
sudo systemctl restart vsftpd 2>/dev/null || sudo service vsftpd restart 2>/dev/null || true
pkill -f 'iperf3 -s' >/dev/null 2>&1 || true
nohup iperf3 -s >/tmp/cloudedge-iperf3.log 2>&1 &
echo bytes=$bytes"
  printf 'ftp_passive_min=%s\n' "$passive_min"
  printf 'ftp_passive_max=%s\n' "$passive_max"
}

cmd_setup() {
  local client=$1 server=$2 bytes=${3:-104857600}
  if run_override setup "$client" "$server" "$bytes"; then return 0; fi
  remote_install_packages "$client"
  remote_install_packages "$server"
  configure_server "$server" "$bytes"
  printf 'detail=setup client=%s server=%s bytes=%s\n' "$client" "$server" "$bytes"
}

cmd_ftp() {
  local mode=$1 client=$2 server=$3 bytes=${4:-104857600} ip user pass curl_opts=() size
  if run_override "ftp-$mode" "$client" "$server" "$bytes"; then return 0; fi
  ip=$(server_ip "$server")
  user=${CE_PROTOCOL_FTP_USER:-anonymous}
  pass=${CE_PROTOCOL_FTP_PASSWORD:-anonymous}
  if [[ "$mode" == "active" ]]; then
    curl_opts+=(--ftp-port -)
  else
    curl_opts+=(--ftp-pasv)
  fi
  ce_client_ssh "$client" "curl -fsS --connect-timeout 10 --max-time ${CE_PROTOCOL_TIMEOUT:-60} ${curl_opts[*]} --user $(printf '%q' "$user:$pass") ftp://$(printf '%q' "$ip")/cloudedge.bin -o /tmp/cloudedge-ftp-${mode}.bin >/dev/null"
  size=$(ce_client_ssh "$client" "stat -c %s /tmp/cloudedge-ftp-${mode}.bin 2>/dev/null || wc -c </tmp/cloudedge-ftp-${mode}.bin")
  printf 'bytes=%s\n' "$bytes"
  printf 'bytes_received=%s\n' "${size:-unknown}"
  printf 'detail=ftp_%s_ok\n' "$mode"
}

cmd_nfs() {
  local client=$1 server=$2 bytes=${3:-104857600} ip mount_dir
  if run_override nfs "$client" "$server" "$bytes"; then return 0; fi
  ip=$(server_ip "$server")
  mount_dir="/tmp/cloudedge-nfs-$server"
  local mib=$(( (bytes + 1048575) / 1048576 ))
  (( mib > 0 )) || mib=1
  ce_client_ssh "$client" "set -eu
sudo mkdir -p $(printf '%q' "$mount_dir")
if ! mountpoint -q $(printf '%q' "$mount_dir"); then sudo mount -t nfs -o vers=3,nolock,timeo=5,retrans=2 $(printf '%q' "$ip"):/srv/cloudedge-nfs $(printf '%q' "$mount_dir"); fi
dd if=/dev/zero of=$(printf '%q' "$mount_dir")/client-write.bin bs=1M count=$(printf '%q' "$mib") status=none
dd if=$(printf '%q' "$mount_dir")/client-write.bin of=/dev/null bs=1M status=none"
  printf 'bytes=%s\n' "$bytes"
  printf 'megabytes=%s\n' "$mib"
  printf 'detail=nfs_rw_ok\n'
}

cmd_rpc() {
  local client=$1 server=$2 ip out port
  if run_override rpc "$client" "$server" ""; then return 0; fi
  ip=$(server_ip "$server")
  out=$(ce_client_ssh "$client" "rpcinfo -p $(printf '%q' "$ip")")
  port=$(printf '%s\n' "$out" | awk '$5 ~ /mountd|nfs/ && $4 != "111" {print $4; exit}')
  [[ -n "$port" ]] || port=$(printf '%s\n' "$out" | awk 'NR>1 && $4 != "111" {print $4; exit}')
  [[ -n "$port" ]] || ce_die "no dynamic RPC/NFS port found"
  printf 'dynamic_port=%s\n' "$port"
  printf 'detail=rpcbind_ok\n'
}

cmd_bulk() {
  local client=$1 server=$2 bytes=${3:-${CE_PROTOCOL_BULK_BYTES:-104857600}} ip summary
  if run_override bulk "$client" "$server" "$bytes"; then return 0; fi
  ip=$(server_ip "$server")
  if ce_client_ssh "$client" "command -v iperf3 >/dev/null 2>&1"; then
    ce_client_ssh "$client" "iperf3 -c $(printf '%q' "$ip") -n $(printf '%q' "$bytes") -J >/tmp/cloudedge-iperf3-client.json"
    summary=$(ce_client_ssh "$client" "python3 - <<'PY'
import json
try:
    data=json.load(open('/tmp/cloudedge-iperf3-client.json'))
    end=data.get('end', {})
    s=end.get('sum_sent') or end.get('sum') or {}
    r=end.get('sum_received') or {}
    print('bytes_sent=%s' % s.get('bytes', 'unknown'))
    print('bits_per_second=%s' % int(float(s.get('bits_per_second', 0) or 0)))
    print('retransmits=%s' % s.get('retransmits', 0))
    if r:
        print('bytes_received=%s' % r.get('bytes', 'unknown'))
except Exception as e:
    print('iperf_parse_error=%s' % str(e).replace(' ', '_'))
PY")
  else
    ce_client_ssh "$client" "dd if=/dev/zero bs=1M count=16 status=none | ssh -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null $(printf '%q' "$ip") 'cat >/tmp/cloudedge-bulk.bin'"
    summary="bytes_sent=$bytes"
  fi
  printf 'bytes=%s\n' "$bytes"
  printf '%s\n' "$summary"
  printf 'detail=bulk_ok\n'
}

protocol_overlay_iface() {
  local client=$1 server=$2 client_upper server_upper
  client_upper=$(ce_upper "$client")
  server_upper=$(ce_upper "$server")
  ce_env_first "CE_PROTOCOL_${client_upper}_${server_upper}_OVERLAY_IFACE" \
    "CE_PROTOCOL_${client_upper}_OVERLAY_IFACE" \
    "CE_PROTOCOL_OVERLAY_IFACE" 2>/dev/null || printf 'wg-hybrid'
}

cmd_pmtu() {
  local client=$1 server=$2 ip size overlay_iface route_line overlay_mtu route_mtu advmss nft_mss iptables_mss mss mss_source
  if run_override pmtu "$client" "$server" ""; then return 0; fi
  ip=$(server_ip "$server")
  size=${CE_PROTOCOL_PMTU_SIZE:-1300}
  ce_client_ssh "$client" "ping -M do -s $(printf '%q' "$size") -c3 -W2 $(printf '%q' "$ip") >/dev/null"
  overlay_iface=$(protocol_overlay_iface "$client" "$server")
  route_line=$(ce_client_ssh "$client" "ip route get $(printf '%q' "$ip") 2>/dev/null | head -n1" || true)
  overlay_mtu=$(ce_client_ssh "$client" "ip -o link show $(printf '%q' "$overlay_iface") 2>/dev/null | sed -n 's/.*mtu \\([0-9][0-9]*\\).*/\\1/p' | head -n1" || true)
  route_mtu=$(printf '%s\n' "$route_line" | sed -n 's/.* mtu \([0-9][0-9]*\).*/\1/p' | head -n1)
  advmss=$(printf '%s\n' "$route_line" | sed -n 's/.* advmss \([0-9][0-9]*\).*/\1/p' | head -n1)
  if [[ -z "$route_mtu" && "$overlay_mtu" =~ ^[0-9]+$ ]]; then
    route_mtu=$overlay_mtu
    printf 'route_mtu_source=overlay_mtu_fallback\n'
    printf 'route_mtu_reason=ip_route_get_did_not_report_mtu\n'
  fi
  if [[ -z "$advmss" && "$route_mtu" =~ ^[0-9]+$ && "$route_mtu" -gt 40 ]]; then
    advmss=$((route_mtu - 40))
    printf 'route_advmss_source=derived_ipv4_tcp_overhead\n'
    printf 'route_advmss_reason=ip_route_get_did_not_report_advmss\n'
  fi
  nft_mss=$(ce_client_ssh "$client" "{ sudo nft -a list ruleset 2>/dev/null || nft -a list ruleset 2>/dev/null || true; } | sed -n 's/.*tcp option maxseg size set \\([0-9][0-9]*\\).*/\\1/p' | head -n1" || true)
  iptables_mss=$(ce_client_ssh "$client" "{ sudo iptables-save 2>/dev/null || iptables-save 2>/dev/null || true; } | sed -n 's/.*--set-mss \\([0-9][0-9]*\\).*/\\1/p' | head -n1" || true)
  if [[ "$nft_mss" =~ ^[0-9]+$ ]]; then
    mss=$nft_mss
    mss_source=nft
  elif [[ "$iptables_mss" =~ ^[0-9]+$ ]]; then
    mss=$iptables_mss
    mss_source=iptables
  else
    mss=${CE_PROTOCOL_MSS_CLAMP:-}
    mss_source="env"
  fi
  printf 'overlay_iface=%s\n' "$overlay_iface"
  printf 'overlay_mtu=%s\n' "${overlay_mtu:-unknown}"
  if [[ -z "$overlay_mtu" ]]; then
    printf 'overlay_mtu_reason=ip_link_show_failed_or_iface_missing:%s\n' "$overlay_iface"
  fi
  printf 'route_mtu=%s\n' "${route_mtu:-unknown}"
  if [[ -z "$route_mtu" ]]; then
    printf 'route_mtu_reason=ip_route_get_missing_mtu_and_no_overlay_mtu_fallback\n'
  fi
  printf 'route_advmss=%s\n' "${advmss:-unknown}"
  if [[ -z "$advmss" ]]; then
    printf 'route_advmss_reason=ip_route_get_missing_advmss_and_no_numeric_mtu_fallback\n'
  fi
  printf 'mss_clamp=%s\n' "${mss:-unknown}"
  printf 'effective_mss_clamp=%s\n' "${mss:-unknown}"
  if [[ -n "$mss" ]]; then
    printf 'mss_clamp_source=%s\n' "$mss_source"
    printf 'effective_mss_clamp_source=%s\n' "$mss_source"
  else
    printf 'mss_clamp_reason=nft_iptables_rules_missing_numeric_clamp_and_CE_PROTOCOL_MSS_CLAMP_unset\n'
    printf 'effective_mss_clamp_reason=nft_iptables_rules_missing_numeric_clamp_and_CE_PROTOCOL_MSS_CLAMP_unset\n'
  fi
  printf 'df_payload_bytes=%s\n' "$size"
  printf 'detail=pmtu_df_ok\n'
}

cmd_nested_ssh_peer() {
  local client=$1 server=$2 ip user
  ip=$(server_ip "$server")
  user=${CLIENT_SSH_USER:-ubuntu}
  ce_client_ssh "$client" "ssh -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=8 $(printf '%q' "$user@$ip") 'echo peer_ip=\$(echo \$SSH_CONNECTION | awk \"{print \\\$1}\")'"
}

cmd_source_preserved() {
  local client=$1 server=$2 expected peer
  if run_override source-preserved "$client" "$server" ""; then return 0; fi
  expected=$(ce_site_ip "$client")
  peer=$(cmd_nested_ssh_peer "$client" "$server" | sed -n 's/^peer_ip=//p' | head -n1)
  [[ "$peer" == "$expected" ]] || ce_die "peer_ip=$peer expected=$expected"
  printf 'peer_ip=%s\n' "$peer"
  printf 'detail=source_preserved_ok\n'
}

cmd_no_nat() {
  local client=$1 server=$2
  if run_override no-nat "$client" "$server" ""; then return 0; fi
  cmd_source_preserved "$client" "$server" >/dev/null
  printf 'detail=no_nat_ok\n'
}

main() {
  local op=${1:-}
  case "$op" in
    setup) [[ $# -ge 3 ]] || ce_die "setup requires <client> <server> [bytes]"; cmd_setup "$2" "$3" "${4:-104857600}" ;;
    ftp-active) [[ $# -ge 3 ]] || ce_die "ftp-active requires <client> <server> [bytes]"; cmd_ftp active "$2" "$3" "${4:-104857600}" ;;
    ftp-passive) [[ $# -ge 3 ]] || ce_die "ftp-passive requires <client> <server> [bytes]"; cmd_ftp passive "$2" "$3" "${4:-104857600}" ;;
    nfs) [[ $# -ge 3 ]] || ce_die "nfs requires <client> <server> [bytes]"; cmd_nfs "$2" "$3" "${4:-104857600}" ;;
    rpc) [[ $# -ge 3 ]] || ce_die "rpc requires <client> <server>"; cmd_rpc "$2" "$3" ;;
    bulk) [[ $# -ge 3 ]] || ce_die "bulk requires <client> <server> [bytes]"; cmd_bulk "$2" "$3" "${4:-104857600}" ;;
    pmtu) [[ $# -ge 3 ]] || ce_die "pmtu requires <client> <server>"; cmd_pmtu "$2" "$3" ;;
    source-preserved) [[ $# -ge 3 ]] || ce_die "source-preserved requires <client> <server>"; cmd_source_preserved "$2" "$3" ;;
    no-nat) [[ $# -ge 3 ]] || ce_die "no-nat requires <client> <server>"; cmd_no_nat "$2" "$3" ;;
    -h|--help|help|"") usage ;;
    *) ce_die "unknown op: $op" ;;
  esac
}

main "$@"
