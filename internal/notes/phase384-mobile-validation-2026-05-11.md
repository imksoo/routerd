# Phase 3.8.4 Web Console mobile validation

Date: 2026-05-11
Host: homert02 (192.168.123.129)
Viewport: 375x812 mobile/touch

## Change

- Confirmed Phase 3.8 Web Console changes on mobile viewport.
- Fixed lightweight DPI fallback so NBNS/NetBIOS first-level encoded names are not mislabeled as DNS queries.
  - Example encoded `EMEBEJEOCACACACACACACACACACACABN` now decodes as `NBNS-query=LAIN<0x01>`.
  - Detection is payload-format based (NB/NBSTAT qtype + IN class), not a hard port-137 special case.
- Web Console renders `NBNS-query=` when `dpi.app=netbios`.

## Validation

- go test ./pkg/dpi ./cmd/routerd-dpi-classifier ./cmd/routerd-firewall-logger: OK
- npm run typecheck: OK
- npm run build: OK
- go test ./...: OK
- make check-schema validate-example: OK
- make build-daemons check-linux-static: OK
- make build-daemons-freebsd: OK
- homert02 routerd restart: active
- homert02 routerctl status: Healthy, generation 53, resourceCount 89

## Mobile evidence

No horizontal overflow at 375px:

- Clients: clientWidth=375, scrollWidth=375
- Firewall: clientWidth=375, scrollWidth=375
- VPN: clientWidth=375, scrollWidth=375
- Generations: clientWidth=375, scrollWidth=375
- Overview: clientWidth=375, scrollWidth=375

Scroll preservation:

- Mobile Firewall refresh: beforeY=600 afterY=600

Screenshots:

- /tmp/webconsole-screenshots/phase384/01-mobile-clients.png
- /tmp/webconsole-screenshots/phase384/02-mobile-firewall.png
- /tmp/webconsole-screenshots/phase384/03-mobile-vpn.png
- /tmp/webconsole-screenshots/phase384/04-mobile-generations.png
- /tmp/webconsole-screenshots/phase384/05-mobile-overview.png
