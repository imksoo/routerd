"""Construct an offline baseline from complete retained-ISO guest/media reads.

The caller binds both observations to the intended host/VM and their source
hashes, then runs the actual guest and media pre-stop evaluators before freeze.
This helper performs no network, process, filesystem, or configuration changes.
"""

import copy
import datetime
import ipaddress
import re
import uuid


def _require(condition, message):
    if not condition:
        raise ValueError(message)


def _text(value):
    return isinstance(value, str) and bool(value.strip()) and value == value.strip()


def _positive_integer(value):
    return isinstance(value, int) and not isinstance(value, bool) and value > 0


def _sha(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def build_rollout_baseline(
    observed, media, *, node, host, vmid, expected_version, management_device,
):
    """Require identity, assigned DHCP management addresses, and persistent media.

    observed is the complete read-only guest probe, including management statuses
    and IPv4 addresses on management_device. media is the actual host media read.
    The legacy runtime_sha256 field identifies runtime *configuration* bytes; the
    artifact manifest and pre-stop evaluator independently check executable bytes.
    """
    try:
        _require(all(_text(v) for v in (node, host, expected_version, management_device)),
                 "missing node/host/version/management device binding")
        _require(_positive_integer(vmid), "invalid VM identifier")
        _require(_text(observed["at"]), "missing observation timestamp")
        timestamp = datetime.datetime.fromisoformat(observed["at"].replace("Z", "+00:00"))
        _require(timestamp.tzinfo is not None, "observation timestamp needs a timezone")
        _require(_text(observed["hostname"]) and observed["hostname"] == media["name"],
                 "guest and media hostnames differ")
        _require(observed["version"] == expected_version, "unexpected observed version")
        _require(_sha(observed["config_sha256"]), "missing runtime configuration hash")
        _require(isinstance(observed["boot_id"], str)
                 and str(uuid.UUID(observed["boot_id"])) == observed["boot_id"], "invalid boot identity")

        statuses, assigned = observed["management"], observed["addresses"]
        _require(isinstance(statuses, list) and bool(statuses), "missing management DHCP observations")
        _require(isinstance(assigned, list) and bool(assigned), "missing assigned management addresses")
        _require(all(isinstance(a, str) for a in assigned), "invalid assigned address type")
        assigned = {str(ipaddress.IPv4Address(address)) for address in assigned}
        management = []
        for status in statuses:
            _require(status["device"] == management_device, "management interface mismatch")
            _require(status["phase"] == "Bound" and status["addressPresent"] is True,
                     "management DHCP is not bound and present")
            _require(isinstance(status["currentAddress"], str), "missing current DHCP address")
            address = str(ipaddress.IPv4Address(status["currentAddress"]))
            _require(address in assigned, "DHCP management address is not assigned")
            _require(address not in management, "duplicate management DHCP observation")
            management.append(address)

        _require(_text(media["boot"]), "missing retained boot order")
        _require(isinstance(media["media"], list), "missing media records")
        slots = {}
        for item in media["media"]:
            slot = item["slot"]
            _require(isinstance(slot, str) and slot not in slots, "duplicate or invalid media slot")
            _require(_text(item["reference"]) and _text(item["path"]), "missing media reference/path")
            if slot != "ide2":
                _require(_sha(item["media_sha256"]) and _positive_integer(item["media_bytes"]),
                         "missing persistent media hash/size")
            slots[slot] = item
        _require(set(slots) == {"ide2", "sata2", "scsi1"}, "incomplete or unexpected retained media")
        config = slots["scsi1"]
        _require(config["persistent_config_path"] == "/router.yaml"
                 and config["persistent_config_sha256"] == observed["config_sha256"]
                 and _positive_integer(config["persistent_config_bytes"])
                 and type(config["debugfs_exitcode"]) is int and config["debugfs_exitcode"] == 0,
                 "persistent and runtime configuration identity differs or is incomplete")
        return {
            "node": node, "host": host, "vmid": vmid, "at": observed["at"],
            "hostname": observed["hostname"], "version": observed["version"],
            "runtime_sha256": observed["config_sha256"], "boot_id": observed["boot_id"],
            "management_address": sorted(management), "media": copy.deepcopy(media),
        }
    except (KeyError, TypeError, AttributeError) as error:
        raise ValueError("incomplete or malformed rollout observation: " + str(error)) from error
