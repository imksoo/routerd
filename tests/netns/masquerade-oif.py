#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-3-Clause
"""Linux kernel reference: masquerade rebuilds a flow after an egress change.

Requires root, iproute2, nftables, conntrack, unshare and nsenter. Run only in
an explicit outer netns; no host network mutations or /run/netns registration.
This is a kernel-behavior reference, not a routerd daemon qualification.
"""
import json
import os
import re
import socket
import select
import subprocess
import time

host = os.environ.get('ROUTERD_HOST_NETNS', '')
current = os.readlink('/proc/self/ns/net')
assert os.environ.get('ROUTERD_RUN_NETNS') == '1'
assert host.startswith('net:[') and host.endswith(']') and current != host


def run(*args, **kwargs):
    try:
        return subprocess.run(args, check=True, text=True, capture_output=True,
                              timeout=5, **kwargs).stdout
    except subprocess.CalledProcessError as error:
        raise RuntimeError(f'{args}: {error.stderr}') from error


peer = subprocess.Popen(['unshare', '-n', 'sleep', '60'])
server = None
try:
    path = f'/proc/{peer.pid}/ns/net'
    deadline = time.monotonic() + 3
    while os.readlink(path) == current:
        assert time.monotonic() < deadline
        time.sleep(.01)
    prefix = ['nsenter', '--net=' + path]
    run('ip', 'link', 'set', 'lo', 'up')
    run('ip', 'address', 'add', '198.18.26.1/32', 'dev', 'lo')
    run(*prefix, 'ip', 'link', 'set', 'lo', 'up')
    run(*prefix, 'ip', 'address', 'add', '203.0.113.80/32', 'dev', 'lo')
    for name, local, remote in [('wan-a', '192.0.2.1', '192.0.2.2'),
                                ('wan-b', '198.51.100.1', '198.51.100.2')]:
        run('ip', 'link', 'add', 'name', name, 'type', 'veth', 'peer', 'name', 'peer-' + name)
        run('ip', 'link', 'set', 'peer-' + name, 'netns', str(peer.pid))
        run('ip', 'address', 'add', local + '/30', 'dev', name)
        run('ip', 'link', 'set', name, 'up')
        run(*prefix, 'ip', 'address', 'add', remote + '/30', 'dev', 'peer-' + name)
        run(*prefix, 'ip', 'link', 'set', 'peer-' + name, 'up')
    run('nft', '-f', '-', input='''table ip reference_nat {
 chain postrouting {
  type nat hook postrouting priority srcnat; policy accept;
  ip saddr 198.18.26.1 oifname { "wan-a", "wan-b" } masquerade
 }
}
''')
    run('ip', 'route', 'add', '203.0.113.80/32', 'via', '192.0.2.2', 'dev', 'wan-a')
    server = subprocess.Popen(prefix + ['python3', '-u', '-c', '''
import socket,json
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM)
s.bind(('203.0.113.80',8081))
print('ready',flush=True)
while True:
 data,addr=s.recvfrom(128)
 s.sendto(json.dumps({'source':addr[0]}).encode(),addr)
'''], stdout=subprocess.PIPE, text=True)
    assert select.select([server.stdout], [], [], 5)[0], 'UDP reference peer did not start'
    assert server.stdout.readline().strip() == 'ready'

    def identity():
        output = run('conntrack', '-L', '-p', 'udp', '--orig-src', '198.18.26.1',
                     '--sport', '44000', '--dport', '8081', '-o', 'id')
        return re.findall(r'\bid=(\d+)\b', output)

    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as client:
        client.bind(('198.18.26.1', 44000))
        client.settimeout(.5)
        client.sendto(b'first', ('203.0.113.80', 8081))
        assert json.loads(client.recv(128))['source'] == '192.0.2.1'
        original = identity()
        assert len(original) == 1
        run('ip', 'route', 'replace', '203.0.113.80/32', 'via', '198.51.100.2', 'dev', 'wan-b')
        try:
            client.sendto(b'egress-changed', ('203.0.113.80', 8081))
        except PermissionError:
            pass  # Local output reports NF_DROP as EPERM; forwarding drops silently.
        else:
            try:
                client.recv(128)
            except socket.timeout:
                pass
            else:
                raise AssertionError('expected native masquerade transition drop')
        assert identity() == [], 'kernel did not remove the old masquerade flow'
        client.sendto(b'retransmission', ('203.0.113.80', 8081))
        assert json.loads(client.recv(128))['source'] == '198.51.100.1'
        replacement = identity()
        assert len(replacement) == 1, replacement
        print(f'conntrack IDs: before={original}, rebuilt={replacement} (IDs may be reused after deletion)')
        print('PASS: same UDP socket/tuple; egress change dropped one packet and removed old ID; retransmission recovered with rebuilt NAT')
finally:
    if server is not None:
        server.kill()
        server.wait(timeout=5)
    peer.kill()
    peer.wait(timeout=5)
