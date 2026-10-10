"""Replay redacted saved observations; never change the original run verdict."""
import copy
import json
from pathlib import Path
import unittest

from test_arp_controller_progress import module as controller
from test_arp_observation_health import state, check

FIXTURES = Path(__file__).parent / 'fixtures'


class SavedObservationTests(unittest.TestCase):
    def setUp(self):
        self.saved = json.loads((FIXTURES / 'arp-r12-boundary.json').read_text())

    def evaluate(self, data=None):
        d = self.saved if data is None else data
        return controller.evaluate_arp_controller_progress(d['controllers'], d['counterSampling'], **d['binding'])

    def test_actual_r12_boundary_completion_is_retained(self):
        result = self.evaluate()
        self.assertTrue(result['success'], result)
        proof = result['intervals'][0]
        self.assertEqual(proof['method'], 'recorded_command_completion')
        self.assertEqual(proof['commandEvidence']['sequence'], 571)
        self.assertEqual(proof['commandEvidence']['packetsSent'], 3)
        first = self.saved['counterSampling']['samples'][proof['firstCommandSample']]
        self.assertGreater(first['epoch'], self.saved['controllers'][1]['epoch'])
        self.assertEqual(proof['originalCommandJSON'], first['lastCommandProbe'])

    def test_same_trace_without_target_record_fails(self):
        for row in self.saved['counterSampling']['samples']:
            row.pop('lastCommandProbe')
        self.assertFalse(self.evaluate()['success'])

    def test_same_trace_without_later_reconcile_fails(self):
        self.saved['controllers'] = self.saved['controllers'][:2]
        self.assertFalse(self.evaluate()['success'])

    def test_partial_write_wrong_sequence_and_changed_record_fail(self):
        for update in ({'packetsSent': 2}, {'sequence': 572}, {'target': 'invalid'}):
            data = copy.deepcopy(self.saved)
            for row in data['counterSampling']['samples']:
                if row['commandProbeCount'] == 571:
                    record = json.loads(row['lastCommandProbe'])
                    record.update(update)
                    row['lastCommandProbe'] = json.dumps(record)
            with self.subTest(update=update):
                self.assertFalse(self.evaluate(data)['success'])

    def test_removed_boundary_reads_cannot_be_interpolated(self):
        rows = self.saved['counterSampling']['samples']
        first = next(i for i, row in enumerate(rows) if row['commandProbeCount'] == 571)
        del rows[first-3:first+2]
        self.assertFalse(self.evaluate()['success'])

    def test_original_r11_error_wins_over_an_old_healthy_progress_snapshot(self):
        saved = json.loads((FIXTURES / 'arp-r11-collector-error.json').read_text())
        self.assertLess(saved['captureDurationSeconds'], 400)
        observation = state()
        observation['terminal'] = {'errors': saved['errors']}
        # Keep the healthy-looking 104-sample progress fixture. Terminal
        # failure must win; a generic "short horizon" must not hide it.
        with self.assertRaisesRegex(ValueError, 'counter sampler error before progress'):
            check(observation)
        observation['terminal'] = None
        observation['counterError'] = saved['counterErrors'][0]
        # Counter sampling alone is diagnostic while the owned capture lives.
        # Preserve the original error without turning this into a new deadline.
        self.assertTrue(check(observation)['success'])
        observation['captureProcess']['alive'] = False
        with self.assertRaisesRegex(ValueError, 'capture'):
            check(observation)


if __name__ == '__main__':
    unittest.main()
