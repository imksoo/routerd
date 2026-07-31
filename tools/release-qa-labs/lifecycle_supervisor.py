#!/usr/bin/env python3
"""Durable, fail-closed supervisor for a paid release-QA mutation lifecycle."""

from __future__ import annotations

import argparse
from datetime import datetime, timedelta, timezone
import hashlib
import json
import fcntl
import os
from pathlib import Path
import signal
import shutil
import subprocess
import sys
import time
from typing import Any


PAID_MODE = "production"
STAGING_MODE = "staging-no-mutation"
MODES = {PAID_MODE, STAGING_MODE}
PHASES = (
    "PRECHECK", "STAGING_ARMED", "MUTATING", "STOPPING", "CLEANING", "VERIFYING_ZERO",
    "DONE", "STAGING_DONE", "FAILED",
)
TERMINAL = {"DONE", "STAGING_DONE", "FAILED"}


class SupervisorError(RuntimeError):
    pass


def now_utc() -> datetime:
    return datetime.now(timezone.utc)


def rfc3339(value: datetime) -> str:
    return value.isoformat(timespec="seconds").replace("+00:00", "Z")


def parse_time(value: str) -> datetime:
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def atomic_json(path: Path, data: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_name(f".{path.name}.{os.getpid()}.tmp")
    tmp.write_text(json.dumps(data, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    os.chmod(tmp, 0o600)
    os.replace(tmp, path)


class Supervisor:
    def __init__(self, args: argparse.Namespace) -> None:
        self.args = args
        self.state_path: Path = args.state
        self.stop_reason: str | None = None
        self.child: subprocess.Popen[bytes] | None = None
        self.lock_file: Any = None
        self.state = self.load_or_new()
        self.configure_pinned_environment()

    @staticmethod
    def digest(path: Path) -> str:
        return hashlib.sha256(path.read_bytes()).hexdigest()

    @staticmethod
    def require_mode_600(path: Path) -> None:
        stat = path.stat()
        if not path.is_file() or (stat.st_mode & 0o777) != 0o600:
            raise SupervisorError(f"runtime input must have mode 0600: {path}")
        if stat.st_uid != os.geteuid() or not os.access(path, os.R_OK):
            raise SupervisorError(f"runtime input must be owned and readable by the service UID: {path}")

    def configured_inputs(self) -> list[tuple[str, Path]]:
        values = []
        for name, attribute in (
            ("contract", "contract"), ("runEnv", "run_env"), ("tfvars", "tfvars"),
            ("pveSshPrivateKey", "pve_ssh_private_key"),
        ):
            value = getattr(self.args, attribute, None)
            if value is not None:
                values.append((name, Path(value).resolve()))
        return values

    def validate_run_root(self) -> Path | None:
        configured = getattr(self.args, "run_root", None)
        if configured is None:
            return None
        root = Path(configured).resolve()
        expected = Path("/var/lib/routerd-release-qa") / self.args.run_id
        if root != expected:
            raise SupervisorError(f"run root must be exactly {expected}")
        if self.state_path.resolve() != root / "runtime/evidence/lifecycle/supervisor-state.json":
            raise SupervisorError("durable state path is outside the canonical run root")
        if self.args.heartbeat.resolve() != root / "runtime/evidence/lifecycle/heartbeat":
            raise SupervisorError("heartbeat path is outside the canonical run root")
        expected_inputs = {
            "contract": root / "runtime/contract.json",
            "runEnv": root / "runtime/run.env.json",
            "tfvars": root / "runtime/terraform.tfvars",
            "pveSshPrivateKey": root / "runtime/secrets/pve_ssh",
        }
        configured_inputs = dict(self.configured_inputs())
        secrets = root / "runtime/secrets"
        if not secrets.is_dir() or (secrets.stat().st_mode & 0o777) != 0o700 or secrets.stat().st_uid != os.geteuid():
            raise SupervisorError("runtime secrets directory must be owned by the service UID with mode 0700")
        if set(configured_inputs) != set(expected_inputs):
            raise SupervisorError("all canonical runtime inputs are required")
        for name, path in configured_inputs.items():
            if path != expected_inputs[name]:
                raise SupervisorError(f"runtime input is not canonical: {path}")
        expected_commands = {
            "precheck_command": "precheck-driver.sh",
            "mutation_command": "mutation-driver.sh",
            "cleanup_command": "supervisor-cleanup.sh",
            "inventory_command": "supervisor-inventory.sh",
        }
        for attribute, filename in expected_commands.items():
            command = getattr(self.args, attribute, None)
            if not command or Path(command[0]).resolve() != root / "repo/tools/release-qa-labs/drivers" / filename:
                raise SupervisorError(f"{attribute} is not the canonical tracked wrapper")
        return root

    def create_pins(self, root: Path) -> dict[str, Any]:
        pinned_root = root / "runtime/pinned"
        pinned_root.mkdir(parents=True, exist_ok=True, mode=0o700)
        os.chmod(pinned_root, 0o700)
        result: dict[str, Any] = {}
        names = {
            "contract": "contract.json", "runEnv": "run.env.json", "tfvars": "terraform.tfvars",
            "pveSshPrivateKey": "pve_ssh",
        }
        for name, source in self.configured_inputs():
            self.require_mode_600(source)
            target = pinned_root / names[name]
            shutil.copyfile(source, target)
            os.chmod(target, 0o600)
            result[name] = {
                "source": str(source),
                "pinned": str(target),
                "sha256": self.digest(target),
            }
        return result

    def verify_pins(self) -> bool:
        pins = self.state.get("inputs")
        if not isinstance(pins, dict):
            return not self.configured_inputs()
        source_tampered = False
        expected_names = {
            "contract": "contract.json", "runEnv": "run.env.json", "tfvars": "terraform.tfvars",
            "pveSshPrivateKey": "pve_ssh",
        }
        run_root = Path(self.state["runRoot"]).resolve() if "runRoot" in self.state else None
        if set(pins) != set(expected_names):
            raise SupervisorError("durable state has an incomplete runtime input set")
        effective = self.state.get("effectiveLifecycle")
        if not isinstance(effective, dict) or effective.get("contractSha256") != pins["contract"].get("sha256"):
            raise SupervisorError("durable lifecycle values are not bound to the pinned contract")
        if (
            effective.get("executionMode") != self.state.get("executionMode")
            or self.state.get("executionMode") not in MODES
        ):
            raise SupervisorError("durable execution mode is not bound to the pinned contract")
        pinned_contract = json.loads(Path(pins["contract"]["pinned"]).read_text(encoding="utf-8"))
        if require_execution_mode(pinned_contract) != self.state.get("executionMode"):
            raise SupervisorError("durable execution mode differs from the pinned contract")
        for name, item in pins.items():
            pinned = Path(item["pinned"]).resolve()
            source = Path(item["source"]).resolve()
            expected = item["sha256"]
            if run_root is not None and pinned != run_root / "runtime/pinned" / expected_names[name]:
                raise SupervisorError("pinned runtime path escaped canonical run root")
            self.require_mode_600(pinned)
            if self.digest(pinned) != expected:
                raise SupervisorError(f"pinned runtime input was modified: {pinned}")
            if not source.is_file() or self.digest(source) != expected:
                source_tampered = True
        return not source_tampered

    def configure_pinned_environment(self) -> None:
        pins = self.state.get("inputs", {})
        if not pins:
            return
        os.environ["ROUTERD_RELEASE_QA_PINNED_CONTRACT"] = pins["contract"]["pinned"]
        os.environ["ROUTERD_RELEASE_QA_PINNED_RUN_ENV"] = pins["runEnv"]["pinned"]
        os.environ["ROUTERD_RELEASE_QA_PINNED_TFVARS"] = pins["tfvars"]["pinned"]
        os.environ["ROUTERD_RELEASE_QA_PINNED_PVE_SSH_PRIVATE_KEY"] = pins["pveSshPrivateKey"]["pinned"]

    @staticmethod
    def duration_seconds(value: Any) -> int:
        if not isinstance(value, str) or len(value) < 2 or not value[:-1].isdigit() or value[-1] not in "smhd":
            raise SupervisorError(f"invalid lifecycle duration: {value}")
        return int(value[:-1]) * {"s": 1, "m": 60, "h": 3600, "d": 86400}[value[-1]]

    def effective_lifecycle(self, pins: dict[str, Any]) -> dict[str, Any]:
        contract = json.loads(Path(pins["contract"]["pinned"]).read_text(encoding="utf-8"))
        lifecycle = contract.get("lifecycle")
        if not isinstance(lifecycle, dict):
            raise SupervisorError("contract lifecycle must be an object")
        effective = {
            "ttlSeconds": self.duration_seconds(lifecycle.get("ttl")),
            "staleSeconds": self.duration_seconds(lifecycle.get("heartbeatStale")),
            "cleanupTimeoutSeconds": self.duration_seconds(lifecycle.get("cleanupTimeout")),
            "inventoryTimeoutSeconds": self.duration_seconds(lifecycle.get("inventoryTimeout")),
            "plannedCleanupAttempts": lifecycle.get("maxCleanupAttempts"),
            "plannedPaidLifecycleSeconds": lifecycle.get("maxPaidLifecycleSeconds"),
            "contractSha256": pins["contract"]["sha256"],
            "executionMode": require_execution_mode(contract),
        }
        attempts = effective["plannedCleanupAttempts"]
        paid = effective["plannedPaidLifecycleSeconds"]
        if not isinstance(attempts, int) or not isinstance(paid, int):
            raise SupervisorError("contract lifecycle integer fields are invalid")
        worst = effective["ttlSeconds"] + attempts * (
            effective["cleanupTimeoutSeconds"] + effective["inventoryTimeoutSeconds"]
        )
        if (
            not 0 < effective["ttlSeconds"] <= 2700
            or not 0 < effective["staleSeconds"] < effective["ttlSeconds"]
            or not 0 < effective["cleanupTimeoutSeconds"] <= 600
            or not 0 < effective["inventoryTimeoutSeconds"] <= 300
            or not 0 < attempts <= 2
            or not 0 < paid <= 4500
            or worst > paid
        ):
            raise SupervisorError("contract lifecycle exceeds policy")
        return effective

    @staticmethod
    def boot_id() -> str:
        try:
            return Path("/proc/sys/kernel/random/boot_id").read_text(encoding="utf-8").strip()
        except OSError:
            return "unknown"

    def acquire_lock(self) -> None:
        lock_path = self.state_path.with_suffix(self.state_path.suffix + ".lock")
        lock_path.parent.mkdir(parents=True, exist_ok=True)
        self.lock_file = lock_path.open("a+", encoding="utf-8")
        try:
            fcntl.flock(self.lock_file.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as exc:
            raise SupervisorError("another durable supervisor owns this run") from exc

    def load_or_new(self) -> dict[str, Any]:
        root = self.validate_run_root()
        if self.state_path.exists():
            self.require_mode_600(self.state_path)
            data = json.loads(self.state_path.read_text(encoding="utf-8"))
            if data.get("runId") != self.args.run_id or data.get("phase") not in PHASES:
                raise SupervisorError("durable state does not match the requested run")
            if data.get("executionMode") not in MODES:
                raise SupervisorError("durable execution mode is invalid")
            if data.get("effectiveLifecycle", {}).get("executionMode") != data.get("executionMode"):
                raise SupervisorError("durable execution mode is not bound to lifecycle state")
            self.state = data
            clean_sources = self.verify_pins()
            if not clean_sources:
                data["sourceInputTamperDetected"] = True
                atomic_json(self.state_path, data)
            return data
        started = now_utc()
        if root is None:
            pins = None
            effective = {
                "ttlSeconds": self.args.ttl_seconds,
                "staleSeconds": self.args.stale_seconds,
                "cleanupTimeoutSeconds": getattr(self.args, "cleanup_timeout_seconds", 600),
                "inventoryTimeoutSeconds": getattr(self.args, "inventory_timeout_seconds", 300),
                "plannedCleanupAttempts": getattr(self.args, "max_cleanup_attempts", 2),
                "plannedPaidLifecycleSeconds": getattr(self.args, "max_paid_lifecycle_seconds", 4500),
                "contractSha256": None,
                "executionMode": PAID_MODE,
            }
        else:
            pins = self.create_pins(root)
            effective = self.effective_lifecycle(pins)
        data = {
            "runId": self.args.run_id,
            "phase": "PRECHECK",
            "startedAt": rfc3339(started),
            "deadline": rfc3339(started + timedelta(seconds=effective["ttlSeconds"])),
            "plannedPaidDeadline": rfc3339(started + timedelta(seconds=effective["plannedPaidLifecycleSeconds"])),
            "effectiveLifecycle": effective,
            "executionHost": os.uname().nodename,
            "bootId": self.boot_id(),
            "mutationPgid": None,
            "history": [],
            "cleanupAttempts": 0,
            "executionMode": effective["executionMode"],
            "mutationCommandExecuted": False,
        }
        if root is not None:
            data["runRoot"] = str(root)
            data["inputs"] = pins
        atomic_json(self.state_path, data)
        return data

    def transition(self, phase: str, **extra: Any) -> None:
        if phase not in PHASES:
            raise SupervisorError(f"unknown phase: {phase}")
        previous = self.state["phase"]
        self.state["phase"] = phase
        self.state.update(extra)
        self.state["history"].append({"from": previous, "to": phase, "at": rfc3339(now_utc())})
        atomic_json(self.state_path, self.state)

    def signal_handler(self, signum: int, _frame: Any) -> None:
        self.stop_reason = signal.Signals(signum).name

    def run_checked(self, command: list[str], label: str) -> None:
        proc = subprocess.run(command, check=False)
        if proc.returncode:
            raise SupervisorError(f"{label} failed with exit {proc.returncode}")

    def pgid_alive(self, pgid: int) -> bool:
        try:
            os.killpg(pgid, 0)
            return True
        except ProcessLookupError:
            return False
        except PermissionError as exc:
            raise SupervisorError(f"cannot inspect mutation process group {pgid}") from exc

    def quiesce(self) -> None:
        pgid = self.state.get("mutationPgid")
        if self.state.get("mutationBootId") not in (None, self.boot_id()):
            self.state["mutationPgid"] = None
            atomic_json(self.state_path, self.state)
            return
        if not isinstance(pgid, int) or not self.pgid_alive(pgid):
            self.state["mutationPgid"] = None
            atomic_json(self.state_path, self.state)
            return
        os.killpg(pgid, signal.SIGTERM)
        deadline = time.monotonic() + self.args.term_grace_seconds
        while self.pgid_alive(pgid) and time.monotonic() < deadline:
            # Reap the process-group leader promptly. An unreaped zombie keeps
            # killpg(..., 0) positive even after every descendant has exited.
            if self.child is not None:
                self.child.poll()
            time.sleep(0.05)
        if self.pgid_alive(pgid):
            os.killpg(pgid, signal.SIGKILL)
        kill_deadline = time.monotonic() + self.args.kill_grace_seconds
        while self.pgid_alive(pgid) and time.monotonic() < kill_deadline:
            if self.child is not None:
                self.child.poll()
            time.sleep(0.05)
        if self.child is not None:
            self.child.poll()
        if self.pgid_alive(pgid):
            raise SupervisorError("mutation process group did not quiesce before cleanup")
        if self.child is not None:
            self.child.wait(timeout=self.args.kill_grace_seconds)
        self.state["mutationPgid"] = None
        atomic_json(self.state_path, self.state)

    def cleanup_and_verify(self, reason: str) -> int:
        pins_clean = self.verify_pins()
        if self.state["phase"] not in {"STOPPING", "CLEANING", "VERIFYING_ZERO"}:
            self.transition("STOPPING", stopReason=reason)
        self.quiesce()
        attempts = int(self.state.get("cleanupAttempts", 0)) + 1
        self.state["cleanupAttempts"] = attempts
        atomic_json(self.state_path, self.state)
        if self.state["phase"] != "CLEANING":
            self.transition("CLEANING")
        cleanup = subprocess.run(
            self.args.cleanup_command,
            check=False,
            timeout=self.state["effectiveLifecycle"]["cleanupTimeoutSeconds"],
        )
        self.transition("VERIFYING_ZERO", cleanupExit=cleanup.returncode)
        inventory = subprocess.run(
            self.args.inventory_command,
            check=False,
            timeout=self.state["effectiveLifecycle"]["inventoryTimeoutSeconds"],
        )
        mutation_succeeded = (
            reason == "mutation-complete"
            and self.state.get("mutationExit") == 0
            and pins_clean
            and not self.state.get("sourceInputTamperDetected", False)
        )
        staging_succeeded = (
            self.state["executionMode"] == STAGING_MODE
            and pins_clean
            and not self.state.get("sourceInputTamperDetected", False)
            and not self.state.get("mutationCommandExecuted", False)
            and self.state.get("stagingRestartRequired") is True
            and reason == "supervisor-restart"
            and any(item.get("to") == "STAGING_ARMED" for item in self.state.get("history", []))
        )
        if cleanup.returncode == 0 and inventory.returncode == 0 and staging_succeeded:
            self.transition(
                "STAGING_DONE", inventoryExit=0, finishedAt=rfc3339(now_utc()),
                result={
                    "status": "pass", "kind": STAGING_MODE,
                    "paidQualification": "not-run", "mutationExecuted": False,
                },
            )
            return 0
        if cleanup.returncode == 0 and inventory.returncode == 0 and mutation_succeeded:
            self.transition("DONE", inventoryExit=0, finishedAt=rfc3339(now_utc()))
            return 0
        if cleanup.returncode == 0 and inventory.returncode == 0:
            self.transition("FAILED", inventoryExit=0, finishedAt=rfc3339(now_utc()))
            return 1
        # Resource recovery is never capped by the planned paid envelope.
        # Preserve a restartable phase so systemd retries with backoff until
        # cleanup succeeds and authoritative inventory is zero.
        self.state["inventoryExit"] = inventory.returncode
        atomic_json(self.state_path, self.state)
        return 2

    def run(self) -> int:
        self.acquire_lock()
        try:
            return self.run_locked()
        finally:
            if self.lock_file is not None:
                self.lock_file.close()
                self.lock_file = None

    def run_locked(self) -> int:
        for sig in (signal.SIGINT, signal.SIGTERM):
            signal.signal(sig, self.signal_handler)
        phase = self.state["phase"]
        if phase in TERMINAL:
            return 0 if phase in {"DONE", "STAGING_DONE"} else 1
        # Any supervisor restart after mutation began fails closed into cleanup.
        if phase != "PRECHECK":
            return self.cleanup_and_verify("supervisor-restart")
        self.run_checked(self.args.precheck_command, "precheck")
        if self.state["executionMode"] == STAGING_MODE:
            if self.stop_reason:
                return self.cleanup_and_verify(self.stop_reason)
            # Deliberately cross one service-manager restart boundary. This is
            # deterministic and happens before any mutation process exists.
            self.transition("STAGING_ARMED", stagingRestartRequired=True)
            return 2
        if now_utc() >= parse_time(self.state["deadline"]):
            return self.cleanup_and_verify("absolute-deadline-before-mutation")
        self.state["mutationCommandExecuted"] = True
        atomic_json(self.state_path, self.state)
        self.child = subprocess.Popen(self.args.mutation_command, start_new_session=True)
        pgid = os.getpgid(self.child.pid)
        self.transition("MUTATING", mutationPgid=pgid, mutationBootId=self.boot_id())
        reason = "mutation-complete"
        mutation_exit: int | None = None
        while mutation_exit is None:
            mutation_exit = self.child.poll()
            if self.stop_reason:
                reason = self.stop_reason
                break
            if now_utc() >= parse_time(self.state["deadline"]):
                reason = "absolute-deadline"
                break
            try:
                heartbeat_age = time.time() - self.args.heartbeat.stat().st_mtime
            except FileNotFoundError:
                heartbeat_age = time.time() - parse_time(self.state["startedAt"]).timestamp()
            if heartbeat_age >= self.state["effectiveLifecycle"]["staleSeconds"]:
                reason = "heartbeat-stale"
                break
            time.sleep(0.1)
        self.state["mutationExit"] = mutation_exit
        atomic_json(self.state_path, self.state)
        result = self.cleanup_and_verify(reason)
        if mutation_exit not in (None, 0):
            return 1
        return result


def command_values(values: list[str] | None, name: str) -> list[str]:
    if not values:
        raise SupervisorError(f"{name} is required")
    return values


def require_execution_mode(contract: dict[str, Any]) -> str:
    execution = contract.get("execution")
    mode = execution.get("mode") if isinstance(execution, dict) else None
    if mode not in MODES:
        raise SupervisorError("contract execution mode is missing or invalid")
    return mode


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--run-id", required=True)
    parser.add_argument("--state", type=Path, required=True)
    parser.add_argument("--heartbeat", type=Path, required=True)
    parser.add_argument("--run-root", type=Path)
    parser.add_argument("--contract", type=Path)
    parser.add_argument("--run-env", type=Path)
    parser.add_argument("--tfvars", type=Path)
    parser.add_argument("--pve-ssh-private-key", type=Path)
    parser.add_argument("--ttl-seconds", type=int, default=2700)
    parser.add_argument("--stale-seconds", type=int, default=300)
    parser.add_argument("--term-grace-seconds", type=float, default=10)
    parser.add_argument("--kill-grace-seconds", type=float, default=5)
    parser.add_argument("--cleanup-timeout-seconds", type=float, default=600)
    parser.add_argument("--inventory-timeout-seconds", type=float, default=300)
    parser.add_argument("--max-cleanup-attempts", type=int, default=2)
    parser.add_argument("--max-paid-lifecycle-seconds", type=int, default=4500)
    parser.add_argument("--precheck-command", nargs="+")
    parser.add_argument("--mutation-command", nargs="+")
    parser.add_argument("--cleanup-command", nargs="+")
    parser.add_argument("--inventory-command", nargs="+")
    args = parser.parse_args(argv)
    if args.ttl_seconds <= 0 or args.ttl_seconds > 2700:
        raise SupervisorError("TTL must be in 1..2700 seconds")
    if args.stale_seconds <= 0 or args.stale_seconds >= args.ttl_seconds:
        raise SupervisorError("stale threshold must be positive and less than TTL")
    if args.max_cleanup_attempts <= 0 or args.max_cleanup_attempts > 2:
        raise SupervisorError("cleanup attempts must be in 1..2")
    worst = args.ttl_seconds + args.max_cleanup_attempts * (args.cleanup_timeout_seconds + args.inventory_timeout_seconds)
    if args.max_paid_lifecycle_seconds > 4500 or worst > args.max_paid_lifecycle_seconds:
        raise SupervisorError("paid lifecycle envelope exceeds policy")
    args.precheck_command = command_values(args.precheck_command, "precheck command")
    args.mutation_command = command_values(args.mutation_command, "mutation command")
    args.cleanup_command = command_values(args.cleanup_command, "cleanup command")
    args.inventory_command = command_values(args.inventory_command, "inventory command")
    return Supervisor(args).run()


if __name__ == "__main__":
    try:
        raise SystemExit(main(sys.argv[1:]))
    except (SupervisorError, OSError, subprocess.SubprocessError, json.JSONDecodeError) as exc:
        print(f"release QA supervisor: {exc}", file=sys.stderr)
        raise SystemExit(2)
