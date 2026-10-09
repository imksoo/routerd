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
import hashlib
import ipaddress
import json
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


def _request_packets(text, source_ip, source_mac):
    packets = []
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
        if not MAC.fullmatch(packet["dst"]):
            raise ValueError("invalid destination MAC in bound ARP request")
        packet["line"] = line
        packets.append(packet)
    return sorted(packets, key=lambda item: item["at"])


def _packets(text, source_ip, source_mac):
    packets = _request_packets(text, source_ip, source_mac)
    broadcast = [{k: v for k, v in packet.items() if k != "line"}
                 for packet in packets if packet["dst"] == "ff:ff:ff:ff:ff:ff"]
    unicast = len(packets) - len(broadcast)
    return broadcast, unicast


def matching_arp_requests(
    packet_text, *, source_ip, source_mac, target_ip, not_before, not_after,
):
    """Parse matching sender requests, including tcpdump's optional (MAC) field.

Both unicast and broadcast requests may establish a client request. This is
also suitable for detecting forbidden self-target requests. It does not prove
a daemon command; that requires attribute_command_probe and its broadcast
accounting. Capture completeness and cross-host clock bounds remain caller gates.
"""
    result = {"success": False, "packets": []}
    try:
        if not all(isinstance(v, str) for v in (packet_text, source_ip, source_mac, target_ip)):
            raise ValueError("capture and addresses must be strings")
        source_ip, target_ip = str(ipaddress.IPv4Address(source_ip)), str(ipaddress.IPv4Address(target_ip))
        source_mac = source_mac.lower()
        if not MAC.fullmatch(source_mac):
            raise ValueError("invalid source MAC")
        start, end = _number(not_before), _number(not_after)
        if start > end:
            raise ValueError("reversed packet interval")
        result["packets"] = [p for p in _request_packets(packet_text, source_ip, source_mac)
                             if p["target"] == target_ip and start <= p["at"] <= end]
        result["success"] = True
    except (KeyError, TypeError, ValueError, OverflowError) as error:
        result["error"] = str(error)
    return result


REQUEST_KEYS = (
    "id", "group_name", "source_node", "type", "subject", "dedupe_key",
    "payload", "observed_at", "expires_at",
)


def first_request_receipt(
    counter_sampling, request, *, clock_uncertainty_seconds, minimum_remaining_seconds=30,
):
    """Use the first completed full-key DB read for either self or positive QA.

Inputs are receiver-local DB/status samples and SHA256-bound changed DB states.
Every sample names its dbStateIndex and brackets SELECT with dbReadEpoch and
dbCompletedEpoch, followed by epoch/completedEpoch for the counter read. Never
substitute a read's start, recorded_at, or a slower observer's polling time.
The caller still proves request origin/freshness, TTL tail, observer identity,
controller progress, packet capture, and absence of competing generations.
"""
    result = {"success": False}
    try:
        uncertainty, minimum = _number(clock_uncertainty_seconds), _number(minimum_remaining_seconds)
        if uncertainty < 0 or minimum <= 0:
            raise ValueError("invalid frozen receipt bounds")
        for key in REQUEST_KEYS:
            if key not in request:
                raise ValueError("incomplete request generation")
        if not isinstance(request["payload"], dict) or any(
            not isinstance(request[k], str) or not request[k] for k in REQUEST_KEYS[:6]
        ):
            raise ValueError("invalid request identity")
        observed, expires = _integer(request["observed_at"]), _integer(request["expires_at"])
        if expires <= observed:
            raise ValueError("invalid request lifetime")
        if counter_sampling["errors"] != [] or counter_sampling["threadExited"] is not True:
            raise ValueError("incomplete or failed receiver sampler")
        states, samples = counter_sampling["dbStates"], counter_sampling["samples"]
        if not isinstance(states, list) or not states or not isinstance(samples, list) or not samples:
            raise ValueError("missing DB snapshots or samples")
        for state in states:
            if not isinstance(state["rows"], list) or any(not isinstance(row, dict) for row in state["rows"]):
                raise ValueError("invalid DB snapshot rows")
            raw = json.dumps(state["rows"], ensure_ascii=False, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()
            if hashlib.sha256(raw).hexdigest() != state["sha256"]:
                raise ValueError("DB snapshot digest mismatch")
        previous_end, first = None, None
        for index, sample in enumerate(samples):
            began, end = _number(sample["dbReadEpoch"]), _number(sample["dbCompletedEpoch"])
            status_start, status_end = _number(sample["epoch"]), _number(sample["completedEpoch"])
            if not began <= end <= status_start <= status_end or (previous_end is not None and began < previous_end):
                raise ValueError("overlapping or reversed DB/status reads")
            previous_end = status_end
            state_index = sample["dbStateIndex"]
            if isinstance(state_index, bool) or not isinstance(state_index, int) or not 0 <= state_index < len(states):
                raise ValueError("missing referenced DB snapshot")
            matches = [row for row in states[state_index]["rows"]
                       if all(key in row and row[key] == request[key] for key in REQUEST_KEYS)]
            if len(matches) > 1:
                raise ValueError("duplicate request generation in DB snapshot")
            if matches and first is None:
                first = {"sampleIndex": index, "dbStateIndex": state_index,
                         "readEpoch": began, "firstReadEpoch": end}
        if first is None:
            raise ValueError("full-key request generation never observed")
        remaining = expires - first["firstReadEpoch"] - uncertainty
        result.update(first, remainingTTLLowerBound=remaining,
                      minimumRemainingSeconds=minimum, clockUncertaintySeconds=uncertainty,
                      success=remaining >= minimum)
        if not result["success"]:
            result["error"] = "insufficient remaining request lifetime"
    except (KeyError, TypeError, ValueError, OverflowError) as error:
        result["error"] = str(error)
    return result


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
