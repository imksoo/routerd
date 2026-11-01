#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-3-Clause
"""Receive one IPv6 Router Advertisement and emit its flag/prefix contract."""
import ipaddress
import json
import socket
import struct
import sys
import time

if len(sys.argv) != 3:
    raise SystemExit("usage: ra-flag-receiver.py INTERFACE TIMEOUT_SECONDS")
interface, timeout_text = sys.argv[1:]
timeout = float(timeout_text)
index = socket.if_nametoindex(interface)
sock = socket.socket(socket.AF_INET6, socket.SOCK_RAW, socket.IPPROTO_ICMPV6)
sock.setsockopt(socket.SOL_SOCKET, socket.SO_BINDTODEVICE, interface.encode() + b"\0")
sock.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_JOIN_GROUP, socket.inet_pton(socket.AF_INET6, "ff02::1") + struct.pack("@I", index))
sock.settimeout(0.25)
deadline = time.monotonic() + timeout
while time.monotonic() < deadline:
    try:
        packet, _ = sock.recvfrom(2048)
    except TimeoutError:
        continue
    if len(packet) < 16 or packet[0] != 134 or packet[1] != 0:
        continue
    result = {"managed": bool(packet[5] & 0x80), "other": bool(packet[5] & 0x40), "prefixes": []}
    offset = 16
    valid = True
    while offset < len(packet):
        if offset + 2 > len(packet):
            valid = False
            break
        option_type, units = packet[offset], packet[offset + 1]
        size = units * 8
        if units == 0 or offset + size > len(packet):
            valid = False
            break
        if option_type == 3 and size == 32:
            result["prefixes"].append({"prefix": str(ipaddress.IPv6Address(packet[offset + 16:offset + 32])) + "/" + str(packet[offset + 2]), "autonomous": bool(packet[offset + 3] & 0x40)})
        offset += size
    if valid:
        print(json.dumps(result, sort_keys=True))
        raise SystemExit(0)
raise SystemExit("timed out waiting for Router Advertisement")
