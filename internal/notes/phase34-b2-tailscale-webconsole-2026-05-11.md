# Phase 3.4 B2: Tailscale runtime status and Web Console VPN visibility

Date: 2026-05-11

## Scope

- Surface Tailscale runtime status from `tailscale status --json` in `TailscaleNode` object status.
- Keep explicit `SystemdUnit` resources from suppressing Tailscale runtime observation.
- Show Tailnet name, MagicDNS suffix, certificate domains, Tailscale IPs, allowed routes, and peer count in the Web Console VPN view.
- Make `routerctl describe` fall back to controller-managed ObjectStatus for resources that are not directly host-observable.

## homert02 validation

Host: `imksoo@192.168.123.129`

Commands executed:

```sh
make build-daemons
make check-linux-static
rsync -a --delete bin/linux/ imksoo@192.168.123.129:/tmp/routerd-b2-bin/
ssh imksoo@192.168.123.129 'sudo install -m 0755 /tmp/routerd-b2-bin/* /usr/local/sbin/ && sudo systemctl restart routerd.service'
ssh imksoo@192.168.123.129 'sudo /usr/local/sbin/routerctl status --json | jq -r ".status.phase + \" resources=\" + (.status.resourceCount|tostring)"'
ssh imksoo@192.168.123.129 'sudo /usr/local/sbin/routerctl describe tailscale/homert02 | sed -n "1,90p"'
ssh imksoo@192.168.123.129 'curl -fsS http://192.168.123.129:8080/api/v1/vpn | jq -r ".tailscale.backendState + \" tailnet=\" + .tailscale.tailnetName + \" peers=\" + ((.tailscale.peers|length)|tostring)"'
```

Result:

- `routerctl status`: `Healthy resources=88`
- `routerctl describe tailscale/homert02`: `Currently observable: yes`, `backendState: Running`, `tailnetName: kajiya.takeshi@gmail.com`, `magicDNSSuffix: taileffd2.ts.net`, `peerCount: 7`
- `/api/v1/vpn`: `Running tailnet=kajiya.takeshi@gmail.com peers=7`

## Local verification

- `go test ./cmd/routerctl ./pkg/controller/chain ./pkg/webconsole ./pkg/render ./pkg/config`
- `cd webconsole && npm run build`
- `make build-daemons`
- `make check-linux-static`
- `make check-schema`
- `make test`
