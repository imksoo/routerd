---
title: Control API v1alpha1
slug: /reference/control-api-v1alpha1
---

# Control API v1alpha1

![Diagram showing the Control API v1alpha1 local socket model from routerd main sockets and managed daemon sockets to status, events, commands, resource phases, and local-only client contracts](/img/diagrams/control-api-v1alpha1.png)

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
| `POST /api/control.routerd.net/v1alpha1/dhcp-lease-event` | local dnsmasq lease-hook event |

## SAM enrollment endpoints

Dynamic SAM enrollment uses the same API handler, but an RR exposes these
endpoints only through the explicitly configured authenticated listener or
privileged local socket described by the enrollment runbook. They are not a
general remote-management API.

| Method and path | Purpose |
| --- | --- |
| `POST /api/control.routerd.net/v1alpha1/sam-enrollment-claims` | Admit one leaf's client-authored enrollment claim. |
| `POST /api/control.routerd.net/v1alpha1/sam-enrollment-claims/{name}/revoke` | Revoke an accepted claim; use it for deliberate operator action, not recovery. |
| `GET /api/control.routerd.net/v1alpha1/sam-enrollment-topologies/{name}?claim=...&claimDigest=...&claimIdentityDigest=...` | Fetch the named RRSet and, only when it is attested, the optional direct peer group. |

`claimIdentityDigest` is a hash of only client-authored claim material. With a
`joinTokenFrom` policy, that material is authenticated by the join HMAC; without
one, the deployment is a trusted control-plane boundary. A current RR uses the
digest to distinguish an explicit revoke, an empty clean-boot admission store,
and a different active identity. `SAMEnrollmentClient` treats identity-aware
absence as permission to re-submit its current direct claim only after every
bootstrap RR has been checked. A different active identity is replaceable only
for initial admission or a local claim change, never a periodic renewal. A
missing digest, an old/mixed RR, a revoke, or any request failure must keep the
RR-only fallback; callers should use the client/controller rather than
interpreting these responses themselves.

## DHCP lease hook

`POST /api/control.routerd.net/v1alpha1/dhcp-lease-event` is a privileged,
local hook endpoint for dnsmasq lease changes; it is not a remote-management or
general-purpose event API. `routerd-dhcp-event-relay` runs once for each
dnsmasq callback and normalizes dnsmasq's raw callback verbs before posting the
event.

Direct callers must send only the canonical `action` values `added`, `renewed`,
or `removed`. The raw dnsmasq verbs `add`, `old`, and `del` are accepted only at
the relay boundary. `ip` is required; `mac`, `hostname`, and `interface` are
optional. `interface` is the value reported by dnsmasq for the lease hook; raw
environment variables are never forwarded through this API.

Each accepted action publishes one exact event topic:

| Action | Topic |
| --- | --- |
| `added` | `routerd.dhcp.lease.added` |
| `renewed` | `routerd.dhcp.lease.renewed` |
| `removed` | `routerd.dhcp.lease.removed` |

## Controller status

`Status.status.controllers` and the `Controllers` endpoint include both the
configured controller mode and runtime reconcile state. Runtime fields include
`interval`, `lastTrigger`, `lastReconcileTime`, `nextReconcileTime`,
`reconcileCount`, `reconcileErrorCount`, `consecutiveErrorCount`,
`currentError`, `lastDuration`, `maxDuration`, `averageDuration`, `lastError`,
`lastErrorTime`, and `lastErrorClearedAt`. `reconcileErrorCount` is cumulative;
use `currentError` and `consecutiveErrorCount` to decide whether the controller
is failing now. These fields are observational; clients should tolerate them
being absent before a controller has run.

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
