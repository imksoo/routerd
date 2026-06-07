---
title: CloudEdge SAM transport layering
---

# CloudEdge SAM transport layering

CloudEdge SAM では、mobile `/32` を WireGuard `AllowedIPs` に入れません。
WireGuard の `AllowedIPs` は cryptokey routing であり、BGP best path、route
reflector、ECMP によって `/32` の owner が動く SAM delivery plane とは役割が違います。

## 推奨 layer

信頼済み underlay、または別途暗号化済み underlay では IPIP を優先します。

```text
physical underlay
  IPIP tunnel
    SAM overlay packets
```

暗号化が必要な場合は、WireGuard を endpoint 専用 transport として置き、その上に
IPIP を載せます。

```text
physical underlay
  WireGuard endpoint transport
    IPIP tunnel
      SAM overlay packets
```

この構成では、WireGuard peer の `AllowedIPs` は `10.252.0.2/32` のような
router-to-router endpoint prefix だけにします。SAM の mobile `/32` は BGP、kernel FIB、
SAM resource が扱います。

## SAMTransportProfile

現在の CloudEdge example では `SAMTransportProfile` を使って、peer ごとの
`TunnelInterface`、endpoint `/32` `IPv4Route`、`BGPPeer` を生成します。

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

`spec.selfNodeRef` は各 router の安定 ID です。複数 peer を持つ profile では、同じ
`spec.topologyNodeRefs` を全 router に配ります。routerd はこの共有 node list を sort
し、unordered node pair ごとに `innerPrefix` から deterministic な `/31` を割り当てます。

## cleanup

Profile は self node ごとに `DynamicConfigPart` を 1 つ書きます。peer の削除はその part
を新しい生成 resource set に置き換えます。profile を削除すると空の active part になり、
生成 tunnel、BGP peer、endpoint route が effective config から消えます。

cleanup は現在の lifecycle GC path が担当します。desired set は dynamic SAM 生成後の
effective view なので、profile が存在する限り生成 transport resource は保持され、
profile output から消えた後にだけ GC 対象になります。
