---
title: Control API v1alpha1
slug: /reference/control-api-v1alpha1
---

# Control API v1alpha1

routerd and its managed daemons expose a local HTTP+JSON API over Unix domain sockets. The API is **not** for remote management — it is the channel through which `routerctl`, the routerd controllers themselves, and operations scripts on the same host read state.

## routerd main process

`routerd serve` listens on:

```text
/run/routerd/routerd.sock
/run/routerd/routerd-status.sock
```

The main control socket is intended for privileged local clients and exposes
mutating endpoints such as apply and delete. The read-only status socket exposes
only status-style endpoints and is safe for regular users to query.

Read endpoints on the main control socket expose status, events, and resource
state. Highlights:

| Method and path | Purpose |
| --- | --- |
| `GET /api/control.routerd.net/v1alpha1/status` | routerd's own status |
| `GET /api/control.routerd.net/v1alpha1/connections` | live connections from conntrack or pf state |
| `GET /api/control.routerd.net/v1alpha1/dns-queries` | DNS query history |
| `GET /api/control.routerd.net/v1alpha1/traffic-flows` | traffic flow history |
| `GET /api/control.routerd.net/v1alpha1/firewall-logs` | firewall log entries |

## Managed daemons

Stateful daemons each have their own socket:

```text
/run/routerd/dhcpv6-client/wan-pd.sock
/run/routerd/dhcpv4-client/wan.sock
/run/routerd/pppoe-client/wan-pppoe.sock
/run/routerd/healthcheck/internet.sock
```

On FreeBSD, the equivalent path is `/var/run/routerd/...`.

## Common daemon endpoints

| Method and path | Purpose |
| --- | --- |
| `GET /v1/healthz` | Liveness check |
| `GET /v1/status` | Daemon status and related resource state |
| `GET /v1/events` | Event log; supports `since`, `wait`, `topic` query parameters |
| `POST /v1/commands/reload` | Re-read configuration |
| `POST /v1/commands/renew` | Daemon-specific active operation (DHCPv6 Renew, DHCPv4 lease refresh, immediate health probe, etc.) |
| `POST /v1/commands/stop` | Graceful shutdown |

The semantics of `renew` differ per daemon: DHCPv6 sends a Renew, DHCPv4 refreshes the lease, healthcheck triggers an immediate probe.

## Phase vocabulary

`ResourceStatus.phase` uses a shared vocabulary across resources:

| Phase | Meaning |
| --- | --- |
| `Pending` | Waiting for required input |
| `Bound` | A lease (DHCP, etc.) is held |
| `Applied` | Host-side state has been applied |
| `Up` | A tunnel or link is up |
| `Installed` | Routes or configuration files are installed |
| `Healthy` | Health check meets its success threshold |
| `Unhealthy` | Health check meets its failure threshold |
| `Error` | An operation failed |

Each phase carries a `conditions` array. Decisions in client code should be based on `phase` and `conditions`, not on log strings.

## Events

Events have a topic and attributes:

```json
{
  "topic": "routerd.dhcpv6.client.prefix.renewed",
  "attributes": {
    "resource.kind": "DHCPv6PrefixDelegation",
    "resource.name": "wan-pd"
  }
}
```

routerd persists events into SQLite. Managed daemons additionally keep them in their own `events.jsonl` files. `EventRule` and `DerivedEvent` consume this stream to emit virtual events.
