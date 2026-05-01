---
title: Control API v1alpha1
slug: /reference/control-api-v1alpha1
---

# Control API v1alpha1

`routerd serve` exposes a small local control API. Operators and tooling
use it to ask the router about its current state and to drive specific
actions without restarting the daemon. The API is meant to feel familiar
to anyone who has used a Kubernetes-style status / action shape.

The default transport is a Unix domain socket:

```text
/run/routerd/routerd.sock
```

API version:

```text
control.routerd.net/v1alpha1
```

The schemas come from the same Go types the daemon uses, so the wire shape
matches the implementation:

- JSON Schema: `schemas/routerd-control-v1alpha1.schema.json`
- OpenAPI 3.1: `schemas/routerd-control-openapi-v1alpha1.json`

## Endpoints

### Status

```text
GET /api/control.routerd.net/v1alpha1/status
```

Returns the latest apply result the daemon is sitting on: phase,
generation, the time of the last apply, and how many resources were
loaded. This is the cheapest way to ask "is the router doing OK right
now?".

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
    "lastApplyTime": "2026-04-26T11:42:30Z",
    "resourceCount": 2
  }
}
```

### NAPT table

```text
GET /api/control.routerd.net/v1alpha1/napt?limit=100
```

Reads Linux conntrack and returns NAT/NAPT-style translations. This is the
quickest way to confirm that NAT is happening and that flows are pinned to
the expected egress through their conntrack mark. `limit=0` returns the
summary section only.

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
          "source": "192.168.10.132",
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

### Apply

```text
POST /api/control.routerd.net/v1alpha1/apply
```

Asks the running daemon to perform an extra apply pass. Useful right
after a YAML change, when you do not want to wait for the periodic
schedule.

Dry-run request body:

```json
{
  "apiVersion": "control.routerd.net/v1alpha1",
  "kind": "ApplyRequest",
  "dryRun": true
}
```

`dryRun: true` runs the same plan as a regular apply but does not change host
state. `dryRun: false` (or omitted) applies the result. Apply is additive:
routerd updates resources present in the current config but leaves previously
managed, now-unmentioned resources in place. Remove resources with the delete
endpoint.

Response:

```json
{
  "apiVersion": "control.routerd.net/v1alpha1",
  "kind": "ApplyResult",
  "result": {
    "phase": "Healthy",
    "resources": []
  }
}
```

### Delete

```text
POST /api/control.routerd.net/v1alpha1/delete
```

Deletes one resource through the running daemon. The target uses
`kind/name`, with the same common aliases accepted by `routerctl`.

```json
{
  "apiVersion": "control.routerd.net/v1alpha1",
  "kind": "DeleteRequest",
  "target": "IPv6PrefixDelegation/wan-pd",
  "dryRun": true
}
```

`dryRun: true` reports the resource and owned artifacts that would be removed.
Without dry-run, routerd removes matching ownership records and performs the
cleanup actions it knows for routerd-owned artifacts.

### DHCPv6 client event

```text
POST /api/control.routerd.net/v1alpha1/dhcp6-event
```

Managed DHCPv6-PD client hook scripts use this endpoint to report lease
events back to the daemon. The endpoint is local-only and is not intended as a
remote management API. routerd stores the event in
`ipv6PrefixDelegation.<name>.lease`, updates `lastReplyAt` for reply-like
events, and records a resource event.

Example body:

```json
{
  "apiVersion": "control.routerd.net/v1alpha1",
  "kind": "DHCP6Event",
  "resource": "wan-pd",
  "reason": "BOUND6",
  "prefix": "2001:db8:1200:1230::/60",
  "iaid": "1",
  "t1": "7200",
  "t2": "12600",
  "pltime": "14400",
  "vltime": "14400",
  "serverID": "00030001020000000001"
}
```

## Talking to it directly

`routerctl` wraps these endpoints, but `curl` works too:

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
  -d '{"apiVersion":"control.routerd.net/v1alpha1","kind":"ApplyRequest","dryRun":true}' \
  http://routerd/api/control.routerd.net/v1alpha1/apply
```

## Daemon scheduler

`routerd serve` carries a small scheduler that drives periodic work on top
of the control API:

- `--observe-interval`: how often the daemon refreshes its read-only
  observation of host state. Defaults to 30 seconds.
- `--apply-interval`: how often the daemon performs a full apply.
  Defaults to 0, which disables scheduled applies — the daemon then
  only applies in response to control API calls.

Health checks are owned by the scheduler rather than mixed into one-shot
CLI commands. This keeps the same apply path in use for daemon mode
and one-shot mode.
