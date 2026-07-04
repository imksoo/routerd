# Log Storage

![Diagram showing routerd log writers, platform-derived SQLite stores, retention, and read-only operational views](/img/diagrams/concept-log-storage.png)

routerd keeps long-lived state separate from operational logs.

The Linux default layout is:

| File | Purpose | Typical retention |
| --- | --- | --- |
| `/var/lib/routerd/routerd.db` | resource state and event table | 30 days for events |
| `/var/lib/routerd/dns-queries.db` | DNS query rows from `routerd-dns-resolver` | 30 days |
| `/var/lib/routerd/traffic-flows.db` | conntrack-derived traffic flows | 30 days |
| `/var/lib/routerd/firewall-logs.db` | firewall accept/drop/reject rows | 90 days |

FreeBSD keeps the same database names under `/var/db/routerd`.

The log tables use column names that can be mapped to OpenTelemetry log
attributes. nDPI and TLS SNI columns are reserved in `traffic-flows.db`, even
when no writer fills them yet.

`LogRetention` removes old rows by signal and can run SQLite incremental
vacuum. It no longer exposes database paths in user config; routerd derives the
event, DNS query, traffic flow, and firewall event stores from the resources
that produce those logs.

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: LogRetention
metadata:
  name: default
spec:
  retention: 30d
  schedule: daily
  vacuum: true
  signals:
    - events
    - dnsQueries
    - trafficFlows
  sinks:
    - LogSink/local-syslog
---
apiVersion: system.routerd.net/v1alpha1
kind: LogRetention
metadata:
  name: firewall-events
spec:
  retention: 90d
  schedule: daily
  vacuum: true
  signals:
    - firewallEvents
```

Inspection commands:

```sh
routerctl get dns-queries --limit 100
routerctl get traffic-flows --limit 100
routerctl get firewall-logs --limit 100
```
