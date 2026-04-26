---
title: 制御 API v1alpha1
slug: /reference/control-api-v1alpha1
---

# 制御 API v1alpha1

routerd はデーモン制御 API を HTTP+JSON で公開します。標準の通信路は Unix ドメインソケットです。

```text
/run/routerd/routerd.sock
```

API バージョン:

```text
control.routerd.net/v1alpha1
```

スキーマは Go の型から生成します。

```text
schemas/routerd-control-v1alpha1.schema.json
```

OpenAPI 3.1 文書も生成します。

```text
schemas/routerd-control-openapi-v1alpha1.json
```

## エンドポイント

### 状態の取得

```text
GET /api/control.routerd.net/v1alpha1/status
```

応答:

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

### NAPT テーブルの取得

```text
GET /api/control.routerd.net/v1alpha1/napt?limit=100
```

Linux の conntrack 状態を読み取り、NAT/NAPT 変換に近い形で返します。`limit=0` は概要のみです。

応答:

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

### 収束処理の実行

```text
POST /api/control.routerd.net/v1alpha1/reconcile
```

予行実行のリクエスト:

```json
{
  "apiVersion": "control.routerd.net/v1alpha1",
  "kind": "ReconcileRequest",
  "dryRun": true
}
```

応答:

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

## curl

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

## スケジューラー

`routerd serve` は小さなスケジューラーを持ちます。

- `--observe-interval`: 読み取り専用の観測と状態更新。標準は `30s`
- `--reconcile-interval`: 定期的な収束処理。標準は `0` で無効

ヘルスチェックは一度だけ実行する CLI に混ぜず、スケジューラーのジョブとして追加していく想定です。
