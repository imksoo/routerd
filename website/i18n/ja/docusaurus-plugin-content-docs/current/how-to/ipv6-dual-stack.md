# IPv6 デュアルスタック BGP と VIP

![dual-stack BGPPeer address family、IPv4 と IPv6 VirtualAddress、DNS A/AAAA record、BGP/VRRP firewall opening の流れ](/img/diagrams/how-to-ipv6-dual-stack.png)

routerd は、1 つの `BGPRouter` から `routerd-bgp` GoBGP の IPv4 unicast と IPv6 unicast を
同時に扱えます。`spec.importPolicy.allowedPrefixes`、
`spec.exportPolicy.allowedPrefixes`、`redistribute.*.allowedPrefixes` は
IPv4/IPv6 が混在したままでかまいません。routerd が、型付きの GoBGP address family へ直接マッピングします。

`BGPPeer.spec.peers` には、IPv6 のピアアドレスをそのまま指定します。

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

API VIP やサービス VIP は、`VirtualAddress` に `spec.family: ipv4` と
`spec.family: ipv6` を指定した並列のリソースとして宣言します。IPv4 VIP は
従来どおり keepalived のホストプレフィックスとして生成し、IPv6 VIP は keepalived
VRRPv3 の `family inet6` として生成します。FreeBSD では、
どちらも親インターフェースの CARP エイリアスとして扱います。

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

IPv4/IPv6 の VIP リソースが同じ `hostname` を持ち、対応する `DNSResolver` と
`DNSZone` がある場合は、A レコードと AAAA レコードが自動で追加されます。ファイアウォールの
生成では、routerd 管理リソースに必要な BGP の TCP/179 と、IPv4/IPv6 VRRP の protocol
112 による制御トラフィックも開きます。

`examples/dualstack-bgp.yaml` は BGP 単体の構成例、
`examples/k8s-api-vip-dualstack.yaml` は Kubernetes API VIP の構成例です。
