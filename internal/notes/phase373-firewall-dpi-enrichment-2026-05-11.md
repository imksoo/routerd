# Phase 3.7.3 Firewall DPI enrichment validation

Date: 2026-05-11

## Scope

- Structured DPI columns were added to firewall log storage.
- Linux NFLOG and FreeBSD pflog inputs now enrich deny entries through `routerd-dpi-classifier`.
- Web Console Firewall views include a DPI column and query search coverage.
- `routerd.firewall.deny.total` is emitted with a `network.protocol.name` label derived from DPI when available.

## Local validation

- `make check-schema`: passed.
- `make validate-example`: passed.
- `go test ./...`: passed.
- Linux amd64 daemon build: passed with static binary check.
- FreeBSD amd64 daemon build: passed.

## homert02 validation

- Deployed latest `routerd`, `routerctl`, `routerd-firewall-logger`, and `routerd-dpi-classifier`.
- Restarted `routerd-dpi-classifier.service`, `routerd-firewall-logger.service`, and `routerd.service`.
- `routerctl status`: Healthy.
- IPv4 outbound smoke: `curl -4 https://www.google.com/generate_204` returned 204.
- Web API `/api/v1/summary` returned firewall rows with structured DPI fields.
- Evidence row: `dpiApp=tls`, `dpiTlsSNI=routerd-firewall-selftest.example`.

## Lab blockers

- router02: SSH by address rejects authentication and DNS name timed out during this phase.
- router04: host key changed, and authentication failed after known_hosts refresh.
- Code paths for NixOS synthesized systemd units and FreeBSD synthesized rc.d services are covered by renderer tests and local builds. Runtime apply on router02/router04 is pending access restoration.
