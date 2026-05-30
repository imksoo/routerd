---
title: Azure と PVE の same-subnet SAM smoke
---

# Azure と PVE の same-subnet SAM smoke

この guide は、Azure の routerd node と on-prem Proxmox VE の routerd node
で、Selective Address Mobility (SAM) により selected `/32` address を交換する
validated operational shape をまとめたものです。Resource semantics は
[Selective Address Mobility reference](../reference/selective-address-mobility.md)
を参照してください。

## Azure 側

- Azure NIC secondary IP は Azure 側に残します。この provider-side object が
  on-prem `/32` 宛 packet を capture します。
- Ubuntu guest OS には captured `/32` を持たせないでください。cloud-init や
  netplan が secondary NIC IP を自動付与することがあります。その設定は抑止する
  か削除します。Claim が `configureOSAddress: false` の場合、routerd は
  reconcile 時に、その特定 address を local interface から de-assign して
  absence を維持します。
- Azure NIC と Linux の両方で IP forwarding を有効化します
  (`net.ipv4.ip_forward=1`)。

## On-Prem PVE 側

- Local same-subnet host が見える LAN/bridge interface で `proxy-arp` capture
  を使います。
- Linux forwarding を有効化します。SAM では routerd が通常の sysctl path で
  `ip_forward` と `proxy_arp` を有効化します。
- Capture interface と WireGuard tunnel の間で、captured `/32` の forwarding
  を firewall policy で許可します。SAM は firewall rule や NAT rule を追加しま
  せん。

## Tunnel And Routing

- WireGuard は on-prem から Azure public IP へ dial する形にします。
- On-prem peer には `persistentKeepalive` を設定し、NAT と cloud edge state を
  維持します。
- Initial smoke は UDR なしで実施します。後で UDR fallback を追加する場合は、
  Azure が captured `/32` を delivery 元の router へ戻す same-subnet loop に注
  意してください。
- SAM delivery は各 claim を tunnel interface への `/32` route に lower しま
  す。Default route は変更しません。

## Verification

```sh
routerctl doctor hybrid
```

`provider-secondary-ip` + `configureOSAddress: false` では、captured `/32` が
local `ip addr` に存在しないこと、delivery route が tunnel を向くこと、
`ip_forward=1` であることを確認します。`proxy-arp` では、`proxy_arp=1`、
proxy neighbor、tunnel への delivery route、`ip_forward=1` を確認します。
