import hashlib
import importlib.util
import json
from pathlib import Path
import unittest


SPEC = importlib.util.spec_from_file_location(
    "arp_probe_attribution", Path(__file__).resolve().parents[1] / "arp_probe_attribution.py"
)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)
SOURCE, TARGET, MAC = "192.0.2.10", "192.0.2.20", "02:00:00:00:00:10"


def request():
    return dict(id="request", group_name="pool", source_node="origin", type="arp.request",
                subject=TARGET + "/32", dedupe_key="request", payload={"requesterIP": SOURCE},
                observed_at=100, expires_at=145)


def state(rows):
    raw = json.dumps(rows, ensure_ascii=False, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()
    return {"rows": rows, "sha256": hashlib.sha256(raw).hexdigest()}


def fixture():
    samples = []
    for at, index in [(109.9, 0), (110, 1), (110.1, 1), (112, 1)]:
        samples.append(dict(dbReadEpoch=at - .01, dbCompletedEpoch=at,
                            epoch=at + .001, completedEpoch=at + .01, dbStateIndex=index))
    return dict(errors=[], threadExited=True, samples=samples, dbStates=[state([]), state([request()])])


def receipt(value=None, selected=None, **options):
    options.setdefault("clock_uncertainty_seconds", 4.5)
    return MODULE.first_request_receipt(fixture() if value is None else value,
                                        request() if selected is None else selected, **options)


def packet(*, hardware="", target=TARGET, sender=SOURCE, mac=MAC, at=110.2, dst="ff:ff:ff:ff:ff:ff"):
    return (f"{at} {mac} > {dst}, ethertype ARP (0x0806), length 42: "
            f"Request who-has {target}{hardware} tell {sender}, length 28")


def packets(text, **options):
    defaults = dict(source_ip=SOURCE, source_mac=MAC, target_ip=TARGET, not_before=109, not_after=111)
    defaults.update(options)
    return MODULE.matching_arp_requests(text, **defaults)


class RequestReceiptTests(unittest.TestCase):
    def test_first_completed_fast_read_preserves_existing_ttl_requirement(self):
        result = receipt()
        self.assertTrue(result["success"], result)
        self.assertEqual(result["firstReadEpoch"], 110)
        self.assertEqual(result["remainingTTLLowerBound"], 30.5)
        self.assertLess(145 - 112 - 4.5, result["minimumRemainingSeconds"])

    def test_select_start_and_recorded_at_cannot_backdate_receipt(self):
        value = fixture()
        value["samples"] = [dict(dbReadEpoch=109, dbCompletedEpoch=111, epoch=111.01,
                                 completedEpoch=111.02, dbStateIndex=1)]
        value["dbStates"][1] = state([dict(request(), recorded_at=100)])
        result = receipt(value)
        self.assertFalse(result["success"])
        self.assertEqual(result["firstReadEpoch"], 111)
        self.assertEqual(result["remainingTTLLowerBound"], 29.5)

    def test_all_generation_keys_are_required_and_exact(self):
        for key in MODULE.REQUEST_KEYS:
            with self.subTest(key=key):
                selected = request()
                selected[key] = {"requesterIP": "192.0.2.30"} if key == "payload" else selected[key] + 1 if isinstance(selected[key], int) else selected[key] + "-other"
                self.assertFalse(receipt(selected=selected)["success"])
                del selected[key]
                self.assertFalse(receipt(selected=selected)["success"])

    def test_missing_corrupt_and_duplicate_states_fail(self):
        for rows in [[], [dict(request(), subject="192.0.2.30/32")], [request(), request()]]:
            value = fixture()
            value["dbStates"][1] = state(rows)
            self.assertFalse(receipt(value)["success"])
        value = fixture()
        value["dbStates"][1]["rows"][0]["payload"]["requesterIP"] = "192.0.2.30"
        self.assertFalse(receipt(value)["success"])

    def test_invalid_read_timing_reference_and_sampler_fail_closed(self):
        for field, replacement in [("dbCompletedEpoch", 120), ("dbReadEpoch", 120),
                                   ("dbStateIndex", -1), ("dbStateIndex", True),
                                   ("dbStateIndex", 9), ("epoch", float("nan"))]:
            value = fixture()
            value["samples"][-1][field] = replacement
            self.assertFalse(receipt(value)["success"], (field, replacement))
        for field, replacement in [("errors", ["sqlite busy"]), ("threadExited", False),
                                   ("samples", []), ("dbStates", [])]:
            value = fixture()
            value[field] = replacement
            self.assertEqual(receipt(value)["success"], field in ("errors", "threadExited"))

    def test_later_bad_evidence_is_not_hidden_by_an_earlier_match(self):
        value = fixture()
        value["samples"][-1]["dbReadEpoch"] = 100
        self.assertFalse(receipt(value)["success"])

    def test_threshold_and_clock_bounds_are_not_relaxed(self):
        self.assertTrue(receipt(clock_uncertainty_seconds=5)["success"])
        self.assertFalse(receipt(clock_uncertainty_seconds=5.001)["success"])
        for options in [dict(clock_uncertainty_seconds=-1), dict(clock_uncertainty_seconds=True),
                        dict(clock_uncertainty_seconds=float("nan")), dict(minimum_remaining_seconds=0)]:
            self.assertFalse(receipt(**options)["success"])


class RequestPacketTests(unittest.TestCase):
    def test_plain_and_parenthesized_target_hardware_are_both_parsed(self):
        result = packets(packet() + "\n" + packet(hardware=" (ff:ff:ff:ff:ff:ff)"))
        self.assertTrue(result["success"], result)
        self.assertEqual(len(result["packets"]), 2)
        self.assertTrue(all(p["sender"] == SOURCE and p["target"] == TARGET for p in result["packets"]))

    def test_exact_sender_mac_target_and_time_are_required(self):
        for options in [dict(mac="02:00:00:00:00:11"), dict(sender="192.0.2.11"),
                        dict(target="192.0.2.21"), dict(at=112)]:
            result = packets(packet(**options))
            self.assertTrue(result["success"], result)
            self.assertEqual(result["packets"], [])

    def test_self_target_with_hardware_annotation_is_detected(self):
        result = packets(packet(target=SOURCE, hardware=" (ff:ff:ff:ff:ff:ff)"), target_ip=SOURCE)
        self.assertTrue(result["success"], result)
        self.assertEqual(len(result["packets"]), 1)

    def test_unicast_client_request_is_not_filtered_as_command_evidence(self):
        result = packets(packet(dst="02:00:00:00:00:20"))
        self.assertTrue(result["success"], result)
        self.assertEqual(len(result["packets"]), 1)

    def test_malformed_bound_source_capture_and_invalid_bounds_fail(self):
        self.assertFalse(packets(packet().replace(TARGET, "unknown"))["success"])
        self.assertFalse(packets(packet(), not_after=108)["success"])
        self.assertFalse(packets(packet(), source_mac="invalid")["success"])
        self.assertFalse(packets(packet(), not_before=float("inf"))["success"])


if __name__ == "__main__":
    unittest.main()
