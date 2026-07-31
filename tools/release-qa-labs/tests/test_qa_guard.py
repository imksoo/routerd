import importlib.util
import json
from pathlib import Path
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
        (self.release / "reviewed.sh").write_text("release script\n", encoding="utf-8")
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
        self.tfvars.write_text(
            'pve_node_name = "pve01"\npve_ssh_host = "pve01.lain.local"\n'
            'pve_endpoint = "https://pve01.lain.local:8006/"\n',
            encoding="utf-8",
        )
        self.tfvars.chmod(0o600)
        secrets = self.runtime / "secrets"
        secrets.mkdir(mode=0o700)
        self.ssh_key = secrets / "pve_ssh"
        self.ssh_key.write_text("fixture private key\n", encoding="utf-8")
        self.ssh_key.chmod(0o600)
        self.azure_source = secrets / "azure-auth-source"
        self.azure_source.mkdir(mode=0o700)
        (self.azure_source / "azureProfile.json").write_text("{}\n", encoding="utf-8")
        (self.azure_source / "azureProfile.json").chmod(0o600)
        write_json(pinned / "run.env.json", {
            "releaseRepo": str(self.release), "pveSshPrivateKey": str(self.ssh_key),
            "azureAuthSource": str(self.azure_source),
        })
        self.contract = {
            "runId": "run-1",
            "environment": "routerd-release-qualification",
            "lifecycle": {
                "ttl": "45m", "heartbeatStale": "5m", "cleanupScope": "run-id",
                "cleanupTimeout": "10m", "inventoryTimeout": "5m",
                "maxCleanupAttempts": 2, "maxPaidLifecycleSeconds": 4500,
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
            "pve": {"node": "pve01", "sshHost": "pve01.lain.local"},
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
                },
            },
            "qaImplementation": {
                "commit": "repo-commit",
                "canonicalRemote": "https://github.com/routerd/routerd",
                "scriptBlobs": {
                    "tools/release-qa-labs/qa_guard.py": __import__("hashlib").sha256(b"guard\n").hexdigest(),
                },
            },
        }

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
        def show(command, stdout=None, stderr=None, check=False):
            repo = Path(command[2])
            relative = command[-1].split(":", 1)[1]
            return mock.Mock(returncode=0, stdout=(repo / relative).read_bytes(), stderr=b"")

        with mock.patch.object(qa_guard, "RUNS_ROOT", self.runs_root), \
             mock.patch.object(qa_guard, "git", side_effect=git_impl), \
             mock.patch.object(qa_guard.subprocess, "run", side_effect=show):
            qa_guard.verify_contract(self.contract_path, self.release, self.framework, "chatty")

    def test_exact_artifact_and_clean_provenance_pass(self):
        self.verify(self.fake_git())

    def test_pve_short_cluster_id_and_fqdn_pair_are_mandatory(self):
        del self.contract["pve"]["sshHost"]
        with self.assertRaisesRegex(qa_guard.GuardError, "sshHost"):
            self.verify(self.fake_git())

    def test_pve_swapped_short_and_fqdn_identities_are_rejected(self):
        self.contract["pve"] = {"node": "pve01.lain.local", "sshHost": "pve01"}
        with self.assertRaisesRegex(qa_guard.GuardError, "cluster node ID"):
            self.verify(self.fake_git())

    def test_pve_short_or_unrelated_ssh_host_is_rejected(self):
        for ssh_host in ("pve01", "other.lain.local"):
            with self.subTest(ssh_host=ssh_host):
                self.contract["pve"]["sshHost"] = ssh_host
                with self.assertRaisesRegex(qa_guard.GuardError, "FQDN|identify"):
                    self.verify(self.fake_git())

    def test_pve_tfvars_identity_mismatch_is_rejected(self):
        cases = (
            'pve_node_name = "pve02"\npve_ssh_host = "pve01.lain.local"\npve_endpoint = "https://pve01.lain.local:8006/"\n',
            'pve_node_name = "pve01"\npve_ssh_host = "pve02.lain.local"\npve_endpoint = "https://pve01.lain.local:8006/"\n',
            'pve_node_name = "pve01"\npve_ssh_host = "pve01.lain.local"\npve_endpoint = "https://pve01:8006/"\n',
        )
        for value in cases:
            with self.subTest(value=value):
                self.tfvars.write_text(value, encoding="utf-8")
                with self.assertRaisesRegex(qa_guard.GuardError, "OpenTofu"):
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
        self.azure_source = self.runtime / "secrets/azure-auth-source"
        self.contract_path = self.runtime / "pinned/contract.json"
        write_json(self.runtime / "pinned/run.env.json", {
            "releaseRepo": str(self.release), "pveSshPrivateKey": str(self.ssh_key),
            "azureAuthSource": str(self.azure_source),
        })
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
        write_json(run_env, {"releaseRepo": str(outside)})
        with self.assertRaisesRegex(qa_guard.GuardError, "escapes canonical run root|must be exactly"):
            self.verify(self.fake_git())

    def test_pve_ssh_private_key_is_canonical_private_regular_and_owned(self):
        secrets = self.runtime / "secrets"
        key = self.ssh_key
        run_env = self.runtime / "pinned/run.env.json"

        def set_key(value):
            write_json(run_env, {"releaseRepo": str(self.release), "pveSshPrivateKey": str(value),
                                 "azureAuthSource": str(self.azure_source)})

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

    def test_azure_auth_source_is_canonical_private_and_symlink_free(self):
        run_env = self.runtime / "pinned/run.env.json"

        def set_source(value):
            write_json(run_env, {"releaseRepo": str(self.release),
                                 "pveSshPrivateKey": str(self.ssh_key),
                                 "azureAuthSource": str(value)})

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

    def test_paid_envelope_overrun_and_one_cent_over_ceiling_are_rejected(self):
        self.contract["lifecycle"]["ttl"] = "46m"
        with self.assertRaisesRegex(qa_guard.GuardError, "lifecycle"):
            self.verify(self.fake_git())
        self.contract["lifecycle"]["ttl"] = "45m"
        self.contract["limits"]["maxEstimatedCostUsd"] = 0.99
        with self.assertRaisesRegex(qa_guard.GuardError, "estimated cost"):
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
            "aws_instance": ["t3.medium"] * 2 + ["t3.large"] * 2 + ["t3.micro"] * 2,
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

    def test_count_overage_is_rejected(self):
        self.assert_rejected(self.plan({"aws_instance": 7, "azurerm_linux_virtual_machine": 4, "oci_core_instance": 4}))

    def test_unknown_managed_resource_type_is_rejected(self):
        self.assert_rejected(self.plan(
            {"aws_instance": 6, "azurerm_linux_virtual_machine": 4, "oci_core_instance": 4},
            extra_resources=[{"type": "aws_nat_gateway"}],
        ))

    def test_replace_or_delete_action_is_rejected(self):
        self.assert_rejected(self.plan(
            {"aws_instance": 6, "azurerm_linux_virtual_machine": 4, "oci_core_instance": 4},
            actions=[{"address": "aws_instance.old", "change": {"actions": ["delete", "create"]}}],
        ))

    def test_cost_ceiling_is_enforced(self):
        self.assert_rejected(
            self.plan({"aws_instance": 6, "azurerm_linux_virtual_machine": 4, "oci_core_instance": 4}),
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
