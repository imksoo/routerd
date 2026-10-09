"""Exercise both naming admission boundaries without providers or host changes."""
import importlib.util
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("qa_guard", ROOT / "qa_guard.py")
qa_guard = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(qa_guard)

VALID_IDS = (
    "a", "A0", "r" * 43,
    "relqa-pvecert-selfprobe-r1-20261009T122000Z",
    "relqa-pvecert-cmdev-r2-20261009T200000Z",
)
INVALID_IDS = (
    "", "r" * 44,
    "relqa-pvecert-command-evidence-r1-20261009T195505Z",
    "-run", "run-", "run_name", "run.name", "run/name", "run name",
    "run\n", "rún", "r' OR '1'='1",
)


class RunIDTests(unittest.TestCase):
    def test_admitted_ids_fit_generated_hostname_and_iam_role(self):
        for run_id in VALID_IDS:
            with self.subTest(run_id=run_id):
                qa_guard.verify_run_id(run_id)
                for node in ("pve-client-a", "pve-client-b", "pve-leaf-a", "pve-rr-b"):
                    self.assertLessEqual(len(f"routerd-{run_id}-{node}".encode()), 64)
                self.assertLessEqual(len(f"routerd-sam-e2e-{run_id}"), 64)
        self.assertEqual(len(f"routerd-{'r' * 43}-pve-client-a"), 64)

    def test_overlong_or_unsafe_ids_are_rejected(self):
        for run_id in INVALID_IDS:
            with self.subTest(run_id=run_id):
                with self.assertRaisesRegex(qa_guard.GuardError, "invalid runId"):
                    qa_guard.verify_run_id(run_id)

    @unittest.skipUnless(shutil.which("tofu"), "OpenTofu is required")
    def test_real_terraform_variable_has_identical_admission(self):
        # Execute the actual variable declaration in a provider-free module.
        # No copied regex, provider initialization, credentials or network.
        variables = (ROOT / "terraform/envs/default/variables.tf").read_text()
        declaration = variables.split('variable "run_id"', 1)[1].split('variable "purpose"', 1)[0]
        with tempfile.TemporaryDirectory() as temp:
            (Path(temp) / "main.tf").write_text(
                'variable "run_id"' + declaration + '\noutput "run_id" { value = var.run_id }\n'
            )
            for run_id in VALID_IDS + INVALID_IDS:
                with self.subTest(run_id=run_id):
                    result = subprocess.run(
                        ["tofu", f"-chdir={temp}", "plan", "-input=false", "-lock=false", "-no-color", f"-var=run_id={run_id}"],
                        text=True, capture_output=True, timeout=20,
                    )
                    if run_id in VALID_IDS:
                        self.assertEqual(result.returncode, 0, result.stderr)
                    else:
                        self.assertNotEqual(result.returncode, 0)
                        self.assertIn("run_id must be 1-43 ASCII", result.stderr)


if __name__ == "__main__":
    unittest.main()
