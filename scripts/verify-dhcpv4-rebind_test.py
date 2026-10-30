#!/usr/bin/env python3
# SPDX-License-Identifier: BSD-3-Clause
import importlib.util
import ipaddress
import struct
import tempfile
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location("rebind", Path(__file__).with_name("verify-dhcpv4-rebind.py"))
rebind = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rebind)
CLIENT, SERVER = "198.18.46.100", "198.18.46.1"


def packet(at, destination, ack=False, xid=7, ciaddr=CLIENT):
    opts = {53: b"\x05" if ack else b"\x03"}
    if ack:
        opts[54] = ipaddress.IPv4Address(SERVER).packed
    return rebind.Packet(at, xid, 2 if ack else 1, SERVER if ack else CLIENT,
                         destination, ciaddr, CLIENT if ack else "0.0.0.0", b"abcdef", opts)


def capture(trace):
    data = struct.pack("<IHHIIII", 0xa1b2c3d4, 2, 4, 0, 0, 65535, 1)
    for p in trace:
        addr = lambda value: ipaddress.IPv4Address(value).packed
        bootp = bytearray(240)
        bootp[:3] = bytes([p.op, 1, 6])
        bootp[4:8] = struct.pack("!I", p.xid)
        bootp[12:16], bootp[16:20], bootp[28:34] = addr(p.ciaddr), addr(p.yiaddr), p.mac
        bootp[236:240] = bytes([99, 130, 83, 99])
        for key, value in p.options.items():
            bootp += bytes([key, len(value)]) + value
        bootp += b"\xff"
        udp = struct.pack("!HHHH", 68 if p.op == 1 else 67, 67 if p.op == 1 else 68, 8 + len(bootp), 0) + bootp
        ip = struct.pack("!BBHHHBBH4s4s", 0x45, 0, 20 + len(udp), 0, 0, 64, 17, 0, addr(p.source), addr(p.destination))
        frame = bytes(12) + b"\x08\x00" + ip + udp
        data += struct.pack("<IIII", p.at, 0, len(frame), len(frame)) + frame
    return data


class RebindCaptureTest(unittest.TestCase):
    def test_matching_exchange_and_packet_decoder(self):
        trace = [packet(1, SERVER), packet(2, "255.255.255.255"), packet(3, CLIENT, ack=True)]
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "rebind.pcap"
            path.write_bytes(capture(trace))
            rebind.verify(rebind.packets(path), CLIENT, SERVER)
            path.write_bytes(path.read_bytes()[:-1])
            with self.assertRaises(ValueError):
                rebind.verify(rebind.packets(path), CLIENT, SERVER)

    def test_acquisition_wrong_ack_and_missing_ack_do_not_pass(self):
        renewal, broadcast, ack = packet(1, SERVER), packet(2, "255.255.255.255"), packet(3, CLIENT, ack=True)
        for trace in ([renewal, broadcast],
                      [renewal, broadcast, ack._replace(xid=8)],
                      [renewal, broadcast._replace(ciaddr="0.0.0.0"), ack],
                      [ack, renewal, broadcast],
                      [renewal, broadcast._replace(options={53: b"\x03", 54: b"server"}), ack]):
            with self.subTest(trace=trace), self.assertRaises(ValueError):
                rebind.verify(trace, CLIENT, SERVER)


if __name__ == "__main__":
    unittest.main()
