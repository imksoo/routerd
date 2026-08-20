import argparse
import importlib.util
import json
from pathlib import Path
import signal
import tempfile
import unittest
from unittest import mock
import os
import sys
import threading
import time


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("lifecycle_supervisor", ROOT / "lifecycle_supervisor.py")
lifecycle = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(lifecycle)


class FakeChild:
    pid = 4321

    def __init__(self, polls):
        self.polls = iter(polls)

    def poll(self):
        return next(self.polls, None)

    def wait(self, timeout=None):
        return 0


class SupervisorTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        root = Path(self.temp.name)
        self.state = root / "state.json"
        self.heartbeat = root / "heartbeat"
        self.heartbeat.touch()
        self.args = argparse.Namespace(
            run_id="run-1",
            state=self.state,
            heartbeat=self.heartbeat,
            ttl_seconds=60,
            stale_seconds=10,
            term_grace_seconds=0.01,
            kill_grace_seconds=0.01,
            precheck_command=["precheck"],
            mutation_command=["mutation"],
            cleanup_command=["cleanup"],
            inventory_command=["inventory"],
            post_zero_command=["post-zero"],
            cleanup_timeout_seconds=600,
            inventory_timeout_seconds=300,
            max_cleanup_attempts=2,
            max_paid_lifecycle_seconds=4500,
            source_input_tamper_detected=False,
        )

    def tearDown(self):
        for key in (
            "ROUTERD_RELEASE_QA_PINNED_CONTRACT", "ROUTERD_RELEASE_QA_PINNED_RUN_ENV",
            "ROUTERD_RELEASE_QA_PINNED_TFVARS", "ROUTERD_RELEASE_QA_PINNED_PVE_SSH_PRIVATE_KEY",
            "ROUTERD_RELEASE_QA_PINNED_GUEST_SSH_PRIVATE_KEY",
            "ROUTERD_RELEASE_QA_PINNED_PVE_SSH_KNOWN_HOSTS",
            "ROUTERD_RELEASE_QA_PINNED_PVE_TOKEN_TFVARS", "ROUTERD_RELEASE_QA_PINNED_PVE_CA_PEM",
        ):
            os.environ.pop(key, None)
        self.temp.cleanup()

    def phases(self):
        return [item["to"] for item in json.loads(self.state.read_text())["history"]]

    def run_with_commands(self, mutation_exit=0, cleanup_exit=0, inventory_exit=0,
                          post_zero_exit=0, precheck_exit=0, stop_reason=None):
        events = []
        child = FakeChild([mutation_exit])

        def popen(command, start_new_session=False):
            events.append("mutation-start")
            self.assertTrue(start_new_session)
            return child

        def run(command, check=False, timeout=None):
            label = command[0]
            events.append(label)
            return mock.Mock(returncode={
                "precheck": precheck_exit,
                "cleanup": cleanup_exit,
                "inventory": inventory_exit,
                "post-zero": post_zero_exit,
            }[label])

        supervisor = lifecycle.Supervisor(self.args)
        supervisor.stop_reason = stop_reason
        with mock.patch.object(lifecycle.subprocess, "Popen", side_effect=popen), \
             mock.patch.object(lifecycle.subprocess, "run", side_effect=run), \
             mock.patch.object(lifecycle.os, "getpgid", return_value=4321), \
             mock.patch.object(supervisor, "pgid_alive", return_value=False):
            rc = supervisor.run()
        return rc, events

    def test_staging_clean_service_restart_never_mutates_and_has_distinct_success(self):
        supervisor = lifecycle.Supervisor(self.args)
        supervisor.state["executionMode"] = lifecycle.STAGING_MODE
        supervisor.state["effectiveLifecycle"]["executionMode"] = lifecycle.STAGING_MODE
        lifecycle.atomic_json(self.state, supervisor.state)
        events = []

        def run(command, check=False, timeout=None):
            events.append(command[0])
            return mock.Mock(returncode=0)

        with mock.patch.object(lifecycle.subprocess, "run", side_effect=run), \
             mock.patch.object(lifecycle.subprocess, "Popen") as popen:
            self.assertEqual(supervisor.run(), 2)
            self.assertEqual(json.loads(self.state.read_text())["phase"], "STAGING_ARMED")
            self.assertEqual(lifecycle.Supervisor(self.args).run(), 0)
        popen.assert_not_called()
        self.assertEqual(events, ["precheck", "cleanup", "inventory", "post-zero"])
        final = json.loads(self.state.read_text())
        self.assertEqual(final["phase"], "STAGING_DONE")
        self.assertIn("STAGING_ARMED", [item["to"] for item in final["history"]])
        self.assertEqual(final["stopReason"], "supervisor-restart")
        self.assertEqual(final["executionMode"], lifecycle.STAGING_MODE)
        self.assertFalse(final["mutationCommandExecuted"])
        self.assertEqual(final["result"], {
            "status": "pass", "kind": lifecycle.STAGING_MODE,
            "paidQualification": "not-run", "mutationExecuted": False,
        })

    def test_staging_signal_cleans_without_mutation(self):
        supervisor = lifecycle.Supervisor(self.args)
        supervisor.state["executionMode"] = lifecycle.STAGING_MODE
        supervisor.state["effectiveLifecycle"]["executionMode"] = lifecycle.STAGING_MODE
        lifecycle.atomic_json(self.state, supervisor.state)
        supervisor.signal_handler(signal.SIGTERM, None)
        events = []

        def run(command, check=False, timeout=None):
            events.append(command[0])
            return mock.Mock(returncode=0)

        with mock.patch.object(lifecycle.subprocess, "run", side_effect=run), \
             mock.patch.object(lifecycle.subprocess, "Popen") as popen:
            self.assertEqual(supervisor.run(), 1)
        popen.assert_not_called()
        self.assertEqual(events, ["precheck", "cleanup", "inventory", "post-zero"])
        final = json.loads(self.state.read_text())
        self.assertEqual(final["stopReason"], "SIGTERM")
        self.assertEqual(final["phase"], "FAILED")
        self.assertFalse(any(item["to"] == "STAGING_ARMED" for item in final["history"]))

    def test_staging_cannot_pass_with_forged_armed_marker_without_history(self):
        supervisor = lifecycle.Supervisor(self.args)
        supervisor.state["executionMode"] = lifecycle.STAGING_MODE
        supervisor.state["effectiveLifecycle"]["executionMode"] = lifecycle.STAGING_MODE
        supervisor.state["stagingRestartRequired"] = True
        lifecycle.atomic_json(self.state, supervisor.state)

        def run(command, check=False, timeout=None):
            return mock.Mock(returncode=0)

        with mock.patch.object(lifecycle.subprocess, "run", side_effect=run), \
             mock.patch.object(lifecycle.subprocess, "Popen") as popen:
            self.assertEqual(supervisor.cleanup_and_verify("supervisor-restart"), 1)
        popen.assert_not_called()
        self.assertEqual(json.loads(self.state.read_text())["phase"], "FAILED")

    def test_staging_nonzero_inventory_is_restartable_not_success(self):
        supervisor = lifecycle.Supervisor(self.args)
        supervisor.state["executionMode"] = lifecycle.STAGING_MODE
        supervisor.state["effectiveLifecycle"]["executionMode"] = lifecycle.STAGING_MODE
        lifecycle.atomic_json(self.state, supervisor.state)
        outcomes = iter((1, 0))

        def run(command, check=False, timeout=None):
            return mock.Mock(returncode=next(outcomes) if command[0] == "inventory" else 0)

        with mock.patch.object(lifecycle.subprocess, "run", side_effect=run), \
             mock.patch.object(lifecycle.subprocess, "Popen") as popen:
            self.assertEqual(supervisor.run(), 2)
            self.assertEqual(lifecycle.Supervisor(self.args).run(), 2)
            self.assertEqual(json.loads(self.state.read_text())["phase"], "VERIFYING_ZERO")
            self.assertEqual(lifecycle.Supervisor(self.args).run(), 0)
        popen.assert_not_called()
        final = json.loads(self.state.read_text())
        self.assertEqual(final["phase"], "STAGING_DONE")
        self.assertEqual(final["cleanupAttempts"], 2)

    def test_durable_mode_cannot_change_on_restart(self):
        supervisor = lifecycle.Supervisor(self.args)
        supervisor.state["executionMode"] = lifecycle.STAGING_MODE
        lifecycle.atomic_json(self.state, supervisor.state)
        with self.assertRaisesRegex(lifecycle.SupervisorError, "not bound"):
            lifecycle.Supervisor(self.args)

    def test_success_cleans_and_verifies_before_done(self):
        rc, events = self.run_with_commands()
        self.assertEqual(rc, 0)
        self.assertEqual(events, ["precheck", "mutation-start", "cleanup", "inventory", "post-zero"])
        self.assertEqual(self.phases(), ["MUTATING", "STOPPING", "CLEANING", "VERIFYING_ZERO", "REVOKING_TOKEN", "DONE"])

    def test_post_zero_revocation_retry_never_repeats_cleanup_or_mutation(self):
        rc, events = self.run_with_commands(post_zero_exit=1)
        self.assertEqual(rc, 2)
        self.assertEqual(events, ["precheck", "mutation-start", "cleanup", "inventory", "post-zero"])
        self.assertEqual(json.loads(self.state.read_text())["phase"], "REVOKING_TOKEN")
        retried = []

        def run(command, check=False, timeout=None):
            retried.append(command[0])
            return mock.Mock(returncode=0)

        with mock.patch.object(lifecycle.subprocess, "run", side_effect=run), \
             mock.patch.object(lifecycle.subprocess, "Popen") as popen:
            self.assertEqual(lifecycle.Supervisor(self.args).run(), 0)
        popen.assert_not_called()
        self.assertEqual(retried, ["post-zero"])
        self.assertEqual(json.loads(self.state.read_text())["phase"], "DONE")

    def test_precheck_failure_is_recovered_to_zero_and_revokes_the_run_token(self):
        rc, events = self.run_with_commands(precheck_exit=9)
        self.assertEqual(rc, 1)
        self.assertEqual(events, ["precheck", "cleanup", "inventory", "post-zero"])
        final = json.loads(self.state.read_text())
        self.assertEqual(final["precheckExit"], 9)
        self.assertFalse(final["mutationCommandExecuted"])
        self.assertEqual(final["stopReason"], "precheck-failed")
        self.assertEqual(final["phase"], "FAILED")

    def test_precheck_start_error_is_recovered_to_zero_and_revokes_the_run_token(self):
        events = []

        def run(command, check=False, timeout=None):
            events.append(command[0])
            if command[0] == "precheck":
                raise FileNotFoundError("fixture precheck missing")
            return mock.Mock(returncode=0)

        supervisor = lifecycle.Supervisor(self.args)
        with mock.patch.object(lifecycle.subprocess, "run", side_effect=run), \
             mock.patch.object(lifecycle.subprocess, "Popen") as popen:
            self.assertEqual(supervisor.run(), 1)
        popen.assert_not_called()
        self.assertEqual(events, ["precheck", "cleanup", "inventory", "post-zero"])
        final = json.loads(self.state.read_text())
        self.assertEqual(final["precheckError"], "FileNotFoundError")
        self.assertEqual(final["phase"], "FAILED")

    def test_production_source_tamper_cleans_to_zero_but_fails_qualification(self):
        self.args.source_input_tamper_detected = True
        rc, events = self.run_with_commands()
        self.assertEqual(rc, 1)
        self.assertEqual(events[-3:], ["cleanup", "inventory", "post-zero"])
        final = json.loads(self.state.read_text())
        self.assertEqual(final["phase"], "FAILED")
        self.assertTrue(final["sourceInputTamperDetected"])
        self.assertEqual(final["inventoryExit"], 0)

    def test_mutation_failure_still_cleans_and_returns_failure(self):
        rc, events = self.run_with_commands(mutation_exit=7)
        self.assertEqual(rc, 1)
        self.assertEqual(events[-3:], ["cleanup", "inventory", "post-zero"])
        final = json.loads(self.state.read_text())
        self.assertEqual(final["phase"], "FAILED")
        self.assertEqual(final["inventoryExit"], 0)

    def test_cleanup_or_inventory_failure_never_reaches_done(self):
        for cleanup_exit, inventory_exit in ((1, 0), (0, 1)):
            with self.subTest(cleanup_exit=cleanup_exit, inventory_exit=inventory_exit):
                self.state.unlink(missing_ok=True)
                rc, _ = self.run_with_commands(cleanup_exit=cleanup_exit, inventory_exit=inventory_exit)
                self.assertEqual(rc, 2)
                self.assertEqual(json.loads(self.state.read_text())["phase"], "VERIFYING_ZERO")

    def test_int_and_term_are_recorded_and_cleanup_follows_quiesce(self):
        for caught_signal in (signal.SIGINT, signal.SIGTERM):
            with self.subTest(caught_signal=caught_signal):
                self.state.unlink(missing_ok=True)
                events = []
                supervisor = lifecycle.Supervisor(self.args)
                supervisor.child = FakeChild([None])
                supervisor.transition("MUTATING", mutationPgid=4321)
                supervisor.signal_handler(caught_signal, None)

                def quiesce():
                    events.append("quiesce")
                    supervisor.state["mutationPgid"] = None

                def run(command, check=False, timeout=None):
                    events.append(command[0])
                    return mock.Mock(returncode=0)

                with mock.patch.object(supervisor, "quiesce", side_effect=quiesce), \
                     mock.patch.object(lifecycle.subprocess, "run", side_effect=run):
                    rc = supervisor.cleanup_and_verify(supervisor.stop_reason)
                self.assertEqual(rc, 1)
                self.assertEqual(events, ["quiesce", "cleanup", "inventory", "post-zero"])
                final = json.loads(self.state.read_text())
                self.assertEqual(final["stopReason"], signal.Signals(caught_signal).name)
                self.assertEqual(final["phase"], "FAILED")
                self.assertEqual(final["inventoryExit"], 0)

    def test_restart_from_each_nonterminal_mutating_phase_recovers_to_cleanup(self):
        for phase in ("MUTATING", "STOPPING", "CLEANING", "VERIFYING_ZERO"):
            with self.subTest(phase=phase):
                self.state.unlink(missing_ok=True)
                supervisor = lifecycle.Supervisor(self.args)
                supervisor.transition(phase, mutationPgid=None)
                events = []

                def run(command, check=False, timeout=None):
                    events.append(command[0])
                    return mock.Mock(returncode=0)

                with mock.patch.object(lifecycle.subprocess, "run", side_effect=run):
                    self.assertEqual(lifecycle.Supervisor(self.args).run(), 1)
                self.assertEqual(events, ["cleanup", "inventory", "post-zero"])
                final = json.loads(self.state.read_text())
                self.assertEqual(final["phase"], "FAILED")
                self.assertEqual(final["inventoryExit"], 0)

    def test_stale_heartbeat_and_absolute_timeout_record_reason(self):
        for reason in ("heartbeat-stale", "absolute-deadline"):
            with self.subTest(reason=reason):
                self.state.unlink(missing_ok=True)
                supervisor = lifecycle.Supervisor(self.args)
                supervisor.child = FakeChild([None])
                supervisor.transition("MUTATING", mutationPgid=None)
                with mock.patch.object(supervisor, "cleanup_and_verify", return_value=0) as cleanup:
                    if reason == "heartbeat-stale":
                        self.heartbeat.unlink()
                        supervisor.state["startedAt"] = "2000-01-01T00:00:00Z"
                        supervisor.state["phase"] = "PRECHECK"
                    else:
                        supervisor.state["deadline"] = "2000-01-01T00:00:00Z"
                        supervisor.state["phase"] = "PRECHECK"
                    with mock.patch.object(supervisor, "run_checked"), \
                         mock.patch.object(lifecycle.subprocess, "Popen", return_value=supervisor.child), \
                         mock.patch.object(lifecycle.os, "getpgid", return_value=4321):
                        supervisor.run()
                    cleanup.assert_called_once()
                    expected = "absolute-deadline-before-mutation" if reason == "absolute-deadline" else reason
                    self.assertIn(expected, cleanup.call_args.args)

    def test_cleanup_timeout_leaves_durable_cleaning_phase_for_restart(self):
        supervisor = lifecycle.Supervisor(self.args)
        supervisor.transition("MUTATING", mutationPgid=None)
        with mock.patch.object(
            lifecycle.subprocess,
            "run",
            side_effect=lifecycle.subprocess.TimeoutExpired(["cleanup"], 900),
        ):
            with self.assertRaises(lifecycle.subprocess.TimeoutExpired):
                supervisor.cleanup_and_verify("supervisor-restart")
        durable = json.loads(self.state.read_text())
        self.assertEqual(durable["phase"], "CLEANING")

    def test_noncanonical_run_root_and_state_path_are_rejected(self):
        self.args.run_root = Path(self.temp.name)
        with self.assertRaisesRegex(lifecycle.SupervisorError, "run root must be exactly"):
            lifecycle.Supervisor(self.args)

    def test_restart_detects_source_tamper_and_cleanup_uses_pinned_inputs(self):
        run_root = Path(self.temp.name) / "canonical-fixture"
        inputs = {}
        for attribute, name in (
            ("contract", "contract.json"), ("run_env", "run.env.json"),
            ("tfvars", "terraform.tfvars"), ("pve_ssh_private_key", "pve_ssh"),
            ("guest_ssh_private_key", "guest_ssh"),
            ("pve_ssh_known_hosts", "pve-known_hosts"),
            ("pve_token_tfvars", "pve-token.tfvars"),
            ("pve_ca_pem", "pve-ca.pem"),
        ):
            path = run_root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            content = name
            if attribute == "contract":
                content = json.dumps({"execution": {"mode": lifecycle.PAID_MODE}, "lifecycle": {
                    "ttl": "30m", "heartbeatStale": "2m", "cleanupTimeout": "4m",
                    "inventoryTimeout": "1m", "maxCleanupAttempts": 2,
                    "maxPaidLifecycleSeconds": 2400,
                }})
            path.write_text(content, encoding="utf-8")
            path.chmod(0o600)
            setattr(self.args, attribute, path)
            inputs[attribute] = path
        self.args.run_root = run_root
        self.args.state = run_root / "evidence/lifecycle/supervisor-state.json"
        self.state = self.args.state
        with mock.patch.object(lifecycle.Supervisor, "validate_run_root", return_value=run_root):
            first = lifecycle.Supervisor(self.args)
            first.transition("MUTATING", mutationPgid=None)
            pinned_token = Path(first.state["inputs"]["pveTokenTfvars"]["pinned"])
            self.assertEqual(pinned_token.read_text(encoding="utf-8"), "pve-token.tfvars")
            pinned_guest_key = Path(first.state["inputs"]["guestSshPrivateKey"]["pinned"])
            self.assertEqual(pinned_guest_key.read_text(encoding="utf-8"), "guest_ssh")
            self.assertNotEqual(
                first.state["inputs"]["pveSshPrivateKey"]["sha256"],
                first.state["inputs"]["guestSshPrivateKey"]["sha256"],
            )
            pinned_ca = Path(first.state["inputs"]["pveCaPem"]["pinned"])
            self.assertEqual(pinned_ca.read_text(encoding="utf-8"), "pve-ca.pem")
            inputs["pve_token_tfvars"].unlink()
            inputs["pve_ca_pem"].unlink()
            restarted = lifecycle.Supervisor(self.args)
        self.assertTrue(restarted.state["sourceInputTamperDetected"])
        observed = []

        def run(command, check=False, timeout=None):
            observed.append({key: os.environ[key] for key in (
                "ROUTERD_RELEASE_QA_PINNED_CONTRACT", "ROUTERD_RELEASE_QA_PINNED_RUN_ENV",
                "ROUTERD_RELEASE_QA_PINNED_TFVARS", "ROUTERD_RELEASE_QA_PINNED_PVE_SSH_PRIVATE_KEY",
                "ROUTERD_RELEASE_QA_PINNED_GUEST_SSH_PRIVATE_KEY",
                "ROUTERD_RELEASE_QA_PINNED_PVE_SSH_KNOWN_HOSTS",
                "ROUTERD_RELEASE_QA_PINNED_PVE_TOKEN_TFVARS",
                "ROUTERD_RELEASE_QA_PINNED_PVE_CA_PEM")})
            return mock.Mock(returncode=0)

        with mock.patch.object(lifecycle.subprocess, "run", side_effect=run):
            self.assertEqual(restarted.cleanup_and_verify("supervisor-restart"), 1)
        self.assertEqual(len(observed), 3)
        self.assertTrue(all("/pinned/" in value for env in observed for value in env.values()))
        self.assertEqual(Path(observed[0]["ROUTERD_RELEASE_QA_PINNED_PVE_TOKEN_TFVARS"]).read_text(),
                         "pve-token.tfvars")
        self.assertEqual(Path(observed[0]["ROUTERD_RELEASE_QA_PINNED_GUEST_SSH_PRIVATE_KEY"]).read_text(),
                         "guest_ssh")
        self.assertEqual(Path(observed[0]["ROUTERD_RELEASE_QA_PINNED_PVE_CA_PEM"]).read_text(),
                         "pve-ca.pem")
        self.assertEqual(Path(observed[0]["ROUTERD_RELEASE_QA_PINNED_PVE_SSH_KNOWN_HOSTS"]).read_text(),
                         "pve-known_hosts")
        durable = json.loads(self.state.read_text())
        durable["executionMode"] = lifecycle.STAGING_MODE
        durable["effectiveLifecycle"]["executionMode"] = lifecycle.STAGING_MODE
        lifecycle.atomic_json(self.state, durable)
        with mock.patch.object(lifecycle.Supervisor, "validate_run_root", return_value=run_root):
            with self.assertRaisesRegex(lifecycle.SupervisorError, "differs from the pinned contract"):
                lifecycle.Supervisor(self.args)

    def test_past_paid_deadline_and_many_failures_never_cap_cleanup_recovery(self):
        supervisor = lifecycle.Supervisor(self.args)
        supervisor.transition("MUTATING", mutationPgid=None)
        supervisor.state["paidDeadline"] = "2000-01-01T00:00:00Z"
        supervisor.state["cleanupAttempts"] = 9
        lifecycle.atomic_json(self.state, supervisor.state)
        calls = []

        def failed_cleanup(command, check=False, timeout=None):
            calls.append(command[0])
            return mock.Mock(returncode=1)

        with mock.patch.object(lifecycle.subprocess, "run", side_effect=failed_cleanup):
            self.assertEqual(supervisor.cleanup_and_verify("supervisor-restart"), 2)
        self.assertEqual(calls, ["cleanup", "inventory"])
        durable = json.loads(self.state.read_text())
        self.assertGreater(durable["cleanupAttempts"], 9)

        # A later boot/restart must retry from durable state and may only stop
        # once inventory independently confirms zero.
        durable["phase"] = "CLEANING"
        lifecycle.atomic_json(self.state, durable)
        restarted = lifecycle.Supervisor(self.args)
        calls.clear()

        def recovered(command, check=False, timeout=None):
            calls.append(command[0])
            return mock.Mock(returncode=0)

        with mock.patch.object(lifecycle.subprocess, "run", side_effect=recovered):
            self.assertEqual(restarted.run(), 1)
        self.assertEqual(calls, ["cleanup", "inventory", "post-zero"])
        self.assertEqual(json.loads(self.state.read_text())["inventoryExit"], 0)

    def test_real_nested_descendant_is_quiesced_before_cleanup(self):
        root = Path(self.temp.name)
        child_pid = root / "descendant.pid"
        cleanup_marker = root / "cleanup"
        mutation = root / "mutation.py"
        mutation.write_text(
            "import pathlib, subprocess\n"
            f"p=subprocess.Popen(['sleep','30']); pathlib.Path({str(child_pid)!r}).write_text(str(p.pid)); p.wait()\n",
            encoding="utf-8",
        )
        cleanup = root / "cleanup.py"
        cleanup.write_text(
            "import os, pathlib, sys, time\n"
            f"pidfile=pathlib.Path({str(child_pid)!r})\n"
            "deadline=time.monotonic()+2\n"
            "while not pidfile.exists() and time.monotonic()<deadline: time.sleep(.01)\n"
            "pid=int(pidfile.read_text())\n"
            "def alive():\n"
            " try:\n"
            "  state=pathlib.Path(f'/proc/{pid}/stat').read_text().split()[2]\n"
            "  return state != 'Z'\n"
            " except (FileNotFoundError, ProcessLookupError): return False\n"
            "if alive(): sys.exit(9)\n"
            f"pathlib.Path({str(cleanup_marker)!r}).write_text('quiesced')\n",
            encoding="utf-8",
        )
        self.args.precheck_command = ["/bin/true"]
        self.args.mutation_command = [sys.executable, str(mutation)]
        self.args.cleanup_command = [sys.executable, str(cleanup)]
        self.args.inventory_command = ["/bin/true"]
        self.args.post_zero_command = ["/bin/true"]
        self.args.term_grace_seconds = 1
        supervisor = lifecycle.Supervisor(self.args)
        def request_stop():
            deadline = time.monotonic() + 2
            while not child_pid.exists() and time.monotonic() < deadline:
                time.sleep(0.01)
            supervisor.stop_reason = "SIGTERM"
        stopper = threading.Thread(target=request_stop)
        stopper.start()
        self.assertEqual(supervisor.run(), 1)
        stopper.join()
        self.assertEqual(cleanup_marker.read_text(), "quiesced")


if __name__ == "__main__":
    unittest.main()
