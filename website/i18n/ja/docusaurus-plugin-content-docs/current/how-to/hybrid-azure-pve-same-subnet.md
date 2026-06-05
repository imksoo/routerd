---
title: Azure と PVE の same-subnet SAM スモークテスト
---

# Azure と PVE の same-subnet SAM スモークテスト

このガイドは、Azure の routerd node とオンプレミス Proxmox VE の routerd node
で、Selective Address Mobility (SAM) により選択した `/32` address を交換する、
検証済みの運用形をまとめたものです。resource semantics は
[選択的アドレス移動性のリファレンス](../reference/selective-address-mobility)
を参照してください。

## Azure 側

- Azure NIC secondary IP は Azure 側に残します。この provider-side object が
  on-prem `/32` 宛の packet を capture します。
- Ubuntu guest OS には captured `/32` を持たせないでください。cloud-init や
  netplan が secondary NIC IP を自動付与することがあります。その設定は抑止する
  か削除します。Claim が `configureOSAddress: false` の場合、routerd は
  reconcile 時に、その特定 address を local interface から de-assign し、
  address が存在しない状態を維持します。
- Azure NIC と Linux の両方で IP forwarding を有効化します
  (`net.ipv4.ip_forward=1`)。

## オンプレミス PVE 側

- local same-subnet host が見える LAN/bridge interface で `proxy-arp` capture
  を使います。
- Linux forwarding を有効化します。SAM では routerd が通常の sysctl path で
  `ip_forward` と `proxy_arp` を有効化します。
- capture interface と WireGuard tunnel の間で、captured `/32` の forwarding
  を firewall policy で許可します。SAM は firewall rule や NAT rule を追加しま
  せん。
- cloud guest image では、provider fabric が packet を drop していると判断する
  前に、host firewall の既定値も確認してください。ルーターは WireGuard の UDP
  listen port を受け付け、capture interface と `wg-hybrid` の間の forwarding を
  許可する必要があります。`routerctl doctor hybrid` は terminal iptables
  drop/reject pattern と、SAM MSS clamp rule の不足を警告します。

## トンネルと routing

- WireGuard は on-prem から Azure public IP へ dial する形にします。
- on-prem peer には `persistentKeepalive` を設定し、NAT と cloud edge state を
  維持します。
- 最初の smoke は UDR なしで実施します。後で UDR fallback を追加する場合は、
  Azure が captured `/32` を delivery 元の router へ戻す same-subnet loop に注
  意してください。
- SAM delivery は各 claim を tunnel interface への `/32` route に lower しま
  す。default route は変更しません。

## 検証

実行:

```sh
routerctl doctor hybrid
```

`provider-secondary-ip` + `configureOSAddress: false` では、captured `/32` が
local `ip addr` に存在しないこと、delivery route が tunnel を向くこと、
`ip_forward=1` であることを確認します。`proxy-arp` では、`proxy_arp=1`、
proxy neighbor、tunnel への delivery route、`ip_forward=1` を確認します。

低 MTU の overlay では、`doctor hybrid` が SAM MSS clamp を報告し、
`nft list table inet routerd_mss` に、選択した `/32` path の
capture-to-tunnel と tunnel-to-capture の両方の rule が含まれていることを確認します。
