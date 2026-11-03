#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-3-Clause
"""Bounded independent IPv4 NTP exchange; never adjusts any clock."""
import argparse
import socket
import struct
import time

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--server', required=True)
parser.add_argument('--source', required=True)
parser.add_argument('--expect', choices=('reply', 'silence'), default='reply')
args = parser.parse_args()
for attempt in range(3):
    packet = bytearray(48)
    packet[0] = 0x23  # NTPv4 client request.
    sent = time.time()
    seconds = int(sent) + 2208988800
    fraction = int((sent % 1) * 2**32)
    struct.pack_into('!II', packet, 40, seconds, fraction)
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as client:
        client.bind((args.source, 0))
        client.settimeout(1)
        client.sendto(packet, (args.server, 123))
        try:
            reply, peer = client.recvfrom(4096)
        except socket.timeout:
            if args.expect == 'silence':
                continue
            raise SystemExit('FAIL: no NTP reply')
    if args.expect == 'silence':
        raise SystemExit('FAIL: denied source received a reply')
    if peer != (args.server, 123) or len(reply) < 48 or reply[0] & 7 != 4:
        raise SystemExit('FAIL: invalid NTP reply identity or shape')
    if reply[24:32] != packet[40:48] or reply[0] >> 6 == 3 or not 1 <= reply[1] <= 15:
        raise SystemExit('FAIL: uncorrelated or unsynchronized NTP reply')
    print(f'PASS: correlated synchronized NTP reply, stratum={reply[1]}')
print(f'PASS: three bounded NTP {args.expect} checks')
