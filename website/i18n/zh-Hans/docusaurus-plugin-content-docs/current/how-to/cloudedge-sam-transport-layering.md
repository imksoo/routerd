---
title: CloudEdge SAM transport layering
---

# CloudEdge SAM transport layering

CloudEdge SAM 不应把 mobile `/32` 放进 WireGuard `AllowedIPs`。WireGuard 的
`AllowedIPs` 是 cryptokey routing；而 SAM delivery plane 中 `/32` owner 会随着 BGP
best path、route reflector 与 ECMP 变化，两者职责不同。

![CloudEdge SAM transport 图：SAMTransportProfile 生成 IPIP delivery，可运行在 endpoint-only WireGuard underlay 上，mobile /32 由 BGP 与 kernel FIB 处理](/img/diagrams/cloudedge-sam-ipip.png)

## 推荐 layer

可信 underlay，或已经通过其他方式加密的 underlay，优先使用 IPIP：

```text
physical underlay
  IPIP tunnel
    SAM overlay packets
```

需要加密时，将 WireGuard 作为 endpoint-only transport，再在其上承载 IPIP：

```text
physical underlay
  WireGuard endpoint transport
    IPIP tunnel
      SAM overlay packets
```

此时 WireGuard peer 的 `AllowedIPs` 只包含 `10.252.0.2/32` 这类 router-to-router
endpoint prefix。SAM mobile `/32` 由 BGP、kernel FIB 与 SAM resource 处理。

## SAMTransportProfile

当前 CloudEdge example 使用 `SAMTransportProfile` 生成每个 peer 的
`TunnelInterface`、endpoint `/32` `IPv4Route` 与 `BGPPeer`。

```yaml
apiVersion: mobility.routerd.net/v1alpha1
kind: SAMNodeSet
metadata:
  name: cloudedge-nodes
spec:
  nodes:
    - { nodeRef: onprem-router, site: onprem, role: onprem, routeReflector: true, samEndpoint: 10.252.0.1 }
    - { nodeRef: aws-router-a, site: aws, role: cloud, samEndpoint: 10.252.0.2 }
    - { nodeRef: azure-router, site: azure, role: cloud, samEndpoint: 10.252.0.3 }
---
apiVersion: mobility.routerd.net/v1alpha1
kind: SAMTransportProfile
metadata:
  name: cloudedge-transport
spec:
  selfNodeRef: onprem-router
  mode: ipip
  encryption: wireguard
  innerPrefix: 10.255.0.0/24
  underlayInterface: wg-hybrid
  localEndpointFrom:
    resource: Interface/wg-hybrid
    field: primaryIPv4
  bgp:
    routerRef: BGPRouter/mobility
    peerASN: 64512
    timersPreset: fast
  peersFrom:
    - resource: SAMNodeSet/cloudedge-nodes
      nodeRefs: [aws-router-a]
```

`spec.selfNodeRef` 是每台 router 的稳定 ID。`SAMNodeSet` 是共享的 identity、topology
和 endpoint source；routerd 会排序该 node set，并为每个 unordered node pair 从
`innerPrefix` 分配 deterministic `/31`。`SAMTransportProfile` 不接受直接的 peer 或
topology list；`nodeRefs` 只选择需要建立 transport 的相邻 node。省略 `nodeRefs` 会选择
所有带 `samEndpoint` 的非 self node。

## cleanup

profile 会为每个 self node 写入一个 `DynamicConfigPart`。删除 peer 时，该 part 被新的生成
resource set 替换；删除 profile 时，则变成空的 active part，生成的 tunnel、BGP peer 与
endpoint route 会从 effective config 消失。

cleanup 由当前 lifecycle GC path 负责。desired set 使用 dynamic SAM 生成后的 effective
view，因此 profile 存在时生成的 transport resource 会被保留，只有从 profile output 消失后
才成为 GC 对象。
