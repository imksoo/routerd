# Phase 3.4 B3: Web Console information design pass

Date: 2026-05-11

## Scope

- Added a top section bar for pages that contain multiple logical sections.
- Kept the left navigation, but made the active sub-section visible outside the sidebar as well.
- Added URL-hash-linked section buttons for Overview, Controllers, Clients, Connections, VPN, Events, and Firewall views.
- Added Overview sub-sections for metrics, interfaces, and resources.
- Added Events sub-sections for list and detail panels.

This is intentionally a small information architecture pass. It avoids a full redesign during Phase 3.4.

## homert02 validation

Host: `imksoo@192.168.123.129`

Commands executed:

```sh
cd webconsole && npm run build
go test ./pkg/webconsole
make check-schema
make build-daemons
make check-linux-static
rsync -a --delete bin/linux/ imksoo@192.168.123.129:/tmp/routerd-b3-bin/
ssh imksoo@192.168.123.129 'sudo install -m 0755 /tmp/routerd-b3-bin/* /usr/local/sbin/ && sudo systemctl restart routerd.service'
ssh imksoo@192.168.123.129 'sudo /usr/local/sbin/routerctl status --json | jq -r ".status.phase + \" resources=\" + (.status.resourceCount|tostring)"'
ssh imksoo@192.168.123.129 'curl -fsSI http://192.168.123.129:8080/ | head -5'
```

Result:

- `routerctl status`: `Healthy resources=88`
- Web Console root: `HTTP/1.1 200 OK`
