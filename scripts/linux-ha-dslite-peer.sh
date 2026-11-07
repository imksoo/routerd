#!/usr/bin/env bash
# Independent peers for tests/fixtures/linux-ha-dslite-pd.json.
# Run only in a dedicated disposable VM, after connecting rd15-lan/rd15-wan.
set -euo pipefail
if [ "${ROUTERD_DISPOSABLE_PEER:-}" != yes ] || [ "$EUID" != 0 ] || [ "$#" != 1 ]; then
 echo 'requires root, ROUTERD_DISPOSABLE_PEER=yes and extracted Kea root argument' >&2
 exit 64
fi
systemd-detect-virt --vm --quiet
if systemctl is-active --quiet routerd.service; then
 echo 'requires inactive routerd service' >&2
 exit 1
fi
ip link show rd15-lan >/dev/null
ip link show rd15-wan >/dev/null
kea_root=$(realpath "$1")
kea=$kea_root/usr/sbin/kea-dhcp6
work=/var/tmp/rd15-integrated-peer
test ! -e "$work"
for ns in rd15-inet-server rd15-ha-client rd15-aftr-{a,b,c,d}; do
 if ip netns list | awk '{print $1}' | grep -Fxq "$ns"; then
  echo "existing namespace: $ns" >&2
  exit 1
 fi
done
install -d -m 700 "$work" "$work/vty"
export LD_LIBRARY_PATH="$kea_root/usr/lib/x86_64-linux-gnu"
"$kea" -v
modprobe ip6_tunnel
sysctl -qw net.ipv4.ip_forward=1 net.ipv6.conf.all.forwarding=1
ip link add rd15-inet type bridge
ip link set rd15-inet mtu 1430 up
ip address add 203.0.113.10/24 dev rd15-inet
ip address add 203.0.113.20/24 dev rd15-inet
ip -6 address add 2001:db8:ffff::10/64 dev rd15-inet nodad
ip -6 route add 2001:db8:1500::/60 via fe80::5eff:fe00:131 dev rd15-wan
ip netns add rd15-inet-server
ip link add rd15-server type veth peer name eth0 netns rd15-inet-server
ip link set rd15-server master rd15-inet
ip link set rd15-server mtu 1430 up
ip -n rd15-inet-server link set lo up
ip -n rd15-inet-server link set eth0 mtu 1430 up
ip -n rd15-inet-server address add 203.0.113.80/24 dev eth0
ip -n rd15-inet-server -6 address add 2001:db8:ffff::80/64 dev eth0 nodad
ip -n rd15-inet-server address add 10.250.0.1/24 dev lo
ip -n rd15-inet-server address add 10.251.0.1/24 dev lo
ip -n rd15-inet-server route add default via 203.0.113.10
ip -n rd15-inet-server -6 route add default via 2001:db8:ffff::10
ip route add 10.250.0.0/24 via 203.0.113.80 dev rd15-inet
ip route add 10.251.0.0/24 via 203.0.113.80 dev rd15-inet
nft -f - <<'NFT'
table ip rd15_peer_nat {
 chain postrouting {
  type nat hook postrouting priority srcnat; policy accept;
  iifname "rd15-wan" oifname "rd15-inet" ip saddr 198.18.31.0/24 snat to 203.0.113.10
  iifname "rd15-lan" oifname "rd15-inet" ip saddr 198.18.29.0/24 snat to 203.0.113.20
 }
}
NFT
index=11; suffix=101
for letter in a b c d; do
 ns=rd15-aftr-$letter
 ip netns add "$ns"
 ip link add rd15-aw-$letter type veth peer name wan0 netns "$ns"
 ip link set rd15-aw-$letter master rd15-wan
 ip link set rd15-aw-$letter mtu 1430 up
 ip link add rd15-ai-$letter type veth peer name inet0 netns "$ns"
 ip link set rd15-ai-$letter master rd15-inet
 ip link set rd15-ai-$letter mtu 1430 up
 ip -n "$ns" link set lo up
 ip -n "$ns" link set wan0 mtu 1430 up
 ip -n "$ns" link set inet0 mtu 1430 up
 ip -n "$ns" -6 address add "2001:db8:25::$index/64" dev wan0 nodad
 ip -n "$ns" address add "203.0.113.$index/24" dev inet0
 ip -n "$ns" -6 route add 2001:db8:1500::/60 via fe80::5eff:fe00:131 dev wan0
 ip -n "$ns" -6 tunnel add aftr mode ipip6 local "2001:db8:25::$index" remote "2001:db8:1500:1::$suffix" dev wan0 encaplimit none
 ip -n "$ns" address add 192.0.0.1/32 peer 192.0.0.2/32 dev aftr
 ip -n "$ns" link set aftr mtu 1390 up
 ip netns exec "$ns" sysctl -qw net.ipv4.ip_forward=1
 ip netns exec "$ns" nft -f - <<NFT
table ip aftr_nat {
 chain postrouting {
  type nat hook postrouting priority srcnat; policy accept;
  oifname "inet0" ip saddr 192.0.0.2 snat to 203.0.113.$index
 }
}
NFT
 ping -6 -I rd15-wan -c 1 -W 2 "2001:db8:25::$index" >/dev/null
 ip netns exec "$ns" ping -c 1 -W 2 203.0.113.80 >/dev/null
 index=$((index+1)); suffix=$((suffix+1))
done
cat > "$work/kea.json" <<EOF
{"Dhcp6": {
 "interfaces-config":{"interfaces":["rd15-wan"]},
 "lease-database":{"type":"memfile","persist":false,"name":"$work/leases.csv"},
 "data-directory":"$work", "renew-timer":60, "rebind-timer":120,
 "preferred-lifetime":300, "valid-lifetime":600,
 "option-data":[{"name":"dns-servers","data":"2001:db8:ffff::80"},
                {"name":"domain-search","data":"upstream.test"}],
 "subnet6":[{"id":15,"subnet":"2001:db8:25::/64","interface":"rd15-wan","pools":[],
 "pd-pools":[{"prefix":"2001:db8:1500::","prefix-len":60,"delegated-len":60}]}]
}}
EOF
"$kea" -t "$work/kea.json"
systemd-run --unit=rd15-integrated-kea --property=Type=exec \
 --setenv="LD_LIBRARY_PATH=$LD_LIBRARY_PATH" --setenv="KEA_PIDFILE_DIR=$work" \
 --setenv="KEA_LOCKFILE_DIR=$work" "$kea" -d -c "$work/kea.json"
cat > "$work/radvd.conf" <<'RA'
interface rd15-wan {
 AdvSendAdvert on; MinRtrAdvInterval 3; MaxRtrAdvInterval 10;
 AdvDefaultLifetime 45; AdvLinkMTU 1430;
 prefix 2001:db8:25::/64 {
  AdvOnLink on; AdvAutonomous on;
  AdvValidLifetime 7200; AdvPreferredLifetime 3600;
 };
};
RA
radvd --configtest -C "$work/radvd.conf"
systemd-run --unit=rd15-integrated-ra --property=Type=exec radvd -n -m stderr -C "$work/radvd.conf" -p "$work/radvd.pid"
cat > "$work/bgpd.conf" <<'FRR'
frr defaults traditional
hostname independent
router bgp 64512
 bgp router-id 198.18.31.10
 no bgp ebgp-requires-policy
 no bgp network import-check
 neighbor 198.18.31.2 remote-as 64500
 neighbor 198.18.31.3 remote-as 64500
 address-family ipv4 unicast
  network 10.250.0.0/24
  network 10.251.0.0/24
 exit-address-family
FRR
touch "$work/vtysh.conf"
systemd-run --unit=rd15-integrated-frr --property=Type=exec /usr/lib/frr/bgpd -Z -n -S -f "$work/bgpd.conf" -i "$work/bgpd.pid" --vty_socket "$work/vty" -P 0 --log stdout
systemd-run --unit=rd15-integrated-dns --property=Type=exec ip netns exec rd15-inet-server \
 dnsmasq --keep-in-foreground --no-resolv --no-hosts --bind-interfaces --interface=eth0 \
 --address=/service.upstream.test/203.0.113.80 --address=/service.upstream.test/2001:db8:ffff::80 \
 --pid-file="$work/dns.pid"
ip netns add rd15-ha-client
ip link add rd15-client type veth peer name eth0 netns rd15-ha-client
ip link set rd15-client master rd15-lan
ip link set rd15-client mtu 1430 up
ip -n rd15-ha-client link set lo up
ip -n rd15-ha-client link set eth0 address 02:15:30:00:00:50 mtu 1430 up
for unit in kea ra frr dns; do
 systemctl is-active --quiet rd15-integrated-$unit
done
ss -unl | grep ':547 '
echo 'Prepared independent HGW/PD, four routed AFTRs, Internet DNS and BGP; product not started'
