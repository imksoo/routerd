import argparse
import fcntl
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

from test_lifecycle_supervisor import lifecycle, ROOT


def private_args(root):
    heartbeat = root / "heartbeat"
    heartbeat.touch()
    return argparse.Namespace(
        run_id="run-1", state=root / "state.json", heartbeat=heartbeat,
        ttl_seconds=60, stale_seconds=10, term_grace_seconds=1,
        kill_grace_seconds=1, precheck_command=["/bin/true"],
        mutation_command=["/bin/true"], cleanup_command=["/bin/true"],
        inventory_command=["/bin/true"], post_zero_command=["/bin/true"],
        cleanup_timeout_seconds=2, inventory_timeout_seconds=2,
        max_cleanup_attempts=2, max_paid_lifecycle_seconds=68,
        source_input_tamper_detected=False,
    )


class SupervisorProcessOwnershipTests(unittest.TestCase):
    def test_competing_initializer_cannot_write_owner_state(self):
        with tempfile.TemporaryDirectory() as directory:
            args = private_args(Path(directory))
            owner = lifecycle.Supervisor(args)
            self.addCleanup(owner.close_lock)
            before = args.state.read_bytes()
            with mock.patch.object(lifecycle.Supervisor, "load_or_new", side_effect=AssertionError("read before lock")):
                with self.assertRaisesRegex(lifecycle.SupervisorError, "another durable supervisor"):
                    lifecycle.Supervisor(args)
            self.assertEqual(args.state.read_bytes(), before)

    def test_competing_launcher_does_not_rebuild_working_auth(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            run_root = base / "run-1"
            runtime = run_root / "runtime"
            lifecycle_dir = runtime / "evidence/lifecycle"
            lifecycle_dir.mkdir(parents=True)
            script = base / "start.sh"
            script.write_text((ROOT / "drivers/start-supervised-release-qa.sh").read_text().replace(
                "/var/lib/routerd-release-qa", str(base)))
            script.chmod(0o700)
            auth = runtime / "provider-state/azure/owner-state"
            auth.parent.mkdir(parents=True)
            auth.write_text("active owner authentication")
            state = lifecycle_dir / "supervisor-state.json"
            with state.with_suffix(".json.lock").open("a+") as lock:
                fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                result = subprocess.run([str(script), str(runtime / "contract.json")],
                                        stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=3)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("another durable supervisor", result.stderr)
            self.assertEqual(auth.read_text(), "active owner authentication")
            self.assertFalse(state.exists())

    def test_failed_start_receipt_never_executes_child_or_relaunches_on_restart(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            args = private_args(root)
            marker = root / "mutation-effect"
            args.mutation_command = [sys.executable, "-c", f"from pathlib import Path; Path({str(marker)!r}).touch()"]
            owner = lifecycle.Supervisor(args)
            owner.state["mutationCommandExecuted"] = True
            owner.transition("STARTING")
            atomic_json = lifecycle.atomic_json

            def fail_receipt(path, state):
                if state["phase"] == "MUTATING":
                    raise OSError("private fsync failure")
                atomic_json(path, state)

            try:
                with mock.patch.object(lifecycle, "atomic_json", side_effect=fail_receipt):
                    with self.assertRaises(OSError):
                        owner.launch_mutation()
                self.assertIsNotNone(owner.child)
                self.assertNotEqual(owner.child.wait(timeout=3), 0)
                self.assertFalse(marker.exists())
                self.assertEqual(json.loads(args.state.read_text())["phase"], "STARTING")
            finally:
                owner.close_lock()
            restarted = lifecycle.Supervisor(args)
            self.assertEqual(restarted.run(), 1)
            self.assertFalse(marker.exists())
            self.assertEqual(json.loads(args.state.read_text())["phase"], "FAILED")

    def test_replaced_or_unknown_group_identity_cannot_signal_a_foreign_process(self):
        for missing_boot in (False, True):
            with self.subTest(missing_boot=missing_boot), tempfile.TemporaryDirectory() as directory:
                args = private_args(Path(directory))
                owner = lifecycle.Supervisor(args)
                process = subprocess.Popen(["/bin/sleep", "30"], start_new_session=True)
                try:
                    identity = lifecycle.observe_process(process.pid)
                    owner.state.update(
                        mutationPgid=identity.pgid, mutationPid=identity.pid,
                        mutationStarted=identity.started + (0 if missing_boot else 1),
                        mutationBootId=None if missing_boot else owner.boot_id(),
                    )
                    lifecycle.atomic_json(args.state, owner.state)
                    with mock.patch.object(lifecycle.signal, "pidfd_send_signal", wraps=signal.pidfd_send_signal) as send:
                        with self.assertRaises(lifecycle.SupervisorError):
                            owner.quiesce()
                        send.assert_not_called()
                    self.assertIsNone(process.poll())
                    self.assertEqual(json.loads(args.state.read_text())["mutationPgid"], identity.pgid)
                finally:
                    process.terminate()
                    process.wait(timeout=3)
                    owner.close_lock()

    def test_malformed_group_identity_is_not_observed_absence(self):
        for value in ("123", True, -1, 0, "missing"):
            with self.subTest(value=value), tempfile.TemporaryDirectory() as directory:
                args = private_args(Path(directory))
                owner = lifecycle.Supervisor(args)
                try:
                    if value == "missing":
                        del owner.state["mutationPgid"]
                    else:
                        owner.state["mutationPgid"] = value
                    lifecycle.atomic_json(args.state, owner.state)
                    before = args.state.read_bytes()
                    with mock.patch.object(lifecycle.os, "killpg", side_effect=AssertionError("unbound process group")):
                        with self.assertRaises(lifecycle.SupervisorError):
                            owner.quiesce()
                    self.assertEqual(args.state.read_bytes(), before)
                finally:
                    owner.close_lock()

    def test_actual_mutation_boundary_requires_current_owner_and_session(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            args = private_args(root)
            admitted = root / "admitted"
            verify = [sys.executable, str(ROOT / "lifecycle_supervisor.py"), "--verify-mutation", str(args.state), args.run_id]
            args.mutation_command = [sys.executable, "-c", (
                "import subprocess; from pathlib import Path; "
                f"subprocess.run({verify!r},check=True); Path({str(admitted)!r}).touch()"
            )]
            self.assertEqual(lifecycle.Supervisor(args).run(), 0)
            self.assertTrue(admitted.exists())
            foreign = subprocess.run(verify, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=3)
            self.assertNotEqual(foreign.returncode, 0)

    def test_precheck_timeout_stops_its_actual_child_before_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            args = private_args(root)
            args.inventory_timeout_seconds = 1
            args.precheck_command = [sys.executable, "-c", "import time; time.sleep(30)"]
            owner = lifecycle.Supervisor(args)
            self.assertEqual(owner.run(), 1)
            state = json.loads(args.state.read_text())
            self.assertEqual(state["precheckError"], "TimeoutExpired")
            self.assertIsNone(state["mutationPgid"])
            self.assertEqual(state["phase"], "FAILED")

    def test_restart_stops_admitted_child_before_rejecting_corrupt_recovery_inputs(self):
        with tempfile.TemporaryDirectory() as directory:
            args = private_args(Path(directory))
            args.mutation_command = [sys.executable, "-c", "import time; time.sleep(30)"]
            owner = lifecycle.Supervisor(args)
            try:
                owner.state["mutationCommandExecuted"] = True
                owner.transition("STARTING")
                owner.launch_mutation()
                owner.state["inputs"] = {"invalid": {}}
                lifecycle.atomic_json(args.state, owner.state)
                owner.close_lock()
                with self.assertRaisesRegex(lifecycle.SupervisorError, "incomplete runtime input"):
                    lifecycle.Supervisor(args)
                self.assertIsNone(json.loads(args.state.read_text())["mutationPgid"])
                self.assertNotEqual(owner.child.wait(timeout=3), 0)
            finally:
                owner.close_lock()
                if owner.child is not None and owner.child.poll() is None:
                    owner.child.terminate()
                    owner.child.wait(timeout=3)


if __name__ == "__main__":
    unittest.main()
