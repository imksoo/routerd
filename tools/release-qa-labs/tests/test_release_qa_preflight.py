"""Preparation mistakes must fail before a certification driver can run."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from test_rollout_baseline import fixture, build

ROOT = Path(__file__).resolve().parents[3]


def module(name):
    spec = importlib.util.spec_from_file_location(name, ROOT / 'scripts' / (name + '.py'))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


class PreparationInputTests(unittest.TestCase):
    def setUp(self):
        self.m = module('release_qa_preflight')
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        self.contract = {'runId': 'new-run', 'execution': {}}

    def bound(self, name, value):
        path = self.root / name
        path.write_text(json.dumps(value))
        return {'path': str(path), 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}

    def plans(self):
        self.contract['execution']['qualification_plans'] = {
            stage: self.bound(stage + '.json', {'runId': 'new-run', 'stage': stage})
            for stage in ('canary', 'final')}

    def baseline(self):
        guest, media = fixture()
        generated = build()
        generated['guest_ssh_applicable'] = False  # extra caller metadata is permitted
        evidence = {'runId': 'new-run', 'success': True,
                    'generatedBaseline': self.bound('baseline.json', [generated]),
                    'nodes': [{'node': 'node1', 'host': 'host1', 'vmid': 101,
                               'expectedVersion': 'routerd test (test)', 'managementDevice': 'eth0',
                               'guest': self.bound('guest.json', {'returncode': 0, 'observed': guest}),
                               'media': self.bound('media.json', {'returncode': 0, 'observed': media})}]}
        self.contract['execution']['nodes'] = [{'name': 'node1', 'host': 'host1', 'vmid': 101}]
        self.contract['execution']['baseline_builder_binding'] = {
            'evidence': self.bound('builder.json', evidence)}
        return evidence

    def update_evidence(self, evidence):
        self.contract['execution']['baseline_builder_binding']['evidence'] = self.bound('builder.json', evidence)

    def consumer(self, evidence):
        execution = self.contract['execution']
        source = self.root / 'consumer.py'
        source.write_text('# saved consumer source\n')
        ref = {'path': str(source), 'sha256': hashlib.sha256(source.read_bytes()).hexdigest()}
        execution['script_sha256'] = {ref['path']: ref['sha256']}
        node = evidence['nodes'][0]
        report = {'runId': 'new-run', 'success': True, 'machineMutations': [], 'formalQualification': False,
                  'evaluator': ref, 'caller': ref, 'common': ref,
                  'nodes': [{'node': 'node1', 'host': 'host1', 'vmid': 101, 'success': True,
                    'guestAssessment': {'pass': True, 'checks': {'management_baseline': True}},
                    'mediaAssessment': {'pass': True, 'checks': {'persistent_config': True}},
                    'guest': node['guest'], 'media': node['media'],
                    'baseline': evidence['generatedBaseline'], 'builderEvidence': execution['baseline_builder_binding']['evidence']}]}
        return report

    def bind_consumer(self, report):
        self.contract['execution']['baseline_builder_binding']['actual_all10_prestop'] = self.bound('consumer.json', report)

    def test_new_phase_ids_and_bound_bytes_pass(self):
        self.plans()
        self.assertEqual(self.m.validate_preparation_inputs(self.contract)['plans'], 2)

    def test_copied_r3_plan_id_is_rejected_even_with_current_hash(self):
        self.plans()
        self.contract['execution']['qualification_plans']['canary'] = self.bound(
            'canary.json', {'runId': 'old-run', 'stage': 'canary'})
        with self.assertRaisesRegex(ValueError, 'identity'):
            self.m.validate_preparation_inputs(self.contract)

    def test_wrong_phase_missing_phase_and_changed_plan_are_rejected(self):
        for change in ('stage', 'missing', 'bytes'):
            self.plans()
            plans = self.contract['execution']['qualification_plans']
            if change == 'stage':
                plans['canary'] = self.bound('canary.json', {'runId': 'new-run', 'stage': 'final'})
            elif change == 'missing':
                del plans['final']
            else:
                Path(plans['canary']['path']).write_text('{}')
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.m.validate_preparation_inputs(self.contract)

    def test_baseline_is_rebuilt_from_actual_saved_inputs(self):
        self.baseline()
        self.assertEqual(self.m.validate_preparation_inputs(self.contract)['baselineNodes'], 1)

    def test_r4_missing_management_address_in_generated_baseline_fails(self):
        e = self.baseline()
        generated = build()
        del generated['management_address']
        e['generatedBaseline'] = self.bound('baseline.json', [generated])
        self.update_evidence(e)
        with self.assertRaisesRegex(ValueError, 'management_address'):
            self.m.validate_preparation_inputs(self.contract)

    def test_bad_saved_guest_and_changed_input_are_not_replaced_by_summary_pass(self):
        for change in ('unbound', 'hash', 'transport', 'identity'):
            e = self.baseline()
            guest, _ = fixture()
            if change == 'unbound':
                guest['management'][0]['phase'] = 'Pending'
            ref = self.bound('guest.json', {'returncode': 1 if change == 'transport' else 0, 'observed': guest})
            e['nodes'][0]['guest'] = ref
            if change == 'hash':
                Path(ref['path']).write_text('{}')
            if change == 'identity':
                e['nodes'][0]['vmid'] = 102
            self.update_evidence(e)
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.m.validate_preparation_inputs(self.contract)

    def test_missing_duplicate_and_foreign_nodes_are_rejected(self):
        for change in ('missing', 'duplicate', 'foreign'):
            e = self.baseline()
            if change == 'missing':
                e['nodes'] = []
            elif change == 'duplicate':
                e['nodes'].append(copy.deepcopy(e['nodes'][0]))
            else:
                e['nodes'][0]['node'] = 'other'
            self.update_evidence(e)
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.m.validate_preparation_inputs(self.contract)

    def test_historical_builder_report_cannot_be_reused(self):
        e = self.baseline()
        e['runId'] = 'old-run'
        self.update_evidence(e)
        with self.assertRaisesRegex(ValueError, 'identity'):
            self.m.validate_preparation_inputs(self.contract)

    def test_phased_rollout_requires_complete_actual_consumer(self):
        self.plans()
        e = self.baseline()
        with self.assertRaisesRegex(ValueError, 'consumer replay'):
            self.m.validate_preparation_inputs(self.contract)
        self.bind_consumer(self.consumer(e))
        self.assertEqual(self.m.validate_preparation_inputs(self.contract)['consumerNodes'], 1)
        del self.contract['execution']['baseline_builder_binding']
        with self.assertRaisesRegex(ValueError, 'baseline_builder_binding'):
            self.m.validate_preparation_inputs(self.contract)

    def test_consumer_empty_checks_wrong_baseline_and_unbound_source_fail(self):
        for change in ('empty', 'failed', 'baseline', 'source', 'old-run'):
            e = self.baseline()
            report = self.consumer(e)
            if change == 'empty':
                report['nodes'][0]['guestAssessment']['checks'] = {}
            elif change == 'failed':
                report['nodes'][0]['guestAssessment']['checks']['management_baseline'] = False
            elif change == 'baseline':
                report['nodes'][0]['baseline'] = self.bound('other-baseline.json', [])
            elif change == 'source':
                Path(report['evaluator']['path']).write_text('# changed after replay\n')
            else:
                report['runId'] = 'old-run'
            self.bind_consumer(report)
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.m.validate_preparation_inputs(self.contract)

    def test_consumer_failed_saved_transport_is_not_replaced_by_summary_pass(self):
        for kind in ('guest', 'media'):
            e = self.baseline()
            report = self.consumer(e)
            report['nodes'][0][kind] = self.bound(
                'consumer-' + kind + '.json', {'returncode': 1, 'observed': {}})
            self.bind_consumer(report)
            with self.subTest(kind=kind), self.assertRaisesRegex(ValueError, 'consumer transport'):
                self.m.validate_preparation_inputs(self.contract)


class AdmissionTests(unittest.TestCase):
    def private_contract(self, root, body='self.assertTrue(True)', decorator=''):
        path = root / 'test_private_caller.py'
        path.write_text('import unittest\nclass Caller(unittest.TestCase):\n'
                        f' {decorator}\n def test_actual_path(self):\n  {body}\n')
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        return {'execution': {'qualification_plans': {'canary': {}, 'final': {}}, 'nodes': ['node'],
                 'script_sha256': {str(path): digest}, 'selfprobe_plan': {},
                 'offline_rehearsal': {'testFiles': [{'path': str(path), 'sha256': digest}],
                   'coverage': {key: ['test_private_caller.Caller.test_actual_path'] for key in
                     ['phase-contract', 'rollout-baseline', 'self-stimulus', 'positive-stimulus',
                      'collector-health', 'controller-progress']}}}}

    def test_actual_private_tests_execute_and_cannot_report_success_for_failure(self):
        m = module('release_qa_preflight')
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            contract = self.private_contract(root)
            self.assertEqual(m.run_private_rehearsal(contract)['tests'], 1)
            for body, decorator in [('self.fail("actual caller bug")', ''),
                                    ('self.assertTrue(True)', '@unittest.skip("missing evidence")'),
                                    ('__import__("socket").socket()', '')]:
                contract = self.private_contract(root, body, decorator)
                with self.subTest(body=body, decorator=decorator), self.assertRaises(ValueError):
                    m.run_private_rehearsal(contract)

    def test_private_test_coverage_and_bound_code_are_mandatory(self):
        m = module('release_qa_preflight')
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for change in ('missing', 'self', 'case', 'source', 'empty'):
                contract = self.private_contract(root)
                execution = contract['execution']
                if change == 'missing':
                    del execution['offline_rehearsal']
                elif change == 'self':
                    del execution['offline_rehearsal']['coverage']['self-stimulus']
                elif change == 'case':
                    execution['offline_rehearsal']['coverage']['self-stimulus'] = ['missing.Case.test_missing']
                elif change == 'empty':
                    execution['offline_rehearsal']['testFiles'] = []
                else:
                    (root / 'test_private_caller.py').write_text('# changed after freeze\n')
                with self.subTest(change=change), self.assertRaises(ValueError):
                    m.run_private_rehearsal(contract)

    def test_actual_bad_plan_gate_leaves_driver_uncalled_and_writes_failure(self):
        cert = module('release_certification')
        prep = PreparationInputTests()
        prep.setUp()
        self.addCleanup(prep.doCleanups)
        prep.plans()
        prep.contract['providers'] = [{'name': 'pve'}]
        prep.contract['execution']['qualification_plans']['final'] = prep.bound(
            'final.json', {'runId': 'old-run', 'stage': 'final'})
        path = prep.root / 'contract.json'
        path.write_text(json.dumps(prep.contract))
        out = prep.root / 'certificate.json'
        with patch.object(cert, 'validate_contract', return_value='a' * 40), \
             patch.object(cert, 'run_driver') as driver:
            with self.assertRaisesRegex(cert.ContractError, 'identity'):
                cert.command_certify('pve', ['--environment', 'test', '--topology', 'full',
                    '--contract', str(path), '--driver', '/must-not-run', '--out', str(out)])
            driver.assert_not_called()
        self.assertFalse(out.exists())
        self.assertEqual(json.loads(out.with_suffix('.offline-preflight.json').read_text())['status'], 'fail')

    def test_network_and_process_attempts_fail_offline_suite(self):
        m = module('release_qa_preflight')
        for action in (lambda: m.socket.socket(), lambda: m.subprocess.Popen(['/bin/false'])):
            suite = unittest.TestSuite([unittest.FunctionTestCase(action)])
            with patch.object(m, 'load_suite', return_value=suite):
                with self.assertRaisesRegex(ValueError, 'network or subprocess'):
                    m.run_regressions()

    def test_failed_offline_gate_never_calls_certification_driver(self):
        cert = module('release_certification')
        contract = {'providers': [{'name': 'pve'}]}
        with patch.object(cert, 'load_json', return_value=contract), \
             patch.object(cert, 'validate_contract', return_value='a' * 40), \
             patch.object(cert, 'offline_preflight', side_effect=cert.ContractError('offline regression failed')), \
             patch.object(cert, 'run_driver') as driver:
            with self.assertRaisesRegex(cert.ContractError, 'offline regression failed'):
                cert.command_certify('pve', ['--environment', 'test', '--topology', 'full',
                    '--contract', '/unused', '--driver', '/must-not-run', '--out', '/unused-output'])
            driver.assert_not_called()

    def test_missing_suite_fails_closed(self):
        m = module('release_qa_preflight')
        with patch.object(m, 'CHECKS', ('missing_required_test_module',)):
            with self.assertRaisesRegex(ValueError, 'offline regressions'):
                m.run_regressions()

    def test_skipped_required_suite_is_failure(self):
        m = module('release_qa_preflight')
        @unittest.skip('required evidence unavailable')
        def skipped(self):
            pass
        case = type('Skipped', (unittest.TestCase,), {'test_required': skipped})
        suite = unittest.TestLoader().loadTestsFromTestCase(case)
        with patch.object(m, 'load_suite', return_value=suite):
            with self.assertRaisesRegex(ValueError, 'offline regressions'):
                m.run_regressions()

    def test_zero_cases_cannot_pass(self):
        m = module('release_qa_preflight')
        with patch.object(m, 'load_suite', return_value=unittest.TestSuite()):
            with self.assertRaisesRegex(ValueError, 'offline regressions'):
                m.run_regressions()


if __name__ == '__main__':
    unittest.main()
