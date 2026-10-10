import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

PATH = Path(__file__).resolve().parents[1] / 'arp_observation_health.py'
spec = importlib.util.spec_from_file_location('arp_observation_health', PATH)
m = importlib.util.module_from_spec(spec); spec.loader.exec_module(m)


def state():
    return {'ownership': {'pid': 10, 'startTicks': '50', 'bootId': 'boot', 'capturePid': 11, 'captureStartTicks': '51'},
            'process': {'pid': 10, 'startTicks': '50', 'alive': True},
            'captureProcess': {'pid': 11, 'startTicks': '51', 'alive': True},
            'bootId': 'boot', 'checkedMonotonic': 102.0, 'checkedEpoch': 1002,
            'progress': {'collectorPID': 10, 'collectorState': 'running', 'collectorErrors': [],
                         'bootId': 'boot', 'completedMonotonic': 100.0,
                         'counterSamplerHealth': {'errors': [], 'sampleCount': 104, 'running': True}},
            'terminal': None, 'counterError': None}


def check(value, **extra):
    return m.assert_collector_running(value, expected_pid=10, expected_start_ticks='50',
                                     expected_boot_id='boot', requires_counters=True, **extra)


class HealthTests(unittest.TestCase):
    def test_fresh_original_progress_passes(self):
        self.assertEqual(check(state())['progressAgeSeconds'], 2)

    def test_progress_age_is_diagnostic_when_capture_is_alive(self):
        s = state(); s['checkedMonotonic'] = 280
        self.assertTrue(check(s)['success'])

    def test_terminal_failure_wins_over_healthy_progress(self):
        s = state(); s['terminal'] = {'errors': ['sample exceeded interval'], 'captureExited': True}
        with self.assertRaisesRegex(ValueError, 'sample exceeded interval'): check(s)

    def test_even_successful_terminal_collector_cannot_receive_stimulus(self):
        s = state(); s['terminal'] = {'errors': [], 'captureExited': True}
        with self.assertRaisesRegex(ValueError, 'already finished'): check(s)

    def test_counter_error_does_not_stop_live_capture(self):
        s = state(); s['counterError'] = {'error': 'sample coverage exceeded limit', 'stage': 'coverage'}
        self.assertTrue(check(s)['success'])

    def test_pid_reuse_stopped_capture_or_changed_boot_are_rejected(self):
        for group, key, value in [('process', 'startTicks', 'new'), ('process', 'alive', False),
                                  ('captureProcess', 'startTicks', 'new'), ('captureProcess', 'alive', False),
                                  ('ownership', 'bootId', 'changed')]:
            s = state(); s[group][key] = value
            with self.subTest(group=group, key=key), self.assertRaises(ValueError): check(s)

    def test_error_progress_cannot_be_made_fresh_by_updating_time(self):
        s = state(); s['progress'].update(collectorState='failed', collectorErrors=['read timeout'], completedMonotonic=102)
        with self.assertRaisesRegex(ValueError, 'read timeout'): check(s)

    def test_sampler_availability_is_diagnostic(self):
        for key, value in [('running', False), ('sampleCount', 0), ('errors', ['read timeout'])]:
            s = state(); s['progress']['counterSamplerHealth'][key] = value
            with self.subTest(key=key): self.assertTrue(check(s)['success'])

    def test_progress_timestamps_do_not_replace_process_liveness(self):
        s = state(); s['progress']['completedMonotonic'] = 103
        self.assertTrue(check(s)['success'])

    def test_reader_only_uses_required_capture_metadata(self):
        d = state()
        reads = [d['ownership'], d['progress'], None]
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); (root/'sys/kernel/random').mkdir(parents=True)
            (root/'sys/kernel/random/boot_id').write_text('boot\n')
            with patch.object(m, '_read_json', side_effect=reads), patch.object(m, '_process', side_effect=[d['process'], d['captureProcess']]):
                observed = m.read_collector_state('/run-owned', proc_root=root)
        self.assertTrue(check(observed)['success'])

    def test_real_readonly_metadata_reader_detects_disappeared_process(self):
        d = state()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); run = root/'run'; run.mkdir(); proc = root/'proc'; proc.mkdir()
            (proc/'sys/kernel/random').mkdir(parents=True)
            (proc/'sys/kernel/random/boot_id').write_text('boot\n')
            for pid, ticks in [(10,'50'), (11,'51')]:
                (proc/str(pid)).mkdir()
                (proc/str(pid)/'stat').write_text(str(pid)+' (name with spaces) '+' '.join(['S']+['0']*18+[ticks]))
            (run/'ownership.json').write_text(json.dumps(d['ownership']))
            (run/'progress.json').write_text(json.dumps(d['progress']))
            with patch.object(m.time, 'monotonic', return_value=102):
                observed = m.read_collector_state(run, proc_root=proc)
                self.assertTrue(check(observed)['success'])
                (proc/'10/stat').unlink()
                observed = m.read_collector_state(run, proc_root=proc)
                with self.assertRaisesRegex(ValueError, 'exited'): check(observed)
            self.assertEqual(json.loads((run/'progress.json').read_text()), d['progress'])


if __name__ == '__main__': unittest.main()
