# Log Storage

![Diagram showing routerd log writers, platform-derived SQLite stores, retention, and read-only operational views](/img/diagrams/concept-log-storage.png)

routerd keeps long-lived state separate from operational logs.

The Linux default layout is:

| File | Purpose | Typical retention |
| --- | --- | --- |
| `/var/lib/routerd/routerd.db` | resource state plus event, access-log, and plugin-run log tables | events: 24 hours by default; explicit policy for other log tables |
| `/var/lib/routerd/dns-queries.db` | DNS query rows from `routerd-dns-resolver` | 30 days |
| `/var/lib/routerd/traffic-flows.db` | conntrack-derived traffic flows | 30 days |
| `/var/lib/routerd/firewall-logs.db` | firewall accept/drop/reject rows | 90 days |
| `/var/lib/routerd/dhcp-fingerprints.db` | DHCP client fingerprint observations | 30 days |

FreeBSD keeps the same database names under `/var/db/routerd`.

The log tables use column names that can be mapped to OpenTelemetry log
attributes. nDPI and TLS SNI columns are reserved in `traffic-flows.db`, even
when no writer fills them yet.

The event journal has an always-on safety bound even when no `LogRetention`
resource is configured. routerd keeps at most 24 hours, 100,000 rows, and 64
MiB of logical event payload; the first limit reached wins. Old events are
removed in batches and state tables are never included. This bound protects
small Live ISO COW filesystems and is not a substitute for archival retention.

`LogRetention` is the configurable local SQLite retention policy. It removes old rows
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
routerctl doctor disk
```

If SQLite reports a full filesystem, routerd keeps locally-derived runtime
events flowing without requiring their forensic journal write to succeed. It
marks the overall status `Degraded`, exposes `EventJournalReadOnly` in runtime
statistics and `routerctl doctor`, writes
`/run/routerd/storage-critical.json`, and updates `systemctl status routerd`.
For a volatile Live ISO overlay, reboot the router OS to clear the COW layer,
then verify the configured retention. On persistent storage, rebooting alone
does not free space: prune or compact the journal, or expand/move the state
filesystem.
