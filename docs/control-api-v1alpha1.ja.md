# Control API v1alpha1

routerd は daemon control API を HTTP+JSON で公開します。標準の transport は Unix domain socket です。

```text
/run/routerd/routerd.sock
```

API version:

```text
control.routerd.net/v1alpha1
```

schema は Go の型から生成します。

```text
schemas/routerd-control-v1alpha1.schema.json
```

OpenAPI 3.1 document も生成します。

```text
schemas/routerd-control-openapi-v1alpha1.json
```

## Endpoints

### GET Status

```text
GET /api/control.routerd.net/v1alpha1/status
```

Response:

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

### GET NAPT Table

```text
GET /api/control.routerd.net/v1alpha1/napt?limit=100
```

Linux conntrack state を読み取り、NAT/NAPT translation に近い形で返します。`limit=0` は summary のみです。

Response:

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

### POST Reconcile

```text
POST /api/control.routerd.net/v1alpha1/reconcile
```

Dry-run request:

```json
{
  "apiVersion": "control.routerd.net/v1alpha1",
  "kind": "ReconcileRequest",
  "dryRun": true
}
```

Response:

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

## Scheduler

`routerd serve` は小さな scheduler を持ちます。

- `--observe-interval`: 読み取り専用の observe/status 更新。標準は `30s`
- `--reconcile-interval`: 定期 reconcile。標準は `0` で無効

ヘルスチェックは one-shot CLI に混ぜず、scheduler job として追加していく想定です。
