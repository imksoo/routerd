"""Test host-effect opt-in boundaries without allowing any real subprocess."""
from contextlib import ExitStack
import importlib.util
import os
from pathlib import Path
import subprocess
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parent
OPT_IN = "ROUTERD_RELEASE_QA_ISOLATED_TESTS"


def load_suite_module(name):
    spec = importlib.util.spec_from_file_location(name, ROOT / (name + ".py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


SYSTEMD = load_suite_module("test_systemd_confinement")
LAUNCHER = load_suite_module("test_launcher_restart")
HOST_CLASSES = (SYSTEMD.SystemdConfinementTests, LAUNCHER.LauncherRestartTests)


class IsolatedHostGateTests(unittest.TestCase):
    def run_cases(self, classes):
        suite = unittest.TestSuite(unittest.defaultTestLoader.loadTestsFromTestCase(cls) for cls in classes)
        result = unittest.TestResult()
        suite.run(result)
        if result.errors or result.failures:
            self.fail("host cases reached forbidden work: " + ", ".join(
                case.id() for case, _ in result.errors + result.failures))
        return result

    def test_unset_and_nonexact_opt_in_skip_every_host_case_before_subprocess(self):
        for value in (None, "", "0", "true", "yes", " 1", "1 "):
            with self.subTest(value=value), mock.patch.dict(os.environ, {}, clear=False):
                os.environ.pop(OPT_IN, None)
                if value is not None:
                    os.environ[OPT_IN] = value
                with mock.patch.object(subprocess, "run", side_effect=AssertionError("forbidden subprocess")) as run:
                    with mock.patch.object(subprocess, "Popen", side_effect=AssertionError("forbidden process")) as popen:
                        result = self.run_cases(HOST_CLASSES)
                self.assertEqual(result.testsRun, 7)
                self.assertEqual(len(result.skipped), 7)
                self.assertTrue(all(OPT_IN in reason for _, reason in result.skipped))
                run.assert_not_called()
                popen.assert_not_called()

    def test_exact_opt_in_reaches_only_mocked_test_bodies(self):
        entered = []
        with mock.patch.dict(os.environ, {OPT_IN: "1"}), ExitStack() as stack:
            run = stack.enter_context(mock.patch.object(subprocess, "run", side_effect=AssertionError("forbidden subprocess")))
            popen = stack.enter_context(mock.patch.object(subprocess, "Popen", side_effect=AssertionError("forbidden process")))
            for cls in HOST_CLASSES:
                for name in unittest.defaultTestLoader.getTestCaseNames(cls):
                    stack.enter_context(mock.patch.object(cls, name, lambda case: entered.append(case.id())))
            result = self.run_cases(HOST_CLASSES)
        self.assertEqual(result.testsRun, 7)
        self.assertEqual(result.skipped, [])
        self.assertEqual(len(entered), 7)
        run.assert_not_called()
        popen.assert_not_called()

    def test_opt_in_does_not_bypass_missing_systemd_prerequisite(self):
        with mock.patch.dict(os.environ, {OPT_IN: "1"}), mock.patch.object(SYSTEMD.shutil, "which", return_value=None):
            with mock.patch.object(subprocess, "run", side_effect=AssertionError("forbidden subprocess")) as run:
                with mock.patch.object(subprocess, "Popen", side_effect=AssertionError("forbidden process")) as popen:
                    result = self.run_cases((SYSTEMD.SystemdConfinementTests,))
        self.assertEqual(len(result.skipped), 5)
        self.assertTrue(all(reason == "systemd-run and sudo are required" for _, reason in result.skipped))
        run.assert_not_called()
        popen.assert_not_called()

    def test_opt_in_keeps_existing_sudo_probe_and_never_starts_systemd_on_failure(self):
        failed = subprocess.CompletedProcess(["sudo", "-n", "true"], 1)
        with mock.patch.dict(os.environ, {OPT_IN: "1"}), mock.patch.object(SYSTEMD.shutil, "which", return_value="/fixture/tool"):
            with mock.patch.object(subprocess, "run", return_value=failed) as run:
                with mock.patch.object(subprocess, "Popen", side_effect=AssertionError("forbidden process")) as popen:
                    result = self.run_cases((SYSTEMD.SystemdConfinementTests,))
        self.assertEqual(len(result.skipped), 5)
        self.assertTrue(all(reason == "passwordless sudo is required for mount namespace test" for _, reason in result.skipped))
        self.assertEqual(run.call_args_list, [mock.call(["sudo", "-n", "true"], check=False)] * 5)
        popen.assert_not_called()


if __name__ == "__main__":
    unittest.main()
