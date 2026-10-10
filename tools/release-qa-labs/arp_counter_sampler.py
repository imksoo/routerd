"""Receiver-local read-only counter and full-key DB arrival sampler.
No capture, stimulus, database writes, process creation or service configuration operations.
Use within run-owned collector; call stop/join before serializing results.
"""
import http.client
import copy
import math
from contextlib import closing
import sqlite3
import hashlib
import json
from pathlib import Path
import socket
import threading
import time

COUNTERS = ('commandProbeCount', 'probeCount', 'proactiveCount',
            'requestObservedCount', 'scanCount')
MAX_SAMPLE_GAP = 0.4
MAX_SLOW_SAMPLES = 32

class UnixHTTP(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__('localhost', timeout=0.08)
        self.path = path
    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.path)

class CounterSampler:
    def __init__(self, socket_path, pid, interval=0.1, duration=400,
                 db_path="/var/lib/routerd/routerd.db", max_sample_gap=MAX_SAMPLE_GAP,
                 on_error=None):
        if (any(isinstance(x, bool) or not isinstance(x, (int, float)) or not math.isfinite(x)
                for x in (interval, duration, max_sample_gap)) or
                not 0.1 <= interval <= 0.2 or not 0 < duration <= 400 or
                not interval <= max_sample_gap <= MAX_SAMPLE_GAP):
            raise ValueError('sampler bounds')
        self.socket_path, self.pid = socket_path, int(pid)
        self.interval, self.duration = interval, duration
        self.samples, self.errors = [], []
        self.db_path = db_path
        self.db_states, self.db_state_bytes = [], 0
        self.stop_event = threading.Event()
        self.thread = threading.Thread(target=self._run, daemon=True)
        self.maximum_samples = int(duration / interval) + 2
        self.max_sample_gap, self.on_error = max_sample_gap, on_error
        self._current_sample, self._stage_seconds = {}, {}
        self._stage, self._observer_identity = None, None
        self.diagnostics = {'missedScheduleTicks': 0, 'overCadenceSamples': 0,
                            'maximumCycleSeconds': 0, 'maximumCoverageSeconds': 0,
                            'slowSamples': [], 'slowSamplesTruncated': 0,
                            'stageTiming': {}}

    def _timed(self, stage, operation):
        self._stage = stage
        began = time.monotonic()
        try:
            return operation()
        finally:
            duration = time.monotonic() - began
            self._stage_seconds[stage] = duration
            total = self.diagnostics['stageTiming'].setdefault(
                stage, {'count': 0, 'totalSeconds': 0, 'maximumSeconds': 0})
            total['count'] += 1
            total['totalSeconds'] += duration
            total['maximumSeconds'] = max(total['maximumSeconds'], duration)

    def _error(self, error):
        record = {'epoch': time.time(), 'error': str(error), 'stage': self._stage,
                  'sample': copy.deepcopy(self._current_sample),
                  'stageSeconds': dict(self._stage_seconds)}
        self.errors.append(record)
        # The adapter may publish a run-owned error marker. No filesystem writes
        # are performed by this read-only sampler itself.
        if self.on_error is not None:
            try:
                self.on_error(copy.deepcopy(record))
            except Exception as exc:
                self.errors.append({'epoch': time.time(), 'error': 'error publication failed: ' + str(exc)})
    def identity(self):
        base = Path('/proc') / str(self.pid)
        args = (base / 'cmdline').read_bytes().decode().split('\0')
        if not args or Path(args[0]).name != 'routerd-arp-observer':
            raise RuntimeError('observer process identity mismatch')
        if '--socket' not in args or args[args.index('--socket') + 1] != self.socket_path:
            raise RuntimeError('observer socket identity mismatch')
        stat = (base / 'stat').read_text()
        return stat.rsplit(') ', 1)[1].split()[19]
    def read_db(self):
        # mode=ro and query_only; fetchall completes the read transaction before
        # the authoritative receiver-local receipt timestamp is taken.
        began = time.time()
        with closing(sqlite3.connect('file:' + self.db_path + '?mode=ro', uri=True,
                                     timeout=0.03)) as db:
            db.row_factory = sqlite3.Row
            db.execute('PRAGMA query_only=ON')
            cursor = db.execute('SELECT * FROM federation_events WHERE type=? ORDER BY id LIMIT 257',
                                ('routerd.mobility.arp.request.observed',))
            rows = [dict(x) for x in cursor.fetchall()]
            cursor.close()
        for row in rows:
            row['payload'] = json.loads(row['payload'])
        completed = time.time()
        if len(rows) > 256:
            raise RuntimeError('fast all-request DB row bound exceeded')
        raw = json.dumps(rows, ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode()
        digest = hashlib.sha256(raw).hexdigest()
        if not self.db_states or self.db_states[-1]['sha256'] != digest:
            if len(self.db_states) >= 512 or self.db_state_bytes + len(raw) > 2097152:
                raise RuntimeError('fast DB state storage bound exceeded')
            self.db_states.append({'sha256': digest, 'rows': rows})
            self.db_state_bytes += len(raw)
        return {'dbReadEpoch': began, 'dbCompletedEpoch': completed,
                'dbStateIndex': len(self.db_states) - 1}
    def sample(self):
        began = time.monotonic()
        self._stage_seconds = {}
        row = self._current_sample = {'pid': self.pid, 'cycleMonotonic': began}
        row.update(self._timed('database', self.read_db))
        row.update(epoch=time.time(), monotonic=time.monotonic())
        row['startTicks'] = self._timed('identityBefore', self.identity)
        conn = UnixHTTP(self.socket_path)
        try:
            self._timed('request', lambda: conn.request('GET', '/v1/status'))
            response = self._timed('responseHeaders', conn.getresponse)
            raw = self._timed('responseBody', lambda: response.read(262145))
            if response.status != 200 or len(raw) > 262144:
                raise RuntimeError('invalid/oversized status response')
            obj = self._timed('decodeStatus', lambda: json.loads(raw))
        finally:
            conn.close()
        if self._timed('identityAfter', self.identity) != row['startTicks']:
            raise RuntimeError('observer changed during sample')
        self._stage = 'validateStatus'
        row['since'] = obj['since']
        if not row['since']:
            raise RuntimeError('missing observer since')
        observed = obj['observed']
        if 'lastCommandProbe' in observed:
            row['lastCommandProbe'] = observed['lastCommandProbe']
        # Never invent missing counter fields; unlike controller omitempty policy,
        # these five observer counters have explicit values in observed R1 status.
        for key in COUNTERS:
            row[key] = int(observed[key])
            if row[key] < 0:
                raise RuntimeError('negative counter')
        row['phase'], row['health'] = obj.get('phase'), obj.get('health')
        row['resources'] = [{'phase': x.get('phase'), 'health': x.get('health')}
                            for x in obj.get('resources', [])]
        row['completedEpoch'] = time.time()
        row['completedMonotonic'] = time.monotonic()
        identity = (row['pid'], row['startTicks'], row['since'])
        if self._observer_identity is not None and identity != self._observer_identity:
            raise RuntimeError('observer identity changed between samples')
        self._observer_identity = identity
        return row
    def _run(self):
        start = time.monotonic()
        next_at, end = start, start + self.duration
        next_tick = 0
        while not self.stop_event.is_set() and time.monotonic() < end:
            if len(self.samples) >= self.maximum_samples:
                self._error('sample limit reached')
                break
            try:
                row = self.sample()
                self._stage = 'coverage'
                cycle = row['completedMonotonic'] - row['cycleMonotonic']
                previous_start = self.samples[-1]['cycleMonotonic'] if self.samples else row['cycleMonotonic']
                coverage = row['completedMonotonic'] - previous_start
                self.diagnostics['maximumCycleSeconds'] = max(self.diagnostics['maximumCycleSeconds'], cycle)
                self.diagnostics['maximumCoverageSeconds'] = max(self.diagnostics['maximumCoverageSeconds'], coverage)
                if cycle > self.interval:
                    self.diagnostics['overCadenceSamples'] += 1
                    if len(self.diagnostics['slowSamples']) < MAX_SLOW_SAMPLES:
                        self.diagnostics['slowSamples'].append(
                            {'sample': copy.deepcopy(row), 'stageSeconds': dict(self._stage_seconds),
                             'cycleSeconds': cycle, 'coverageSeconds': coverage,
                             'scheduleLagSeconds': row['cycleMonotonic'] - next_at})
                    else:
                        self.diagnostics['slowSamplesTruncated'] += 1
                # Preserve the same 400ms coverage requirement used by command
                # attribution. A cadence miss is not itself a missing read.
                # This span includes both complete DB/status reads and the wait
                # between them; no joining over an unobserved interval is allowed.
                if not 0 <= cycle <= self.max_sample_gap or not 0 <= coverage <= self.max_sample_gap:
                    raise RuntimeError('sample coverage exceeded limit')
                self.samples.append(row)
            except Exception as exc:
                self._error(exc)
                # Fail closed: no joining across a missing/ambiguous counter interval.
                break
            next_tick += 1
            next_at = start + next_tick * self.interval
            # Keep the fixed cadence grid without catch-up reads or invented
            # samples. A scheduling stall is still rejected by actual coverage.
            completed = time.monotonic()
            while next_at <= completed:
                self.diagnostics['missedScheduleTicks'] += 1
                next_tick += 1
                next_at = start + next_tick * self.interval
            self.stop_event.wait(max(0, min(end, next_at) - time.monotonic()))
    def start(self):
        self.thread.start()
    def stop(self):
        self.stop_event.set()
        self.thread.join(timeout=1)
        if self.thread.is_alive():
            self.errors.append({'epoch': time.time(), 'error': 'sampler join timeout'})
    def result(self):
        return {'samples': self.samples, 'errors': self.errors,
                'intervalSeconds': self.interval, 'maximumSamples': self.maximum_samples,
                'threadExited': not self.thread.is_alive(),
                'dbStates': self.db_states, 'dbStateBytes': self.db_state_bytes,
                'maxSampleGapSeconds': self.max_sample_gap,
                'diagnostics': self.diagnostics}
