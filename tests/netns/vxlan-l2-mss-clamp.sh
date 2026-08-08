#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tests/netns/lib.sh
source "$SCRIPT_DIR/lib.sh"
require_common
for command in nft python3; do
  command -v "$command" >/dev/null || { echo "missing $command" >&2; exit 77; }
done

LEFT="${TEST_ID}-mss-left"; RIGHT="${TEST_ID}-mss-right"
LC="${TEST_ID}-mss-lc"; RC="${TEST_ID}-mss-rc"
for ns in "$LEFT" "$RIGHT" "$LC" "$RC"; do create_ns "$ns"; done
create_veth_pair "$LEFT" underlay 198.18.0.1/30 "$RIGHT" underlay 198.18.0.2/30

for ns in "$LEFT" "$RIGHT"; do
  ip -n "$ns" link add br0 type bridge
  ip -n "$ns" link set br0 up
done

VETH_COUNTER=$((VETH_COUNTER+1)); la="${TEST_IF_ID}m${VETH_COUNTER}a"; lb="${TEST_IF_ID}m${VETH_COUNTER}b"
ip link add "$la" type veth peer name "$lb"; ip link set "$la" netns "$LEFT"; ip link set "$lb" netns "$LC"
ip -n "$LEFT" link set "$la" name lan0; ip -n "$LEFT" link set lan0 master br0; ip -n "$LEFT" link set lan0 up
ip -n "$LC" link set "$lb" name eth0; ip -n "$LC" link set eth0 address 02:00:00:00:01:01; ip -n "$LC" link set eth0 up

VETH_COUNTER=$((VETH_COUNTER+1)); ra="${TEST_IF_ID}m${VETH_COUNTER}a"; rb="${TEST_IF_ID}m${VETH_COUNTER}b"
ip link add "$ra" type veth peer name "$rb"; ip link set "$ra" netns "$RIGHT"; ip link set "$rb" netns "$RC"
ip -n "$RIGHT" link set "$ra" name lan0; ip -n "$RIGHT" link set lan0 master br0; ip -n "$RIGHT" link set lan0 up
ip -n "$RC" link set "$rb" name eth0; ip -n "$RC" link set eth0 address 02:00:00:00:02:02; ip -n "$RC" link set eth0 up

for side in "$LEFT" "$RIGHT"; do
  local_ip=198.18.0.1; remote_ip=198.18.0.2
  [[ "$side" == "$RIGHT" ]] && { local_ip=198.18.0.2; remote_ip=198.18.0.1; }
  ip -n "$side" link add vx0 type vxlan id 1128 local "$local_ip" dev underlay dstport 4789 nolearning
  ip -n "$side" link set vx0 master br0; ip -n "$side" link set vx0 mtu 1280; ip -n "$side" link set vx0 up
  ip netns exec "$side" bridge fdb append 00:00:00:00:00:00 dev vx0 dst "$remote_ip"
done

cat >"$WORKDIR/probe.py" <<'PY'
import socket,struct,sys,time
mode,family,value=sys.argv[1],sys.argv[2],sys.argv[3]
src=bytes.fromhex('020000000101'); dst=bytes.fromhex('020000000202')
def csum(data):
    if len(data)%2:data+=b'\0'
    s=sum(struct.unpack('!%dH'%(len(data)//2),data))
    while s>>16:s=(s&65535)+(s>>16)
    return (~s)&65535
def frame():
    if family=='udp': return dst+src+struct.pack('!H',0x88b5)+b'routerd-nontcp-transparent'
    mss=int(value); opt=struct.pack('!BBH',2,4,mss)
    tcp=struct.pack('!HHIIBBHHH',12345,443,1,0,6<<4,2,65535,0,0)+opt
    if family=='4':
        ip=struct.pack('!BBHHHBBH4s4s',0x45,0,20+len(tcp),1,0,64,6,0,socket.inet_aton('192.0.2.1'),socket.inet_aton('192.0.2.2'))
        ip=ip[:10]+struct.pack('!H',csum(ip))+ip[12:]
        return dst+src+struct.pack('!H',0x0800)+ip+tcp
    ip6=struct.pack('!IHBB16s16s',6<<28,len(tcp),6,64,socket.inet_pton(socket.AF_INET6,'2001:db8::1'),socket.inet_pton(socket.AF_INET6,'2001:db8::2'))
    return dst+src+struct.pack('!H',0x86dd)+ip6+tcp
s=socket.socket(socket.AF_PACKET,socket.SOCK_RAW,socket.htons(3)); s.bind(('eth0',0))
if mode=='send': s.send(frame()); sys.exit(0)
s.settimeout(5); deadline=time.time()+5
while time.time()<deadline:
    p=s.recv(4096)
    if p[0:6]!=dst: continue
    et=struct.unpack('!H',p[12:14])[0]
    if family=='udp' and et==0x88b5 and b'routerd-nontcp-transparent' in p: print('udp-pass');sys.exit(0)
    if family=='4' and et==0x0800: off=14+(p[14]&15)*4
    elif family=='6' and et==0x86dd: off=14+40
    else: continue
    if p[off+13]&2:
        print(struct.unpack('!H',p[off+22:off+24])[0]);sys.exit(0)
raise SystemExit('probe timeout')
PY

probe() {
  family=$1; sent=$2; expected=$3
  out="$WORKDIR/out-$family-$sent-$expected"
  ip netns exec "$RC" python3 "$WORKDIR/probe.py" recv "$family" "$sent" >"$out" & pid=$!
  sleep .2; ip netns exec "$LC" python3 "$WORKDIR/probe.py" send "$family" "$sent"
  wait "$pid"; [[ "$(cat "$out")" == "$expected" ]]
}

# A: without policy, oversized values pass unchanged.
probe 4 1460 1460
probe 6 1440 1440

cat >"$WORKDIR/l2-mss.nft" <<'NFT'
add table bridge routerd_l2_mss
flush table bridge routerd_l2_mss
table bridge routerd_l2_mss {
 comment "routerd.owner=routerd routerd.generation=1 routerd.l2-mss.digest=netns"
 chain forward {
  type filter hook forward priority -150; policy accept;
  iifname "lan0" oifname "vx0" ether type ip ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1240 tcp option maxseg size set 1240
  iifname "lan0" oifname "vx0" ether type ip6 meta l4proto tcp tcp flags syn / syn,rst tcp option maxseg size > 1220 tcp option maxseg size set 1220
 }
}
NFT
ip netns exec "$LEFT" nft -c -f "$WORKDIR/l2-mss.nft"
ip netns exec "$LEFT" nft -f "$WORKDIR/l2-mss.nft"

# B: oversize is reduced, smaller MSS and non-TCP Ethernet remain untouched.
probe 4 1460 1240
probe 6 1440 1220
probe 4 1200 1200
probe 6 1180 1180
probe udp 0 udp-pass

ip netns exec "$LEFT" nft delete table bridge routerd_l2_mss
probe 4 1460 1460
echo "vxlan L2 MSS clamp A/B PASS"
