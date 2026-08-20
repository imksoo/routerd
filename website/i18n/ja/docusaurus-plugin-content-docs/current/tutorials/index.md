---
title: チュートリアル
slug: /tutorials
---

# チュートリアル

![routerd を安全に学ぶ順番。Ubuntu VM、設定、検証、dry-run、サービス、機能別チュートリアル](/img/diagrams/tutorial-index.png)

最初は、**隔離した Ubuntu Server VM** で練習します。VM の画面を直接開ける
コンソールを用意し、変更する NIC とは別の管理経路を残してください。
普段使っている家・学校・職場の回線を、最初の練習台にしてはいけません。

## おすすめの順番

| 順番 | すること | ページ |
| --- | --- | --- |
| 0 | WAN、LAN、DHCP、NAT の短い説明を読む | [ネットワーク基礎](./network-basics.md) |
| 1 | Ubuntu Server VM に routerd を入れる | [インストール](./install.md) |
| 2 | 設定を検証し、ネットワークを変えない dry-run をする | [はじめに](./getting-started.md) |
| 3 | WAN と LAN を持つ小さなルーターを作る | [最初のルーターを立ち上げる](./first-router.md) |
| 4 | DHCP、DNS、RA などを LAN 側に足す | [LAN 側サービス](./lan-side-services.md) |
| 5 | DHCPv6-PD、PPPoE、DS-Lite などを WAN 側に足す | [WAN 側サービス](./wan-side-services.md) |
| 6 | NAT44 の目的と、ファイアウォールの現在地を知る | [NAT44 とファイアウォールの準備](./basic-firewall.md) |

最初の 3 段階では、先に `routerd validate`、次に
`routerd apply --once --dry-run` を使います。`routerctl` は、
`routerd.service` が起動してから状態を見るためのクライアントです。

## 目的から選ぶ

| やりたいこと | チュートリアル / 例 |
| --- | --- |
| リリースアーカイブから導入する | [インストール](./install.md) |
| YAML から最初のルーターを作る | [はじめに](./getting-started.md) |
| IPv4 NAT の完全な例を見る | [基本的な IPv4 NAT ルーター](../config-examples/basic-ipv4-nat.md) |
| ゲスト端末を分けるときの注意を知る | [ゲスト / IoT の分離を設計する](../config-examples/guest-isolation.md) |
| ディスクを使わない mini PC を試す | [ディスクレス mini PC](./diskless-minipc-walkthrough.md) |
| FreeBSD の導入の土台を確認する | [FreeBSD で始める](./freebsd-getting-started.md) |

:::caution ファイアウォールとゲスト分離

ファイアウォールのリソースはまだ土台を整えている段階です。インターネットに
公開するルーターの唯一の防御には使わないでください。また、同じスイッチや
同じ Wi-Fi の端末を、本当の意味で分けるには VLAN（スイッチ内の別 LAN） /
SSID（Wi-Fi の名前）の分離が必要です。

:::

## 対応する環境

この入門の手順は Ubuntu Server を対象にしています。FreeBSD と NixOS は
導入とサービス連携の土台がありますが、Ubuntu と同じネットワーク機能の
対応を意味しません。詳しくは [対応プラットフォーム](../platforms.md) を
確認してください。
