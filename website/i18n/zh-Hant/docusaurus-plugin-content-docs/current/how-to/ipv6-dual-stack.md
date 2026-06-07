# IPv6 雙協定堆疊 BGP 與 VIP

![dual-stack BGPPeer address family、IPv4 與 IPv6 VirtualAddress、DNS A/AAAA record、BGP/VRRP firewall opening 的流程](/img/diagrams/how-to-ipv6-dual-stack.png)

routerd 可從單一 `BGPRouter` 同時處理 `routerd-bgp` GoBGP 的 IPv4 unicast 與 IPv6 unicast。
`spec.importPolicy.allowedPrefixes`、`spec.exportPolicy.allowedPrefixes` 及
`redistribute.*.allowedPrefixes` 中 IPv4/IPv6 混合指定皆可，
routerd 會直接映射至對應型別的 GoBGP address family。

`BGPPeer.spec.peers` 中可直接指定 IPv6 的對等位址：

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

API VIP 與服務 VIP 以並列資源的方式宣告，分別在 `VirtualAddress` 中指定
`spec.family: ipv4` 與 `spec.family: ipv6`。
IPv4 VIP 依慣例以 keepalived 主機前綴產生；IPv6 VIP 以 keepalived VRRPv3 的
`family inet6` 產生。在 FreeBSD 上，兩者皆作為父介面的 CARP alias 處理。

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

當 IPv4/IPv6 的 VIP 資源擁有相同的 `hostname`，且有對應的 `DNSResolver` 與 `DNSZone` 時，
A 記錄與 AAAA 記錄會自動新增。防火牆產生時，也會開放 routerd 受管資源所需的 BGP TCP/179，
以及 IPv4/IPv6 VRRP protocol 112 的控制流量。

`examples/dualstack-bgp.yaml` 為純 BGP 的配置範例；
`examples/k8s-api-vip-dualstack.yaml` 為 Kubernetes API VIP 的配置範例。
