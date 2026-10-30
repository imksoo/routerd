#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-3-Clause
"""Own one Linux tcpdump lifetime and its capture file across SSH calls."""

import argparse
import contextlib
import fcntl
import json
import os
from pathlib import Path
import select
import shutil
import signal
import stat
import sys
import time


class CaptureError(RuntimeError):
    pass


def scope():
    return (Path('/proc/sys/kernel/random/boot_id').read_text().strip()
            + ':' + os.readlink('/proc/self/ns/pid'))


def process(descriptor, pid):
    proc_pid = None
    for line in Path(f'/proc/self/fdinfo/{descriptor}').read_text().splitlines():
        if line.startswith('Pid:'):
            proc_pid = int(line.split()[1])
    if proc_pid is None or proc_pid <= 0:
        raise CaptureError('capture process has exited')
    fields = Path(f'/proc/{proc_pid}/stat').read_text().rsplit(')', 1)[1].split()
    return {'pid': pid, 'procPid': proc_pid, 'started': int(fields[19]),
            'pgid': os.getpgid(pid), 'session': os.getsid(pid)}


def save(path, receipt):
    temporary = path.with_name(path.name + '.new')
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        with os.fdopen(descriptor, 'w') as stream:
            json.dump(receipt, stream, sort_keys=True)
            stream.write('\n')
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        temporary.unlink(missing_ok=True)


def private_directory(path):
    path.mkdir(parents=True, mode=0o700, exist_ok=True)
    info = path.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & 0o077:
        raise CaptureError('capture directory must be private and owned by this UID')


@contextlib.contextmanager
def locked_receipt(path):
    private_directory(path.parent)
    fd = os.open(str(path) + '.lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & 0o077:
            raise CaptureError('capture lock is not privately owned')
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield fd
    finally:
        os.close(fd)


def load(path, capture):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd) as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & 0o077:
            raise CaptureError('capture receipt is not privately owned')
        receipt = json.load(stream)
    if receipt.get('version') != 1 or receipt.get('capture') != str(capture) or receipt.get('scope') != scope():
        raise CaptureError('capture receipt does not identify this native world and file')
    return receipt


def capture_file(receipt):
    path = Path(receipt['capture'])
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        info = os.fstat(fd)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid()
                or info.st_mode & 0o077 or [info.st_dev, info.st_ino] != receipt['file']):
            raise CaptureError('capture file identity differs from its owner')
        return fd
    except BaseException:
        os.close(fd)
        raise


def check_process(fd, receipt):
    expected = receipt['process']
    if process(fd, expected['pid']) != expected:
        raise CaptureError('capture process identity differs from its owner')
    proc = Path('/proc') / str(expected['procPid'])
    command = (proc / 'cmdline').read_bytes().rstrip(b'\0').split(b'\0')
    if command != [os.fsencode(a) for a in receipt['command']]:
        raise CaptureError('capture process command differs from its owner')
    binary = (proc / 'exe').stat()
    output = (proc / 'fd/1').stat()
    if [binary.st_dev, binary.st_ino] != receipt['executable'] or [output.st_dev, output.st_ino] != receipt['file']:
        raise CaptureError('capture executable/output identity differs from its owner')


def wait_exit(fd, timeout):
    poller = select.poll()
    poller.register(fd, select.POLLIN)
    return bool(poller.poll(max(1, int(timeout * 1000))))


def reap(pid):
    try:
        os.waitpid(pid, os.WNOHANG)
    except ChildProcessError:
        pass  # A previous SSH invocation owned the child; pidfd proves exit.


def start(capture, path, log, command, timeout=5.0):
    if os.path.lexists(path):
        raise CaptureError('capture receipt already exists; use a fresh test identity')
    if not hasattr(os, 'pidfd_open') or not hasattr(signal, 'pidfd_send_signal'):
        raise CaptureError('capture ownership requires Linux pidfds')
    executable = Path(command[0]).stat()
    receipt = {'version': 1, 'scope': scope(), 'capture': str(capture), 'phase': 'preparing',
               'command': command, 'executable': [executable.st_dev, executable.st_ino]}
    save(path, receipt)
    output = os.open(capture, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        diagnostic = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    except BaseException:
        os.close(output)
        raise
    identity = os.fstat(output)
    receipt['file'] = [identity.st_dev, identity.st_ino]
    admission, grant = os.pipe()
    ready, notify = os.pipe()
    try:
        pid = os.fork()
    except BaseException:
        for descriptor in (output, diagnostic, admission, grant, ready, notify):
            os.close(descriptor)
        raise
    if pid == 0:
        try:
            os.close(grant)
            os.close(ready)
            os.setsid()
            os.write(notify, b'1')
            os.close(notify)
            allowed = os.read(admission, 1)
            os.close(admission)
            if allowed != b'1':
                os._exit(125)
            os.dup2(output, 1)
            os.dup2(diagnostic, 2)
            os.execv(command[0], command)
        except BaseException:
            os._exit(126)
    os.close(admission)
    os.close(notify)
    fd = None
    try:
        if not select.select([ready], [], [], timeout)[0] or os.read(ready, 1) != b'1':
            raise CaptureError('capture child did not reach admission')
        fd = os.pidfd_open(pid)
        receipt['process'] = process(fd, pid)
        receipt['phase'] = 'starting'
        save(path, receipt)  # The child cannot execute tcpdump before this ACK.
        os.write(grant, b'1')
        os.close(grant)
        grant = None
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if wait_exit(fd, 0.01):
                raise CaptureError('capture exited before its pcap header was ready')
            check = capture_file(receipt)
            try:
                if os.fstat(check).st_size >= 24:
                    check_process(fd, receipt)
                    if os.read(check, 4) not in (b'\xd4\xc3\xb2\xa1', b'\xa1\xb2\xc3\xd4', b'\x4d\x3c\xb2\xa1', b'\xa1\xb2\x3c\x4d'):
                        raise CaptureError('capture did not write a pcap header')
                    receipt['phase'] = 'running'
                    save(path, receipt)
                    return
            finally:
                os.close(check)
        raise CaptureError('capture pcap readiness deadline expired')
    except BaseException:
        if grant is not None:
            os.close(grant)
            grant = None
        if fd is None:
            fd = os.pidfd_open(pid)
        with contextlib.suppress(ProcessLookupError):
            signal.pidfd_send_signal(fd, signal.SIGKILL)
        if not wait_exit(fd, timeout):
            raise CaptureError('failed capture child did not exit')
        reap(pid)
        raise
    finally:
        if grant is not None:
            os.close(grant)
        for descriptor in (ready, output, diagnostic, fd):
            if descriptor is not None:
                os.close(descriptor)


def stop(capture, path, timeout=10.0):
    receipt = load(path, capture)
    if receipt.get('phase') == 'stopped':
        return
    if receipt.get('phase') not in ('running', 'stopping'):
        raise CaptureError('capture launch outcome is not confirmed')
    fd = None
    try:
        try:
            fd = os.pidfd_open(receipt['process']['pid'])
        except ProcessLookupError:
            if receipt['phase'] != 'stopping':
                raise CaptureError('running capture exited without an acknowledged stop')
        gone = fd is None or wait_exit(fd, 0)
        if not gone:
            current = process(fd, receipt['process']['pid'])
            gone = current['started'] != receipt['process']['started']
        if gone:
            if receipt['phase'] != 'stopping':
                raise CaptureError('running capture identity disappeared before stop')
            checked = capture_file(receipt)
            os.close(checked)
            reap(receipt['process']['pid'])
            receipt['phase'] = 'stopped'
            save(path, receipt)
            return
        check_process(fd, receipt)
        checked = capture_file(receipt)
        os.close(checked)
        receipt['phase'] = 'stopping'
        save(path, receipt)
        check_process(fd, receipt)
        signal.pidfd_send_signal(fd, signal.SIGINT)
        if not wait_exit(fd, timeout):
            raise CaptureError('capture has not acknowledged exit; receipt retained')
        reap(receipt['process']['pid'])
        receipt['phase'] = 'stopped'
        save(path, receipt)
    finally:
        if fd is not None:
            os.close(fd)


def copy(capture, path, destination):
    receipt = load(path, capture)
    if receipt.get('phase') != 'stopped':
        raise CaptureError('capture copy requires acknowledged stop')
    with os.fdopen(capture_file(receipt), 'rb') as stream:
        if os.fstat(stream.fileno()).st_size < 24:
            raise CaptureError('capture file is incomplete')
        shutil.copyfileobj(stream, destination)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('operation', choices=('start', 'stop', 'copy'))
    parser.add_argument('capture', type=Path)
    parser.add_argument('receipt', type=Path)
    parser.add_argument('--interface')
    parser.add_argument('--filter')
    args = parser.parse_args()
    if not args.capture.is_absolute() or args.capture.parent != args.receipt.parent:
        raise CaptureError('capture and receipt must share one absolute private directory')
    with locked_receipt(args.receipt):
        if args.operation == 'start':
            executable = shutil.which('tcpdump')
            if not executable or not args.interface or not args.filter:
                raise CaptureError('capture start requires tcpdump, interface and filter')
            command = [executable, '-U', '-i', args.interface, '-w', '-', args.filter]
            start(args.capture, args.receipt, Path(str(args.capture) + '.log'), command)
        elif args.operation == 'stop':
            stop(args.capture, args.receipt)
        else:
            copy(args.capture, args.receipt, sys.stdout.buffer)


if __name__ == '__main__':
    try:
        main()
    except (CaptureError, OSError, ValueError, KeyError) as error:
        print(f'capture owner: {error}', file=sys.stderr)
        raise SystemExit(1)
