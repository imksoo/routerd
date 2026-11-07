#!/usr/bin/env python3
"""Bounded independent application probe; run inside the qualification namespaces.

server WORK: Internet namespace with explicit v4/v6 service addresses.
client WORK: DHCP/SLAAC client namespace, with .120-.183 LAN aliases installed.
No reconnects: each TCP worker owns exactly one socket for the entire run.
"""
import concurrent.futures
import json
import pathlib
import selectors
import socket
import sys
import threading
import time

mode, directory = sys.argv[1:]
work = pathlib.Path(directory)
end = time.monotonic() + 1800
lock = threading.Lock()


def server():
    def handle(conn, peer):
        with conn:
            conn.settimeout(1)
            first = True
            pending = b''
            while time.monotonic() < end:
                try:
                    data = conn.recv(65536)
                except socket.timeout:
                    continue
                if not data:
                    return
                pending += data
                while b'\n' in pending:
                    line, pending = pending.split(b'\n', 1)
                    request = json.loads(line)
                    if first:
                        with lock, (work / 'accepts.jsonl').open('a') as output:
                            output.write(json.dumps({'id': request['id'], 'peer': peer}) + '\n')
                        first = False
                    conn.sendall((json.dumps({**request, 'peer': peer}) + '\n').encode())

    def tcp(address, port):
        family = socket.AF_INET6 if ':' in address else socket.AF_INET
        with socket.socket(family) as listener:
            listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            listener.bind((address, port))
            listener.listen(64)
            listener.settimeout(1)
            with concurrent.futures.ThreadPoolExecutor(max_workers=16) as pool:
                while time.monotonic() < end:
                    try:
                        conn, peer = listener.accept()
                    except socket.timeout:
                        continue
                    if port == 8080:
                        conn.close()
                    else:
                        pool.submit(handle, conn, peer)

    def udp(address):
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
            sock.bind((address, 8081))
            sock.settimeout(1)
            while time.monotonic() < end:
                try:
                    data, peer = sock.recvfrom(65535)
                except socket.timeout:
                    continue
                request = json.loads(data)
                sock.sendto(json.dumps({**request, 'peer': peer}).encode(), peer)

    jobs = [(tcp, ('203.0.113.80', 8080)), (tcp, ('203.0.113.80', 8082)),
            (tcp, ('10.250.0.1', 8082)), (tcp, ('2001:db8:ffff::80', 8082)),
            (udp, ('203.0.113.80',))]
    with concurrent.futures.ThreadPoolExecutor(max_workers=len(jobs)) as pool:
        futures = [pool.submit(function, *args) for function, args in jobs]
        for future in futures:
            future.result()


def client():
    global end
    states = {}

    def received(label, request, reply, local):
        if reply['id'] != label or reply['seq'] != request['seq'] or reply['data'] != request['data']:
            raise RuntimeError('application reply mismatch: ' + label)
        now = time.time()
        peer = reply['peer'][:2]
        with lock:
            state = states.setdefault(label, {'count': 0, 'changed': 0, 'maxGap': 0})
            if state['count']:
                state['maxGap'] = max(state['maxGap'], now - state['last'])
                if peer != state['peer']:
                    state['changed'] += 1
            else:
                state['first'] = now
                state['peer'] = peer
                state['local'] = list(local[:2])
            state['count'] += 1
            state['last'] = now

    def tcp(label, address, local=None):
        family = socket.AF_INET6 if ':' in address else socket.AF_INET
        with socket.socket(family) as sock:
            sock.settimeout(20)
            if local:
                sock.bind((local, 50000))
            sock.connect((address, 8082))
            sock.settimeout(1)
            pending = b''
            sequence = 0
            while time.monotonic() < end:
                request = {'id': label, 'seq': sequence, 'data': 'x' * 16384}
                sock.sendall((json.dumps(request) + '\n').encode())
                while b'\n' not in pending and time.monotonic() < end:
                    try:
                        data = sock.recv(65536)
                    except socket.timeout:
                        continue
                    if not data:
                        raise RuntimeError('persistent connection closed: ' + label)
                    pending += data
                if b'\n' not in pending:
                    return
                line, pending = pending.split(b'\n', 1)
                received(label, request, json.loads(line), sock.getsockname())
                sequence += 1
                time.sleep(.5)

    def udp():
        selector = selectors.DefaultSelector()
        sockets = []
        try:
            for index in range(64):
                sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
                sock.bind((f'198.18.30.{120+index}', 49000))
                sock.setblocking(False)
                selector.register(sock, selectors.EVENT_READ, f'udp-{index}')
                sockets.append(sock)
            sequence = 0
            while time.monotonic() < end:
                request_data = 'u' * 256
                for key in selector.get_map().values():
                    request = {'id': key.data, 'seq': sequence, 'data': request_data}
                    key.fileobj.sendto(json.dumps(request).encode(), ('203.0.113.80', 8081))
                until = time.monotonic() + .5
                while time.monotonic() < until:
                    for key, _ in selector.select(max(0, until-time.monotonic())):
                        data, remote = key.fileobj.recvfrom(65535)
                        if remote != ('203.0.113.80', 8081):
                            raise RuntimeError('UDP reply source mismatch')
                        reply = json.loads(data)
                        if reply['seq'] != sequence:
                            continue  # Delayed in-flight reply after a disruption.
                        received(key.data, {'seq': sequence, 'data': request_data}, reply,
                                 key.fileobj.getsockname())
                sequence += 1
        finally:
            selector.close()
            for sock in sockets:
                sock.close()

    with concurrent.futures.ThreadPoolExecutor(max_workers=11) as pool:
        futures = [pool.submit(tcp, f'tcp-{i}', '203.0.113.80', f'198.18.30.{120+i}') for i in range(8)]
        futures += [pool.submit(tcp, 'bgp', '10.250.0.1'),
                    pool.submit(tcp, 'ipv6', '2001:db8:ffff::80'), pool.submit(udp)]
        try:
            while time.monotonic() < end:
                for future in futures:
                    if future.done():
                        future.result()
                        raise RuntimeError('application worker ended early')
                with lock:
                    result = {'time': time.time(), 'flows': dict(states)}
                    temporary = work / 'traffic.next'
                    temporary.write_text(json.dumps(result))
                    temporary.replace(work / 'traffic.json')
                time.sleep(1)
        finally:
            # Executor shutdown waits for workers. End their loops first so a
            # failed connection is reported immediately, not after 30 minutes.
            end = time.monotonic()


if mode == 'server':
    server()
elif mode == 'client':
    client()
else:
    raise SystemExit('use server WORK or client WORK')
