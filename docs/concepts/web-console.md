# Web Console

`WebConsole` enables a read-only HTTP view for routerd. It is intended for
local operations on a management network. It does not change configuration,
restart services, apply resources, or edit the state database.

Configuration changes remain limited to YAML files and `routerctl` commands.
The web console only reads:

- routerd daemon status
- resource status in the SQLite state database
- bus events in the SQLite event table
- conntrack / NAPT observations
- DNS query history from `dns-queries.db`
- traffic flow history from `traffic-flows.db`
- firewall deny history from `firewall-logs.db`

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

The first screen shows:

- overall routerd phase and generation
- resource phases for PD, DS-Lite, DNS, NAT, routes, health checks, VPN, and firewall resources
- recent routerd events
- conntrack count and sampled NAPT entries
- a `dst label` column for conntrack rows, derived from recent DNS answers
- client traffic totals from recent flow history
- recent firewall denies grouped by source and destination

The JSON endpoints are also read-only:

| Path | Content |
| --- | --- |
| `/api/summary` | status, resource phases, recent events, and NAPT summary |
| `/api/resources` | resource statuses from the state database |
| `/api/events` | recent bus events |
| `/api/napt` | conntrack / NAPT observation |
| `/api/dns-queries?since=1h&client=&qname=&limit=100` | DNS query log rows |
| `/api/traffic-flows?since=1h&client=&peer=&limit=100` | traffic flow log rows with DNS-derived hostnames |
| `/api/firewall-logs?since=24h&action=drop&src=&limit=100` | firewall log rows |
