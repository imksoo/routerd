from pathlib import Path
import json
import unittest


ROOT = Path(__file__).resolve().parents[1]


class SupervisorUnitTests(unittest.TestCase):
    def test_unit_is_boot_enabled_restartable_and_executes_tracked_supervisor(self):
        self.assertTrue((ROOT / "lifecycle_supervisor.py").stat().st_mode & 0o111)
        unit = (ROOT / "supervisor" / "routerd-release-qa@.service").read_text(encoding="utf-8")
        self.assertIn("Restart=on-failure", unit)
        self.assertIn("WantedBy=multi-user.target", unit)
        self.assertIn("UMask=0077", unit)
        self.assertIn("ReadWritePaths=/var/lib/routerd-release-qa/%i/runtime", unit)
        self.assertIn("runtime/secrets", unit)
        self.assertIn("/var/lib/routerd-release-qa-sealed/%i", unit)
        self.assertIn("Requires=routerd-release-qa-prepare@%i.service", unit)
        self.assertNotIn("ReadWritePaths=/var/lib/routerd-release-qa/.azure", unit)
        self.assertNotIn("ReadWritePaths=/var/lib/routerd-release-qa\n", unit)
        exec_start = next(line for line in unit.splitlines() if line.startswith("ExecStart="))
        self.assertIn("drivers/start-supervised-release-qa.sh", exec_start)
        self.assertNotIn("/tmp/", exec_start)
        launcher = ROOT / "drivers" / "start-supervised-release-qa.sh"
        self.assertTrue(launcher.is_file())
        self.assertTrue(launcher.stat().st_mode & 0o111)
        self.assertIn("lifecycle_supervisor.py", launcher.read_text(encoding="utf-8"))

        prepare = (ROOT / "supervisor" / "routerd-release-qa-prepare@.service").read_text(encoding="utf-8")
        self.assertIn("Group=routerd-release-qa", prepare)
        self.assertNotIn("User=routerd-release-qa", prepare)
        self.assertIn("StateDirectory=routerd-release-qa-sealed/%i", prepare)

    def test_launcher_does_not_replace_contract_lifecycle_with_policy_maxima(self):
        launcher = (ROOT / "drivers" / "start-supervised-release-qa.sh").read_text(encoding="utf-8")
        for hardcoded in (
            "--ttl-seconds 2700", "--stale-seconds 300", "--cleanup-timeout-seconds 600",
            "--inventory-timeout-seconds 300", "--max-cleanup-attempts 2",
            "--max-paid-lifecycle-seconds 4500",
        ):
            self.assertNotIn(hardcoded, launcher)
        self.assertIn("pinned", launcher.lower())
        self.assertIn("azure-auth-source.sha256", launcher)
        self.assertIn('export AZURE_CONFIG_DIR="$azure_state"', launcher)

    def test_contract_example_matches_repo_native_deployment_layout(self):
        repo = ROOT.parents[1]
        example = json.loads((ROOT / "contract.example.json").read_text(encoding="utf-8"))
        for relative in example["qaImplementation"]["scriptBlobs"]:
            self.assertTrue((repo / relative).is_file(), relative)
            self.assertFalse((ROOT / relative).exists(), "flattened QA copy must not masquerade as repo root")
        self.assertIn("/<run>/repo/tools/release-qa-labs/", example["tofu"]["workingDirectory"])
        self.assertIn("/<run>/repo/tools/release-qa-labs/", example["tofu"]["lockPath"])
        for key in ("statePath", "variablesPath", "outputPath"):
            self.assertIn("/<run>/runtime/", example["tofu"][key])

    def test_all_ssh_consumers_use_one_pinned_runtime_key(self):
        common = (ROOT / "drivers/common.sh").read_text(encoding="utf-8")
        self.assertIn("pveSshPrivateKey", common)
        self.assertIn("ROUTERD_RELEASE_QA_PINNED_PVE_SSH_PRIVATE_KEY", common)
        pve = (ROOT / "drivers/pve-certification-driver.sh").read_text(encoding="utf-8")
        qualification = (ROOT / "drivers/qualification-driver.sh").read_text(encoding="utf-8")
        self.assertIn('ssh_key="$pve_ssh_private_key"', pve)
        self.assertIn('"$ssh_key"', pve)
        self.assertIn('"$pve_ssh_private_key"', qualification)
        for script in (pve, qualification):
            self.assertNotIn("/home/imksoo", script)
        for path in ROOT.rglob("*"):
            if path.is_file() and "tests" not in path.parts and "__pycache__" not in path.parts:
                self.assertNotIn("/home/imksoo", path.read_text(encoding="utf-8", errors="ignore"), str(path))

    def test_supervisor_declares_fourth_digest_pinned_ssh_input(self):
        source = (ROOT / "lifecycle_supervisor.py").read_text(encoding="utf-8")
        self.assertIn('(\"pveSshPrivateKey\", \"pve_ssh_private_key\")', source)
        self.assertIn('"ROUTERD_RELEASE_QA_PINNED_PVE_SSH_PRIVATE_KEY"', source)

    def test_pve_network_and_cluster_id_consumers_are_separated(self):
        example = json.loads((ROOT / "contract.example.json").read_text(encoding="utf-8"))
        self.assertEqual(example["pve"]["node"], "pve01")
        self.assertEqual(example["pve"]["sshHost"], "pve01.lain.local")
        drivers = {path.name: path.read_text(encoding="utf-8")
                   for path in (ROOT / "drivers").glob("*.sh")}
        self.assertIn(".pve.node", drivers["common.sh"])
        for name, source in drivers.items():
            if name != "common.sh":
                self.assertNotIn(".pve.node", source, name)
        for name in ("remote-egress-preflight.sh", "inventory-driver.sh",
                     "pve-certification-driver.sh"):
            self.assertIn("$pve_ssh_host", drivers[name], name)
        inventory = drivers["inventory-driver.sh"]
        self.assertIn("/nodes/$(printf '%q' \"$pve_node\")/network", inventory)
        outputs = (ROOT / "terraform/envs/default/outputs.tf").read_text(encoding="utf-8")
        self.assertIn("node_name       = var.pve_node_name", outputs)
        self.assertIn("node_ssh_host   = var.pve_ssh_host", outputs)

    def test_oci_search_uses_explicit_complete_pagination(self):
        inventory = (ROOT / "drivers/inventory-driver.sh").read_text(encoding="utf-8")
        search = inventory[inventory.index("search resource structured-search"):]
        self.assertNotIn("--all", search)
        self.assertIn('oci_args+=(--page "$page_token")', search)
        self.assertIn("repeated a pagination token", search)
        self.assertIn('pagination:{status:"complete", pages:length}', search)


if __name__ == "__main__":
    unittest.main()
