---
title: 状態データベース
slug: /operations/state-database
---

# 状態データベース

routerd は観測状態と所有台帳を 1 つの SQLite ファイルに持ちます。このページでは
実用的なクエリと、ファイルの置き場所を扱います。

## パス

| OS | 既定 |
|---|---|
| Linux | `/var/lib/routerd/routerd.db` |
| FreeBSD | `/var/db/routerd/routerd.db` |

`routerd apply` や `routerd serve` に `--state-dir` を渡すと、その配下に置かれます。

## テーブル

| テーブル | 役割 |
|---|---|
| `generations` | apply 1 周ごとに 1 行: `id`、`created_at`、`config_hash`、`outcome`、警告 JSON |
| `objects` | 1 リソース 1 行: `api_version`、`kind`、`name`、`status` JSON |
| `artifacts` | 所有台帳。routerd が所有するホスト側成果物ごとに 1 行 |
| `events` | apply 中に記録すべき観測 |
| `access_logs` | 将来のコントロール API 監査用に予約 |

概念的な対応は k8s 流です。`objects` ≒ Kubernetes の `status` 付きオブジェクト、
`artifacts` ≒ owner reference、`events` ≒ Kubernetes events。

## よくあるクエリ

### PD が把握しているプレフィックス

```bash
sqlite3 /var/lib/routerd/routerd.db <<'SQL'
select json_extract(status, '$.currentPrefix') as current,
       json_extract(status, '$.lastPrefix') as last,
       json_extract(status, '$.lastObservedAt') as observed_at
from objects
where kind = 'DHCPv6PrefixDelegation';
SQL
```

### 直近のイベント

```bash
sqlite3 /var/lib/routerd/routerd.db \
  "select created_at, type, reason, message
   from events
   order by id desc limit 30;"
```

### routerd が所有しているもの

```bash
sqlite3 /var/lib/routerd/routerd.db \
  "select kind, name, owner_kind, owner_name
   from artifacts
   order by kind, name;"
```

### 直近の apply

```bash
sqlite3 /var/lib/routerd/routerd.db \
  "select id, created_at, config_hash, outcome
   from generations
   order by id desc limit 5;"
```

## `routerctl` 経由の確認

日々の確認はまず `routerctl` を使うのが楽です。

```bash
routerctl get
routerctl describe ipv6pd/wan-pd
routerctl show inventory/host -o yaml
```

これらはデーモンのローカルソケット経由で同じ DB を読みますが、型付き API です。
CLI が出していない横断クエリが必要なときに SQLite シェルを使ってください。

## バックアップと移行

DB は通常の SQLite ファイルです。routerd が動いた状態でバックアップするには:

```bash
sqlite3 /var/lib/routerd/routerd.db ".backup /tmp/routerd-backup.db"
```

ルーターホストを移行するときは YAML と一緒にこの DB をコピーしてください。
routerd は次回起動時にスキーマ差分を検出し、新バイナリが forward migration を
含んでいればその場で移行します。

## DB を触らない方がよいとき

- apply が走っている最中 (writer と競合します)
- 「古そうなプレフィックス」「古そうな所有行」を手で「直す」目的。次の apply で
  ホスト状態を読み直して reconcile します。台帳を手で編集すると、routerd が
  自分の所有物を見つけられなくなる恐れがあります。

本当に状態をリセットしたい場合は、デーモンを止めてファイルを別名退避し、
次の `apply` で YAML から再構築させてください。台帳が失われたホスト側成果物は
routerd が回収しません。必要なら手で消す覚悟で行ってください。
