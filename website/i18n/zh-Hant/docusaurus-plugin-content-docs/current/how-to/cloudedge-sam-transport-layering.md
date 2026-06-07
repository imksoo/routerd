---
title: CloudEdge SAM transport layering
---

# CloudEdge SAM transport layering

CloudEdge SAM 不應把 mobile `/32` 放進 WireGuard `AllowedIPs`。WireGuard 的
`AllowedIPs` 是 cryptokey routing；而 SAM delivery plane 中 `/32` owner 會隨著 BGP
best path、route reflector 與 ECMP 變化，兩者職責不同。

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
kind: SAMTransportProfile
metadata:
  name: cloudedge-transport
spec:
  selfNodeRef: onprem-router
  mode: ipip
  encryption: wireguard
  innerPrefix: 10.255.0.0/24
  topologyNodeRefs:
    - onprem-router
    - aws-router-a
    - azure-router
  underlayInterface: wg-hybrid
  localEndpointFrom:
    resource: Interface/wg-hybrid
    field: primaryIPv4
  bgp:
    routerRef: BGPRouter/mobility
    peerASN: 64512
    timersPreset: fast
  peers:
    - nodeRef: aws-router-a
      remoteEndpoint: 10.252.0.2
```

`spec.selfNodeRef` 是每台 router 的穩定 ID。一個 profile 有多個 peer 時，所有 router
必須使用相同的 `spec.topologyNodeRefs`。routerd 會排序這份共享 node list，並為每個
unordered node pair 從 `innerPrefix` 分配 deterministic `/31`。

## cleanup

profile 會為每個 self node 寫入一個 `DynamicConfigPart`。刪除 peer 時，該 part 被新的產生
resource set 取代；刪除 profile 時，則變成空的 active part，產生的 tunnel、BGP peer 與
endpoint route 會從 effective config 消失。

cleanup 由目前 lifecycle GC path 負責。desired set 使用 dynamic SAM 產生後的 effective
view，因此 profile 存在時產生的 transport resource 會被保留，只有從 profile output 消失後
才成為 GC 對象。
