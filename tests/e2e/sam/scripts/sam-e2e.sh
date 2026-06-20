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
  --skip-matrix           Skip SSH hostname matrix; useful when rerunning performance after a clean matrix
  --skip-legacy-protocols Skip FTP/RPC/NFS/CIFS pseudo-client matrix
  --performance-tests     Run SAM iperf3/ping probes, plus public direct comparison for cross-cloud AWS/Azure/OCI pairs
  --failover-transfer-tests Run a throttled client-to-client HTTP transfer during each failover stop
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
skip_matrix=0
legacy_protocols=1
performance_tests=0
failover_transfer_tests=0
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
    --skip-matrix) skip_matrix=1; shift ;;
    --skip-legacy-protocols) legacy_protocols=0; shift ;;
    --performance-tests) performance_tests=1; shift ;;
    --failover-transfer-tests) failover_transfer_tests=1; shift ;;
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

mkdir -p "$evidence_dir"/{preflight,deploy,convergence,matrix,legacy,performance,failover-transfer,provider,diagnostics,cleanup,ssh}
cp "$tofu_output" "$evidence_dir/tofu-output.json"
nodes_json="$evidence_dir/nodes.json"
fabric_json="$evidence_dir/fabric.json"
jq '.nodes.value' "$tofu_output" >"$nodes_json"
jq '.fabric.value' "$tofu_output" >"$fabric_json"

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
  ssh -n "${ssh_base[@]}" "$user@$host" "$@"
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
    echo "failover_transfer_tests=$failover_transfer_tests"
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

collect_provider_inventory() {
  local label="$1"
  local dir="$evidence_dir/provider/$label"
  local aws_region azure_rg azure_route_table oci_region oci_compartment_id oci_route_table_id
  local pve_node pve_capture_bridge node id iface
  local status=0 oci_compartment_name
  mkdir -p "$dir"

  cp "$fabric_json" "$dir/fabric.json"
  jq -r '
    to_entries[]
    | [
        .key,
        (.value.site // ""),
        (.value.role // ""),
        (.value.instance_id // ""),
        (.value.interface_id // ""),
        (.value.vm_id // "")
      ]
    | @tsv
  ' "$nodes_json" >"$dir/nodes.tsv"

  aws_region="$(jq -r '.aws.region // empty' "$fabric_json")"
  if [ -n "$aws_region" ]; then
    if ! command -v aws >/dev/null 2>&1; then
      echo "FAIL: aws CLI is required for AWS provider inventory" >"$dir/aws.txt"
      status=1
    else
    {
      echo "## aws caller identity"
      if ! aws sts get-caller-identity; then
        echo "FAIL: aws CLI authentication failed; check AWS_PROFILE/credentials"
        status=1
      fi
      echo "## aws instances"
      mapfile -t ids < <(jq -r 'to_entries[] | select(.value.site == "aws") | .value.instance_id // empty' "$nodes_json" | sort -u)
      if [ "${#ids[@]}" -gt 0 ]; then
        if ! aws ec2 describe-instances --region "$aws_region" --instance-ids "${ids[@]}"; then
          echo "FAIL: aws EC2 instance inventory failed"
          status=1
        fi
      fi
      echo "## aws network interfaces"
      mapfile -t ifaces < <(jq -r 'to_entries[] | select(.value.site == "aws") | .value.interface_id // empty' "$nodes_json" | sort -u)
      if [ "${#ifaces[@]}" -gt 0 ]; then
        if ! aws ec2 describe-network-interfaces --region "$aws_region" --network-interface-ids "${ifaces[@]}"; then
          echo "FAIL: aws EC2 network-interface inventory failed"
          status=1
        fi
      fi
      echo "## aws route tables"
      if ! aws ec2 describe-route-tables --region "$aws_region" --route-table-ids \
        "$(jq -r '.aws.rr_route_table // empty' "$fabric_json")" \
        "$(jq -r '.aws.leaf_route_table_id // empty' "$fabric_json")"; then
        echo "FAIL: aws EC2 route-table inventory failed"
        status=1
      fi
    } >"$dir/aws.txt" 2>&1
    fi
  fi

  azure_rg="$(jq -r '.azure.resource_group_name // empty' "$fabric_json")"
  azure_route_table="$(jq -r '.azure.route_table_name // empty' "$fabric_json")"
  if [ -n "$azure_rg" ]; then
    if ! command -v az >/dev/null 2>&1; then
      echo "FAIL: az CLI is required for Azure provider inventory" >"$dir/azure.txt"
      status=1
    else
    {
      echo "## azure account"
      if ! az account show --output json; then
        echo "FAIL: azure CLI authentication failed; check az login/subscription"
        status=1
      fi
      echo "## azure vm list"
      if ! az vm list --resource-group "$azure_rg" --show-details --output json; then
        echo "FAIL: azure VM inventory failed"
        status=1
      fi
      echo "## azure nic list"
      if ! az network nic list --resource-group "$azure_rg" --output json; then
        echo "FAIL: azure NIC inventory failed"
        status=1
      fi
      echo "## azure route table"
      if [ -n "$azure_route_table" ]; then
        if ! az network route-table show --resource-group "$azure_rg" --name "$azure_route_table" --output json; then
          echo "FAIL: azure route-table inventory failed"
          status=1
        fi
      fi
      echo "## azure role assignments"
      if ! az role assignment list --resource-group "$azure_rg" --output json; then
        echo "FAIL: azure role-assignment inventory failed"
        status=1
      fi
    } >"$dir/azure.txt" 2>&1
    fi
  fi

  oci_region="$(jq -r '.oci.region // empty' "$fabric_json")"
  oci_compartment_id="$(jq -r '.oci.compartment_id // empty' "$fabric_json")"
  oci_route_table_id="$(jq -r '.oci.route_table_id // empty' "$fabric_json")"
  if [ -n "$oci_region" ] && [ -n "$oci_compartment_id" ]; then
    if ! command -v oci >/dev/null 2>&1; then
      echo "FAIL: oci CLI is required for OCI provider inventory" >"$dir/oci.txt"
      status=1
    else
    {
      echo "## oci compartment"
      if ! oci iam compartment get --region "$oci_region" --compartment-id "$oci_compartment_id"; then
        echo "FAIL: oci CLI authentication or compartment lookup failed"
        status=1
      fi
      oci_compartment_name="$(oci iam compartment get --region "$oci_region" --compartment-id "$oci_compartment_id" --query 'data.name' --raw-output 2>/dev/null || true)"
      echo "oci_compartment_name=$oci_compartment_name"
      if [ "$oci_compartment_name" = "ManagedCompartmentForPaaS" ]; then
        echo "FAIL: OCI compartment must not be ManagedCompartmentForPaaS"
        status=1
      elif [ -z "$oci_compartment_name" ]; then
        echo "FAIL: OCI compartment name is empty; check OCI profile, region, and OCID"
        status=1
      fi
      echo "## oci instances"
      while read -r node id; do
        [ -n "$id" ] || continue
        echo "### $node $id"
        if ! oci compute instance get --region "$oci_region" --instance-id "$id"; then
          echo "FAIL: oci instance inventory failed for $node"
          status=1
        fi
      done < <(jq -r 'to_entries[] | select(.value.site == "oci") | [.key, (.value.instance_id // "")] | @tsv' "$nodes_json")
      echo "## oci vnics"
      while read -r node iface; do
        [ -n "$iface" ] || continue
        echo "### $node $iface"
        if ! oci network vnic get --region "$oci_region" --vnic-id "$iface"; then
          echo "FAIL: oci VNIC inventory failed for $node"
          status=1
        fi
      done < <(jq -r 'to_entries[] | select(.value.site == "oci") | [.key, (.value.interface_id // "")] | @tsv' "$nodes_json")
      echo "## oci route table"
      if [ -n "$oci_route_table_id" ]; then
        if ! oci network route-table get --region "$oci_region" --rt-id "$oci_route_table_id"; then
          echo "FAIL: oci route-table inventory failed"
          status=1
        fi
      fi
    } >"$dir/oci.txt" 2>&1
    fi
  fi

  {
    pve_node="$(jq -r '.pve.node_name // empty' "$fabric_json")"
    pve_capture_bridge="$(jq -r '.pve.capture_bridge // empty' "$fabric_json")"
    echo "pve_node=$pve_node"
    echo "pve_capture_bridge=$pve_capture_bridge"
    jq -r 'to_entries[] | select(.value.site == "pve") | [.key, (.value.role // ""), (.value.vm_id // ""), (.value.private_ip // ""), (.value.public_ip // "")] | @tsv' "$nodes_json"
    if command -v qm >/dev/null 2>&1; then
      while read -r node id; do
        [ -n "$id" ] || continue
        echo "### qm config $node $id"
        qm config "$id" || true
      done < <(jq -r 'to_entries[] | select(.value.site == "pve") | [.key, (.value.vm_id // "")] | @tsv' "$nodes_json")
    fi
  } >"$dir/pve.txt" 2>&1

  return "$status"
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
  local started="$SECONDS"
  local deadline=$((SECONDS + 600))
  local ok=0
  local status_text=TIMEOUT
  local client_ips_json
  client_ips_json="$(jq -c '[to_entries[] | select(.value.role == "client") | .value.private_ip + "/32"]' "$nodes_json")"
  while [ "$SECONDS" -lt "$deadline" ]; do
    ok=1
    for node in "${routers[@]}"; do
      node_is_stopped "$node" && continue
      ssh_node "$node" 'sudo routerctl doctor sam >/tmp/routerd-sam-doctor.txt 2>&1' >/dev/null 2>&1 || true
    done
    for node in "${leaf_routers[@]}"; do
      node_is_stopped "$node" && continue
      if ! ssh_node "$node" "jq -r '.[] | sub(\"/32$\"; \"\")' <<'JSON' | while read -r ip; do ip route get \"\$ip\" >/dev/null || exit 1; done
$client_ips_json
JSON"; then
        ok=0
      fi
    done
    [ "$ok" -eq 1 ] && break
    sleep 10
  done
  [ "$ok" -eq 1 ] && status_text=PASS
  printf '%s\t%s\t%s\n' "$label" "$status_text" "$((SECONDS - started))" >>"$evidence_dir/convergence/summary.tsv"
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
        ssh_node "$src" "ip route get '$dst_ip' from '$src_ip'" || true
        echo "## ping"
        ssh_node "$src" "ping -I '$src_ip' -c 3 -W 2 '$dst_ip'" || true
        echo "## traceroute"
        ssh_node "$src" "timeout 20s sh -c \"traceroute -n -w 2 -q 1 '$dst_ip' || tracepath '$dst_ip'\" || true"
        echo "## ssh-hostname"
        actual=
        for attempt in 1 2 3; do
          actual="$(ssh -i "$ssh_key" -o UserKnownHostsFile="$known_hosts" -o StrictHostKeyChecking=yes -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=10 -o ServerAliveCountMax=3 "$src_user@$src_public" "ssh -i ~/.ssh/routerd-cloudedge-lab-20260529 -o UserKnownHostsFile=~/.ssh/routerd-e2e-known_hosts -o StrictHostKeyChecking=yes -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=10 -o ServerAliveCountMax=3 '$dst_user@$dst_ip' hostname 2>/dev/null" 2>"$out/${src}_to_${dst}.nested-ssh.stderr" | tail -n 1)" || true
          [ "$actual" = "$dst_host" ] && break
          sleep 2
        done
        [ "$actual" = "$dst_host" ] || result=FAIL_HOSTNAME
        printf '%s\n' "$actual"
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
        sudo iperf3 -s -D </dev/null >/dev/null 2>&1
        sudo systemctl --no-pager --plain is-active rpcbind || true
        sudo systemctl --no-pager --plain is-active nfs-server nfs-kernel-server smbd vsftpd 2>/dev/null || true
        ss -lntup | grep -E ":(21|111|139|445|2049|20048|5201)\b" || true'
    } >"$evidence_dir/legacy/setup-${node}.txt" 2>&1 || return 1
  done
}

setup_performance_services() {
  [ "$performance_tests" -eq 1 ] || return 0
  local node
  for node in "${clients[@]}"; do
    {
      echo "## setup performance services on $node"
      ssh_node "$node" 'set -euo pipefail
        export DEBIAN_FRONTEND=noninteractive
        if command -v apt-get >/dev/null 2>&1; then
          echo "iperf3 iperf3/start_daemon boolean false" | sudo debconf-set-selections || true
          sudo apt-get update
          sudo apt-get install -y --no-install-recommends iperf3
        fi
        if sudo iptables -S INPUT >/dev/null 2>&1; then
          sudo iptables -C INPUT -p tcp --dport 5201 -j ACCEPT >/dev/null 2>&1 || sudo iptables -I INPUT 1 -p tcp --dport 5201 -j ACCEPT
          sudo iptables -C INPUT -p udp --dport 5201 -j ACCEPT >/dev/null 2>&1 || sudo iptables -I INPUT 1 -p udp --dport 5201 -j ACCEPT
        fi
        sudo pkill iperf3 >/dev/null 2>&1 || true
        sudo iperf3 -s -D </dev/null >/dev/null 2>&1
        ss -lntup | grep -E ":5201\b" || true'
    } >"$evidence_dir/performance/setup-${node}.txt" 2>&1 || return 1
  done
}

setup_failover_transfer_services() {
  [ "$failover_transfer_tests" -eq 1 ] || return 0
  local node
  for node in "${clients[@]}"; do
    {
      echo "## setup failover transfer service on $node"
      ssh_node "$node" 'set -euo pipefail
        export DEBIAN_FRONTEND=noninteractive
        if command -v apt-get >/dev/null 2>&1; then
          sudo apt-get update
          sudo apt-get install -y --no-install-recommends curl python3
        fi
        sudo mkdir -p /srv/routerd-e2e/http
        if [ ! -f /srv/routerd-e2e/http/failover-transfer.bin ]; then
          sudo dd if=/dev/zero of=/srv/routerd-e2e/http/failover-transfer.bin bs=1M count=64 status=none
        fi
        sudo chmod -R 0755 /srv/routerd-e2e/http
        if sudo iptables -S INPUT >/dev/null 2>&1; then
          sudo iptables -C INPUT -s 10.77.60.0/24 -p tcp --dport 8080 -j ACCEPT >/dev/null 2>&1 || sudo iptables -I INPUT 1 -s 10.77.60.0/24 -p tcp --dport 8080 -j ACCEPT
        fi
        sudo pkill -f "[p]ython3 -m http.server 8080" >/dev/null 2>&1 || true
        nohup python3 -m http.server 8080 --bind 0.0.0.0 --directory /srv/routerd-e2e/http >/tmp/routerd-e2e-http.log 2>&1 &
        echo $! >/tmp/routerd-e2e-http.pid
        sleep 1
        ss -lntp | grep -E ":8080\b"'
    } >"$evidence_dir/failover-transfer/setup-${node}.txt" 2>&1 || return 1
  done
}

is_global_ipv4() {
  local ip="$1" a b c d
  IFS=. read -r a b c d <<EOF
$ip
EOF
  case "$a.$b.$c.$d" in
    ""|*[!0-9.]*)
      return 1
      ;;
  esac
  [ "$a" -ge 1 ] 2>/dev/null && [ "$a" -le 223 ] || return 1
  [ "$b" -ge 0 ] 2>/dev/null && [ "$b" -le 255 ] || return 1
  [ "$c" -ge 0 ] 2>/dev/null && [ "$c" -le 255 ] || return 1
  [ "$d" -ge 0 ] 2>/dev/null && [ "$d" -le 255 ] || return 1
  [ "$a" -eq 10 ] && return 1
  [ "$a" -eq 127 ] && return 1
  [ "$a" -eq 169 ] && [ "$b" -eq 254 ] && return 1
  [ "$a" -eq 172 ] && [ "$b" -ge 16 ] && [ "$b" -le 31 ] && return 1
  [ "$a" -eq 192 ] && [ "$b" -eq 168 ] && return 1
  [ "$a" -eq 100 ] && [ "$b" -ge 64 ] && [ "$b" -le 127 ] && return 1
  [ "$a" -ge 224 ] && return 1
  return 0
}

is_cloud_site() {
  case "$1" in
    aws|azure|oci)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
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
  local status=0 src dst src_ip dst_ip src_public dst_public src_site dst_site result public_result
  mkdir -p "$out"
  : >"$out/summary.tsv"
  : >"$out/public-summary.tsv"
  : >"$out/comparison.tsv"
  printf 'src\tdst\tsam_tcp_bps\tsam_udp_bps\tsam_ping_loss_pct\tpublic_tcp_bps\tpublic_udp_bps\tpublic_ping_loss_pct\tpublic_status\n' >"$out/comparison.tsv"
  for src in "${clients[@]}"; do
    for dst in "${clients[@]}"; do
      [ "$src" != "$dst" ] || continue
      src_ip="$(node_field "$src" private_ip)"
      dst_ip="$(node_field "$dst" private_ip)"
      src_public="$(node_field "$src" public_ip)"
      dst_public="$(node_field "$dst" public_ip)"
      src_site="$(node_field "$src" site)"
      dst_site="$(node_field "$dst" site)"
      result=PASS
      {
        echo "=== sam private performance $src -> $dst ==="
        echo "SRC=$src SRCIP=$src_ip DST=$dst DSTIP=$dst_ip"
        echo "## tcp iperf3"
        ok=0
        for attempt in 1 2 3; do
          ssh_node "$dst" 'sudo pkill -9 iperf3 >/dev/null 2>&1 || true; sleep 2; sudo iperf3 -s -D </dev/null >/dev/null 2>&1; sleep 2'
          ssh_node "$src" "timeout 20s iperf3 -J -c '$dst_ip' -B '$src_ip' -t 5" >"$out/${src}_to_${dst}.iperf3-tcp.json" && { ok=1; break; }
          sleep 5
        done
        [ "$ok" -eq 1 ] || result=FAIL_TCP
        echo "## udp iperf3"
        ok=0
        for attempt in 1 2 3; do
          ssh_node "$dst" 'sudo pkill -9 iperf3 >/dev/null 2>&1 || true; sleep 2; sudo iperf3 -s -D </dev/null >/dev/null 2>&1; sleep 2'
          ssh_node "$src" "timeout 20s iperf3 -J -u -b 10M -c '$dst_ip' -B '$src_ip' -t 5" >"$out/${src}_to_${dst}.iperf3-udp.json" && { ok=1; break; }
          sleep 5
        done
        [ "$ok" -eq 1 ] || result=FAIL_UDP
        echo "## small packet ping sample"
        ok=0
        for attempt in 1 2 3; do
          ssh_node "$src" "timeout 20s ping -I '$src_ip' -s 56 -c 100 -i 0.01 '$dst_ip'" >"$out/${src}_to_${dst}.ping-pps.txt" && { ok=1; break; }
          sleep 2
        done
        [ "$ok" -eq 1 ] || result=FAIL_PING_PPS
      } >"$out/${src}_to_${dst}.txt" 2>&1 || result=FAIL
      printf '%s\t%s\t%s\n' "$src" "$dst" "$result" >>"$out/summary.tsv"
      [ "$result" = "PASS" ] || status=1

      public_result=PASS
      if ! is_cloud_site "$src_site" || ! is_cloud_site "$dst_site"; then
        public_result=SKIP_PUBLIC_CLOUD_ONLY
        printf '%s\t%s\t%s\n' "$src" "$dst" "$public_result" >>"$out/public-summary.tsv"
      elif [ "$src_site" = "$dst_site" ]; then
        public_result=SKIP_PUBLIC_SAME_CLOUD
        printf '%s\t%s\t%s\n' "$src" "$dst" "$public_result" >>"$out/public-summary.tsv"
      elif ! is_global_ipv4 "$dst_public"; then
        public_result=SKIP_PUBLIC_NON_GLOBAL_DST
        printf '%s\t%s\t%s\n' "$src" "$dst" "$public_result" >>"$out/public-summary.tsv"
      else
        {
          echo "=== public direct performance $src -> $dst ==="
          echo "SRC=$src SRCPUBLIC=$src_public DST=$dst DSTPUBLIC=$dst_public"
          echo "## tcp iperf3"
          ok=0
          for attempt in 1 2 3; do
            ssh_node "$dst" 'sudo pkill -9 iperf3 >/dev/null 2>&1 || true; sleep 2; sudo iperf3 -s -D </dev/null >/dev/null 2>&1; sleep 2'
            ssh_node "$src" "timeout 20s iperf3 -J -c '$dst_public' -t 5" >"$out/${src}_to_${dst}.public-iperf3-tcp.json" && { ok=1; break; }
            sleep 5
          done
          [ "$ok" -eq 1 ] || public_result=FAIL_PUBLIC_TCP
          echo "## udp iperf3"
          ok=0
          for attempt in 1 2 3; do
            ssh_node "$dst" 'sudo pkill -9 iperf3 >/dev/null 2>&1 || true; sleep 2; sudo iperf3 -s -D </dev/null >/dev/null 2>&1; sleep 2'
            ssh_node "$src" "timeout 20s iperf3 -J -u -b 10M -c '$dst_public' -t 5" >"$out/${src}_to_${dst}.public-iperf3-udp.json" && { ok=1; break; }
            sleep 5
          done
          [ "$ok" -eq 1 ] || public_result=FAIL_PUBLIC_UDP
          echo "## public ping sample"
          ok=0
          for attempt in 1 2 3; do
            ssh_node "$src" "timeout 20s ping -c 20 -W 2 '$dst_public'" >"$out/${src}_to_${dst}.public-ping.txt" && { ok=1; break; }
            sleep 2
          done
          [ "$ok" -eq 1 ] || public_result=FAIL_PUBLIC_PING
        } >"$out/${src}_to_${dst}.public.txt" 2>&1 || public_result=FAIL_PUBLIC
        printf '%s\t%s\t%s\n' "$src" "$dst" "$public_result" >>"$out/public-summary.tsv"
        [ "$public_result" = "PASS" ] || status=1
      fi

      sam_tcp_bps="$(jq -r '.end.sum_received.bits_per_second // .end.sum.bits_per_second // empty' "$out/${src}_to_${dst}.iperf3-tcp.json" 2>/dev/null || true)"
      sam_udp_bps="$(jq -r '.end.sum.bits_per_second // empty' "$out/${src}_to_${dst}.iperf3-udp.json" 2>/dev/null || true)"
      sam_ping_loss="$(awk -F, '/packet loss/ {gsub(/^[[:space:]]+|% packet loss.*/, "", $3); print $3}' "$out/${src}_to_${dst}.ping-pps.txt" 2>/dev/null || true)"
      public_tcp_bps=
      public_udp_bps=
      public_ping_loss=
      if [ "$public_result" = "PASS" ]; then
        public_tcp_bps="$(jq -r '.end.sum_received.bits_per_second // .end.sum.bits_per_second // empty' "$out/${src}_to_${dst}.public-iperf3-tcp.json" 2>/dev/null || true)"
        public_udp_bps="$(jq -r '.end.sum.bits_per_second // empty' "$out/${src}_to_${dst}.public-iperf3-udp.json" 2>/dev/null || true)"
        public_ping_loss="$(awk -F, '/packet loss/ {gsub(/^[[:space:]]+|% packet loss.*/, "", $3); print $3}' "$out/${src}_to_${dst}.public-ping.txt" 2>/dev/null || true)"
      fi
      printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$src" "$dst" "$sam_tcp_bps" "$sam_udp_bps" "$sam_ping_loss" "$public_tcp_bps" "$public_udp_bps" "$public_ping_loss" "$public_result" >>"$out/comparison.tsv"
    done
  done
  return "$status"
}

run_validation_set() {
  local label="$1"
  local status=0
  wait_convergence "$label" || status=1
  [ "$skip_matrix" -eq 1 ] || client_matrix "$label" || status=1
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
    ssh_node "$node" 'hostname; sudo routerctl doctor sam || true; sudo routerctl get status -o json || true; sudo routerctl describe MobilityPool/cloudedge -o json || true; sudo routerctl action list || true; ip -br addr; ip route; ip rule; ip neigh show || true; sysctl net.ipv4.ip_forward net.ipv4.conf.all.rp_filter net.ipv4.conf.default.rp_filter net.ipv4.conf.all.proxy_arp net.ipv4.conf.all.accept_local 2>/dev/null || true; journalctl -u routerd.service -u routerd-bgp.service --since "30 minutes ago" --no-pager -n 500' >"$dir/${node}.txt" 2>&1 || true
  done
}

collect_load_balance_report() {
  [ "$load_balance_report" -eq 1 ] || return 0
  local label="$1"
  local dir="$evidence_dir/diagnostics/load-balance-$label"
  mkdir -p "$dir"
  : >"$dir/owner-table.tsv"
  : >"$dir/owner-summary.tsv"
  : >"$dir/owner-site-summary.tsv"
  : >"$dir/owner-distribution.tsv"
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
    {
      echo $'observer_site\towner_site\tcount'
      awk -F '\t' '
      function site_from_node(node) {
        if (node ~ /^aws-/) return "aws"
        if (node ~ /^azure-/) return "azure"
        if (node ~ /^oci-/) return "oci"
        if (node ~ /^pve-/) return "pve"
        if (node == "" || node == "<unknown>") return "<unknown>"
        return "other"
      }
      {
        observer_site = site_from_node($1)
        owner = ($3 == "" ? "<unknown>" : $3)
        owner_site = site_from_node(owner)
        key = observer_site "\t" owner_site
        count[key]++
      }
      END {
        for (key in count) {
          print key "\t" count[key]
        }
      }
      ' "$dir/owner-table.tsv" | sort
    } >"$dir/owner-site-summary.tsv"
    {
      echo $'owner_site\towner\tunique_addresses\tobserver_rows'
      awk -F '\t' '
      function site_from_node(node) {
        if (node ~ /^aws-/) return "aws"
        if (node ~ /^azure-/) return "azure"
        if (node ~ /^oci-/) return "oci"
        if (node ~ /^pve-/) return "pve"
        if (node == "" || node == "<unknown>") return "<unknown>"
        return "other"
      }
      {
        owner = ($3 == "" ? "<unknown>" : $3)
        owner_site = site_from_node(owner)
        owner_key = owner_site "\t" owner
        rows[owner_key]++
        addresses[owner_key SUBSEP $2] = 1
      }
      END {
        for (k in addresses) {
          split(k, parts, SUBSEP)
          unique[parts[1]]++
        }
        for (owner_key in rows) {
          print owner_key "\t" unique[owner_key] "\t" rows[owner_key]
        }
      }
      ' "$dir/owner-table.tsv" | sort
    } >"$dir/owner-distribution.tsv"
  fi
}

failover_transfer_pair() {
  local failed_node="$1"
  local failed_role failed_site dst="" src="" dst_site client
  failed_role="$(node_field "$failed_node" role)"
  failed_site="$(node_field "$failed_node" site)"
  if [ "$failed_role" = "leaf" ]; then
    for client in "${clients[@]}"; do
      [ "$(node_field "$client" site)" = "$failed_site" ] || continue
      dst="$client"
      break
    done
  fi
  [ -n "$dst" ] || dst="${clients[0]}"
  dst_site="$(node_field "$dst" site)"
  for client in "${clients[@]}"; do
    [ "$client" != "$dst" ] || continue
    [ "$(node_field "$client" site)" != "$dst_site" ] || continue
    src="$client"
    break
  done
  if [ -z "$src" ]; then
    for client in "${clients[@]}"; do
      [ "$client" = "$dst" ] || { src="$client"; break; }
    done
  fi
  printf '%s %s\n' "$src" "$dst"
}

start_failover_transfer() {
  [ "$failover_transfer_tests" -eq 1 ] || return 0
  local label="$1" failed_node="$2"
  local src dst src_ip dst_ip out remote_pid
  read -r src dst < <(failover_transfer_pair "$failed_node")
  [ -n "$src" ] && [ -n "$dst" ] || return 1
  src_ip="$(node_field "$src" private_ip)"
  dst_ip="$(node_field "$dst" private_ip)"
  out="$evidence_dir/failover-transfer/$label"
  mkdir -p "$out"
  {
    echo "label=$label"
    echo "failed_node=$failed_node"
    echo "src=$src"
    echo "src_ip=$src_ip"
    echo "dst=$dst"
    echo "dst_ip=$dst_ip"
    echo "url=http://$dst_ip:8080/failover-transfer.bin"
  } >"$out/metadata.txt"
  remote_pid="$(ssh_node "$src" "rm -f /tmp/routerd-${label}.log /tmp/routerd-${label}.bin; (date -u '+started=%Y-%m-%dT%H:%M:%SZ'; timeout 150s curl -fS --limit-rate 512k --connect-timeout 10 --max-time 150 -o /tmp/routerd-${label}.bin 'http://$dst_ip:8080/failover-transfer.bin'; rc=\$?; date -u '+finished=%Y-%m-%dT%H:%M:%SZ'; echo \"rc=\$rc\"; ls -l /tmp/routerd-${label}.bin 2>/dev/null || true; exit \$rc) >/tmp/routerd-${label}.log 2>&1 & echo \$!")"
  printf '%s %s\n' "$src" "$remote_pid"
}

finish_failover_transfer() {
  [ "$failover_transfer_tests" -eq 1 ] || return 0
  local label="$1" src="$2" remote_pid="$3"
  local out="$evidence_dir/failover-transfer/$label"
  [ -n "$src" ] && [ -n "$remote_pid" ] || return 1
  mkdir -p "$out"
  {
    echo "## wait remote transfer"
    ssh_node "$src" "deadline=\$((SECONDS + 170)); while kill -0 '$remote_pid' >/dev/null 2>&1 && [ \"\$SECONDS\" -lt \"\$deadline\" ]; do sleep 2; done; if kill -0 '$remote_pid' >/dev/null 2>&1; then echo still-running; kill '$remote_pid' >/dev/null 2>&1 || true; fi"
    echo "## transfer log"
    ssh_node "$src" "cat /tmp/routerd-${label}.log; rm -f /tmp/routerd-${label}.bin"
  } >"$out/result.txt" 2>&1 || return 1
  grep -q '^rc=0$' "$out/result.txt"
}

run_failover() {
  local status=0
  local transfer_src transfer_pid
  [ "${#failover_nodes[@]}" -gt 0 ] || return 0
  for node in "${failover_nodes[@]}"; do
    collect_diagnostics "before-failover-${node}"
    collect_provider_inventory "before-failover-${node}" || status=1
    transfer_src=
    transfer_pid=
    if [ "$failover_transfer_tests" -eq 1 ]; then
      read -r transfer_src transfer_pid < <(start_failover_transfer "during-failover-${node}" "$node") || status=1
      sleep 3
    fi
    ssh_node "$node" 'sudo systemctl stop routerd.service routerd-bgp.service' >"$evidence_dir/convergence/failover-stop-${node}.txt" 2>&1
    stopped_routers+=("$node")
    run_validation_set "after-failover-${node}" || status=1
    if [ "$failover_transfer_tests" -eq 1 ]; then
      finish_failover_transfer "during-failover-${node}" "$transfer_src" "$transfer_pid" || status=1
    fi
    collect_diagnostics "after-failover-${node}"
    collect_provider_inventory "after-failover-${node}" || status=1
  done
  return "$status"
}

run_rejoin() {
  local status=0 node
  [ "$rejoin_after_failover" -eq 1 ] || return 0
  [ "${#failover_nodes[@]}" -gt 0 ] || return 0
  for node in "${failover_nodes[@]}"; do
    collect_diagnostics "before-rejoin-${node}"
    collect_provider_inventory "before-rejoin-${node}" || status=1
    ssh_node "$node" 'sudo systemctl start routerd-bgp.service routerd.service; sudo systemctl is-active routerd.service routerd-bgp.service' >"$evidence_dir/convergence/rejoin-start-${node}.txt" 2>&1 || status=1
    mark_node_running "$node"
    run_validation_set "after-rejoin-${node}" || status=1
    collect_diagnostics "after-rejoin-${node}"
    collect_provider_inventory "after-rejoin-${node}" || status=1
  done
  return "$status"
}

teardown() {
  [ -n "$destroy_cmd" ] || return 0
  bash -lc "$destroy_cmd" >"$evidence_dir/cleanup/destroy.txt" 2>&1
}

record_note
printf 'label\tstatus\telapsed_seconds\n' >"$evidence_dir/convergence/summary.tsv"
preflight || mark_failed "preflight"
collect_provider_inventory "preflight" || mark_failed "provider inventory preflight"
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
  setup_performance_services || mark_failed "performance service setup"
fi
if [ "$overall" -eq 0 ]; then
  setup_failover_transfer_services || mark_failed "failover transfer service setup"
fi
if [ "$overall" -eq 0 ]; then
  run_validation_set "initial" || mark_failed "initial validation set"
fi
collect_diagnostics "post-matrix"
collect_provider_inventory "post-matrix" || mark_failed "provider inventory post-matrix"
if [ "$overall" -eq 0 ]; then
  run_failover || mark_failed "failover"
fi
if [ "$overall" -eq 0 ]; then
  run_rejoin || mark_failed "rejoin"
fi
teardown || mark_failed "teardown"

echo "evidence: $evidence_dir"
exit "$overall"
