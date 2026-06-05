#!/usr/bin/env bash
# Cloud fabric evidence collector for CloudEdge SAM.

set -euo pipefail

SELF=$(basename "${BASH_SOURCE[0]}")
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/../.." && pwd)
# shellcheck source=scripts/runners/cloudedge-runner-lib.sh
. "$SCRIPT_DIR/cloudedge-runner-lib.sh"

usage() {
  cat <<EOF
$SELF - CloudEdge cloud fabric evidence collector

USAGE:
  $SELF collect --provider aws|azure|oci --test-id TEST_ID --out BUNDLE_DIR [--capture-address IP]

OUTPUT:
  BUNDLE_DIR/03-control-plane/TEST_ID-provider-cloud-fabric.json
  BUNDLE_DIR/03-control-plane/cloud-fabric-manifest.json
  BUNDLE_DIR/03-control-plane/cloud-fabric-summary.md
  BUNDLE_DIR/03-control-plane/cloud-fabric-test-record.csv

ENV:
  CE_FABRIC_<PROVIDER>_JSON_COMMAND  Optional fake/offline raw JSON command.
  CE_FABRIC_JSON_COMMAND             Generic fake/offline raw JSON command.
  CE_FABRIC_SCHEMA                   Schema path (default scripts/cloudedge-cloud-fabric-schema.json).

  AWS live:   CE_AWS_ENI_ID, optional CE_AWS_ROUTE_TABLE_IDS, CE_AWS_SECURITY_GROUP_IDS,
              CE_AWS_NETWORK_ACL_IDS, CE_AWS_VPC_ID.
  Azure live: CE_AZURE_RESOURCE_GROUP, CE_AZURE_NIC_NAME, optional CE_AZURE_ROUTE_TABLE_IDS,
              CE_AZURE_NSG_IDS, CE_AZURE_FLOW_LOG_IDS.
  OCI live:   CE_OCI_VNIC_ID, CE_OCI_COMPARTMENT_ID, optional CE_OCI_ROUTE_TABLE_ID,
              CE_OCI_NSG_IDS, CE_OCI_SECURITY_LIST_IDS, CE_OCI_FLOW_LOG_IDS.

Missing CLI/auth/config records NOT-RUN with a reason instead of failing bare.
EOF
}

op=""
provider=""
test_id=""
out=""
capture_address=""
schema=${CE_FABRIC_SCHEMA:-"$REPO_ROOT/scripts/cloudedge-cloud-fabric-schema.json"}

while [[ $# -gt 0 ]]; do
  case "$1" in
    collect)
      op=$1
      shift
      ;;
    --provider)
      provider=${2:-}
      shift 2
      ;;
    --test-id)
      test_id=${2:-}
      shift 2
      ;;
    --out)
      out=${2:-}
      shift 2
      ;;
    --capture-address)
      capture_address=${2:-}
      shift 2
      ;;
    --schema)
      schema=${2:-}
      shift 2
      ;;
    -h|--help|help)
      usage
      exit 0
      ;;
    *)
      ce_die "unknown argument: $1"
      ;;
  esac
done

[[ -n "$op" ]] || { usage; exit 0; }
[[ "$op" == "collect" ]] || ce_die "unknown op: $op"
case "$provider" in aws|azure|oci) ;; *) ce_die "--provider must be aws, azure, or oci" ;; esac
[[ "$test_id" =~ ^[0-9]{8}-[0-9]{4}-[A-Z0-9-]+-[0-9]{2}$ ]] || ce_die "bad --test-id: $test_id"
[[ -n "$out" ]] || ce_die "--out is required"
[[ -f "$schema" ]] || ce_die "schema not found: $schema"

slot_dir() {
  local root=$1
  if [[ "$(basename "$root")" == "03-control-plane" ]]; then
    printf '%s' "$root"
  else
    printf '%s/03-control-plane' "$root"
  fi
}

json_quote_array() {
  python3 - "$@" <<'PY'
import json
import sys
items = [item for arg in sys.argv[1:] for item in arg.split(",") if item]
print(json.dumps(items))
PY
}

not_run_json() {
  local reason=$1
  python3 - "$provider" "$reason" <<'PY'
import json
import sys
print(json.dumps({"provider": sys.argv[1], "notRunReason": sys.argv[2]}, sort_keys=True))
PY
}

override_command() {
  local upper command
  upper=$(ce_upper "$provider")
  command=$(ce_env_first "CE_FABRIC_${upper}_JSON_COMMAND" CE_FABRIC_JSON_COMMAND 2>/dev/null || true)
  [[ -n "$command" ]] || return 1
  CE_FABRIC_PROVIDER=$provider CE_FABRIC_TEST_ID=$test_id CE_FABRIC_CAPTURE_ADDRESS=$capture_address bash -lc "$command"
}

collect_aws() {
  local eni=${CE_AWS_ENI_ID:-}
  [[ -n "$eni" ]] || { not_run_json "missing CE_AWS_ENI_ID"; return 0; }
  ce_have aws || { not_run_json "aws CLI not installed"; return 0; }
  if ! aws sts get-caller-identity --output json >/dev/null 2>&1; then
    not_run_json "aws CLI not authenticated"
    return 0
  fi
  local tmp
  tmp=$(mktemp -d "${TMPDIR:-/tmp}/cloudedge-aws-fabric.XXXXXX")
  trap 'rm -rf "$tmp"' RETURN
  if ! aws ec2 describe-network-interfaces --network-interface-ids "$eni" --output json >"$tmp/eni.json" 2>"$tmp/eni.err"; then
    not_run_json "aws describe-network-interfaces failed: $(tr '\n' ' ' <"$tmp/eni.err")"
    return 0
  fi
  aws ec2 describe-route-tables ${CE_AWS_ROUTE_TABLE_IDS:+--route-table-ids $CE_AWS_ROUTE_TABLE_IDS} --output json >"$tmp/routes.json" 2>/dev/null || printf '{"RouteTables":[]}\n' >"$tmp/routes.json"
  aws ec2 describe-security-groups ${CE_AWS_SECURITY_GROUP_IDS:+--group-ids $CE_AWS_SECURITY_GROUP_IDS} --output json >"$tmp/sg.json" 2>/dev/null || printf '{"SecurityGroups":[]}\n' >"$tmp/sg.json"
  aws ec2 describe-network-acls ${CE_AWS_NETWORK_ACL_IDS:+--network-acl-ids $CE_AWS_NETWORK_ACL_IDS} --output json >"$tmp/nacl.json" 2>/dev/null || printf '{"NetworkAcls":[]}\n' >"$tmp/nacl.json"
  aws ec2 describe-flow-logs ${CE_AWS_VPC_ID:+--filter Name=resource-id,Values=$CE_AWS_VPC_ID} --output json >"$tmp/flows.json" 2>/dev/null || printf '{"FlowLogs":[]}\n' >"$tmp/flows.json"
  python3 - "$tmp" <<'PY'
import json
import sys
from pathlib import Path
root = Path(sys.argv[1])
eni = json.loads((root / "eni.json").read_text())
ni = (eni.get("NetworkInterfaces") or [{}])[0]
print(json.dumps({
    "provider": "aws",
    "networkInterface": ni,
    "routeTables": json.loads((root / "routes.json").read_text()).get("RouteTables", []),
    "securityGroups": json.loads((root / "sg.json").read_text()).get("SecurityGroups", []),
    "networkAcls": json.loads((root / "nacl.json").read_text()).get("NetworkAcls", []),
    "flowLogs": json.loads((root / "flows.json").read_text()).get("FlowLogs", []),
}, sort_keys=True))
PY
}

collect_azure() {
  local rg=${CE_AZURE_RESOURCE_GROUP:-} nic=${CE_AZURE_NIC_NAME:-}
  [[ -n "$rg" && -n "$nic" ]] || { not_run_json "missing CE_AZURE_RESOURCE_GROUP or CE_AZURE_NIC_NAME"; return 0; }
  ce_have az || { not_run_json "az CLI not installed"; return 0; }
  if ! az account show -o json >/dev/null 2>&1; then
    not_run_json "az CLI not authenticated"
    return 0
  fi
  local tmp
  tmp=$(mktemp -d "${TMPDIR:-/tmp}/cloudedge-azure-fabric.XXXXXX")
  trap 'rm -rf "$tmp"' RETURN
  if ! az network nic show -g "$rg" -n "$nic" -o json >"$tmp/nic.json" 2>"$tmp/nic.err"; then
    not_run_json "az network nic show failed: $(tr '\n' ' ' <"$tmp/nic.err")"
    return 0
  fi
  az network nic show-effective-route-table -g "$rg" -n "$nic" -o json >"$tmp/effective-routes.json" 2>/dev/null || printf '[]\n' >"$tmp/effective-routes.json"
  az network nic list-effective-nsg -g "$rg" -n "$nic" -o json >"$tmp/effective-security.json" 2>/dev/null || printf '{}\n' >"$tmp/effective-security.json"
  python3 - "$tmp" "$(json_quote_array "${CE_AZURE_ROUTE_TABLE_IDS:-}")" "$(json_quote_array "${CE_AZURE_NSG_IDS:-}")" "$(json_quote_array "${CE_AZURE_FLOW_LOG_IDS:-}")" <<'PY'
import json
import sys
from pathlib import Path
root = Path(sys.argv[1])
print(json.dumps({
    "provider": "azure",
    "nic": json.loads((root / "nic.json").read_text()),
    "effectiveRoutes": json.loads((root / "effective-routes.json").read_text()),
    "effectiveSecurityRules": json.loads((root / "effective-security.json").read_text()),
    "routeTables": json.loads(sys.argv[2]),
    "networkSecurityGroups": json.loads(sys.argv[3]),
    "flowLogs": json.loads(sys.argv[4]),
}, sort_keys=True))
PY
}

collect_oci() {
  local vnic=${CE_OCI_VNIC_ID:-} compartment=${CE_OCI_COMPARTMENT_ID:-}
  [[ -n "$vnic" && -n "$compartment" ]] || { not_run_json "missing CE_OCI_VNIC_ID or CE_OCI_COMPARTMENT_ID"; return 0; }
  ce_have oci || { not_run_json "oci CLI not installed"; return 0; }
  if ! oci iam region-subscription list >/dev/null 2>&1; then
    not_run_json "oci CLI not authenticated"
    return 0
  fi
  local tmp
  tmp=$(mktemp -d "${TMPDIR:-/tmp}/cloudedge-oci-fabric.XXXXXX")
  trap 'rm -rf "$tmp"' RETURN
  if ! oci network vnic get --vnic-id "$vnic" >"$tmp/vnic.json" 2>"$tmp/vnic.err"; then
    not_run_json "oci network vnic get failed: $(tr '\n' ' ' <"$tmp/vnic.err")"
    return 0
  fi
  oci network private-ip list --vnic-id "$vnic" >"$tmp/private-ips.json" 2>/dev/null || printf '{"data":[]}\n' >"$tmp/private-ips.json"
  if [[ -n "${CE_OCI_ROUTE_TABLE_ID:-}" ]]; then
    oci network route-table get --rt-id "$CE_OCI_ROUTE_TABLE_ID" >"$tmp/route-table.json" 2>/dev/null || printf '{"data":{"route-rules":[]}}\n' >"$tmp/route-table.json"
  else
    printf '{"data":{"route-rules":[]}}\n' >"$tmp/route-table.json"
  fi
  python3 - "$tmp" "$(json_quote_array "${CE_OCI_NSG_IDS:-}")" "$(json_quote_array "${CE_OCI_SECURITY_LIST_IDS:-}")" "$(json_quote_array "${CE_OCI_FLOW_LOG_IDS:-}")" <<'PY'
import json
import sys
from pathlib import Path
root = Path(sys.argv[1])
vnic = json.loads((root / "vnic.json").read_text()).get("data", {})
private_ips = json.loads((root / "private-ips.json").read_text()).get("data", [])
route_rules = json.loads((root / "route-table.json").read_text()).get("data", {}).get("route-rules", [])
print(json.dumps({
    "provider": "oci",
    "vnic": vnic,
    "privateIps": private_ips,
    "routeRules": route_rules,
    "networkSecurityGroups": json.loads(sys.argv[2]),
    "securityLists": json.loads(sys.argv[3]),
    "flowLogs": json.loads(sys.argv[4]),
}, sort_keys=True))
PY
}

collect_raw() {
  if override_command; then
    return 0
  fi
  case "$provider" in
    aws) collect_aws ;;
    azure) collect_azure ;;
    oci) collect_oci ;;
  esac
}

control_dir=$(slot_dir "$out")
mkdir -p "$control_dir"
raw_file=$(mktemp "${TMPDIR:-/tmp}/cloudedge-fabric-raw.XXXXXX.json")
trap 'rm -f "$raw_file"' EXIT
collect_raw >"$raw_file"

python3 - "$provider" "$test_id" "$capture_address" "$control_dir" "$schema" "$raw_file" <<'PY'
import csv
import json
import os
import sys
import datetime as dt
from pathlib import Path

provider, test_id, capture_address, control_dir_s, schema_s, raw_file = sys.argv[1:]
control_dir = Path(control_dir_s)
raw = json.loads(Path(raw_file).read_text(encoding="utf-8"))
observed_at = dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")
json_path = control_dir / f"{test_id}-{provider}-cloud-fabric.json"
rel_json = f"03-control-plane/{json_path.name}"

def as_list(value):
    if value is None:
        return []
    if isinstance(value, list):
        return value
    return [value]

def lower_keys(value):
    if isinstance(value, dict):
        return {str(k).lower().replace("-", "").replace("_", ""): v for k, v in value.items()}
    return {}

def field(obj, *names, default=None):
    cur = obj
    for name in names:
        if not isinstance(cur, dict):
            return default
        lk = lower_keys(cur)
        cur = lk.get(str(name).lower().replace("-", "").replace("_", ""), default)
    return cur

def compact(items):
    return [x for x in items if x not in ("", None, [], {})]

def ref(kind, value, name=""):
    if isinstance(value, dict):
        ident = (
            field(value, "id", default=None)
            or field(value, f"{kind}Id", default=None)
            or field(value, "groupId", default=None)
            or field(value, "networkAclId", default=None)
            or field(value, "flowLogId", default=None)
            or field(value, "routeTableId", default=None)
            or field(value, "name", default="")
        )
        label = field(value, "name", default="") or field(value, f"{kind}Name", default="") or field(value, "groupName", default="")
        return {"type": kind, "id": str(ident), "name": str(label or name)}
    return {"type": kind, "id": str(value), "name": name}

def route(prefix, target, source):
    return {"prefix": str(prefix or ""), "target": str(target or ""), "source": source}

def check(name, ok, reason=""):
    return {"name": name, "result": "PASS" if ok else "PARTIAL", "reason": "" if ok else reason}

def normalize_aws(raw):
    ni = raw.get("networkInterface") or raw.get("NetworkInterface") or {}
    ips = as_list(field(ni, "privateIpAddresses", default=[]))
    primary = None
    secondary = []
    for item in ips:
        ip = field(item, "privateIpAddress", default=None)
        if field(item, "primary", default=False):
            primary = ip
        elif ip:
            secondary.append(ip)
    primary = primary or field(ni, "privateIpAddress", default=None)
    source_dest_check = field(ni, "sourceDestCheck", default=None)
    forwarding = None if source_dest_check is None else not bool(source_dest_check)
    route_tables = as_list(raw.get("routeTables") or raw.get("RouteTables"))
    routes = []
    local_route = False
    for table in route_tables:
        for r in as_list(field(table, "routes", default=[])):
            prefix = field(r, "destinationCidrBlock", default="") or field(r, "destinationIpv6CidrBlock", default="")
            target = field(r, "gatewayId", default="") or field(r, "networkInterfaceId", default="") or field(r, "natGatewayId", default="") or field(r, "transitGatewayId", default="")
            routes.append(route(prefix, target, "route-table"))
            if target == "local":
                local_route = True
    groups = as_list(field(ni, "groups", default=[])) + as_list(raw.get("securityGroups"))
    nacls = as_list(raw.get("networkAcls") or raw.get("NetworkAcls"))
    flows = as_list(raw.get("flowLogs") or raw.get("FlowLogs"))
    normalized = {
        "captureAddress": capture_address or (secondary[0] if secondary else None),
        "primaryAddress": primary,
        "secondaryAddresses": secondary,
        "forwardingEnabled": forwarding,
        "localRouteObserved": local_route if routes else None,
        "routeTargets": routes,
        "securityRefs": [ref("security-group", g) for g in groups] + [ref("network-acl", n) for n in nacls],
        "flowLogRefs": [ref("flow-log", f) for f in flows],
        "providerSpecific": {
            "eniId": field(ni, "networkInterfaceId", default=""),
            "sourceDestCheck": source_dest_check,
            "routeTableCount": len(route_tables),
        },
    }
    checks = [
        check("aws_eni_primary_ip", bool(primary), "ENI primary private IP missing"),
        check("aws_eni_secondary_ip", bool(secondary), "ENI secondary private IP missing"),
        check("aws_source_dest_check_disabled", forwarding is True, "source/dest check is not disabled"),
        check("aws_route_tables", bool(route_tables), "route table evidence missing"),
        check("aws_sg_nacl", bool(groups) and bool(nacls), "SG or NACL evidence missing"),
        check("aws_flow_logs", bool(flows), "VPC flow log reference missing"),
        check("aws_longest_prefix_local_route", local_route, "local route evidence missing"),
    ]
    return normalized, checks

def normalize_azure(raw):
    nic = raw.get("nic") or {}
    ipconfigs = as_list(field(nic, "ipConfigurations", default=[]))
    primary = None
    secondary = []
    for item in ipconfigs:
        ip = field(item, "privateIpAddress", default=None)
        if field(item, "primary", default=False):
            primary = ip
        elif ip:
            secondary.append(ip)
    forwarding = field(nic, "enableIpForwarding", default=None)
    effective_routes = as_list(raw.get("effectiveRoutes"))
    routes = []
    for r in effective_routes:
        prefixes = as_list(field(r, "addressPrefix", default=[])) or as_list(field(r, "addressPrefixes", default=[]))
        for prefix in prefixes:
            routes.append(route(prefix, field(r, "nextHopType", default=""), "effective-route"))
    udrs = as_list(raw.get("routeTables"))
    nsgs = as_list(raw.get("networkSecurityGroups"))
    effective_security = raw.get("effectiveSecurityRules") or {}
    security_refs = [ref("network-security-group", n) for n in nsgs]
    if effective_security:
        security_refs.append({"type": "effective-security-rules", "id": "effective", "name": ""})
    flows = as_list(raw.get("flowLogs"))
    udr_routes = [route("", u.get("id", u.get("name", str(u))) if isinstance(u, dict) else u, "user-defined-route") for u in udrs]
    normalized = {
        "captureAddress": capture_address or (secondary[0] if secondary else None),
        "primaryAddress": primary,
        "secondaryAddresses": secondary,
        "forwardingEnabled": None if forwarding is None else bool(forwarding),
        "localRouteObserved": bool(routes) if routes else None,
        "routeTargets": routes + udr_routes,
        "securityRefs": security_refs,
        "flowLogRefs": [ref("flow-log", f) for f in flows],
        "providerSpecific": {
            "nicId": field(nic, "id", default=""),
            "ipForwarding": forwarding,
            "udrCount": len(udrs),
        },
    }
    checks = [
        check("azure_nic_primary_ip", bool(primary), "NIC primary private IP missing"),
        check("azure_secondary_ip_config", bool(secondary), "secondary IP config missing"),
        check("azure_ip_forwarding_enabled", forwarding is True, "NIC IP forwarding is not enabled"),
        check("azure_effective_routes", bool(effective_routes), "effective route evidence missing"),
        check("azure_effective_security", bool(effective_security), "effective security evidence missing"),
        check("azure_udr", bool(udrs), "UDR evidence missing"),
        check("azure_flow_logs", bool(flows), "VNet flow log reference missing"),
    ]
    return normalized, checks

def normalize_oci(raw):
    vnic = raw.get("vnic") or {}
    private_ips = as_list(raw.get("privateIps"))
    primary = field(vnic, "privateIp", default=None)
    secondary = []
    for item in private_ips:
        ip = field(item, "ipAddress", default=None)
        if ip and ip != primary:
            secondary.append(ip)
    skip = field(vnic, "skipSourceDestCheck", default=None)
    route_rules = as_list(raw.get("routeRules"))
    routes = []
    private_ip_target = False
    for r in route_rules:
        target = field(r, "networkEntityId", default="") or field(r, "target", default="")
        routes.append(route(field(r, "destination", default=""), target, "route-rule"))
        if str(target).startswith("ocid1.privateip."):
            private_ip_target = True
    nsgs = as_list(raw.get("networkSecurityGroups"))
    sls = as_list(raw.get("securityLists"))
    flows = as_list(raw.get("flowLogs"))
    delete_risk = raw.get("deleteUnassignBlackholeRisk")
    if delete_risk is None:
        delete_risk = bool(secondary and private_ip_target)
    normalized = {
        "captureAddress": capture_address or (secondary[0] if secondary else None),
        "primaryAddress": primary,
        "secondaryAddresses": secondary,
        "forwardingEnabled": None if skip is None else bool(skip),
        "localRouteObserved": private_ip_target if routes else None,
        "routeTargets": routes,
        "securityRefs": [ref("network-security-group", n) for n in nsgs] + [ref("security-list", s) for s in sls],
        "flowLogRefs": [ref("flow-log", f) for f in flows],
        "providerSpecific": {
            "vnicId": field(vnic, "id", default=""),
            "skipSourceDestCheck": skip,
            "privateIpOcidRouteTargetObserved": private_ip_target,
            "deleteUnassignBlackholeRisk": bool(delete_risk),
        },
    }
    checks = [
        check("oci_vnic_primary_ip", bool(primary), "VNIC primary private IP missing"),
        check("oci_secondary_private_ip", bool(secondary), "secondary private IP missing"),
        check("oci_skip_source_dest_check", skip is True, "skipSourceDestCheck is not true"),
        check("oci_private_ip_ocid_route_target", private_ip_target, "route rule private IP OCID target missing"),
        check("oci_nsg_security_list", bool(nsgs) or bool(sls), "NSG/security list evidence missing"),
        check("oci_flow_logs", bool(flows), "VCN flow log reference missing"),
        check("oci_delete_unassign_blackhole_risk_recorded", "deleteUnassignBlackholeRisk" in raw or private_ip_target, "delete/unassign blackhole risk not recorded"),
    ]
    return normalized, checks

if raw.get("notRunReason"):
    normalized = {
        "captureAddress": capture_address or None,
        "primaryAddress": None,
        "secondaryAddresses": [],
        "forwardingEnabled": None,
        "localRouteObserved": None,
        "routeTargets": [],
        "securityRefs": [],
        "flowLogRefs": [],
        "providerSpecific": {},
    }
    checks = [{"name": f"{provider}_collector_available", "result": "NOT-RUN", "reason": raw["notRunReason"]}]
    result = "NOT-RUN"
    reason = raw["notRunReason"]
else:
    if provider == "aws":
        normalized, checks = normalize_aws(raw)
    elif provider == "azure":
        normalized, checks = normalize_azure(raw)
    elif provider == "oci":
        normalized, checks = normalize_oci(raw)
    else:
        raise SystemExit(f"unsupported provider {provider}")
    partial = [c for c in checks if c["result"] != "PASS"]
    result = "PARTIAL" if partial else "PASS"
    reason = "; ".join(f"{c['name']}: {c['reason']}" for c in partial)

doc = {
    "schemaVersion": 1,
    "testId": test_id,
    "phase": "CF",
    "provider": provider,
    "result": result,
    "reason": reason,
    "observedAt": observed_at,
    "evidencePath": rel_json,
    "normalized": normalized,
    "checks": checks,
    "raw": raw,
}
json_path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")

try:
    import jsonschema
except Exception:
    jsonschema = None
if jsonschema is not None:
    jsonschema.validate(instance=doc, schema=json.loads(Path(schema_s).read_text(encoding="utf-8")))

manifest_path = control_dir / "cloud-fabric-manifest.json"
if manifest_path.exists():
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except Exception:
        manifest = {"runs": []}
else:
    manifest = {"runs": []}
manifest["runs"] = [
    run for run in manifest.get("runs", [])
    if not (run.get("testId") == test_id and run.get("provider") == provider)
]
manifest["runs"].append(doc)
manifest_path.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")

summary_path = control_dir / "cloud-fabric-summary.md"
lines = [
    "# Cloud Fabric Evidence Summary",
    "",
    "| TEST_ID | Provider | Result | Evidence | Reason |",
    "| --- | --- | --- | --- | --- |",
]
for run in sorted(manifest["runs"], key=lambda r: (r.get("testId", ""), r.get("provider", ""))):
    lines.append(f"| {run['testId']} | {run['provider']} | {run['result']} | `{run['evidencePath']}` | {run.get('reason', '')} |")
lines.append("")
summary_path.write_text("\n".join(lines), encoding="utf-8")

record_path = control_dir / "cloud-fabric-test-record.csv"
with record_path.open("w", newline="", encoding="utf-8") as f:
    writer = csv.DictWriter(f, fieldnames=["TEST_ID", "PHASE", "TARGET", "RESULT", "EVIDENCE", "NOTES"])
    writer.writeheader()
    for run in sorted(manifest["runs"], key=lambda r: (r.get("testId", ""), r.get("provider", ""))):
        writer.writerow({
            "TEST_ID": run["testId"],
            "PHASE": "CF",
            "TARGET": f"{run['provider']} cloud fabric evidence",
            "RESULT": run["result"],
            "EVIDENCE": run["evidencePath"],
            "NOTES": run.get("reason", ""),
        })

print(f"result={result}")
if reason:
    print(f"reason={reason}")
print(f"json={json_path}")
print(f"summary={summary_path}")
print(f"record={record_path}")
PY
