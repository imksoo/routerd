---
title: ドキュメント
slug: /
sidebar_position: 0
sidebar_label: 概要
---

# routerd ドキュメント

![routerd を安全に試す順番。隔離した Ubuntu VM、設定ファイル、検証、dry-run、サービス起動、状態確認](/img/diagrams/intro.png)

routerd は、ルーターにしてほしいことを YAML ファイルに書くための道具です。
たとえば「この線を WAN にする」「LAN にこの IP アドレスを付ける」「LAN から
外へ出るときだけ IPv4 NAT を使う」と書きます。routerd は、その内容を読んで
Linux ホストの設定を作ります。

現在の最初の対象は **Ubuntu Server** です。FreeBSD と NixOS には、ビルド、
導入場所、サービス管理の土台がありますが、各 OS に合ったネットワーク設定の
生成はまだそろっていません。この入門では Ubuntu Server を使ってください。
対応状況は [対応プラットフォーム](./platforms.md) で確認できます。

:::caution 最初は本物の回線を触らない

routerd はネットワークを変えられます。最初の実験は、普段の自宅・学校・職場の
ルーターではなく、**隔離した Ubuntu Server VM** で行ってください。
Proxmox、KVM、VirtualBox などの VM コンソールを開ける状態にし、変更する NIC
とは別の管理経路も残します。SSH だけに頼らないでください。

:::

:::tip 推奨の安定版

新規に導入するなら、[安定版マイルストーン](./releases/stable.md) にある推奨版から
始めてください。

:::

## 最初の順番

1. 隔離した Ubuntu Server VM と、そのコンソールを用意します。
2. routerd を導入します。
3. 小さな YAML を書き、`routerd validate` で形を確かめます。
4. `routerd apply --once --dry-run` を、使い捨ての state・ledger・status ファイルを
   指定して実行します。この段階では本当に反映しません。
5. VM の構成が安全だと分かってから `routerd.service` を起動します。
6. サービスが起動した**後で**、`routerctl get status` を使って状態を見ます。

具体的なコマンドは [はじめに](./tutorials/getting-started.md) にあります。

## 目的から探す

| やりたいこと | 読むページ |
| --- | --- |
| Ubuntu Server VM に導入する | [インストール](./tutorials/install.md) |
| ネットワークの基本用語を知る | [routerd を始める前のネットワーク基礎](./tutorials/network-basics.md) |
| 最初の安全な確認をする | [はじめに](./tutorials/getting-started.md) |
| WAN と LAN を持つ小さなルーターを作る | [最初のルーターを立ち上げる](./tutorials/first-router.md) |
| IPv4 NAT の形を知る | [基本的な IPv4 NAT ルーター](./config-examples/basic-ipv4-nat.md) |
| routerd が設定をどう扱うか知る | [適用と生成](./concepts/apply-and-render.md) |
| ファイアウォールの現在地を知る | [NAT44 とファイアウォールの準備](./tutorials/basic-firewall.md) |
| ゲスト端末を本当に分ける方法を知る | [ゲスト / IoT の分離を設計する](./config-examples/guest-isolation.md) |
| 稼働中のルーターを運用する | [調整（リコンサイル）](./operations/reconcile.md) |
| リソース名や項目を調べる | [リソース API](./api-v1alpha1.md) |

## 大切な言葉

- **WAN** は、インターネットや上流ルーターにつながる側です。
- **LAN** は、自分で使う PC やスマートフォンをつなぐ側です。
- **YAML** は、設定を人が読み書きしやすい形で書くファイル形式です。
- **validate** は、設定の書き方を確認することです。
- **dry-run** は、本当には変更せず、何をする予定か試すことです。
- **daemon（デーモン）** は、起動し続けて状態を見守るプログラムです。

分からない言葉が出たら、まず [ネットワーク基礎](./tutorials/network-basics.md) を
読んでください。全部を暗記する必要はありません。

## できることと、まだ途中のこと

routerd は、インターフェース、IPv4 アドレス、DHCP、NAT44、経路、dnsmasq を使う
LAN サービスなどを扱えます。一方、ファイアウォールのリソースは現在も土台を
整えている段階です。インターネットに公開する機器の唯一の防御としては使わないで
ください。

同じスイッチや同じ Wi-Fi にいる端末は、ルーターを通らず直接通信できることが
あります。ゲストと信頼済み端末を本当に分けるには、管理できるスイッチや AP で
VLAN / SSID を分ける必要があります。

## 次の一歩

[隔離した Ubuntu Server VM へのインストール](./tutorials/install.md) から始めます。
