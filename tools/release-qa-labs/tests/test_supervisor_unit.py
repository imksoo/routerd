from pathlib import Path
import json
import re
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
        orphan_cleanup = ROOT / "drivers" / "pve-orphan-cleanup.sh"
        self.assertTrue(orphan_cleanup.is_file())
        self.assertTrue(orphan_cleanup.stat().st_mode & 0o111)

        prepare = (ROOT / "supervisor" / "routerd-release-qa-prepare@.service").read_text(encoding="utf-8")
        unit_section, service_section = prepare.split("\n[Service]\n", 1)
        self.assertIn("Group=routerd-release-qa", prepare)
        self.assertNotIn("User=routerd-release-qa", prepare)
        self.assertIn("RemainAfterExit=yes", prepare)
        self.assertIn("StopWhenUnneeded=yes", unit_section)
        self.assertNotIn("StopWhenUnneeded=yes", service_section)
        self.assertNotIn("PartOf=routerd-release-qa@%i.service", prepare)
        self.assertIn("StateDirectory=routerd-release-qa-sealed/%i", prepare)

    def test_launcher_does_not_replace_contract_lifecycle_with_policy_maxima(self):
        launcher = (ROOT / "drivers" / "start-supervised-release-qa.sh").read_text(encoding="utf-8")
        for hardcoded in (
            "--ttl-seconds 2700", "--stale-seconds 300", "--cleanup-timeout-seconds 600",
            "--inventory-timeout-seconds 300", "--max-cleanup-attempts 2",
            "--max-paid-lifecycle-seconds 5100",
        ):
            self.assertNotIn(hardcoded, launcher)
        self.assertIn("pinned", launcher.lower())
        self.assertIn("azure-auth-source.sha256", launcher)
        self.assertIn('export AZURE_CONFIG_DIR="$azure_state"', launcher)

    def test_release_precheck_requires_the_bounded_profile_timeout_tool(self):
        precheck = (ROOT / "drivers" / "precheck-driver.sh").read_text(encoding="utf-8")
        self.assertIn("require_command timeout", precheck)

    def test_contract_example_matches_repo_native_deployment_layout(self):
        repo = ROOT.parents[1]
        example = json.loads((ROOT / "contract.example.json").read_text(encoding="utf-8"))
        self.assertEqual(example["stateMode"], "fresh-fabric-fresh-state")
        self.assertEqual(example["safety"]["pveManagementControlPlane"], "none")
        self.assertEqual(example["safety"]["pveTLS"], "pinned-ca")
        for relative in example["qaImplementation"]["scriptBlobs"]:
            self.assertTrue((repo / relative).is_file(), relative)
            self.assertFalse((ROOT / relative).exists(), "flattened QA copy must not masquerade as repo root")
        self.assertIn("/<run>/repo/tools/release-qa-labs/", example["tofu"]["workingDirectory"])
        self.assertIn("/<run>/repo/tools/release-qa-labs/", example["tofu"]["lockPath"])
        for key in ("statePath", "variablesPath", "outputPath"):
            self.assertIn("/<run>/runtime/", example["tofu"][key])

    def test_pve_host_and_guest_ssh_consumers_use_distinct_pinned_runtime_keys(self):
        common = (ROOT / "drivers/common.sh").read_text(encoding="utf-8")
        self.assertIn("pveSshPrivateKey", common)
        self.assertIn("ROUTERD_RELEASE_QA_PINNED_PVE_SSH_PRIVATE_KEY", common)
        self.assertIn("guestSshPrivateKey", common)
        self.assertIn("ROUTERD_RELEASE_QA_PINNED_GUEST_SSH_PRIVATE_KEY", common)
        self.assertIn("pveSshKnownHosts", common)
        self.assertIn("ROUTERD_RELEASE_QA_PINNED_PVE_SSH_KNOWN_HOSTS", common)
        pve = (ROOT / "drivers/pve-certification-driver.sh").read_text(encoding="utf-8")
        qualification = (ROOT / "drivers/qualification-driver.sh").read_text(encoding="utf-8")
        self.assertIn('ssh_key="$guest_ssh_private_key"', pve)
        self.assertIn('"$ssh_key"', pve)
        self.assertIn('UserKnownHostsFile="$pve_ssh_known_hosts"', pve)
        self.assertIn("GlobalKnownHostsFile=/dev/null", pve)
        self.assertIn('pve_guest_ssh=(ssh -n -i "$ssh_key" -o BatchMode=yes -o StrictHostKeyChecking=yes', pve)
        self.assertIn('UserKnownHostsFile="$pve_guest_known_hosts"', pve)
        self.assertIn('--guest-known-hosts-out "$pve_guest_known_hosts"', pve)
        self.assertIn('--pve-ssh-key "$pve_ssh_private_key"', pve)
        self.assertNotIn('--ssh-key "$pve_ssh_private_key"', pve)
        self.assertNotIn('StrictHostKeyChecking=no', pve)
        for name in (
            "remote-egress-preflight.sh",
            "pve-substrate-preflight.sh",
            "inventory-driver.sh",
        ):
            source = (ROOT / "drivers" / name).read_text(encoding="utf-8")
            self.assertIn('UserKnownHostsFile="$pve_ssh_known_hosts"', source, name)
            self.assertIn("GlobalKnownHostsFile=/dev/null", source, name)
            self.assertNotIn("StrictHostKeyChecking=no", source, name)
        self.assertIn('--ssh-key "$guest_ssh_private_key"', qualification)
        self.assertIn('--pve-ssh-key "$pve_ssh_private_key"', qualification)
        self.assertIn("sam-representative-redundancy.sh", qualification)
        self.assertNotIn("sam-full-validation.sh", qualification)
        for script in (pve, qualification):
            self.assertNotIn("/home/imksoo", script)
        for path in ROOT.rglob("*"):
            if path.is_file() and "tests" not in path.parts and "__pycache__" not in path.parts:
                self.assertNotIn("/home/imksoo", path.read_text(encoding="utf-8", errors="ignore"), str(path))

    def test_qa_coordinator_never_starts_routerd_for_generated_config_validation(self):
        repo = ROOT.parents[1]
        harness = (repo / "tests/e2e/cloudedge/scripts/sam-e2e.sh").read_text(encoding="utf-8")
        self.assertIn("routerd_on_qa_host=false", harness)
        self.assertIn('select(.value.site == "pve" and .value.role == "client")', harness)
        self.assertIn('ssh_node "$validation_node" "$validation_script"', harness)
        self.assertIn("scp_from_node", harness)
        host_side, remote_side = harness.split("cat <<'REMOTE_VALIDATE'", 1)
        self.assertNotIn("routerd_bin", host_side)
        self.assertNotIn("routerctl_bin", host_side)
        self.assertNotIn("serve --sandbox", host_side)
        self.assertIn("serve --sandbox", remote_side)

    def test_supervisor_pins_each_pve_credential_input(self):
        source = (ROOT / "lifecycle_supervisor.py").read_text(encoding="utf-8")
        self.assertIn('(\"pveSshPrivateKey\", \"pve_ssh_private_key\")', source)
        self.assertIn('"ROUTERD_RELEASE_QA_PINNED_PVE_SSH_PRIVATE_KEY"', source)
        self.assertIn('(\"guestSshPrivateKey\", \"guest_ssh_private_key\")', source)
        self.assertIn('"ROUTERD_RELEASE_QA_PINNED_GUEST_SSH_PRIVATE_KEY"', source)
        self.assertIn('(\"pveSshKnownHosts\", \"pve_ssh_known_hosts\")', source)
        self.assertIn('"ROUTERD_RELEASE_QA_PINNED_PVE_SSH_KNOWN_HOSTS"', source)
        self.assertIn('(\"pveTokenTfvars\", \"pve_token_tfvars\")', source)
        self.assertIn('"ROUTERD_RELEASE_QA_PINNED_PVE_TOKEN_TFVARS"', source)
        self.assertIn('(\"pveCaPem\", \"pve_ca_pem\")', source)
        self.assertIn('"ROUTERD_RELEASE_QA_PINNED_PVE_CA_PEM"', source)

    def test_pve_token_consumers_use_only_the_pinned_runtime_input(self):
        common = (ROOT / "drivers/common.sh").read_text(encoding="utf-8")
        self.assertIn("ROUTERD_RELEASE_QA_PINNED_PVE_TOKEN_TFVARS", common)
        self.assertIn('expected_pve_token_tfvars="$runtime_root/pinned/pve-token.tfvars"', common)
        self.assertNotIn(".pveTokenTfvars", common)

    def test_root_owned_release_checkout_is_trusted_only_per_git_read(self):
        common = (ROOT / "drivers/common.sh").read_text(encoding="utf-8")
        mutation = (ROOT / "drivers/mutation-driver.sh").read_text(encoding="utf-8")
        guard = (ROOT / "qa_guard.py").read_text(encoding="utf-8")
        generator = (ROOT.parents[1] / "tests/e2e/cloudedge/configs/sam-e2e-generate.sh").read_text(encoding="utf-8")
        self.assertIn('repo_root="$(cd "$framework_root/../.." && pwd -P)"', common)
        self.assertIn('git -c safe.directory="$repo_root" -C "$repo_root"', common)
        self.assertIn('git_at_checkout_root rev-parse HEAD', mutation)
        self.assertIn('f"safe.directory={trusted_repo}"', guard)
        self.assertIn('checkout_root = run_root / "repo"', guard)
        self.assertNotIn('git(framework, "rev-parse", "--show-toplevel")', guard)
        self.assertIn('repo_root="$(cd "$harness_root/../../.." && pwd -P)"', generator)
        self.assertIn('git -c "safe.directory=$repo_root" -C "$harness_root"', generator)
        self.assertNotIn('git -C "$harness_root"', generator)
        for source in (common, mutation, guard, generator):
            self.assertNotIn("git config --global", source)
            self.assertNotIn("safe.directory=*", source)

    def test_pve_tls_uses_only_the_pinned_ca_and_never_insecure_mode(self):
        common = (ROOT / "drivers/common.sh").read_text(encoding="utf-8")
        preflight = (ROOT / "drivers/pve-substrate-preflight.sh").read_text(encoding="utf-8")
        variables = (ROOT / "terraform/envs/default/variables.tf").read_text(encoding="utf-8")
        self.assertIn("ROUTERD_RELEASE_QA_PINNED_PVE_CA_PEM", common)
        self.assertIn('expected_pve_ca_pem="$runtime_root/pinned/pve-ca.pem"', common)
        self.assertIn('SSL_CERT_FILE="$pve_ca_pem"', common)
        self.assertIn("PROXMOX_VE_INSECURE=false", common)
        self.assertIn("curl_args=(-q", preflight)
        self.assertIn('--cacert "$pve_ca_pem"', preflight)
        self.assertNotIn("--insecure", preflight)
        self.assertIn("default = false", variables)
        self.assertIn("pve_insecure must be false", variables)

    def test_pve_network_and_cluster_id_consumers_are_separated(self):
        example = json.loads((ROOT / "contract.example.json").read_text(encoding="utf-8"))
        self.assertEqual(example["pve"]["node"], "<certified-pve-leaf-node>")
        self.assertEqual(example["pve"]["sshHost"], "<certified-pve-leaf-ssh-host>")
        self.assertNotEqual(example["pve"]["underlayBridge"], example["pve"]["captureBridge"])
        self.assertEqual(set(example["pve"]["vmids"].values()), {0})
        for rr in example["pve"]["rrNodes"].values():
            self.assertTrue(rr["node"].startswith("<certified-"))
            self.assertEqual(rr["vmid"], 0)
            self.assertNotEqual(rr["underlayBridge"], example["pve"]["captureBridge"])
        drivers = {path.name: path.read_text(encoding="utf-8")
                   for path in (ROOT / "drivers").glob("*.sh")}
        self.assertIn(".pve.node", drivers["common.sh"])
        for name, source in drivers.items():
            if name != "common.sh":
                self.assertNotIn(".pve.node", source, name)
        for name in ("inventory-driver.sh", "pve-certification-driver.sh"):
            self.assertIn("$pve_ssh_host", drivers[name], name)
        # The egress preflight must reach the leaf PVE host and both RR hosts,
        # so it intentionally expands the contract into a distinct host list
        # rather than using the leaf-only shell variable.
        preflight = drivers["remote-egress-preflight.sh"]
        self.assertIn("pve_hosts", preflight)
        self.assertIn('.pve.rrNodes["pve-rr-a"].sshHost', preflight)
        self.assertIn('.pve.rrNodes["pve-rr-b"].sshHost', preflight)
        inventory = drivers["inventory-driver.sh"]
        self.assertIn("/nodes/$(printf '%q' \"$pve_node\")/network", inventory)
        outputs = (ROOT / "terraform/envs/default/outputs.tf").read_text(encoding="utf-8")
        self.assertRegex(outputs, re.compile(r"^\s*leaf_host\s+= var\.pve_node_name$", re.MULTILINE))
        self.assertRegex(outputs, re.compile(r"^\s*leaf_ssh_host\s+= var\.pve_ssh_host$", re.MULTILINE))

    def test_public_pve_examples_are_parseable_but_non_executable(self):
        tfvars = (ROOT / "terraform/envs/default/terraform.tfvars.example").read_text(encoding="utf-8")
        for unsafe_value in ("svnet1", "pve02", "pve03", "= 171", "= 172", "= 181", "= 182"):
            self.assertNotIn(unsafe_value, tfvars)
        for placeholder in (
            "<certified-pve-leaf-node>",
            "<certified-pve-underlay-bridge>",
            "<certified-pve-rr-a-node>",
            "<certified-pve-rr-b-node>",
            "<certified-pve-leaf-a-vmid>",
            "<certified-pve-rr-a-vmid>",
        ):
            self.assertIn(placeholder, tfvars)

    def test_qualification_driver_requires_the_current_rr_evidence_contract(self):
        qualification = (ROOT / "drivers" / "qualification-driver.sh").read_text(encoding="utf-8")
        self.assertIn('safety.pveManagementControlPlane', qualification)
        self.assertIn('[ "$safety_pve_management_control_plane" = "none" ]', qualification)
        for gate in (
            "rrAStaged",
            "rrAJoined",
            "rrBStaged",
            "rrBJoined",
            "rrPairReady",
            "rrBControlPlaneContinuity",
            "rrBContinuityCanary",
        ):
            self.assertIn(f"{gate}:true", qualification)

    def test_oci_search_uses_explicit_complete_pagination(self):
        inventory = (ROOT / "drivers/inventory-driver.sh").read_text(encoding="utf-8")
        search = inventory[inventory.index("search resource structured-search"):]
        self.assertNotIn("--all", search)
        self.assertIn('oci_args+=(--page "$page_token")', search)
        self.assertIn("repeated a pagination token", search)
        self.assertIn('pagination:{status:"complete", pages:length}', search)


if __name__ == "__main__":
    unittest.main()
