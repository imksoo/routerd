---
title: CloudEdge SAM transport layering
---

# CloudEdge SAM transport layering

CloudEdge SAM 不應把 mobile `/32` 放進 WireGuard `AllowedIPs`。WireGuard 的
`AllowedIPs` 是 cryptokey routing；而 SAM delivery plane 中 `/32` owner 會隨著 BGP
best path、route reflector 與 ECMP 變化，兩者職責不同。

![CloudEdge SAM transport 圖：SAMTransportProfile 產生 IPIP delivery，可運行在 endpoint-only WireGuard underlay 上，mobile /32 由 BGP 與 kernel FIB 處理](/img/diagrams/cloudedge-sam-ipip.png)

## 推薦 layer

可信 underlay，或已經透過其他方式加密的 underlay，優先使用 IPIP：

```text
physical underlay
  IPIP tunnel
    SAM overlay packets
```

需要加密時，將 WireGuard 作為 endpoint-only transport，再在其上承載 IPIP：

```text
physical underlay
  WireGuard endpoint transport
    IPIP tunnel
      SAM overlay packets
```

此時 WireGuard peer 的 `AllowedIPs` 只包含 `10.252.0.2/32` 這類 router-to-router
endpoint prefix。SAM mobile `/32` 由 BGP、kernel FIB 與 SAM resource 處理。

## SAMTransportProfile

目前 CloudEdge example 使用 `SAMTransportProfile` 產生每個 peer 的
`TunnelInterface`、endpoint `/32` `IPv4Route` 與 `BGPPeer`。

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

`spec.selfNodeRef` 是每台 router 的穩定 ID。`SAMNodeSet` 是共享的 identity、topology
和 endpoint source；routerd 會排序該 node set，並為每個 unordered node pair 從
`innerPrefix` 分配 deterministic `/31`。`SAMTransportProfile` 不接受直接的 peer 或
topology list；`nodeRefs` 只選擇需要建立 transport 的相鄰 node。省略 `nodeRefs` 會選擇
所有帶 `samEndpoint` 的非 self node。

## cleanup

profile 會為每個 self node 寫入一個 `DynamicConfigPart`。刪除 peer 時，該 part 被新的產生
resource set 取代；刪除 profile 時，則變成空的 active part，產生的 tunnel、BGP peer 與
endpoint route 會從 effective config 消失。

cleanup 由目前 lifecycle GC path 負責。desired set 使用 dynamic SAM 產生後的 effective
view，因此 profile 存在時產生的 transport resource 會被保留，只有從 profile output 消失後
才成為 GC 對象。
