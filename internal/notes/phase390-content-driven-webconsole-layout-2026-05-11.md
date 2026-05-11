# Phase 3.9 content-driven Web Console layout validation

Date: 2026-05-11

## Scope

- Removed fixed desktop shell/sidebar/table sizing in the Web Console.
- Switched the application shell, navigation, card grids, filters, and tables to `clamp()`, `rem`, `min()`, `max-content`, and content-driven grid tracks.
- Kept the shell bounded but wide enough on desktop so table-heavy pages can use available space instead of overflowing inside narrow wrappers.
- Moved Generations Diff/YAML results above the long table and scroll/focus the result card after View/Diff actions.
- Removed the repeated per-row `rollback CLI only` badge from Generations actions.
- Added a VS Code-style Diff overview ruler that marks added/removed regions on the right edge of the diff panel.
- Kept mobile navigation behavior and table wrapper overflow local to each table.

## homert02 validation

- Host: `homert02` (`192.168.123.129`)
- Services restarted: `routerd.service`, `routerd-dpi-classifier.service`, `routerd-firewall-logger.service`
- Status: `Healthy`
- Generation: `53`
- Resource count: `89`

## Width comparison

Before screenshots and metrics:

- `/tmp/webconsole-screenshots/phase390-before/`

After screenshots and metrics:

- `/tmp/webconsole-screenshots/phase390-after/`

Measured layout width:

| Viewport | Before main | Before sidebar | After main | After sidebar | Horizontal overflow |
| --- | ---: | ---: | ---: | ---: | --- |
| 1440x900 | 1192px | 248px | 1130px | 224px | false |
| 1280x800 | not captured | not captured | 998px | 205px | false |
| 1920x1080 | not captured | not captured | 1312px | 224px | false |
| 375x812 | 375px | 375px | 375px | 375px | false |

Generations action check:

- `Diff` action creates `#generation-result`, scrolls it to viewport top, and focuses it.
- `View` action creates `#generation-result`, scrolls it to viewport top, and focuses it.
- `Diff prev` shows overview ruler markers for changed regions.
- The repeated `rollback CLI only` text is absent from the Generations table.
- Evidence: `/tmp/webconsole-screenshots/phase390-after/generations-action-scroll.json`
- Screenshots:
  - `/tmp/webconsole-screenshots/phase390-after/desktop-1440-generations-action-result.png`
  - `/tmp/webconsole-screenshots/phase390-after/desktop-1440-generations-view-result.png`
  - `/tmp/webconsole-screenshots/phase390-after/desktop-1440-generations-final.png`
  - `/tmp/webconsole-screenshots/phase390-after/desktop-1440-generations-diff-prev-ruler.png`

All ten views were captured after deployment:

- Overview
- Clients
- Resources
- Controllers
- Connections
- VPN
- Events
- Firewall
- Config
- Generations

## Checks

- `npm run typecheck`
- `npm run build`
- `go test ./...`
- `make check-schema validate-example`
- `make build-daemons check-linux-static`
- `make build-daemons-freebsd`
