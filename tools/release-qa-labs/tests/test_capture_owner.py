import importlib.util
import io
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[3]
SPEC = importlib.util.spec_from_file_location('capture_owner', ROOT / 'scripts/runners/cloudedge_capture.py')
owner = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(owner)

CAPTURE = '''import os, signal
signal.signal(signal.SIGINT, lambda *_: exit(0))
os.write(1, bytes.fromhex('d4c3b2a1') + bytes(20))
while True: signal.pause()
'''


class CaptureOwnerTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        self.capture = self.directory / 'point.pcap'
        self.receipt = self.directory / 'point.pid'
        self.log = self.directory / 'point.log'
        self.command = [sys.executable, '-c', CAPTURE]

    def start(self):
        with owner.locked_receipt(self.receipt):
            owner.start(self.capture, self.receipt, self.log, self.command, timeout=2)
        original = json.loads(self.receipt.read_text())

        def cleanup():
            try:
                fd = os.pidfd_open(original['process']['pid'])
            except ProcessLookupError:
                return
            try:
                try:
                    signal.pidfd_send_signal(fd, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                self.assertTrue(owner.wait_exit(fd, 2))
                owner.reap(original['process']['pid'])
            finally:
                os.close(fd)

        self.addCleanup(cleanup)
        return original

    def test_real_admitted_child_stop_and_exact_copy(self):
        self.start()
        with self.assertRaisesRegex(owner.CaptureError, 'acknowledged stop'):
            owner.copy(self.capture, self.receipt, io.BytesIO())
        owner.stop(self.capture, self.receipt, timeout=2)
        data = io.BytesIO()
        owner.copy(self.capture, self.receipt, data)
        self.assertEqual(data.getvalue(), bytes.fromhex('d4c3b2a1') + bytes(20))
        owner.stop(self.capture, self.receipt, timeout=2)

    def test_receipt_write_failure_prevents_exec(self):
        real_save = owner.save

        def fail_admission(path, receipt):
            if receipt['phase'] == 'starting':
                raise OSError('injected durable admission failure')
            real_save(path, receipt)

        with mock.patch.object(owner, 'save', side_effect=fail_admission):
            with self.assertRaisesRegex(OSError, 'admission failure'):
                owner.start(self.capture, self.receipt, self.log, self.command, timeout=2)
        self.assertEqual(self.capture.stat().st_size, 0)
        self.assertEqual(json.loads(self.receipt.read_text())['phase'], 'preparing')

    def test_stop_lost_ack_reopens_and_observes_actual_exit(self):
        self.start()
        real_save = owner.save

        def lose_ack(path, receipt):
            if receipt['phase'] == 'stopped':
                raise OSError('lost stop acknowledgement')
            real_save(path, receipt)

        with mock.patch.object(owner, 'save', side_effect=lose_ack):
            with self.assertRaisesRegex(OSError, 'lost stop acknowledgement'):
                owner.stop(self.capture, self.receipt, timeout=2)
        self.assertEqual(json.loads(self.receipt.read_text())['phase'], 'stopping')
        owner.stop(self.capture, self.receipt, timeout=2)
        self.assertEqual(json.loads(self.receipt.read_text())['phase'], 'stopped')

    def test_foreign_birth_and_file_replacement_are_not_signaled(self):
        original = self.start()
        changed = json.loads(self.receipt.read_text())
        changed['process']['started'] += 1
        owner.save(self.receipt, changed)
        with self.assertRaises(owner.CaptureError):
            owner.stop(self.capture, self.receipt, timeout=2)
        owner.save(self.receipt, original)
        held = self.directory / 'original.pcap'
        self.capture.rename(held)
        self.capture.write_bytes(b'foreign data')
        self.capture.chmod(0o600)
        with self.assertRaisesRegex(owner.CaptureError, 'file identity'):
            owner.stop(self.capture, self.receipt, timeout=2)
        fd = os.pidfd_open(original['process']['pid'])
        try:
            self.assertFalse(owner.wait_exit(fd, 0))
        finally:
            os.close(fd)

    def test_unexpected_exit_is_not_successful_capture_stop(self):
        receipt = self.start()
        fd = os.pidfd_open(receipt['process']['pid'])
        try:
            signal.pidfd_send_signal(fd, signal.SIGKILL)
            self.assertTrue(owner.wait_exit(fd, 2))
            owner.reap(receipt['process']['pid'])
        finally:
            os.close(fd)
        with self.assertRaisesRegex(owner.CaptureError, 'exited|disappeared'):
            owner.stop(self.capture, self.receipt, timeout=2)
        self.assertEqual(json.loads(self.receipt.read_text())['phase'], 'running')

    def test_existing_capture_is_not_truncated_and_legacy_pid_is_rejected(self):
        self.capture.write_bytes(b'foreign capture')
        with self.assertRaises(FileExistsError):
            owner.start(self.capture, self.receipt, self.log, self.command, timeout=2)
        self.assertEqual(self.capture.read_bytes(), b'foreign capture')
        self.receipt.write_text(str(os.getpid()))
        self.receipt.chmod(0o600)
        with self.assertRaises((ValueError, AttributeError, owner.CaptureError)):
            owner.stop(self.capture, self.receipt, timeout=2)

    def test_startup_exit_and_receipt_lock_are_explicit_failures(self):
        with self.assertRaisesRegex(owner.CaptureError, 'exited'):
            owner.start(self.capture, self.receipt, self.log, [sys.executable, '-c', 'pass'], timeout=2)
        with owner.locked_receipt(self.receipt):
            with self.assertRaises(BlockingIOError):
                with owner.locked_receipt(self.receipt):
                    self.fail('second capture operation acquired the lock')


if __name__ == '__main__':
    unittest.main()
