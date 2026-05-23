# IPv6 双协议栈 BGP 与 VIP

routerd 可从单一 `BGPRouter` 同时处理 `routerd-bgp` GoBGP 的 IPv4 unicast 与 IPv6 unicast。
`spec.importPolicy.allowedPrefixes`、`spec.exportPolicy.allowedPrefixes` 及
`redistribute.*.allowedPrefixes` 中 IPv4/IPv6 混合指定皆可，
routerd 会直接映射至对应类型的 GoBGP address family。

`BGPPeer.spec.peers` 中可直接指定 IPv6 的对等地址：

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

API VIP 与服务 VIP 以并列资源的方式声明，分别在 `VirtualAddress` 中指定
`spec.family: ipv4` 与 `spec.family: ipv6`。
IPv4 VIP 依惯例以 keepalived 主机前缀生成；IPv6 VIP 以 keepalived VRRPv3 的
`family inet6` 生成。在 FreeBSD 上，两者皆作为父接口的 CARP alias 处理。

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

当 IPv4/IPv6 的 VIP 资源拥有相同的 `hostname`，且有对应的 `DNSResolver` 与 `DNSZone` 时，
A 记录与 AAAA 记录会自动新增。防火墙生成时，也会开放 routerd 受管资源所需的 BGP TCP/179，
以及 IPv4/IPv6 VRRP protocol 112 的控制流量。

`examples/dualstack-bgp.yaml` 为纯 BGP 的配置示例；
`examples/k8s-api-vip-dualstack.yaml` 为 Kubernetes API VIP 的配置示例。
