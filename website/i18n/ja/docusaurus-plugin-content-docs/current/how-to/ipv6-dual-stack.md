# IPv6 dual-stack BGP と VIP

routerd は 1 つの `BGPRouter` から `routerd-bgp` GoBGP の IPv4 unicast と IPv6 unicast を
同時に扱えます。`spec.importPolicy.allowedPrefixes`、
`spec.exportPolicy.allowedPrefixes`、`redistribute.*.allowedPrefixes` は
IPv4/IPv6 混在のままでよく、routerd が型付き GoBGP address family に直接 map します。

`BGPPeer.spec.peers` には IPv6 peer address をそのまま指定します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: BGPPeer
metadata:
  name: k8s-speakers
spec:
  routerRef: BGPRouter/lan
  peerASN: 64513
  peers:
    - 192.168.70.21
    - fd00:70::21
```

API VIP や service VIP は `VirtualAddress` に `spec.family: ipv4` と
`spec.family: ipv6` を指定した並列 resource として宣言します。IPv4 VIP は
従来通り keepalived の host prefix として描画し、IPv6 VIP は keepalived
VRRPv3 の `family inet6` として描画します。FreeBSD では
どちらも parent interface の CARP alias として扱います。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: VirtualAddress
metadata:
  name: k8s-api-vip-v6
spec:
  interface: lan
  address: fd00:70::10/128
  family: ipv6
  hostname: k8s-api.cluster.example
  mode: vrrp
  vrrp:
    virtualRouterID: 71
    priority: 150
    peers:
      - fd00:70::3
```

IPv4/IPv6 の VIP resource が同じ `hostname` を持ち、対応する `DNSResolver` と
`DNSZone` がある場合、A record と AAAA record が自動で追加されます。firewall
renderer は routerd 管理 resource に必要な BGP TCP/179 と IPv4/IPv6 VRRP protocol
112 の control traffic も開きます。

`examples/dualstack-bgp.yaml` は BGP 単体の形、
`examples/k8s-api-vip-dualstack.yaml` は Kubernetes API VIP の構成例です。
