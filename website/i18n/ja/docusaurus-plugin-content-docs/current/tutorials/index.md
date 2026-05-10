---
title: チュートリアル
slug: /tutorials
---

# チュートリアル

## ディスクレス mini PC で 5 分ルーター

routerd ライブ ISO を起動し、テキストウィザードに答えます。
設定を USB メモリーへ保存すると、内蔵ディスクへ OS を入れずに、
小型 x86 mini PC を永続ルーターとして使えます。

[ディスクレス手順を始める](/docs/tutorials/diskless-minipc-walkthrough)

![ディスクレス mini PC の流れ](/img/routerd-diskless-minipc.svg)

## 目的から選ぶ

| 目的 | チュートリアル |
| --- | --- |
| リリースアーカイブから導入する | [Install](/docs/tutorials/install) |
| YAML から最初のルーターを作る | [Getting started](/docs/tutorials/getting-started) |
| WAN 取得とトンネルを設定する | [WAN-side services](/docs/tutorials/wan-side-services) |
| LAN の DHCP、DNS、RA、NTP を設定する | [LAN-side services](/docs/tutorials/lan-side-services) |
| 保守的な firewall baseline を追加する | [Basic firewall](/docs/tutorials/basic-firewall) |
| NixOS から始める | [NixOS getting started](/docs/tutorials/nixos-getting-started) |
| FreeBSD から始める | [FreeBSD getting started](/docs/tutorials/freebsd-getting-started) |

routerd の特徴は、同じリソースモデルで仮想 SDN/VNET 間のルーターと、
ディスクレス物理 mini PC ルーターの両方を記述できることです。
最初の導入に合う手順から始め、ネットワークが広がっても同じリソースを使えます。
