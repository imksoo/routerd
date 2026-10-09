import importlib.util
from pathlib import Path
import unittest


SPEC = importlib.util.spec_from_file_location(
    "arp_probe_attribution", Path(__file__).resolve().parents[1] / "arp_probe_attribution.py"
)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)
SOURCE = "192.0.2.10"
TARGET = "192.0.2.20"
MAC = "02:00:00:00:00:10"
FRAMES = [100.2, 100.7, 101.2]


def packet(at, target=TARGET, dst="ff:ff:ff:ff:ff:ff"):
    return (f"{at:.6f} {MAC} > {dst}, ethertype ARP (0x0806), length 42: "
            f"Request who-has {target} tell {SOURCE}, length 28")


def fixture():
    samples = []
    for index in range(51):
        at = 98 + index / 10
        samples.append({
            "epoch": at, "completedEpoch": at + 0.01,
            "pid": 100, "startTicks": "200", "since": "2026-01-01T00:00:00Z",
            "commandProbeCount": int(at > 101.21),
            "probeCount": sum(at > frame for frame in FRAMES),
            "proactiveCount": "0", "requestObservedCount": "0", "scanCount": "0",
            # A retained foreign request is deliberately present in every row.
            "db": [{"target": "192.0.2.30", "observed_at": 90, "expires_at": 135}],
        })
    return samples, "\n".join(packet(at) for at in FRAMES)


def evaluate(samples=None, packets=None, **overrides):
    original_samples, original_packets = fixture()
    options = dict(
        source_ip=SOURCE, source_mac=MAC, target_ip=TARGET,
        probe_retries=2, probe_timeout_seconds=0.5,
        valid_after=99, valid_before=130,
        capture_complete=True, kernel_drops=0, other_observers_passive=True,
    )
    options.update(overrides)
    return MODULE.attribute_command_probe(
        original_samples if samples is None else samples,
        original_packets if packets is None else packets, **options,
    )


class ProbeAttributionTests(unittest.TestCase):
    def test_target_accounting_passes_with_retained_background_fact(self):
        result = evaluate()
        self.assertTrue(result["success"], result)
        self.assertEqual(len(result["acceptedWindows"]), 1)
        self.assertEqual(result["acceptedWindows"][0]["counterDeltas"]["probeCount"], 3)

    def test_unicast_kernel_requests_do_not_prove_a_probe(self):
        self.assertFalse(evaluate(packets="\n".join(packet(at, dst="02:00:00:00:00:20") for at in FRAMES))["success"])

    def test_unicast_noise_does_not_change_complete_broadcast_accounting(self):
        _, packets = fixture()
        result = evaluate(packets=packets + "\n" + packet(100.4, dst="02:00:00:00:00:20"))
        self.assertTrue(result["success"], result)
        self.assertEqual(result["unicastRequestsExcluded"], 1)

    def test_command_for_another_target_cannot_be_credited(self):
        packets = "\n".join(packet(at, target="192.0.2.30") for at in FRAMES)
        packets += "\n" + "\n".join(packet(at, dst="02:00:00:00:00:20") for at in FRAMES)
        self.assertFalse(evaluate(packets=packets)["success"])

    def test_autonomous_paths_prevent_attribution(self):
        for counter in MODULE.AUTONOMOUS:
            with self.subTest(counter=counter):
                samples, _ = fixture()
                for sample in samples:
                    sample[counter] = int(sample["epoch"] > 100.1)
                self.assertFalse(evaluate(samples=samples)["success"])

    def test_two_second_mixed_command_proactive_evidence_stays_inconclusive(self):
        rows = []
        for at, commands, probes, proactive in [(99, 6, 84, 12), (101, 7, 88, 13), (103, 7, 90, 13)]:
            row = fixture()[0][0].copy()
            row.update(epoch=at, completedEpoch=at + 0.03, commandProbeCount=commands,
                       probeCount=probes, proactiveCount=proactive)
            rows.append(row)
        self.assertFalse(evaluate(samples=rows)["success"])

    def test_missing_or_extra_broadcast_frames_fail(self):
        for frames in [FRAMES[:-1], FRAMES + [101.21]]:
            with self.subTest(frames=frames):
                self.assertFalse(evaluate(packets="\n".join(packet(at) for at in frames))["success"])

    def test_wrong_target_or_retry_spacing_fails(self):
        self.assertFalse(evaluate(packets="\n".join(packet(at, target="192.0.2.30") for at in FRAMES))["success"])
        self.assertFalse(evaluate(packets="\n".join(packet(at) for at in [100.2, 100.25, 101.2]))["success"])

    def test_cannot_select_only_the_tail_of_an_existing_burst(self):
        self.assertFalse(evaluate(probe_retries=1)["success"])

    def test_inflight_probe_activity_prevents_a_quiet_guard(self):
        _, packets = fixture()
        self.assertFalse(evaluate(packets=packets + "\n" + packet(99.8, target="192.0.2.30"))["success"])

    def test_counter_reset_and_observer_restart_fail(self):
        for field, value in [("probeCount", 0), ("pid", 101), ("startTicks", "201"), ("since", "2026-01-02T00:00:00Z")]:
            with self.subTest(field=field):
                samples, _ = fixture()
                samples[-1][field] = value
                self.assertFalse(evaluate(samples=samples)["success"])

    def test_capture_and_binding_requirements_fail_closed(self):
        for options in [dict(capture_complete=False), dict(kernel_drops=1),
                        dict(other_observers_passive=False), dict(target_ip=SOURCE),
                        dict(valid_after=102), dict(valid_before=101)]:
            with self.subTest(options=options):
                self.assertFalse(evaluate(**options)["success"])

    def test_snapshot_edge_packet_is_ambiguous(self):
        samples, packets = fixture()
        # The completion first appears in a read overlapping the final frame.
        sample = next(s for s in samples if s["epoch"] == 101.2)
        sample.update(epoch=101.195, completedEpoch=101.225,
                      commandProbeCount=1, probeCount=3)
        result = evaluate(samples=samples[:samples.index(sample) + 1], packets=packets)
        self.assertFalse(result["success"])
        self.assertGreater(result["rejections"].get("snapshot_boundary_ambiguity", 0), 0)

    def test_later_snapshot_can_resolve_the_same_completion_boundary(self):
        samples, packets = fixture()
        sample = next(s for s in samples if s["epoch"] == 101.2)
        sample.update(epoch=101.195, completedEpoch=101.225,
                      commandProbeCount=1, probeCount=3)
        result = evaluate(samples=samples, packets=packets)
        self.assertTrue(result["success"], result)
        self.assertEqual(len(result["acceptedWindows"]), 1)
        self.assertGreater(result["acceptedWindows"][0]["end"], 101.225)

    def test_malformed_matching_capture_or_samples_cannot_pass(self):
        self.assertFalse(evaluate(packets=packet(100.2).replace(TARGET, "unknown"))["success"])
        for replacement in [float("nan"), True, -1]:
            samples, _ = fixture()
            samples[0]["probeCount"] = replacement
            self.assertFalse(evaluate(samples=samples)["success"])
        samples, _ = fixture()
        del samples[0]["commandProbeCount"]
        self.assertFalse(evaluate(samples=samples)["success"])


if __name__ == "__main__":
    unittest.main()
