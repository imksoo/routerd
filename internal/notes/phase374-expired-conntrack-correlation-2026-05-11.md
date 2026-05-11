# Phase 3.7.4 expired conntrack correlation

Date: 2026-05-11

## Scope

- Add a short-retention expired flow ring to `firewall-logs.db`.
- Observe Linux conntrack `DESTROY` events and FreeBSD pf state disappearance.
- Correlate deny events with recently expired reverse flows.
- Surface `orphan_return` vs `true_suspicious` in API and Web Console.
- Add an OTel deny metric attribute for `routerd.firewall.correlation`.

## Local verification

- `go test ./pkg/logstore ./cmd/routerd-firewall-logger`
- `npm run typecheck`
- `npm run build`
- `make generate-schema`
- `make check-schema`
- `go test ./...`
- `make validate-example`
- `make build-daemons check-linux-static`
- `make build-daemons-freebsd`

## homert02 validation

Updated binaries:

- `/usr/local/sbin/routerd`
- `/usr/local/sbin/routerctl`
- `/usr/local/sbin/routerd-firewall-logger`

Services after restart:

- `routerd.service`: active
- `routerd-firewall-logger.service`: active
- `routerd-dpi-classifier.service`: active

`routerctl status`:

- phase: `Healthy`
- generation: `53`
- resourceCount: `89`

Selftest command:

```sh
sudo /usr/local/sbin/routerd-firewall-logger selftest \
  --path /var/lib/routerd/firewall-logs.db \
  --dpi-socket /run/routerd/dpi-classifier/default.sock
```

API evidence from `http://192.168.123.129:8080/api/v1/summary`:

```json
{
  "ruleName": "selftest-orphan-return",
  "action": "drop",
  "srcAddress": "198.51.100.10",
  "dstAddress": "192.0.0.2",
  "protocol": "tcp",
  "correlation": "orphan_return",
  "correlationDetail": "likely orphan return from expired conn (orig: 172.18.0.10:53168 -> 198.51.100.10:443, expired 30s ago, 4.0KiB transferred)",
  "expiredAgeSeconds": 30,
  "expiredBytes": 4096
}
```

Recent live WAN drops showed:

```json
{
  "ruleName": "routerd firewall wan-to-self deny",
  "action": "drop",
  "protocol": "udp",
  "correlation": "true_suspicious",
  "correlationDetail": "no expired reverse flow match"
}
```

Production traffic check:

- `curl -4 https://www.google.com/generate_204`: HTTP 204

## Deferred backfill

router02 and router04 SSH/access blockers are deferred to the Phase 3.7 final backfill per user instruction.
