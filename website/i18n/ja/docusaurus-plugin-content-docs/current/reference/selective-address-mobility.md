---
title: Selective Address Mobility
slug: /reference/selective-address-mobility
---

# Selective Address Mobility

これは full L2 extension ではありません。routerd CloudEdge は public
cloud に Ethernet segment を延伸しません。Public cloud fabric は運用者が
制御する broadcast domain を提供せず、route と address ownership のモデルも
provider ごとに異なります。

Selective Address Mobility は、選択した IPv4 `/32` address だけを片側で
capture し、routerd-to-routerd overlay 経由で owner 側へ届ける抽象です。
TCP/IP の source/destination address は保持されます。Firewall と NAT は
別の routerd layer であり、mobility resource の field ではありません。

## Resource Model

CloudEdge Mobility control plane では、operator が書く mobility intent は
`MobilityPool` だけです。論理 IPv4 pool、読み取る EventGroup、member node と
site、lease/capture policy を宣言します。

```yaml
apiVersion: mobility.routerd.net/v1alpha1
kind: MobilityPool
metadata: { name: lab-same-subnet }
spec:
  prefix: 10.0.0.0/24
  groupRef: cloudedge
  members:
    - nodeRef: onprem-router
      site: onprem
      role: onprem
      capture:
        type: proxy-arp
        interface: lan
      deliveryTo:
        - nodeRef: cloud-router
          peerRef: cloud-main
          mode: route
          tunnelInterface: wg-hybrid
      delivery:
        peerRef: cloud-main
        mode: route
        tunnelInterface: wg-hybrid
    - nodeRef: cloud-router
      site: azure
      role: cloud
      capture:
        type: provider-secondary-ip
        providerRef: azure-lab
        providerMode: nic-secondary-ip
        nicRef: /subscriptions/.../networkInterfaces/routerd-nic
        configureOSAddress: false
        target:
          region: japaneast
          ipConfigName: mobility-capture
      placement:
        group: azure-edge
        priority: 10
      maintenance:
        drain: false
      delivery:
        peerRef: onprem-main
        mode: route
        tunnelInterface: wg-hybrid
  leasePolicy:
    ttl: 5m
    holdDuration: 30s
  capturePolicy:
    mode: all-non-owner-sites
    deprovisionHoldDuration: 30s
```

routerd は `routerd.client.ipv4.observed` federation event を read-only な
`AddressLease` state に射影します。Lease は config Kind ではなく、手で書くもの
ではありません。`routerctl mobility leases` で確認します。

同一 provider の cloud router maintenance では、`members[].placement.group`
内の non-drained member から `priority`、次に `nodeRef` の順で active capture
member を選びます。`members[].maintenance.drain: true` にすると、その member は
active 選出から外れ、planner が generated capture claim と provider action plan
を次の候補へ移します。placement projection を deterministic に保つため、pool の
全 node に同じ `MobilityPool` config を配ります。

`AddressMobilityDomain` と `RemoteAddressClaim` は低位の SAM 表現です。既存の
hand-authored SAM config は引き続きサポートしますが、CloudEdge Mobility の本線では
mobility planner が `MobilityPool` と `AddressLease` state から導出し、
`DynamicConfigPart` として保存します。

`AddressMobilityDomain` は mobile address が属する IPv4 prefix を定義します。
`mode` は `selective-address` のみです。

`RemoteAddressClaim` は 1 つの mobile `/32`、owner side、capture mechanism、
overlay peer への route delivery を宣言します。

`AddressMobilityDomain.spec.peerRef` は domain-level の default/documentation
peer で、grouping metadata として扱います。MVP dataplane が実際の delivery
peer として使うのは `RemoteAddressClaim.spec.delivery.peerRef` であり、claim
ごとに必須です。

`CloudProviderProfile` は provider capability と external-command auth を
記述します。Mobility planner は provider API を直接呼びません。Cloud capture
では `assign-secondary-ip` や `ensure-forwarding-enabled` の dry-run
`ActionPlan` を生成し、別の provider-action executor 経路が
`ProviderActionPolicy` で明示的に許可された場合だけ import/execution します。

`OverlayPeer` は remote routerd peer と underlay を表します。`HybridRoute` は
通常の L3 remote-prefix routing のために残り、address mobility は prefix
route ではなく per-address forwarding の抽象です。

## Capture And Delivery

Supported capture types:

| Type | Meaning |
| --- | --- |
| `provider-secondary-ip` | Provider-owned secondary address object などで cloud fabric が `/32` を capture します。 |
| `proxy-arp` | Site router が selected address に対して local に ARP 応答します。 |

Reserved capture types rejected by MVP validation:

| Type | Status |
| --- | --- |
| `static-host-route` | Later dataplane design 用に予約されています。 |
| `garp` | Later dataplane design 用に予約されています。 |

Delivery mode は `route` です。Captured `/32` を named overlay peer と任意の
tunnel interface へ転送する intent として表します。Linux dataplane では、この
delivery は claim address そのものの managed `IPv4Route` に lowering されます
（例: `10.0.0.9/32 dev wg-hybrid`）。SAM claim が default route に lowering
されることはありません。

`members[].deliveryTo[]` は owner の `nodeRef`、次に `site`、最後に `role` の
順で delivery を選びます。`members[].delivery` は fallback です。これにより、
on-prem router が AWS、Azure、OCI へ異なる overlay peer で delivery する
4-site demo でも、全 node に同じ `MobilityPool` config を配れます。

`members[].capture.target` は生成される provider `ActionPlan.target` へコピーする
non-secret な provider target hint です。region、compartment ID、resource
group、NIC name、IP config name などの識別子だけを置き、credential、token、
private key は provider auth mechanism 側に置きます。

Linux の `proxy-arp` capture では、routerd は通常の sysctl controller で
`net.ipv4.conf.<capture-interface>.proxy_arp=1` を有効化し、
`ip neigh add proxy <address> dev <capture-interface>` 相当の proxy neighbor
entry を netlink で追加し、通常の sysctl controller で
`net.ipv4.ip_forward=1` を有効化します。

`provider-secondary-ip` では provider fabric が address capture を担当します。
`configureOSAddress: false` の場合、routerd は mobile address を local OS
address として設定しません。Linux では、cloud-init、netplan、guest agent など
がその address を戻した場合でも、その特定 address だけを local interface から
削除します。そのうえで IPv4 forwarding と overlay への `/32` delivery route を
管理します。Claim を削除しても routerd は address を戻しません。Guest OS への
address assignment は routerd が所有していないためです。

Status ではこれを `captureOSAddressAbsence` として報告します。
`enforced: true` は、routerd が captured address を local OS interface から
無くすことを継続的に enforcement している、という audit flag です。
`lastReconcileRemoved: true` は、直近の reconcile が実際にその address を削除した
ことを示します。Address がすでに無い steady state では通常 `false` です。

FreeBSD など non-Linux host では live SAM capture は未対応です。Controller は
host を変更せず、`SAM capture not implemented on this OS` と報告します。

Linux live dataplane は Azure + PVE same-subnet lab で smoke test 済みです。
ただし pre-release behavior なので、production 利用前に provider と firewall
policy の実構成で検証してください。

## Reverse Path Filtering

Strict reverse-path filtering は SAM forwarded traffic を drop する可能性が
あります。Mobile `/32` が直接接続 subnet に属して見える一方で、return path が
overlay になるためです。routerd は SAM のために `rp_filter` を黙って変更しませ
ん。これは interface policy として影響が大きいためです。

`routerctl doctor hybrid` は host check が有効な場合に
`net.ipv4.conf.<capture-or-tunnel-interface>.rp_filter` を読みます。値が strict
(`1`) の場合は warning を出し、対象 interface で loose mode (`2`) を検討する
remedy を表示します。

## Provider Capabilities

| Provider | MVP capability descriptor |
| --- | --- |
| Azure | NIC secondary IP と router NIC の IP forwarding。 |
| AWS | ENI secondary private IPv4 と source/destination check disabled。 |
| OCI | VNIC private IP object と source/destination check disabled。 |
| GCP | Alias IP または route capability。provider profile の capability で gate します。 |

Profile は宣言的な descriptor です。Mobility planner は provider `ActionPlan`
を生成できますが、address assignment や NIC flag 変更は provider-action
execution policy と executor plugin によって gate されます。Planner 自身は
provider state を変更しません。

## Same-Subnet Flow

`10.0.0.0/24` lab では、`10.0.0.7/32` が cloud VM の address、
`10.0.0.9/32` が on-prem/PVE VM の address です。目的は、cloud VM
`10.0.0.7` から on-prem VM `10.0.0.9` へ TCP connection を開始し、両方の VM
の default gateway は local のまま、NAT なしで通信させることです。

1. Cloud VM が `10.0.0.9` へ送信します。
2. Azure NIC secondary IP capture が `10.0.0.9/32` 宛の packet を cloud
   routerd node へ届けます。
3. Cloud routerd node は packet を `wg-hybrid` 経由で on-prem routerd peer
   へ delivery します。
4. On-prem 側は `10.0.0.9` の owner へ forwarding します。
5. Source/destination IP は元の endpoint address のままです。

Reverse path の `10.0.0.7/32` は on-prem 側の proxy-ARP で capture します。
PVE LAN host は `.7` へ on-prem routerd node 経由で到達し、on-prem routerd
node が overlay 経由で cloud routerd node へ delivery します。

Split example config は次の 2 つです。

- `examples/hybrid-azure-pve-same-subnet-cloud.yaml`: cloud routerd node に適用し、
  on-prem VM `10.0.0.9/32` の provider-secondary-IP claim を含みます。
- `examples/hybrid-azure-pve-same-subnet-onprem.yaml`: on-prem routerd node に適用し、
  cloud VM `10.0.0.7/32` の proxy-ARP claim を含みます。

## Firewall And NAT Composition

Selective Address Mobility は通常の switching/forwarding plane にあります。
`nat`、`preserveSource`、firewall、zone field は持ちません。Address
transparency は intrinsic です。

Mobile address に firewall や NAT を適用する場合は、既存の `FirewallZone`、
`FirewallRule`、`NAT44Rule` resource で literal `/32` address を参照します。
MVP ではこれらの Kind から `RemoteAddressClaim` への cross-kind reference は
ありません。SAM-forwarded traffic は、他の forwarded traffic と同じく既存の
firewall/conntrack path を通ります。

特に、delivery された `/32` traffic は capture interface と tunnel interface
の間で Linux firewall の `FORWARD` chain を通過します。Forwarding policy が
default-drop の router では、その captured address の forwarding path を明示的
に許可してください。SAM 自体は firewall rule を追加しません。

## クラウドノードでの overlay / federation アドレッシング

Event Federation の transport(`routerd-eventd` の listen address と各
`EventPeer.endpoint`)と、それが乗る WireGuard overlay(`OverlayPeer`、
`WireGuardInterface`/`WireGuardPeer`)は、全ノードで自分が end-to-end に制御
できるアドレス範囲を使ってください。クラウドインスタンスでは、provider が内部
利用のために予約している範囲から overlay / federation アドレスを取っては
**いけません**。

- `169.254.0.0/16`(RFC 3927 link-local)。クラウドのインスタンスメタデータ
  (IMDS)は `169.254.169.254` にあり、イメージによってはブロック全体を予約
  します。Oracle Cloud の Linux イメージは `169.254.0.0/16` 全体を
  `InstanceServices` chain にルーティングするため、`169.254.x` の overlay
  アドレス宛 federation SYN は loopback に引き込まれて RST されます(同じ
  アドレスへの ICMP は通るのに、です)。AWS/Azure も IMDS に
  `169.254.169.254` を使います。症状: lease は収束するのにノード間の
  `routerd-eventd` TCP が張れない。
- `100.64.0.0/10`(RFC 6598 CGNAT)。provider underlay の CGNAT や Tailscale
  (`100.x` の tailnet アドレス、MagicDNS)が使います。この範囲の overlay は
  Tailscale 参加や carrier NAT と衝突します。

overlay 専用に予約した RFC 1918 の範囲(例: `10.x.y.0/24`)を、WireGuard
interface/peer アドレス・`OverlayPeer` endpoint・`routerd-eventd` の listen /
`EventPeer` endpoint に使ってください。mobility pool の `/24`(captured
address)とも、上記のクラウド予約範囲とも分離します。これは全 provider
(AWS/Azure/OCI)に当てはまり、OCI が link-local 予約を最も厳格に強制するだけ
です。

## Out Of Scope

MVP は full L2 extension、EVPN、BUM forwarding、broadcast/multicast
extension、ungated な automatic cloud API mutation、dynamic patch/replace、
自動 `rp_filter` 変更を実装しません。
