---
title: 状態データベース
slug: /operations/state-database
---

# 状態データベース

routerd 本体は SQLite に状態とイベントを保存します。
専用デーモンは、それぞれの lease または state ファイルと events.jsonl を持ちます。

## 主なパス

| 種類 | パス |
| --- | --- |
| routerd DB | `/var/lib/routerd/routerd.db` |
| DHCPv6-PD lease | `/var/lib/routerd/dhcpv6-client/<name>/lease.json` |
| DHCPv4 lease | `/var/lib/routerd/dhcpv4-client/<name>/lease.json` |
| PPPoE state | `/var/lib/routerd/pppoe-client/<name>/state.json` |
| HealthCheck state | `/var/lib/routerd/healthcheck/<name>/state.json` |
| デーモンイベント | `/var/lib/routerd/<daemon>/<name>/events.jsonl` |

## events テーブル

bus はイベントを SQLite に保存します。
EventRule と DerivedEvent は、このイベント列を入力にします。

例:

```sql
select ts, topic, attributes
from events
order by ts desc
limit 20;
```

## バックアップ

状態データベースは観測結果です。
YAML の代わりではありません。
本当に保存すべき意図は、設定ファイルと git の履歴に置きます。
