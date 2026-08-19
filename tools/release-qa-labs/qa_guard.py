#!/usr/bin/env python3
"""Fail-closed release-lab contract, provenance, plan and inventory guards."""

from __future__ import annotations

import argparse
import base64
import binascii
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


POLICY_MAX_TTL_SECONDS = 55 * 60
POLICY_MAX_PAID_LIFECYCLE_SECONDS = 85 * 60
POLICY_MAX_CLEANUP_SECONDS = 10 * 60
POLICY_MAX_INVENTORY_SECONDS = 5 * 60
POLICY_MAX_CLEANUP_ATTEMPTS = 2
QUALIFICATION_PROFILE = "representative-redundancy"
PVE_CERTIFICATION_ONLY_SCOPE = "pve-certification-only"
FULL_REPRESENTATIVE_SCOPE = "full-representative"
QUALIFICATION_RUN_SCOPES = {PVE_CERTIFICATION_ONLY_SCOPE, FULL_REPRESENTATIVE_SCOPE}
MAX_PROVISIONING_BUDGET_SECONDS = 18 * 60
MAX_QUALIFICATION_BUDGET_SECONDS = 32 * 60
MIN_SUPERVISOR_RESERVE_SECONDS = 5 * 60
REQUIRED_QUALIFICATION_SCRIPT_BLOBS = {
    "tests/e2e/cloudedge/configs/sam-e2e-generate.sh",
    "tests/e2e/cloudedge/scripts/sam-representative-redundancy.sh",
    "tests/e2e/cloudedge/scripts/sam-e2e.sh",
    "tests/e2e/cloudedge/scripts/sam-pve-qga-addresses.sh",
    "tests/e2e/cloudedge/scripts/sam-pve-bridge-audit.sh",
    "tools/release-qa-labs/drivers/pve-capture-bridge.sh",
}
# Cleanup retries still need the token, so the supervised revoker is a distinct
# post-zero phase. Its source is nevertheless part of the pre-mutation review
# boundary, so a later, unreviewed script cannot remove a credential after the
# authoritative zero-inventory result.
REQUIRED_POST_ZERO_CLEANUP_BLOBS = {
    "tools/release-qa-labs/drivers/revoke-pve-run-token.sh",
    "tools/release-qa-labs/drivers/pve-capture-bridge.sh",
    "tools/release-qa-labs/drivers/pve-orphan-cleanup.sh",
}
RUNS_ROOT = Path("/var/lib/routerd-release-qa")
POLICY_MAX_COST_USD = 1.00
APPROVED_EXECUTION_HOSTS = {"chatty", "chatty.lain.local"}
PRODUCTION_MODE = "production"
STAGING_MODE = "staging-no-mutation"
EXECUTION_MODES = {PRODUCTION_MODE, STAGING_MODE}
FRESH_STATE_MODE = "fresh-fabric-fresh-state"
PRODUCTION_ENVIRONMENT = "routerd-release-qualification"
STAGING_ENVIRONMENT = "routerd-release-qa-staging"
APPROVED_REGIONS = {"aws": "ap-northeast-1", "azure": "japaneast", "oci": "ap-tokyo-1", "pve": "local"}
APPROVED_COUNTS = {"aws": 4, "azure": 4, "oci": 4, "pve": 7}
APPROVED_TYPES = {
    "aws": {"t3.large": 2, "t3.micro": 2},
    "azure": {"Standard_B1s": 4},
    "oci": {"VM.Standard.E2.1": 4},
    "pve": {"template-stage": 1, "template-clone": 6},
}
# Deliberately conservative review rates. They are policy inputs, not a billing quote.
HOURLY_USD = {
    ("aws", "t3.large"): 0.12,
    ("aws", "t3.micro"): 0.02,
    ("azure", "Standard_B1s"): 0.03,
    ("oci", "VM.Standard.E2.1"): 0.05,
    ("pve", "template-stage"): 0.0,
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
        "aws_vpc": 1, "aws_internet_gateway": 1, "aws_subnet": 1,
        "aws_route_table": 1, "aws_route_table_association": 1,
        "aws_security_group": 1, "aws_iam_role": 1,
        "aws_iam_role_policy": 1, "aws_iam_instance_profile": 1,
        "aws_instance": 4,
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
        "terraform_data": 1,
        "proxmox_virtual_environment_vm": 7,
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


def git(checkout_root: Path, *args: str, check: bool = True) -> str:
    # The durable supervisor intentionally runs as an unprivileged account
    # against a root-owned, immutable checkout.  Scope Git's ownership
    # exception to this one canonical path and command; do not persist a
    # global safe.directory entry or trust a wildcard.
    trusted_repo = checkout_root.resolve()
    proc = subprocess.run(
        ["git", "-c", f"safe.directory={trusted_repo}", "-C", str(trusted_repo), *args],
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if check and proc.returncode:
        raise GuardError(f"git {' '.join(args)} failed in {trusted_repo}: {proc.stderr.strip()}")
    return proc.stdout.strip()


def normalized_remote(url: str) -> str:
    value = url.strip().removesuffix(".git")
    if value.startswith("git@github.com:"):
        value = "https://github.com/" + value.split(":", 1)[1]
    return value


def verify_repo(checkout_root: Path, commit: str, remote: str, *, tracked_path: Path | None = None) -> None:
    if git(checkout_root, "rev-parse", "HEAD") != commit:
        raise GuardError(f"HEAD mismatch in {checkout_root}")
    if git(checkout_root, "status", "--porcelain", "--untracked-files=all"):
        raise GuardError(f"repository is not clean: {checkout_root}")
    origin = normalized_remote(git(checkout_root, "remote", "get-url", "origin"))
    if origin != normalized_remote(remote):
        raise GuardError(f"canonical origin mismatch: {origin}")
    refs = git(checkout_root, "branch", "-r", "--contains", commit)
    if not refs:
        raise GuardError(f"commit is not reachable from a remote-tracking ref: {commit}")
    # Remote-tracking refs can be stale. Require a fresh canonical-origin
    # advertisement containing the exact reviewed commit before provisioning.
    advertised = git(checkout_root, "ls-remote", "--heads", "--tags", "origin")
    if commit not in {line.split()[0] for line in advertised.splitlines() if line.split()}:
        raise GuardError(f"commit is not freshly reachable from canonical origin: {commit}")
    if tracked_path is not None:
        relative = tracked_path.resolve().relative_to(checkout_root.resolve())
        git(checkout_root, "ls-files", "--error-unmatch", str(relative))


def verify_script_blobs(checkout_root: Path, commit: str, blobs: dict[str, Any]) -> None:
    if not blobs:
        raise GuardError("at least one reviewed script blob identity is required")
    trusted_repo = checkout_root.resolve()
    for relative, expected in blobs.items():
        if not isinstance(relative, str) or not isinstance(expected, str):
            raise GuardError("script blob identities must map paths to SHA-256 strings")
        content = subprocess.run(
            [
                "git",
                "-c",
                f"safe.directory={trusted_repo}",
                "-C",
                str(trusted_repo),
                "show",
                f"{commit}:{relative}",
            ],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        if content.returncode:
            raise GuardError(f"reviewed script is not tracked at exact commit: {relative}")
        committed = hashlib.sha256(content.stdout).hexdigest()
        working = hashlib.sha256((checkout_root / relative).read_bytes()).hexdigest()
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


_PVE_NODE_LABEL = r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?"
_PVE_RR_NAMES = ("pve-rr-a", "pve-rr-b")
_PVE_KNOWN_HOST_KEY_TYPES = frozenset({
    "ssh-ed25519",
    "ssh-rsa",
    "ecdsa-sha2-nistp256",
    "ecdsa-sha2-nistp384",
    "ecdsa-sha2-nistp521",
    "sk-ssh-ed25519@openssh.com",
})
_PVE_WORKLOAD_NAMES = ("pve-leaf-a", "pve-client-a", "pve-leaf-b", "pve-client-b")
_PVE_VM_NAMES = (*_PVE_WORKLOAD_NAMES, *_PVE_RR_NAMES)
_PVE_TFVARS_VM_IDS = {
    "pve-leaf-a": "pve_router_vm_id",
    "pve-client-a": "pve_client_vm_id",
    "pve-leaf-b": "pve_leaf_b_router_vm_id",
    "pve-client-b": "pve_leaf_b_client_vm_id",
    "pve-rr-a": "pve_rr_a_vm_id",
    "pve-rr-b": "pve_rr_b_vm_id",
}
_PVE_TEMPLATE_STAGE_TFVARS = {
    "sourceNode": "pve_template_source_node",
    "sourceTemplateVMID": "pve_template_vm_id",
    "vmid": "pve_template_stage_vm_id",
    "datastore": "pve_datastore_id",
}
_PVE_STATIC_MANAGEMENT_TFVARS = frozenset({
    "pve_leaf_a_router_management_ipv4_cidr",
    "pve_leaf_a_client_management_ipv4_cidr",
    "pve_leaf_b_router_management_ipv4_cidr",
    "pve_leaf_b_client_management_ipv4_cidr",
    "pve_rr_a_management_ipv4_cidr",
    "pve_rr_b_management_ipv4_cidr",
    "pve_management_gateway_ipv4",
})


def verify_pve_management_safety(contract: dict[str, Any]) -> None:
    """Require the release profile to leave the shared PVE management L2 passive.

    The six test VMs acquire management IPv4 only from the existing PVE
    underlay DHCP service. routerd must neither acquire an address nor
    advertise service on that shared underlay. QGA discovery occurs before
    generated routerd configuration is accepted; placing both constraints in
    the signed contract makes weakening the policy an explicit reviewable
    change.
    """

    safety = require(contract, "safety", dict)
    if safety.get("pveManagementControlPlane") != "none":
        raise GuardError(
            "safety.pveManagementControlPlane must be none; PVE management DHCP and IPv6 RA are prohibited"
        )
    pve = require(contract, "pve", dict)
    if pve.get("managementAddressSource") != "qga-dhcp":
        raise GuardError(
            "pve.managementAddressSource must be qga-dhcp; static PVE management addresses are prohibited"
        )
    for obsolete in ("managementCIDRs", "managementGatewayIPv4"):
        if obsolete in pve:
            raise GuardError(
                f"pve.{obsolete} is obsolete; QGA must discover PVE DHCP management addresses"
            )


def verify_pve_tls_policy(contract: dict[str, Any], tfvars_path: Path) -> None:
    """Make PVE API trust explicit and reject every insecure provider mode.

    The bpg/proxmox provider does not have a per-provider CA-file attribute.
    The launcher supplies the pinned CA through a child-process-only
    ``SSL_CERT_FILE`` instead.  Keeping this contract and tfvars check here
    prevents an ambient environment setting or a second assignment from
    weakening that trust boundary.
    """

    safety = require(contract, "safety", dict)
    if safety.get("pveTLS") != "pinned-ca":
        raise GuardError("safety.pveTLS must be pinned-ca for release qualification")

    assignments: list[str] = []
    assignment_start = re.compile(r"^\s*pve_insecure\s*=")
    assignment = re.compile(r"^\s*pve_insecure\s*=\s*(\S+)\s*(?:(?:#|//).*)?$")
    for line in tfvars_path.read_text(encoding="utf-8").splitlines():
        if not assignment_start.match(line):
            continue
        match = assignment.fullmatch(line)
        assignments.append(match.group(1) if match is not None else "<invalid>")
    if assignments != ["false"]:
        raise GuardError(
            "OpenTofu pve_insecure must be explicitly false exactly once for release qualification"
        )


def verify_run_scoped_pve_token(token_path: Path, run_id: str, token_owner: str) -> None:
    """Require an ephemeral PVE token that can be revoked after this run.

    The secret is intentionally never returned or included in an error.  The
    post-zero revocation hook derives the same non-secret identity from the
    immutable copy and refuses to delete a token belonging to another run.
    """

    try:
        content = token_path.read_text(encoding="utf-8")
    except (OSError, UnicodeError) as exc:
        raise GuardError(f"cannot read PVE token source: {exc}") from exc
    assignments = re.findall(
        r'^\s*pve_api_token\s*=\s*"([^"\r\n]+)"\s*(?:(?:#|//).*)?$',
        content,
        flags=re.MULTILINE,
    )
    if len(assignments) != 1:
        raise GuardError("PVE token source must contain exactly one quoted pve_api_token assignment")
    identity, separator, secret = assignments[0].partition("=")
    user, bang, token_name = identity.partition("!")
    if (
        not separator
        or not secret
        or not bang
        or user != token_owner
        or token_name != run_id
        or re.fullmatch(r"[A-Za-z0-9._-]+@[A-Za-z0-9._-]+", user) is None
        or re.fullmatch(r"[A-Za-z0-9._-]{1,64}", token_name) is None
    ):
        raise GuardError(
            "PVE API token must use a non-empty run-scoped identity whose token name equals runId"
        )


def verify_pve_node_identity(node: str, ssh_host: str, *, label: str) -> None:
    if not re.fullmatch(_PVE_NODE_LABEL, node):
        raise GuardError(f"{label}.node must be an exact short Proxmox cluster node ID")
    if len(ssh_host) > 253 or not re.fullmatch(rf"{_PVE_NODE_LABEL}(?:\.{_PVE_NODE_LABEL})+", ssh_host):
        raise GuardError(f"{label}.sshHost must be a DNS FQDN")
    if ssh_host == node or ssh_host.split(".", 1)[0] != node:
        raise GuardError(f"{label}.sshHost FQDN must identify the {label}.node cluster ID")


def pve_ssh_hosts(contract: dict[str, Any]) -> set[str]:
    """Return the three explicit PVE SSH identities used by release QA.

    Do not accept a wildcard or a hashed known_hosts entry here.  The release
    run is intentionally tied to the concrete PVE hosts declared in its
    pinned contract, which makes a host rename or an unexpected fourth host a
    reviewable input change rather than an ambient SSH configuration detail.
    """

    pve = require(contract, "pve", dict)
    hosts = {require(pve, "sshHost", str)}
    rr_nodes = require(pve, "rrNodes", dict)
    for rr_name in _PVE_RR_NAMES:
        rr = require(rr_nodes, rr_name, dict)
        hosts.add(require(rr, "sshHost", str))
    if len(hosts) != 3:
        raise GuardError("PVE contract must name three distinct SSH hosts")
    return hosts


def verify_pve_ssh_known_hosts(path: Path, contract: dict[str, Any]) -> None:
    """Require a complete, explicit host-key pin set for root@PVE SSH.

    OpenSSH normally falls back to the service account's home-directory
    known_hosts file.  That is not a stable QA input, so every production PVE
    SSH invocation instead receives this run-pinned file by explicit option.
    The restricted parser deliberately accepts ordinary, exact FQDN entries
    only; aliases, hashed entries, wildcards, and host lists would obscure the
    identity that the release contract reviewed.
    """

    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except (OSError, UnicodeError) as exc:
        raise GuardError(f"cannot read PVE SSH known_hosts: {exc}") from exc

    expected_hosts = pve_ssh_hosts(contract)
    pinned_hosts: set[str] = set()
    for line_number, raw in enumerate(lines, start=1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        fields = line.split()
        if len(fields) < 3:
            raise GuardError(f"PVE SSH known_hosts line {line_number} is malformed")
        host, key_type, key_blob = fields[:3]
        if host.startswith("@") or host.startswith("|") or "," in host or host not in expected_hosts:
            raise GuardError(
                f"PVE SSH known_hosts line {line_number} must pin one exact contract PVE FQDN"
            )
        if key_type not in _PVE_KNOWN_HOST_KEY_TYPES:
            raise GuardError(f"PVE SSH known_hosts line {line_number} has an unsupported key type")
        try:
            decoded = base64.b64decode(key_blob + "=" * (-len(key_blob) % 4), validate=True)
        except (binascii.Error, ValueError):
            raise GuardError(f"PVE SSH known_hosts line {line_number} has invalid key material") from None
        if len(decoded) < 4:
            raise GuardError(f"PVE SSH known_hosts line {line_number} has invalid key material")
        algorithm_length = int.from_bytes(decoded[:4], byteorder="big")
        try:
            algorithm = decoded[4:4 + algorithm_length].decode("ascii")
        except UnicodeDecodeError:
            raise GuardError(f"PVE SSH known_hosts line {line_number} has invalid key material") from None
        if algorithm != key_type or len(decoded) < 4 + algorithm_length:
            raise GuardError(f"PVE SSH known_hosts line {line_number} has invalid key material")
        pinned_hosts.add(host)

    missing = expected_hosts - pinned_hosts
    if missing:
        raise GuardError("PVE SSH known_hosts does not pin every contract PVE host")


def canonical_ssh_public_key(value: str, *, label: str) -> str:
    """Validate an OpenSSH public key and discard its non-semantic comment.

    The release profile deploys one guest key to PVE and every cloud provider.
    Comparing the algorithm/blob pair rather than the free-form comment keeps
    the contract, Terraform, the private key and the AWS registered key on the
    same identity without accidentally treating a comment-only difference as a
    different credential.
    """

    fields = value.strip().split()
    if len(fields) < 2:
        raise GuardError(f"{label} must be an OpenSSH public key")
    key_type, key_blob = fields[:2]
    if key_type not in _PVE_KNOWN_HOST_KEY_TYPES:
        raise GuardError(f"{label} has an unsupported OpenSSH key type")
    try:
        decoded = base64.b64decode(key_blob + "=" * (-len(key_blob) % 4), validate=True)
    except (binascii.Error, ValueError):
        raise GuardError(f"{label} has invalid OpenSSH key material") from None
    if len(decoded) < 4:
        raise GuardError(f"{label} has invalid OpenSSH key material")
    algorithm_length = int.from_bytes(decoded[:4], byteorder="big")
    try:
        algorithm = decoded[4:4 + algorithm_length].decode("ascii")
    except UnicodeDecodeError:
        raise GuardError(f"{label} has invalid OpenSSH key material") from None
    if algorithm != key_type or len(decoded) < 4 + algorithm_length:
        raise GuardError(f"{label} has invalid OpenSSH key material")
    return f"{key_type} {key_blob}"


def public_key_from_private(path: Path, *, label: str) -> str:
    """Derive a public key without exposing private-key diagnostics."""

    proc = subprocess.run(
        ["ssh-keygen", "-y", "-f", str(path)],
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
        check=False,
    )
    if proc.returncode:
        raise GuardError(f"{label} cannot derive an OpenSSH public key")
    return canonical_ssh_public_key(proc.stdout, label=label)


def read_pve_tfvars_scalars(tfvars_path: Path) -> dict[str, str | int]:
    string_names = {
        "ssh_public_key",
        "pve_node_name",
        "pve_ssh_host",
        "pve_endpoint",
        "pve_boot_source",
        "pve_template_source_node",
        "pve_datastore_id",
        "pve_underlay_bridge",
        "pve_capture_bridge",
        "pve_rr_fault_domain",
        *{
            f"pve_rr_{suffix}_{field}"
            for suffix in ("a", "b")
            for field in ("host", "ssh_host", "underlay_bridge")
        },
    }
    integer_names = set(_PVE_TFVARS_VM_IDS.values()) | {
        "pve_template_vm_id",
        "pve_template_stage_vm_id",
    }
    names = "|".join(sorted(string_names | integer_names))
    assignment = re.compile(
        rf'^\s*({names})\s*=\s*(?:"([^"\n]+)"|([0-9]+))\s*(?:#.*)?$'
    )
    values: dict[str, str | int] = {}
    for line in tfvars_path.read_text(encoding="utf-8").splitlines():
        match = assignment.fullmatch(line)
        if match is None:
            continue
        name, string_value, integer_value = match.groups()
        if name in values:
            raise GuardError(f"duplicate {name} in OpenTofu variables")
        if name in string_names:
            if string_value is None:
                raise GuardError(f"OpenTofu {name} must be a quoted string")
            values[name] = string_value
        else:
            if integer_value is None:
                raise GuardError(f"OpenTofu {name} must be an integer")
            values[name] = int(integer_value)
    return values


def require_tfvars_true(tfvars_path: Path, name: str) -> None:
    """Require one unambiguous true boolean rather than a permissive default.

    Full clones are a lifecycle safety property here: a linked child could
    retain a dependency on the disposable shared stage template.  Parsing this
    scalar separately keeps the identity parser intentionally narrow.
    """

    assignments: list[str] = []
    pattern = re.compile(rf"^\s*{re.escape(name)}\s*=\s*(\S+)\s*(?:(?:#|//).*)?$")
    for line in tfvars_path.read_text(encoding="utf-8").splitlines():
        match = pattern.fullmatch(line)
        if match is not None:
            assignments.append(match.group(1))
    if assignments != ["true"]:
        raise GuardError(f"OpenTofu {name} must be explicitly true exactly once for shared-template qualification")


def reject_static_management_tfvars(tfvars_path: Path) -> None:
    for line in tfvars_path.read_text(encoding="utf-8").splitlines():
        match = re.match(r"^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=", line)
        if match and match.group(1) in _PVE_STATIC_MANAGEMENT_TFVARS:
            raise GuardError(
                f"OpenTofu {match.group(1)} is obsolete; PVE management IPv4 must be discovered through QGA"
            )


def verify_pve_identities(contract: dict[str, Any], tfvars_path: Path) -> str:
    pve = require(contract, "pve", dict)
    node = require(pve, "node", str)
    ssh_host = require(pve, "sshHost", str)
    token_owner = require(pve, "tokenOwner", str)
    datastore = require(pve, "datastore", str).strip()
    underlay_bridge = require(pve, "underlayBridge", str).strip()
    capture_bridge = require(pve, "captureBridge", str).strip()
    verify_pve_node_identity(node, ssh_host, label="pve")
    if (
        re.fullmatch(r"[A-Za-z0-9._-]+@[A-Za-z0-9._-]+", token_owner) is None
        or token_owner.split("@", 1)[0] == "root"
    ):
        raise GuardError(
            "pve.tokenOwner must be a pre-provisioned non-root PVE service account"
        )
    if not underlay_bridge:
        raise GuardError("pve.underlayBridge must be a non-empty management/underlay bridge name")
    if not capture_bridge:
        raise GuardError("pve.captureBridge must be a non-empty run-scoped bridge name")
    if underlay_bridge == capture_bridge:
        raise GuardError("pve.underlayBridge must not equal pve.captureBridge")
    for name, value in {
        "pve.datastore": datastore,
        "pve.underlayBridge": underlay_bridge,
        "pve.captureBridge": capture_bridge,
    }.items():
        if not re.fullmatch(r"[A-Za-z0-9._-]+", value):
            raise GuardError(f"{name} must contain only safe PVE identifier characters")
    if pve.get("bootSource") != "template":
        raise GuardError(
            "pve.bootSource must be template; release qualification does not permit the removed ISO/bootstrap path"
        )

    template_stage = require(pve, "templateStage", dict)
    stage_source_node = require(template_stage, "sourceNode", str)
    stage_source_vmid = require(template_stage, "sourceTemplateVMID", int)
    stage_vmid = require(template_stage, "vmid", int)
    stage_datastore = require(template_stage, "datastore", str).strip()
    if stage_source_node != node:
        raise GuardError("pve.templateStage.sourceNode must equal pve.node")
    verify_pve_node_identity(stage_source_node, ssh_host, label="pve.templateStage.source")
    if (
        isinstance(stage_source_vmid, bool)
        or isinstance(stage_vmid, bool)
        or stage_source_vmid <= 0
        or stage_vmid <= 0
        or stage_source_vmid == stage_vmid
    ):
        raise GuardError("pve.templateStage sourceTemplateVMID and vmid must be distinct positive integers")
    if stage_datastore != datastore:
        raise GuardError("pve.templateStage.datastore must equal pve.datastore")

    if pve.get("rrFaultDomain") != "host-redundant":
        raise GuardError("pve.rrFaultDomain must be host-redundant")
    rr_nodes = require(pve, "rrNodes", dict)
    if set(rr_nodes) != set(_PVE_RR_NAMES):
        raise GuardError("pve.rrNodes must contain exactly pve-rr-a and pve-rr-b")

    expected: dict[str, str | int] = {
        "pve_node_name": node,
        "pve_ssh_host": ssh_host,
        "pve_endpoint": f"https://{ssh_host}:8006/",
        "pve_boot_source": "template",
        "pve_datastore_id": datastore,
        "pve_underlay_bridge": underlay_bridge,
        "pve_capture_bridge": capture_bridge,
        "pve_rr_fault_domain": "host-redundant",
    }
    expected.update({
        _PVE_TEMPLATE_STAGE_TFVARS["sourceNode"]: stage_source_node,
        _PVE_TEMPLATE_STAGE_TFVARS["sourceTemplateVMID"]: stage_source_vmid,
        _PVE_TEMPLATE_STAGE_TFVARS["vmid"]: stage_vmid,
        _PVE_TEMPLATE_STAGE_TFVARS["datastore"]: stage_datastore,
    })
    rr_node_ids: set[str] = set()
    rr_ssh_hosts: set[str] = set()
    rr_vmids: set[int] = set()
    for rr_name in _PVE_RR_NAMES:
        rr = rr_nodes.get(rr_name)
        if not isinstance(rr, dict):
            raise GuardError(f"pve.rrNodes.{rr_name} must be an object")
        rr_label = f"pve.rrNodes.{rr_name}"
        rr_node = require(rr, "node", str)
        rr_ssh_host = require(rr, "sshHost", str)
        rr_vmid = require(rr, "vmid", int)
        rr_underlay_bridge = require(rr, "underlayBridge", str).strip()
        if "wireGuardEndpoint" in rr:
            raise GuardError(
                f"{rr_label}.wireGuardEndpoint is obsolete; PVE local WireGuard bootstrap uses peer management addresses"
            )
        if "managementCIDR" in rr:
            raise GuardError(
                f"{rr_label}.managementCIDR is obsolete; QGA discovers PVE DHCP management addresses"
            )
        if isinstance(rr_vmid, bool) or rr_vmid <= 0:
            raise GuardError(f"{rr_label}.vmid must be a positive integer")
        if not rr_underlay_bridge:
            raise GuardError(f"{rr_label}.underlayBridge must be non-empty")
        if rr_underlay_bridge == capture_bridge:
            raise GuardError(f"{rr_label}.underlayBridge must not equal pve.captureBridge")
        verify_pve_node_identity(rr_node, rr_ssh_host, label=rr_label)
        if rr_node in rr_node_ids:
            raise GuardError("pve.rrNodes must use distinct Proxmox cluster node IDs")
        if rr_ssh_host in rr_ssh_hosts:
            raise GuardError("pve.rrNodes must use distinct SSH hosts")
        if rr_vmid in rr_vmids:
            raise GuardError("pve.rrNodes must use distinct positive VMIDs")
        rr_node_ids.add(rr_node)
        rr_ssh_hosts.add(rr_ssh_host)
        rr_vmids.add(rr_vmid)

        suffix = rr_name.removeprefix("pve-rr-")
        expected.update({
            f"pve_rr_{suffix}_host": rr_node,
            f"pve_rr_{suffix}_ssh_host": rr_ssh_host,
            f"pve_rr_{suffix}_vm_id": rr_vmid,
            f"pve_rr_{suffix}_underlay_bridge": rr_underlay_bridge,
        })

    vmids = require(pve, "vmids", dict)
    if set(vmids) != set(_PVE_VM_NAMES):
        raise GuardError("pve.vmids must map exactly the six named PVE topology nodes to VMIDs")
    if (
        any(isinstance(vmid, bool) or not isinstance(vmid, int) or vmid <= 0 for vmid in vmids.values())
        or len(set(vmids.values())) != len(vmids)
    ):
        raise GuardError("pve.vmids must contain exactly six unique positive VMIDs")
    if stage_source_vmid in vmids.values() or stage_vmid in vmids.values():
        raise GuardError("pve.templateStage VMIDs must not overlap any of the six workload VMIDs")
    for rr_name in _PVE_RR_NAMES:
        if vmids[rr_name] != rr_nodes[rr_name]["vmid"]:
            raise GuardError(f"pve.vmids.{rr_name} must equal pve.rrNodes.{rr_name}.vmid")
    for node_name, tfvars_name in _PVE_TFVARS_VM_IDS.items():
        expected[tfvars_name] = vmids[node_name]
    if node in rr_node_ids or ssh_host in rr_ssh_hosts:
        raise GuardError("pve leaf host must be distinct from both PVE RR hosts")

    reject_static_management_tfvars(tfvars_path)
    require_tfvars_true(tfvars_path, "pve_clone_full")
    values = read_pve_tfvars_scalars(tfvars_path)
    for name, wanted in expected.items():
        if values.get(name) != wanted:
            raise GuardError(f"OpenTofu {name} must equal the pinned PVE contract identity")
    return token_owner


def verify_guest_ssh_binding(contract: dict[str, Any], tfvars_path: Path, guest_key: Path, pve_key: Path) -> None:
    """Bind guest access to one non-root identity across all providers.

    PVE host administration has a separate root key.  The guest key must be
    the contract identity, must be the key Terraform deploys to every guest,
    and must not resolve to the PVE root-key identity.  The remote egress
    gate additionally verifies the same public key against the selected AWS
    EC2 key pair before any PVE clone or cloud mutation.
    """

    guest = require(contract, "guestSSH", dict)
    expected = canonical_ssh_public_key(
        require(guest, "publicKey", str), label="guestSSH.publicKey"
    )
    values = read_pve_tfvars_scalars(tfvars_path)
    tfvars_key = values.get("ssh_public_key")
    if not isinstance(tfvars_key, str):
        raise GuardError("OpenTofu ssh_public_key must be an exact quoted guest public key")
    if canonical_ssh_public_key(tfvars_key, label="OpenTofu ssh_public_key") != expected:
        raise GuardError("OpenTofu ssh_public_key does not equal contract guestSSH.publicKey")
    if public_key_from_private(guest_key, label="guest SSH private key") != expected:
        raise GuardError("guest SSH private key does not equal contract guestSSH.publicKey")
    if public_key_from_private(pve_key, label="PVE SSH private key") == expected:
        raise GuardError("guest SSH key must not equal the root PVE SSH key")


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
    # The released supervisor and the certification entry points both consume
    # the v2 provenance shape.  Rejecting the retired top-level labsCommit
    # here keeps a malformed contract from passing precheck only to fail after
    # the mutation phase has started.
    if require(contract, "schemaVersion", str) != "release-environment-contract/v2":
        raise GuardError("release QA requires release-environment-contract/v2")
    if "labsCommit" in contract:
        raise GuardError("v2 run contract must use qaImplementation.commit")
    run_id = require(contract, "runId", str)
    if not run_id or run_id in {".", ".."} or "/" in run_id:
        raise GuardError("invalid runId")
    if require(contract, "stateMode", str) != FRESH_STATE_MODE:
        raise GuardError(
            "stateMode must be fresh-fabric-fresh-state; legacy state migrations are not permitted"
        )
    run_root = RUNS_ROOT / run_id
    contract_resolved = confined_path(contract_path, run_root, "contract")
    if contract_resolved not in {run_root / "runtime/contract.json", run_root / "runtime/pinned/contract.json"}:
        raise GuardError("contract path is not canonical or pinned")
    canonical_framework = run_root / "repo/tools/release-qa-labs"
    checkout_root = run_root / "repo"
    confined_path(framework, run_root, "framework", exact=canonical_framework)

    tofu = require(contract, "tofu", dict)
    tofu_work = canonical_framework / "terraform/envs/default"
    confined_path(require(tofu, "workingDirectory", str), run_root, "tofu workingDirectory", exact=tofu_work)
    confined_path(require(tofu, "statePath", str), run_root, "tofu statePath", exact=run_root / "runtime/terraform.tfstate")
    confined_path(require(tofu, "variablesPath", str), run_root, "tofu variablesPath", exact=run_root / "runtime/terraform.tfvars")
    token_owner = verify_pve_identities(contract, run_root / "runtime/terraform.tfvars")
    verify_pve_management_safety(contract)
    verify_pve_tls_policy(contract, run_root / "runtime/terraform.tfvars")
    confined_path(require(tofu, "outputPath", str), run_root, "tofu outputPath", exact=run_root / "runtime/tofu-output-full.json")
    if "lockPath" in tofu:
        confined_path(require(tofu, "lockPath", str), run_root, "tofu lockPath", exact=tofu_work / ".terraform.lock.hcl")
    run_env_path = run_root / "runtime/pinned/run.env.json"
    confined_path(run_env_path, run_root, "run environment", exact=run_env_path)
    run_env = load_json(run_env_path)
    expected_release = confined_path(
        require(run_env, "releaseRepo", str), run_root, "release repository", exact=checkout_root
    )
    confined_path(release_repo, run_root, "release repository argument", exact=expected_release)
    https_proxy = require(run_env, "httpsProxy", str)
    match = re.fullmatch(r"http://127\.0\.0\.1:([0-9]{4,5})", https_proxy)
    if match is None or not 1024 <= int(match.group(1)) <= 65535:
        raise GuardError("httpsProxy must be the tracked IPv4 loopback proxy endpoint")
    secrets_dir = run_root / "runtime/secrets"
    if not secrets_dir.is_dir() or (secrets_dir.stat().st_mode & 0o777) != 0o700:
        raise GuardError("runtime secrets directory must have mode 0700")
    pve_token = confined_path(
        require(run_env, "pveTokenTfvars", str), run_root, "PVE token source",
        exact=secrets_dir / "pve-token.tfvars",
    )
    try:
        token_stat = pve_token.stat()
    except OSError as exc:
        raise GuardError(f"cannot stat PVE token source: {exc}") from exc
    if not pve_token.is_file() or (token_stat.st_mode & 0o777) != 0o600:
        raise GuardError("PVE token source must be a regular mode 0600 file")
    if token_stat.st_uid != os.geteuid() or not os.access(pve_token, os.R_OK):
        raise GuardError("PVE token source must be owned and readable by the executing UID")
    verify_run_scoped_pve_token(pve_token, run_id, token_owner)
    ssh_key = confined_path(
        require(run_env, "pveSshPrivateKey", str), run_root, "PVE SSH private key",
        exact=secrets_dir / "pve_ssh",
    )
    key_stat = ssh_key.stat()
    if not ssh_key.is_file() or (key_stat.st_mode & 0o777) != 0o600:
        raise GuardError("PVE SSH private key must be a regular mode 0600 file")
    if key_stat.st_uid != os.geteuid() or not os.access(ssh_key, os.R_OK):
        raise GuardError("PVE SSH private key must be owned and readable by the executing UID")
    guest_ssh_key = confined_path(
        require(run_env, "guestSshPrivateKey", str), run_root, "guest SSH private key",
        exact=secrets_dir / "guest_ssh",
    )
    try:
        guest_key_stat = guest_ssh_key.stat()
    except OSError as exc:
        raise GuardError(f"cannot stat guest SSH private key: {exc}") from exc
    if not guest_ssh_key.is_file() or (guest_key_stat.st_mode & 0o777) != 0o600:
        raise GuardError("guest SSH private key must be a regular mode 0600 file")
    if guest_key_stat.st_uid != os.geteuid() or not os.access(guest_ssh_key, os.R_OK):
        raise GuardError("guest SSH private key must be owned and readable by the executing UID")
    verify_guest_ssh_binding(contract, run_root / "runtime/terraform.tfvars", guest_ssh_key, ssh_key)
    pve_ssh_known_hosts = confined_path(
        require(run_env, "pveSshKnownHosts", str), run_root, "PVE SSH known_hosts",
        exact=secrets_dir / "pve-known_hosts",
    )
    try:
        known_hosts_stat = pve_ssh_known_hosts.stat()
    except OSError as exc:
        raise GuardError(f"cannot stat PVE SSH known_hosts: {exc}") from exc
    if not pve_ssh_known_hosts.is_file() or (known_hosts_stat.st_mode & 0o777) != 0o600:
        raise GuardError("PVE SSH known_hosts must be a regular mode 0600 file")
    if known_hosts_stat.st_uid != os.geteuid() or not os.access(pve_ssh_known_hosts, os.R_OK):
        raise GuardError("PVE SSH known_hosts must be owned and readable by the executing UID")
    verify_pve_ssh_known_hosts(pve_ssh_known_hosts, contract)
    pve_ca = confined_path(
        require(run_env, "pveCaPem", str), run_root, "PVE CA source",
        exact=secrets_dir / "pve-ca.pem",
    )
    try:
        ca_stat = pve_ca.stat()
        ca_text = pve_ca.read_text(encoding="utf-8")
    except (OSError, UnicodeError) as exc:
        raise GuardError(f"cannot read PVE CA source: {exc}") from exc
    if not pve_ca.is_file() or (ca_stat.st_mode & 0o777) != 0o600:
        raise GuardError("PVE CA source must be a regular mode 0600 file")
    if ca_stat.st_uid != os.geteuid() or not os.access(pve_ca, os.R_OK):
        raise GuardError("PVE CA source must be owned and readable by the executing UID")
    if "-----BEGIN CERTIFICATE-----" not in ca_text or "-----END CERTIFICATE-----" not in ca_text:
        raise GuardError("PVE CA source must contain PEM certificate data")
    azure_source = confined_path(
        require(run_env, "azureAuthSource", str), run_root, "Azure authentication source",
        exact=secrets_dir / "azure-auth-source",
    )
    source_stat = azure_source.stat()
    if not azure_source.is_dir() or (source_stat.st_mode & 0o777) != 0o700:
        raise GuardError("Azure authentication source must be a mode 0700 directory")
    if source_stat.st_uid != os.geteuid():
        raise GuardError("Azure authentication source must be owned by the executing UID")
    source_files = list(azure_source.rglob("*"))
    if not source_files:
        raise GuardError("Azure authentication source must not be empty")
    for source_file in source_files:
        if source_file.is_symlink():
            raise GuardError("Azure authentication source must not contain symlinks")
        stat = source_file.stat()
        wanted = 0o700 if source_file.is_dir() else 0o600
        if (not source_file.is_dir() and not source_file.is_file()) or (stat.st_mode & 0o777) != wanted:
            raise GuardError("Azure authentication source entries must be mode 0700 directories or mode 0600 files")
        if stat.st_uid != os.geteuid():
            raise GuardError("Azure authentication source entries must be owned by the executing UID")
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

    qualification = require(contract, "qualification", dict)
    profile = require(qualification, "profile", str)
    run_scope = require(qualification, "runScope", str)
    provisioning_budget = require(qualification, "provisioningBudgetSeconds", int)
    qualification_budget = require(qualification, "qualificationBudgetSeconds", int)
    supervisor_reserve = require(qualification, "minimumSupervisorReserveSeconds", int)
    if profile != QUALIFICATION_PROFILE:
        raise GuardError("release qualification must use the representative-redundancy profile")
    if run_scope not in QUALIFICATION_RUN_SCOPES:
        raise GuardError("release qualification runScope is missing or invalid")
    if not 0 < provisioning_budget <= MAX_PROVISIONING_BUDGET_SECONDS:
        raise GuardError("provision/certification budget exceeds the bounded profile")
    if not 0 < qualification_budget <= MAX_QUALIFICATION_BUDGET_SECONDS:
        raise GuardError("qualification budget exceeds the bounded profile")
    if supervisor_reserve < MIN_SUPERVISOR_RESERVE_SECONDS:
        raise GuardError("qualification contract leaves too little supervisor reserve")
    if provisioning_budget + qualification_budget + supervisor_reserve > ttl:
        raise GuardError("provision/certification and qualification budgets exceed lifecycle TTL")

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
    verify_repo(checkout_root, release_commit, release_remote)
    artifact_script_blobs = require(artifact, "scriptBlobs", dict)
    missing_qualification_blobs = REQUIRED_QUALIFICATION_SCRIPT_BLOBS - set(artifact_script_blobs)
    if missing_qualification_blobs:
        raise GuardError(
            "routerdArtifact.scriptBlobs is missing required qualification scripts: "
            f"{sorted(missing_qualification_blobs)}"
        )
    verify_script_blobs(checkout_root, release_commit, artifact_script_blobs)
    if git(checkout_root, "tag", "--points-at", "HEAD"):
        raise GuardError("release candidate must remain untagged until all gates pass")
    expected_parent = require(artifact, "parentMainCommit", str)
    if git(checkout_root, "rev-parse", "HEAD^") != expected_parent:
        raise GuardError("release candidate parent is not the frozen main commit")
    git(checkout_root, "merge-base", "--is-ancestor", expected_parent, "origin/main")
    version = require(artifact, "version", str)
    if version not in artifact_path.name:
        raise GuardError("artifact filename does not contain the exact version")

    qa = require(contract, "qaImplementation", dict)
    verify_repo(
        checkout_root,
        require(qa, "commit", str),
        require(qa, "canonicalRemote", str),
        tracked_path=framework / "qa_guard.py",
    )
    qa_script_blobs = require(qa, "scriptBlobs", dict)
    missing_post_zero_blobs = REQUIRED_POST_ZERO_CLEANUP_BLOBS - set(qa_script_blobs)
    if missing_post_zero_blobs:
        raise GuardError(
            "qaImplementation.scriptBlobs is missing required post-zero cleanup scripts: "
            f"{sorted(missing_post_zero_blobs)}"
        )
    verify_script_blobs(checkout_root, require(qa, "commit", str), qa_script_blobs)
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
        "aws_instance": ("instance_type", {"t3.large": 2, "t3.micro": 2}),
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
