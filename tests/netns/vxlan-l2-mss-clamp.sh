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

# Build and invoke the production controller/renderer; this test carries no
# hand-written nft policy.
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
(cd "$REPO_ROOT" && go build -o "$WORKDIR/path-mtu-driver" ./tests/netns/path-mtu-controller-driver)
OWNER="$WORKDIR/l2-mss.owner"
POLICY="$WORKDIR/l2-mss.nft"

# A failing command later in one nft batch must roll back the preceding strict
# table creation.
cat >"$WORKDIR/rollback.nft" <<'NFT'
create table bridge routerd_l2_rollback
add rule bridge routerd_l2_rollback missing_chain counter
NFT
! ip netns exec "$LEFT" nft -f "$WORKDIR/rollback.nft" >/dev/null 2>&1
! ip netns exec "$LEFT" nft list table bridge routerd_l2_rollback >/dev/null 2>&1

# Exclusive-create collision: a foreign table occupying the random candidate
# name makes the entire production batch fail with EEXIST and remains intact.
collision_token=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
collision_table=routerd_l2_aaaaaaaaaaaa
ip netns exec "$LEFT" nft create table bridge "$collision_table"
ip netns exec "$LEFT" nft create chain bridge "$collision_table" foreign_before_create
! ip netns exec "$LEFT" env ROUTERD_NETNS_TEST_TOKEN="$collision_token" "$WORKDIR/path-mtu-driver" enable "$WORKDIR/collision.nft" "$WORKDIR/collision.owner" >/dev/null 2>&1
ip netns exec "$LEFT" nft list chain bridge "$collision_table" foreign_before_create >/dev/null
! ip netns exec "$LEFT" nft list chain bridge "$collision_table" rules_aaaaaaaaaaaa >/dev/null 2>&1
ip netns exec "$LEFT" nft delete table bridge "$collision_table"

# Crash boundary 1: phase-one creation is deliberately inert until its exact
# kernel handle is durable. A crash before that journal write must never leave
# a hooked chain, and recovery must quarantine rather than adopt handle zero.
stage_token=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
stage_table=routerd_l2_bbbbbbbbbbbb
! ip netns exec "$LEFT" env ROUTERD_NETNS_TEST_TOKEN="$stage_token" ROUTERD_NETNS_FAILPOINT=stage-created-before-handle-journal \
  "$WORKDIR/path-mtu-driver" enable "$WORKDIR/stage-crash.nft" "$WORKDIR/stage-crash.owner" >/dev/null 2>&1
ip netns exec "$LEFT" nft list chain bridge "$stage_table" rules_bbbbbbbbbbbb >"$WORKDIR/stage-chain.txt"
! grep -q 'hook forward' "$WORKDIR/stage-chain.txt"
python3 -c 'import json,sys;m=json.load(open(sys.argv[1]));g=m["generations"][0];assert g["state"]=="staged" and g.get("handle",0)==0 and not m.get("activeToken")' "$WORKDIR/stage-crash.owner"
ip netns exec "$LEFT" "$WORKDIR/path-mtu-driver" enable "$WORKDIR/stage-crash.nft" "$WORKDIR/stage-crash.owner"
python3 -c 'import json,sys;m=json.load(open(sys.argv[1]));assert any(g["state"]=="foreign" and g.get("handle",0)==0 for g in m["generations"]);assert sum(g["state"]=="active" for g in m["generations"])==1' "$WORKDIR/stage-crash.owner"
# The quarantined object is test-owned, inert and uniquely named; remove it as
# harness cleanup only (the production controller correctly refuses name use).
ip netns exec "$LEFT" nft delete table bridge "$stage_table"
ip netns exec "$LEFT" "$WORKDIR/path-mtu-driver" disable "$WORKDIR/stage-crash.nft" "$WORKDIR/stage-crash.owner"

# A: without policy, oversized values pass unchanged.
probe 4 1460 1460
probe 6 1440 1440
# Establish old active A, then change the desired MTU so phase two creates B.
# A crash after B's exact handle is durable but before promotion leaves two
# hooks; recovery must promote B and retire/delete A in the same journal step.
ip netns exec "$LEFT" "$WORKDIR/path-mtu-driver" enable "$POLICY" "$OWNER"
read -r prior_table prior_handle < <(python3 -c 'import json,sys;m=json.load(open(sys.argv[1]));g=next(x for x in m["generations"] if x["state"]=="active");print(g["table"],g["handle"])' "$OWNER")
! ip netns exec "$LEFT" env ROUTERD_NETNS_MTU=1290 ROUTERD_NETNS_FAILPOINT=active-hook-created-before-promotion \
  "$WORKDIR/path-mtu-driver" enable "$POLICY" "$OWNER" >/dev/null 2>&1
read -r crash_table crash_handle < <(python3 -c 'import json,sys;m=json.load(open(sys.argv[1]));g=next(x for x in m["generations"] if x["state"]=="activating");assert g["handle"]>0;print(g["table"],g["handle"])' "$OWNER")
ip netns exec "$LEFT" nft list table bridge "$prior_table" >/dev/null
ip netns exec "$LEFT" nft list table bridge "$crash_table" >/dev/null
ip netns exec "$LEFT" env ROUTERD_NETNS_MTU=1290 "$WORKDIR/path-mtu-driver" enable "$POLICY" "$OWNER"
read -r recovered_table recovered_handle < <(python3 -c 'import json,sys;m=json.load(open(sys.argv[1]));g=next(x for x in m["generations"] if x["state"]=="active");print(g["table"],g["handle"])' "$OWNER")
[[ "$crash_table" == "$recovered_table" && "$crash_handle" == "$recovered_handle" ]]
! ip netns exec "$LEFT" nft list table bridge "$prior_table" >/dev/null 2>&1
# Restore the normal 1280-byte policy for the A/B data-plane assertions.
ip netns exec "$LEFT" "$WORKDIR/path-mtu-driver" enable "$POLICY" "$OWNER"

# B: oversize is reduced, smaller MSS and non-TCP Ethernet remain untouched.
probe 4 1460 1240
probe 6 1440 1220
probe 4 1200 1200
probe 6 1180 1180
probe udp 0 udp-pass

# Delete one live rule while retaining table handle and proof comment. The
# production observer must compare the complete canonical ruleset, rotate to a
# fresh generation, and restore all four rules.
read -r drift_table drift_chain drift_handle < <(python3 -c 'import json,sys;m=json.load(open(sys.argv[1]));g=next(x for x in m["generations"] if x["state"]=="active");print(g["table"],g["chain"],g["handle"])' "$OWNER")
rule_handle="$(ip netns exec "$LEFT" nft -a list chain bridge "$drift_table" "$drift_chain" | sed -n 's/.*tcp option maxseg size set.*# handle \([0-9][0-9]*\).*/\1/p' | head -1)"
[[ -n "$rule_handle" ]]
ip netns exec "$LEFT" nft delete rule bridge "$drift_table" "$drift_chain" handle "$rule_handle"
ip netns exec "$LEFT" "$WORKDIR/path-mtu-driver" enable "$POLICY" "$OWNER"
read -r repaired_table repaired_chain repaired_handle < <(python3 -c 'import json,sys;m=json.load(open(sys.argv[1]));g=next(x for x in m["generations"] if x["state"]=="active");print(g["table"],g["chain"],g["handle"])' "$OWNER")
[[ "$repaired_table" != "$drift_table" && "$repaired_handle" != "$drift_handle" ]]
[[ "$(ip netns exec "$LEFT" nft list chain bridge "$repaired_table" "$repaired_chain" | grep -c 'tcp option maxseg size set')" -eq 4 ]]
probe 4 1460 1240

# Preserve the private proof and exact table handle but inject an unknown DROP
# expression into the owned rules chain. This is owned content drift, not a
# foreign replacement: reconcile must delete the exact old table handle before
# leaving the repaired generation active.
read -r drop_table drop_chain drop_handle < <(python3 -c 'import json,sys;m=json.load(open(sys.argv[1]));g=next(x for x in m["generations"] if x["state"]=="active");print(g["table"],g["chain"],g["handle"])' "$OWNER")
ip netns exec "$LEFT" nft add rule bridge "$drop_table" "$drop_chain" counter drop
same_handle="$(ip netns exec "$LEFT" nft -a list table bridge "$drop_table" | sed -n 's/.*# handle \([0-9][0-9]*\).*/\1/p' | head -1)"
[[ "$same_handle" == "$drop_handle" ]]
cat >"$WORKDIR/nft-delete-error" <<'SH'
#!/bin/sh
if [ "$1" = "delete" ] && [ "$2" = "table" ] && [ "$3" = "bridge" ] && [ "$4" = "handle" ]; then
  echo 'injected exact-handle delete failure' >&2
  exit 1
fi
exec nft "$@"
SH
chmod 0755 "$WORKDIR/nft-delete-error"
! ip netns exec "$LEFT" env ROUTERD_NETNS_NFT="$WORKDIR/nft-delete-error" "$WORKDIR/path-mtu-driver" enable "$POLICY" "$OWNER" >/dev/null 2>&1
python3 -c 'import json,sys;m=json.load(open(sys.argv[1]));h=int(sys.argv[2]);assert any(g["state"]=="retired" and g["handle"]==h for g in m["generations"]);assert sum(g["state"]=="active" for g in m["generations"])==1' "$OWNER" "$drop_handle"
ip netns exec "$LEFT" nft list table bridge "$drop_table" >/dev/null
ip netns exec "$LEFT" "$WORKDIR/path-mtu-driver" enable "$POLICY" "$OWNER"
read -r drop_repaired_table drop_repaired_handle < <(python3 -c 'import json,sys;m=json.load(open(sys.argv[1]));g=next(x for x in m["generations"] if x["state"]=="active");print(g["table"],g["handle"])' "$OWNER")
[[ "$drop_repaired_table" != "$drop_table" && "$drop_repaired_handle" != "$drop_handle" ]]
! ip netns exec "$LEFT" nft list table bridge "$drop_table" >/dev/null 2>&1
probe 4 1460 1240

# Keep the exact durable table handle but replace both chains without their
# HMAC comments and install DROP. The journal handle still proves ownership;
# proof loss is content drift and the exact table must be retired/deleted.
read -r proof_table proof_chain proof_handle < <(python3 -c 'import json,sys;m=json.load(open(sys.argv[1]));g=next(x for x in m["generations"] if x["state"]=="active");print(g["table"],g["chain"],g["handle"])' "$OWNER")
proof_hook="forward_${proof_chain#rules_}"
cat >"$WORKDIR/proof-loss.nft" <<NFT
flush chain bridge $proof_table $proof_hook
delete chain bridge $proof_table $proof_hook
flush chain bridge $proof_table $proof_chain
delete chain bridge $proof_table $proof_chain
create chain bridge $proof_table $proof_chain
add rule bridge $proof_table $proof_chain counter drop
create chain bridge $proof_table $proof_hook { type filter hook forward priority -150; policy accept; }
add rule bridge $proof_table $proof_hook jump $proof_chain
NFT
ip netns exec "$LEFT" nft -f "$WORKDIR/proof-loss.nft"
proof_same_handle="$(ip netns exec "$LEFT" nft -a list table bridge "$proof_table" | sed -n 's/.*# handle \([0-9][0-9]*\).*/\1/p' | head -1)"
[[ "$proof_same_handle" == "$proof_handle" ]]
ip netns exec "$LEFT" "$WORKDIR/path-mtu-driver" enable "$POLICY" "$OWNER"
read -r proof_repaired_table proof_repaired_handle < <(python3 -c 'import json,sys;m=json.load(open(sys.argv[1]));g=next(x for x in m["generations"] if x["state"]=="active");print(g["table"],g["handle"])' "$OWNER")
[[ "$proof_repaired_table" != "$proof_table" && "$proof_repaired_handle" != "$proof_handle" ]]
! ip netns exec "$LEFT" nft list table bridge "$proof_table" >/dev/null 2>&1
probe 4 1460 1240

# Deterministic proof-replay replacement: dump and recreate the exact owned
# table, including its public proof and rules. The new kernel object has a new
# handle; the controller must not rebind the nonzero journal handle to it.
read -r old_table old_chain old_handle < <(python3 -c 'import json,sys;m=json.load(open(sys.argv[1]));g=next(x for x in m["generations"] if x["state"]=="active");print(g["table"],g["chain"],g["handle"])' "$OWNER")
ip netns exec "$LEFT" nft list table bridge "$old_table" >"$WORKDIR/replay.nft"
ip netns exec "$LEFT" nft delete table bridge "$old_table"
ip netns exec "$LEFT" nft -f "$WORKDIR/replay.nft"
replacement_handle="$(ip netns exec "$LEFT" nft -a list table bridge "$old_table" | sed -n 's/.*# handle \([0-9][0-9]*\).*/\1/p' | head -1)"
[[ -n "$replacement_handle" && "$replacement_handle" != "$old_handle" ]]
ip netns exec "$LEFT" "$WORKDIR/path-mtu-driver" enable "$POLICY" "$OWNER"
read -r new_table new_chain < <(python3 -c 'import json,sys;m=json.load(open(sys.argv[1]));g=next(x for x in m["generations"] if x["state"]=="active");print(g["table"],g["chain"])' "$OWNER")
[[ "$old_table" != "$new_table" ]]
ip netns exec "$LEFT" nft list chain bridge "$old_table" "$old_chain" >/dev/null
probe 4 1460 1240

# A transient/permission list failure is not ENOENT: fail closed and preserve
# the owner journal byte-for-byte instead of losing the exact live handle.
cp "$OWNER" "$WORKDIR/owner.before-list-error"
cat >"$WORKDIR/nft-list-error" <<'SH'
#!/bin/sh
if [ "$1" = "--json" ]; then echo 'Operation not permitted' >&2; exit 1; fi
exec nft "$@"
SH
chmod 0755 "$WORKDIR/nft-list-error"
! ip netns exec "$LEFT" env ROUTERD_NETNS_NFT="$WORKDIR/nft-list-error" "$WORKDIR/path-mtu-driver" enable "$POLICY" "$OWNER" >/dev/null 2>&1
cmp "$WORKDIR/owner.before-list-error" "$OWNER"

# Removal also goes through the production controller and leaves non-TCP/L2
# forwarding untouched.
ip netns exec "$LEFT" "$WORKDIR/path-mtu-driver" disable "$POLICY" "$OWNER"
# The replayed foreign generation remains untouched even on disable. It still
# clamps traffic, so remove it only as explicit harness cleanup before the
# no-policy forwarding assertion.
ip netns exec "$LEFT" nft list chain bridge "$old_table" "$old_chain" >/dev/null
ip netns exec "$LEFT" nft delete table bridge "$old_table"
probe 4 1460 1460
probe udp 0 udp-pass
! ip netns exec "$LEFT" nft list chain bridge "$new_table" "$new_chain" >/dev/null 2>&1
echo "vxlan L2 MSS clamp A/B PASS"
