import datetime
import importlib.util
import json
from pathlib import Path
import unittest


SPEC = importlib.util.spec_from_file_location(
    "arp_command_evidence", Path(__file__).resolve().parents[1] / "arp_probe_attribution.py"
)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)
SOURCE, TARGET, OTHER, MAC = "192.0.2.10", "192.0.2.20", "192.0.2.30", "02:00:00:00:00:10"


def stamp(at):
    return datetime.datetime.fromtimestamp(at, datetime.timezone.utc).isoformat().replace("+00:00", "Z")


def packet(at, target=TARGET, destination="ff:ff:ff:ff:ff:ff"):
    return (f"{at:.6f} {MAC} > {destination}, ethertype ARP (0x0806), length 42: "
            f"Request who-has {target} tell {SOURCE}, length 28")


def fixture():
    # A prior command ends only 1ms before the selected command's first packet;
    # proactive work overlaps it. Aggregate target isolation is impossible.
    frames = [(99.249, OTHER), (99.749, OTHER), (100.249, OTHER),
              (100.25, TARGET), (100.75, TARGET), (101.25, TARGET),
              (100.57, "192.0.2.40"), (101.07, "192.0.2.40"), (101.57, "192.0.2.40")]
    records = [dict(sequence=1, target=OTHER, startedAt=stamp(99.24), completedAt=stamp(100.2495), packetsSent=3),
               dict(sequence=2, target=TARGET, startedAt=stamp(100.2498), completedAt=stamp(101.251), packetsSent=3)]
    rows = []
    for index in range(61):
        at = 98 + index / 10
        completed = [r for r in records if MODULE._command_timestamp(r["completedAt"]) < at]
        row = dict(epoch=at, completedEpoch=at+.002, pid=100, startTicks="200", since=stamp(50),
                   commandProbeCount=len(completed), probeCount=sum(t < at for t, _ in frames),
                   proactiveCount=int(at > 100.569), requestObservedCount=0, scanCount=0)
        if completed:
            row["lastCommandProbe"] = json.dumps(completed[-1])
        rows.append(row)
    return rows, "\n".join(packet(at, target) for at, target in sorted(frames))


def evaluate(rows=None, packets=None, **overrides):
    original_rows, original_packets = fixture()
    options = dict(source_ip=SOURCE, source_mac=MAC, target_ip=TARGET,
                   probe_retries=2, probe_timeout_seconds=.5, valid_after=100.2, valid_before=103,
                   capture_complete=True, kernel_drops=0, other_observers_passive=True)
    options.update(overrides)
    return MODULE.attribute_recorded_command(original_rows if rows is None else rows,
                                             original_packets if packets is None else packets, **options)


def change_records(rows, **updates):
    for row in rows:
        if row["commandProbeCount"] == 2:
            record = json.loads(row["lastCommandProbe"])
            record.update(updates)
            row["lastCommandProbe"] = json.dumps(record)


class CommandEvidenceTests(unittest.TestCase):
    def test_explicit_target_record_accepts_interleaved_work(self):
        result = evaluate()
        self.assertTrue(result["success"], result)
        self.assertEqual(len(result["acceptedWindows"]), 1)
        window = result["acceptedWindows"][0]
        self.assertEqual(window["commandEvidence"]["sequence"], 2)
        self.assertEqual(window["commandEvidence"]["packetsSent"], 3)
        self.assertEqual(len(window["probePackets"]), 3)
        self.assertNotIn("counterDeltas", window)

    def test_aggregate_counters_never_replace_absent_record(self):
        rows, packets = fixture()
        for row in rows:
            row.pop("lastCommandProbe", None)
        result = evaluate(rows, packets)
        self.assertFalse(result["success"])
        self.assertIn("missing original", result["error"])

    def test_record_for_another_target_cannot_credit_selected_packets(self):
        rows, packets = fixture()
        change_records(rows, target=OTHER)
        self.assertFalse(evaluate(rows, packets)["success"])

    def test_stale_record_and_suppressed_command_cannot_pass(self):
        rows, packets = fixture()
        change_records(rows, startedAt=stamp(90), completedAt=stamp(91))
        self.assertFalse(evaluate(rows, packets)["success"])
        rows, packets = fixture()
        record = rows[-1]["lastCommandProbe"]
        for row in rows:
            row["commandProbeCount"] = 2
            row["lastCommandProbe"] = record
        self.assertFalse(evaluate(rows, packets)["success"])

    def test_partial_send_and_inconsistent_sequence_are_rejected(self):
        for updates in [dict(packetsSent=2), dict(sequence=1), dict(sequence=0), dict(sequence=True)]:
            rows, packets = fixture()
            change_records(rows, **updates)
            self.assertFalse(evaluate(rows, packets)["success"])

    def test_record_cannot_change_under_one_sequence(self):
        rows, packets = fixture()
        value = json.loads(rows[-1]["lastCommandProbe"])
        value["target"] = OTHER
        rows[-1]["lastCommandProbe"] = json.dumps(value)
        self.assertFalse(evaluate(rows, packets)["success"])

    def test_record_timing_must_fit_process_and_status_read(self):
        for updates in [dict(startedAt=stamp(49)), dict(startedAt=stamp(102)),
                        dict(completedAt=stamp(110)), dict(startedAt="2026-01-01T00:00:00"),
                        dict(completedAt="invalid")]:
            rows, packets = fixture()
            change_records(rows, **updates)
            self.assertFalse(evaluate(rows, packets)["success"])

    def test_receipt_and_expiry_bounds_are_not_widened(self):
        self.assertFalse(evaluate(valid_after=100.25)["success"])
        self.assertFalse(evaluate(valid_before=101.25)["success"])

    def test_missing_extra_or_unicast_target_packets_fail(self):
        rows, packets = fixture()
        lines = packets.splitlines()
        for altered in ["\n".join(line for line in lines if not line.startswith("100.750000")),
                        packets + "\n" + packet(100.8),
                        packets.replace("ff:ff:ff:ff:ff:ff", "02:00:00:00:00:20")]:
            self.assertFalse(evaluate(rows, altered)["success"])

    def test_packet_outside_explicit_command_window_is_not_credited(self):
        rows, packets = fixture()
        self.assertFalse(evaluate(rows, packets.replace("100.250000", "100.240000"))["success"])
        self.assertFalse(evaluate(rows, packets.replace("101.250000", "101.260000"))["success"])

    def test_wrong_retry_spacing_fails(self):
        rows, packets = fixture()
        self.assertFalse(evaluate(rows, packets.replace("100.750000", "100.400000"))["success"])

    def test_recorded_writes_do_not_require_aggregate_write_counter(self):
        rows, packets = fixture()
        for row in rows:
            row["probeCount"] = 0
        self.assertTrue(evaluate(rows, packets)["success"])

    def test_recorded_command_does_not_require_continuous_sampling(self):
        rows, packets = fixture()
        self.assertTrue(evaluate([r for r in rows if not 100.3 < r["epoch"] < 101], packets)["success"])
        self.assertTrue(evaluate([r for r in rows if r["epoch"] >= 100.3], packets)["success"])

    def test_process_restart_or_counter_reset_fails(self):
        for field, value in [("pid", 101), ("startTicks", "201"), ("since", stamp(60)), ("commandProbeCount", 0)]:
            rows, packets = fixture()
            rows[-1][field] = value
            self.assertFalse(evaluate(rows, packets)["success"])

    def test_capture_and_other_observer_requirements_remain_mandatory(self):
        for options in [dict(capture_complete=False), dict(kernel_drops=1),
                        dict(other_observers_passive=False), dict(target_ip=SOURCE)]:
            self.assertFalse(evaluate(**options)["success"])

    def test_malformed_record_is_not_skipped_after_valid_evidence(self):
        for value in ["invalid", "[]", "null", "{}", {}, None]:
            rows, packets = fixture()
            rows[-1]["lastCommandProbe"] = value
            self.assertFalse(evaluate(rows, packets)["success"])

    def test_nanosecond_timestamps_keep_the_fraction(self):
        self.assertAlmostEqual(MODULE._command_timestamp("1970-01-01T00:01:40.123456789Z"), 100.123456789, places=9)


if __name__ == "__main__":
    unittest.main()
