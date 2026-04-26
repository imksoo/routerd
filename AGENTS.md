# AGENTS.md

## Project

This repository implements `routerd`, a declarative router resource reconciler.

Initial target:
- Ubuntu Server
- install layout under `/usr/local`
- Go
- YAML config
- external trusted local plugins
- systemd service

MVP scope is intentionally small:
- Interface alias resources
- IPv4 static address resource
- IPv4 DHCP resource stub or minimal implementation
- plugin protocol
- dry-run
- status JSON
- systemd service example

## API Groups

Use:
- `routerd.net/v1alpha1` for top-level Router config
- `net.routerd.net/v1alpha1` for network resources
- `plugin.routerd.net/v1alpha1` for plugin manifests

Do not use placeholder groups such as `routerd.io`.

## Safety

Do not mutate the host network in normal unit tests.

Any test that changes network state must be isolated under:
- `tests/netns`
- explicit `sudo`
- clear documentation

Do not implement remote plugin install in the MVP.

## Commands

Use these commands when available:

```sh
go test ./...
go build ./cmd/routerd
routerd validate --config examples/basic-static.yaml
routerd reconcile --config examples/basic-static.yaml --once --dry-run
```

Default install paths should stay friendly to both Linux source installs and future FreeBSD ports:

- Config: `/usr/local/etc/routerd/router.yaml`
- Plugin dir: `/usr/local/libexec/routerd/plugins`
- Binary: `/usr/local/sbin/routerd`

Linux runtime defaults are `/run/routerd` and `/var/lib/routerd`.
FreeBSD runtime defaults are `/var/run/routerd` and `/var/db/routerd`.

If a Makefile exists, prefer:

```sh
make test
make build
make validate-example
make dry-run-example
```

## Coding Rules

- Keep the first implementation small.
- Do not implement firewall or NAT until the core resource/plugin loop works.
- Prefer explicit code over clever abstractions.
- Keep plugin protocol documented.
- Subprocess execution must be wrapped so it can be tested.
- Config loading, validation, plugin discovery, dependency ordering, and reconcile must have tests.
- The same reconcile code must work in daemon mode and one-shot CLI mode.
- Do not hardcode shell snippets in the core for resource-specific operations.
- Core orchestrates; plugins act.

## Documentation

Keep these documents updated:
- `docs/api-v1alpha1.md`
- `docs/plugin-protocol.md`
- `README.md`

Whenever the plugin protocol changes, update `docs/plugin-protocol.md`.
Whenever the config schema changes, update `docs/api-v1alpha1.md`.

## Non-goals for MVP

Do not implement:
- firewall
- NAT
- DS-Lite
- IPv6 PD
- PBR
- config console
- LLM assistant
- remote plugin registry
- full rollback
- Proxmox lab automation
