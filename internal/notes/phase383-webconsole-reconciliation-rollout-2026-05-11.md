# Phase 3.8.3 Web Console reconciliation rollout validation

Date: 2026-05-11
Host: homert02 (192.168.123.129)

## Change

- Added page-level scroll snapshots around refresh/reconciliation.
- Added per-container scroll keys to Resources, Controllers, Connections, Events, VPN, Firewall, Client traffic/leases, Generations, and diff/config panels.
- Preserved filter/search/sort state through refresh because state remains local and summary data is reconciled by stable keys.
- Removed non-actionable peer/client badge strips from Clients and VPN. The same information remains in structured sections/tables where it can be scanned with context.

## Validation

- npm run typecheck: OK
- npm run build: OK
- go test ./...: OK
- make check-schema validate-example: OK
- make build-daemons check-linux-static: OK
- make build-daemons-freebsd: OK
- homert02 routerd restart: active
- homert02 routerctl status: Healthy, generation 53, resourceCount 89

## Browser evidence

- Firewall vertical scroll preserved across refresh: beforeY=650 afterY=650.
- Generations filter state preserved across refresh: query remained `Healthy`.
- Connections currently fit in the viewport on homert02, so there is no vertical scroll to preserve for that data set.
- Clients and VPN screenshots confirm the non-actionable badge strips are gone.

Screenshots:

- /tmp/webconsole-screenshots/phase383/01-connections-scroll-filter.png
- /tmp/webconsole-screenshots/phase383/02-firewall-scroll-filter.png
- /tmp/webconsole-screenshots/phase383/03-clients-no-badge-strip.png
- /tmp/webconsole-screenshots/phase383/04-vpn-no-peer-strip.png
- /tmp/webconsole-screenshots/phase383/05-generations-scroll-filter.png
