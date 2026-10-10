"""Final aggregation must not turn missing observation into a product defect."""
import importlib.util
import json
from pathlib import Path
import unittest


REPO = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location("release_certification", REPO / "scripts/release_certification.py")
certification = importlib.util.module_from_spec(spec)
spec.loader.exec_module(certification)


class QualificationOutcomeTests(unittest.TestCase):
    def outcome(self, result, exit_code=1, cleanup=0, inventory=0, aborted=None):
        return certification.qualification_outcome(
            result, driver_exit=exit_code, cleanup_exit=cleanup,
            inventory_exit=inventory, aborted=aborted)

    def test_observation_inconclusive_survives_final_aggregation(self):
        result = {"status": "fail", "classification": "observation_inconclusive",
                  "checks": [{"name": "self stimulus", "result": "fail",
                              "classification": "observation_inconclusive"}]}
        self.assertEqual(self.outcome(result), ("fail", "observation_inconclusive"))
        schema = json.loads(certification.QUALIFICATION_SCHEMA.read_text())
        self.assertIn("observation_inconclusive", schema["properties"]["classification"]["enum"])

    def test_missing_or_invalid_driver_report_is_not_product_evidence(self):
        for result in ({}, {"status": "fail"}, {"status": "fail", "classification": "none"},
                       {"status": "fail", "classification": "inconclusive_or_failure"}):
            with self.subTest(result=result):
                self.assertEqual(self.outcome(result), ("fail", "infra_failure"))

    def test_positive_product_violation_cannot_be_hidden_by_later_pass(self):
        result = {"status": "pass", "classification": "none", "checks": [
            {"name": "self ARP", "result": "fail", "classification": "product_failure"},
            {"name": "later self ARP", "result": "pass", "classification": "none"}]}
        self.assertEqual(self.outcome(result, exit_code=0), ("fail", "product_failure"))

    def test_healthy_adjacent_and_cleanup_failure_remain_distinct(self):
        result = {"status": "pass", "classification": "none", "checks": []}
        self.assertEqual(self.outcome(result, exit_code=0), ("pass", "none"))
        self.assertEqual(self.outcome(result, exit_code=0, cleanup=1), ("fail", "infra_failure"))
        self.assertEqual(self.outcome(result, exit_code=0, inventory=1), ("fail", "infra_failure"))
        self.assertEqual(self.outcome(result, exit_code=0, aborted={"reason": "ttl"}), ("fail", "infra_failure"))


if __name__ == "__main__":
    unittest.main()
