#!/usr/bin/env python3
"""Isolated user-systemd semantics check; never invokes conntrackd or routerd."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile
import uuid


class ObservationError(Exception):
    pass


class ProductFailure(Exception):
    pass


def timestamp():
    return datetime.now(timezone.utc).isoformat()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--evidence-dir", type=pathlib.Path,
                        help="new directory for retained command records (default: /tmp)")
    args = parser.parse_args()
    if args.evidence_dir:
        args.evidence_dir.mkdir(parents=True, exist_ok=False)
        evidence = args.evidence_dir.resolve()
    else:
        evidence = pathlib.Path(tempfile.mkdtemp(prefix="routerd-ct-evidence-"))
    report_path = evidence / "result.json"
    token = "routerd-ct-test-" + uuid.uuid4().hex
    stock, managed = token + "-stock.service", token + "-managed.service"
    vendor = pathlib.Path(os.environ.get("XDG_DATA_HOME", pathlib.Path.home() / ".local/share")) / "systemd/user"
    config = pathlib.Path(os.environ.get("XDG_CONFIG_HOME", pathlib.Path.home() / ".config")) / "systemd/user"
    stock_path, managed_path, mask_path = vendor / stock, config / managed, config / stock
    contents = {
        stock_path: "[Unit]\nDescription=Isolated stock test\n[Service]\nExecStart=/usr/bin/sleep infinity\n",
        managed_path: "[Unit]\nDescription=Isolated managed test\nConflicts=" + stock + "\n[Service]\nExecStart=/usr/bin/sleep infinity\n",
    }
    report = {
        "started_at": timestamp(), "passed": False, "product_outcome": "not_observed",
        "requirement": "A masked stock unit rejects restart without stopping the conflicting managed process.",
        "scope": "Isolated user-systemd units; not routerd entry selection, real conntrackd, HA or packet forwarding.",
        "source": {"path": str(pathlib.Path(__file__).resolve()),
                   "sha256": hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()},
        "units": {str(path): body for path, body in contents.items()},
        "dependencies": {}, "records": [], "cleanup_errors": [],
    }
    owned_paths = []
    manager_touched = False
    mask_attempted = False

    def save():
        temporary = report_path.with_suffix(".tmp")
        temporary.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
        temporary.replace(report_path)

    def run(*command, phase="test"):
        record = {"phase": phase, "args": [systemctl, "--user", *command],
                  "started_at": timestamp(), "rc": None, "stdout": "", "stderr": ""}
        try:
            result = subprocess.run(record["args"], capture_output=True, text=True,
                                    timeout=15, env={**os.environ, "LC_ALL": "C"})
            record.update(rc=result.returncode, stdout=result.stdout, stderr=result.stderr)
        except subprocess.TimeoutExpired as error:
            def decoded(value):
                return value.decode(errors="replace") if isinstance(value, bytes) else value or ""
            record.update(error="watchdog_timeout", stdout=decoded(error.stdout), stderr=decoded(error.stderr))
        except OSError as error:
            record["error"] = str(error)
        record["ended_at"] = timestamp()
        report["records"].append(record)
        save()
        return record

    def successful(record):
        if record["rc"] != 0:
            raise ObservationError("Command did not complete successfully: " + repr(record["args"]))
        return record

    def state(unit, phase="test"):
        record = successful(run("show", unit, "--property=LoadState,ActiveState,MainPID", phase=phase))
        values = dict(line.split("=", 1) for line in record["stdout"].splitlines() if "=" in line)
        if not all(values.get(key) for key in ("LoadState", "ActiveState", "MainPID")) or not values["MainPID"].isdigit():
            raise ObservationError("Incomplete unit observation: " + unit)
        return values

    def require(condition, description):
        if not condition:
            raise ProductFailure(description)

    save()
    print("Evidence: " + str(report_path), file=sys.stderr, flush=True)
    try:
        systemctl = shutil.which("systemctl")
        if not systemctl:
            raise ObservationError("systemctl not found")
        for executable in (systemctl, "/usr/bin/sleep"):
            resolved = pathlib.Path(executable).resolve()
            report["dependencies"][str(resolved)] = hashlib.sha256(resolved.read_bytes()).hexdigest()
        if any(path.exists() or path.is_symlink() for path in (stock_path, managed_path, mask_path)):
            raise ObservationError("Unique unit name collision; no existing files will be changed")
        for path, body in contents.items():
            path.parent.mkdir(parents=True, exist_ok=True)
            with path.open("x") as output:
                owned_paths.append(path)
                output.write(body)
        save()
        manager_touched = True
        successful(run("daemon-reload"))
        successful(run("start", managed))
        initial = state(managed)
        require(initial["ActiveState"] == "active" and int(initial["MainPID"]) > 0, "Managed fixture did not become active")
        successful(run("restart", stock))
        stopped = state(managed)
        require(stopped["ActiveState"] == "inactive" and stopped["MainPID"] == "0", "Unprotected stock restart did not stop managed fixture")
        mask_attempted = True
        successful(run("mask", "--now", stock))
        masked = state(stock)
        require(masked["LoadState"] == "masked" and masked["ActiveState"] == "inactive", "Stock fixture was not masked and inactive")
        successful(run("start", managed))
        before = state(managed)
        require(before["ActiveState"] == "active" and int(before["MainPID"]) > 0, "Protected managed fixture did not become active")
        rejected = run("restart", stock)
        if rejected["rc"] == 0:
            raise ProductFailure("Masked stock restart unexpectedly succeeded")
        if rejected["rc"] is None or not re.search(r"Unit " + re.escape(stock) + r" is masked\b", rejected["stderr"]):
            raise ObservationError("Restart failed without attributable mask rejection")
        after, still_masked = state(managed), state(stock)
        require(still_masked["LoadState"] == "masked" and still_masked["ActiveState"] == "inactive", "Stock mask no longer effective")
        require(after["ActiveState"] == "active" and before["MainPID"] == after["MainPID"], "Masked restart disturbed managed process")
        report.update(product_outcome="pass", managed_pid_before=before["MainPID"], managed_pid_after=after["MainPID"])
    except ProductFailure as error:
        report.update(product_outcome="fail", error=str(error))
    except Exception as error:
        report.update(product_outcome="observation_error", error=repr(error))
    finally:
        # Preserve every failed attempt before removing invocation-owned files.
        save()
        can_remove = True
        if manager_touched:
            if run("stop", managed, stock, phase="cleanup")["rc"] != 0:
                report["cleanup_errors"].append("stop command failed")
            for unit in (managed, stock):
                try:
                    stopped = state(unit, phase="cleanup")
                    if stopped["ActiveState"] != "inactive" or stopped["MainPID"] != "0":
                        raise ObservationError("Unit not confirmed stopped: " + unit)
                except Exception as error:
                    report["cleanup_errors"].append(str(error))
                    can_remove = False
            if mask_attempted and run("unmask", stock, phase="cleanup")["rc"] != 0:
                report["cleanup_errors"].append("unmask command failed")
        if can_remove:
            for path in owned_paths:
                try:
                    path.unlink()
                except OSError as error:
                    report["cleanup_errors"].append(str(error))
            if manager_touched and run("daemon-reload", phase="cleanup")["rc"] != 0:
                report["cleanup_errors"].append("daemon-reload command failed")
        report["remaining_paths"] = [str(path) for path in (*owned_paths, mask_path)
                                     if path.exists() or path.is_symlink()]
        if report["remaining_paths"]:
            report["cleanup_errors"].append("Invocation-owned paths remain; inspect evidence before manual cleanup")
        report["cleanup_outcome"] = "fail" if report["cleanup_errors"] else "pass"
        report["passed"] = report["product_outcome"] == "pass" and not report["cleanup_errors"]
        report["ended_at"] = timestamp()
        save()
        print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0 if report["passed"] else 1 if report["product_outcome"] == "fail" and not report["cleanup_errors"] else 2


if __name__ == "__main__":
    sys.exit(main())
