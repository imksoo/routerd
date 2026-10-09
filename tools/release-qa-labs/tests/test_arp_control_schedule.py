import copy
import importlib.util
from pathlib import Path
import unittest


SPEC = importlib.util.spec_from_file_location(
    "arp_control_schedule", Path(__file__).resolve().parents[1] / "arp_control_schedule.py"
)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def build(**changes):
    options = dict(
        ping_offsets=[60, 120], arp_offsets=[223, 293], observation_seconds=400,
        capture_lead_seconds=20, request_generation_window_seconds=30,
        dispatch_timeout_seconds=10, request_ttl_seconds=45,
        clock_allowance_seconds=10, expiry_tail_seconds=10, quiet_guard_seconds=1.55,
    )
    options.update(changes)
    return MODULE.build_positive_control_schedule(**options)


class PositiveControlScheduleTests(unittest.TestCase):
    def test_independent_controls_have_complete_expiry_tail(self):
        result = build()
        self.assertEqual(result["requiredObservationSeconds"], 388)
        self.assertEqual(result["observationReserveSeconds"], 12)
        self.assertEqual(len(result["opportunities"]), 2)
        self.assertTrue(all(not x["conditional"] for x in result["opportunities"]))
        self.assertTrue(result["arrivalDoesNotCancel"])
        self.assertTrue(result["attributionRequired"])

    def test_receipt_is_not_an_input_for_skipping_an_opportunity(self):
        with self.assertRaises(TypeError):
            build(request_already_received=True)

    def test_missing_single_duplicate_and_reversed_opportunities_fail(self):
        for changes in [dict(ping_offsets=[]), dict(arp_offsets=[]), dict(arp_offsets=[223]),
                        dict(arp_offsets=[223, 223]), dict(arp_offsets=[293, 223]),
                        dict(ping_offsets=[120, 60])]:
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                build(**changes)

    def test_next_generation_must_follow_dispatch_expiry_clock_and_guard(self):
        # 10 s dispatch + 45 s TTL + 10 s clock + 1.55 s guard.
        for offsets in [[223, 283], [223, 288], [223, 289.55]]:
            with self.subTest(offsets=offsets), self.assertRaises(ValueError):
                build(arp_offsets=offsets)

    def test_first_control_cannot_overlap_ping_generated_request_lifetime(self):
        for offsets in [[180, 293], [206.55, 293]]:
            with self.subTest(offsets=offsets), self.assertRaises(ValueError):
                build(arp_offsets=offsets)

    def test_capture_lead_and_complete_final_tail_are_reserved(self):
        for changes in [dict(observation_seconds=388), dict(capture_lead_seconds=32),
                        dict(expiry_tail_seconds=22), dict(arp_offsets=[223, 305]),
                        dict(dispatch_timeout_seconds=22)]:
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                build(**changes)

    def test_larger_clock_bounds_cannot_reuse_the_same_plan(self):
        with self.assertRaises(ValueError):
            build(clock_allowance_seconds=20)

    def test_malformed_or_missing_time_bounds_fail(self):
        for value in [True, -1, float("inf"), float("nan"), "400", None]:
            with self.subTest(value=value), self.assertRaises(ValueError):
                build(observation_seconds=value)
        for field in ["request_ttl_seconds", "dispatch_timeout_seconds", "expiry_tail_seconds",
                      "request_generation_window_seconds", "quiet_guard_seconds"]:
            with self.subTest(field=field), self.assertRaises(ValueError):
                build(**{field: 0})
        with self.assertRaises(ValueError):
            build(request_generation_window_seconds=5)

    def test_inputs_are_not_mutated(self):
        offsets = [223, 293]
        before = copy.deepcopy(offsets)
        result = build(arp_offsets=offsets)
        result["opportunities"][0]["offsetSeconds"] = 0
        self.assertEqual(offsets, before)


if __name__ == "__main__":
    unittest.main()
