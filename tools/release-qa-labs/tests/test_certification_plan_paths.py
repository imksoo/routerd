from pathlib import Path
import re
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[1]


class CertificationPlanPathTests(unittest.TestCase):
    def assert_runtime_plan_pipeline(
        self, driver_name, plan_name, backup_variable, *, extra_plan_names=()
    ):
        script = (ROOT / "drivers" / driver_name).read_text(encoding="utf-8")
        common = (ROOT / "drivers/common.sh").read_text(encoding="utf-8")
        self.assertIn('plan_root="$runtime_root/plans"', common)
        self.assertIn('mkdir -p "$lifecycle_dir" "$command_log_dir" "$plan_root"', common)
        self.assertIn('chmod 700 "$plan_root"', common)
        expected = f'plan="$plan_root/{plan_name}.tfplan"'
        self.assertIn(expected, script)
        self.assertNotRegex(script, rf'plan="\$tf_dir/{plan_name}\.tfplan"')

        # The saved plan is created, rendered for the guard, and applied via
        # the one immutable shell variable. A second literal plan path would
        # permit validating a different file than the one applied.
        self.assertIn('-out="$plan"', script)
        self.assertIn('show -json "$plan"', script)
        self.assertIn(f'-backup="{backup_variable}" "$plan"', script)
        literals = re.findall(r'[A-Za-z0-9_-]+\.tfplan', script)
        self.assertEqual(literals, [*extra_plan_names, f"{plan_name}.tfplan"])

    def test_cloud_saved_plan_is_runtime_only_and_single_source(self):
        self.assert_runtime_plan_pipeline(
            "cloud-certification-driver.sh", "cloud", "$cloud_state_backup"
        )

    def test_pve_saved_plan_is_runtime_only_and_single_source(self):
        self.assert_runtime_plan_pipeline(
            "pve-certification-driver.sh", "pve", "$pve_state_backup",
            extra_plan_names=("pve-template-stage.tfplan",)
        )
        script = (ROOT / "drivers/pve-certification-driver.sh").read_text(encoding="utf-8")
        # Cross-host PVE clones deliberately have a second, stage-only saved
        # plan. It is created, inspected, and applied before the six-clone
        # plan; both files must remain runtime-confined and single-source.
        self.assertIn('stage_plan="$plan_root/pve-template-stage.tfplan"', script)
        self.assertNotIn('stage_plan="$tf_dir/pve-template-stage.tfplan"', script)
        self.assertIn('-out="$stage_plan"', script)
        self.assertIn('show -json "$stage_plan"', script)
        self.assertIn('stage_state_backup="$pve_dir/tofu-template-stage-pre-apply.tfstate"', script)
        self.assertIn('-backup="$stage_state_backup" "$stage_plan"', script)
        self.assertIn('pve_state_backup="$pve_dir/tofu-pve-pre-apply.tfstate"', script)
        self.assertIn('-backup="$pve_state_backup" "$plan"', script)
        literals = re.findall(r'[A-Za-z0-9_-]+\.tfplan', script)
        self.assertEqual(literals, ["pve-template-stage.tfplan", "pve.tfplan"])

    def test_cloud_provider_graph_check_uses_standard_grep(self):
        script = (ROOT / "drivers/cloud-certification-driver.sh").read_text(
            encoding="utf-8"
        )
        needle = "provider[registry.opentofu.org/hashicorp/oci]"
        self.assertIn("for command in tofu jq grep aws az oci ssh; do", script)
        self.assertIn(f"grep -Fq '{needle}'", script)
        self.assertNotIn("for command in tofu jq rg ", script)
        self.assertNotRegex(script, r"(?m)^if rg(?: |$)")

        for graph, expected in (
            (f"└── {needle}\n", 0),
            ("└── provider[registry.opentofu.org/oracle/oci]\n", 1),
        ):
            result = subprocess.run(
                ["grep", "-Fq", needle],
                input=graph,
                text=True,
                check=False,
            )
            self.assertEqual(result.returncode, expected)

    def test_pve_phase_never_refreshes_or_probes_uncreated_cloud_nodes(self):
        pve = (ROOT / "drivers/pve-certification-driver.sh").read_text(encoding="utf-8")
        cloud = (ROOT / "drivers/cloud-certification-driver.sh").read_text(encoding="utf-8")
        outputs = (ROOT / "terraform/envs/default/outputs.tf").read_text(encoding="utf-8")

        self.assertNotIn("apply -refresh-only", pve)
        self.assertIn('output -json pve_nodes', pve)
        self.assertIn('output -json pve_fabric', pve)
        self.assertIn('pve_output_path="$pve_dir/tofu-output-pve-qga.json"', pve)
        self.assertIn('select(.value.site == "pve")', pve)
        self.assertNotIn("StrictHostKeyChecking=accept-new", pve)
        self.assertIn('output "pve_nodes"', outputs)
        self.assertIn('output "pve_fabric"', outputs)
        self.assertIn("pve = local.pve_fabric", outputs)

        # The cloud phase is the only place that reads a complete output. It
        # must retain the PVE QGA projection instead of asking cloud state to
        # rediscover the PVE DHCP address or SSH host key.
        self.assertIn('tofu -chdir="$tf_dir" output -json >"$raw_output"', cloud)
        self.assertIn('pve_qga_output="$evidence_root/certification/pve/tofu-output-pve-qga.json"', cloud)
        self.assertIn('install -m 0600 "$merged_output" "$tofu_output_path"', cloud)
        self.assertIn('full topology output handoff', cloud)
        # QGA owns only management/bootstrap facts.  The complete output
        # remains the source of overlay and client topology fields.
        self.assertIn('.nodes.value[$entry.key] * {', cloud)
        self.assertIn('.value.overlay_ip | type == "string"', cloud)
        self.assertIn('.value.client_ip | type == "string"', cloud)

    def test_pve_phase_initializes_its_local_backend_before_pve_mutation(self):
        pve = (ROOT / "drivers/pve-certification-driver.sh").read_text(encoding="utf-8")

        init = pve.index("run_with_progress tofu-pve-init")
        bridge = pve.index("run_with_progress pve-capture-bridge-ensure")
        stage_plan = pve.index("run_with_progress tofu-pve-template-stage-plan")
        self.assertLess(init, bridge)
        self.assertLess(bridge, stage_plan)
        self.assertIn('tofu -chdir="$tf_dir" init -input=false -lockfile=readonly', pve)
        self.assertIn('-backend-config="path=$tofu_state_path"', pve)

    def test_mutating_tofu_commands_keep_state_backups_in_runtime_evidence(self):
        pve = (ROOT / "drivers/pve-certification-driver.sh").read_text(encoding="utf-8")
        cloud = (ROOT / "drivers/cloud-certification-driver.sh").read_text(encoding="utf-8")
        cleanup = (ROOT / "drivers/cleanup-driver.sh").read_text(encoding="utf-8")

        self.assertIn('stage_state_backup="$pve_dir/tofu-template-stage-pre-apply.tfstate"', pve)
        self.assertIn('pve_state_backup="$pve_dir/tofu-pve-pre-apply.tfstate"', pve)
        self.assertIn('cloud_state_backup="$preflight_dir/tofu-cloud-pre-apply.tfstate"', cloud)
        self.assertIn('destroy_state_backup="$cleanup_evidence/tofu-pre-destroy.tfstate"', cleanup)
        self.assertIn('-backup="$stage_state_backup" "$stage_plan"', pve)
        self.assertIn('-backup="$pve_state_backup" "$plan"', pve)
        self.assertIn('-backup="$cloud_state_backup" "$plan"', cloud)
        self.assertIn('-backup="$destroy_state_backup"', cleanup)

    def test_full_pve_rr_guard_uses_open_tofu_supported_predicates(self):
        main = (ROOT / "terraform/envs/default/main.tf").read_text(encoding="utf-8")

        # The qualification host executes OpenTofu, whose HCL function set
        # does not provide setequals().  With the two-name cardinality guard,
        # explicit membership is an exact set check without provider access.
        self.assertNotIn("setequals(", main)
        self.assertIn('contains(keys(local.pve_rr_nodes), "pve-rr-a")', main)
        self.assertIn('contains(keys(local.pve_rr_nodes), "pve-rr-b")', main)


if __name__ == "__main__":
    unittest.main()
