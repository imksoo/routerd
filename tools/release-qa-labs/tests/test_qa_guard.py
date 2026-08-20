import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("qa_guard", ROOT / "qa_guard.py")
qa_guard = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(qa_guard)


def write_json(path, value):
    path.write_text(json.dumps(value), encoding="utf-8")
    path.chmod(0o600)


class ContractGuardTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.runs_root = self.root / "runs"
        self.run_root = self.runs_root / "run-1"
        self.release = self.run_root / "repo"
        self.framework = self.release / "tools/release-qa-labs"
        self.framework.mkdir(parents=True)
        (self.framework / "qa_guard.py").write_text("guard\n", encoding="utf-8")
        self.pve_token_revoker = self.framework / "drivers/revoke-pve-run-token.sh"
        self.pve_token_revoker.parent.mkdir()
        self.pve_token_revoker.write_text("revoker\n", encoding="utf-8")
        self.pve_capture_bridge = self.framework / "drivers/pve-capture-bridge.sh"
        self.pve_capture_bridge.write_text("bridge boundary\n", encoding="utf-8")
        self.pve_orphan_cleanup = self.framework / "drivers/pve-orphan-cleanup.sh"
        self.pve_orphan_cleanup.write_text("orphan recovery boundary\n", encoding="utf-8")
        (self.release / "reviewed.sh").write_text("release script\n", encoding="utf-8")
        self.representative_profile = self.release / "tests/e2e/cloudedge/scripts/sam-representative-redundancy.sh"
        self.e2e_harness = self.release / "tests/e2e/cloudedge/scripts/sam-e2e.sh"
        self.e2e_generator = self.release / "tests/e2e/cloudedge/configs/sam-e2e-generate.sh"
        self.pve_qga = self.release / "tests/e2e/cloudedge/scripts/sam-pve-qga-addresses.sh"
        self.pve_bridge_audit = self.release / "tests/e2e/cloudedge/scripts/sam-pve-bridge-audit.sh"
        self.representative_profile.parent.mkdir(parents=True)
        self.representative_profile.write_text("representative profile\n", encoding="utf-8")
        self.e2e_harness.write_text("e2e harness\n", encoding="utf-8")
        self.e2e_generator.parent.mkdir(parents=True)
        self.e2e_generator.write_text("e2e generator\n", encoding="utf-8")
        self.pve_qga.write_text("pve qga\n", encoding="utf-8")
        self.pve_bridge_audit.write_text("pve bridge audit\n", encoding="utf-8")
        self.runtime = self.run_root / "runtime"
        self.runtime.mkdir()
        self.artifact = self.runtime / "routerd-v1-linux-amd64.tar.gz"
        self.artifact.write_bytes(b"exact artifact")
        pinned = self.runtime / "pinned"
        pinned.mkdir()
        self.contract_path = pinned / "contract.json"
        self.tf_dir = self.framework / "terraform/envs/default"
        self.tf_dir.mkdir(parents=True)
        self.tfvars = self.runtime / "terraform.tfvars"
        self.tfvars_values = {
            "pve_node_name": "pve01",
            "pve_ssh_host": "pve01.lain.local",
            "pve_endpoint": "https://pve01.lain.local:8006/",
            "pve_boot_source": "template",
            "pve_datastore_id": "qnap",
            "pve_template_source_node": "pve01",
            "pve_template_vm_id": 9000,
            "pve_template_stage_vm_id": 9599,
            "pve_clone_full": True,
            "pve_underlay_bridge": "vmbr0",
            "pve_capture_bridge": "vmbr42",
            "pve_router_vm_id": 131,
            "pve_client_vm_id": 141,
            "pve_leaf_b_router_vm_id": 181,
            "pve_leaf_b_client_vm_id": 182,
            "pve_rr_fault_domain": "host-redundant",
            "pve_rr_a_host": "pve02",
            "pve_rr_a_ssh_host": "pve02.local",
            "pve_rr_a_vm_id": 171,
            "pve_rr_a_underlay_bridge": "vmbr0",
            "pve_rr_b_host": "pve03",
            "pve_rr_b_ssh_host": "pve03.local",
            "pve_rr_b_vm_id": 172,
            "pve_rr_b_underlay_bridge": "vmbr0",
            "pve_insecure": False,
        }
        secrets = self.runtime / "secrets"
        secrets.mkdir(mode=0o700)
        self.ssh_key = secrets / "pve_ssh"
        subprocess.run(
            ["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(self.ssh_key)],
            check=True,
        )
        self.guest_ssh_key = secrets / "guest_ssh"
        subprocess.run(
            ["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(self.guest_ssh_key)],
            check=True,
        )
        self.guest_public_key = " ".join(
            self.guest_ssh_key.with_suffix(".pub").read_text(encoding="utf-8").split()[:2]
        )
        self.tfvars_values["ssh_public_key"] = self.guest_public_key
        self.write_tfvars()
        self.tfvars.chmod(0o600)
        self.pve_known_hosts = secrets / "pve-known_hosts"
        self.pve_known_hosts.write_text(
            "\n".join((
                "pve01.lain.local ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEZpeHR1cmUtcHZlLWhvc3Qta2V5LTE=",
                "pve02.local ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEZpeHR1cmUtcHZlLWhvc3Qta2V5LTI=",
                "pve03.local ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEZpeHR1cmUtcHZlLWhvc3Qta2V5LTM=",
            )) + "\n",
            encoding="utf-8",
        )
        self.pve_known_hosts.chmod(0o600)
        self.pve_ca = secrets / "pve-ca.pem"
        self.pve_ca.write_text(
            "-----BEGIN CERTIFICATE-----\nfixture\n-----END CERTIFICATE-----\n",
            encoding="utf-8",
        )
        self.pve_ca.chmod(0o600)
        self.pve_token = secrets / "pve-token.tfvars"
        self.pve_token.write_text(
            'pve_api_token = "release-qa@pve!run-1=fixture-token-secret"\n',
            encoding="utf-8",
        )
        self.pve_token.chmod(0o600)
        self.azure_source = secrets / "azure-auth-source"
        self.azure_source.mkdir(mode=0o700)
        (self.azure_source / "azureProfile.json").write_text("{}\n", encoding="utf-8")
        (self.azure_source / "azureProfile.json").chmod(0o600)
        self.write_run_env()
        self.contract = {
            "schemaVersion": "release-environment-contract/v2",
            "runId": "run-1",
            "stateMode": "fresh-fabric-fresh-state",
            "environment": "routerd-release-qualification",
            "qualification": {
                "profile": "representative-redundancy",
                "runScope": "full-representative",
                "provisioningBudgetSeconds": 1080,
                "qualificationBudgetSeconds": 1920,
                "minimumSupervisorReserveSeconds": 300,
            },
            "safety": {
                "pveManagementControlPlane": "none",
                "pveTLS": "pinned-ca",
            },
            "lifecycle": {
                "ttl": "55m", "heartbeatStale": "5m", "cleanupScope": "run-id",
                "cleanupTimeout": "10m", "inventoryTimeout": "5m",
                "maxCleanupAttempts": 2, "maxPaidLifecycleSeconds": 5100,
            },
            "execution": {
                "mode": "production",
                "host": "chatty", "requireRemote": True,
                "providerMirror": str(self.runs_root / "provider-mirror"),
            },
            "tofu": {
                "workingDirectory": str(self.tf_dir), "statePath": str(self.runtime / "terraform.tfstate"),
                "variablesPath": str(self.tfvars), "outputPath": str(self.runtime / "tofu-output-full.json"),
            },
            "guestSSH": {"publicKey": self.guest_public_key},
            "pve": {
                "node": "pve01",
                "sshHost": "pve01.lain.local",
                "tokenOwner": "release-qa@pve",
                "datastore": "qnap",
                "bootSource": "template",
                "templateStage": {
                    "sourceNode": "pve01",
                    "sourceTemplateVMID": 9000,
                    "vmid": 9599,
                    "datastore": "qnap",
                },
                "underlayBridge": "vmbr0",
                "captureBridge": "vmbr42",
                "managementAddressSource": "qga-dhcp",
                "vmids": {
                    "pve-leaf-a": 131,
                    "pve-client-a": 141,
                    "pve-leaf-b": 181,
                    "pve-client-b": 182,
                    "pve-rr-a": 171,
                    "pve-rr-b": 172,
                },
                "rrFaultDomain": "host-redundant",
                "rrNodes": {
                    "pve-rr-a": {
                        "node": "pve02",
                        "sshHost": "pve02.local",
                        "vmid": 171,
                        "underlayBridge": "vmbr0",
                    },
                    "pve-rr-b": {
                        "node": "pve03",
                        "sshHost": "pve03.local",
                        "vmid": 172,
                        "underlayBridge": "vmbr0",
                    },
                },
            },
            "limits": {
                "maxEstimatedCostUsd": 1.0,
                "providerCounts": qa_guard.APPROVED_COUNTS,
                "instanceTypes": qa_guard.APPROVED_TYPES,
                "regions": qa_guard.APPROVED_REGIONS,
            },
            "routerdArtifact": {
                "path": str(self.artifact),
                "sha256": __import__("hashlib").sha256(self.artifact.read_bytes()).hexdigest(),
                "commit": "repo-commit",
                "canonicalRemote": "https://github.com/routerd/routerd",
                "parentMainCommit": "main-commit",
                "version": "v1",
                "scriptBlobs": {
                    "reviewed.sh": __import__("hashlib").sha256(b"release script\n").hexdigest(),
                    "tests/e2e/cloudedge/scripts/sam-representative-redundancy.sh": __import__("hashlib").sha256(b"representative profile\n").hexdigest(),
                    "tests/e2e/cloudedge/scripts/sam-e2e.sh": __import__("hashlib").sha256(b"e2e harness\n").hexdigest(),
                    "tests/e2e/cloudedge/configs/sam-e2e-generate.sh": __import__("hashlib").sha256(b"e2e generator\n").hexdigest(),
                    "tests/e2e/cloudedge/scripts/sam-pve-qga-addresses.sh": __import__("hashlib").sha256(b"pve qga\n").hexdigest(),
                    "tests/e2e/cloudedge/scripts/sam-pve-bridge-audit.sh": __import__("hashlib").sha256(b"pve bridge audit\n").hexdigest(),
                    "tools/release-qa-labs/drivers/pve-capture-bridge.sh": __import__("hashlib").sha256(b"bridge boundary\n").hexdigest(),
                },
            },
            "qaImplementation": {
                "commit": "repo-commit",
                "canonicalRemote": "https://github.com/routerd/routerd",
                "scriptBlobs": {
                    "tools/release-qa-labs/qa_guard.py": __import__("hashlib").sha256(b"guard\n").hexdigest(),
                    "tools/release-qa-labs/drivers/revoke-pve-run-token.sh": __import__("hashlib").sha256(b"revoker\n").hexdigest(),
                    "tools/release-qa-labs/drivers/pve-capture-bridge.sh": __import__("hashlib").sha256(b"bridge boundary\n").hexdigest(),
                    "tools/release-qa-labs/drivers/pve-orphan-cleanup.sh": __import__("hashlib").sha256(b"orphan recovery boundary\n").hexdigest(),
                },
            },
        }

    def write_tfvars(self, **overrides):
        values = {**self.tfvars_values, **overrides}
        lines = []
        for name, value in values.items():
            if isinstance(value, bool):
                rendered = "true" if value else "false"
            elif isinstance(value, int):
                rendered = str(value)
            else:
                rendered = f'"{value}"'
            lines.append(f"{name} = {rendered}")
        self.tfvars.write_text("\n".join(lines) + "\n", encoding="utf-8")

    def write_run_env(self, **overrides):
        values = {
            "releaseRepo": str(self.release),
            "pveSshPrivateKey": str(self.ssh_key),
            "guestSshPrivateKey": str(self.guest_ssh_key),
            "pveSshKnownHosts": str(self.pve_known_hosts),
            "pveCaPem": str(self.pve_ca),
            "pveTokenTfvars": str(self.pve_token),
            "azureAuthSource": str(self.azure_source),
            "httpsProxy": "http://127.0.0.1:18081",
        }
        values.update(overrides)
        write_json(self.runtime / "pinned/run.env.json", values)

    def tearDown(self):
        self.temp.cleanup()

    def fake_git(self, dirty_repo=None, remote_missing=False):
        qa_repo = self.release

        def invoke(repo, *args, check=True):
            repo = Path(repo)
            command = tuple(args)
            if command == ("rev-parse", "--show-toplevel"):
                return str(qa_repo)
            if command == ("rev-parse", "HEAD"):
                return "repo-commit"
            if command == ("status", "--porcelain", "--untracked-files=all"):
                return " M tracked.sh" if repo == dirty_repo else ""
            if command == ("remote", "get-url", "origin"):
                return "git@github.com:routerd/routerd.git"
            if command[:2] == ("branch", "-r"):
                return "origin/main"
            if command == ("ls-remote", "--heads", "--tags", "origin"):
                if remote_missing:
                    return ""
                return "repo-commit\trefs/heads/reviewed"
            if command[:2] == ("ls-files", "--error-unmatch"):
                return command[-1]
            if command == ("tag", "--points-at", "HEAD"):
                return ""
            if command == ("rev-parse", "HEAD^"):
                return "main-commit"
            if command == ("merge-base", "--is-ancestor", "main-commit", "origin/main"):
                return ""
            raise AssertionError(f"unexpected git call: {repo} {command}")

        return invoke

    def verify(self, git_impl):
        write_json(self.contract_path, self.contract)
        real_run = subprocess.run

        def show(command, stdout=None, stderr=None, check=False, **kwargs):
            if command and command[0] == "ssh-keygen":
                return real_run(command, stdout=stdout, stderr=stderr, check=check, **kwargs)
            repo = Path(command[4])
            relative = command[-1].split(":", 1)[1]
            return mock.Mock(returncode=0, stdout=(repo / relative).read_bytes(), stderr=b"")

        with mock.patch.object(qa_guard, "RUNS_ROOT", self.runs_root), \
             mock.patch.object(qa_guard, "git", side_effect=git_impl), \
             mock.patch.object(qa_guard.subprocess, "run", side_effect=show):
            qa_guard.verify_contract(self.contract_path, self.release, self.framework, "chatty")

    def test_exact_artifact_and_clean_provenance_pass(self):
        self.verify(self.fake_git())

    def test_public_contract_example_pve_placeholders_fail_closed(self):
        example = json.loads((ROOT / "contract.example.json").read_text(encoding="utf-8"))
        with self.assertRaisesRegex(qa_guard.GuardError, "cluster node ID"):
            qa_guard.verify_pve_identities(example, self.tfvars)

    def test_pve_short_cluster_id_and_fqdn_pair_are_mandatory(self):
        del self.contract["pve"]["sshHost"]
        with self.assertRaisesRegex(qa_guard.GuardError, "sshHost"):
            self.verify(self.fake_git())

    def test_pve_token_owner_must_be_an_explicit_non_root_service_account(self):
        for value in (None, "root@pam", "not-a-pve-user"):
            with self.subTest(value=value):
                if value is None:
                    self.contract["pve"].pop("tokenOwner")
                else:
                    self.contract["pve"]["tokenOwner"] = value
                with self.assertRaisesRegex(qa_guard.GuardError, "tokenOwner"):
                    self.verify(self.fake_git())
                self.contract["pve"]["tokenOwner"] = "release-qa@pve"

    def test_pve_swapped_short_and_fqdn_identities_are_rejected(self):
        self.contract["pve"]["node"] = "pve01.lain.local"
        self.contract["pve"]["sshHost"] = "pve01"
        with self.assertRaisesRegex(qa_guard.GuardError, "cluster node ID"):
            self.verify(self.fake_git())

    def test_only_fresh_fabric_state_is_accepted(self):
        self.contract["stateMode"] = "legacy-state-migration"
        with self.assertRaisesRegex(qa_guard.GuardError, "fresh-fabric-fresh-state"):
            self.verify(self.fake_git())

    def test_pve_management_control_plane_must_be_disabled(self):
        for value in (None, "dhcp", "allow"):
            with self.subTest(value=value):
                if value is None:
                    self.contract.pop("safety")
                else:
                    self.contract["safety"] = {
                        "pveManagementControlPlane": value,
                        "pveTLS": "pinned-ca",
                    }
                with self.assertRaisesRegex(qa_guard.GuardError, "safety|pveManagementControlPlane"):
                    self.verify(self.fake_git())
                self.contract["safety"] = {
                    "pveManagementControlPlane": "none",
                    "pveTLS": "pinned-ca",
                }

    def test_pve_tls_policy_requires_a_pinned_ca_and_explicit_false_provider_flag(self):
        for value in (None, "system-ca", "insecure"):
            with self.subTest(pve_tls=value):
                if value is None:
                    self.contract["safety"].pop("pveTLS")
                else:
                    self.contract["safety"]["pveTLS"] = value
                with self.assertRaisesRegex(qa_guard.GuardError, "pveTLS"):
                    self.verify(self.fake_git())
                self.contract["safety"]["pveTLS"] = "pinned-ca"

        self.write_tfvars(pve_insecure=True)
        with self.assertRaisesRegex(qa_guard.GuardError, "pve_insecure"):
            self.verify(self.fake_git())
        self.write_tfvars(pve_insecure="false")
        with self.assertRaisesRegex(qa_guard.GuardError, "pve_insecure"):
            self.verify(self.fake_git())
        self.write_tfvars()
        self.tfvars.write_text(
            self.tfvars.read_text(encoding="utf-8").replace("pve_insecure = false\n", ""),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(qa_guard.GuardError, "pve_insecure"):
            self.verify(self.fake_git())
        self.write_tfvars()
        self.tfvars.write_text(
            self.tfvars.read_text(encoding="utf-8") + "pve_insecure = false\n",
            encoding="utf-8",
        )
        with self.assertRaisesRegex(qa_guard.GuardError, "pve_insecure"):
            self.verify(self.fake_git())

    def test_pve_short_or_unrelated_ssh_host_is_rejected(self):
        for ssh_host in ("pve01", "other.lain.local", "192.0.2.4"):
            with self.subTest(ssh_host=ssh_host):
                self.contract["pve"]["sshHost"] = ssh_host
                with self.assertRaisesRegex(qa_guard.GuardError, "FQDN|identify"):
                    self.verify(self.fake_git())

    def test_pve_tfvars_identity_mismatch_is_rejected(self):
        cases = (
            {"pve_node_name": "pve02"},
            {"pve_ssh_host": "pve02.lain.local"},
            {"pve_endpoint": "https://pve01:8006/"},
            {"pve_underlay_bridge": "vmbr99"},
            {"pve_capture_bridge": "vmbr99"},
        )
        for overrides in cases:
            with self.subTest(overrides=overrides):
                self.write_tfvars(**overrides)
                with self.assertRaisesRegex(qa_guard.GuardError, "OpenTofu"):
                    self.verify(self.fake_git())

    def test_pve_rr_fault_domain_and_exact_pair_are_mandatory(self):
        self.contract["pve"]["rrFaultDomain"] = "same-host"
        with self.assertRaisesRegex(qa_guard.GuardError, "rrFaultDomain"):
            self.verify(self.fake_git())
        self.contract["pve"]["rrFaultDomain"] = "host-redundant"
        self.contract["pve"]["rrNodes"]["pve-rr-c"] = self.contract["pve"]["rrNodes"].pop("pve-rr-b")
        with self.assertRaisesRegex(qa_guard.GuardError, "exactly pve-rr-a and pve-rr-b"):
            self.verify(self.fake_git())

    def test_pve_rr_identity_fields_are_strict(self):
        cases = (
            ("node", "pve02.local", "cluster node ID"),
            ("sshHost", "other.local", "identify"),
            ("vmid", 0, "positive integer"),
            ("underlayBridge", "", "underlayBridge"),
        )
        rr_a = self.contract["pve"]["rrNodes"]["pve-rr-a"]
        for key, bad_value, expected in cases:
            with self.subTest(key=key, bad_value=bad_value):
                original = rr_a[key]
                rr_a[key] = bad_value
                with self.assertRaisesRegex(qa_guard.GuardError, expected):
                    self.verify(self.fake_git())
                rr_a[key] = original

    def test_pve_management_source_must_be_qga_dhcp_and_static_contract_fields_are_rejected(self):
        for value in (None, "certified-static", "dhcp"):
            with self.subTest(source=value):
                if value is None:
                    self.contract["pve"].pop("managementAddressSource")
                else:
                    self.contract["pve"]["managementAddressSource"] = value
                with self.assertRaisesRegex(qa_guard.GuardError, "managementAddressSource"):
                    self.verify(self.fake_git())
                self.contract["pve"]["managementAddressSource"] = "qga-dhcp"
        for field, value in (
            ("managementCIDRs", {"pve-leaf-a": "192.168.1.10/24"}),
            ("managementGatewayIPv4", "192.168.1.1"),
        ):
            with self.subTest(field=field):
                self.contract["pve"][field] = value
                with self.assertRaisesRegex(qa_guard.GuardError, "obsolete"):
                    self.verify(self.fake_git())
                del self.contract["pve"][field]

    def test_pve_rr_local_management_cidr_is_rejected_as_duplicate_contract_state(self):
        self.contract["pve"]["rrNodes"]["pve-rr-a"]["managementCIDR"] = "192.168.1.171/24"
        with self.assertRaisesRegex(qa_guard.GuardError, "obsolete"):
            self.verify(self.fake_git())

    def test_pve_rr_public_wireguard_endpoint_is_rejected_as_obsolete(self):
        self.contract["pve"]["rrNodes"]["pve-rr-a"]["wireGuardEndpoint"] = "pve02.example.test:51820"
        with self.assertRaisesRegex(qa_guard.GuardError, "obsolete"):
            self.verify(self.fake_git())

    def test_pve_rr_nodes_hosts_and_vmids_must_be_distinct(self):
        rr_a = self.contract["pve"]["rrNodes"]["pve-rr-a"]
        rr_b = self.contract["pve"]["rrNodes"]["pve-rr-b"]
        cases = (
            (
                {"node": rr_a["node"], "sshHost": rr_a["sshHost"]},
                "distinct Proxmox",
            ),
            ({"vmid": rr_a["vmid"]}, "distinct positive VMIDs"),
        )
        for overrides, expected in cases:
            with self.subTest(overrides=overrides):
                original = {key: rr_b[key] for key in overrides}
                rr_b.update(overrides)
                with self.assertRaisesRegex(qa_guard.GuardError, expected):
                    self.verify(self.fake_git())
                rr_b.update(original)

    def test_pve_leaf_host_must_be_distinct_from_both_rr_hosts(self):
        self.contract["pve"]["rrNodes"]["pve-rr-a"]["node"] = self.contract["pve"]["node"]
        self.contract["pve"]["rrNodes"]["pve-rr-a"]["sshHost"] = self.contract["pve"]["sshHost"]
        with self.assertRaisesRegex(qa_guard.GuardError, "leaf host must be distinct"):
            self.verify(self.fake_git())

    def test_pve_rr_underlay_never_uses_the_capture_bridge(self):
        self.contract["pve"]["rrNodes"]["pve-rr-a"]["underlayBridge"] = self.contract["pve"]["captureBridge"]
        with self.assertRaisesRegex(qa_guard.GuardError, "must not equal pve.captureBridge"):
            self.verify(self.fake_git())

    def test_pve_leaf_underlay_never_uses_the_capture_bridge(self):
        self.contract["pve"]["underlayBridge"] = self.contract["pve"]["captureBridge"]
        with self.assertRaisesRegex(qa_guard.GuardError, "pve.underlayBridge must not equal pve.captureBridge"):
            self.verify(self.fake_git())

    def test_pve_rr_tfvars_are_bound_to_the_contract(self):
        cases = (
            {"pve_rr_fault_domain": "same-host"},
            {"pve_rr_a_host": "pve04"},
            {"pve_rr_a_ssh_host": "pve04.local"},
            {"pve_rr_a_vm_id": 173},
            {"pve_rr_a_underlay_bridge": "vmbr42"},
            {"pve_rr_b_host": "pve04"},
            {"pve_rr_b_ssh_host": "pve04.local"},
            {"pve_rr_b_vm_id": 173},
            {"pve_rr_b_underlay_bridge": "vmbr42"},
        )
        for overrides in cases:
            with self.subTest(overrides=overrides):
                self.write_tfvars(**overrides)
                with self.assertRaisesRegex(qa_guard.GuardError, "OpenTofu"):
                    self.verify(self.fake_git())

    def test_static_pve_management_tfvars_are_rejected(self):
        for name in qa_guard._PVE_STATIC_MANAGEMENT_TFVARS:
            with self.subTest(name=name):
                self.write_tfvars(**{name: "192.168.1.10/24"})
                with self.assertRaisesRegex(qa_guard.GuardError, "obsolete"):
                    self.verify(self.fake_git())

    def test_pve_workload_vmids_are_bound_to_the_contract(self):
        for tfvars_name in (
            "pve_router_vm_id",
            "pve_client_vm_id",
            "pve_leaf_b_router_vm_id",
            "pve_leaf_b_client_vm_id",
        ):
            with self.subTest(tfvars_name=tfvars_name):
                self.write_tfvars(**{tfvars_name: 999})
                with self.assertRaisesRegex(qa_guard.GuardError, "OpenTofu"):
                    self.verify(self.fake_git())

    def test_pve_vmids_are_named_complete_distinct_and_match_rr_nodes(self):
        del self.contract["pve"]["vmids"]["pve-client-b"]
        with self.assertRaisesRegex(qa_guard.GuardError, "exactly the six named"):
            self.verify(self.fake_git())
        self.contract["pve"]["vmids"]["pve-client-b"] = 182
        self.contract["pve"]["vmids"]["pve-client-b"] = 181
        with self.assertRaisesRegex(qa_guard.GuardError, "exactly six unique"):
            self.verify(self.fake_git())
        self.contract["pve"]["vmids"]["pve-client-b"] = 0
        with self.assertRaisesRegex(qa_guard.GuardError, "exactly six unique positive"):
            self.verify(self.fake_git())
        self.contract["pve"]["vmids"]["pve-client-b"] = 182
        self.contract["pve"]["vmids"]["pve-rr-a"] = 173
        with self.assertRaisesRegex(qa_guard.GuardError, "must equal pve.rrNodes"):
            self.verify(self.fake_git())

    def test_pve_rr_duplicate_or_unquoted_tfvars_are_rejected(self):
        original = self.tfvars.read_text(encoding="utf-8")
        self.tfvars.write_text(
            original + 'pve_rr_a_host = "pve04"\n', encoding="utf-8"
        )
        with self.assertRaisesRegex(qa_guard.GuardError, "duplicate pve_rr_a_host"):
            self.verify(self.fake_git())
        self.write_tfvars()
        self.tfvars.write_text(
            self.tfvars.read_text(encoding="utf-8").replace(
                'pve_rr_a_host = "pve02"', "pve_rr_a_host = pve02"
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(qa_guard.GuardError, "OpenTofu pve_rr_a_host"):
            self.verify(self.fake_git())
        self.write_tfvars()
        self.tfvars.write_text(
            self.tfvars.read_text(encoding="utf-8").replace(
                "pve_router_vm_id = 131", 'pve_router_vm_id = "131"'
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(qa_guard.GuardError, "OpenTofu pve_router_vm_id"):
            self.verify(self.fake_git())

    def test_execution_mode_is_explicit_and_staging_identity_is_separate(self):
        del self.contract["execution"]["mode"]
        with self.assertRaisesRegex(qa_guard.GuardError, "mode"):
            self.verify(self.fake_git())
        self.contract["execution"]["mode"] = qa_guard.STAGING_MODE
        with self.assertRaisesRegex(qa_guard.GuardError, "staging-specific"):
            self.verify(self.fake_git())
        self.contract["environment"] = "routerd-release-qa-staging"
        self.contract["execution"]["mode"] = qa_guard.PRODUCTION_MODE
        with self.assertRaisesRegex(qa_guard.GuardError, "production environment"):
            self.verify(self.fake_git())

    def test_staging_identity_and_contract_mode_pass_together(self):
        old_root = self.run_root
        new_root = self.runs_root / "relqa-staging-fixture"
        old_root.rename(new_root)
        self.contract = json.loads(
            json.dumps(self.contract).replace(str(old_root), str(new_root))
        )
        self.contract["runId"] = new_root.name
        self.contract["environment"] = "routerd-release-qa-staging"
        self.contract["execution"]["mode"] = qa_guard.STAGING_MODE
        self.run_root = new_root
        self.release = new_root / "repo"
        self.framework = self.release / "tools/release-qa-labs"
        self.runtime = new_root / "runtime"
        self.artifact = Path(self.contract["routerdArtifact"]["path"])
        self.tf_dir = Path(self.contract["tofu"]["workingDirectory"])
        self.tfvars = Path(self.contract["tofu"]["variablesPath"])
        self.ssh_key = self.runtime / "secrets/pve_ssh"
        self.guest_ssh_key = self.runtime / "secrets/guest_ssh"
        self.pve_known_hosts = self.runtime / "secrets/pve-known_hosts"
        self.pve_ca = self.runtime / "secrets/pve-ca.pem"
        self.pve_token = self.runtime / "secrets/pve-token.tfvars"
        self.azure_source = self.runtime / "secrets/azure-auth-source"
        self.contract_path = self.runtime / "pinned/contract.json"
        self.pve_token.write_text(
            f'pve_api_token = "release-qa@pve!{self.contract["runId"]}=fixture-token-secret"\n',
            encoding="utf-8",
        )
        self.write_run_env()
        self.verify(self.fake_git())

    def test_artifact_tamper_is_rejected(self):
        self.artifact.write_bytes(b"tampered")
        with self.assertRaisesRegex(qa_guard.GuardError, "SHA-256 mismatch"):
            self.verify(self.fake_git())

    def test_dirty_release_repo_is_rejected(self):
        with self.assertRaisesRegex(qa_guard.GuardError, "repository is not clean"):
            self.verify(self.fake_git(self.release))

    def test_reviewed_script_blob_tamper_is_rejected(self):
        (self.release / "reviewed.sh").write_text("tampered script\n", encoding="utf-8")
        with self.assertRaisesRegex(qa_guard.GuardError, "script blob identity mismatch"):
            self.verify(self.fake_git())

    def test_representative_profile_and_harness_blobs_are_required(self):
        for path in qa_guard.REQUIRED_QUALIFICATION_SCRIPT_BLOBS:
            with self.subTest(path=path):
                script_blobs = self.contract["routerdArtifact"]["scriptBlobs"]
                expected = script_blobs.pop(path)
                with self.assertRaisesRegex(qa_guard.GuardError, "missing required qualification scripts"):
                    self.verify(self.fake_git())
                script_blobs[path] = expected

    def test_stale_local_remote_tracking_ref_is_not_fresh_reachability(self):
        with self.assertRaisesRegex(qa_guard.GuardError, "freshly reachable from canonical origin"):
            self.verify(self.fake_git(remote_missing=True))

    def test_internal_paths_outside_run_root_are_rejected_before_provenance(self):
        outside = self.root / "outside"
        cases = (
            ("workingDirectory", str(outside)), ("statePath", str(outside / "state")),
            ("variablesPath", str(outside / "vars")), ("outputPath", str(outside / "output")),
        )
        for key, value in cases:
            with self.subTest(key=key):
                original = self.contract["tofu"][key]
                self.contract["tofu"][key] = value
                with self.assertRaisesRegex(qa_guard.GuardError, "escapes canonical run root|must be exactly"):
                    self.verify(self.fake_git())
                self.contract["tofu"][key] = original
        original_artifact = self.contract["routerdArtifact"]["path"]
        self.contract["routerdArtifact"]["path"] = str(outside / "artifact")
        with self.assertRaisesRegex(qa_guard.GuardError, "escapes canonical run root|must be exactly"):
            self.verify(self.fake_git())
        self.contract["routerdArtifact"]["path"] = original_artifact

    def test_symlink_alias_and_release_repo_redirect_are_rejected(self):
        outside = self.root / "outside"
        outside.mkdir()
        alias = self.run_root / "terraform-alias"
        alias.symlink_to(outside, target_is_directory=True)
        original = self.contract["tofu"]["workingDirectory"]
        self.contract["tofu"]["workingDirectory"] = str(alias)
        with self.assertRaisesRegex(qa_guard.GuardError, "symlink escape or alias"):
            self.verify(self.fake_git())
        self.contract["tofu"]["workingDirectory"] = original

        run_env = self.runtime / "pinned/run.env.json"
        self.write_run_env(releaseRepo=str(outside))
        with self.assertRaisesRegex(qa_guard.GuardError, "escapes canonical run root|must be exactly"):
            self.verify(self.fake_git())

    def test_pve_ssh_private_key_is_canonical_private_regular_and_owned(self):
        secrets = self.runtime / "secrets"
        key = self.ssh_key
        run_env = self.runtime / "pinned/run.env.json"

        def set_key(value):
            self.write_run_env(pveSshPrivateKey=str(value))

        set_key(key)
        self.verify(self.fake_git())
        for label, candidate in (
            ("outside", self.root / "outside-key"),
            ("broad", key),
        ):
            with self.subTest(label=label):
                if label == "outside":
                    candidate.write_text("key", encoding="utf-8")
                    candidate.chmod(0o600)
                else:
                    candidate.chmod(0o644)
                set_key(candidate)
                with self.assertRaises(qa_guard.GuardError):
                    self.verify(self.fake_git())
                key.chmod(0o600)
        alias = secrets / "alias"
        alias.symlink_to(key)
        set_key(alias)
        with self.assertRaises(qa_guard.GuardError):
            self.verify(self.fake_git())

        set_key(key)
        with mock.patch.object(qa_guard.os, "geteuid", return_value=key.stat().st_uid + 1):
            with self.assertRaisesRegex(qa_guard.GuardError, "owned.*readable"):
                self.verify(self.fake_git())
        with mock.patch.object(qa_guard.os, "access", return_value=False):
            with self.assertRaisesRegex(qa_guard.GuardError, "owned.*readable"):
                self.verify(self.fake_git())

    def test_guest_ssh_private_key_is_canonical_private_regular_and_owned(self):
        secrets = self.runtime / "secrets"
        key = self.guest_ssh_key

        def set_key(value):
            self.write_run_env(guestSshPrivateKey=str(value))

        set_key(key)
        self.verify(self.fake_git())
        for label, candidate in (
            ("outside", self.root / "outside-guest-key"),
            ("broad", key),
        ):
            with self.subTest(label=label):
                if label == "outside":
                    candidate.write_text("key", encoding="utf-8")
                    candidate.chmod(0o600)
                else:
                    candidate.chmod(0o644)
                set_key(candidate)
                with self.assertRaises(qa_guard.GuardError):
                    self.verify(self.fake_git())
                key.chmod(0o600)
        alias = secrets / "guest-alias"
        alias.symlink_to(key)
        set_key(alias)
        with self.assertRaises(qa_guard.GuardError):
            self.verify(self.fake_git())

        set_key(key)
        with mock.patch.object(qa_guard.os, "geteuid", return_value=key.stat().st_uid + 1):
            with self.assertRaisesRegex(qa_guard.GuardError, "owned.*readable"):
                self.verify(self.fake_git())
        with mock.patch.object(qa_guard.os, "access", return_value=False):
            with self.assertRaisesRegex(qa_guard.GuardError, "owned.*readable"):
                self.verify(self.fake_git())

    def test_guest_ssh_private_key_must_differ_from_the_pve_root_key(self):
        key = self.guest_ssh_key
        original = key.read_bytes()
        pve_public_key = " ".join(
            self.ssh_key.with_suffix(".pub").read_text(encoding="utf-8").split()[:2]
        )

        # Identical content is forbidden even though the input names differ.
        key.write_bytes(self.ssh_key.read_bytes())
        key.chmod(0o600)
        with self.assertRaises(qa_guard.GuardError):
            self.verify(self.fake_git())

        # A different private-key encoding with the same public identity is
        # equally forbidden.  Changing the OpenSSH key comment reserializes
        # the private file while preserving the public key material.
        key.write_bytes(self.ssh_key.read_bytes())
        key.chmod(0o600)
        subprocess.run(
            ["ssh-keygen", "-q", "-c", "-P", "", "-C", "guest-fixture", "-f", str(key)],
            check=True,
        )
        self.assertNotEqual(key.read_bytes(), self.ssh_key.read_bytes())
        self.assertEqual(
            " ".join(key.with_suffix(".pub").read_text(encoding="utf-8").split()[:2]),
            pve_public_key,
        )
        self.contract["guestSSH"]["publicKey"] = pve_public_key
        self.tfvars_values["ssh_public_key"] = pve_public_key
        self.write_tfvars()
        self.tfvars.chmod(0o600)
        with self.assertRaises(qa_guard.GuardError):
            self.verify(self.fake_git())

        # The canonical filenames may not be two hard links to one root key.
        key.unlink()
        os.link(self.ssh_key, key)
        with self.assertRaises(qa_guard.GuardError):
            self.verify(self.fake_git())
        # Preserve a regular file until tempfile teardown so no later cleanup
        # operation observes an unexpected hard-link-only fixture.
        key.unlink()
        key.write_bytes(original)
        key.chmod(0o600)

    def test_pve_ssh_known_hosts_is_canonical_complete_and_private(self):
        known_hosts = self.pve_known_hosts
        self.write_run_env(pveSshKnownHosts=str(known_hosts))
        self.verify(self.fake_git())

        outside = self.root / "outside-known_hosts"
        outside.write_text(known_hosts.read_text(encoding="utf-8"), encoding="utf-8")
        outside.chmod(0o600)
        self.write_run_env(pveSshKnownHosts=str(outside))
        with self.assertRaisesRegex(qa_guard.GuardError, "PVE SSH known_hosts.*exactly"):
            self.verify(self.fake_git())

        self.write_run_env(pveSshKnownHosts=str(known_hosts))
        known_hosts.chmod(0o644)
        with self.assertRaisesRegex(qa_guard.GuardError, "PVE SSH known_hosts.*0600"):
            self.verify(self.fake_git())
        known_hosts.chmod(0o600)
        known_hosts.write_text(
            "pve01.lain.local ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEZpeHR1cmUtcHZlLWhvc3Qta2V5LTE=\n",
            encoding="utf-8",
        )
        with self.assertRaisesRegex(qa_guard.GuardError, "does not pin every"):
            self.verify(self.fake_git())

    def test_pve_token_is_canonical_private_and_run_scoped_without_exposing_secret(self):
        token = self.pve_token
        self.write_run_env(pveTokenTfvars=str(token))
        self.verify(self.fake_git())

        outside = self.root / "outside-token.tfvars"
        outside.write_text(
            'pve_api_token = "release-qa@pve!run-1=outside-token-secret"\n',
            encoding="utf-8",
        )
        outside.chmod(0o600)
        self.write_run_env(pveTokenTfvars=str(outside))
        with self.assertRaisesRegex(qa_guard.GuardError, "PVE token source.*exactly"):
            self.verify(self.fake_git())

        self.write_run_env(pveTokenTfvars=str(token))
        token.chmod(0o644)
        with self.assertRaisesRegex(qa_guard.GuardError, "PVE token source.*0600"):
            self.verify(self.fake_git())
        token.chmod(0o600)
        token.write_text(
            'pve_api_token = "release-qa@pve!another-run=fixture-token-secret"\n',
            encoding="utf-8",
        )
        with self.assertRaisesRegex(qa_guard.GuardError, "run-scoped"):
            self.verify(self.fake_git())
        token.write_text(
            'pve_api_token = "other-owner@pve!run-1=fixture-token-secret"\n',
            encoding="utf-8",
        )
        with self.assertRaisesRegex(qa_guard.GuardError, "run-scoped"):
            self.verify(self.fake_git())
        token.write_text('pve_api_token = "release-qa@pve!run-1="\n', encoding="utf-8")
        with self.assertRaisesRegex(qa_guard.GuardError, "run-scoped"):
            self.verify(self.fake_git())
        token.write_text(
            'pve_api_token = "release-qa@pve!run-1=fixture-token-secret"\n',
            encoding="utf-8",
        )

    def test_post_zero_cleanup_blobs_are_required(self):
        blobs = self.contract["qaImplementation"]["scriptBlobs"]
        for path in qa_guard.REQUIRED_POST_ZERO_CLEANUP_BLOBS:
            with self.subTest(path=path):
                expected = blobs.pop(path)
                with self.assertRaisesRegex(qa_guard.GuardError, "post-zero cleanup"):
                    self.verify(self.fake_git())
                blobs[path] = expected

    def test_v2_contract_rejects_retired_labs_commit_at_precheck(self):
        self.contract["labsCommit"] = "repo-commit"
        with self.assertRaisesRegex(qa_guard.GuardError, "qaImplementation.commit"):
            self.verify(self.fake_git())

    def test_release_qa_requires_the_v2_contract_shape(self):
        self.contract["schemaVersion"] = "release-environment-contract/v1"
        with self.assertRaisesRegex(qa_guard.GuardError, "release-environment-contract/v2"):
            self.verify(self.fake_git())

    def test_azure_auth_source_is_canonical_private_and_symlink_free(self):
        def set_source(value):
            self.write_run_env(azureAuthSource=str(value))

        set_source(self.azure_source)
        self.verify(self.fake_git())
        outside = self.root / "outside-azure"
        outside.mkdir(mode=0o700)
        (outside / "azureProfile.json").write_text("{}\n", encoding="utf-8")
        (outside / "azureProfile.json").chmod(0o600)
        set_source(outside)
        with self.assertRaisesRegex(qa_guard.GuardError, "escapes canonical run root|must be exactly"):
            self.verify(self.fake_git())
        set_source(self.azure_source)
        (self.azure_source / "alias").symlink_to(self.azure_source / "azureProfile.json")
        with self.assertRaisesRegex(qa_guard.GuardError, "symlink"):
            self.verify(self.fake_git())
        (self.azure_source / "alias").unlink()
        (self.azure_source / "azureProfile.json").chmod(0o644)
        with self.assertRaisesRegex(qa_guard.GuardError, "0600"):
            self.verify(self.fake_git())

    def test_pve_ca_is_canonical_private_regular_and_pem_encoded(self):
        secrets = self.runtime / "secrets"
        ca = self.pve_ca

        self.write_run_env(pveCaPem=str(ca))
        self.verify(self.fake_git())
        outside = self.root / "outside-ca.pem"
        outside.write_text(
            "-----BEGIN CERTIFICATE-----\noutside\n-----END CERTIFICATE-----\n",
            encoding="utf-8",
        )
        outside.chmod(0o600)
        self.write_run_env(pveCaPem=str(outside))
        with self.assertRaisesRegex(qa_guard.GuardError, "PVE CA source.*exactly"):
            self.verify(self.fake_git())

        self.write_run_env(pveCaPem=str(ca))
        ca.chmod(0o644)
        with self.assertRaisesRegex(qa_guard.GuardError, "PVE CA source.*0600"):
            self.verify(self.fake_git())
        ca.chmod(0o600)
        ca.write_text("not a certificate\n", encoding="utf-8")
        with self.assertRaisesRegex(qa_guard.GuardError, "PEM certificate"):
            self.verify(self.fake_git())
        ca.write_text(
            "-----BEGIN CERTIFICATE-----\nfixture\n-----END CERTIFICATE-----\n",
            encoding="utf-8",
        )
        alias = secrets / "pve-ca-alias.pem"
        alias.symlink_to(ca)
        self.write_run_env(pveCaPem=str(alias))
        with self.assertRaisesRegex(qa_guard.GuardError, "symlink escape or alias|must be exactly"):
            self.verify(self.fake_git())

    def test_paid_envelope_overrun_and_one_cent_over_ceiling_are_rejected(self):
        self.contract["lifecycle"]["ttl"] = "56m"
        with self.assertRaisesRegex(qa_guard.GuardError, "lifecycle"):
            self.verify(self.fake_git())
        self.contract["lifecycle"]["ttl"] = "55m"
        self.contract["limits"]["maxEstimatedCostUsd"] = 0.84
        with self.assertRaisesRegex(qa_guard.GuardError, "estimated cost"):
            self.verify(self.fake_git())

    def test_qualification_profile_is_bounded_inside_the_mutation_ttl(self):
        self.contract["qualification"]["profile"] = "full-validation"
        with self.assertRaisesRegex(qa_guard.GuardError, "representative-redundancy"):
            self.verify(self.fake_git())
        self.contract["qualification"]["profile"] = "representative-redundancy"
        self.contract["qualification"]["provisioningBudgetSeconds"] = 1081
        with self.assertRaisesRegex(qa_guard.GuardError, "provision/certification budget"):
            self.verify(self.fake_git())
        self.contract["qualification"]["provisioningBudgetSeconds"] = 1080
        self.contract["qualification"]["minimumSupervisorReserveSeconds"] = 299
        with self.assertRaisesRegex(qa_guard.GuardError, "supervisor reserve"):
            self.verify(self.fake_git())
        self.contract["qualification"]["minimumSupervisorReserveSeconds"] = 300
        self.contract["qualification"]["qualificationBudgetSeconds"] = 1921
        with self.assertRaisesRegex(qa_guard.GuardError, "qualification budget"):
            self.verify(self.fake_git())

    def test_qualification_run_scope_is_explicit_and_closed(self):
        self.contract["qualification"].pop("runScope")
        with self.assertRaisesRegex(qa_guard.GuardError, "runScope"):
            self.verify(self.fake_git())
        self.contract["qualification"]["runScope"] = "cloud-only"
        with self.assertRaisesRegex(qa_guard.GuardError, "runScope"):
            self.verify(self.fake_git())
        self.contract["qualification"]["runScope"] = "pve-certification-only"
        self.verify(self.fake_git())


class PlanGuardTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.path = Path(self.temp.name) / "plan.json"

    def tearDown(self):
        self.temp.cleanup()

    def plan(self, counts, extra_resources=None, actions=None):
        resources = []
        for kind, count in counts.items():
            resources.extend({"type": kind} for _ in range(count))
        resources.extend(extra_resources or [])
        return {
            "planned_values": {"root_module": {"resources": resources}},
            "resource_changes": actions or [],
        }

    def assert_rejected(self, value, phase="cloud", ceiling=1.0):
        write_json(self.path, value)
        with self.assertRaises(qa_guard.GuardError):
            qa_guard.verify_plan(self.path, phase, ceiling)

    def test_exact_cloud_plan_passes(self):
        value = self.plan(qa_guard.PLAN_COUNTS["cloud"])
        flavors = {
            "aws_instance": ["t3.large"] * 2 + ["t3.micro"] * 2,
            "azurerm_linux_virtual_machine": ["Standard_B1s"] * 4,
            "oci_core_instance": ["VM.Standard.E2.1"] * 4,
        }
        fields = {"aws_instance": "instance_type", "azurerm_linux_virtual_machine": "size", "oci_core_instance": "shape"}
        offsets = {kind: 0 for kind in flavors}
        for resource in value["planned_values"]["root_module"]["resources"]:
            kind = resource["type"]
            if kind in flavors:
                resource["values"] = {fields[kind]: flavors[kind][offsets[kind]]}
                offsets[kind] += 1
        write_json(self.path, value)
        qa_guard.verify_plan(self.path, "cloud", 1.0)

    def test_exact_pve_plan_passes(self):
        write_json(self.path, self.plan(qa_guard.PLAN_COUNTS["pve"]))
        qa_guard.verify_plan(self.path, "pve", 1.0)

    def test_legacy_aws_rr_plan_shape_is_rejected(self):
        counts = dict(qa_guard.PLAN_COUNTS["cloud"])
        counts.update({
            "aws_subnet": 2,
            "aws_route_table": 2,
            "aws_route_table_association": 2,
        })
        self.assert_rejected(self.plan(counts))

    def test_pve_plan_requires_the_rr_topology_assertion(self):
        counts = dict(qa_guard.PLAN_COUNTS["pve"])
        counts["terraform_data"] = 0
        self.assert_rejected(self.plan(counts), phase="pve")

    def test_count_overage_is_rejected(self):
        self.assert_rejected(self.plan({"aws_instance": 5, "azurerm_linux_virtual_machine": 4, "oci_core_instance": 4}))

    def test_unknown_managed_resource_type_is_rejected(self):
        self.assert_rejected(self.plan(
            {"aws_instance": 4, "azurerm_linux_virtual_machine": 4, "oci_core_instance": 4},
            extra_resources=[{"type": "aws_nat_gateway"}],
        ))

    def test_replace_or_delete_action_is_rejected(self):
        self.assert_rejected(self.plan(
            {"aws_instance": 4, "azurerm_linux_virtual_machine": 4, "oci_core_instance": 4},
            actions=[{"address": "aws_instance.old", "change": {"actions": ["delete", "create"]}}],
        ))

    def test_cost_ceiling_is_enforced(self):
        self.assert_rejected(
            self.plan({"aws_instance": 4, "azurerm_linux_virtual_machine": 4, "oci_core_instance": 4}),
            ceiling=0.01,
        )


class InventoryGuardTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.path = Path(self.temp.name) / "inventory.json"

    def tearDown(self):
        self.temp.cleanup()

    def inventory(self, **overrides):
        scopes = [{"name": name, "count": 0, "queryStatus": "complete"} for name in qa_guard.REQUIRED_ZERO_SCOPES]
        by_name = {item["name"]: item for item in scopes}
        for name, values in overrides.items():
            by_name[name].update(values)
        return {"scopes": scopes}

    def reject(self, value):
        write_json(self.path, value)
        with self.assertRaises(qa_guard.GuardError):
            qa_guard.verify_inventory(self.path)

    def test_exhaustive_zero_inventory_passes(self):
        write_json(self.path, self.inventory())
        qa_guard.verify_inventory(self.path)

    def test_nonzero_and_missing_scope_are_rejected(self):
        self.reject(self.inventory(**{"aws-tagged-resources": {"count": 1}}))
        value = self.inventory()
        value["scopes"].pop()
        self.reject(value)

    def test_query_failure_is_not_zero(self):
        self.reject(self.inventory(**{"oci-tagged-resources": {"queryStatus": "failed"}}))

    def test_partial_or_unknown_query_is_not_exhaustive(self):
        self.reject(self.inventory(**{"aws-tagged-resources": {"queryStatus": "partial"}}))
        self.reject(self.inventory(**{"pve-bridges": {"queryStatus": "unknown"}}))

    def test_invalid_json_is_rejected(self):
        self.path.write_text("{", encoding="utf-8")
        with self.assertRaises(qa_guard.GuardError):
            qa_guard.verify_inventory(self.path)

    def test_duplicate_scope_is_rejected(self):
        value = self.inventory()
        value["scopes"].append({"name": "tofu-state", "count": 0, "queryStatus": "complete"})
        self.reject(value)


if __name__ == "__main__":
    unittest.main()
