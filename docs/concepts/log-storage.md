# Log Storage

routerd keeps long-lived state separate from operational logs.

The default layout is:

| File | Purpose | Typical retention |
| --- | --- | --- |
| `/var/lib/routerd/routerd.db` | resource state and event table | 30 days for events |
| `/var/lib/routerd/dns-queries.db` | DNS query rows from `routerd-dns-resolver` | 30 days |
| `/var/lib/routerd/traffic-flows.db` | conntrack-derived traffic flows | 30 days |
| `/var/lib/routerd/firewall-logs.db` | firewall accept/drop/reject rows | 90 days |

The log tables use column names that can be mapped to OpenTelemetry log
attributes. nDPI and TLS SNI columns are reserved in `traffic-flows.db`, even
when no writer fills them yet.

`LogRetention` removes old rows and can run SQLite incremental vacuum:

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: LogRetention
metadata:
  name: default
spec:
  schedule: daily
  incrementalVacuum: true
  targets:
    - file: /var/lib/routerd/routerd.db
      retention: 30d
    - file: /var/lib/routerd/dns-queries.db
      retention: 30d
    - file: /var/lib/routerd/traffic-flows.db
      retention: 30d
    - file: /var/lib/routerd/firewall-logs.db
      retention: 90d
```

Inspection commands:

```sh
routerctl dns-queries --since 1h
routerctl traffic-flows --since 1h
routerctl firewall-logs --since 24h --action drop
```
