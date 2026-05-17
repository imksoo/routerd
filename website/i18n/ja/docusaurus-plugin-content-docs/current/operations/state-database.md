---
title: 状態データベース
slug: /operations/state-database
---

# 状態データベース

routerd は状態とイベントを SQLite に永続化します。各 daemon は加えて自身の lease または state ファイルと event log を持ちます。

## 主なパス

| 種類 | パス |
| --- | --- |
| routerd 状態 DB | `/var/lib/routerd/routerd.db` |
| DHCPv6-PD lease | `/var/lib/routerd/dhcpv6-client/<name>/lease.json` |
| DHCPv4 lease | `/var/lib/routerd/dhcpv4-client/<name>/lease.json` |
| PPPoE state | `/var/lib/routerd/pppoe-client/<name>/state.json` |
| HealthCheck state | `/var/lib/routerd/healthcheck/<name>/state.json` |
| daemon 別 events | `/var/lib/routerd/<daemon>/<name>/events.jsonl` |

## events テーブル

bus はイベントを SQLite に永続化します。`EventRule` や `DerivedEvent` はこのストリームを入力にします。
日常運用では `sqlite3` を直接叩かず、`routerctl events` を使ってください：

```sh
routerctl events --limit 20
routerctl events --topic routerd.resource.status.changed
routerctl events --resource DNSResolver/lan-resolver -o json
```

## バックアップの考え方

状態 DB は **観測** された状態を持ちます。設定の代替ではありません。
意図の正典は YAML 設定ファイルで、git 管理してください。
ホストを再構築する場合、SQLite を復元するより、設定ファイルを当てて routerd に reconcile させる方が確実です。

forensic 用途で操作イベントの履歴を残したい場合は、`events.db`、`dns-queries.db`、`traffic-flows.db`、`firewall-logs.db` の定期 snapshot を取ってください。これらは追記専用で、`routerd.db` のような point-in-time backup は不要です。

## 関連項目

- [ログストレージ](../concepts/log-storage.md)
- [Reconcile と削除](./reconcile)
