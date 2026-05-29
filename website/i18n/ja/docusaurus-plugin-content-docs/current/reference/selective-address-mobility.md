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

`AddressMobilityDomain` は mobile address が属する IPv4 prefix を定義します。
`mode` は `selective-address` のみです。

`RemoteAddressClaim` は 1 つの mobile `/32`、owner side、capture mechanism、
overlay peer への route delivery を宣言します。

`CloudProviderProfile` は provider capability と external-command auth を
記述するだけです。MVP では cloud API call を行いません。

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
tunnel interface へ転送する intent として表します。Live capture と `/32`
forwarding dataplane は後続 step です。

## Provider Capabilities

| Provider | MVP capability descriptor |
| --- | --- |
| Azure | NIC secondary IP と router NIC の IP forwarding。 |
| AWS | ENI secondary private IPv4 と source/destination check disabled。 |
| OCI | VNIC private IP object と source/destination check disabled。 |
| GCP | Alias IP または route capability。provider profile の capability で gate します。 |

## Firewall And NAT Composition

Selective Address Mobility は通常の switching/forwarding plane にあります。
`nat`、`preserveSource`、firewall、zone field は持ちません。Address
transparency は intrinsic です。

Mobile address に firewall や NAT を適用する場合は、既存の `FirewallZone`、
`FirewallRule`、`NAT44Rule` resource で literal `/32` address を参照します。
MVP ではこれらの Kind から `RemoteAddressClaim` への cross-kind reference は
ありません。SAM-forwarded traffic は、他の forwarded traffic と同じく既存の
firewall/conntrack path を通ります。

## Out Of Scope

MVP は full L2 extension、EVPN、BUM forwarding、broadcast/multicast
extension、automatic cloud API mutation、dynamic patch/replace、netlink
programming、proxy-ARP programming、`/32` route forwarding、`ip_forward`
変更を実装しません。
