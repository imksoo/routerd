#!/usr/bin/env python3
"""Read-only Azure quota admission for the closed three-B1s release profile."""

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile


class CapacityError(RuntimeError):
    def __init__(self, message, *, quotas=None):
        super().__init__(message)
        self.quotas = quotas


# The pinned Terraform module and qa_guard APPROVED_TYPES require three B1s
# machines, each with one vCPU. Do not infer demand from only running VMs:
# Azure's usage also accounts for deallocated allocations.
REQUIRED_CORES = 3
REGION = "japaneast"
QUOTAS = ("cores", "standardBSFamily")


def quota_request(contract, tfvars_text):
    qualification = contract.get("qualification") if isinstance(contract, dict) else None
    if not isinstance(qualification, dict) or qualification.get("profile") != "representative-redundancy":
        raise CapacityError("invalid qualification profile for Azure capacity precheck")
    scope = qualification.get("runScope")
    if scope == "pve-certification-only":
        return None
    if scope != "full-representative":
        raise CapacityError("invalid qualification scope for Azure capacity precheck")
    providers = contract.get("providers")
    if not isinstance(providers, list) or any(not isinstance(p, dict) for p in providers):
        raise CapacityError("invalid Azure provider context")
    azure = [p for p in providers if p.get("name") == "azure"]
    if len(azure) != 1 or any(azure[0].get(key) != value for key, value in {
        "region": REGION, "profile": "active-az-account", "authMode": "az-cli-session",
    }.items()):
        raise CapacityError("Azure provider context differs from the closed profile")
    limits = contract.get("limits")
    if not isinstance(limits, dict):
        raise CapacityError("Azure capacity limits are missing")
    for group, expected in (("providerCounts", REQUIRED_CORES),
                            ("instanceTypes", {"Standard_B1s": REQUIRED_CORES}), ("regions", REGION)):
        values = limits.get(group)
        if not isinstance(values, dict) or values.get("azure") != expected:
            raise CapacityError("Azure capacity limits differ from the closed profile")
    if (type(limits["providerCounts"]["azure"]) is not int or
            type(limits["instanceTypes"]["azure"]["Standard_B1s"]) is not int):
        raise CapacityError("Azure capacity counts must be integers")

    keys = "azure_subscription_id|azure_location"
    candidate = re.compile(rf"^\s*({keys})\s*=")
    literal = re.compile(rf'^\s*({keys})\s*=\s*"([^"\n]+)"\s*(?:#.*)?$')
    values = {}
    for line in tfvars_text.splitlines():
        if not candidate.match(line):
            continue
        match = literal.fullmatch(line)
        if match is None or match[1] in values:
            raise CapacityError("Azure tfvars context must have unique literal assignments")
        values[match[1]] = match[2]
    subscription = values.get("azure_subscription_id", "")
    if not re.fullmatch(r"[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}", subscription):
        raise CapacityError("Azure tfvars subscription must be an explicit subscription UUID")
    if values.get("azure_location") != REGION:
        raise CapacityError("Azure tfvars location differs from the contract region")
    return {"subscription": subscription, "region": REGION, "requiredCores": REQUIRED_CORES}


def quota_integer(value):
    if type(value) is int and value >= 0:
        return value
    if isinstance(value, str) and re.fullmatch(r"[0-9]{1,20}", value):
        return int(value)
    raise CapacityError("Azure quota values must be nonnegative integers or ASCII digit strings")


def verify_usage(payload):
    if not isinstance(payload, list) or not payload:
        raise CapacityError("Azure usage must be a nonempty list")
    result = {}
    for item in payload:
        if not isinstance(item, dict) or not isinstance(item.get("name"), dict):
            raise CapacityError("malformed Azure usage entry")
        name = item["name"].get("value")
        if not isinstance(name, str) or not name:
            raise CapacityError("malformed Azure usage name")
        if name not in QUOTAS:
            continue
        if name in result or item.get("unit") != "Count":
            raise CapacityError("duplicate Azure quota or unexpected quota unit")
        current = quota_integer(item.get("currentValue"))
        limit = quota_integer(item.get("limit"))
        result[name] = {"current": current, "limit": limit, "required": REQUIRED_CORES,
                        "remainingAfter": limit - current - REQUIRED_CORES}
    if set(result) != set(QUOTAS):
        raise CapacityError("Azure usage is missing regional cores or standardBSFamily quota")
    for name, quota in result.items():
        if quota["remainingAfter"] < 0:
            raise CapacityError(f"insufficient Azure {name} quota: current={quota['current']} "
                                f"required={REQUIRED_CORES} limit={quota['limit']}", quotas=result)
    return result


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise CapacityError("Azure capacity input has duplicate JSON keys")
        result[key] = value
    return result


def azure_json(arguments):
    try:
        process = subprocess.run(
            ["az", *arguments, "--output", "json", "--only-show-errors"],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=20, check=False,
        )
    except subprocess.TimeoutExpired:
        raise CapacityError("Azure quota query timed out (20 seconds)") from None
    except OSError:
        raise CapacityError("Azure quota query could not start") from None
    if process.returncode:
        raise CapacityError("Azure quota query failed; check the selected subscription authentication/permissions")
    try:
        return json.loads(process.stdout, object_pairs_hook=unique_object)
    except (ValueError, TypeError):
        raise CapacityError("Azure quota query returned invalid JSON") from None


def query_usage(request):
    # Inventory and other existing Azure calls use the active CLI account.
    # Never validate capacity in one subscription and clean up in another.
    # Do not switch accounts: this run's authentication snapshot is immutable.
    account = azure_json(["account", "show"])
    active = account.get("id") if isinstance(account, dict) else None
    if not isinstance(active, str) or active.lower() != request["subscription"].lower():
        raise CapacityError("active Azure subscription differs from pinned tfvars")
    return azure_json(["vm", "list-usage", "--subscription", request["subscription"],
                       "--location", request["region"]])


def write_result(directory, result):
    directory.mkdir(parents=True, mode=0o700, exist_ok=True)
    directory.chmod(0o700)
    descriptor, temporary = tempfile.mkstemp(prefix=".result-", dir=directory)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as output:
            json.dump(result, output, indent=2)
            output.write("\n")
        os.replace(temporary, directory / "result.json")
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--contract", type=Path, required=True)
    parser.add_argument("--tfvars", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    result = {"status": "fail", "checkedAt": datetime.now(timezone.utc).isoformat()}
    try:
        contract_bytes = args.contract.read_bytes()
        tfvars_bytes = args.tfvars.read_bytes()
        request = quota_request(json.loads(contract_bytes, object_pairs_hook=unique_object),
                                tfvars_bytes.decode("utf-8"))
        result.update(contractSha256=hashlib.sha256(contract_bytes).hexdigest(),
                      tfvarsSha256=hashlib.sha256(tfvars_bytes).hexdigest())
        if request is None:
            result.update(status="not-applicable", reason="pve-certification-only")
        else:
            result.update(region=request["region"], requiredCores=request["requiredCores"],
                          subscriptionSha256=hashlib.sha256(request["subscription"].encode()).hexdigest())
            result["quotas"] = verify_usage(query_usage(request))
            result["status"] = "pass"
    except (CapacityError, OSError, ValueError) as error:
        result["reason"] = str(error) if isinstance(error, CapacityError) else "invalid Azure capacity input"
        if isinstance(error, CapacityError) and error.quotas is not None:
            result["quotas"] = error.quotas
    write_result(args.out, result)
    if result["status"] == "fail":
        print(f"Azure capacity precheck: {result['reason']}", file=sys.stderr)
        return 2
    print(f"Azure capacity precheck: {result['status']}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
