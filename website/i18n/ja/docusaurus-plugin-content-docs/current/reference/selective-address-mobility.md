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
tunnel interface へ転送する intent として表します。Linux dataplane では、この
delivery は claim address そのものの managed `IPv4Route` に lowering されます
（例: `10.0.0.9/32 dev wg-hybrid`）。SAM claim が default route に lowering
されることはありません。

Linux の `proxy-arp` capture では、routerd は通常の sysctl controller で
`net.ipv4.conf.<capture-interface>.proxy_arp=1` を有効化し、
`ip neigh add proxy <address> dev <capture-interface>` 相当の proxy neighbor
entry を netlink で追加し、通常の sysctl controller で
`net.ipv4.ip_forward=1` を有効化します。

`provider-secondary-ip` では provider fabric が address capture を担当します。
`configureOSAddress: false` の場合、routerd は mobile address を local OS
address として設定しません。routerd は IPv4 forwarding と overlay への `/32`
delivery route だけを管理します。

FreeBSD など non-Linux host では live SAM capture は未対応です。Controller は
host を変更せず、`SAM capture not implemented on this OS` と報告します。

Linux live dataplane は実装済みですが、Azure + PVE lab の real kernel ではまだ
検証されていません。Production 利用前に lab smoke validation が必要です。

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
extension、automatic cloud API mutation、dynamic patch/replace、
provider-side address assignment、自動 `rp_filter` 変更を実装しません。
