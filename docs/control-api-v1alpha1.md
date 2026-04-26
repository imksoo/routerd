# Control API v1alpha1

routerd exposes a local daemon control API over HTTP+JSON. The default transport
is a Unix domain socket:

```text
/run/routerd/routerd.sock
```

The API version is:

```text
control.routerd.net/v1alpha1
```

The schema is generated from Go types:

```text
schemas/routerd-control-v1alpha1.schema.json
```

An OpenAPI 3.1 document is also generated:

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

This reads Linux conntrack state and returns NAT/NAPT-like translations. Use
`limit=0` for summary only.

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

`routerd serve` owns a small scheduler:

- `--observe-interval`: periodic read-only observe/status refresh; defaults to `30s`
- `--reconcile-interval`: periodic reconcile; defaults to `0`, disabled

Health checks should be added as scheduler jobs rather than mixed into one-shot
CLI commands.
