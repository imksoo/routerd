#!/usr/bin/env python3
"""Validate saved release preparation inputs without contacting providers."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parent.parent
FRAMEWORK = ROOT / 'tools/release-qa-labs'


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


def check(contract):
    inputs = validate_preparation_inputs(contract)
    return {'status': 'pass', 'offlineOnly': True, 'preparation': inputs}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--contract', type=Path, required=True)
    parser.add_argument('--out', type=Path)
    args = parser.parse_args()
    try:
        raw = args.contract.read_bytes()
        report = check(json.loads(raw))
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
