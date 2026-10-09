import copy
import datetime
import importlib.util
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

    def check(self):
        return module.evaluate_arp_controller_progress(self.samples, self.fast,
            observer_pid=20, observer_start_ticks='1000', observer_since=stamp(50))

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
