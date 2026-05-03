---
title: 状態と所有
slug: /concepts/state-and-ownership
sidebar_position: 5
---

# 状態と所有

routerd は、宣言した意図と観測した状態を分けて扱います。
YAML は利用者が管理する意図です。
SQLite、lease ファイル、events.jsonl は routerd と専用デーモンが観測した状態です。

## 状態の置き場所

Linux の既定値は次の通りです。

| 種類 | 例 |
| --- | --- |
| routerd 状態データベース | `/var/lib/routerd/routerd.db` |
| DHCPv6-PD リース | `/var/lib/routerd/dhcpv6-client/wan-pd/lease.json` |
| DHCPv4 リース | `/var/lib/routerd/dhcpv4-client/wan/lease.json` |
| PPPoE 状態 | `/var/lib/routerd/pppoe-client/<name>/state.json` |
| ヘルスチェック状態 | `/var/lib/routerd/healthcheck/<name>/state.json` |
| 実行時ソケット | `/run/routerd/.../*.sock` |

FreeBSD では `/var/run` と `/var/db` 系のパスを使う構成があります。

## 所有の考え方

routerd が作るホスト側の構成物には、所有元のリソースがあります。
たとえば dnsmasq 設定は DHCP と RA の各リソースから、`routerd-dns-resolver` の設定は `DNSResolver` と `DNSZone` から、nftables の NAT テーブルは `NAT44Rule` から作られます。

所有元が分かると、次の判断ができます。

- この構成物は routerd が変更してよいものか。
- YAML からリソースを消したとき、ホスト側も消してよいか。
- 既存の設定を取り込むだけか、routerd が新しく作るのか。

## 古くなった状態を使わない

リースや観測値は便利ですが、古くなった値を使い続けると危険です。
特に DHCPv6-PD のプレフィックスは、Bound であることを確認できる場合だけ下流へ展開します。
確認できない場合は、AAAA、RA、DHCPv6 サーバー、LAN IPv6 アドレスの適用を止めます。

## イベント

routerd と専用デーモンは、状態変化をイベントとして記録します。
イベントは SQLite の `events` テーブルやデーモンごとの `events.jsonl` に残ります。
EventRule と DerivedEvent は、このイベントや状態を使って仮想的な状態変化を作ります。
