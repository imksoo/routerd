---
title: 状態と所有
slug: /concepts/state-and-ownership
sidebar_position: 5
---

# 状態と所有

routerd は記憶を 2 つに分けています。「宣言したもの」(YAML、git) と
「観測・作り出したもの」(ローカル SQLite データベース) です。この分離があるから
デーモンを動かしっぱなしでも安心して任せられます。

## 状態データベース

routerd の状態は 1 つの SQLite ファイルにあります。既定のパス:

- Linux: `/var/lib/routerd/routerd.db`
- FreeBSD: `/var/db/routerd/routerd.db`

スキーマは k8s 流で、いくつかの小さなテーブルがそれぞれ型付き JSON を持ちます。

| テーブル | 役割 |
|---|---|
| `generations` | `apply` 1 周分: 時刻、設定ハッシュ、警告、結果 |
| `objects` | 1 リソース 1 行: 最新の観測ステータスを JSON で |
| `artifacts` | 所有台帳: routerd が入れたホスト側のファイルやユニット (どのリソース由来か) |
| `events` | apply 中に記録すべき観測 (警告、プレフィックス変動、lease lost など) |
| `access_logs` | 将来のローカル制御 API 監査用に予約 |

簡単なクエリ例:

```bash
sqlite3 /var/lib/routerd/routerd.db \
  "select json_extract(status, '$.currentPrefix')
   from objects
   where kind = 'DHCPv6PrefixDelegation' and name = 'wan-pd';"
```

実行時に `sqlite3` コマンドは不要です。デーモンはドライバを組み込んでいます。
`sqlite3` は運用者の便利ツールとして使います。

## `objects` に入るもの

YAML にあるリソースごと (および `Inventory` のような観測専用種) に、`status` を
JSON で持ちます。中身は種類により異なります。たとえば `DHCPv6PrefixDelegation` の
`status` は次のようなフィールドを持ちます。

- `currentPrefix`: 直近で観測した委譲プレフィックス
- `lastPrefix`: その 1 つ前の値 (apply 跨ぎで保持)
- `lastObservedAt`: 直近の観測時刻
- `duid`、`duidText`、`iaid`、`expectedDUID`: DHCPv6 クライアントの識別子
- `validLifetime`: 取れていればリース寿命

`spec` を `status` にコピーすることはありません。望む設定の真実の元は常に YAML です。

## `artifacts` に入るもの

`artifacts` は所有台帳です。routerd がホスト側に作ったオブジェクト
(`systemd-networkd` のドロップイン、`dnsmasq.conf`、カーネル経路) ごとに
次の情報を記録します。

- どの routerd リソースがそれを所有しているか
- どんな種類のホスト成果物か (ファイル、sysctl、経路、サービスユニット)
- 後から特定できる識別子 (パス、キーなど)

YAML からリソースを消すと、次の apply で台帳を辿り、所有者がもう望む集合にいない
成果物を見つけて取り外します。これにより、YAML を編集しても dnsmasq の孤児
ドロップインや古い経路が居残ることを避けます。

台帳は **永続的な記録** であり、キャッシュではありません。ホスト状態だけから
復元するのは危険です。routerd 由来か運用者が手で作ったかを区別できなくなります。

## `events` に入るもの

`events` は apply 中の意味のある遷移を残します。例:

- `Normal InventoryObserved`: ホストインベントリが変わった
- `Warning ApplyWarning`: 何かが望む状態に届かなかった
- `Normal PrefixObserved`: DHCPv6-PD プレフィックスを初観測した

`routerctl describe` の最後に直近のイベントが並ぶので、そこで確認できます。

## なぜこの分離が大事か

「望むもの」(YAML)、「実際そうなっているか」(objects)、「routerd が入れたもの」
(artifacts) を分けてあるから、apply は安全で戻せるものになります。

- リソースを消す → apply が台帳から成果物を見つけて、ホスト側から取り外す
- リソースは残っている → apply が観測 status と spec を diff して必要な所だけ直す
- 台帳に無いもの → routerd の所有ではないので触らない。手で置いたファイルが
  黙って消えることはない

routerd を長時間動くデーモンとして任せられるのも、この性質のおかげです。
毎周毎周ホスト状態を読み直し、本当に違うときだけ収束させます。

## 次に読むもの

- [リソース所有](../reference/resource-ownership) — 台帳の正式なルール
- [運用: 状態データベース](../operations/state-database) — 実用的なクエリと観察
- [apply と render](./apply-and-render) — この状態を読み書きする動詞
