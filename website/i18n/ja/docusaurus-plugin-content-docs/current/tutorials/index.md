---
title: チュートリアル
slug: /tutorials
---

# チュートリアル

![インストール、はじめに、ディスクレスライブ ISO から WAN サービス、LAN サービス、ファイアウォール、FreeBSD へ進む routerd チュートリアルの進め方](/img/diagrams/tutorial-index.png)

## ディスクレス mini PC で 5 分ルーター

routerd ライブ ISO を起動し、テキストウィザードに答えます。
設定を USB メモリーへ保存すれば、内蔵ディスクに OS を入れずに、
小型 x86 mini PC を永続的なルーターとして使えます。

[ディスクレス手順を始める](/docs/tutorials/diskless-minipc-walkthrough)

![ディスクレス mini PC の流れ](/img/routerd-diskless-minipc.svg)

## 目的から選ぶ

| 目的 | チュートリアル |
| --- | --- |
| リリースアーカイブから導入する | [インストール](/docs/tutorials/install) |
| YAML から最初のルーターを作る | [はじめに](/docs/tutorials/getting-started) |
| WAN 取得とトンネルを設定する | [WAN 側サービス](/docs/tutorials/wan-side-services) |
| LAN の DHCP、DNS、RA、NTP を設定する | [LAN 側サービス](/docs/tutorials/lan-side-services) |
| 保守的なファイアウォールの基本構成を追加する | [基本のファイアウォール](/docs/tutorials/basic-firewall) |
| FreeBSD から始める | [FreeBSD で始める](/docs/tutorials/freebsd-getting-started) |

routerd の特徴は、同じリソースモデルで、仮想 SDN/VNET 間のルーターと、
ディスクレスな物理 mini PC ルーターの両方を記述できることです。
最初の導入に合う手順から始めれば、ネットワークが広がっても同じリソースを使い続けられます。
