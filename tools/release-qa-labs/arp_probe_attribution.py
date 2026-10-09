#!/usr/bin/env python3
"""Offline attribution of a completed ARP command to captured broadcast probes.

This verifies one positive-control subgate, not a complete qualification. The
caller must independently bind the observer/configuration, the fresh federated
request generation, its remaining TTL, controller progress, complete captures,
and preserved environment. Retained database facts are not pending commands.

Samples use receiver-local epoch/completedEpoch timestamps around each daemon
status read, pid/startTicks/since identity, and the five COUNTERS below. Counters
may be JSON integers or daemon decimal strings. Packet text is tcpdump output
from ``-nn -tt -e``. valid_after/valid_before must already include measured clock
uncertainty for the selected request generation. All limits must be frozen
before collecting qualification evidence.
"""

import collections
import ipaddress
import math
import re


COUNTERS = (
    "commandProbeCount", "probeCount", "proactiveCount",
    "requestObservedCount", "scanCount",
)
AUTONOMOUS = ("proactiveCount", "requestObservedCount", "scanCount")
MAC = re.compile(r"[0-9a-f]{2}(?::[0-9a-f]{2}){5}\Z")
REQUEST = re.compile(
    r"^(?P<at>[0-9]+(?:\.[0-9]+)?)\s+"
    r"(?P<src>[0-9a-f:]{17}) > (?P<dst>[0-9a-f:]{17}), "
    r".*ethertype ARP .*Request who-has (?P<target>[0-9.]+)"
    r"(?: \([^)]*\))? tell (?P<sender>[0-9.]+),", re.IGNORECASE
)


def _number(value):
    if isinstance(value, bool):
        raise ValueError("boolean is not a timestamp or duration")
    value = float(value)
    if not math.isfinite(value):
        raise ValueError("non-finite timestamp or duration")
    return value


def _integer(value):
    if isinstance(value, bool) or not re.fullmatch(r"[0-9]+", str(value)):
        raise ValueError("counter/identity must be a nonnegative integer")
    return int(value)


def _samples(rows):
    parsed = []
    for row in rows:
        start, end = _number(row["epoch"]), _number(row["completedEpoch"])
        identity = (_integer(row["pid"]), _integer(row["startTicks"]), row["since"])
        if start > end or not identity[0] or not isinstance(identity[2], str) or not identity[2]:
            raise ValueError("invalid sample timing or observer identity")
        counts = {name: _integer(row[name]) for name in COUNTERS}
        if parsed:
            previous = parsed[-1]
            if start < previous["end"]:
                raise ValueError("overlapping or reversed sample timestamps")
            if identity != previous["identity"]:
                raise ValueError("observer identity changed")
            if any(counts[name] < previous["counts"][name] for name in COUNTERS):
                raise ValueError("observer counter reset")
        parsed.append({"start": start, "end": end, "identity": identity, "counts": counts})
    if len(parsed) < 2:
        raise ValueError("insufficient counter samples")
    return parsed


def _packets(text, source_ip, source_mac):
    broadcast, unicast = [], 0
    for line in text.splitlines():
        line = line.lower()
        match = REQUEST.match(line)
        if not match:
            if source_mac + " > " in line and "ethertype arp" in line and "request who-has" in line:
                raise ValueError("unparseable ARP request from bound observer MAC")
            continue
        packet = match.groupdict()
        if packet["src"] != source_mac or packet["sender"] != source_ip:
            continue
        packet["target"] = str(ipaddress.IPv4Address(packet["target"]))
        packet["at"] = _number(packet["at"])
        if packet["dst"] == "ff:ff:ff:ff:ff:ff":
            broadcast.append(packet)
        else:
            unicast += 1
    broadcast.sort(key=lambda item: item["at"])
    return broadcast, unicast


def attribute_command_probe(
    samples, packet_text, *, source_ip, source_mac, target_ip,
    probe_retries, probe_timeout_seconds, valid_after, valid_before,
    capture_complete, kernel_drops, other_observers_passive,
    max_sample_gap=0.4, retry_spacing_tolerance=0.15,
):
    """Return fail-closed target attribution, with explicit accepted windows.

For a completed command, require exactly retries+1 broadcast requests and
probeCount increments, one command completion, and no autonomous increments.
Every bound-source broadcast in the interval must target the selected address.
Packet timestamps inside an endpoint status-read interval remain ambiguous.
Unicast kernel NUD requests cannot establish a daemon probe.
"""
    result = {"success": False, "acceptedWindows": [], "rejections": {}}
    try:
        if not all(isinstance(value, str) for value in (source_ip, source_mac, target_ip, packet_text)):
            raise ValueError("addresses, MAC, and capture must be strings")
        source_ip = str(ipaddress.IPv4Address(source_ip))
        target_ip = str(ipaddress.IPv4Address(target_ip))
        source_mac = source_mac.lower()
        if not MAC.fullmatch(source_mac) or source_ip == target_ip:
            raise ValueError("invalid positive-control address or MAC")
        if capture_complete is not True or _integer(kernel_drops) != 0:
            raise ValueError("capture is incomplete or dropped packets")
        if other_observers_passive is not True:
            raise ValueError("other observers may actively probe")
        attempts = _integer(probe_retries) + 1
        timeout = _number(probe_timeout_seconds)
        gap = _number(max_sample_gap)
        tolerance = _number(retry_spacing_tolerance)
        valid_after, valid_before = _number(valid_after), _number(valid_before)
        if timeout <= 0 or gap <= 0 or tolerance < 0 or valid_after >= valid_before:
            raise ValueError("invalid frozen timing bounds")
        rows = _samples(samples)
        packets, unicast = _packets(packet_text, source_ip, source_mac)
    except (KeyError, TypeError, ValueError, OverflowError) as error:
        result["error"] = str(error)
        return result

    rejected = collections.Counter()
    accepted_completions = set()
    maximum_window = (attempts - 1) * timeout + 2 * gap + 2 * tolerance
    quiet_guard = (attempts - 1) * timeout + gap + tolerance
    for end_index in range(1, len(rows)):
        after = rows[end_index]
        completion = after["counts"]["commandProbeCount"]
        if completion <= rows[0]["counts"]["commandProbeCount"] or completion in accepted_completions:
            continue
        for begin_index in range(end_index - 1, -1, -1):
            before = rows[begin_index]
            if after["end"] - before["start"] > maximum_window:
                break
            delta = {name: after["counts"][name] - before["counts"][name] for name in COUNTERS}
            if delta["commandProbeCount"] != 1 or delta["probeCount"] != attempts:
                rejected["command_or_probe_count"] += 1
                continue
            if any(delta[name] != 0 for name in AUTONOMOUS):
                rejected["autonomous_probe_overlap"] += 1
                continue
            window = rows[begin_index:end_index + 1]
            if any(b["end"] - a["start"] > gap for a, b in zip(window, window[1:])):
                rejected["sparse_counter_samples"] += 1
                continue
            if before["start"] < valid_after or after["end"] > valid_before:
                rejected["outside_request_validity"] += 1
                continue
            frames = [p for p in packets if before["start"] <= p["at"] <= after["end"]]
            if len(frames) != attempts or any(p["target"] != target_ip for p in frames):
                rejected["broadcast_target_or_count"] += 1
                continue
            if any(not before["end"] < p["at"] < after["start"] for p in frames):
                rejected["snapshot_boundary_ambiguity"] += 1
                continue
            if any(abs(b["at"] - a["at"] - timeout) > tolerance for a, b in zip(frames, frames[1:])):
                rejected["retry_spacing"] += 1
                continue
            guard_start = frames[0]["at"] - quiet_guard
            guard_index = next((i for i in range(begin_index, -1, -1) if rows[i]["end"] <= guard_start), None)
            if guard_index is None or any(guard_start <= p["at"] < frames[0]["at"] for p in packets):
                rejected["no_quiet_probe_guard"] += 1
                continue
            guard_rows = rows[guard_index:begin_index + 1]
            if any(row["counts"] != before["counts"] for row in guard_rows) or any(
                b["end"] - a["start"] > gap for a, b in zip(guard_rows, guard_rows[1:])
            ):
                rejected["guard_counter_activity_or_gap"] += 1
                continue
            result["acceptedWindows"].append({
                "beforeSample": begin_index, "afterSample": end_index,
                "start": before["start"], "end": after["end"],
                "counterDeltas": delta, "probePackets": frames,
                "observerIdentity": list(before["identity"]),
                "quietGuardStart": guard_start,
            })
            accepted_completions.add(completion)
            break
    result["success"] = bool(result["acceptedWindows"])
    result["rejections"] = dict(rejected)
    result["unicastRequestsExcluded"] = unicast
    result["method"] = "one command completion, exact probe/capture accounting, no autonomous increments"
    return result
