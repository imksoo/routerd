# Phase 3.8.1 Web Console SSE reconciliation validation

Date: 2026-05-11

Scope:
- Added Web Console SSE endpoint at `/api/v1/events/stream` and `/v1/events/stream`.
- Wired the routerd controller bus into Web Console when the controller chain is active.
- Added EventSource subscription in the Web Console. Router events trigger throttled refreshes, with a 30s polling fallback.
- Added key-based reconciliation for summary arrays so unchanged rows keep object identity across refreshes.

Local verification:
- `go test ./pkg/webconsole`
- `go test ./...`
- `npm run typecheck`
- `npm run build`
- `make check-schema validate-example`
- `make build-daemons check-linux-static`
- `make build-daemons-freebsd`

homert02 validation:
- Deployed static Linux `routerd` and `routerctl`.
- `routerd.service` stayed active.
- `routerctl status` stayed Healthy, generation 53, resourceCount 89.
- `curl -N http://192.168.123.129:8080/api/v1/events/stream` returned an SSE `connected` event.
- Playwright confirmed the Web Console shows `Live updates`.
- Refresh did not change scroll position in the captured view.

Screenshots:
- `/tmp/webconsole-screenshots/phase381/01-clients-live-updates.png`
- `/tmp/webconsole-screenshots/phase381/02-scroll-after-refresh.png`
- `/tmp/webconsole-screenshots/phase381/03-firewall-live-updates.png`

