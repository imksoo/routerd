#!/usr/bin/env python3
"""Fail-closed release-lab contract, provenance, plan and inventory guards."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import sys
from typing import Any, Iterable


class GuardError(RuntimeError):
    pass


POLICY_MAX_TTL_SECONDS = 45 * 60
POLICY_MAX_PAID_LIFECYCLE_SECONDS = 75 * 60
POLICY_MAX_CLEANUP_SECONDS = 10 * 60
POLICY_MAX_INVENTORY_SECONDS = 5 * 60
POLICY_MAX_CLEANUP_ATTEMPTS = 2
RUNS_ROOT = Path("/var/lib/routerd-release-qa")
POLICY_MAX_COST_USD = 1.00
APPROVED_EXECUTION_HOSTS = {"chatty", "chatty.lain.local"}
PRODUCTION_MODE = "production"
STAGING_MODE = "staging-no-mutation"
EXECUTION_MODES = {PRODUCTION_MODE, STAGING_MODE}
PRODUCTION_ENVIRONMENT = "routerd-release-qualification"
STAGING_ENVIRONMENT = "routerd-release-qa-staging"
APPROVED_REGIONS = {"aws": "ap-northeast-1", "azure": "japaneast", "oci": "ap-tokyo-1", "pve": "local"}
APPROVED_COUNTS = {"aws": 6, "azure": 4, "oci": 4, "pve": 4}
APPROVED_TYPES = {
    "aws": {"t3.medium": 2, "t3.large": 2, "t3.micro": 2},
    "azure": {"Standard_B1s": 4},
    "oci": {"VM.Standard.E2.1": 4},
    "pve": {"template-clone": 4},
}
# Deliberately conservative review rates. They are policy inputs, not a billing quote.
HOURLY_USD = {
    ("aws", "t3.medium"): 0.06,
    ("aws", "t3.large"): 0.12,
    ("aws", "t3.micro"): 0.02,
    ("azure", "Standard_B1s"): 0.03,
    ("oci", "VM.Standard.E2.1"): 0.05,
    ("pve", "template-clone"): 0.0,
}
STORAGE_AND_IPV4_ALLOWANCE_USD = 0.10
REQUIRED_ZERO_SCOPES = {
    "tofu-state",
    "aws-tagged-resources",
    "azure-resource-group",
    "azure-contained-resources",
    "oci-tagged-resources",
    "pve-vms",
    "pve-bridges",
}
PLAN_COUNTS = {
    "cloud": {
        "aws_vpc": 1, "aws_internet_gateway": 1, "aws_subnet": 2,
        "aws_route_table": 2, "aws_route_table_association": 2,
        "aws_security_group": 1, "aws_iam_role": 1,
        "aws_iam_role_policy": 1, "aws_iam_instance_profile": 1,
        "aws_instance": 6,
        "azurerm_resource_group": 1, "azurerm_virtual_network": 1,
        "azurerm_subnet": 1, "azurerm_network_security_group": 1,
        "azurerm_route_table": 1,
        "azurerm_subnet_network_security_group_association": 1,
        "azurerm_subnet_route_table_association": 1,
        "azurerm_public_ip": 4, "azurerm_network_interface": 4,
        "azurerm_linux_virtual_machine": 4, "azurerm_role_assignment": 2,
        "oci_core_vcn": 1, "oci_core_internet_gateway": 1,
        "oci_core_route_table": 1, "oci_core_security_list": 1,
        "oci_core_subnet": 1, "oci_core_instance": 4,
    },
    "pve": {
        "proxmox_virtual_environment_network_linux_bridge": 1,
        "proxmox_virtual_environment_vm": 4,
    },
}


def load_json(path: Path) -> dict[str, Any]:
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise GuardError(f"cannot read JSON {path}: {exc}") from exc
    if not isinstance(data, dict):
        raise GuardError(f"JSON root must be an object: {path}")
    return data


def require(mapping: dict[str, Any], key: str, kind: type | tuple[type, ...]) -> Any:
    value = mapping.get(key)
    if not isinstance(value, kind):
        raise GuardError(f"{key} must be {getattr(kind, '__name__', kind)}")
    return value


def git(repo: Path, *args: str, check: bool = True) -> str:
    proc = subprocess.run(
        ["git", "-C", str(repo), *args],
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if check and proc.returncode:
        raise GuardError(f"git {' '.join(args)} failed in {repo}: {proc.stderr.strip()}")
    return proc.stdout.strip()


def normalized_remote(url: str) -> str:
    value = url.strip().removesuffix(".git")
    if value.startswith("git@github.com:"):
        value = "https://github.com/" + value.split(":", 1)[1]
    return value


def verify_repo(repo: Path, commit: str, remote: str, *, tracked_path: Path | None = None) -> None:
    if git(repo, "rev-parse", "HEAD") != commit:
        raise GuardError(f"HEAD mismatch in {repo}")
    if git(repo, "status", "--porcelain", "--untracked-files=all"):
        raise GuardError(f"repository is not clean: {repo}")
    origin = normalized_remote(git(repo, "remote", "get-url", "origin"))
    if origin != normalized_remote(remote):
        raise GuardError(f"canonical origin mismatch: {origin}")
    refs = git(repo, "branch", "-r", "--contains", commit)
    if not refs:
        raise GuardError(f"commit is not reachable from a remote-tracking ref: {commit}")
    # Remote-tracking refs can be stale. Require a fresh canonical-origin
    # advertisement containing the exact reviewed commit before provisioning.
    advertised = git(repo, "ls-remote", "--heads", "--tags", "origin")
    if commit not in {line.split()[0] for line in advertised.splitlines() if line.split()}:
        raise GuardError(f"commit is not freshly reachable from canonical origin: {commit}")
    if tracked_path is not None:
        relative = tracked_path.resolve().relative_to(repo.resolve())
        git(repo, "ls-files", "--error-unmatch", str(relative))


def verify_script_blobs(repo: Path, commit: str, blobs: dict[str, Any]) -> None:
    if not blobs:
        raise GuardError("at least one reviewed script blob identity is required")
    for relative, expected in blobs.items():
        if not isinstance(relative, str) or not isinstance(expected, str):
            raise GuardError("script blob identities must map paths to SHA-256 strings")
        content = subprocess.run(
            ["git", "-C", str(repo), "show", f"{commit}:{relative}"],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        if content.returncode:
            raise GuardError(f"reviewed script is not tracked at exact commit: {relative}")
        committed = hashlib.sha256(content.stdout).hexdigest()
        working = hashlib.sha256((repo / relative).read_bytes()).hexdigest()
        if committed != expected or working != expected:
            raise GuardError(f"reviewed script blob identity mismatch: {relative}")


def duration_seconds(value: str) -> int:
    if len(value) < 2 or not value[:-1].isdigit() or value[-1] not in "smhd":
        raise GuardError(f"invalid duration: {value}")
    factor = {"s": 1, "m": 60, "h": 3600, "d": 86400}[value[-1]]
    return int(value[:-1]) * factor


def estimated_cost(ttl_seconds: int) -> float:
    hourly = sum(
        HOURLY_USD[(provider, flavor)] * count
        for provider, flavors in APPROVED_TYPES.items()
        for flavor, count in flavors.items()
    )
    return hourly * ttl_seconds / 3600 + STORAGE_AND_IPV4_ALLOWANCE_USD


def verify_pve_identities(contract: dict[str, Any], tfvars_path: Path) -> None:
    pve = require(contract, "pve", dict)
    node = require(pve, "node", str)
    ssh_host = require(pve, "sshHost", str)
    label = r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?"
    if not re.fullmatch(label, node):
        raise GuardError("pve.node must be an exact short Proxmox cluster node ID")
    if len(ssh_host) > 253 or not re.fullmatch(rf"{label}(?:\.{label})+", ssh_host):
        raise GuardError("pve.sshHost must be a DNS FQDN")
    if ssh_host == node or ssh_host.split(".", 1)[0] != node:
        raise GuardError("pve.sshHost FQDN must identify the pve.node cluster ID")

    values: dict[str, str] = {}
    assignment = re.compile(r'^\s*(pve_node_name|pve_ssh_host|pve_endpoint)\s*=\s*"([^"\n]+)"\s*(?:#.*)?$')
    for line in tfvars_path.read_text(encoding="utf-8").splitlines():
        match = assignment.fullmatch(line)
        if match:
            if match.group(1) in values:
                raise GuardError(f"duplicate {match.group(1)} in OpenTofu variables")
            values[match.group(1)] = match.group(2)
    if values.get("pve_node_name") != node:
        raise GuardError("OpenTofu pve_node_name must equal pve.node")
    if values.get("pve_ssh_host") != ssh_host:
        raise GuardError("OpenTofu pve_ssh_host must equal pve.sshHost")
    if values.get("pve_endpoint") != f"https://{ssh_host}:8006/":
        raise GuardError("OpenTofu pve_endpoint must use the exact pve.sshHost FQDN")


def confined_path(value: str | Path, root: Path, label: str, *, exact: Path | None = None) -> Path:
    path = Path(value)
    if not path.is_absolute():
        raise GuardError(f"{label} must be absolute")
    lexical = Path(os.path.abspath(path))
    resolved = path.resolve(strict=False)
    if lexical != resolved:
        raise GuardError(f"{label} contains a symlink escape or alias: {path}")
    if exact is not None and resolved != exact:
        raise GuardError(f"{label} must be exactly {exact}")
    if resolved != root and root not in resolved.parents:
        raise GuardError(f"{label} escapes canonical run root: {path}")
    return resolved


def verify_contract(contract_path: Path, release_repo: Path, framework: Path, actual_host: str | None) -> None:
    contract = load_json(contract_path)
    run_id = require(contract, "runId", str)
    if not run_id or run_id in {".", ".."} or "/" in run_id:
        raise GuardError("invalid runId")
    run_root = RUNS_ROOT / run_id
    contract_resolved = confined_path(contract_path, run_root, "contract")
    if contract_resolved not in {run_root / "runtime/contract.json", run_root / "runtime/pinned/contract.json"}:
        raise GuardError("contract path is not canonical or pinned")
    canonical_framework = run_root / "repo/tools/release-qa-labs"
    confined_path(framework, run_root, "framework", exact=canonical_framework)

    tofu = require(contract, "tofu", dict)
    tofu_work = canonical_framework / "terraform/envs/default"
    confined_path(require(tofu, "workingDirectory", str), run_root, "tofu workingDirectory", exact=tofu_work)
    confined_path(require(tofu, "statePath", str), run_root, "tofu statePath", exact=run_root / "runtime/terraform.tfstate")
    confined_path(require(tofu, "variablesPath", str), run_root, "tofu variablesPath", exact=run_root / "runtime/terraform.tfvars")
    verify_pve_identities(contract, run_root / "runtime/terraform.tfvars")
    confined_path(require(tofu, "outputPath", str), run_root, "tofu outputPath", exact=run_root / "runtime/tofu-output-full.json")
    if "lockPath" in tofu:
        confined_path(require(tofu, "lockPath", str), run_root, "tofu lockPath", exact=tofu_work / ".terraform.lock.hcl")
    run_env_path = run_root / "runtime/pinned/run.env.json"
    confined_path(run_env_path, run_root, "run environment", exact=run_env_path)
    run_env = load_json(run_env_path)
    expected_release = confined_path(
        require(run_env, "releaseRepo", str), run_root, "release repository", exact=run_root / "repo"
    )
    confined_path(release_repo, run_root, "release repository argument", exact=expected_release)
    token_path = run_env.get("pveTokenTfvars")
    if token_path:
        confined_path(require(run_env, "pveTokenTfvars", str), run_root, "PVE token source")
    secrets_dir = run_root / "runtime/secrets"
    if not secrets_dir.is_dir() or (secrets_dir.stat().st_mode & 0o777) != 0o700:
        raise GuardError("runtime secrets directory must have mode 0700")
    ssh_key = confined_path(
        require(run_env, "pveSshPrivateKey", str), run_root, "PVE SSH private key",
        exact=secrets_dir / "pve_ssh",
    )
    key_stat = ssh_key.stat()
    if not ssh_key.is_file() or (key_stat.st_mode & 0o777) != 0o600:
        raise GuardError("PVE SSH private key must be a regular mode 0600 file")
    if key_stat.st_uid != os.geteuid() or not os.access(ssh_key, os.R_OK):
        raise GuardError("PVE SSH private key must be owned and readable by the executing UID")
    lifecycle = require(contract, "lifecycle", dict)
    ttl = duration_seconds(require(lifecycle, "ttl", str))
    stale = duration_seconds(require(lifecycle, "heartbeatStale", str))
    cleanup = duration_seconds(require(lifecycle, "cleanupTimeout", str))
    inventory = duration_seconds(require(lifecycle, "inventoryTimeout", str))
    attempts = require(lifecycle, "maxCleanupAttempts", int)
    paid = require(lifecycle, "maxPaidLifecycleSeconds", int)
    if ttl > POLICY_MAX_TTL_SECONDS or stale >= ttl:
        raise GuardError("lifecycle exceeds TTL policy or stale threshold is not less than TTL")
    if lifecycle.get("cleanupScope") != "run-id":
        raise GuardError("cleanupScope must be run-id")
    worst = ttl + attempts * (cleanup + inventory)
    if (
        cleanup > POLICY_MAX_CLEANUP_SECONDS
        or inventory > POLICY_MAX_INVENTORY_SECONDS
        or attempts <= 0
        or attempts > POLICY_MAX_CLEANUP_ATTEMPTS
        or paid > POLICY_MAX_PAID_LIFECYCLE_SECONDS
        or worst > paid
    ):
        raise GuardError("paid lifecycle cleanup/retry envelope exceeds policy")

    execution = require(contract, "execution", dict)
    mode = require(execution, "mode", str)
    if mode not in EXECUTION_MODES:
        raise GuardError("contract execution mode is missing or invalid")
    environment = require(contract, "environment", str)
    if mode == STAGING_MODE:
        if not run_id.startswith("relqa-staging-") or environment != STAGING_ENVIRONMENT:
            raise GuardError("staging mode requires a staging-specific runId and environment")
    elif run_id.startswith("relqa-staging-") or environment != PRODUCTION_ENVIRONMENT:
        raise GuardError("production mode requires the production environment and non-staging runId")
    expected_host = require(execution, "host", str)
    if expected_host not in APPROVED_EXECUTION_HOSTS or execution.get("requireRemote") is not True:
        raise GuardError("execution must require an approved remote host")
    host = actual_host or socket.getfqdn()
    aliases = {host, host.split(".", 1)[0]}
    if expected_host not in aliases and expected_host.split(".", 1)[0] not in aliases:
        raise GuardError(f"wrong execution host: actual={host} expected={expected_host}")

    limits = require(contract, "limits", dict)
    ceiling = require(limits, "maxEstimatedCostUsd", (int, float))
    if ceiling <= 0 or ceiling > POLICY_MAX_COST_USD:
        raise GuardError("monetary ceiling exceeds repository policy")
    if limits.get("providerCounts") != APPROVED_COUNTS:
        raise GuardError("provider counts differ from the approved topology")
    if limits.get("instanceTypes") != APPROVED_TYPES:
        raise GuardError("instance types differ from the approved topology")
    if limits.get("regions") != APPROVED_REGIONS:
        raise GuardError("regions differ from the approved topology")
    cost = estimated_cost(paid)
    if cost > float(ceiling):
        raise GuardError(f"estimated cost {cost:.2f} exceeds ceiling {ceiling:.2f}")

    artifact = require(contract, "routerdArtifact", dict)
    artifact_path = Path(require(artifact, "path", str)).resolve()
    confined_path(require(artifact, "path", str), run_root, "artifact")
    digest = hashlib.sha256(artifact_path.read_bytes()).hexdigest()
    if digest != require(artifact, "sha256", str):
        raise GuardError("artifact SHA-256 mismatch")
    release_commit = require(artifact, "commit", str)
    release_remote = require(artifact, "canonicalRemote", str)
    verify_repo(release_repo, release_commit, release_remote)
    verify_script_blobs(release_repo, release_commit, require(artifact, "scriptBlobs", dict))
    if git(release_repo, "tag", "--points-at", "HEAD"):
        raise GuardError("release candidate must remain untagged until all gates pass")
    expected_parent = require(artifact, "parentMainCommit", str)
    if git(release_repo, "rev-parse", "HEAD^") != expected_parent:
        raise GuardError("release candidate parent is not the frozen main commit")
    git(release_repo, "merge-base", "--is-ancestor", expected_parent, "origin/main")
    version = require(artifact, "version", str)
    if version not in artifact_path.name:
        raise GuardError("artifact filename does not contain the exact version")

    qa = require(contract, "qaImplementation", dict)
    qa_repo = Path(git(framework, "rev-parse", "--show-toplevel"))
    verify_repo(
        qa_repo,
        require(qa, "commit", str),
        require(qa, "canonicalRemote", str),
        tracked_path=framework / "qa_guard.py",
    )
    verify_script_blobs(qa_repo, require(qa, "commit", str), require(qa, "scriptBlobs", dict))
    mirror = require(execution, "providerMirror", str)
    mirror_path = Path(mirror).resolve(strict=False)
    if mirror_path != RUNS_ROOT / "provider-mirror" or Path(os.path.abspath(mirror)) != mirror_path:
        raise GuardError("provider mirror is not the canonical read-only mirror")
    print(json.dumps({
        "status": "pass", "executionMode": mode,
        "estimatedCostUsd": round(cost, 2), "executionHost": host,
    }))


def walk_modules(module: dict[str, Any]) -> Iterable[dict[str, Any]]:
    yield from module.get("resources", [])
    for child in module.get("child_modules", []):
        yield from walk_modules(child)


def verify_plan(path: Path, phase: str, ceiling: float) -> None:
    plan = load_json(path)
    root = plan.get("planned_values", {}).get("root_module", {})
    resources = list(walk_modules(root))
    wanted = PLAN_COUNTS[phase]
    present_types = {str(r.get("type")) for r in resources}
    unknown = present_types - set(wanted)
    if unknown:
        raise GuardError(f"plan contains resource types outside the closed allowlist: {sorted(unknown)}")
    actual = {kind: sum(r.get("type") == kind for r in resources) for kind in wanted}
    if actual != wanted:
        raise GuardError(f"plan resource count mismatch: actual={actual} expected={wanted}")
    flavor_fields = {
        "aws_instance": ("instance_type", {"t3.medium": 2, "t3.large": 2, "t3.micro": 2}),
        "azurerm_linux_virtual_machine": ("size", {"Standard_B1s": 4}),
        "oci_core_instance": ("shape", {"VM.Standard.E2.1": 4}),
    }
    for kind, (field, expected) in flavor_fields.items():
        if kind not in wanted:
            continue
        observed: dict[str, int] = {}
        for resource in resources:
            if resource.get("type") == kind:
                value = str(resource.get("values", {}).get(field))
                observed[value] = observed.get(value, 0) + 1
        if observed != expected:
            raise GuardError(f"plan instance type mismatch for {kind}: {observed}")
    changes = plan.get("resource_changes", [])
    forbidden = [c.get("address") for c in changes if set(c.get("change", {}).get("actions", [])) - {"create", "read", "no-op"}]
    if forbidden:
        raise GuardError(f"fresh plan has non-create actions: {forbidden}")
    if estimated_cost(POLICY_MAX_PAID_LIFECYCLE_SECONDS) > ceiling:
        raise GuardError("plan exceeds monetary ceiling")
    print(json.dumps({"status": "pass", "phase": phase, "resourceCounts": actual}))


def verify_inventory(path: Path) -> None:
    inventory = load_json(path)
    scopes = inventory.get("scopes")
    if not isinstance(scopes, list):
        raise GuardError("inventory scopes must be a list")
    names = [item.get("name") for item in scopes if isinstance(item, dict)]
    if len(names) != len(set(names)):
        raise GuardError("inventory contains duplicate scope names")
    by_name = {item.get("name"): item.get("count") for item in scopes if isinstance(item, dict)}
    incomplete = {
        str(item.get("name")): item.get("queryStatus")
        for item in scopes
        if isinstance(item, dict)
        and item.get("name") in REQUIRED_ZERO_SCOPES
        and item.get("queryStatus") != "complete"
    }
    missing = REQUIRED_ZERO_SCOPES - set(by_name)
    nonzero = {name: by_name.get(name) for name in REQUIRED_ZERO_SCOPES if by_name.get(name) != 0}
    if missing or nonzero or incomplete:
        raise GuardError(
            f"inventory is not exhaustive and zero: missing={sorted(missing)} "
            f"nonzero={nonzero} incomplete={incomplete}"
        )
    print(json.dumps({"status": "pass", "scopeCount": len(REQUIRED_ZERO_SCOPES)}))


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="command", required=True)
    contract = sub.add_parser("contract")
    contract.add_argument("--contract", type=Path, required=True)
    contract.add_argument("--release-repo", type=Path, required=True)
    contract.add_argument("--framework", type=Path, required=True)
    contract.add_argument("--actual-host")
    plan = sub.add_parser("plan")
    plan.add_argument("--plan-json", type=Path, required=True)
    plan.add_argument("--phase", choices=("cloud", "pve"), required=True)
    plan.add_argument("--cost-ceiling", type=float, required=True)
    inventory = sub.add_parser("inventory")
    inventory.add_argument("--inventory-json", type=Path, required=True)
    args = parser.parse_args(argv)
    if args.command == "contract":
        verify_contract(args.contract, args.release_repo, args.framework, args.actual_host)
    elif args.command == "plan":
        verify_plan(args.plan_json, args.phase, args.cost_ceiling)
    else:
        verify_inventory(args.inventory_json)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main(sys.argv[1:]))
    except (GuardError, OSError, subprocess.SubprocessError) as exc:
        print(f"release QA guard: {exc}", file=sys.stderr)
        raise SystemExit(2)
