# Log Storage

![Diagram showing routerd log writers, platform-derived SQLite stores, retention, and read-only operational views](/img/diagrams/concept-log-storage.png)

routerd keeps long-lived state separate from operational logs.

The Linux default layout is:

| File | Purpose | Typical retention |
| --- | --- | --- |
| `/var/lib/routerd/routerd.db` | resource state plus event, access-log, and plugin-run log tables | 30 days for log tables |
| `/var/lib/routerd/dns-queries.db` | DNS query rows from `routerd-dns-resolver` | 30 days |
| `/var/lib/routerd/traffic-flows.db` | conntrack-derived traffic flows | 30 days |
| `/var/lib/routerd/firewall-logs.db` | firewall accept/drop/reject rows | 90 days |
| `/var/lib/routerd/dhcp-fingerprints.db` | DHCP client fingerprint observations | 30 days |

FreeBSD keeps the same database names under `/var/db/routerd`.

The log tables use column names that can be mapped to OpenTelemetry log
attributes. nDPI and TLS SNI columns are reserved in `traffic-flows.db`, even
when no writer fills them yet.

`LogRetention` is the sole local SQLite retention policy. It removes old rows
by signal and can run SQLite incremental vacuum. It no longer exposes database
paths or per-writer retention in user config; routerd derives the event, DNS
query, traffic flow, firewall event, DHCP fingerprint, access-log, and
plugin-run stores from the
registered log-table catalogue. New operational log tables must register their
signal and timestamp column in that catalogue before they can be retained.

`dhcp-sticky.db`, lease replication data, configuration generations, and
federation state are operational state, not logs. They are intentionally not
eligible for `LogRetention`, because deleting them can change router behaviour.

Specify `LogRetention.spec.sinks` to archive rows to named `LogSink` resources
before local deletion. A failed archive prevents deletion, so the local copy is
not removed before the external destination acknowledges it.

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
    - accessLogs
    - pluginRuns
    - dnsQueries
    - trafficFlows
    - dhcpFingerprints
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
