---
title: 制御 API v1alpha1
slug: /reference/control-api-v1alpha1
---

# 制御 API v1alpha1

`routerd serve` は、ルータの状態を尋ねたり、特定の操作を依頼するための小さな制御 API をローカルで公開します。デーモンを再起動せずに、運用者やツールから問い合わせ・操作するためのものです。Kubernetes 風の status / action 形状に近い見た目になっています。

既定の通信路は Unix ドメインソケットです。

```text
/run/routerd/routerd.sock
```

API バージョン:

```text
control.routerd.net/v1alpha1
```

スキーマはデーモン本体と同じ Go の型から生成しています。実装と仕様が乖離しません。

- JSON Schema: `schemas/routerd-control-v1alpha1.schema.json`
- OpenAPI 3.1: `schemas/routerd-control-openapi-v1alpha1.json`

## エンドポイント

### ステータス

```text
GET /api/control.routerd.net/v1alpha1/status
```

直近の反映結果（フェーズ、世代番号、最後に反映した時刻、読み込まれているリソース数）を返します。「いまルータは健全に動いているか」を最も軽く確認するための入口です。

```json
{
  "apiVersion": "control.routerd.net/v1alpha1",
  "kind": "Status",
  "metadata": {
    "name": "routerd"
  },
  "status": {
    "phase": "Healthy",
    "generation": 1777203750,
    "lastReconcileTime": "2026-04-26T11:42:30Z",
    "resourceCount": 2
  }
}
```

### NAPT テーブル

```text
GET /api/control.routerd.net/v1alpha1/napt?limit=100
```

Linux のコネクション追跡を読み出し、NAT/NAPT に近い形で返します。NAT が動いているか、フローが想定の出口にコネクション追跡マークで固定できているかを最短で確認できます。`limit=0` の場合は集計情報だけを返します。

```json
{
  "apiVersion": "control.routerd.net/v1alpha1",
  "kind": "NAPTTable",
  "metadata": {
    "name": "conntrack"
  },
  "status": {
    "count": 312,
    "max": 65536,
    "byMark": {
      "0": 10,
      "256": 101
    },
    "entries": [
      {
        "family": "ipv4",
        "protocol": "tcp",
        "state": "ESTABLISHED",
        "original": {
          "source": "192.168.160.132",
          "destination": "93.184.216.34",
          "sourcePort": "34567",
          "destinationPort": "443"
        },
        "reply": {
          "source": "93.184.216.34",
          "destination": "192.0.0.2",
          "sourcePort": "443",
          "destinationPort": "34567"
        },
        "mark": "256"
      }
    ]
  }
}
```

### 反映の実行

```text
POST /api/control.routerd.net/v1alpha1/reconcile
```

動作中のデーモンに、追加で 1 回の反映を依頼します。YAML を書き換えた直後に、定期反映の周期を待たず即座に試したい場合に向いています。

予行実行のリクエストボディ:

```json
{
  "apiVersion": "control.routerd.net/v1alpha1",
  "kind": "ReconcileRequest",
  "dryRun": true
}
```

レスポンス:

```json
{
  "apiVersion": "control.routerd.net/v1alpha1",
  "kind": "ReconcileResult",
  "result": {
    "phase": "Healthy",
    "resources": []
  }
}
```

`dryRun: true` のときは通常の反映と同じ計画を立てますが、ホストの状態は変更しません。`dryRun: false`（または省略）のときに実際に反映します。

## 直接叩く例

`routerctl` がこれらをラップしていますが、`curl` でも問題なく扱えます。

```sh
curl --unix-socket /run/routerd/routerd.sock \
  http://routerd/api/control.routerd.net/v1alpha1/status
```

```sh
curl --unix-socket /run/routerd/routerd.sock \
  'http://routerd/api/control.routerd.net/v1alpha1/napt?limit=20'
```

```sh
curl --unix-socket /run/routerd/routerd.sock \
  -H 'Content-Type: application/json' \
  -d '{"apiVersion":"control.routerd.net/v1alpha1","kind":"ReconcileRequest","dryRun":true}' \
  http://routerd/api/control.routerd.net/v1alpha1/reconcile
```

## デーモンのスケジューラ

`routerd serve` は、制御 API と同じ反映処理を周期的に動かすための小さなスケジューラを内蔵しています。

- `--observe-interval`: ホスト状態を読み取るだけの観測を行う周期。既定値は 30 秒。
- `--reconcile-interval`: 反映を行う周期。既定値は 0 で、定期反映は無効です。この設定では、制御 API から依頼があったときだけ反映します。

ヘルスチェックは 1 回限りの CLI コマンド側ではなく、このスケジューラの仕事として扱います。デーモン動作と 1 回限りの CLI 動作で同じ反映経路が使われる構成を維持するためです。
