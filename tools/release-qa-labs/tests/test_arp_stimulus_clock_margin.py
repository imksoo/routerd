import importlib.util
from pathlib import Path
import unittest


SPEC = importlib.util.spec_from_file_location(
    "arp_control_schedule", Path(__file__).resolve().parents[1] / "arp_control_schedule.py"
)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def margin(**changes):
    options = dict(stimulus_lead_seconds=6.1, maximum_host_clock_bound_seconds=3,
                   dispatch_timeout_seconds=15, transport_reserve_seconds=8)
    options.update(changes)
    return MODULE.validate_stimulus_clock_margin(**options)


class StimulusClockMarginTests(unittest.TestCase):
    def test_real_lead_reserves_transport_and_requires_actual_evidence(self):
        result = margin()
        self.assertEqual(result["requiredActualLeadSeconds"], 6.1)
        self.assertAlmostEqual(result["minimumClockMarginSeconds"], .1)
        self.assertTrue(result["requiresMeasuredLead"])
        self.assertTrue(result["attributionRequired"])
        self.assertLess(6.1 + result["transportReserveSeconds"],
                        result["dispatchTimeoutSeconds"])

    def test_immediate_arp_can_fail_with_actual_r6_clock_bounds(self):
        # Observed packet/event offsets from coordinator dispatch, without
        # deliberate lead. The strict gate correctly refuses this uncertainty.
        for offset, bound in [(1.159738, 1.2728798389434814),
                              (1.160454, 1.2884411811828613)]:
            self.assertLess(offset - bound, 0)
        with self.assertRaises(ValueError):
            margin(stimulus_lead_seconds=0)

    def test_positive_margin_at_both_extremes_of_every_clock_offset(self):
        lead = margin()["requiredActualLeadSeconds"]
        for bound in [0, .1, 1.2884411811828613, 2.999999, 3]:
            for actual_offset in [-bound, 0, bound]:
                # Earliest physical send is dispatch start + actual lead.
                host_packet_time = lead + actual_offset
                self.assertGreater(host_packet_time - bound, 0)

    def test_one_bound_of_delay_is_insufficient_at_negative_clock_offset(self):
        for lead in [1.5, 3, 5.9, 6]:
            with self.subTest(lead=lead), self.assertRaises(ValueError):
                margin(stimulus_lead_seconds=lead)

    def test_clock_growth_cannot_reuse_the_same_lead(self):
        for bound in [3.05, 3.1, 4]:
            with self.subTest(bound=bound), self.assertRaises(ValueError):
                margin(maximum_host_clock_bound_seconds=bound)

    def test_real_delay_must_fit_with_transport_before_absolute_deadline(self):
        for changes in [dict(dispatch_timeout_seconds=10),
                        dict(dispatch_timeout_seconds=14.1),
                        dict(transport_reserve_seconds=8.9),
                        dict(stimulus_lead_seconds=7)]:
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                margin(**changes)

    def test_two_delayed_controls_fit_full_observation_and_independent_ttls(self):
        budget = margin()
        result = MODULE.build_positive_control_schedule(
            ping_offsets=[60, 120], arp_offsets=[213, 286], observation_seconds=400,
            capture_lead_seconds=20, request_generation_window_seconds=30,
            dispatch_timeout_seconds=budget["dispatchTimeoutSeconds"],
            request_ttl_seconds=45, clock_allowance_seconds=10,
            expiry_tail_seconds=10, quiet_guard_seconds=1.55,
        )
        self.assertEqual(result["requiredObservationSeconds"], 386)
        self.assertEqual(result["observationReserveSeconds"], 14)
        self.assertAlmostEqual(result["minimumSeparationSeconds"], 71.55)
        self.assertTrue(all(not x["conditional"] for x in result["opportunities"]))

    def test_malformed_bounds_or_absent_required_reserves_fail(self):
        for field in ["stimulus_lead_seconds", "maximum_host_clock_bound_seconds",
                      "dispatch_timeout_seconds", "transport_reserve_seconds"]:
            for value in [True, -1, float("inf"), float("nan"), "3", None]:
                with self.subTest(field=field, value=value), self.assertRaises(ValueError):
                    margin(**{field: value})
        for field in ["stimulus_lead_seconds", "dispatch_timeout_seconds",
                      "transport_reserve_seconds"]:
            with self.subTest(field=field), self.assertRaises(ValueError):
                margin(**{field: 0})


if __name__ == "__main__":
    unittest.main()
