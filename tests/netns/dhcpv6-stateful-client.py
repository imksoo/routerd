#!/usr/bin/env python3
"""Minimal independent DHCPv6 stateful client for the isolated server gate."""
import ipaddress
import os
import socket
import struct
import sys
import time


iface = sys.argv[1]
index = socket.if_nametoindex(iface)
mac = bytes.fromhex(open('/sys/class/net/' + iface + '/address').read().strip().replace(':', ''))
duid = struct.pack('!HH', 3, 1) + mac
iaid = 0x72643135


def option(code, data):
    return struct.pack('!HH', code, len(data)) + data


def ia_na(address=None):
    data = struct.pack('!III', iaid, 0, 0)
    if address is not None:
        data += option(5, socket.inet_pton(socket.AF_INET6, address) + struct.pack('!II', 0, 0))
    return option(3, data)


def parse_options(data):
    options = {}
    offset = 4
    while offset + 4 <= len(data):
        code, size = struct.unpack('!HH', data[offset:offset + 4])
        offset += 4
        options.setdefault(code, []).append(data[offset:offset + size])
        offset += size
    if offset != len(data):
        raise AssertionError('malformed DHCPv6 option length')
    return options


def reply_address(options):
    entries = options.get(3, [])
    assert len(entries) == 1 and len(entries[0]) >= 12, entries
    ia = entries[0]
    got_iaid, _, _ = struct.unpack('!III', ia[:12])
    assert got_iaid == iaid, hex(got_iaid)
    nested = ia[12:]
    while nested:
        assert len(nested) >= 4, nested.hex()
        code, size = struct.unpack('!HH', nested[:4])
        value = nested[4:4 + size]
        assert len(value) == size, nested.hex()
        if code == 5:
            assert len(value) == 24, value.hex()
            address = socket.inet_ntop(socket.AF_INET6, value[:16])
            preferred, valid = struct.unpack('!II', value[16:])
            assert preferred > 0 and valid >= preferred, (preferred, valid)
            return address, preferred, valid
        nested = nested[4 + size:]
    raise AssertionError('Reply has no IAADDR')


def domain_names(data):
    names = []
    offset = 0
    while offset < len(data):
        labels = []
        while True:
            assert offset < len(data), data.hex()
            size = data[offset]
            offset += 1
            if size == 0:
                break
            assert size & 0xc0 == 0 and offset + size <= len(data), data.hex()
            labels.append(data[offset:offset + size].decode('ascii'))
            offset += size
        names.append('.'.join(labels))
    return names


def require_options(options):
    assert options.get(23) == [socket.inet_pton(socket.AF_INET6, 'fd00:1234::1')], options.get(23)
    assert options.get(31) == [socket.inet_pton(socket.AF_INET6, 'fd00:1234::123')], options.get(31)
    assert options.get(24) and domain_names(options[24][0]) == ['home.arpa'], options.get(24)


sock = socket.socket(socket.AF_INET6, socket.SOCK_DGRAM)
sock.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_MULTICAST_IF, index)
sock.bind(('fe80::2', 546, 0, index))
sock.settimeout(1)


def exchange(message_type, extras, target):
    txid = os.urandom(3)
    packet = bytes([message_type]) + txid + option(1, duid) + extras
    sock.sendto(packet, target)
    deadline = time.monotonic() + 8
    while time.monotonic() < deadline:
        try:
            response, peer = sock.recvfrom(4096)
        except TimeoutError:
            continue
        if len(response) >= 4 and response[1:4] == txid:
            return response, peer
    raise TimeoutError('no matching DHCPv6 response')


requested = option(6, struct.pack('!HHH', 23, 24, 31))
advertise, server_peer = exchange(1, ia_na() + requested, ('ff02::1:2', 547, 0, index))
assert advertise[0] == 2, advertise.hex()
advertise_options = parse_options(advertise)
server_id = advertise_options.get(2, [None])[0]
assert server_id, advertise_options
offered, _, _ = reply_address(advertise_options)
assert ipaddress.ip_address(offered) in ipaddress.ip_network('fd00:1234::100/124'), offered

reply, server_peer = exchange(3, option(2, server_id) + ia_na(offered) + requested, ('ff02::1:2', 547, 0, index))
assert reply[0] == 7, reply.hex()
reply_options = parse_options(reply)
address, preferred, valid = reply_address(reply_options)
assert address == offered, (offered, address)
require_options(reply_options)

renew_target = (server_peer[0], 547, 0, index)
renew, _ = exchange(5, option(2, server_id) + ia_na(address) + requested, renew_target)
assert renew[0] == 7, renew.hex()
renew_options = parse_options(renew)
renewed, renewed_preferred, renewed_valid = reply_address(renew_options)
assert renewed == address, (address, renewed)
assert renewed_preferred > 0 and renewed_valid >= renewed_preferred
require_options(renew_options)

release, _ = exchange(8, option(2, server_id) + ia_na(address), renew_target)
assert release[0] == 7, release.hex()
print('{"dhcpv6":"solicit-request-renew-release","address":"%s","preferred":%d,"valid":%d}' % (address, preferred, valid))
