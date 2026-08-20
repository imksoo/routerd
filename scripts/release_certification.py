#!/usr/bin/env python3
"""Release environment certification and qualification lifecycle helpers."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import re
import signal
import subprocess
import sys
import time
from pathlib import Path
from typing import Any


SCRIPT_DIR = Path(__file__).resolve().parent
REPO_ROOT = SCRIPT_DIR.parent
CERT_SCHEMA = (
    REPO_ROOT
    / "docs/releases/manifests/release-environment-certification.schema.json"
)
QUALIFICATION_SCHEMA = (
    REPO_ROOT / "docs/releases/manifests/release-qualification-result.schema.json"
)
PROVIDERS = ("pve", "aws", "azure", "oci")
CERTIFIER_NAMES = {
    "pve": "certify-pve-substrate.sh",
    "cloud": "certify-cloud-substrate.sh",
}


class ContractError(RuntimeError):
    """A release contract or lifecycle invariant failed."""


def utc_now() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0)


def rfc3339(value: dt.datetime) -> str:
    return value.astimezone(dt.timezone.utc).isoformat().replace("+00:00", "Z")


def parse_rfc3339(value: str) -> dt.datetime:
    normalized = value[:-1] + "+00:00" if value.endswith("Z") else value
    parsed = dt.datetime.fromisoformat(normalized)
    if parsed.tzinfo is None:
        raise ContractError(f"date-time lacks an offset: {value}")
    return parsed.astimezone(dt.timezone.utc)


def parse_duration(value: str) -> int:
    match = re.fullmatch(r"([1-9][0-9]*)([smhd])", value)
    if not match:
        raise ContractError(f"invalid duration {value!r}; use values such as 5m or 24h")
    number = int(match.group(1))
    multiplier = {"s": 1, "m": 60, "h": 3600, "d": 86400}[match.group(2)]
    return number * multiplier


def load_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise ContractError(f"file not found: {path}") from exc
    except json.JSONDecodeError as exc:
        raise ContractError(f"invalid JSON in {path}: {exc}") from exc


def write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(f".{path.name}.{os.getpid()}.tmp")
    temporary.write_text(
        json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )
    os.replace(temporary, path)


def _matches_type(value: Any, expected: str) -> bool:
    if expected == "object":
        return isinstance(value, dict)
    if expected == "array":
        return isinstance(value, list)
    if expected == "string":
        return isinstance(value, str)
    if expected == "integer":
        return isinstance(value, int) and not isinstance(value, bool)
    if expected == "number":
        return isinstance(value, (int, float)) and not isinstance(value, bool)
    if expected == "boolean":
        return isinstance(value, bool)
    if expected == "null":
        return value is None
    raise ContractError(f"unsupported schema type: {expected}")


def validate_schema(value: Any, schema: dict[str, Any], location: str = "$") -> None:
    """Validate the JSON Schema subset used by release evidence schemas."""
    if "$ref" in schema:
        raise ContractError(f"{location}: $ref is not supported by the local validator")
    if "oneOf" in schema:
        matches = 0
        for candidate in schema["oneOf"]:
            try:
                validate_schema(value, candidate, location)
            except ContractError:
                continue
            matches += 1
        if matches != 1:
            raise ContractError(f"{location}: expected exactly one oneOf schema match")
    if "const" in schema and value != schema["const"]:
        raise ContractError(f"{location}: expected constant {schema['const']!r}")
    if "enum" in schema and value not in schema["enum"]:
        raise ContractError(f"{location}: value {value!r} is not in {schema['enum']!r}")
    expected_type = schema.get("type")
    if expected_type is not None:
        types = expected_type if isinstance(expected_type, list) else [expected_type]
        if not any(_matches_type(value, item) for item in types):
            raise ContractError(f"{location}: expected type {expected_type!r}")
    if isinstance(value, dict):
        required = schema.get("required", [])
        for name in required:
            if name not in value:
                raise ContractError(f"{location}: missing required property {name!r}")
        properties = schema.get("properties", {})
        additional = schema.get("additionalProperties", True)
        for name, child in value.items():
            child_location = f"{location}.{name}"
            if name in properties:
                validate_schema(child, properties[name], child_location)
            elif additional is False:
                raise ContractError(f"{location}: unexpected property {name!r}")
            elif isinstance(additional, dict):
                validate_schema(child, additional, child_location)
    if isinstance(value, list):
        minimum = schema.get("minItems")
        if minimum is not None and len(value) < minimum:
            raise ContractError(f"{location}: requires at least {minimum} items")
        if schema.get("uniqueItems"):
            encoded = [json.dumps(item, sort_keys=True) for item in value]
            if len(encoded) != len(set(encoded)):
                raise ContractError(f"{location}: items must be unique")
        item_schema = schema.get("items")
        if isinstance(item_schema, dict):
            for index, item in enumerate(value):
                validate_schema(item, item_schema, f"{location}[{index}]")
    if isinstance(value, str):
        minimum = schema.get("minLength")
        if minimum is not None and len(value) < minimum:
            raise ContractError(f"{location}: requires at least {minimum} characters")
        pattern = schema.get("pattern")
        if pattern is not None and re.search(pattern, value) is None:
            raise ContractError(f"{location}: does not match {pattern!r}")
        if schema.get("format") == "date-time":
            parse_rfc3339(value)
    if isinstance(value, int) and not isinstance(value, bool):
        minimum = schema.get("minimum")
        maximum = schema.get("maximum")
        if minimum is not None and value < minimum:
            raise ContractError(f"{location}: must be at least {minimum}")
        if maximum is not None and value > maximum:
            raise ContractError(f"{location}: must be at most {maximum}")


def validate_document(document: Any, schema_path: Path) -> None:
    schema = load_json(schema_path)
    if not isinstance(schema, dict):
        raise ContractError(f"schema is not an object: {schema_path}")
    validate_schema(document, schema)


def normalize_providers(value: str) -> list[str]:
    providers = [item.strip() for item in value.split(",") if item.strip()]
    if not providers:
        raise ContractError("provider set is empty")
    unknown = sorted(set(providers) - set(PROVIDERS))
    if unknown:
        raise ContractError(f"unknown providers: {','.join(unknown)}")
    if len(providers) != len(set(providers)):
        raise ContractError("provider set contains duplicates")
    return sorted(providers)


def validate_contract(
    contract: dict[str, Any], environment: str, topology: str, providers: list[str]
) -> str:
    # The certification manifest embeds the full run contract.  Validate the
    # exact embedded schema before a provider driver can run, rather than
    # discovering a contract/schema drift only while serializing evidence
    # after a potentially mutating certification step.
    certification_schema = load_json(CERT_SCHEMA)
    try:
        run_schema = certification_schema["properties"]["run"]
    except (KeyError, TypeError) as exc:
        raise ContractError("release certification schema lacks the embedded run schema") from exc
    if not isinstance(run_schema, dict):
        raise ContractError("release certification embedded run schema is invalid")
    validate_schema(contract, run_schema, "$")

    required = {
        "schemaVersion",
        "runId",
        "environment",
        "topology",
        "stateMode",
        "routerdArtifact",
        "providers",
        "tofu",
        "pve",
        "lifecycle",
    }
    missing = sorted(required - set(contract))
    if missing:
        raise ContractError(f"run contract is missing: {', '.join(missing)}")
    schema_version = contract["schemaVersion"]
    if schema_version not in {
        "release-environment-contract/v1",
        "release-environment-contract/v2",
    }:
        raise ContractError("unsupported run contract schemaVersion")
    if schema_version == "release-environment-contract/v1":
        if "qaImplementation" in contract:
            raise ContractError("v1 run contract must use labsCommit")
        labs_commit = contract.get("labsCommit")
        provenance_name = "labsCommit"
    else:
        if "labsCommit" in contract:
            raise ContractError("v2 run contract must use qaImplementation.commit")
        qa_implementation = contract.get("qaImplementation")
        if not isinstance(qa_implementation, dict):
            raise ContractError("v2 run contract requires qaImplementation")
        labs_commit = qa_implementation.get("commit")
        provenance_name = "qaImplementation.commit"
    if contract["environment"] != environment:
        raise ContractError("run contract environment does not match the request")
    if contract["topology"] != topology:
        raise ContractError("run contract topology does not match the request")
    contract_providers = sorted(item["name"] for item in contract["providers"])
    if contract_providers != sorted(providers):
        raise ContractError(
            f"run contract providers {contract_providers!r} do not match {sorted(providers)!r}"
        )
    artifact = contract["routerdArtifact"]
    artifact_path = Path(artifact["path"])
    if not artifact_path.is_file():
        raise ContractError(f"release artifact does not exist: {artifact_path}")
    digest_builder = hashlib.sha256()
    with artifact_path.open("rb") as artifact_stream:
        for chunk in iter(lambda: artifact_stream.read(1024 * 1024), b""):
            digest_builder.update(chunk)
    digest = digest_builder.hexdigest()
    if digest != artifact["sha256"]:
        raise ContractError(
            f"release artifact checksum mismatch: expected {artifact['sha256']}, got {digest}"
        )
    if not re.fullmatch(r"[0-9a-f]{40}", artifact["commit"]):
        raise ContractError("routerdArtifact.commit must be a full 40-character commit")
    if not isinstance(labs_commit, str) or not re.fullmatch(
        r"[0-9a-f]{40}", labs_commit
    ):
        raise ContractError(
            f"{provenance_name} must be a full 40-character commit"
        )
    if contract["lifecycle"]["cleanupScope"] != "run-id":
        raise ContractError("cleanupScope must be run-id")
    return labs_commit


def run_driver(
    executable: Path,
    contract_path: Path,
    output_path: Path,
    repair: bool,
) -> dict[str, Any]:
    if not executable.is_file() or not os.access(executable, os.X_OK):
        raise ContractError(f"driver is not executable: {executable}")
    command = [
        str(executable),
        "--contract",
        str(contract_path),
        "--out",
        str(output_path),
    ]
    if repair:
        command.append("--repair")
    completed = subprocess.run(command, check=False)
    if not output_path.is_file():
        raise ContractError(
            f"driver did not write its result (exit {completed.returncode}): {output_path}"
        )
    result = load_json(output_path)
    if not isinstance(result, dict):
        raise ContractError("driver result must be a JSON object")
    if completed.returncode != 0 and result.get("status") == "pass":
        raise ContractError("driver exited nonzero but reported pass")
    return result


def validate_driver_result(
    result: dict[str, Any], component: str, providers: list[str]
) -> None:
    if result.get("status") not in {"pass", "fail", "blocked"}:
        raise ContractError("driver status must be pass, fail, or blocked")
    checks = result.get("checks")
    repairs = result.get("repairs")
    if not isinstance(checks, list) or not checks:
        raise ContractError("driver must return at least one check")
    if not isinstance(repairs, list):
        raise ContractError("driver repairs must be an array")
    for index, check in enumerate(checks):
        if not isinstance(check, dict):
            raise ContractError(f"driver check {index} is not an object")
        for field in ("name", "component", "result", "checkedAt"):
            if field not in check:
                raise ContractError(f"driver check {index} is missing {field}")
        parse_rfc3339(check["checkedAt"])
        if check["result"] not in {"pass", "fail", "blocked", "skipped"}:
            raise ContractError(f"driver check {index} has an invalid result")
        if component == "pve" and check["component"] not in {
            "pve",
            "cross-substrate",
            "tooling",
        }:
            raise ContractError(f"PVE driver emitted cloud check {check['name']!r}")
        if component == "cloud" and check["component"] not in {
            "cloud",
            "cross-substrate",
            "tooling",
        }:
            raise ContractError(f"cloud driver emitted PVE check {check['name']!r}")
    observed = {
        check.get("provider")
        for check in checks
        if check.get("result") == "pass" and check.get("provider")
    }
    missing = sorted(set(providers) - observed)
    if missing:
        raise ContractError(
            f"driver has no passing provider-scoped check for: {', '.join(missing)}"
        )
    if result["status"] == "pass" and any(
        check["result"] in {"fail", "blocked"} for check in checks
    ):
        raise ContractError("driver reported pass with failed or blocked checks")


def certifier_entry(component: str, result: dict[str, Any], started: str) -> dict[str, Any]:
    return {
        "name": CERTIFIER_NAMES[component],
        "version": "1",
        "result": result["status"],
        "startedAt": started,
        "finishedAt": rfc3339(utc_now()),
    }


def certification_status(
    certifiers: list[dict[str, Any]], checks: list[dict[str, Any]]
) -> str:
    results = {entry["result"] for entry in certifiers}
    check_results = {entry["result"] for entry in checks}
    if "blocked" in results or "blocked" in check_results:
        return "blocked"
    if "fail" in results or "fail" in check_results:
        return "fail"
    return "pass"


def command_certify(component: str, argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog=CERTIFIER_NAMES[component])
    parser.add_argument("--environment", required=True)
    parser.add_argument("--topology", required=True)
    parser.add_argument("--providers", default="pve" if component == "pve" else "")
    parser.add_argument("--contract", type=Path, required=True)
    parser.add_argument("--driver", type=Path, required=True)
    parser.add_argument("--driver-out", type=Path)
    parser.add_argument("--pve-certification", type=Path)
    parser.add_argument("--cloud-certification", type=Path)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--repair", action="store_true")
    parser.add_argument("--valid-for", default="24h")
    args = parser.parse_args(argv)

    providers = normalize_providers(args.providers)
    expected = ["pve"] if component == "pve" else [p for p in providers if p != "pve"]
    if component == "pve" and "pve" not in providers:
        raise ContractError("PVE certifier requires pve in the provider set")
    if component == "cloud" and not expected:
        raise ContractError("cloud certifier requires at least one cloud provider")
    validity = parse_duration(args.valid_for)
    if validity > 168 * 3600:
        raise ContractError("certification validity cannot exceed 168h")

    contract = load_json(args.contract)
    if not isinstance(contract, dict):
        raise ContractError("run contract must be a JSON object")
    if component == "pve" and args.pve_certification:
        raise ContractError("PVE certifier cannot consume --pve-certification")
    if component == "cloud" and args.cloud_certification:
        raise ContractError("cloud certifier cannot consume --cloud-certification")
    other_certification_path = (
        args.pve_certification if component == "cloud" else args.cloud_certification
    )
    contract_provider_set = sorted(item["name"] for item in contract["providers"])
    labs_commit = validate_contract(
        contract, args.environment, args.topology, contract_provider_set
    )

    started = rfc3339(utc_now())
    driver_out = args.driver_out or args.out.with_suffix(".driver.json")
    result = run_driver(args.driver, args.contract, driver_out, args.repair)
    validate_driver_result(result, component, expected)

    certifiers: list[dict[str, Any]] = []
    checks: list[dict[str, Any]] = []
    repairs: list[dict[str, Any]] = []
    tool_versions: dict[str, str] = {}
    other_providers: list[str] = []
    if other_certification_path:
        other = load_json(other_certification_path)
        validate_document(other, CERT_SCHEMA)
        other_providers = other["providers"]
        if component == "cloud" and other_providers != ["pve"]:
            raise ContractError("cloud certifier requires a PVE-only input manifest")
        if component == "pve" and (
            not other_providers or "pve" in other_providers
        ):
            raise ContractError("PVE certifier requires a cloud-only input manifest")
        assert_certification_matches(
            other,
            args.environment,
            args.topology,
            other_providers,
            require_pass=True,
        )
        if other["run"]["runId"] != contract["runId"]:
            raise ContractError(
                "input certification runId does not match the run contract"
            )
        certifiers.extend(other["certifiers"])
        checks.extend(other["checks"])
        repairs.extend(other["repairs"])
        tool_versions.update(other.get("toolVersions", {}))

    certifiers.append(certifier_entry(component, result, started))
    checks.extend(result["checks"])
    repairs.extend(result["repairs"])
    tool_versions.update(result.get("toolVersions", {}))
    issued = utc_now()
    manifest = {
        "schemaVersion": "release-environment-certification/v1",
        "manifestId": f"{contract['runId']}:{component}:{int(issued.timestamp())}",
        "environment": args.environment,
        "topology": args.topology,
        "status": certification_status(certifiers, checks),
        "issuedAt": rfc3339(issued),
        "expiresAt": rfc3339(issued + dt.timedelta(seconds=validity)),
        "routerdCommit": contract["routerdArtifact"]["commit"],
        "labsCommit": labs_commit,
        "providers": sorted(set(expected) | set(other_providers)),
        "certifiers": certifiers,
        "checks": checks,
        "repairs": repairs,
        "run": contract,
        "toolVersions": tool_versions,
        "qualificationPolicy": {
            "noRepairDuringQualification": True,
            "defaultValidityHours": max(1, validity // 3600),
        },
        "notes": result.get("notes", ""),
    }
    validate_document(manifest, CERT_SCHEMA)
    write_json(args.out, manifest)
    print(f"{CERTIFIER_NAMES[component]}: {manifest['status']} ({args.out})")
    return 0 if manifest["status"] == "pass" else 1


def assert_certification_matches(
    certification: dict[str, Any],
    environment: str,
    topology: str,
    providers: list[str],
    *,
    allow_provider_subset: bool = False,
    require_pass: bool = True,
) -> None:
    if require_pass and certification["status"] != "pass":
        raise ContractError(
            f"certification status is {certification['status']!r}, not 'pass'"
        )
    if parse_rfc3339(certification["expiresAt"]) <= utc_now():
        raise ContractError("certification has expired")
    if certification["environment"] != environment:
        raise ContractError("certification environment does not match")
    if certification["topology"] != topology:
        raise ContractError("certification topology does not match")
    actual = set(certification["providers"])
    requested = set(providers)
    if allow_provider_subset:
        if not actual.issubset(requested):
            raise ContractError("certification providers are outside the requested set")
    elif actual != requested:
        raise ContractError(
            f"certification providers {sorted(actual)!r} do not match {sorted(requested)!r}"
        )
    names = {entry["name"] for entry in certification["certifiers"]}
    if "pve" in requested and "certify-pve-substrate.sh" not in names:
        raise ContractError("PVE certifier result is missing")
    if requested & {"aws", "azure", "oci"} and "certify-cloud-substrate.sh" not in names:
        raise ContractError("cloud certifier result is missing")
    passed = {
        check.get("provider")
        for check in certification["checks"]
        if check["result"] == "pass" and check.get("provider")
    }
    missing = sorted(requested - passed)
    if missing:
        raise ContractError(
            f"certification lacks passing provider checks for: {', '.join(missing)}"
        )


def command_preflight(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="release-environment-preflight.sh")
    parser.add_argument("--certification", type=Path, required=True)
    parser.add_argument("--environment", required=True)
    parser.add_argument("--topology", required=True)
    parser.add_argument("--providers", required=True)
    parser.add_argument("--release")
    args = parser.parse_args(argv)
    providers = normalize_providers(args.providers)
    certification = load_json(args.certification)
    validate_document(certification, CERT_SCHEMA)
    assert_certification_matches(
        certification, args.environment, args.topology, providers
    )
    contract = certification["run"]
    validate_contract(contract, args.environment, args.topology, providers)
    if args.release and args.release not in {
        contract["routerdArtifact"]["version"],
        contract["routerdArtifact"]["commit"],
        contract["routerdArtifact"]["commit"][:8],
    }:
        raise ContractError("requested release does not match the certified artifact")
    print(
        "release-environment-preflight.sh: pass "
        f"(manifest={certification['manifestId']})"
    )
    return 0


def executable(path: str, label: str) -> Path:
    value = Path(path)
    if not value.is_file() or not os.access(value, os.X_OK):
        raise ContractError(f"{label} is not executable: {value}")
    return value.resolve()


def run_lifecycle_command(
    command: Path,
    run_id: str,
    evidence_dir: Path,
    label: str,
    timeout_seconds: int,
) -> int:
    evidence_dir.mkdir(parents=True, exist_ok=True)
    output = evidence_dir / f"{label}.log"
    with output.open("a", encoding="utf-8") as stream:
        process = subprocess.Popen(
            [
                str(command),
                "--run-id",
                run_id,
                "--evidence-dir",
                str(evidence_dir),
            ],
            stdout=stream,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )
        try:
            return process.wait(timeout=timeout_seconds)
        except subprocess.TimeoutExpired:
            stream.write(
                f"{label}: timed out after {timeout_seconds}s; terminating process group\n"
            )
            stream.flush()
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                return process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait()
                return 124


def cleanup_once(
    cleanup: Path,
    inventory: Path,
    run_id: str,
    evidence_dir: Path,
    timeout_seconds: int,
) -> tuple[int, int]:
    lock = evidence_dir / "cleanup.lock"
    try:
        lock.mkdir(parents=True)
        owner = True
    except FileExistsError:
        owner = False
    if not owner:
        for _ in range(180):
            result_path = evidence_dir / "cleanup-result.json"
            if result_path.is_file():
                result = load_json(result_path)
                return int(result["cleanupExit"]), int(result["inventoryExit"])
            time.sleep(1)
        return 124, 124
    cleanup_exit = run_lifecycle_command(
        cleanup, run_id, evidence_dir, "cleanup", timeout_seconds
    )
    inventory_exit = run_lifecycle_command(
        inventory,
        run_id,
        evidence_dir,
        "post-cleanup-inventory",
        timeout_seconds,
    )
    write_json(
        evidence_dir / "cleanup-result.json",
        {
            "cleanupExit": cleanup_exit,
            "inventoryExit": inventory_exit,
            "finishedAt": rfc3339(utc_now()),
            "runId": run_id,
        },
    )
    return cleanup_exit, inventory_exit


def terminate_qualification(pid_file: Path) -> None:
    if not pid_file.is_file():
        return
    try:
        pid = int(pid_file.read_text(encoding="utf-8").strip())
    except (OSError, ValueError):
        return
    try:
        os.killpg(pid, signal.SIGTERM)
    except ProcessLookupError:
        return
    time.sleep(2)
    try:
        os.killpg(pid, signal.SIGKILL)
    except ProcessLookupError:
        return


def command_watchdog(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="release qualification watchdog")
    parser.add_argument("--run-id", required=True)
    parser.add_argument("--evidence-dir", type=Path, required=True)
    parser.add_argument("--heartbeat", type=Path, required=True)
    parser.add_argument("--done", type=Path, required=True)
    parser.add_argument("--abort", type=Path, required=True)
    parser.add_argument("--pid-file", type=Path, required=True)
    parser.add_argument("--cleanup-command", required=True)
    parser.add_argument("--inventory-command", required=True)
    parser.add_argument("--ttl-seconds", type=int, required=True)
    parser.add_argument("--stale-seconds", type=int, required=True)
    parser.add_argument("--cleanup-timeout-seconds", type=int, required=True)
    args = parser.parse_args(argv)
    cleanup = executable(args.cleanup_command, "cleanup command")
    inventory = executable(args.inventory_command, "inventory command")
    started = time.time()
    while args.done.exists() is False:
        now = time.time()
        reason = ""
        if now - started >= args.ttl_seconds:
            reason = "ttl-expired"
        else:
            try:
                age = now - args.heartbeat.stat().st_mtime
            except FileNotFoundError:
                age = now - started
            if age >= args.stale_seconds:
                reason = "heartbeat-stale"
        if reason:
            write_json(
                args.abort,
                {
                    "reason": reason,
                    "runId": args.run_id,
                    "triggeredAt": rfc3339(utc_now()),
                },
            )
            terminate_qualification(args.pid_file)
            cleanup_exit, inventory_exit = cleanup_once(
                cleanup,
                inventory,
                args.run_id,
                args.evidence_dir,
                args.cleanup_timeout_seconds,
            )
            return 0 if cleanup_exit == 0 and inventory_exit == 0 else 1
        time.sleep(min(5, max(1, args.stale_seconds // 4)))
    return 0


def start_watchdog(
    args: argparse.Namespace,
    run_id: str,
    evidence_dir: Path,
    heartbeat: Path,
    done: Path,
    abort: Path,
    pid_file: Path,
    ttl_seconds: int,
    stale_seconds: int,
    cleanup_timeout_seconds: int,
) -> int:
    log_path = args.watchdog_log or evidence_dir / "watchdog.log"
    log_path.parent.mkdir(parents=True, exist_ok=True)
    command = [
        sys.executable,
        str(Path(__file__).resolve()),
        "watchdog",
        "--run-id",
        run_id,
        "--evidence-dir",
        str(evidence_dir),
        "--heartbeat",
        str(heartbeat),
        "--done",
        str(done),
        "--abort",
        str(abort),
        "--pid-file",
        str(pid_file),
        "--cleanup-command",
        str(args.cleanup_command),
        "--inventory-command",
        str(args.inventory_command),
        "--ttl-seconds",
        str(ttl_seconds),
        "--stale-seconds",
        str(stale_seconds),
        "--cleanup-timeout-seconds",
        str(cleanup_timeout_seconds),
    ]
    with log_path.open("a", encoding="utf-8") as stream:
        process = subprocess.Popen(
            command,
            stdin=subprocess.DEVNULL,
            stdout=stream,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )
    return process.pid


def command_qualification(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="release-qualification-smoke.sh")
    parser.add_argument("--certification", type=Path, required=True)
    parser.add_argument("--release", required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--qualification-command", required=True)
    parser.add_argument("--cleanup-command", required=True)
    parser.add_argument("--inventory-command", required=True)
    parser.add_argument("--evidence-dir", type=Path, required=True)
    parser.add_argument("--heartbeat", type=Path)
    parser.add_argument("--ttl", default="75m")
    parser.add_argument("--heartbeat-stale", default="5m")
    parser.add_argument("--cleanup-timeout", default="15m")
    parser.add_argument("--watchdog-log", type=Path)
    args = parser.parse_args(argv)

    started = utc_now()
    try:
        qualification = executable(
            args.qualification_command, "qualification command"
        )
        cleanup = executable(args.cleanup_command, "cleanup command")
        inventory = executable(args.inventory_command, "inventory command")
        ttl_seconds = parse_duration(args.ttl)
        stale_seconds = parse_duration(args.heartbeat_stale)
        cleanup_timeout_seconds = parse_duration(args.cleanup_timeout)
        if stale_seconds >= ttl_seconds:
            raise ContractError("heartbeat stale duration must be shorter than TTL")
        certification = load_json(args.certification)
        validate_document(certification, CERT_SCHEMA)
        providers = certification["providers"]
        command_preflight(
            [
                "--certification",
                str(args.certification),
                "--environment",
                certification["environment"],
                "--topology",
                certification["topology"],
                "--providers",
                ",".join(providers),
                "--release",
                args.release,
            ]
        )
    except ContractError as exc:
        fallback: dict[str, Any] = {}
        try:
            loaded = load_json(args.certification)
            if isinstance(loaded, dict):
                fallback = loaded
        except ContractError:
            pass
        result = {
            "schemaVersion": "release-qualification-result/v1",
            "runId": fallback.get("run", {}).get("runId", "unknown"),
            "certificationManifestId": fallback.get("manifestId", "unknown"),
            "release": args.release,
            "status": "fail",
            "classification": "preflight_failure",
            "startedAt": rfc3339(started),
            "finishedAt": rfc3339(utc_now()),
            "driverExit": -1,
            "cleanupExit": -1,
            "inventoryExit": -1,
            "watchdogPid": None,
            "watchdogAbort": None,
            "checks": [{"name": "release preflight", "result": "fail", "summary": str(exc)}],
        }
        validate_document(result, QUALIFICATION_SCHEMA)
        write_json(args.out, result)
        print(f"release-qualification-smoke.sh: preflight failure ({args.out})")
        return 1

    contract = certification["run"]
    run_id = contract["runId"]
    evidence_dir = args.evidence_dir.resolve()
    evidence_dir.mkdir(parents=True, exist_ok=True)
    heartbeat = (args.heartbeat or evidence_dir / "heartbeat").resolve()
    done = evidence_dir / "qualification.done"
    abort = evidence_dir / "watchdog-abort.json"
    pid_file = evidence_dir / "qualification.pid"
    heartbeat.touch()
    watchdog_pid = start_watchdog(
        args,
        run_id,
        evidence_dir,
        heartbeat,
        done,
        abort,
        pid_file,
        ttl_seconds,
        stale_seconds,
        cleanup_timeout_seconds,
    )
    if watchdog_pid <= 0:
        raise ContractError("independent watchdog did not start")

    driver_result_path = evidence_dir / "qualification-driver-result.json"
    command = [
        str(qualification),
        "--certification",
        str(args.certification.resolve()),
        "--release",
        args.release,
        "--out",
        str(driver_result_path),
        "--heartbeat",
        str(heartbeat),
    ]
    driver = subprocess.Popen(command, start_new_session=True)
    pid_file.write_text(f"{driver.pid}\n", encoding="utf-8")
    driver_exit = driver.wait()
    cleanup_exit, inventory_exit = cleanup_once(
        cleanup, inventory, run_id, evidence_dir, cleanup_timeout_seconds
    )
    done.touch()

    driver_result: dict[str, Any] = {}
    if driver_result_path.is_file():
        loaded = load_json(driver_result_path)
        if isinstance(loaded, dict):
            driver_result = loaded
    aborted = load_json(abort) if abort.is_file() else None
    result_status = "pass"
    classification = "none"
    if aborted:
        result_status = "fail"
        classification = "infra_failure"
    elif cleanup_exit != 0 or inventory_exit != 0:
        result_status = "fail"
        classification = "infra_failure"
    elif driver_exit != 0 or driver_result.get("status") != "pass":
        result_status = "fail"
        classification = driver_result.get("classification", "product_failure")
    result = {
        "schemaVersion": "release-qualification-result/v1",
        "runId": run_id,
        "certificationManifestId": certification["manifestId"],
        "release": args.release,
        "status": result_status,
        "classification": classification,
        "startedAt": rfc3339(started),
        "finishedAt": rfc3339(utc_now()),
        "driverExit": driver_exit,
        "cleanupExit": cleanup_exit,
        "inventoryExit": inventory_exit,
        "watchdogPid": watchdog_pid,
        "watchdogAbort": aborted,
        "checks": driver_result.get("checks", []),
    }
    validate_document(result, QUALIFICATION_SCHEMA)
    write_json(args.out, result)
    print(f"release-qualification-smoke.sh: {result_status} ({args.out})")
    return 0 if result_status == "pass" else 1


def main() -> int:
    if len(sys.argv) < 2:
        raise ContractError("missing command")
    command, argv = sys.argv[1], sys.argv[2:]
    if command == "pve":
        return command_certify("pve", argv)
    if command == "cloud":
        return command_certify("cloud", argv)
    if command == "preflight":
        return command_preflight(argv)
    if command == "qualification":
        return command_qualification(argv)
    if command == "watchdog":
        return command_watchdog(argv)
    raise ContractError(f"unknown command: {command}")


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ContractError as exc:
        print(f"release certification: {exc}", file=sys.stderr)
        raise SystemExit(1) from exc
