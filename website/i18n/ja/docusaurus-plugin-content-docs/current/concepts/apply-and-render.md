---
title: apply と render
slug: /concepts/apply-and-render
sidebar_position: 4
---

# apply と render

routerd には主要な動詞が 6 つあります。日々の流れ「YAML を直す → 変更を見る → ホストへ
反映する → 何が起きたか確認する」をこれらでカバーします。

## 一覧

| 動詞 | コマンド | 用途 |
|---|---|---|
| `validate` | `routerd validate` | ホストに触らずに YAML をスキーマ検査する |
| `render` | `routerd render` | この YAML から書き出されるファイルを見る |
| `apply` | `routerd apply` | ホストを YAML の形に合わせる |
| `get` | `routerctl get` | リソースを一行ずつ要約 (kubectl 流) |
| `describe` | `routerctl describe` | 一つのリソースを節立てて要約 |
| `show` | `routerctl show` | 一つのリソースの完全データを YAML/JSON で見る |

`validate`、`render`、`apply` は YAML ファイルを対象にします。`get`、`describe`、
`show` は動いているルーター (SQLite 状態とローカルのコントロールソケット)を見ます。

## `validate`

`routerd validate --config router.yaml` は YAML を読んで、各リソースをスキーマに
照らして問題を報告します。ホストには触りません。CI に組み込んだり、apply の前に毎回
走らせたりできます。

```bash
routerd validate --config router.yaml
```

`validate` が通れば YAML の構造は正しいです。「この設定が実機の現実と整合する」までは
保証しません (たとえばホストに該当のインターフェース名が存在するかは別問題です)。
それは `apply --dry-run` の役目です。

## `render`

`routerd render` は、与えた YAML と対象プラットフォームに対し、routerd が書き出す
ファイルを表示します。

```bash
routerd render linux --config router.yaml
routerd render freebsd --config router.yaml
```

出力には `systemd-networkd` のドロップイン、`dnsmasq.conf`、`nftables.conf`、
FreeBSD の `rc.conf` 断片などが含まれます。新しいリソースを実装中に「何が出るか」を
確認するときに便利です。

`render` は読み取り専用です。ホストと通信する必要もありません。

## `apply`

`routerd apply` がホストを変更する唯一の動詞です。次の手順で進みます。

1. YAML を読む
2. ホストの現状と、routerd のローカル状態データベースを読む
3. 変更計画を作る (または `--dry-run` ならドライランで見せる)
4. 生成ファイルを書き、必要ならサービスを再起動し、状態 DB を更新する
5. 警告があればイベントとして記録する

```bash
sudo routerd apply --once --config /usr/local/etc/routerd/router.yaml
```

`--once` は 1 回だけ apply して終わります。`--once` を付けない `routerd serve` は
デーモンとして定期的に同じ apply を回します。中身は同じコードで、`serve` は単に
`apply` をループするだけです。

事前確認として `apply --dry-run` を使えます。

```bash
sudo routerd apply --once --dry-run --config /usr/local/etc/routerd/router.yaml
```

ファイルを書かずサービスも再起動せず、変更計画だけを表示します。

## `get`、`describe`、`show`

確認用の 3 動詞は kubectl の慣行に倣っています。

- `routerctl get`: リソースを一行ずつ要約。一覧を眺めたり pipe で扱ったりするのに向く
- `routerctl describe`: 一つのリソースを節立てて人間向けに要約 (観測状態や直近イベント
  も含む)
- `routerctl show`: 一つのリソースの完全データを YAML / JSON で出す。スクリプトで
  扱いやすい

例:

```bash
routerctl get
routerctl describe interface/wan
routerctl describe ipv6pd/wan-pd
routerctl show ipv6pd/wan-pd -o yaml
```

同じ 3 動詞が `Inventory` (`routerctl describe inventory/host`)、`Router`、その他
すべての種類に対して使えます。

## 流れの図

```
              YAML
                │
        ┌───────┴───────┐
        │   validate    │  スキーマ検査 (ホスト不要)
        └───────┬───────┘
                │
        ┌───────┴───────┐
        │    render     │  生成ファイルの先見 (ホスト不要)
        └───────┬───────┘
                │
        ┌───────┴───────┐
        │     apply     │  ファイル書き出し、サービス再起動
        └───────┬───────┘
                │
        ┌───────┴───────┐
        │ host + state  │  実機ルーター + SQLite 状態
        └───────┬───────┘
                │
        ┌───────┴───────┐
        │ get / describe│
        │ / show        │  状態の確認
        └───────────────┘
```

ホストを変更するのは `apply` だけです。それ以外はすべて読み取り専用です。

## 次に読むもの

- [状態と所有](./state-and-ownership) — `apply` が何を記録するか
- [インストール](../tutorials/install) — 最初の apply
- [リソース所有](../reference/resource-ownership) — `apply` が約束することと
  クリーンアップの規則
