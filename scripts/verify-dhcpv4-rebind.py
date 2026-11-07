#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-3-Clause
"""Require an actual Renew -> broadcast Rebind -> matching ACK in a pcap.

For the disposable-VM Ethernet/IPv4 fixture only; no packet generation and no
product decoder. Captures are temporary. RFC 2131 section 4.4.5 defines the wire
distinction; acquisition requests with ciaddr=0 must not satisfy this check.
"""
import argparse
import ipaddress
import struct
from pathlib import Path
from typing import NamedTuple


class Packet(NamedTuple):
    at: int
    xid: int
    op: int
    source: str
    destination: str
    ciaddr: str
    yiaddr: str
    mac: bytes
    options: dict[int, bytes]


def packets(path):
    with Path(path).open("rb") as stream:
        header = stream.read(24)
        if len(header) != 24 or header[:4] not in (b"\xd4\xc3\xb2\xa1", b"\xa1\xb2\xc3\xd4"):
            raise ValueError("expected classic microsecond Ethernet pcap")
        endian = "<" if header[:4] == b"\xd4\xc3\xb2\xa1" else ">"
        if struct.unpack(endian + "I", header[20:24])[0] != 1:
            raise ValueError("capture must use Ethernet link type")
        while record := stream.read(16):
            if len(record) != 16:
                raise ValueError("truncated capture header")
            sec, usec, captured, original = struct.unpack(endian + "IIII", record)
            if captured != original or captured > 65535:
                raise ValueError("truncated or oversized captured frame")
            frame = stream.read(captured)
            if len(frame) != captured:
                raise ValueError("truncated captured frame")
            if len(frame) < 34 or frame[12:14] != b"\x08\x00":
                continue
            ip = frame[14:]
            ihl = (ip[0] & 15) * 4
            if ip[0] >> 4 != 4 or ihl < 20 or len(ip) < ihl + 8 or ip[9] != 17:
                continue
            if struct.unpack("!H", ip[6:8])[0] & 0x3fff:
                continue
            length = struct.unpack("!H", ip[2:4])[0]
            if length < ihl + 8 or len(ip) < length:
                raise ValueError("truncated IPv4 datagram")
            udp = ip[ihl:length]
            sport, dport, ulen = struct.unpack("!HHH", udp[:6])
            if (sport, dport) not in ((68, 67), (67, 68)):
                continue
            if len(ip) < length or ulen < 248 or ulen > len(udp):
                raise ValueError("truncated DHCP datagram")
            bootp = udp[8:ulen]
            if (bootp[0], sport, dport) not in ((1, 68, 67), (2, 67, 68)):
                continue
            if bootp[1:3] != b"\x01\x06" or bootp[236:240] != b"\x63\x82\x53\x63":
                continue
            options = {}
            offset = 240
            while offset < len(bootp):
                code = bootp[offset]
                offset += 1
                if code == 255:
                    break
                if code == 0:
                    continue
                if offset >= len(bootp):
                    raise ValueError("missing DHCP option length")
                size = bootp[offset]
                offset += 1
                if offset + size > len(bootp):
                    raise ValueError("truncated DHCP option")
                options[code] = options.get(code, b"") + bootp[offset:offset + size]
                offset += size
            addr = lambda raw: str(ipaddress.IPv4Address(raw))
            yield Packet(sec * 1000000 + usec, struct.unpack("!I", bootp[4:8])[0],
                         bootp[0], addr(ip[12:16]), addr(ip[16:20]),
                         addr(bootp[12:16]), addr(bootp[16:20]), bootp[28:34], options)


def verify(trace, address, server):
    renewals = {}
    rebinds = {}
    for packet in trace:
        key = packet.xid, packet.mac
        if (packet.op == 1 and packet.options.get(53) == b"\x03"
                and packet.source == address and packet.ciaddr == address
                and 50 not in packet.options and 54 not in packet.options):
            if packet.destination == server:
                renewals[key] = packet.at
            elif packet.destination == "255.255.255.255" and key in renewals and packet.at > renewals[key]:
                rebinds[key] = packet.at
        if (packet.op == 2 and packet.options.get(53) == b"\x05"
                and packet.options.get(54) == ipaddress.IPv4Address(server).packed
                and packet.yiaddr == address and key in rebinds
                and packet.at >= rebinds[key]):
            return
    raise ValueError("no correlated unicast Renew, broadcast Rebind and ACK")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("capture")
    parser.add_argument("address")
    parser.add_argument("server")
    args = parser.parse_args()
    verify(packets(args.capture), args.address, args.server)
    print("PASS: captured unicast Renew, broadcast Rebind and matching server ACK")
