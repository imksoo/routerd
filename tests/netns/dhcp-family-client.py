#!/usr/bin/env python3
"""Bounded DHCPv4/DHCPv6/RA exchange inside an explicitly isolated netns."""
import json,os,select,socket,struct,sys,time

iface=sys.argv[1]
mac=bytes.fromhex(open('/sys/class/net/'+iface+'/address').read().strip().replace(':',''))
index=socket.if_nametoindex(iface)
xid=struct.unpack('!I',os.urandom(4))[0]
v4=socket.socket(socket.AF_INET,socket.SOCK_DGRAM)
v4.setsockopt(socket.SOL_SOCKET,socket.SO_BROADCAST,1)
v4.setsockopt(socket.SOL_SOCKET,socket.SO_BINDTODEVICE,iface.encode()+b'\0')
v4.bind(('',68))
v4.settimeout(8)
raw=socket.socket(socket.AF_PACKET,socket.SOCK_RAW,socket.htons(0x0800));raw.bind((iface,0))
def checksum(data):
    total=sum(struct.unpack('!'+str(len(data)//2)+'H',data));total=(total>>16)+(total&65535);total+=(total>>16)
    return (~total)&65535

def options(data):
    result={};i=240
    while i<len(data):
        key=data[i];i+=1
        if key==255:break
        if key==0:continue
        size=data[i];i+=1
        result[key]=data[i:i+size];i+=size
    return result
def request(kind,extra=b''):
    packet=struct.pack('!BBBBIHH4s4s4s4s16s64s128s',1,1,6,0,xid,0,0x8000,b'\0'*4,b'\0'*4,b'\0'*4,b'\0'*4,mac+b'\0'*10,b'\0'*64,b'\0'*128)
    packet+=b'\x63\x82\x53\x63'+bytes([53,1,kind])+b'\x3d\x07\x01'+mac+extra+b'\xff'
    packet=packet.ljust(300,b'\0')
    udp=struct.pack('!HHHH',68,67,len(packet)+8,0)+packet
    ip=struct.pack('!BBHHHBBH4s4s',0x45,0,len(udp)+20,1,0,64,17,0,b'\0'*4,b'\xff'*4)
    ip=ip[:10]+struct.pack('!H',checksum(ip))+ip[12:]
    raw.send(b'\xff'*6+mac+b'\x08\x00'+ip+udp)
def receive(kind):
    deadline=time.monotonic()+8
    while time.monotonic()<deadline:
        data,source=v4.recvfrom(4096)
        if len(data)>=240 and data[0]==2 and struct.unpack('!I',data[4:8])[0]==xid and options(data).get(53)==bytes([kind]):return data,source
    raise TimeoutError('no matching DHCP response; observation incomplete')
request(1)
offer,server=receive(2)
opts=options(offer)
request(3,b'\x32\x04'+offer[16:20]+b'\x36\x04'+opts[54])
ack,_=receive(5)
assert ack[16:20]==offer[16:20]
v4.close();raw.close()
v6=socket.socket(socket.AF_INET6,socket.SOCK_DGRAM)
v6.setsockopt(socket.IPPROTO_IPV6,socket.IPV6_MULTICAST_IF,index)
v6.bind(('fe80::2',546,0,index));v6.settimeout(8)
tx=os.urandom(3)
duid=struct.pack('!HH',3,1)+mac
msg=b'\x0b'+tx+struct.pack('!HH',1,len(duid))+duid+struct.pack('!HHH',6,2,23)
v6.sendto(msg,('ff02::1:2',547,0,index))
response,peer=v6.recvfrom(4096)
assert response[:4]==b'\x07'+tx,(response.hex(),peer)
i=4;opts6={}
while i+4<=len(response):
    key,size=struct.unpack('!HH',response[i:i+4]);i+=4
    opts6[key]=response[i:i+size];i+=size
assert opts6.get(23)==socket.inet_pton(socket.AF_INET6,'fd00:1234::1'),opts6
v6.close()
ra=socket.socket(socket.AF_INET6,socket.SOCK_RAW,socket.IPPROTO_ICMPV6)
ra.setsockopt(socket.IPPROTO_IPV6,socket.IPV6_MULTICAST_IF,index)
ra.setsockopt(socket.IPPROTO_IPV6,socket.IPV6_MULTICAST_HOPS,255)
ra.bind(('fe80::2',0,0,index));ra.settimeout(8)
ra.sendto(b'\x85\x00'+b'\0'*6,('ff02::2',0,0,index))
deadline=time.monotonic()+8
while time.monotonic()<deadline:
    packet,peer=ra.recvfrom(4096)
    if packet and packet[0]==134:
        assert struct.unpack('!H',packet[6:8])[0]>0, 'received withdrawal RA'
        break
else:raise TimeoutError('no matching RA; observation incomplete')
ra.close()
print(json.dumps({'xid':hex(xid),'dhcpv4':'offer-and-ack','address':socket.inet_ntoa(ack[16:20]),'dhcpv6':'information-reply-with-DNS','ra':'positive-router-lifetime'}))
