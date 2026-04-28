# AGENTS.md

## Project

This repository implements `routerd`, a declarative router resource applier.

Current primary target:
- Ubuntu Server
- install layout under `/usr/local`
- Go 1.24+
- YAML config
- generated JSON Schema / OpenAPI for authoring and tool integration
- trusted local plugins
- systemd service
- local HTTP+JSON control API over a Unix domain socket
- `routerctl` client CLI

The implementation is pre-release. The MVP scope is a working boundary, not a
permanent constraint. If a small design change prevents obvious future migration
cost or improves router safety, propose it clearly and implement it after the
tradeoff is understood.

Current implemented scope includes:
- Interface alias resources and ownership/adoption planning
- IPv4 static and DHCP address resources
- DHCPv4 server scopes through managed dnsmasq
- DHCPv6/RA scopes through managed dnsmasq
- IPv6 DHCP client and prefix delegation through systemd-networkd drop-ins
- delegated IPv6 LAN address derivation
- self-address selection policies
- DNS conditional forwarding
- PPPoE interface rendering and systemd unit management
- DS-Lite tunnels and AFTR address selection
- IPv4 source NAT
- IPv4 policy routing and route sets
- IPv4 default route policy with health checks
- IPv4 reverse path filter resources
- Path MTU propagation and TCP MSS clamping
- minimal default-deny home-router firewall resources
- sysctl, hostname, NTP client, and log sink resources
- plugin protocol
- dry-run, plan, status JSON, and daemon apply
- local NAPT/conntrack inspection through `routerctl`

## API Groups

Use:
- `routerd.net/v1alpha1` for top-level Router config
- `net.routerd.net/v1alpha1` for network resources
- `firewall.routerd.net/v1alpha1` for firewall resources
- `system.routerd.net/v1alpha1` for local system resources
- `plugin.routerd.net/v1alpha1` for plugin manifests

Do not use placeholder groups such as `routerd.io`.

## Safety

Do not mutate the host network in normal unit tests.

Any test that changes network state must be isolated under:
- `tests/netns`
- explicit `sudo`
- clear documentation

Be careful when applying config to a remote router. Prefer this sequence:
- validate
- plan
- dry-run apply
- confirm the management path is not being removed
- apply
- verify service state and connectivity

Do not implement remote plugin install or a remote plugin registry yet.

## Commands

Prefer Makefile targets when available:

```sh
make test
make build
make check-schema
make validate-example
make dry-run-example
make website-build
```

Useful direct commands:

```sh
go test ./...
go build ./cmd/routerd
go build ./cmd/routerctl
routerd validate --config examples/router-lab.yaml
routerd plan --config examples/router-lab.yaml
routerd apply --config examples/router-lab.yaml --once --dry-run --status-file /tmp/routerd-status.json
routerctl status
routerctl show napt --limit 20
```

Default source-install paths:

- Config: `/usr/local/etc/routerd/router.yaml`
- Plugin dir: `/usr/local/libexec/routerd/plugins`
- Binary: `/usr/local/sbin/routerd`
- Client binary: `/usr/local/sbin/routerctl`

Linux runtime defaults are `/run/routerd` and `/var/lib/routerd`.

OS-specific defaults (paths, feature flags) live in `pkg/platform`. New
OS-specific behavior should consult `platform.Current()` rather than reading
`runtime.GOOS` directly, and renderers that depend on Linux-only host
integrations (netplan, systemd-networkd, nftables) should branch on the
matching `platform.Features` flag or a build tag rather than failing at
runtime on FreeBSD/NixOS.

NixOS and FreeBSD are second-tier targets. The build, install layout, and
service-manager integration scaffolds (`contrib/nix/` flake + module,
`contrib/freebsd/routerd` rc.d script) are in place, but pf and NixOS-native
interface renderers are still pending. Do not imply parity in user-facing
text — keep the language scoped to "groundwork" until the matching renderer
exists. The full matrix lives in `docs/platforms.md`.

## Coding Rules

- Keep changes scoped and explicit.
- Treat MVP boundaries as guidance, not as an excuse to bake in avoidable technical debt.
- Prefer early typed API shapes and generated machine-readable schema when resource fields are introduced.
- Prefer explicit code over clever abstractions.
- Keep plugin protocol documented.
- Subprocess execution must be wrapped so it can be tested.
- Config loading, validation, plugin discovery, dependency ordering, rendering, and apply behavior must have tests.
- The same apply code must work in daemon mode and one-shot CLI mode.
- Do not hardcode shell snippets in the core for resource-specific operations.
- Core orchestrates; renderers and resource-specific apply helpers perform concrete OS work.
- For firewall/NAT/routing changes, keep the default behavior conservative and add focused renderer tests.
- For dnsmasq, nftables, iproute2, pppd, or systemd syntax changes, validate against the real command when practical.

## Documentation

Keep these documents updated:
- `README.md`
- `README.ja.md`
- `docs/api-v1alpha1.md`
- `docs/api-v1alpha1.ja.md`
- `docs/resource-ownership.md`
- `docs/resource-ownership.ja.md`
- `docs/plugin-protocol.md`
- `docs/plugin-protocol.ja.md`
- `docs/control-api-v1alpha1.md`
- `docs/control-api-v1alpha1.ja.md`
- `docs/releases/changelog.md`
- website docs under `website/i18n/ja/docusaurus-plugin-content-docs/current/`

Whenever the plugin protocol changes, update plugin protocol docs.
Whenever the config schema changes, update API docs and regenerate schema:

```sh
make generate-schema
make check-schema
```

When control API types change, update control API docs and generated schemas.
When a resource kind starts managing a new host artifact, update resource
ownership docs and keep artifact intent tests covering the resource.

## Current Non-goals

Do not implement without a separate design discussion:
- remote plugin registry or remote plugin installation
- full rollback of all OS-level changes
- interactive config console
- built-in LLM assistant
- Proxmox lab automation
- general-purpose firewall rule language beyond the current minimal model
