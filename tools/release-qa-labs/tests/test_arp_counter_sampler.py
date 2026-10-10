import copy
import importlib.util
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch

PATH = Path(__file__).resolve().parents[1] / 'arp_counter_sampler.py'
spec = importlib.util.spec_from_file_location('arp_counter_sampler', PATH)
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class Clock:
    def __init__(self):
        self.now = 10.0
        self.extra_wait = 0.0
    def monotonic(self): return self.now
    def time(self): return 1000 + self.now
    def is_set(self): return False
    def wait(self, seconds):
        self.now += seconds + self.extra_wait
        self.extra_wait = 0.0


class CadenceTests(unittest.TestCase):
    def run_samples(self, durations, *, stall=0, on_error=None, duration=0.6):
        clock = Clock()
        sampler = m.CounterSampler('/unused', 1, duration=duration, on_error=on_error)
        sampler.stop_event = clock
        clock.extra_wait = stall
        durations = iter(durations)
        def sample():
            began = clock.now
            elapsed = next(durations, 0.01)
            clock.now += elapsed
            row = {'cycleMonotonic': began, 'epoch': 1000 + began,
                   'completedMonotonic': clock.now, 'completedEpoch': 1000 + clock.now,
                   'commandProbeCount': 1, 'lastCommandProbe': 'original JSON string'}
            sampler._current_sample = row
            sampler._stage_seconds = {'database': min(0.002, elapsed), 'responseHeaders': elapsed}
            return row
        sampler.sample = sample
        with patch.object(m.time, 'monotonic', clock.monotonic), patch.object(m.time, 'time', clock.time):
            sampler._run()
        return sampler

    def test_170ms_read_is_kept_under_existing_400ms_coverage(self):
        s = self.run_samples([0.17])
        self.assertFalse(s.errors)
        self.assertEqual(len(s.samples), 5)
        self.assertAlmostEqual(s.samples[0]['completedMonotonic'], 10.17)
        self.assertAlmostEqual(s.samples[1]['cycleMonotonic'], 10.2)
        self.assertEqual(s.diagnostics['missedScheduleTicks'], 1)
        self.assertEqual(s.diagnostics['overCadenceSamples'], 1)
        self.assertLess(s.diagnostics['maximumCoverageSeconds'], 0.4)
        self.assertEqual(s.samples[0]['lastCommandProbe'], 'original JSON string')
        self.assertEqual(s.diagnostics['slowSamples'][0]['stageSeconds']['responseHeaders'], 0.17)

    def test_overlong_first_read_is_not_admitted_and_publishes_original(self):
        published = []
        s = self.run_samples([0.401], on_error=published.append)
        self.assertFalse(s.samples)
        self.assertEqual(s.errors[0]['error'], 'sample coverage exceeded limit')
        self.assertGreater(s.errors[0]['sample']['completedMonotonic'] - s.errors[0]['sample']['cycleMonotonic'], 0.4)
        self.assertEqual(published, s.errors)
        self.assertEqual(s.errors[0]['stageSeconds']['responseHeaders'], 0.401)

    def test_adjacent_read_span_includes_previous_read_and_wait(self):
        s = self.run_samples([0.01, 0.31])
        self.assertEqual(len(s.samples), 1)
        self.assertEqual(s.errors[0]['error'], 'sample coverage exceeded limit')
        self.assertGreater(s.diagnostics['maximumCoverageSeconds'], 0.4)

    def test_scheduler_stall_is_a_real_gap_even_when_reads_are_fast(self):
        s = self.run_samples([0.01], stall=0.31)
        self.assertEqual(len(s.samples), 1)
        self.assertEqual(s.errors[0]['error'], 'sample coverage exceeded limit')

    def test_error_publisher_failure_cannot_hide_original(self):
        def fail(_): raise OSError('disk failure')
        s = self.run_samples([0.401], on_error=fail)
        self.assertIn('coverage', s.errors[0]['error'])
        self.assertIn('publication failed', s.errors[1]['error'])

    def test_slow_diagnostics_are_bounded_without_discarding_samples(self):
        s = self.run_samples([0.11] * 200, duration=10)
        self.assertFalse(s.errors)
        self.assertEqual(len(s.diagnostics['slowSamples']), 32)
        self.assertGreater(s.diagnostics['slowSamplesTruncated'], 0)
        self.assertEqual(len(s.samples), s.diagnostics['overCadenceSamples'])

    def test_bounds_cannot_expand_the_evidence_gap(self):
        for value in [0, -1, 0.401, float('nan'), float('inf'), True]:
            with self.subTest(value=value), self.assertRaises(ValueError):
                m.CounterSampler('/unused', 1, max_sample_gap=value)


class Response:
    status = 200
    def __init__(self, obj): self.obj = obj
    def read(self, limit): return json.dumps(self.obj).encode()[:limit]


class Connection:
    def __init__(self, obj): self.obj = obj; self.closed = False
    def request(self, method, path):
        assert (method, path) == ('GET', '/v1/status')
    def getresponse(self): return Response(self.obj)
    def close(self): self.closed = True


class ReadTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.db = str(Path(self.directory.name) / 'state.db')
        with sqlite3.connect(self.db) as db:
            db.execute('CREATE TABLE federation_events (id TEXT, group_name TEXT, source_node TEXT, type TEXT, subject TEXT, dedupe_key TEXT, payload TEXT, observed_at INTEGER, expires_at INTEGER, recorded_at INTEGER)')
            db.execute('INSERT INTO federation_events VALUES (?,?,?,?,?,?,?,?,?,?)',
                       ('request', 'pool', 'sender', 'routerd.mobility.arp.request.observed', '192.0.2.2/32', 'request', '{"address":"192.0.2.2/32"}', 1, 46, 2))
        self.obj = {'since': '2026-10-10T00:00:00Z', 'phase': 'Running', 'health': 'ok',
                    'observed': {key: '1' for key in m.COUNTERS}, 'resources': []}
        self.obj['observed']['lastCommandProbe'] = '{"sequence":1,"target":"192.0.2.2","packetsSent":3}'
        self.sampler = m.CounterSampler('/fixture.sock', 123, duration=0.1, db_path=self.db)
        self.sampler.identity = lambda: '1234'

    def test_original_full_receipt_and_command_string_survive(self):
        conn = Connection(self.obj)
        with patch.object(m, 'UnixHTTP', return_value=conn): row = self.sampler.sample()
        self.assertTrue(conn.closed)
        self.assertLessEqual(row['dbCompletedEpoch'], row['epoch'])
        self.assertLessEqual(row['epoch'], row['completedEpoch'])
        self.assertEqual(row['lastCommandProbe'], self.obj['observed']['lastCommandProbe'])
        self.assertEqual(self.sampler.db_states[0]['rows'][0]['payload']['address'], '192.0.2.2/32')
        self.assertIn('responseHeaders', self.sampler._stage_seconds)
        with sqlite3.connect(self.db) as db: self.assertEqual(db.execute('SELECT count(*) FROM federation_events').fetchone()[0], 1)

    def test_changed_observer_is_rejected(self):
        with patch.object(m, 'UnixHTTP', return_value=Connection(self.obj)): self.sampler.sample()
        changed = copy.deepcopy(self.obj); changed['since'] = '2026-10-10T00:00:01Z'
        with patch.object(m, 'UnixHTTP', return_value=Connection(changed)), self.assertRaisesRegex(RuntimeError, 'identity changed'):
            self.sampler.sample()

    def test_missing_counter_is_not_invented(self):
        del self.obj['observed']['probeCount']
        with patch.object(m, 'UnixHTTP', return_value=Connection(self.obj)):
            self.sampler._run()
        self.assertFalse(self.sampler.samples)
        self.assertIn('probeCount', self.sampler.errors[0]['error'])
        self.assertEqual(self.sampler.errors[0]['stage'], 'validateStatus')
        self.assertEqual(self.sampler.errors[0]['sample']['dbStateIndex'], 0)

    def test_db_failure_has_stage_details_and_no_valid_sample(self):
        self.sampler.db_path += '.absent'
        self.sampler._run()
        self.assertFalse(self.sampler.samples)
        self.assertEqual(self.sampler.errors[0]['stage'], 'database')
        self.assertIn('database', self.sampler.errors[0]['stageSeconds'])


if __name__ == '__main__': unittest.main()
