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
  listenAddress: 192.168.123.129
  port: 8080
  title: homert02
```

Keep the listener on a management address. Do not expose it on an untrusted
WAN interface.

## What the console reads

The Web Console reads:

- routerd daemon status
- resource status in the SQLite state database
- bus events in the SQLite event table
- live connection observations from conntrack or pf state
- DNS query history from `dns-queries.db`
- traffic flow history from `traffic-flows.db`
- firewall deny history from `firewall-logs.db`
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
- a Connections view grouped by family and protocol, with pagination and page
  size controls
- DNS query, traffic flow, and firewall log views backed by separate log
  databases
- a Config view with syntax-highlighted, foldable, read-only YAML

Connection rows show the forward direction by default. Return-path data is not
shown as a separate primary row because conntrack commonly reports the same
conversation from both directions.

## API boundary

Web Console APIs are read-only and exposed only under `/api/v1`.

| Path | Content |
| --- | --- |
| `/api/v1/summary` | status, resource phases, recent events, and connection summary |
| `/api/v1/resources` | resource statuses from the state database |
| `/api/v1/events` | recent bus events |
| `/api/v1/connections` | live connection observation from conntrack or pf state |
| `/api/v1/dns-queries?since=1h&client=&qname=&limit=100` | DNS query log rows |
| `/api/v1/traffic-flows?since=1h&client=&peer=&limit=100` | traffic flow log rows with DNS-derived hostnames |
| `/api/v1/firewall-logs?since=24h&action=drop&src=&limit=100` | firewall log rows |
| `/api/v1/config` | active YAML configuration |
