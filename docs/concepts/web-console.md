# Web Console

`WebConsole` enables a read-only HTTP view for routerd. It is intended for
local operations on a management network. It does not change configuration,
restart services, apply resources, or edit the state database.

Configuration changes remain limited to YAML files and `routerctl` commands.
The browser is for observation only.

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: WebConsole
metadata:
  name: mgmt
spec:
  enabled: true
  listenAddressFrom:
    resource: Interface/mgmt
    field: ipv4Addresses
  port: 8080
  title: edge-router
```

Keep the listener on a management address. Do not expose it on an untrusted
WAN interface. Use `listenAddressFrom` when the management address is owned by
the operating system or IPAM. The value is resolved from resource status at
startup, and `listenAddress` remains available for a literal fallback address.

## What the console reads

The Web Console reads:

- routerd daemon status
- resource status in the SQLite state database
- bus events in the SQLite event table
- live connection observations from conntrack or pf state
- DNS query history from `dns-queries.db`
- traffic flow history from `traffic-flows.db`
- firewall deny history from `firewall-logs.db`
- current dnsmasq DHCP lease files for client names, MAC addresses, and local
  vendor hints
- the active YAML configuration, shown read-only

## Current screens

The current Fluent UI web app provides:

- a status overview for PD, DS-Lite, DNS, NAT, routes, health checks, VPN,
  packages, sysctl, systemd units, and log resources
- resource change highlighting when a phase or observed value changes
- an Events view with a selectable detail pane, so large attributes do not
  crowd the event table
- DHCP lease event details, including MAC address, IP address, hostname, and
  resource names when present
- a Connections view grouped by family and protocol, with filtering, sorting,
  pagination, and page size controls
- DNS query, traffic flow, and firewall log views backed by separate log
  databases
- dedicated BGP, VRRP, and IngressService operational pages at `/bgp`, `/vrrp`,
  and `/ingress`. These pages use Server-Sent Events to refresh resource
  tables, keep a local 5/15/60 minute SVG trend, and show a filtered event log
  for the relevant resources.
- Firewall rows combine firewall logs, DNS answers, DHCP leases, MAC vendor
  hints, and current conntrack reply tuples. This helps explain whether a
  denied packet is unsolicited traffic or an off-path reply for an existing
  NAT mapping.
- a Config view with a structured, foldable YAML tree and a raw YAML fallback

Connection rows show the forward direction by default. Return-path data is not
shown as a separate primary row because conntrack commonly reports the same
conversation from both directions.

## API boundary

Web Console APIs are read-only. The JSON endpoints are under `/api/v1`; the
SSE stream is also available through the short `/api/events/stream` alias.

| Path | Content |
| --- | --- |
| `/api/v1/summary` | status, resource phases, recent events, and connection summary |
| `/api/v1/resources` | resource statuses from the state database |
| `/api/v1/events?limit=200&resourceKind=&resourceName=&q=` | recent bus events with optional filters |
| `/api/v1/events/stream` or `/api/events/stream` | Server-Sent Events stream for `routerd.*` bus events |
| `/api/v1/connections` | live connection observation from conntrack or pf state |
| `/api/v1/dns-queries?since=1h&client=&qname=&limit=100` | DNS query log rows |
| `/api/v1/traffic-flows?since=1h&client=&peer=&limit=100` | traffic flow log rows with DNS-derived hostnames |
| `/api/v1/firewall-logs?since=24h&action=drop&src=&limit=100` | firewall log rows |
| `/api/v1/bgp`, `/api/v1/vrrp`, `/api/v1/ingress` | filtered operational status for Kubernetes-edge routing and VIP resources |
| `/api/v1/config` | active YAML configuration |
| `/api/v1/generations?limit=100` | completed apply generations and whether a YAML snapshot is stored |
| `/api/v1/generations/<id>/config` | stored YAML for one apply generation |
| `/api/v1/generations/<from>/diff/<to>` | unified diff between two stored YAML generations |
