import copy
import datetime
import importlib.util
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('arp_controller_progress', Path(__file__).resolve().parents[1] / 'arp_controller_progress.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


def stamp(value):
    return datetime.datetime.fromtimestamp(value, datetime.timezone.utc).isoformat()


def controller(start, count, last):
    return {'epoch': start, 'completedEpoch': start + .05, 'bootId': 'boot', 'mainPID': '10',
            'controllers': [{'name': 'mobility-arp-request', 'reconcileCount': count,
                             'lastReconcileTime': stamp(last), 'lastSuccessTime': stamp(last),
                             'reconcileErrorCount': 37, 'currentError': False}]}


class ControllerProgressTests(unittest.TestCase):
    def setUp(self):
        self.samples = [controller(100, 10, 99), controller(102.05, 10, 99), controller(104.10, 11, 103)]
        self.fast = {'errors': [], 'threadExited': True, 'samples': [
            {'epoch': 99.9 + i * .1, 'completedEpoch': 99.91 + i * .1,
             'pid': 20, 'startTicks': '1000', 'since': stamp(50),
             'commandProbeCount': 100 + int(i >= 11), 'probeCount': i,
             'proactiveCount': 0, 'scanCount': 0, 'requestObservedCount': 0}
            for i in range(45)]}

    def check(self, **kwargs):
        return module.evaluate_arp_controller_progress(self.samples, self.fast,
            observer_pid=20, observer_start_ticks='1000', observer_since=stamp(50), **kwargs)

    def recorded_boundary(self, completed=102.025, first_read=102.1):
        old = {'sequence': 100, 'target': '192.0.2.10', 'startedAt': '1970-01-01T00:01:30Z',
               'completedAt': '1970-01-01T00:01:31Z', 'packetsSent': 3}
        new = dict(old, sequence=101,
                   startedAt=stamp(completed - 1).replace('+00:00', 'Z'),
                   completedAt=stamp(completed).replace('+00:00', 'Z'))
        for row in self.fast['samples']:
            record = new if row['epoch'] >= first_read else old
            row['commandProbeCount'] = record['sequence']
            row['lastCommandProbe'] = json.dumps(record, separators=(',', ':'))

    def change_record(self, **changes):
        for row in self.fast['samples']:
            if row['commandProbeCount'] == 101:
                record = json.loads(row['lastCommandProbe'])
                record.update(changes)
                row['lastCommandProbe'] = json.dumps(record)

    def test_recorded_completion_at_right_boundary(self):
        self.recorded_boundary()
        result = self.check()
        self.assertTrue(result['success'], result)
        proof = result['intervals'][0]
        self.assertEqual(proof['method'], 'recorded_command_completion')
        self.assertEqual(proof['laterCompletedReadIndex'], 2)
        self.assertEqual(proof['commandEvidence']['sequence'], 101)
        row = self.fast['samples'][proof['firstCommandSample']]
        self.assertGreater(row['epoch'], self.samples[1]['epoch'])
        self.assertEqual(proof['originalCommandJSON'], row['lastCommandProbe'])
        self.assertLessEqual(proof['commandReadEnd'] - proof['commandReadStart'], .4)

    def test_recorded_completion_at_left_boundary(self):
        self.recorded_boundary(completed=100.075, first_read=100.1)
        result = self.check()
        self.assertTrue(result['success'], result)
        self.assertEqual(result['intervals'][0]['method'], 'recorded_command_completion')

    def test_recorded_completion_outside_original_interval_fails(self):
        for completed, first_read in [(100.025, 100.1), (102.075, 102.1)]:
            with self.subTest(completed=completed):
                self.recorded_boundary(completed, first_read)
                self.assertFalse(self.check()['success'])

    def test_recorded_completion_requires_later_reconcile(self):
        self.recorded_boundary()
        self.samples = self.samples[:2]
        self.assertFalse(self.check()['success'])

    def test_recorded_completion_requires_adjacent_single_increment(self):
        self.recorded_boundary()
        for row in self.fast['samples']:
            if row['commandProbeCount'] == 101:
                row['commandProbeCount'] = 102
                record = json.loads(row['lastCommandProbe'])
                record['sequence'] = 102
                row['lastCommandProbe'] = json.dumps(record)
        self.assertFalse(self.check()['success'])

    def test_recorded_completion_requires_bounded_observation_bracket(self):
        self.recorded_boundary()
        self.fast['samples'] = [r for r in self.fast['samples'] if not 101.7 < r['epoch'] < 102.1]
        result = self.check()
        self.assertFalse(result['success'], result)
        self.assertEqual(result['intervals'][0]['reason'], 'neither reconcile nor command completion progressed')

    def test_recorded_completion_cannot_replace_missing_interior_coverage(self):
        self.recorded_boundary()
        self.fast['samples'] = [r for r in self.fast['samples'] if not 100.5 < r['epoch'] < 101.2]
        self.assertFalse(self.check()['success'])

    def test_recorded_completion_must_be_newly_observed(self):
        self.recorded_boundary(completed=101.8, first_read=102.1)
        # Two complete earlier reads still report the previous sequence after
        # the asserted completion. The record cannot qualify as a new completion.
        self.assertFalse(self.check()['success'])

    def test_present_malformed_original_command_json_fails(self):
        for value in [None, '', '{', '[]', {}, 'x' * 4097]:
            with self.subTest(value=repr(value)[:40]):
                self.recorded_boundary()
                self.fast['samples'][25]['lastCommandProbe'] = value
                self.assertFalse(self.check()['success'])

    def test_record_counter_and_packet_count_are_strict(self):
        for field, values in [('sequence', [0, True, 100, 102, '101']),
                              ('packetsSent', [0, True, 2, 4, '3'])]:
            for value in values:
                with self.subTest(field=field, value=value):
                    self.recorded_boundary()
                    self.change_record(**{field: value})
                    self.assertFalse(self.check()['success'])

    def test_command_target_must_be_ipv4(self):
        for target in ['::1', 'not-an-ip', 123, None]:
            with self.subTest(target=target):
                self.recorded_boundary()
                self.change_record(target=target)
                self.assertFalse(self.check()['success'])

    def test_command_timestamps_are_strict_and_bound_to_lifetime(self):
        for changes in [
            {'startedAt': '1970-01-01T00:00:49Z'},
            {'startedAt': '1970-01-01T00:01:43Z'},
            {'completedAt': '1970-01-01T00:01:43Z'},
            {'completedAt': '1970-01-01T00:01:42.025'},
            {'completedAt': '1970-01-01T00:01:42.025+00:00'},
            {'completedAt': '1970-01-01T00:01:42.0250000000Z'},
            {'completedAt': None},
        ]:
            with self.subTest(changes=changes):
                self.recorded_boundary()
                self.change_record(**changes)
                self.assertFalse(self.check()['success'])

    def test_nanosecond_command_timestamp(self):
        self.recorded_boundary()
        self.change_record(completedAt='1970-01-01T00:01:42.025000123Z')
        self.assertTrue(self.check()['success'])

    def test_same_sequence_cannot_change_record(self):
        self.recorded_boundary()
        row = self.fast['samples'][25]
        record = json.loads(row['lastCommandProbe'])
        record['target'] = '192.0.2.20'
        row['lastCommandProbe'] = json.dumps(record)
        self.assertFalse(self.check()['success'])

    def test_serial_records_cannot_overlap(self):
        self.recorded_boundary()
        self.change_record(startedAt='1970-01-01T00:01:30.5Z')
        self.assertFalse(self.check()['success'])

    def test_missing_record_never_enables_boundary_fallback(self):
        self.recorded_boundary()
        for row in self.fast['samples']:
            del row['lastCommandProbe']
        self.assertFalse(self.check()['success'])

    def test_zero_commands_allow_empty_initial_record(self):
        for row in self.fast['samples']:
            row['commandProbeCount'] = 0
            row['lastCommandProbe'] = None
        self.samples[1] = controller(102.05, 11, 101)
        self.samples[2] = controller(104.1, 12, 103)
        self.assertTrue(self.check()['success'])

    def test_frozen_packet_count_configuration_is_explicit(self):
        self.recorded_boundary()
        self.assertFalse(self.check(expected_command_packets=2)['success'])
        self.assertTrue(self.check(expected_command_packets=3)['success'])
        for packets in [0, -1, True, '3']:
            self.assertFalse(self.check(expected_command_packets=packets)['success'])

    def test_maximum_coverage_cannot_be_widened(self):
        self.recorded_boundary()
        for gap in [0, -.1, .401, True, float('nan')]:
            with self.subTest(gap=gap):
                self.assertFalse(self.check(max_sample_gap=gap)['success'])

    def test_zero_error_counter_uses_control_api_omitempty_semantics(self):
        for row in self.samples:
            del row['controllers'][0]['reconcileErrorCount']
        self.assertTrue(self.check()['success'])
        self.samples[1]['controllers'][0]['reconcileErrorCount'] = 1
        self.assertFalse(self.check()['success'])

    def test_serial_command_work_with_later_successful_reconcile(self):
        result = self.check()
        self.assertTrue(result['success'], result)
        self.assertEqual([x['method'] for x in result['intervals']], ['serial_command_completions', 'completed_reconcile'])
        self.assertEqual(result['intervals'][0]['laterCompletedReadIndex'], 2)

    def test_completed_reconciles_need_no_fallback(self):
        self.samples[1] = controller(102.05, 11, 101)
        self.samples[2] = controller(104.1, 12, 103)
        self.assertTrue(self.check()['success'])

    def test_command_work_does_not_hide_stuck_final_reconcile(self):
        self.samples = self.samples[:2]
        self.assertFalse(self.check()['success'])

    def test_idle_stall_cannot_use_autonomous_packets(self):
        for row in self.fast['samples']:
            row['commandProbeCount'] = 100
            row['proactiveCount'] = row['probeCount']
        self.assertFalse(self.check()['success'])

    def test_command_completion_outside_interior_window_does_not_qualify(self):
        for row in self.fast['samples']:
            row['commandProbeCount'] = 100 + int(row['epoch'] >= 102.02)
        self.assertFalse(self.check()['success'])

    def test_counter_coverage_gap_fails(self):
        self.fast['samples'] = [r for r in self.fast['samples'] if not 100.5 < r['epoch'] < 101.2]
        self.assertFalse(self.check()['success'])

    def test_missing_boundary_coverage_fails(self):
        self.fast['samples'] = self.fast['samples'][8:]
        self.assertFalse(self.check()['success'])

    def test_sampler_errors_or_incomplete_collection_fail(self):
        for key, value in [('errors', ['read failure']), ('threadExited', False)]:
            with self.subTest(key=key):
                old = self.fast[key]
                self.fast[key] = value
                self.assertFalse(self.check()['success'])
                self.fast[key] = old

    def test_process_identity_changes_fail(self):
        for key, value in [('pid', 21), ('startTicks', '2000'), ('since', stamp(60))]:
            with self.subTest(key=key):
                old = self.fast['samples'][10][key]
                self.fast['samples'][10][key] = value
                self.assertFalse(self.check()['success'])
                self.fast['samples'][10][key] = old

    def test_router_identity_changes_fail(self):
        for key in ['bootId', 'mainPID']:
            with self.subTest(key=key):
                old = self.samples[1][key]
                self.samples[1][key] = 'replacement'
                self.assertFalse(self.check()['success'])
                self.samples[1][key] = old

    def test_command_counter_regression_fails(self):
        self.fast['samples'][12]['commandProbeCount'] = 0
        self.assertFalse(self.check()['success'])

    def test_completion_counter_or_timestamp_regression_fails(self):
        for key, value in [('reconcileCount', 9), ('lastReconcileTime', stamp(98))]:
            with self.subTest(key=key):
                old = self.samples[1]['controllers'][0][key]
                self.samples[1]['controllers'][0][key] = value
                self.assertFalse(self.check()['success'])
                self.samples[1]['controllers'][0][key] = old

    def test_inconsistent_completion_fields_fail(self):
        self.samples[1]['controllers'][0]['reconcileCount'] = 11
        self.assertFalse(self.check()['success'])

    def test_new_or_current_error_fails(self):
        for key, value in [('currentError', True), ('reconcileErrorCount', 38), ('lastSuccessTime', stamp(98))]:
            with self.subTest(key=key):
                old = self.samples[1]['controllers'][0][key]
                self.samples[1]['controllers'][0][key] = value
                self.assertFalse(self.check()['success'])
                self.samples[1]['controllers'][0][key] = old

    def test_ambiguous_or_wrong_controller_fails(self):
        self.samples[1]['controllers'].append(copy.deepcopy(self.samples[1]['controllers'][0]))
        self.assertFalse(self.check()['success'])
        self.samples[1]['controllers'] = [{'name': 'other'}]
        self.assertFalse(self.check()['success'])

    def test_malformed_counter_and_timestamp_fail(self):
        for value in [True, -1, 1.5, '101', None]:
            with self.subTest(value=value):
                self.fast['samples'][10]['commandProbeCount'] = value
                self.assertFalse(self.check()['success'])
        self.fast['samples'][10]['commandProbeCount'] = 100
        self.fast['samples'][10]['epoch'] = float('nan')
        self.assertFalse(self.check()['success'])


if __name__ == '__main__':
    unittest.main()
