#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"

require_common

LEFT="${TEST_ID}-left"
RIGHT="${TEST_ID}-right"
LEFT_CLIENT="${TEST_ID}-left-client"
RIGHT_CLIENT="${TEST_ID}-right-client"

for ns in "$LEFT" "$RIGHT" "$LEFT_CLIENT" "$RIGHT_CLIENT"; do
  create_ns "$ns"
done

# Routed underlay. The VXLAN device itself is joined only to the inner bridge.
create_veth_pair "$LEFT" underlay 198.18.0.1/30 "$RIGHT" underlay 198.18.0.2/30

for endpoint in "$LEFT" "$RIGHT"; do
  ip -n "$endpoint" link add br-l2 type bridge
  ip -n "$endpoint" link set br-l2 up
done

VETH_COUNTER=$((VETH_COUNTER + 1))
LEFT_PORT="${TEST_IF_ID}x${VETH_COUNTER}a"
LEFT_PEER="${TEST_IF_ID}x${VETH_COUNTER}b"
ip link add "$LEFT_PORT" type veth peer name "$LEFT_PEER"
ip link set "$LEFT_PORT" netns "$LEFT"
ip link set "$LEFT_PEER" netns "$LEFT_CLIENT"
ip -n "$LEFT" link set "$LEFT_PORT" name lan0
ip -n "$LEFT" link set lan0 master br-l2
ip -n "$LEFT" link set lan0 up
ip -n "$LEFT_CLIENT" link set "$LEFT_PEER" name eth0
ip -n "$LEFT_CLIENT" link set eth0 address 02:00:00:00:00:11
ip -n "$LEFT_CLIENT" link set eth0 up

VETH_COUNTER=$((VETH_COUNTER + 1))
RIGHT_PORT="${TEST_IF_ID}x${VETH_COUNTER}a"
RIGHT_PEER="${TEST_IF_ID}x${VETH_COUNTER}b"
ip link add "$RIGHT_PORT" type veth peer name "$RIGHT_PEER"
ip link set "$RIGHT_PORT" netns "$RIGHT"
ip link set "$RIGHT_PEER" netns "$RIGHT_CLIENT"
ip -n "$RIGHT" link set "$RIGHT_PORT" name lan0
ip -n "$RIGHT" link set lan0 master br-l2
ip -n "$RIGHT" link set lan0 up
ip -n "$RIGHT_CLIENT" link set "$RIGHT_PEER" name eth0
ip -n "$RIGHT_CLIENT" link set eth0 address 02:00:00:00:00:22
ip -n "$RIGHT_CLIENT" link set eth0 up

# Keep this command sequence aligned with pkg/vxlan.Commands: nolearning plus
# an all-zero FDB entry floods broadcast and unknown-unicast frames to peers.
ip -n "$LEFT" link add vx-l2 type vxlan id 200001 local 198.18.0.1 dev underlay dstport 4789 nolearning
ip -n "$RIGHT" link add vx-l2 type vxlan id 200001 local 198.18.0.2 dev underlay dstport 4789 nolearning
ip -n "$LEFT" link set vx-l2 master br-l2
ip -n "$RIGHT" link set vx-l2 master br-l2
ip -n "$LEFT" link set vx-l2 up
ip -n "$RIGHT" link set vx-l2 up
ip netns exec "$LEFT" bridge fdb append 00:00:00:00:00:00 dev vx-l2 dst 198.18.0.2
ip netns exec "$RIGHT" bridge fdb append 00:00:00:00:00:00 dev vx-l2 dst 198.18.0.1

cat >"$WORKDIR/l2-probe.py" <<'PY'
import ipaddress
import socket
import struct
import sys
import time

IFNAME = "eth0"
SRC = bytes.fromhex("020000000011")
DST = bytes.fromhex("020000000022")
BCAST = b"\xff" * 6

def ethernet(dst, src, ethertype, payload):
    return dst + src + struct.pack("!H", ethertype) + payload

def checksum(data):
    if len(data) % 2:
        data += b"\x00"
    total = sum(struct.unpack("!%dH" % (len(data) // 2), data))
    while total >> 16:
        total = (total & 0xffff) + (total >> 16)
    return (~total) & 0xffff

def ipv4_udp(sport, dport, payload, src="0.0.0.0", dst="255.255.255.255"):
    udp = struct.pack("!HHHH", sport, dport, 8 + len(payload), 0) + payload
    src_bytes, dst_bytes = socket.inet_aton(src), socket.inet_aton(dst)
    header = struct.pack("!BBHHHBBH4s4s", 0x45, 0, 20 + len(udp), 1, 0, 64, 17, 0,
                         src_bytes, dst_bytes)
    header = header[:10] + struct.pack("!H", checksum(header)) + header[12:]
    return header + udp

def ipv6_packet(next_header, payload, src, dst):
    src_bytes = ipaddress.IPv6Address(src).packed
    dst_bytes = ipaddress.IPv6Address(dst).packed
    return struct.pack("!IHBB16s16s", 6 << 28, len(payload), next_header, 255,
                       src_bytes, dst_bytes) + payload

def ipv6_pseudo(src, dst, next_header, length):
    return (ipaddress.IPv6Address(src).packed + ipaddress.IPv6Address(dst).packed +
            struct.pack("!I3xB", length, next_header))

def icmpv6_packet(kind, src, dst):
    target = ipaddress.IPv6Address("2001:db8::22").packed
    bodies = {
        133: b"\x00" * 4,
        134: struct.pack("!BBHII", 64, 0, 1800, 0, 0),
        135: b"\x00" * 4 + target,
        136: struct.pack("!I", 0x60000000) + target,
    }
    message = struct.pack("!BBH", kind, 0, 0) + bodies[kind]
    value = checksum(ipv6_pseudo(src, dst, 58, len(message)) + message)
    message = struct.pack("!BBH", kind, 0, value) + bodies[kind]
    return ipv6_packet(58, message, src, dst)

def ipv6_udp(sport, dport, payload, src, dst):
    udp = struct.pack("!HHHH", sport, dport, 8 + len(payload), 0) + payload
    value = checksum(ipv6_pseudo(src, dst, 17, len(udp)) + udp)
    udp = struct.pack("!HHHH", sport, dport, len(udp), value) + payload
    return ipv6_packet(17, udp, src, dst)

def dhcpv4(message_type, src_mac):
    op = 1 if message_type == 1 else 2
    yiaddr = socket.inet_aton("0.0.0.0" if op == 1 else "192.0.2.123")
    fixed = struct.pack("!BBBBIHH", op, 1, 6, 0, 0x12345678, 0, 0x8000)
    fixed += socket.inet_aton("0.0.0.0") + yiaddr
    fixed += socket.inet_aton("0.0.0.0") + socket.inet_aton("0.0.0.0")
    fixed += src_mac.ljust(16, b"\x00") + b"\x00" * 64 + b"\x00" * 128
    options = b"\x63\x82\x53\x63\x35\x01" + bytes([message_type])
    if message_type == 2:
        options += b"\x36\x04" + socket.inet_aton("192.0.2.1")
    return fixed + options + b"\xff"

def dhcpv6(message_type, client_mac, server_mac=None):
    client_duid = struct.pack("!HH", 3, 1) + client_mac
    client_id = struct.pack("!HH", 1, len(client_duid)) + client_duid
    if message_type == 1:
        return bytes([1, 0x12, 0x34, 0x56]) + client_id
    server_duid = struct.pack("!HH", 3, 1) + server_mac
    server_id = struct.pack("!HH", 2, len(server_duid)) + server_duid
    return bytes([7, 0x12, 0x34, 0x56]) + server_id + client_id

def dhcpv6_options(data):
    options = {}
    while len(data) >= 4:
        code, length = struct.unpack("!HH", data[:4])
        if len(data) < 4 + length:
            return {}
        options[code] = data[4:4 + length]
        data = data[4 + length:]
    return options if not data else {}

def arp(src):
    return struct.pack("!HHBBH6s4s6s4s", 1, 0x0800, 6, 4, 1, src,
                       socket.inet_aton("0.0.0.0"), b"\x00" * 6,
                       socket.inet_aton("192.0.2.1"))

def build(name, reverse=False):
    src, dst = (DST, SRC) if reverse else (SRC, DST)
    if name == "arp":
        return ethernet(BCAST, src, 0x0806, arp(src))
    if name == "dhcp4-discover":
        return ethernet(BCAST, src, 0x0800, ipv4_udp(68, 67, dhcpv4(1, src)))
    if name == "dhcp4-offer":
        return ethernet(BCAST, src, 0x0800,
                        ipv4_udp(67, 68, dhcpv4(2, dst), src="192.0.2.1"))
    if name == "rs":
        return ethernet(bytes.fromhex("333300000002"), src, 0x86DD,
                        icmpv6_packet(133, "fe80::11", "ff02::2"))
    if name == "ra":
        return ethernet(bytes.fromhex("333300000001"), src, 0x86DD,
                        icmpv6_packet(134, "fe80::22", "ff02::1"))
    if name == "ns":
        return ethernet(bytes.fromhex("3333ff000022"), src, 0x86DD,
                        icmpv6_packet(135, "fe80::11", "ff02::1:ff00:22"))
    if name == "na":
        return ethernet(dst, src, 0x86DD,
                        icmpv6_packet(136, "fe80::22", "fe80::11"))
    if name == "dhcp6-solicit":
        return ethernet(bytes.fromhex("333300010002"), src, 0x86DD,
                        ipv6_udp(546, 547, dhcpv6(1, src), "fe80::11", "ff02::1:2"))
    if name == "dhcp6-reply":
        return ethernet(dst, src, 0x86DD,
                        ipv6_udp(547, 546, dhcpv6(7, dst, src), "fe80::22", "fe80::11"))
    raise ValueError(name)

def classify(frame):
    if len(frame) < 14:
        return None
    ethertype = struct.unpack("!H", frame[12:14])[0]
    payload = frame[14:]
    if ethertype == 0x0806 and len(payload) >= 28:
        htype, ptype, hlen, plen, operation = struct.unpack("!HHBBH", payload[:8])
        if (htype, ptype, hlen, plen, operation) == (1, 0x0800, 6, 4, 1):
            return "arp"
    if ethertype == 0x0800 and len(payload) >= 28 and payload[9] == 17:
        ihl = (payload[0] & 0x0f) * 4
        if checksum(payload[:ihl]) != 0:
            return None
        sport, dport = struct.unpack("!HH", payload[ihl:ihl + 4])
        dhcp = payload[ihl + 8:]
        if len(dhcp) < 244 or dhcp[236:240] != b"\x63\x82\x53\x63":
            return None
        message_type = dhcp[242] if dhcp[240:242] == b"\x35\x01" else 0
        xid = struct.unpack("!I", dhcp[4:8])[0]
        client_mac = dhcp[28:34]
        if (sport, dport, message_type, xid, client_mac) == (68, 67, 1, 0x12345678, SRC):
            return "dhcp4-discover"
        if (sport, dport, message_type, xid, client_mac) == (67, 68, 2, 0x12345678, SRC):
            return "dhcp4-offer"
    if ethertype == 0x86DD and len(payload) >= 44:
        next_header = payload[6]
        upper = payload[40:]
        if next_header == 58:
            names = {133: "rs", 134: "ra", 135: "ns", 136: "na"}
            name = names.get(upper[0])
            minimum = {133: 8, 134: 16, 135: 24, 136: 24}
            src_ip = str(ipaddress.IPv6Address(payload[8:24]))
            dst_ip = str(ipaddress.IPv6Address(payload[24:40]))
            if (name and len(upper) >= minimum[upper[0]] and
                    checksum(ipv6_pseudo(src_ip, dst_ip, 58, len(upper)) + upper) == 0):
                return name
        if next_header == 17:
            sport, dport = struct.unpack("!HH", upper[:4])
            src_ip = str(ipaddress.IPv6Address(payload[8:24]))
            dst_ip = str(ipaddress.IPv6Address(payload[24:40]))
            if checksum(ipv6_pseudo(src_ip, dst_ip, 17, len(upper)) + upper) != 0:
                return None
            message_type = upper[8] if len(upper) >= 12 else 0
            transaction_id = upper[9:12]
            options = dhcpv6_options(upper[12:])
            client_duid = struct.pack("!HH", 3, 1) + SRC
            server_duid = struct.pack("!HH", 3, 1) + DST
            if ((sport, dport, message_type, transaction_id) ==
                    (546, 547, 1, b"\x12\x34\x56") and options.get(1) == client_duid):
                return "dhcp6-solicit"
            if ((sport, dport, message_type, transaction_id) ==
                    (547, 546, 7, b"\x12\x34\x56") and
                    options.get(1) == client_duid and options.get(2) == server_duid):
                return "dhcp6-reply"
    return None

mode = sys.argv[1]
names = sys.argv[2:]
sock = socket.socket(socket.AF_PACKET, socket.SOCK_RAW, socket.htons(0x0003))
sock.bind((IFNAME, 0))
if mode == "send":
    reverse = names[0] == "reverse"
    for name in names[1:]:
        sock.send(build(name, reverse))
        time.sleep(0.1)
else:
    pending = set(names)
    deadline = time.monotonic() + 8
    while pending and time.monotonic() < deadline:
        sock.settimeout(max(0.1, deadline - time.monotonic()))
        try:
            name = classify(sock.recv(4096))
        except socket.timeout:
            break
        if name in pending:
            print(name, flush=True)
            pending.remove(name)
    if pending:
        print("missing: " + ",".join(sorted(pending)), file=sys.stderr)
        sys.exit(1)
PY

run_direction() {
  local receiver="$1" sender="$2" reverse="$3"
  shift 3
  local result="$WORKDIR/${receiver}.result"
  ip netns exec "$receiver" timeout 10 python3 "$WORKDIR/l2-probe.py" receive "$@" >"$result" &
  local receiver_pid=$!
  sleep 0.3
  ip netns exec "$sender" python3 "$WORKDIR/l2-probe.py" send "$reverse" "$@"
  wait "$receiver_pid" || fail "control-plane frames did not cross VXLAN toward $receiver"
  [[ "$(wc -l <"$result")" -eq "$#" ]] || fail "unexpected receive count toward $receiver"
}

run_direction "$RIGHT_CLIENT" "$LEFT_CLIENT" forward \
  arp dhcp4-discover rs ns dhcp6-solicit
run_direction "$LEFT_CLIENT" "$RIGHT_CLIENT" reverse \
  arp dhcp4-offer ra na dhcp6-reply

log "PASS: ARP, DHCPv4, IPv6 RS/RA/NS/NA, and DHCPv6 crossed the VXLAN bridge in both required directions"
