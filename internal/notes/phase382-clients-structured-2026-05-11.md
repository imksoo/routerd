# Phase 3.8.2 Web Console Clients structured view validation

Date: 2026-05-11
Host: homert02 (192.168.123.129)

## Change

- Replaced the flat Clients inventory table with OS-family sections.
- Added online/offline ordering per section.
- Added collapsed one-line device rows with expandable address/fingerprint details.
- Preserved existing search/filter input path because the view still receives the filtered client list.

## Validation

- npm run typecheck: OK
- npm run build: OK
- go test ./...: OK
- make check-schema validate-example: OK
- make build-daemons check-linux-static: OK
- make build-daemons-freebsd: OK
- homert02 routerd restart: active
- homert02 routerctl status: Healthy, generation 53, resourceCount 89

## Screenshots

- /tmp/webconsole-screenshots/phase382/01-clients-structured.png
- /tmp/webconsole-screenshots/phase382/02-clients-expanded.png
