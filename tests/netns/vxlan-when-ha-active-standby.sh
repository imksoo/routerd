#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"
require_common

ON_A="${TEST_ID}-on-a"; ON_B="${TEST_ID}-on-b"
OCI_A="${TEST_ID}-oci-a"; OCI_B="${TEST_ID}-oci-b"
LEFT="${TEST_ID}-left"; RIGHT="${TEST_ID}-right"
for ns in "$ON_A" "$ON_B" "$OCI_A" "$OCI_B" "$LEFT" "$RIGHT"; do create_ns "$ns"; done

UNDERLAY="${TEST_IF_ID}u"; ON_LAN="${TEST_IF_ID}o"; OCI_LAN="${TEST_IF_ID}c"
create_bridge_segment "$UNDERLAY" \
  "$ON_A" ul0 198.18.113.1/24 "$ON_B" ul0 198.18.113.2/24 \
  "$OCI_A" ul0 198.18.113.3/24 "$OCI_B" ul0 198.18.113.4/24
create_bridge_segment "$ON_LAN" "$ON_A" lan0 169.254.113.1/32 "$ON_B" lan0 169.254.113.2/32 "$LEFT" eth0 169.254.113.11/32
create_bridge_segment "$OCI_LAN" "$OCI_A" lan0 169.254.113.3/32 "$OCI_B" lan0 169.254.113.4/32 "$RIGHT" eth0 169.254.113.12/32

for ns in "$ON_A" "$ON_B" "$OCI_A" "$OCI_B"; do
  ip -n "$ns" link add br-l2 type bridge stp_state 0
  ip -n "$ns" link set lan0 master br-l2
  ip -n "$ns" link set br-l2 up
  ip -n "$ns" addr flush dev lan0
done
ip -n "$LEFT" addr flush dev eth0
ip -n "$RIGHT" addr flush dev eth0
ip -n "$LEFT" link set eth0 address 02:00:00:00:11:31
ip -n "$RIGHT" link set eth0 address 02:00:00:00:11:32

# Build the small namespace adapter once. It invokes the production chain
# controller; the shell harness only supplies producer state and packets.
CONTROLLER="$WORKDIR/vxlan-controller-driver"
go build -o "$CONTROLLER" ./tests/netns/vxlan-controller-driver
gate() {
  local ns="$1" role="$2" witness="$3" local_ip="$4" peer_ip="$5"
  "$CONTROLLER" "$ns" "$role" "$witness" "$local_ip" "$peer_ip" 200113
}

cat >"$WORKDIR/frame.py" <<'PY'
import socket, struct, sys, time
mode, marker = sys.argv[1], sys.argv[2].encode()
s = socket.socket(socket.AF_PACKET, socket.SOCK_RAW, socket.htons(0x0003))
s.bind(("eth0", 0))
types = {b"dhcp4-discover": 1, b"dhcp4-offer": 2, b"dhcp4-request": 3, b"dhcp4-ack": 5}

def protocol_frame(name, src):
    if name == b"arp":
        body = struct.pack("!HHBBH", 1, 0x0800, 6, 4, 1) + src + socket.inet_aton("192.0.2.11") + b"\0"*6 + socket.inet_aton("192.0.2.12") + name
        return b"\xff"*6 + src + b"\x08\x06" + body
    icmp = {b"ra": 134, b"nd": 135}
    if name in icmp:
        payload = bytes([icmp[name], 0, 0, 0]) + name
        header = struct.pack("!IHBB16s16s", 0x60000000, len(payload), 58, 255, b"\x20\x01\x0d\xb8"+b"\0"*12, b"\xff\x02"+b"\0"*13+b"\x01")
        return b"\x33\x33\x00\x00\x00\x01" + src + b"\x86\xdd" + header + payload
    if name == b"dhcp6":
        payload = b"\x01\x11\x31\x13" + name
        udp = struct.pack("!HHHH", 546, 547, 8+len(payload), 0) + payload
        header = struct.pack("!IHBB16s16s", 0x60000000, len(udp), 17, 64, b"\x20\x01\x0d\xb8"+b"\0"*12, b"\xff\x02"+b"\0"*11+b"\x01\x00\x02")
        return b"\x33\x33\x00\x01\x00\x02" + src + b"\x86\xdd" + header + udp
    return b"\xff"*6 + src + b"\x88\xb5" + name

def ipv4_checksum(data):
    if len(data) % 2: data += b"\0"
    total = sum(struct.unpack("!%dH" % (len(data)//2), data))
    while total >> 16: total = (total & 0xffff) + (total >> 16)
    return (~total) & 0xffff

def dhcp_frame(name, src):
    mt = types[name]; reply = mt in (2, 5); op = 2 if reply else 1
    fixed = struct.pack("!BBBBIHH", op, 1, 6, 0, 0x11311131, 0, 0x8000)
    fixed += socket.inet_aton("0.0.0.0") + socket.inet_aton("192.0.2.113" if reply else "0.0.0.0")
    fixed += socket.inet_aton("0.0.0.0") * 2
    fixed += bytes.fromhex("020000001131").ljust(16, b"\0") + b"\0" * 192
    bootp = fixed + b"\x63\x82\x53\x63\x35\x01" + bytes([mt]) + b"\xff"
    sport, dport = ((67, 68) if reply else (68, 67))
    udp = struct.pack("!HHHH", sport, dport, 8 + len(bootp), 0) + bootp
    sip = socket.inet_aton("192.0.2.1" if reply else "0.0.0.0")
    dip = socket.inet_aton("255.255.255.255")
    ip = struct.pack("!BBHHHBBH4s4s", 0x45, 0, 20+len(udp), 0x1131, 0, 64, 17, 0, sip, dip)
    ip = ip[:10] + struct.pack("!H", ipv4_checksum(ip)) + ip[12:]
    return b"\xff"*6 + src + b"\x08\x00" + ip + udp

def classify(frame):
    if len(frame) < 14 or frame[12:14] != b"\x08\x00": return None
    ip = frame[14:]; ihl=(ip[0]&15)*4
    if len(ip) < ihl+8+244 or ip[9] != 17: return None
    sport,dport=struct.unpack("!HH", ip[ihl:ihl+4]); bootp=ip[ihl+8:]
    if bootp[4:8] != struct.pack("!I", 0x11311131) or bootp[236:240] != b"\x63\x82\x53\x63": return None
    mt = bootp[242] if bootp[240:242] == b"\x35\x01" else 0
    names={1:b"dhcp4-discover",2:b"dhcp4-offer",3:b"dhcp4-request",5:b"dhcp4-ack"}
    if (sport,dport) != ((67,68) if mt in (2,5) else (68,67)): return None
    return names.get(mt)

def protocol_match(frame, name):
    if len(frame) < 14: return False
    et = frame[12:14]
    if name == b"arp": return et == b"\x08\x06" and name in frame[14:]
    if name in (b"ra", b"nd"):
        return et == b"\x86\xdd" and len(frame) > 58 and frame[20] == 58 and frame[54] == {b"ra":134,b"nd":135}[name] and name in frame[54:]
    if name == b"dhcp6": return et == b"\x86\xdd" and len(frame) > 62 and frame[20] == 17 and frame[54:58] == struct.pack("!HH",546,547) and name in frame[62:]
    return et == b"\x88\xb5" and name in frame[14:]

if mode == "send":
    src = bytes.fromhex(sys.argv[3].replace(":", ""))
    s.send(dhcp_frame(marker, src) if marker in types else protocol_frame(marker, src))
else:
    s.settimeout(.15); end=time.monotonic()+1.25; count=0
    while time.monotonic() < end:
        try: frame=s.recv(2048)
        except TimeoutError: continue
        if (marker in types and classify(frame) == marker) or (marker not in types and protocol_match(frame, marker)): count += 1
    print(count)
PY

expect_frames() {
	local src="$1" dst="$2" marker="$3" expected="$4" mac="$5"
	local out="$WORKDIR/$marker.count"
  ip netns exec "$dst" python3 "$WORKDIR/frame.py" recv "$marker" >"$out" & local receiver=$!
  sleep .2
  ip netns exec "$src" python3 "$WORKDIR/frame.py" send "$marker" "$mac"
  wait "$receiver"
  [[ "$(cat "$out")" == "$expected" ]] || fail "$marker received $(cat "$out"), want $expected"
}

assert_single_forwarder_pair() {
  local count=0 ns
  for ns in "$ON_A" "$ON_B" "$OCI_A" "$OCI_B"; do
    ip -n "$ns" link show vx-l2 >/dev/null 2>&1 && count=$((count+1))
  done
  [[ "$count" == 2 ]] || fail "active VXLAN links=$count, want one endpoint per site"
}

# Inject dual MASTER everywhere. Only candidate A has the independent witness.
gate "$ON_A" master leader 198.18.113.1 198.18.113.3
gate "$ON_B" master standby 198.18.113.2 198.18.113.4
gate "$OCI_A" master leader 198.18.113.3 198.18.113.1
gate "$OCI_B" master standby 198.18.113.4 198.18.113.2
assert_single_forwarder_pair
expect_frames "$LEFT" "$RIGHT" BUM-A 1 02:00:00:00:11:31
for proto in arp ra nd dhcp6; do expect_frames "$LEFT" "$RIGHT" "$proto" 1 02:00:00:00:11:31; done
for phase in discover request; do expect_frames "$LEFT" "$RIGHT" "dhcp4-$phase" 1 02:00:00:00:11:31; done
for phase in offer ack; do expect_frames "$RIGHT" "$LEFT" "dhcp4-$phase" 1 02:00:00:00:11:32; done

# Witness loss is fail closed even though both local elections still say MASTER.
for spec in "$ON_A 198.18.113.1 198.18.113.3" "$ON_B 198.18.113.2 198.18.113.4" \
            "$OCI_A 198.18.113.3 198.18.113.1" "$OCI_B 198.18.113.4 198.18.113.2"; do
  read -r ns local_ip peer_ip <<<"$spec"; gate "$ns" master unknown "$local_ip" "$peer_ip"
done
expect_frames "$LEFT" "$RIGHT" WITNESS-LOSS 0 02:00:00:00:11:31

# Fenced failover to candidate B and verify DORA/BUM are still delivered once.
gate "$ON_A" backup standby 198.18.113.1 198.18.113.3
gate "$OCI_A" backup standby 198.18.113.3 198.18.113.1
gate "$ON_B" master leader 198.18.113.2 198.18.113.4
gate "$OCI_B" master leader 198.18.113.4 198.18.113.2
assert_single_forwarder_pair
expect_frames "$LEFT" "$RIGHT" BUM-B 1 02:00:00:00:11:31
for proto in arp ra nd dhcp6; do expect_frames "$LEFT" "$RIGHT" "$proto" 1 02:00:00:00:11:31; done
for phase in discover request; do expect_frames "$LEFT" "$RIGHT" "dhcp4-$phase" 1 02:00:00:00:11:31; done
for phase in offer ack; do expect_frames "$RIGHT" "$LEFT" "dhcp4-$phase" 1 02:00:00:00:11:32; done

# Exactly one learned source entry on the remote site detects duplicate ports
# and the most common loop/MAC-flap failure mode.
learned="$(bridge fdb show br "$OCI_LAN" | grep -ci '02:00:00:00:11:31' || true)"
[[ "$learned" == 1 ]] || fail "remote learned source entries=$learned, want 1"
log "PASS: dual-MASTER fenced, witness loss fail-closed, failover DORA/BUM single-copy"
