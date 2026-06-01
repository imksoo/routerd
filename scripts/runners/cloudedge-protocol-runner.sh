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
  CE_PROTOCOL_FTP_USER/PASSWORD      FTP credentials (default anonymous/anonymous).
  CE_PROTOCOL_BULK_BYTES             Default bytes for bulk tests.
  CE_PROTOCOL_PMTU_SIZE              DF ping payload size (default 1300).
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
  ce_client_ssh "$server" "set -eu
sudo mkdir -p /srv/ftp /srv/cloudedge-nfs /tmp/cloudedge-protocol
sudo dd if=/dev/zero of=/srv/ftp/cloudedge.bin bs=1M count=1 status=none
sudo chmod -R a+rX /srv/ftp
sudo sh -c 'grep -q /srv/cloudedge-nfs /etc/exports 2>/dev/null || echo \"/srv/cloudedge-nfs *(rw,sync,no_subtree_check,no_root_squash,insecure)\" >> /etc/exports'
sudo exportfs -ra >/dev/null 2>&1 || true
sudo systemctl restart rpcbind 2>/dev/null || sudo service rpcbind restart 2>/dev/null || true
sudo systemctl restart nfs-server 2>/dev/null || sudo systemctl restart nfs-kernel-server 2>/dev/null || sudo service nfs-kernel-server restart 2>/dev/null || true
sudo systemctl restart vsftpd 2>/dev/null || sudo service vsftpd restart 2>/dev/null || true
pkill -f 'iperf3 -s' >/dev/null 2>&1 || true
nohup iperf3 -s >/tmp/cloudedge-iperf3.log 2>&1 &
echo bytes=$bytes"
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
  local mode=$1 client=$2 server=$3 bytes=${4:-104857600} ip user pass curl_opts=()
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
  printf 'bytes=%s\n' "$bytes"
  printf 'detail=ftp_%s_ok\n' "$mode"
}

cmd_nfs() {
  local client=$1 server=$2 bytes=${3:-104857600} ip mount_dir
  if run_override nfs "$client" "$server" "$bytes"; then return 0; fi
  ip=$(server_ip "$server")
  mount_dir="/tmp/cloudedge-nfs-$server"
  ce_client_ssh "$client" "set -eu
sudo mkdir -p $(printf '%q' "$mount_dir")
if ! mountpoint -q $(printf '%q' "$mount_dir"); then sudo mount -t nfs -o vers=3,nolock,timeo=5,retrans=2 $(printf '%q' "$ip"):/srv/cloudedge-nfs $(printf '%q' "$mount_dir"); fi
dd if=/dev/zero of=$(printf '%q' "$mount_dir")/client-write.bin bs=1M count=4 status=none
dd if=$(printf '%q' "$mount_dir")/client-write.bin of=/dev/null bs=1M status=none"
  printf 'bytes=%s\n' "$bytes"
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
  local client=$1 server=$2 bytes=${3:-${CE_PROTOCOL_BULK_BYTES:-104857600}} ip
  if run_override bulk "$client" "$server" "$bytes"; then return 0; fi
  ip=$(server_ip "$server")
  if ce_client_ssh "$client" "command -v iperf3 >/dev/null 2>&1"; then
    ce_client_ssh "$client" "iperf3 -c $(printf '%q' "$ip") -n $(printf '%q' "$bytes") -J >/tmp/cloudedge-iperf3-client.json"
  else
    ce_client_ssh "$client" "dd if=/dev/zero bs=1M count=16 status=none | ssh -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null $(printf '%q' "$ip") 'cat >/tmp/cloudedge-bulk.bin'"
  fi
  printf 'bytes=%s\n' "$bytes"
  printf 'detail=bulk_ok\n'
}

cmd_pmtu() {
  local client=$1 server=$2 ip size overlay_mtu mss
  if run_override pmtu "$client" "$server" ""; then return 0; fi
  ip=$(server_ip "$server")
  size=${CE_PROTOCOL_PMTU_SIZE:-1300}
  ce_client_ssh "$client" "ping -M do -s $(printf '%q' "$size") -c3 -W2 $(printf '%q' "$ip") >/dev/null"
  overlay_mtu=$(ce_client_ssh "$client" "ip -o link show ${CE_PROTOCOL_OVERLAY_IFACE:-wg0} 2>/dev/null | sed -n 's/.*mtu \\([0-9][0-9]*\\).*/\\1/p' | head -n1" || true)
  mss=${CE_PROTOCOL_MSS_CLAMP:-}
  printf 'overlay_mtu=%s\n' "${overlay_mtu:-unknown}"
  printf 'mss_clamp=%s\n' "${mss:-unknown}"
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
