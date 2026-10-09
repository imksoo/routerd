"""Validate fixed positive-control opportunities without inspecting trial results.

The caller sends ordinary ARP requests from the client's actual interface and
address. This module neither executes commands nor establishes attribution.
Natural background traffic can still make a trial inconclusive; the unchanged
packet/counter, quiet guard, generation, clock and expiry-tail gates decide that.
"""

import math


def _seconds(value):
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ValueError("timing bounds must be numbers")
    if not math.isfinite(value) or value < 0:
        raise ValueError("timing bounds must be finite and nonnegative")
    return float(value)


def _offsets(values):
    if not isinstance(values, (list, tuple)) or not values:
        raise ValueError("missing scheduled opportunities")
    result = [_seconds(value) for value in values]
    if result != sorted(set(result)):
        raise ValueError("opportunities must be unique and ordered")
    return result


def validate_stimulus_clock_margin(
    *, stimulus_lead_seconds, maximum_host_clock_bound_seconds,
    dispatch_timeout_seconds, transport_reserve_seconds,
):
    """Reserve real sender delay for an unchanged conservative freshness gate.

    If a host timestamp has offset in [-bound, bound], subtracting bound
    places its lower bound as much as 2*bound before real time. A deliberate
    sender delay greater than that amount establishes positive margin after
    the actual coordinator dispatch start, even at the worst allowed offset.

    The caller measures the delay with the sender's monotonic clock before
    executing ARP, enforces the remaining absolute dispatch deadline, and
    checks every host's clock bound at both ends. Requested delay alone is
    not evidence; packet/event freshness and all attribution gates still run.
    """
    lead, bound, dispatch, reserve = map(_seconds, (
        stimulus_lead_seconds, maximum_host_clock_bound_seconds,
        dispatch_timeout_seconds, transport_reserve_seconds,
    ))
    if min(lead, dispatch, reserve) <= 0:
        raise ValueError("lead, dispatch and transport reserve must be positive")
    if lead <= 2 * bound:
        raise ValueError("sender lead cannot establish a positive clock margin")
    if lead + reserve >= dispatch:
        raise ValueError("sender lead leaves insufficient dispatch reserve")
    return {
        "requiredActualLeadSeconds": lead,
        "maximumHostClockBoundSeconds": bound,
        "minimumClockMarginSeconds": lead - 2 * bound,
        "dispatchTimeoutSeconds": dispatch,
        "transportReserveSeconds": reserve,
        "requiresMeasuredLead": True,
        "attributionRequired": True,
    }


def build_positive_control_schedule(
    ping_offsets, arp_offsets, *, observation_seconds, capture_lead_seconds,
    request_generation_window_seconds, dispatch_timeout_seconds,
    request_ttl_seconds, clock_allowance_seconds, expiry_tail_seconds,
    quiet_guard_seconds,
):
    """Return unconditional ARP opportunities after bounded ping-generated work.

All offsets are relative to stimulus start. capture_lead_seconds reserves time
between the first capture starting and stimulus start. The generation window
reserves the expected ping/NUD delay; it is not proof that background requests
ceased. The caller enforces dispatch deadlines and independently checks actual
capture coverage, full request identity, competing generations and expiry tails.

No arrival/result input is accepted: delivery alone cannot cancel a scheduled
control. Require at least two independently timed ARP opportunities and enough
room between them for the preceding generation to expire plus the quiet guard.
"""
    pings, arps = _offsets(ping_offsets), _offsets(arp_offsets)
    if len(arps) < 2:
        raise ValueError("need two independent positive ARP opportunities")
    duration, lead, generation, dispatch, ttl, clock, tail, quiet = map(_seconds, (
        observation_seconds, capture_lead_seconds, request_generation_window_seconds,
        dispatch_timeout_seconds, request_ttl_seconds, clock_allowance_seconds,
        expiry_tail_seconds, quiet_guard_seconds,
    ))
    if min(duration, generation, dispatch, ttl, tail, quiet) <= 0:
        raise ValueError("observation and control bounds must be positive")
    if generation < dispatch:
        raise ValueError("generation window cannot be shorter than dispatch")
    first_safe = pings[-1] + generation + ttl + clock + quiet
    if arps[0] <= first_safe:
        raise ValueError("first ARP opportunity overlaps bounded ping-generated work")
    separation = dispatch + ttl + clock + quiet
    if any(right <= left + separation for left, right in zip(arps, arps[1:])):
        raise ValueError("ARP opportunities do not allow independent generations")
    required_end = lead + arps[-1] + dispatch + ttl + clock + tail
    if required_end >= duration:
        raise ValueError("observation cannot cover the complete final expiry tail")
    return {
        "opportunities": [
            {"offsetSeconds": offset, "kind": "arping", "conditional": False,
             "dispatchTimeoutSeconds": dispatch}
            for offset in arps
        ],
        "minimumSeparationSeconds": separation,
        "requiredObservationSeconds": required_end,
        "observationReserveSeconds": duration - required_end,
        "arrivalDoesNotCancel": True,
        "attributionRequired": True,
    }
