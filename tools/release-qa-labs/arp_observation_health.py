"""Read-only collector health proof before ARP test stimuli.

Run this reader on the guest so freshness uses a single monotonic clock. The
coordinator must bind the PID/start ticks/boot ID from that run's ready record.
A terminal result always forbids another stimulus, even if it has no errors.
"""
import json
import math
from pathlib import Path
import time

MAX_PROGRESS_AGE = 10.0


def _number(value):
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or value < 0:
        raise ValueError('invalid progress time or bound')
    return float(value)


def _read_json(path):
    if not path.exists():
        return None
    with path.open() as stream:
        text = stream.read(1048577)
    if len(text) > 1048576:
        raise ValueError('collector metadata exceeds bound')
    return json.loads(text)


def _process(pid, proc_root):
    if isinstance(pid, bool) or not isinstance(pid, int) or pid <= 0:
        raise ValueError('invalid collector process ID')
    try:
        stat = (proc_root / str(pid) / 'stat').read_text().rsplit(') ', 1)[1].split()
    except FileNotFoundError:
        return {'pid': pid, 'alive': False}
    return {'pid': pid, 'alive': stat[0] not in ('Z', 'X'), 'startTicks': stat[19]}


def read_collector_state(directory, *, proc_root='/proc'):
    """Fresh run-owned metadata plus current process identities; no mutations."""
    directory, proc_root = Path(directory), Path(proc_root)
    ownership = _read_json(directory / 'ownership.json')
    if not isinstance(ownership, dict):
        raise ValueError('missing collector ownership')
    state = {'ownership': ownership,
             'process': _process(ownership['pid'], proc_root),
             'captureProcess': _process(ownership['capturePid'], proc_root),
             'bootId': (proc_root / 'sys/kernel/random/boot_id').read_text().strip(),
             'progress': _read_json(directory / 'progress.json')}
    # Check terminal/error markers after progress, so a freshly published
    # failure cannot be hidden by an older successful progress snapshot.
    state['counterError'] = _read_json(directory / 'counter-error.json')
    state['terminal'] = _read_json(directory / 'complete.json')
    state['checkedMonotonic'] = time.monotonic()
    state['checkedEpoch'] = time.time()
    return state


def assert_collector_running(state, *, expected_pid, expected_start_ticks,
                             expected_boot_id, requires_counters=False,
                             max_progress_age=MAX_PROGRESS_AGE):
    """Raise with the original collector error before a new stimulus is sent."""
    maximum = _number(max_progress_age)
    if not 0 < maximum <= MAX_PROGRESS_AGE:
        raise ValueError('progress age exceeds bound')
    if state['counterError'] is not None:
        raise ValueError('counter collector failed: ' + json.dumps(state['counterError'], sort_keys=True))
    if state['terminal'] is not None:
        raise ValueError('collector already finished: ' + json.dumps(state['terminal'], sort_keys=True))
    ownership, process, capture = state['ownership'], state['process'], state['captureProcess']
    expected = (expected_pid, str(expected_start_ticks), expected_boot_id)
    if (isinstance(expected_pid, bool) or not isinstance(expected_pid, int) or expected_pid <= 0
            or not expected_start_ticks or not expected_boot_id):
        raise ValueError('missing expected collector identity')
    if (ownership['pid'], str(ownership['startTicks']), ownership['bootId']) != expected:
        raise ValueError('collector ownership changed')
    if (not process['alive'] or process['pid'] != expected_pid
            or str(process.get('startTicks')) != str(expected_start_ticks)
            or state['bootId'] != expected_boot_id):
        raise ValueError('collector process/boot changed or exited')
    if (not capture['alive'] or capture['pid'] != ownership['capturePid']
            or str(capture.get('startTicks')) != str(ownership['captureStartTicks'])):
        raise ValueError('capture process changed or exited')
    progress = state['progress']
    if not isinstance(progress, dict):
        raise ValueError('missing collector progress')
    if (progress['collectorState'] != 'running' or progress.get('collectorErrors')
            or progress['collectorPID'] != expected_pid or progress['bootId'] != expected_boot_id):
        raise ValueError('collector progress failed or identity changed: ' + json.dumps(progress.get('collectorErrors', [])))
    age = _number(state['checkedMonotonic']) - _number(progress['completedMonotonic'])
    if not 0 <= age <= maximum:
        raise ValueError('stale or future collector progress')
    if requires_counters:
        health = progress['counterSamplerHealth']
        count = health['sampleCount']
        if (health['errors'] or health['running'] is not True
                or isinstance(count, bool) or not isinstance(count, int) or count <= 0):
            raise ValueError('counter sampler failed or has no live samples')
    return {'success': True, 'progressAgeSeconds': age,
            'collectorPID': expected_pid, 'checkedEpoch': state['checkedEpoch']}
