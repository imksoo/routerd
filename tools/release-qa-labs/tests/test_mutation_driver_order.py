from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]


class MutationDriverOrderTests(unittest.TestCase):
    def test_pve_certification_must_finish_before_cloud_provisioning(self):
        source = (ROOT / "drivers/mutation-driver.sh").read_text(encoding="utf-8")
        pve_stage = source.index("run_provision_stage pve-certification")
        cloud_stage = source.index("run_provision_stage cloud-certification")
        self.assertLess(pve_stage, cloud_stage)

        pve_block = source[pve_stage:cloud_stage]
        cloud_block = source[cloud_stage:source.index("touch_heartbeat", cloud_stage)]
        self.assertIn("certify-pve-substrate.sh", pve_block)
        self.assertNotIn("--cloud-certification", pve_block)
        self.assertIn("certify-cloud-substrate.sh", cloud_block)
        self.assertIn('--pve-certification "$pve_certification"', cloud_block)
        self.assertIn('--out "$full_certification"', cloud_block)

    def test_pve_only_scope_exits_before_cloud_or_product_qualification(self):
        source = (ROOT / "drivers/mutation-driver.sh").read_text(encoding="utf-8")
        pve_stage = source.index("run_provision_stage pve-certification")
        pve_only_gate = source.index('qualification_scope" = "pve-certification-only"')
        cloud_stage = source.index("run_provision_stage cloud-certification")
        product_stage = source.index('"$script_dir/qualification-driver.sh"')
        pve_only_block = source[pve_only_gate:cloud_stage]

        self.assertLess(pve_stage, pve_only_gate)
        self.assertLess(pve_only_gate, cloud_stage)
        self.assertLess(cloud_stage, product_stage)
        self.assertIn('cloudProvisioning:false', pve_only_block)
        self.assertIn('productQualification:false', pve_only_block)
        self.assertIn('releaseQualification:"not-run"', pve_only_block)
        self.assertIn("exit 0", pve_only_block)


if __name__ == "__main__":
    unittest.main()
