---
title: routerd とは
slug: /concepts/what-is-routerd
sidebar_position: 1
---

# routerd とは

routerd は **ルーター用ホスト** のための小さな宣言的コントロールプレーンです。
ルーターのあるべき振る舞いを YAML で書き、`routerd apply` を実行すると、
インターフェース、アドレス、DHCP/DNS サービス、NAT、ポリシールーティング、
ファイアウォール、経路ヘルスチェックなどがまとめてその形に揃います。

このページは「routerd とは何か、何を解決するか、他のツールとの位置関係」を説明します。
すぐ使い始めたい場合は [チュートリアル](../tutorials/install) へどうぞ。

## routerd が解決したい問題

ルーター用ホストを手で設定すると、設定対象がいろいろなところに散らばります。

- `/etc/netplan/*.yaml`: インターフェースとアドレス
- `dnsmasq.conf`: LAN の DHCP・DHCPv6・RA・DNS
- `nftables.conf`: NAT とファイアウォール
- `dhclient` / `dhcp6c` / `systemd-networkd`: WAN の DHCP / PD
- `sysctl.conf`: IP 転送、`accept_ra` など
- `systemd-timesyncd`、ホスト名、...

これらをそれぞれ別々に、しばしば場当たり的なシェルコマンドで設定することになります。
同じルーターを別ホストで再現したい場合、運用者が同じ手順を頭から踏み直す必要があり、
抜け漏れに気付きにくい構造です。「これがルーターの定義」と言える単一の成果物が
存在しません。

## routerd がやること

routerd はルーターを **ひとつのリソースグラフ** として扱います。ルーターの YAML
には型付きリソース（インターフェース、DHCP クライアント、NAT ルール、ファイアウォール
ゾーンなど）が並び、`routerd apply` 1 回でホストがその形に追従します。

具体的には次のような流れです。

- YAML ファイル（`Router` リソースとリソース一覧）を読む
- 検証する (`routerd validate`)
- 実機に対する変更計画を表示する (`routerd apply --dry-run`)
- 反映する (`routerd apply`)
- 観測した状態と所有しているものをローカルの SQLite に記録する
- デーモンとして定期的に同じ apply を回し、ホストの状態を維持する

YAML ファイルがあればルーターを再現できます。git に置いて、レビューは差分で、
ロールバックはコミットで行えます。

## routerd ではないもの

- **汎用の構成管理ツールではありません。** routerd の型はルーターの振る舞いに
  特化しています。Ansible や Puppet の代わりにはなりません。
- **Linux ディストリビューションやアプライアンスではありません。** routerd は
  既存の Linux または FreeBSD ホストの上で動き、ホスト自身のデーモン
  (systemd-networkd、dnsmasq、nftables、KAME `dhcp6c` など) を使います。
- **リモート API ではありません。** 集中コントロールプレーンはありません。
  routerd は各ルーターホストでローカルに動き、`routerctl` がローカルの
  コントロールソケットでデーモンと話します。

## 誰のためのものか

- 「現場のルーター」(自宅、ラボ、支店ルーターなど) を少数台数で運用していて、
  レビュー可能で再現可能にしたい運用者。
- 他のスタックではすでに Infrastructure as Code を書いていて、ルーターも
  そこに含めたい人。
- ホストを差し替える (Ubuntu → NixOS、NIC の交換) ときに、ネットワーク設定を
  書き直したくない人。

## 次に読むもの

- [設計思想](./design-philosophy) — routerd の根本にある考え方
- [リソースモデル](./resource-model) — `Router`、`Resource`、`kind`、
  `metadata.name`
- [apply と render](./apply-and-render) — 日常的に使う動詞
- [状態と所有](./state-and-ownership) — routerd が記憶するもの
- [インストール](../tutorials/install) — 使い始める
