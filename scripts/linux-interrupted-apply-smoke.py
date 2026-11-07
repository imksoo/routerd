#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-3-Clause
"""Interrupt the real ApplyAPI after canonical replacement in a disposable VM."""
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time

if sys.argv[1:] != ["--disposable-vm"] or os.geteuid() != 0:
    raise SystemExit("requires root and --disposable-vm")
subprocess.run(["systemd-detect-virt", "--vm", "--quiet"], check=True)
work = Path("/var/tmp/rd15-reboot")
if (work / "role").read_text().strip() != "setup-router":
    raise SystemExit("requires the dedicated reboot router fixture")
canonical = Path("/usr/local/etc/routerd/router.yaml")
before = canonical.read_bytes()
pid = int(subprocess.check_output(
    ["systemctl", "show", "-p", "MainPID", "--value", "routerd"], text=True))
if pid <= 1 or os.readlink(f"/proc/{pid}/exe") != "/usr/local/sbin/routerd":
    raise SystemExit("cannot verify the current product process")
fd = os.pidfd_open(pid)
candidate = json.loads((work / "candidate.json").read_text())
for resource in candidate["spec"]["resources"]:
    if resource["kind"] == "DNSZone" and resource["metadata"]["name"] == "home":
        resource["spec"]["records"].append(
            {"hostname": "recovery", "ipv4": "198.18.15.1"})
        break
else:
    raise SystemExit("fixture DNS zone missing")
path = work / "interrupted-candidate.json"
path.write_text(json.dumps(candidate))
subprocess.run(["/usr/local/sbin/routerd", "validate", "--config", str(path)], check=True)
with (work / "interrupted-apply-output").open("w") as output:
    child = subprocess.Popen(
        ["/usr/local/sbin/routerctl", "apply", "-f", str(path), "--replace"],
        stdout=output, stderr=subprocess.STDOUT)
    interrupted = False
    try:
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            if canonical.read_bytes() != before:
                # pidfd retains the observed process identity, not a recycled PID.
                # This does not assert whether directory fsync has returned.
                if child.poll() is not None:
                    raise RuntimeError("apply already returned; no in-flight interruption proved")
                signal.pidfd_send_signal(fd, signal.SIGKILL)
                interrupted = True
                break
            if child.poll() is not None:
                raise RuntimeError("apply returned before canonical replacement")
            time.sleep(0.001)
        if not interrupted:
            raise RuntimeError("canonical replacement was not observed")
        result = child.wait(timeout=30)
        if result == 0:
            raise RuntimeError("apply acknowledged success; lost-response scenario not proved")
        print(f"Interrupted product PID {pid} after canonical replacement; apply exit={result}")
        print("Recovery is not yet proven: verify the canonical DNS record and independent traffic after automatic restart.")
    finally:
        os.close(fd)
        if child.poll() is None:
            child.terminate()
            try:
                child.wait(timeout=5)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=5)
