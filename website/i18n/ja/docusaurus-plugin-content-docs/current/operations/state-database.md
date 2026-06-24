---
title: 状態データベース
slug: /operations/state-database
---

# 状態データベース

![Diagram showing routerd state database paths, daemon lease and event files, routerctl event access, and backup philosophy where YAML remains authoritative and event databases provide forensic history](/img/diagrams/operations-state-database.png)

routerd は状態とイベントを SQLite に永続化します。各デーモンはこれに加えて、自身のリースまたは状態ファイルとイベントログを持ちます。

## 主なパス

| 種類 | パス |
| --- | --- |
| routerd 状態 DB | `/var/lib/routerd/routerd.db` |
| DHCPv6-PD リース | `/var/lib/routerd/dhcpv6-client/<name>/lease.json` |
| DHCPv4 リース | `/var/lib/routerd/dhcpv4-client/<name>/lease.json` |
| PPPoE 状態 | `/var/lib/routerd/pppoe-client/<name>/state.json` |
| HealthCheck 状態 | `/var/lib/routerd/healthcheck/<name>/state.json` |
| デーモン別イベント | `/var/lib/routerd/<daemon>/<name>/events.jsonl` |

## events テーブル

イベントバスはイベントを SQLite に永続化します。`EventRule` や `DerivedEvent` は、このストリームを入力にします。
日常の運用では `sqlite3` を直接叩かず、`routerctl events` を使ってください。

```sh
routerctl events --limit 20
routerctl events --topic routerd.resource.status.changed
routerctl events --resource DNSResolver/lan-resolver -o json
```

## バックアップの考え方

状態 DB は**観測した**状態を持つもので、設定の代わりにはなりません。
意図の正本は YAML 設定ファイルなので、git で管理してください。
ホストを再構築するときは、SQLite を復元するよりも、設定ファイルを当てて routerd に調整（リコンサイル）させる方が確実です。

事後調査の用途で操作イベントの履歴を残したい場合は、`events.db`、`dns-queries.db`、`traffic-flows.db`、`firewall-logs.db` のスナップショットを定期的に取ってください。これらは追記専用なので、`routerd.db` のような特定時点のバックアップは不要です。

## 関連項目

- [ログストレージ](../concepts/log-storage.md)
- [Reconcile と削除](./reconcile)
