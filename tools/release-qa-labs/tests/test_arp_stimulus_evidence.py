"""Synthetic replay of delayed collection and adjacent valid ARP observations."""
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


HELPER = Path(__file__).resolve().parents[1] / "arp_stimulus_evidence.py"
spec = importlib.util.spec_from_file_location("arp_stimulus_evidence", HELPER)
evidence = importlib.util.module_from_spec(spec)
spec.loader.exec_module(evidence)


class StimulusEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.binding = {"target": "192.0.2.10", "positiveTarget": "192.0.2.20",
                        "clockBounds": {"client": 1.096847, "origin": 1.101406}}
        self.client = {"samples": [{"bootId": "fixture-boot"}]}
        self.packet = {"at": 1001.032774, "src": "02:00:00:00:00:01",
                       "dst": "ff:ff:ff:ff:ff:ff", "sender": "192.0.2.1",
                       "target": "192.0.2.10"}
        argv = ["arping", "-I", "fixture0", "-s", "192.0.2.1",
                "-b", "-c", "1", "-w", "2", "192.0.2.10"]
        self.stimulus = {"case": "self", "senderRole": "client", "stimulusId": "fixture-id",
                         "argv": argv, "result": {
                             "stimulusId": "fixture-id", "bootId": "fixture-boot", "argv": argv,
                             "startedEpoch": 1001.02, "endedEpoch": 1001.04,
                             "sendMonotonic": 10.0, "endMonotonic": 10.02,
                             "returncode": 0, "success": True}}

    def proof(self, stimuli):
        return evidence.causal_packets(
            stimuli, "self", self.binding, self.client, {}, [self.packet],
            [dict(self.packet, at=1001.032593)], [{"time": 1001.032672}], float)

    def test_original_timing_failure_and_healthy_legacy_neighbor(self):
        legacy = {"case": "self", "result": {"returncode": 0},
                  "transport": {"started_at": 1000.0, "completed_at": 1004.0}}
        self.assertAlmostEqual(self.packet["at"] - self.binding["clockBounds"]["client"]
                               - legacy["transport"]["started_at"], -0.064073)
        self.assertFalse(self.proof([legacy])["success"])
        legacy["transport"]["started_at"] = 997.0
        self.assertTrue(self.proof([legacy])["success"])

    def test_actual_sender_window_survives_coordinator_delay(self):
        self.stimulus["transport"] = {"started_at": 1000.0, "completed_at": 1004.0}
        self.assertTrue(self.proof([self.stimulus])["success"])
        self.stimulus["result"]["startedEpoch"] = 1001.035
        self.assertFalse(self.proof([self.stimulus])["success"])

    def test_missing_or_mismatched_new_records_cannot_use_legacy_fallback(self):
        for key, value in (("stimulusId", "wrong-id"), ("bootId", "wrong-boot"),
                           ("returncode", 1), ("startedEpoch", float("nan"))):
            with self.subTest(key=key):
                item = copy.deepcopy(self.stimulus)
                item["transport"] = {"started_at": 997.0, "completed_at": 1004.0}
                item["result"][key] = value
                self.assertFalse(self.proof([item])["success"])
        del self.stimulus["result"]["startedEpoch"]
        self.assertFalse(self.proof([self.stimulus])["success"])

    def test_overlapping_commands_and_reverse_ping_do_not_prove_causality(self):
        self.assertFalse(self.proof([self.stimulus, copy.deepcopy(self.stimulus)])["success"])
        self.stimulus["senderRole"] = "receiver"
        self.assertFalse(self.proof([self.stimulus])["success"])

    def test_observation_failures_are_distinct_from_proven_violations(self):
        self.assertEqual(evidence.classify_assessment({"self_qualified_generation": False}, {}),
                         "observation_inconclusive")
        self.assertEqual(evidence.classify_assessment({"no_self_probe": False,
                                                       "self_probe_packet_parse": False}, {}),
                         "observation_inconclusive")
        for violation in ("no_self_probe", "no_self_rejection", "no_confirmed_ping_failure"):
            self.assertEqual(evidence.classify_assessment({violation: False}, {}), "product_failure")
        self.assertEqual(evidence.classify_assessment({"collector_success": False},
                                                     {"receiver": {"errors": ["fixture"]}}),
                         "infra_failure")
        self.assertEqual(evidence.merge_classification("product_failure", "pass", "infra_failure"),
                         "product_failure")

    def test_driver_preserves_structured_outcome_and_rejects_missing_evidence(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = Path(tmp) / "result.json"
            for classification in ("pass", "observation_inconclusive", "product_failure", "infra_failure"):
                success = classification == "pass"
                result.write_text(json.dumps({"success": success, "classification": classification}))
                self.assertEqual(evidence.command_outcome({"evidenceResult": str(result)}, 0 if success else 1),
                                 (success, classification))
            self.assertEqual(evidence.command_outcome({"evidenceResult": str(result)}, 0),
                             (False, "infra_failure"))
        self.assertEqual(evidence.command_outcome({"argv": ["python3", "selfprobe-test.py"]}, 0),
                         (False, "infra_failure"))


if __name__ == "__main__":
    unittest.main()
