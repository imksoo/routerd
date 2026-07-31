from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[1]


class CertificationPlanPathTests(unittest.TestCase):
    def assert_runtime_plan_pipeline(self, driver_name, plan_name):
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
        self.assertIn('apply -input=false -auto-approve "$plan"', script)
        literals = re.findall(r'[A-Za-z0-9_-]+\.tfplan', script)
        self.assertEqual(literals, [f"{plan_name}.tfplan"])

    def test_cloud_saved_plan_is_runtime_only_and_single_source(self):
        self.assert_runtime_plan_pipeline("cloud-certification-driver.sh", "cloud")

    def test_pve_saved_plan_is_runtime_only_and_single_source(self):
        self.assert_runtime_plan_pipeline("pve-certification-driver.sh", "pve")


if __name__ == "__main__":
    unittest.main()
