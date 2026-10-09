"""Offline liveness proof for serial mobility-arp-request reconciles.

Completion counters advance after the whole target batch returns. A repeated
completion counter is acceptable only with independently sampled command
completions inside that read interval and a later reconcile completion. This
module does not prove target attribution or replace error/TTL/packet gates.
"""

import datetime
import math


def _number(value):
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ValueError("expected a numeric timestamp or gap")
    if not math.isfinite(value) or value < 0:
        raise ValueError("expected a finite nonnegative value")
    return float(value)


def _counter(value):
    if isinstance(value, bool) or not isinstance(value, int) or value < 0:
        raise ValueError("expected a nonnegative integer counter")
    return value


def _timestamp(value):
    parsed = datetime.datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        raise ValueError("controller timestamp has no timezone")
    return _number(parsed.timestamp())


def evaluate_arp_controller_progress(
    samples, counter_sampling, *, observer_pid, observer_start_ticks,
    observer_since, max_sample_gap=0.4,
):
    """Evaluate every original controller read; never discard equal pairs.

    counter_sampling is the receiver-local high-frequency observer series.
    Its PID/start ticks/since must be bound to the expected on-demand observer.
    The caller must exclude other command producers during the observation.
    A pair can establish either completed-reconcile progress or synchronous
    command-completion progress followed by a successful reconcile in this
    same bounded observation. Autonomous probe/scan counters cannot qualify.
    """
    result = {"success": False, "intervals": [], "errors": []}
    try:
        gap = _number(max_sample_gap)
        if gap <= 0 or _counter(observer_pid) == 0 or not observer_start_ticks:
            raise ValueError("missing observer binding or sample gap")
        _timestamp(observer_since)
        if len(samples) < 2:
            raise ValueError("need at least two controller reads")
        if counter_sampling["errors"] or counter_sampling["threadExited"] is not True:
            raise ValueError("counter sampler is incomplete or has errors")
        commands = counter_sampling["samples"]
        if len(commands) < 2:
            raise ValueError("missing independent command counter reads")
        fast = []
        for row in commands:
            if (row["pid"], str(row["startTicks"]), row["since"]) != (
                observer_pid, str(observer_start_ticks), observer_since,
            ):
                raise ValueError("observer process identity changed")
            start, end = _number(row["epoch"]), _number(row["completedEpoch"])
            count = _counter(row["commandProbeCount"])
            if end < start or (fast and (start < fast[-1][1] or count < fast[-1][2])):
                raise ValueError("command read timestamps or counters regressed")
            fast.append((start, end, count))
        controllers = []
        identity = samples[0]["bootId"], samples[0]["mainPID"]
        if not all(identity):
            raise ValueError("missing router identity")
        for row in samples:
            if (row["bootId"], row["mainPID"]) != identity:
                raise ValueError("router boot or PID changed")
            candidates = row["controllers"]
            if len(candidates) != 1 or candidates[0]["name"] != "mobility-arp-request":
                raise ValueError("missing or ambiguous ARP controller")
            c = candidates[0]
            start, end = _number(row["epoch"]), _number(row["completedEpoch"])
            last = _timestamp(c["lastReconcileTime"])
            success = _timestamp(c["lastSuccessTime"])
            count, errors = _counter(c["reconcileCount"]), _counter(c.get("reconcileErrorCount", 0))
            if end < start or last > end or c["currentError"] is not False or success != last:
                raise ValueError("invalid or unsuccessful controller read")
            if controllers and (start <= controllers[-1]["end"] or errors != controllers[0]["errors"]):
                raise ValueError("controller reads overlap or error count changed")
            controllers.append(dict(start=start, end=end, last=last, count=count, errors=errors))
        for index, (left, right) in enumerate(zip(controllers, controllers[1:])):
            proof = {"leftIndex": index, "rightIndex": index + 1, "success": False,
                     "beforeCount": left["count"], "afterCount": right["count"]}
            result["intervals"].append(proof)
            if right["count"] > left["count"] and right["last"] > left["last"]:
                proof.update(success=True, method="completed_reconcile")
                continue
            if right["count"] != left["count"] or right["last"] != left["last"]:
                proof["reason"] = "counter/time regression or inconsistent completion"
                continue
            # Interior reads prove command work after the first controller read
            # finished and before the next began, without cross-host clocks.
            window = [x for x in fast if left["end"] <= x[0] and x[1] <= right["start"]]
            if (len(window) < 2 or window[0][0] - left["end"] > gap or
                    right["start"] - window[-1][1] > gap or
                    any(b[1] - a[0] > gap for a, b in zip(window, window[1:]))):
                proof["reason"] = "missing interior command counter coverage"
                continue
            if window[-1][2] <= window[0][2]:
                proof["reason"] = "neither reconcile nor command completion progressed"
                continue
            later = next((i for i in range(index + 2, len(controllers))
                          if controllers[i]["count"] > right["count"] and
                          controllers[i]["last"] > right["last"]), None)
            if later is None:
                proof["reason"] = "in-flight reconcile did not complete within observation"
                continue
            proof.update(success=True, method="serial_command_completions",
                         commandCountBefore=window[0][2], commandCountAfter=window[-1][2],
                         firstCommandReadStart=window[0][0], lastCommandReadEnd=window[-1][1],
                         laterCompletedReadIndex=later)
        result["success"] = all(x["success"] for x in result["intervals"])
    except (KeyError, TypeError, ValueError, AttributeError, OverflowError) as error:
        result["errors"].append(str(error))
    return result
