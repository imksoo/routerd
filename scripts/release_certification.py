#!/usr/bin/env python3
"""Release environment certification and qualification lifecycle helpers."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import fcntl
import importlib.util
import json
import os
import re
import subprocess
import sys
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
    with temporary.open("w", encoding="utf-8") as stream:
        os.fchmod(stream.fileno(), 0o600)
        stream.write(json.dumps(value, indent=2, sort_keys=True) + "\n")
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary, path)
    directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


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


def command_validate_contract(argv: list[str]) -> int:
    """Check prospective bytes without issuing evidence or invoking a driver."""
    parser = argparse.ArgumentParser(description=command_validate_contract.__doc__)
    parser.add_argument("--contract", type=Path, required=True)
    parser.add_argument("--environment", required=True)
    parser.add_argument("--topology", required=True)
    parser.add_argument("--providers", required=True)
    args = parser.parse_args(argv)
    try:
        raw = args.contract.read_bytes()
        contract = json.loads(raw)
    except (OSError, ValueError) as exc:
        raise ContractError(f"cannot read contract {args.contract}: {exc}") from exc
    if not isinstance(contract, dict):
        raise ContractError("run contract must be a JSON object")
    validate_contract(
        contract, args.environment, args.topology, normalize_providers(args.providers)
    )
    offline = offline_preflight(contract, contract_path=args.contract)
    print(json.dumps({
        "status": "pass", "validationOnly": True, "checkedAt": rfc3339(utc_now()),
        "contractPath": str(args.contract.resolve()),
        "contractSha256": hashlib.sha256(raw).hexdigest(),
        "artifactSha256": contract["routerdArtifact"]["sha256"],
        "offlinePreflight": offline,
    }, sort_keys=True))
    return 0


def offline_preflight(contract, *, contract_path=None, report_path=None):
    """Always recheck source regressions and the actual saved preparation."""
    try:
        spec = importlib.util.spec_from_file_location(
            "release_qa_preflight", SCRIPT_DIR / "release_qa_preflight.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        raw = contract_path.read_bytes() if contract_path else None
        if raw is not None and json.loads(raw) != contract:
            raise ValueError("contract changed before offline preflight")
        report = module.check(contract)
        report["checkedAt"] = rfc3339(utc_now())
        if raw is not None:
            if raw != contract_path.read_bytes():
                raise ValueError("contract changed during offline preflight")
            report["contractSha256"] = hashlib.sha256(raw).hexdigest()
    except (OSError, ValueError, ImportError) as exc:
        if report_path:
            write_json(report_path, {"status": "fail", "checkedAt": rfc3339(utc_now()), "error": str(exc)})
        raise ContractError("offline preflight: " + str(exc)) from exc
    if report_path:
        write_json(report_path, report)
    return report


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
    offline_preflight(contract, contract_path=args.contract,
                      report_path=args.out.with_suffix(".offline-preflight.json"))

    certifiers: list[dict[str, Any]] = []
    checks: list[dict[str, Any]] = []
    repairs: list[dict[str, Any]] = []
    tool_versions: dict[str, str] = {}
    other_providers: list[str] = []
    inherited_expiry = None
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
        if other["run"] != contract:
            raise ContractError(
                "input certification run contract does not match (artifact/QA/execution identity)"
            )
        inherited_expiry = parse_rfc3339(other["expiresAt"])
        certifiers.extend(other["certifiers"])
        checks.extend(other["checks"])
        repairs.extend(other["repairs"])
        tool_versions.update(other.get("toolVersions", {}))

    started = rfc3339(utc_now())
    driver_out = args.driver_out or args.out.with_suffix(".driver.json")
    result = run_driver(args.driver, args.contract, driver_out, args.repair)
    validate_driver_result(result, component, expected)

    certifiers.append(certifier_entry(component, result, started))
    checks.extend(result["checks"])
    repairs.extend(result["repairs"])
    tool_versions.update(result.get("toolVersions", {}))
    issued = utc_now()
    expires = issued + dt.timedelta(seconds=validity)
    if inherited_expiry is not None:
        expires = min(expires, inherited_expiry)
        if expires <= issued:
            raise ContractError("input certification expired while the driver ran")
    manifest = {
        "schemaVersion": "release-environment-certification/v1",
        "manifestId": f"{contract['runId']}:{component}:{int(issued.timestamp())}",
        "environment": args.environment,
        "topology": args.topology,
        "status": certification_status(certifiers, checks),
        "issuedAt": rfc3339(issued),
        "expiresAt": rfc3339(expires),
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
    offline_preflight(contract)
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


def qualification_outcome(
    driver_result: dict[str, Any], *, driver_exit: int, cleanup_exit: int,
    inventory_exit: int, aborted: Any,
) -> tuple[str, str]:
    """Keep proven violations and incomplete observations distinct at the last caller."""
    checks = driver_result.get("checks", [])
    if not isinstance(checks, list):
        return "fail", "infra_failure"
    # A later successful opportunity must not erase an observed product violation.
    if any(isinstance(check, dict) and check.get("result") == "fail"
           and check.get("classification") == "product_failure" for check in checks):
        return "fail", "product_failure"
    if (driver_result.get("classification") == "product_failure"
            and isinstance(driver_result.get("productEvidence"), list)
            and driver_result["productEvidence"]):
        return "fail", "product_failure"
    if aborted or cleanup_exit != 0 or inventory_exit != 0:
        return "fail", "infra_failure"
    if (driver_exit == 0 and driver_result.get("status") == "pass"
            and driver_result.get("classification") == "none"):
        return "pass", "none"
    classification = driver_result.get("classification")
    if classification == "product_failure":
        classification = "observation_inconclusive"
    if classification not in {"preflight_failure", "product_failure", "infra_failure", "observation_inconclusive"}:
        # A missing, contradictory or unrecognised report proves no product defect.
        classification = "infra_failure"
    return "fail", classification


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

    evidence_dir = args.evidence_dir.resolve()
    evidence_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    lock_path = evidence_dir / "supervisor-state.json.lock"
    with lock_path.open("a+") as lock:
        os.fchmod(lock.fileno(), 0o600)
        try:
            fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as exc:
            raise ContractError("another qualification supervisor owns this evidence") from exc
        return qualification_locked(args, evidence_dir, lock.fileno())


def qualification_locked(args: argparse.Namespace, evidence_dir: Path, lock_fd: int) -> int:
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
        supervisor = REPO_ROOT / "tools/release-qa-labs/lifecycle_supervisor.py"
        request = {
            "certification": certification, "release": args.release,
            "ttlSeconds": ttl_seconds, "staleSeconds": stale_seconds,
            "cleanupTimeoutSeconds": cleanup_timeout_seconds,
            "commands": {str(path): hashlib.sha256(path.read_bytes()).hexdigest()
                         for path in (qualification, cleanup, inventory, supervisor)},
        }
        request_sha = hashlib.sha256(json.dumps(request, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
        state_path = evidence_dir / "supervisor-state.json"
        admitted_certification = evidence_dir / "qualification-input.json"
        if state_path.exists():
            previous = load_json(state_path)
            if not isinstance(previous, dict) or previous.get("requestSha256") != request_sha:
                raise ContractError("qualification request differs from its durable owner")
            if load_json(admitted_certification) != certification:
                raise ContractError("admitted qualification input was modified")
        else:
            if any((evidence_dir / name).exists() for name in (
                "qualification.pid", "qualification.done", "cleanup.lock", "cleanup-result.json", "qualification-driver-result.json",
            )):
                raise ContractError("qualification ownership evidence has no durable owner; preserve it and use a fresh evidence directory")
            write_json(admitted_certification, certification)
            admitted_certification.chmod(0o600)
        command_preflight(
            [
                "--certification",
                str(admitted_certification),
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
    heartbeat = (args.heartbeat or evidence_dir / "heartbeat").resolve()
    if not state_path.exists():
        heartbeat.touch()
    driver_result_path = evidence_dir / "qualification-driver-result.json"
    command = [
        str(qualification), "--certification", str(admitted_certification),
        "--release", args.release, "--out", str(driver_result_path),
        "--heartbeat", str(heartbeat),
    ]
    # The independent durable supervisor is the sole worker/cleanup owner.
    # It inherits the already-held admission lock and survives this frontend.
    owner_command = [
        sys.executable, str(supervisor), "--run-id", run_id,
        "--state", str(state_path), "--lock-fd", str(lock_fd),
        "--request-sha256", request_sha, "--heartbeat", str(heartbeat),
        "--ttl-seconds", str(ttl_seconds), "--stale-seconds", str(stale_seconds),
        "--cleanup-timeout-seconds", str(cleanup_timeout_seconds),
        "--inventory-timeout-seconds", str(cleanup_timeout_seconds),
        "--max-paid-lifecycle-seconds", "8700",
        "--precheck-command", "/bin/true", "--mutation-command", command[0],
        *[f"--mutation-arg={value}" for value in command[1:]],
        "--cleanup-command", str(cleanup),
        *[f"--cleanup-arg={value}" for value in ("--run-id", run_id, "--evidence-dir", str(evidence_dir))],
        "--inventory-command", str(inventory),
        *[f"--inventory-arg={value}" for value in ("--run-id", run_id, "--evidence-dir", str(evidence_dir))],
        "--post-zero-command", "/bin/true",
    ]
    log_path = args.watchdog_log or evidence_dir / "supervisor.log"
    log_path.parent.mkdir(parents=True, exist_ok=True)
    with log_path.open("a") as log:
        os.fchmod(log.fileno(), 0o600)
        owner = subprocess.Popen(owner_command, stdin=subprocess.DEVNULL,
                                 stdout=log, stderr=subprocess.STDOUT,
                                 start_new_session=True, pass_fds=(lock_fd,))
    watchdog_pid = owner.pid
    owner_exit = owner.wait()
    state = load_json(state_path)
    driver_exit = state.get("mutationExit")
    if not isinstance(driver_exit, int):
        driver_exit = -1
    cleanup_exit = state.get("cleanupExit", -1)
    inventory_exit = state.get("inventoryExit", -1)
    reason = state.get("stopReason")
    aborted = None if reason == "mutation-complete" else {"reason": reason or "owner-incomplete", "runId": run_id}
    if aborted is None and (
        owner_exit not in (0, 1) or state.get("phase") not in {"DONE", "FAILED"}
        or state.get("tokenRevocationExit") != 0 or state.get("mutationPgid") is not None
    ):
        aborted = {"reason": "owner-incomplete", "runId": run_id}
    driver_result: dict[str, Any] = {}
    if driver_result_path.is_file():
        loaded = load_json(driver_result_path)
        if isinstance(loaded, dict):
            driver_result = loaded
    write_json(evidence_dir / "cleanup-result.json", {
        "cleanupExit": cleanup_exit, "inventoryExit": inventory_exit,
        "finishedAt": rfc3339(utc_now()), "runId": run_id,
    })
    result_status, classification = qualification_outcome(
        driver_result, driver_exit=driver_exit, cleanup_exit=cleanup_exit,
        inventory_exit=inventory_exit, aborted=aborted,
    )
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
    if command == "validate-contract":
        return command_validate_contract(argv)
    if command == "pve":
        return command_certify("pve", argv)
    if command == "cloud":
        return command_certify("cloud", argv)
    if command == "preflight":
        return command_preflight(argv)
    if command == "qualification":
        return command_qualification(argv)
    raise ContractError(f"unknown command: {command}")


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ContractError as exc:
        print(f"release certification: {exc}", file=sys.stderr)
        raise SystemExit(1) from exc
