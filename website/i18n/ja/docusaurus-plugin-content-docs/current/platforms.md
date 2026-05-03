# 対応プラットフォーム

routerd の第一対象は Ubuntu Server です。
NixOS と FreeBSD は実機検証済みの範囲がありますが、すべての生成器と適用処理が Ubuntu と同じではありません。
利用者向け文書では、実装済みの範囲と土台だけの範囲を分けて説明します。

## Ubuntu Server

Ubuntu は現在の主対象です。
標準のソースインストール先は `/usr/local` 配下です。

主に次を使います。

- systemd ユニット
- `/run/routerd` と `/var/lib/routerd`
- dnsmasq
- nftables
- iproute2
- pppd と rp-pppoe
- WireGuard

`routerd-dhcpv6-client`、`routerd-dhcpv4-client`、`routerd-pppoe-client`、`routerd-healthcheck` は Ubuntu 上で systemd サービスとして動かします。

## NixOS

NixOS は第二対象です。
Phase 1.7 では router02 で DHCPv6-PD デーモンを宣言的な NixOS 設定へ移しました。
一時的な systemd ユニットではなく、`/etc/nixos/routerd-generated.nix` による管理です。

確認済みの範囲:

- `routerd-dhcpv6-client` の systemd ユニット生成
- `nixos-rebuild test` と `nixos-rebuild switch`
- DHCPv6-PD の Bound 維持
- WireGuard と VXLAN の検証
- VRF の一部検証

未完了の範囲:

- NixOS らしい全リソース生成の完全対応
- pf 相当ではなく nftables 前提の細部整理
- すべての LAN サービスの宣言的統合

## FreeBSD

FreeBSD も第二対象です。
router01 と router04 では `routerd-dhcpv6-client` を daemon(8) で動かし、DHCPv6-PD の Bound 維持を確認しています。

確認済みの範囲:

- DHCPv6-PD デーモンの起動とリース永続化
- FreeBSD と NixOS 間の WireGuard 検証
- VXLAN over WireGuard の検証
- FreeBSD 向け PPPoE 実装の土台

未完了の範囲:

- pf 生成器
- FreeBSD らしいネットワーク設定生成の完全対応
- すべての systemd 前提機能の rc.d 置き換え

## ラボの扱い

現在の健全な実装ラボは pve05、pve06、pve07 上の VM です。
過去に pve01 から pve04 の vmbr0 VLAN 1901 経路で DHCPv6-PD が不安定に見えた時期がありました。
その経路は設計判断の根拠にしません。

現在の 5 台ラボは次の状態です。

| ホスト | OS | 役割 |
| --- | --- | --- |
| router01 | FreeBSD | `routerd-dhcpv6-client` で DHCPv6-PD |
| router02 | NixOS | 宣言的 systemd ユニットで DHCPv6-PD |
| router03 | Ubuntu | DHCPv6-PD と PPPoE 試験 |
| router04 | FreeBSD | DHCPv6-PD と複数 OS 横断検証 |
| router05 | Ubuntu | routerd コントローラーチェーン、DS-Lite 実適用、dnsmasq 検証 |

## OS 判定の実装方針

新しい OS 差分を追加するときは、`runtime.GOOS` を直接読むのではなく `pkg/platform` を使います。
Linux 専用機能は `platform.Features` や build tag で分けます。
対応していない OS で実行時に突然失敗するより、検証や計画の段階で明示することを優先します。
