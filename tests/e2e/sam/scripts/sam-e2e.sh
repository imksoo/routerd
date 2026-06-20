#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  scripts/sam-e2e.sh --tofu-output tofu-output.json --artifact routerd.tar.gz --evidence-dir DIR [options]

Options:
  --ssh-key FILE          Fixed lab SSH key (default: ~/.ssh/routerd-cloudedge-lab-20260529)
  --configs-dir DIR       Use existing generated configs instead of generating into evidence/config-gen
  --skip-deploy           Do not install routerd/configs; useful for diagnostics-only reruns
  --failover-node NODE    Optional router node name; may be repeated. Stops routerd.service and reruns convergence/matrix
  --rejoin-after-failover Restart stopped failover nodes and rerun convergence/matrix
  --load-balance-report   Capture MobilityPool owner-table snapshots after each matrix run
  --skip-legacy-protocols Skip FTP/RPC/NFS/CIFS pseudo-client matrix
  --performance-tests     Run iperf3/ping performance probes between pseudo-clients
  --destroy-cmd CMD       Optional teardown command, for example: 'tofu destroy -auto-approve'

This harness consumes `tofu output -json` from cloudedge-mobility/terraform/envs/sam-e2e.
Pseudo-client to pseudo-client SSH hostname verification is the PASS authority.
USAGE
}

tofu_output=
artifact=
evidence_dir=
ssh_key="${HOME}/.ssh/routerd-cloudedge-lab-20260529"
configs_dir=
skip_deploy=0
failover_nodes=()
stopped_routers=()
rejoin_after_failover=0
load_balance_report=0
legacy_protocols=1
performance_tests=0
destroy_cmd=
overall=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tofu-output) tofu_output="$2"; shift 2 ;;
    --artifact) artifact="$2"; shift 2 ;;
    --evidence-dir) evidence_dir="$2"; shift 2 ;;
    --ssh-key) ssh_key="$2"; shift 2 ;;
    --configs-dir) configs_dir="$2"; shift 2 ;;
    --skip-deploy) skip_deploy=1; shift ;;
    --failover-node) failover_nodes+=("$2"); shift 2 ;;
    --rejoin-after-failover) rejoin_after_failover=1; shift ;;
    --load-balance-report) load_balance_report=1; shift ;;
    --skip-legacy-protocols) legacy_protocols=0; shift ;;
    --performance-tests) performance_tests=1; shift ;;
    --destroy-cmd) destroy_cmd="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$tofu_output" ] || { echo "--tofu-output is required" >&2; exit 2; }
[ -n "$artifact" ] || { echo "--artifact is required" >&2; exit 2; }
[ -n "$evidence_dir" ] || { echo "--evidence-dir is required" >&2; exit 2; }
[ -f "$tofu_output" ] || { echo "tofu output not found: $tofu_output" >&2; exit 2; }
[ -f "$artifact" ] || { echo "artifact not found: $artifact" >&2; exit 2; }
[ -f "$ssh_key" ] || { echo "ssh key not found: $ssh_key" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }

mkdir -p "$evidence_dir"/{preflight,deploy,convergence,matrix,legacy,performance,diagnostics,cleanup,ssh}
cp "$tofu_output" "$evidence_dir/tofu-output.json"
nodes_json="$evidence_dir/nodes.json"
jq '.nodes.value' "$tofu_output" >"$nodes_json"

mapfile -t routers < <(jq -r 'to_entries[] | select(.value.role == "rr" or .value.role == "leaf") | .key' "$nodes_json" | sort)
mapfile -t leaf_routers < <(jq -r 'to_entries[] | select(.value.role == "leaf") | .key' "$nodes_json" | sort)
mapfile -t clients < <(jq -r 'to_entries[] | select(.value.role == "client") | .key' "$nodes_json" | sort)
mapfile -t pve_dataplane_nodes < <(jq -r 'to_entries[] | select(.value.site == "pve" and (.value.role == "leaf" or .value.role == "client")) | .key' "$nodes_json" | sort)

[ "${#routers[@]}" -gt 0 ] || { echo "no router nodes found in $nodes_json" >&2; exit 2; }
[ "${#leaf_routers[@]}" -gt 0 ] || { echo "no leaf router nodes found in $nodes_json" >&2; exit 2; }
[ "${#clients[@]}" -gt 1 ] || { echo "at least two client nodes are required in $nodes_json" >&2; exit 2; }

known_hosts="$evidence_dir/ssh/known_hosts"
: >"$known_hosts"

node_field() {
  local node="$1" field="$2"
  jq -r --arg node "$node" --arg field "$field" '.[$node][$field]' "$nodes_json"
}

node_is_stopped() {
  local want="$1" node
  for node in "${stopped_routers[@]}"; do
    [ "$node" = "$want" ] && return 0
  done
  return 1
}

mark_node_running() {
  local want="$1" node next=()
  for node in "${stopped_routers[@]}"; do
    [ "$node" = "$want" ] || next+=("$node")
  done
  stopped_routers=("${next[@]}")
}

ssh_base=(-i "$ssh_key" -o UserKnownHostsFile="$known_hosts" -o StrictHostKeyChecking=yes -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=10 -o ServerAliveCountMax=3)

ssh_node() {
  local node="$1"; shift
  local user host
  user="$(node_field "$node" ssh_user)"
  host="$(node_field "$node" public_ip)"
  ssh "${ssh_base[@]}" "$user@$host" "$@"
}

scp_node() {
  local src="$1" node="$2" dst="$3"
  local user host
  user="$(node_field "$node" ssh_user)"
  host="$(node_field "$node" public_ip)"
  scp -i "$ssh_key" -o UserKnownHostsFile="$known_hosts" -o StrictHostKeyChecking=yes -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=10 -o ServerAliveCountMax=3 "$src" "$user@$host:$dst"
}

record_note() {
  {
    date -u '+timestamp=%Y-%m-%dT%H:%M:%SZ'
    echo "artifact=$artifact"
    sha256sum "$artifact"
    echo "ssh_key=$ssh_key"
    ssh-keygen -lf "${ssh_key}.pub" 2>/dev/null || ssh-keygen -y -f "$ssh_key" | ssh-keygen -lf -
    echo "legacy_protocols=$legacy_protocols"
    echo "performance_tests=$performance_tests"
    echo "rejoin_after_failover=$rejoin_after_failover"
    echo "policy_read=cloudedge-mobility/LAB_POLICY.md and ~/routerd-orchestration.md must be reread before real-machine validation"
  } >"$evidence_dir/run-note.txt"
}

mark_failed() {
  overall=1
  echo "FAIL: $*" >&2
}

preflight() {
  echo "== preflight =="
  for node in "${routers[@]}" "${clients[@]}"; do
    host="$(node_field "$node" public_ip)"
    ssh-keyscan -H "$host" >>"$known_hosts" 2>"$evidence_dir/ssh/${node}.keyscan.err" || true
  done
  for node in "${routers[@]}" "${clients[@]}"; do
    {
      echo "## $node"
      ssh_node "$node" 'hostname; id; ip -br addr; ip route; pgrep -a routerd || true; command -v routerd || true'
    } >"$evidence_dir/preflight/${node}.txt" 2>&1 || {
      echo "$node preflight failed" >&2
      return 1
    }
  done
}

generate_configs() {
  if [ -n "$configs_dir" ]; then
    echo "$configs_dir"
    return
  fi
  local gen_dir="$evidence_dir/config-gen"
  cloudedge-mobility/configs/sam-e2e-generate.sh --tofu-output "$tofu_output" --out-dir "$gen_dir" >"$evidence_dir/deploy/config-generate.log" 2>&1
  echo "$gen_dir/configs"
}

deploy() {
  [ "$skip_deploy" -eq 0 ] || return 0
  local cfg_dir="$1"
  for node in "${routers[@]}"; do
    cfg="$cfg_dir/$node.yaml"
    [ -f "$cfg" ] || { echo "missing config for $node: $cfg" >&2; return 1; }
    {
      echo "## install $node"
      scp_node "$artifact" "$node" /tmp/routerd-sam-e2e.tar.gz
      scp_node "$cfg" "$node" /tmp/router.yaml
      if [ -f "$evidence_dir/config-gen/secrets/eventd-cloudedge.key" ]; then
        scp_node "$evidence_dir/config-gen/secrets/eventd-cloudedge.key" "$node" /tmp/eventd-cloudedge.key
      fi
      ssh_node "$node" 'set -e; rm -rf /tmp/routerd-sam-e2e; mkdir -p /tmp/routerd-sam-e2e; tar -xzf /tmp/routerd-sam-e2e.tar.gz -C /tmp/routerd-sam-e2e; cd /tmp/routerd-sam-e2e; sudo ./install.sh --yes --prefix /usr/local; sudo mkdir -p /usr/local/etc/routerd/secrets; sudo install -m 0600 /tmp/router.yaml /usr/local/etc/routerd/router.yaml; if [ -f /tmp/eventd-cloudedge.key ]; then sudo install -m 0600 /tmp/eventd-cloudedge.key /usr/local/etc/routerd/secrets/eventd-cloudedge.key; fi; sudo systemctl restart routerd.service routerd-bgp.service; sudo systemctl is-active routerd.service routerd-bgp.service'
    } >"$evidence_dir/deploy/${node}.txt" 2>&1
  done
}

setup_pve_dataplane() {
  for node in "${pve_dataplane_nodes[@]}"; do
    ip="$(node_field "$node" private_ip)"
    ssh_node "$node" "set -e; if ! ip -4 addr show dev eth1 | grep -qw '$ip/24'; then sudo ip addr add '$ip/24' dev eth1; fi; ip -br addr show dev eth1" \
      >"$evidence_dir/preflight/${node}-dataplane-ip.txt" 2>&1
  done
}

wait_convergence() {
  local label="$1"
  local deadline=$((SECONDS + 600))
  local ok=0
  local client_ips_json
  client_ips_json="$(jq -c '[to_entries[] | select(.value.role == "client") | .value.private_ip + "/32"]' "$nodes_json")"
  while [ "$SECONDS" -lt "$deadline" ]; do
    ok=1
    for node in "${routers[@]}"; do
      node_is_stopped "$node" && continue
      if ! ssh_node "$node" 'sudo routerctl doctor sam >/tmp/routerd-sam-doctor.txt 2>&1' >/dev/null 2>&1; then
        ok=0
      fi
    done
    for node in "${leaf_routers[@]}"; do
      node_is_stopped "$node" && continue
      if ! ssh_node "$node" "sudo routerctl describe MobilityPool/cloudedge -o json | jq -e --argjson want '$client_ips_json' '
        (.resource.status.ownershipResolverOwnerTable // []) as \$rows
        | (\$rows | map(.address)) as \$have
        | all(\$want[]; . as \$ip | \$have | index(\$ip))
      ' >/dev/null"; then
        ok=0
        continue
      fi
      if ! ssh_node "$node" "sudo routerctl action list -o json 2>/dev/null | jq -e '
        (if type == \"array\" then . else (.items // []) end)
        | map(select(.status == \"pending\" or .status == \"running\"))
        | length == 0
      ' >/dev/null"; then
        ok=0
      fi
    done
    [ "$ok" -eq 1 ] && break
    sleep 10
  done
  for node in "${routers[@]}"; do
    node_is_stopped "$node" && continue
    ssh_node "$node" 'sudo routerctl doctor sam; sudo routerctl get status -o json; ip -br addr; ip route' >"$evidence_dir/convergence/${label}-${node}.txt" 2>&1 || true
  done
  [ "$ok" -eq 1 ]
}

client_matrix() {
  local label="$1"
  local out="$evidence_dir/matrix/$label"
  mkdir -p "$out"
  : >"$out/summary.tsv"
  for src in "${clients[@]}"; do
    for dst in "${clients[@]}"; do
      [ "$src" != "$dst" ] || continue
      src_ip="$(node_field "$src" private_ip)"
      dst_ip="$(node_field "$dst" private_ip)"
      dst_host="$(node_field "$dst" name)"
      src_user="$(node_field "$src" ssh_user)"
      src_public="$(node_field "$src" public_ip)"
      dst_user="$(node_field "$dst" ssh_user)"
      result=PASS
      {
        echo "=== $src -> $dst ==="
        echo "SRC=$src SRCIP=$src_ip DST=$dst DSTIP=$dst_ip"
        echo "## route-get"
        ssh_node "$src" "ip route get '$dst_ip' from '$src_ip'" || result=FAIL
        echo "## ping"
        ssh_node "$src" "ping -I '$src_ip' -c 3 -W 2 '$dst_ip'" || result=FAIL
        echo "## traceroute"
        ssh_node "$src" "timeout 20s sh -c \"traceroute -n -w 2 -q 1 '$dst_ip' || tracepath '$dst_ip'\" || true"
        echo "## ssh-hostname"
        actual="$(ssh -i "$ssh_key" -o UserKnownHostsFile="$known_hosts" -o StrictHostKeyChecking=yes -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=10 -o ServerAliveCountMax=3 "$src_user@$src_public" "ssh -i ~/.ssh/routerd-cloudedge-lab-20260529 -o UserKnownHostsFile=~/.ssh/routerd-e2e-known_hosts -o StrictHostKeyChecking=yes -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=10 -o ServerAliveCountMax=3 '$dst_user@$dst_ip' hostname 2>/dev/null" 2>"$out/${src}_to_${dst}.nested-ssh.stderr" | tail -n 1)" || result=FAIL
        printf '%s\n' "$actual"
        [ "$actual" = "$dst_host" ] || result=FAIL_HOSTNAME
      } >"$out/${src}_to_${dst}.txt" 2>&1 || result=FAIL
      printf '%s\t%s\t%s\n' "$src" "$dst" "$result" >>"$out/summary.tsv"
    done
  done
  ! grep -qv $'\tPASS$' "$out/summary.tsv"
}

setup_client_ssh() {
  local client_known_hosts="$evidence_dir/ssh/client_known_hosts"
  : >"$client_known_hosts"
  for dst in "${clients[@]}"; do
    dst_ip="$(node_field "$dst" private_ip)"
    dst_public="$(node_field "$dst" public_ip)"
    ssh-keyscan -T 10 "$dst_public" 2>"$evidence_dir/ssh/${dst}.client-keyscan.err" \
      | awk -v host="$dst_ip" 'NF >= 3 {$1 = host; print}' >>"$client_known_hosts"
  done
  for client in "${clients[@]}"; do
    client_name="$(node_field "$client" name)"
    scp_node "$ssh_key" "$client" /tmp/routerd-cloudedge-lab-20260529
    scp_node "$client_known_hosts" "$client" /tmp/routerd-e2e-known_hosts
    ssh_node "$client" "set -e; sudo hostnamectl set-hostname '$client_name'; mkdir -p ~/.ssh; install -m 0600 /tmp/routerd-cloudedge-lab-20260529 ~/.ssh/routerd-cloudedge-lab-20260529; install -m 0644 /tmp/routerd-e2e-known_hosts ~/.ssh/routerd-e2e-known_hosts"
  done
}

setup_legacy_protocol_services() {
  [ "$legacy_protocols" -eq 1 ] || return 0
  local node
  for node in "${clients[@]}"; do
    {
      echo "## setup legacy protocol services on $node"
      ssh_node "$node" 'set -euo pipefail
        export DEBIAN_FRONTEND=noninteractive
        if command -v apt-get >/dev/null 2>&1; then
          echo "iperf3 iperf3/start_daemon boolean false" | sudo debconf-set-selections || true
          sudo apt-get update
          sudo apt-get install -y --no-install-recommends curl rpcbind nfs-kernel-server nfs-common samba smbclient cifs-utils vsftpd iperf3
        fi
        sudo mkdir -p /srv/routerd-e2e/ftp/pub /srv/routerd-e2e/nfs /srv/routerd-e2e/cifs /srv/routerd-e2e/http
        printf "ftp probe from %s\n" "$(hostname)" | sudo tee /srv/routerd-e2e/ftp/pub/probe.txt >/dev/null
        printf "nfs probe from %s\n" "$(hostname)" | sudo tee /srv/routerd-e2e/nfs/probe.txt >/dev/null
        printf "cifs probe from %s\n" "$(hostname)" | sudo tee /srv/routerd-e2e/cifs/probe.txt >/dev/null
        sudo chmod 0755 /srv/routerd-e2e /srv/routerd-e2e/ftp
        sudo chmod -R 0777 /srv/routerd-e2e/ftp/pub /srv/routerd-e2e/nfs /srv/routerd-e2e/cifs /srv/routerd-e2e/http

        if sudo iptables -S INPUT >/dev/null 2>&1; then
          sudo iptables -C INPUT -s 10.77.60.0/24 -j ACCEPT >/dev/null 2>&1 || sudo iptables -I INPUT 1 -s 10.77.60.0/24 -j ACCEPT
        fi

        sudo mkdir -p /etc/exports.d
        printf "/srv/routerd-e2e/nfs 10.77.60.0/24(rw,sync,no_subtree_check,no_root_squash,insecure)\n" | sudo tee /etc/exports.d/routerd-e2e.exports >/dev/null
        sudo mkdir -p /etc/nfs.conf.d
        printf "[mountd]\nport=20048\n" | sudo tee /etc/nfs.conf.d/routerd-e2e.conf >/dev/null
        if [ -f /etc/default/nfs-kernel-server ]; then
          if grep -q "^RPCMOUNTDOPTS=" /etc/default/nfs-kernel-server; then
            sudo sed -i "s/^RPCMOUNTDOPTS=.*/RPCMOUNTDOPTS=\"--port 20048\"/" /etc/default/nfs-kernel-server
          else
            printf "RPCMOUNTDOPTS=\"--port 20048\"\n" | sudo tee -a /etc/default/nfs-kernel-server >/dev/null
          fi
        fi
        sudo systemctl enable --now rpcbind >/dev/null 2>&1 || sudo systemctl restart rpcbind
        sudo exportfs -ra
        sudo systemctl restart nfs-server || sudo systemctl restart nfs-kernel-server

        if ! grep -q "^\[routerd_e2e\]" /etc/samba/smb.conf; then
          sudo tee -a /etc/samba/smb.conf >/dev/null <<'"'"'SMBEOF'"'"'

[routerd_e2e]
   path = /srv/routerd-e2e/cifs
   browseable = yes
   read only = no
   guest ok = yes
   force user = nobody
SMBEOF
        fi
        sudo systemctl restart smbd || true
        sudo systemctl restart nmbd || true
        sudo modprobe cifs >/dev/null 2>&1 || true

        sudo tee /etc/vsftpd.conf >/dev/null <<'"'"'VSFTPEOF'"'"'
listen=YES
listen_ipv6=NO
anonymous_enable=YES
anon_root=/srv/routerd-e2e/ftp
no_anon_password=YES
write_enable=YES
anon_upload_enable=YES
anon_mkdir_write_enable=YES
anon_other_write_enable=YES
local_enable=NO
dirmessage_enable=NO
xferlog_enable=YES
connect_from_port_20=YES
seccomp_sandbox=NO
pasv_enable=YES
pasv_min_port=30000
pasv_max_port=30010
VSFTPEOF
        sudo systemctl restart vsftpd
        sudo pkill iperf3 >/dev/null 2>&1 || true
        sudo iperf3 -s -D
        sudo systemctl --no-pager --plain is-active rpcbind || true
        sudo systemctl --no-pager --plain is-active nfs-server nfs-kernel-server smbd vsftpd 2>/dev/null || true
        ss -lntup | grep -E ":(21|111|139|445|2049|20048|5201)\b" || true'
    } >"$evidence_dir/legacy/setup-${node}.txt" 2>&1 || return 1
  done
}

legacy_protocol_matrix() {
  [ "$legacy_protocols" -eq 1 ] || return 0
  local label="$1"
  local out="$evidence_dir/legacy/$label"
  local status=0 src dst src_ip dst_ip src_user src_public result
  mkdir -p "$out"
  : >"$out/summary.tsv"
  for src in "${clients[@]}"; do
    for dst in "${clients[@]}"; do
      [ "$src" != "$dst" ] || continue
      src_ip="$(node_field "$src" private_ip)"
      dst_ip="$(node_field "$dst" private_ip)"
      src_user="$(node_field "$src" ssh_user)"
      src_public="$(node_field "$src" public_ip)"
      result=PASS
      {
        echo "=== legacy $src -> $dst ==="
        echo "SRC=$src SRCIP=$src_ip DST=$dst DSTIP=$dst_ip"
        echo "## rpcinfo"
        ssh_node "$src" "timeout 15s rpcinfo -p '$dst_ip'" || result=FAIL_RPC
        echo "## ftp read"
        ssh_node "$src" "timeout 20s curl -fsS --connect-timeout 10 'ftp://$dst_ip/pub/probe.txt'" || result=FAIL_FTP
        echo "## ftp write"
        ssh_node "$src" "printf 'ftp upload from $src to $dst\n' | timeout 20s curl -fsS --connect-timeout 10 -T - 'ftp://$dst_ip/pub/upload-${src}.txt'" || result=FAIL_FTP
        echo "## nfs mount/read/write"
        timeout 45s ssh -i "$ssh_key" -o UserKnownHostsFile="$known_hosts" -o StrictHostKeyChecking=yes -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=10 -o ServerAliveCountMax=3 "$src_user@$src_public" "set -e; mnt=\$(mktemp -d); trap 'sudo umount \"\$mnt\" >/dev/null 2>&1 || true; rmdir \"\$mnt\" >/dev/null 2>&1 || true' EXIT; sudo timeout 25s mount -t nfs -o vers=3,proto=tcp,timeo=5,retrans=1,mountport=20048 '$dst_ip:/srv/routerd-e2e/nfs' \"\$mnt\"; cat \"\$mnt/probe.txt\"; printf 'nfs write from $src to $dst\n' | sudo tee \"\$mnt/write-${src}.txt\" >/dev/null; test -s \"\$mnt/write-${src}.txt\"" || result=FAIL_NFS
        echo "## cifs mount/read/write"
        timeout 45s ssh -i "$ssh_key" -o UserKnownHostsFile="$known_hosts" -o StrictHostKeyChecking=yes -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=10 -o ServerAliveCountMax=3 "$src_user@$src_public" "set -e; sudo modprobe cifs >/dev/null 2>&1 || true; mnt=\$(mktemp -d); trap 'sudo umount \"\$mnt\" >/dev/null 2>&1 || true; rmdir \"\$mnt\" >/dev/null 2>&1 || true' EXIT; sudo timeout 25s mount -t cifs '//$dst_ip/routerd_e2e' \"\$mnt\" -o guest,vers=3.0; cat \"\$mnt/probe.txt\"; printf 'cifs write from $src to $dst\n' | sudo tee \"\$mnt/write-${src}.txt\" >/dev/null; test -s \"\$mnt/write-${src}.txt\"" || result=FAIL_CIFS
      } >"$out/${src}_to_${dst}.txt" 2>&1 || result=FAIL
      printf '%s\t%s\t%s\n' "$src" "$dst" "$result" >>"$out/summary.tsv"
      [ "$result" = "PASS" ] || status=1
    done
  done
  return "$status"
}

performance_matrix() {
  [ "$performance_tests" -eq 1 ] || return 0
  local label="$1"
  local out="$evidence_dir/performance/$label"
  local status=0 src dst src_ip dst_ip result
  mkdir -p "$out"
  : >"$out/summary.tsv"
  for src in "${clients[@]}"; do
    for dst in "${clients[@]}"; do
      [ "$src" != "$dst" ] || continue
      src_ip="$(node_field "$src" private_ip)"
      dst_ip="$(node_field "$dst" private_ip)"
      result=PASS
      {
        echo "=== performance $src -> $dst ==="
        echo "SRC=$src SRCIP=$src_ip DST=$dst DSTIP=$dst_ip"
        echo "## tcp iperf3"
        ssh_node "$src" "timeout 20s iperf3 -J -c '$dst_ip' -B '$src_ip' -t 5" >"$out/${src}_to_${dst}.iperf3-tcp.json" || result=FAIL_TCP
        echo "## udp iperf3"
        ssh_node "$src" "timeout 20s iperf3 -J -u -b 10M -c '$dst_ip' -B '$src_ip' -t 5" >"$out/${src}_to_${dst}.iperf3-udp.json" || result=FAIL_UDP
        echo "## small packet ping sample"
        ssh_node "$src" "timeout 20s ping -I '$src_ip' -s 56 -c 100 -i 0.01 '$dst_ip'" >"$out/${src}_to_${dst}.ping-pps.txt" || result=FAIL_PING_PPS
      } >"$out/${src}_to_${dst}.txt" 2>&1 || result=FAIL
      printf '%s\t%s\t%s\n' "$src" "$dst" "$result" >>"$out/summary.tsv"
      [ "$result" = "PASS" ] || status=1
    done
  done
  return "$status"
}

run_validation_set() {
  local label="$1"
  local status=0
  wait_convergence "$label" || status=1
  client_matrix "$label" || status=1
  legacy_protocol_matrix "$label" || status=1
  performance_matrix "$label" || status=1
  collect_load_balance_report "$label"
  return "$status"
}

collect_diagnostics() {
  local label="$1"
  local dir="$evidence_dir/diagnostics/$label"
  mkdir -p "$dir"
  for node in "${routers[@]}"; do
    ssh_node "$node" 'hostname; sudo routerctl doctor sam || true; sudo routerctl get status -o json || true; sudo routerctl describe MobilityPool/cloudedge -o json || true; ip -br addr; ip route; journalctl -u routerd.service -u routerd-bgp.service --since "30 minutes ago" --no-pager -n 500' >"$dir/${node}.txt" 2>&1 || true
  done
}

collect_load_balance_report() {
  [ "$load_balance_report" -eq 1 ] || return 0
  local label="$1"
  local dir="$evidence_dir/diagnostics/load-balance-$label"
  mkdir -p "$dir"
  : >"$dir/owner-table.tsv"
  : >"$dir/owner-summary.tsv"
  for node in "${leaf_routers[@]}"; do
    node_is_stopped "$node" && continue
    ssh_node "$node" 'sudo routerctl describe MobilityPool/cloudedge -o json' >"$dir/${node}.json" 2>"$dir/${node}.stderr" || continue
    jq -r --arg node "$node" '
      (.resource.status.ownershipResolverOwnerTable // [])[]
      | [
          $node,
          (.address // ""),
          (.ownerNodeRef // .owner // .ownerNode // ""),
          (.site // .ownerSite // ""),
          (.source // .reason // "")
        ]
      | @tsv
    ' "$dir/${node}.json" >>"$dir/owner-table.tsv" || true
  done
  if [ -s "$dir/owner-table.tsv" ]; then
    {
      echo $'observer\towner\tsite\tcount'
      awk -F '\t' '
      {
        owner = ($3 == "" ? "<unknown>" : $3)
        site = ($4 == "" ? "<unknown>" : $4)
        key = $1 "\t" owner "\t" site
        count[key]++
      }
      END {
        for (key in count) {
          print key "\t" count[key]
        }
      }
      ' "$dir/owner-table.tsv" | sort
    } >"$dir/owner-summary.tsv"
  fi
}

run_failover() {
  local status=0
  [ "${#failover_nodes[@]}" -gt 0 ] || return 0
  for node in "${failover_nodes[@]}"; do
    ssh_node "$node" 'sudo systemctl stop routerd.service routerd-bgp.service' >"$evidence_dir/convergence/failover-stop-${node}.txt" 2>&1
    stopped_routers+=("$node")
    run_validation_set "after-failover-${node}" || status=1
  done
  return "$status"
}

run_rejoin() {
  local status=0 node
  [ "$rejoin_after_failover" -eq 1 ] || return 0
  [ "${#failover_nodes[@]}" -gt 0 ] || return 0
  for node in "${failover_nodes[@]}"; do
    ssh_node "$node" 'sudo systemctl start routerd-bgp.service routerd.service; sudo systemctl is-active routerd.service routerd-bgp.service' >"$evidence_dir/convergence/rejoin-start-${node}.txt" 2>&1 || status=1
    mark_node_running "$node"
    run_validation_set "after-rejoin-${node}" || status=1
  done
  return "$status"
}

teardown() {
  [ -n "$destroy_cmd" ] || return 0
  bash -lc "$destroy_cmd" >"$evidence_dir/cleanup/destroy.txt" 2>&1
}

record_note
preflight || mark_failed "preflight"
if [ "$overall" -eq 0 ]; then
  setup_pve_dataplane || mark_failed "PVE dataplane IP setup"
fi
if [ "$overall" -eq 0 ]; then
  cfg_dir="$(generate_configs)" || mark_failed "config generation"
fi
if [ "$overall" -eq 0 ]; then
  deploy "$cfg_dir" || mark_failed "deploy"
fi
if [ "$overall" -eq 0 ]; then
  setup_client_ssh || mark_failed "client SSH setup"
fi
if [ "$overall" -eq 0 ]; then
  setup_legacy_protocol_services || mark_failed "legacy protocol service setup"
fi
if [ "$overall" -eq 0 ]; then
  run_validation_set "initial" || mark_failed "initial validation set"
fi
collect_diagnostics "post-matrix"
if [ "$overall" -eq 0 ]; then
  run_failover || mark_failed "failover"
fi
if [ "$overall" -eq 0 ]; then
  run_rejoin || mark_failed "rejoin"
fi
teardown || mark_failed "teardown"

echo "evidence: $evidence_dir"
exit "$overall"
