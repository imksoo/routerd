#!/usr/bin/env python3
"""Host-safe regression admission and validation of saved preparation inputs.

No provider, guest, certification driver or phase-plan command is executed here.
The fixed suite excludes host integration and external CLI tests. Results are
fresh checks, not a cached PASS that can survive a change to the implementation.
"""
import argparse
from contextlib import contextmanager
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parent.parent
FRAMEWORK = ROOT / 'tools/release-qa-labs'
CHECKS = (
    'test_arp_command_evidence', 'test_arp_probe_attribution',
    'test_arp_request_receipt', 'test_arp_control_schedule',
    'test_arp_stimulus_clock_margin', 'test_arp_controller_progress',
    'test_arp_counter_sampler', 'test_arp_observation_health',
    'test_rollout_baseline', 'test_iso_storage_evidence',
    'test_run_id.RunIDTests.test_admitted_ids_fit_generated_hostname_and_iam_role',
    'test_run_id.RunIDTests.test_overlong_or_unsafe_ids_are_rejected',
    'test_release_qa_preflight.PreparationInputTests',
    'test_saved_arp_observations', 'test_arp_stimulus_evidence',
    'test_qualification_outcome',
)
REQUIRED_REGRESSIONS = {
    'test_arp_command_evidence.CommandEvidenceTests.test_aggregate_counters_never_replace_absent_record',
    'test_arp_command_evidence.CommandEvidenceTests.test_explicit_target_record_accepts_interleaved_work',
    'test_arp_request_receipt.RequestReceiptTests.test_select_start_and_recorded_at_cannot_backdate_receipt',
    'test_arp_request_receipt.RequestPacketTests.test_plain_and_parenthesized_target_hardware_are_both_parsed',
    'test_release_qa_preflight.PreparationInputTests.test_copied_r3_plan_id_is_rejected_even_with_current_hash',
    'test_release_qa_preflight.PreparationInputTests.test_r4_missing_management_address_in_generated_baseline_fails',
    'test_release_qa_preflight.PreparationInputTests.test_phased_rollout_requires_complete_actual_consumer',
    'test_arp_control_schedule.PositiveControlScheduleTests.test_receipt_is_not_an_input_for_skipping_an_opportunity',
    'test_arp_stimulus_clock_margin.StimulusClockMarginTests.test_immediate_arp_can_fail_with_actual_r6_clock_bounds',
    'test_iso_storage_evidence.ISOStorageEvidenceTests.test_unknown_or_malformed_lines_are_not_discarded',
    'test_arp_counter_sampler.CadenceTests.test_170ms_read_is_kept_under_existing_400ms_coverage',
    'test_arp_counter_sampler.CadenceTests.test_adjacent_read_span_includes_previous_read_and_wait',
    'test_arp_observation_health.HealthTests.test_terminal_failure_wins_over_healthy_progress',
    'test_saved_arp_observations.SavedObservationTests.test_actual_r12_boundary_completion_is_retained',
    'test_saved_arp_observations.SavedObservationTests.test_same_trace_without_later_reconcile_fails',
    'test_arp_observation_health.HealthTests.test_pid_reuse_stopped_capture_or_changed_boot_are_rejected',
    'test_arp_stimulus_evidence.StimulusEvidenceTests.test_observation_failures_are_distinct_from_proven_violations',
    'test_qualification_outcome.QualificationOutcomeTests.test_bare_product_label_is_inconclusive',
    'test_qualification_outcome.QualificationOutcomeTests.test_explicit_product_evidence_survives_abort_and_cleanup_failure',
}


def case_ids(suite):
    for item in suite:
        if isinstance(item, unittest.TestSuite):
            yield from case_ids(item)
        else:
            yield item.id()


def load_suite():
    # A long-lived caller must not test modules cached from an older source.
    for name, module in list(sys.modules.items()):
        path = getattr(module, '__file__', None)
        if path and Path(path).resolve().parent == FRAMEWORK / 'tests':
            del sys.modules[name]
    sys.path.insert(0, str(FRAMEWORK / 'tests'))
    try:
        suite = unittest.TestLoader().loadTestsFromNames(CHECKS)
        missing = REQUIRED_REGRESSIONS - set(case_ids(suite))
        require(not missing, 'required offline regressions missing: ' + ', '.join(sorted(missing)))
        return suite
    finally:
        sys.path.pop(0)


def run_regressions():
    try:
        with offline_io_guard():
            return run_suite(load_suite())
    except SystemExit as exc:
        raise ValueError('offline regressions exited during loading') from exc


@contextmanager
def offline_io_guard():
    def forbidden(*args, **kwargs):
        raise AssertionError('offline regression attempted network or subprocess execution')
    with patch.object(subprocess, 'Popen', side_effect=forbidden), \
         patch.object(os, 'system', side_effect=forbidden), \
         patch.object(socket, 'socket', side_effect=forbidden):
        yield


def run_suite(suite):
    stream = io.StringIO()
    with offline_io_guard():
        result = unittest.TextTestRunner(stream=stream, verbosity=1).run(suite)
    if not result.wasSuccessful() or result.testsRun == 0 or result.skipped:
        raise ValueError('required offline regressions failed:\n' + stream.getvalue())
    return {'tests': result.testsRun, 'failures': 0, 'skips': 0}


def run_private_rehearsal(contract):
    """Run the actual bound caller tests, not an unverified PASS receipt."""
    execution = contract.get('execution', {})
    if not execution.get('qualification_plans') or not execution.get('nodes'):
        return None
    try:
        rehearsal = execution['offline_rehearsal']
        files, coverage = rehearsal['testFiles'], rehearsal['coverage']
        require(isinstance(files, list) and bool(files), 'missing private test files')
        required = {'phase-contract', 'rollout-baseline'}
        if 'selfprobe_plan' in execution:
            required |= {'self-stimulus', 'positive-stimulus', 'collector-health', 'controller-progress'}
        require(isinstance(coverage, dict) and required <= set(coverage), 'missing private caller coverage')
        require(all(isinstance(ids, list) and ids and all(isinstance(i, str) and i for i in ids)
                    for ids in coverage.values()), 'invalid private caller test IDs')
        bound = execution['script_sha256']
        # Check the actual dependencies, not only the test file. The existing
        # contract author/driver still owns completeness of this dependency set.
        before = {path: hashlib.sha256(Path(path).read_bytes()).hexdigest() for path in bound}
        require(before == bound, 'private caller dependency changed')
        suite, names = unittest.TestSuite(), set()
        for reference in files:
            path = Path(reference['path'])
            require(path.is_absolute() and path.suffix == '.py' and path.stem.isidentifier(), 'invalid private test path')
            require(path.stem not in names, 'duplicate private test module')
            names.add(path.stem)
            require(bound[str(path)] == reference['sha256'], 'private test source not bound')
            spec = importlib.util.spec_from_file_location(path.stem, path)
            module = importlib.util.module_from_spec(spec)
            # Imports may execute code too. Block accidental remote work there
            # as well as in test methods. This is not a security sandbox.
            sys.path.insert(0, str(path.parent))
            try:
                with offline_io_guard():
                    # Execute the bytes just bound by SHA256. A same-size edit
                    # within one mtime tick must not load old test bytecode.
                    exec(compile(path.read_bytes(), str(path), 'exec'), module.__dict__)
                    tests = unittest.TestLoader().loadTestsFromModule(module)
            finally:
                sys.path.pop(0)
            require(tests.countTestCases() > 0, 'empty private caller test module')
            suite.addTests(tests)
        expected = {test for ids in coverage.values() for test in ids}
        require(expected <= set(case_ids(suite)), 'declared private caller test missing')
        result = run_suite(suite)
        require(before == {path: hashlib.sha256(Path(path).read_bytes()).hexdigest() for path in bound},
                'private caller dependency changed during rehearsal')
        return {**result, 'coverage': coverage, 'sourceSha256': before}
    except (OSError, KeyError, TypeError, AttributeError, ImportError, SyntaxError, AssertionError, SystemExit) as exc:
        raise ValueError('private offline rehearsal: ' + str(exc)) from exc


def require(ok, message):
    if not ok:
        raise ValueError(message)


def bound_json(reference):
    require(isinstance(reference, dict), 'missing bound input')
    path = Path(reference['path'])
    require(path.is_absolute(), 'bound input must have an absolute path')
    data = path.read_bytes()
    require(hashlib.sha256(data).hexdigest() == reference['sha256'], 'bound input checksum mismatch: ' + str(path))
    return json.loads(data)


def node_map(rows, name):
    require(isinstance(rows, list) and bool(rows), 'missing preparation nodes')
    result = {row[name]: row for row in rows}
    require(len(result) == len(rows), 'duplicate preparation nodes')
    return result


def validate_preparation_inputs(contract):
    """Validate saved phase plans and retained-media baseline producer/consumer data.

    These execution extensions belong to phased retained-ISO qualification.
    Cloud contracts without them retain their existing provider admission gates.
    This does not certify current live health or freshness of guest observations.
    """
    counts = {'plans': 0, 'baselineNodes': 0, 'consumerNodes': 0}
    try:
        execution = contract.get('execution', {})
        plans = execution.get('qualification_plans')
        if plans is not None:
            require(isinstance(plans, dict) and set(plans) == {'canary', 'final'}, 'missing or unknown qualification phase')
            for stage, reference in plans.items():
                plan = bound_json(reference)
                require(plan['runId'] == contract['runId'] and plan['stage'] == stage, 'phase plan identity mismatch')
                counts['plans'] += 1

        binding = execution.get('baseline_builder_binding')
        if plans is not None and execution.get('nodes'):
            require(binding is not None, 'phased retained rollout requires baseline_builder_binding')
        if binding is None:
            return counts
        evidence = bound_json(binding['evidence'])
        require(evidence['runId'] == contract['runId'] and evidence['success'] is True, 'baseline report identity or success mismatch')
        expected = node_map(execution['nodes'], 'name')
        inputs = node_map(evidence['nodes'], 'node')
        generated = node_map(bound_json(evidence['generatedBaseline']), 'node')
        require(set(expected) == set(inputs) == set(generated), 'baseline node coverage mismatch')
        spec = importlib.util.spec_from_file_location('rollout_baseline_preflight', FRAMEWORK / 'rollout_baseline.py')
        baseline = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(baseline)
        for name, row in inputs.items():
            require(all(row[key] == expected[name][key] for key in ('host', 'vmid')), 'baseline node identity mismatch')
            guest, media = bound_json(row['guest']), bound_json(row['media'])
            require(type(guest['returncode']) is int and guest['returncode'] == 0
                    and type(media['returncode']) is int and media['returncode'] == 0, 'baseline transport failed')
            rebuilt = baseline.build_rollout_baseline(guest['observed'], media['observed'],
                node=name, host=row['host'], vmid=row['vmid'], expected_version=row['expectedVersion'],
                management_device=row['managementDevice'])
            for key, value in rebuilt.items():
                require(key in generated[name] and generated[name][key] == value, 'generated baseline mismatch: ' + name + '.' + key)
            counts['baselineNodes'] += 1

        consumer_ref = binding.get('actual_all10_prestop')
        if plans is not None:
            require(consumer_ref is not None, 'missing actual pre-stop consumer replay')
        if consumer_ref is not None:
            consumer = bound_json(consumer_ref)
            require(consumer['runId'] == contract['runId'] and consumer['success'] is True,
                    'consumer report identity or success mismatch')
            require(consumer['machineMutations'] == [] and consumer['formalQualification'] is False,
                    'consumer replay must use saved inputs only')
            rows = node_map(consumer['nodes'], 'node')
            require(set(rows) == set(expected), 'consumer node coverage mismatch')
            for key in ('evaluator', 'caller', 'common'):
                ref = consumer[key]
                require(execution['script_sha256'][ref['path']] == ref['sha256'], 'consumer source not bound to contract')
                require(hashlib.sha256(Path(ref['path']).read_bytes()).hexdigest() == ref['sha256'], 'consumer source changed')
            for name, row in rows.items():
                require(row['success'] is True and all(row[key] == expected[name][key] for key in ('host', 'vmid')),
                        'consumer node identity or success mismatch')
                require(row['baseline'] == evidence['generatedBaseline'] and row['builderEvidence'] == binding['evidence'],
                        'consumer used a different baseline')
                for kind in ('guestAssessment', 'mediaAssessment'):
                    assessment = row[kind]
                    require(assessment['pass'] is True and bool(assessment['checks'])
                            and all(value is True for value in assessment['checks'].values()), 'consumer check failed')
                for key in ('guest', 'media'):
                    observed = bound_json(row[key])
                    require(type(observed['returncode']) is int and observed['returncode'] == 0,
                            'consumer transport failed: ' + name + '.' + key)
                    require(isinstance(observed.get('observed'), dict) and bool(observed['observed']),
                            'consumer observation missing: ' + name + '.' + key)
                counts['consumerNodes'] += 1
    except (OSError, KeyError, TypeError, AttributeError) as exc:
        raise ValueError('incomplete preparation evidence: ' + str(exc)) from exc
    return counts


def source_hashes():
    paths = list(FRAMEWORK.glob('*.py')) + list((FRAMEWORK / 'tests').rglob('*.py'))
    paths += list((FRAMEWORK / 'tests/fixtures').rglob('*.json'))
    paths += [Path(__file__), ROOT / 'scripts/release_certification.py',
              ROOT / 'docs/releases/manifests/release-environment-certification.schema.json']
    return {str(p.relative_to(ROOT)): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(set(paths))}


def check(contract=None):
    before = source_hashes()
    regressions = run_regressions()
    inputs = validate_preparation_inputs(contract) if contract is not None else None
    private = run_private_rehearsal(contract) if contract is not None else None
    require(before == source_hashes(), 'offline check source changed during validation')
    return {'status': 'pass', 'offlineOnly': True, 'regressions': regressions,
            'preparation': inputs, 'privateRehearsal': private, 'sourceSha256': before}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--contract', type=Path)
    parser.add_argument('--out', type=Path)
    args = parser.parse_args()
    try:
        raw = args.contract.read_bytes() if args.contract else None
        report = check(json.loads(raw) if raw is not None else None)
        if args.contract:
            require(raw == args.contract.read_bytes(), 'contract changed during offline preflight')
            report['contractSha256'] = hashlib.sha256(raw).hexdigest()
    except (OSError, ValueError) as exc:
        if args.out:
            args.out.parent.mkdir(parents=True, exist_ok=True)
            args.out.write_text(json.dumps({'status': 'fail', 'error': str(exc)}) + '\n')
        raise
    text = json.dumps(report, sort_keys=True, indent=2) + '\n'
    if args.out:
        args.out.parent.mkdir(parents=True, exist_ok=True)
        args.out.write_text(text)
    else:
        print(text, end='')


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError) as exc:
        print('release QA offline preflight: ' + str(exc), file=sys.stderr)
        raise SystemExit(1)
