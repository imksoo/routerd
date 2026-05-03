---
title: ルーターラボ
---

# ルーターラボ

routerd の実装検証は、現在 pve05、pve06、pve07 上の VM を中心に行っています。
このページは、現在のラボで確認済みの範囲をまとめます。

## 5 台ラボ

| ホスト | OS | 状態 |
| --- | --- | --- |
| router01 | FreeBSD | `routerd-dhcpv6-client` で DHCPv6-PD Bound |
| router02 | NixOS | 宣言的 systemd ユニットで DHCPv6-PD Bound |
| router03 | Ubuntu | DHCPv6-PD Bound、PPPoE 試験に使用 |
| router04 | FreeBSD | DHCPv6-PD Bound、WireGuard/VXLAN 検証 |
| router05 | Ubuntu | routerd コントローラーチェーン、DS-Lite 実適用、dnsmasq 検証 |

5 台間の IPv6 疎通と、DHCPv6-PD の重複なしを確認済みです。

## DS-Lite 実適用

router05 では、条件付き DNS 転送で `gw.transix.jp` を HGW 側の RDNSS へ問い合わせ、AFTR IPv6 を解決しました。
その後、`ip6tnl` トンネル、IPv4 既定経路、NAT44 を実適用し、IPv4 HTTP 通信を確認しています。

NGN HGW の DHCPv6 情報要求では AFTR option が返らないため、この環境では `DSLiteTunnel.spec.aftrFQDN` による静的フォールバックが正しい経路です。

## Tier S 検証

router02 と router04 で WireGuard の相互接続を確認しました。
さらに VXLAN over WireGuard の双方向疎通を確認しています。
router02 では VRF の基本操作も確認しました。

IPsec は cloud VPN 接続向けの設定生成と単体試験が中心で、実クラウド接続は別検証です。

## 使わない経路

過去に pve01 から pve04 の vmbr0 VLAN 1901 経路で、DHCPv6-PD が不安定に見える検証がありました。
現在の実装判断は、その経路を根拠にしません。
