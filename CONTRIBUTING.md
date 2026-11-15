# Contributing to routerd

routerd is pre-release router control-plane software. Contributions are
welcome, but changes that touch networking, firewalling, routing, installers,
or host service management must be reviewed carefully.

[AGENTS.md](AGENTS.md) holds the development rules for this repository,
including the rules on tests, real-target validation, and commit messages.
Where this file and AGENTS.md differ, AGENTS.md governs.

## Development setup

Use the Go version named in `go.mod`. The Makefile is for development tasks:

```sh
make check-schema
make validate-example
```

End users install from release archives with `install.sh`. Do not add a second
installer path to the Makefile.

If you changed shell scripts, also run:

```sh
shellcheck packaging/install.sh packaging/uninstall.sh scripts/*.sh
```

If you changed resource API fields, regenerate the schema and resource
reference:

```sh
make generate-schema
make generate-resource-docs
```

Enable the commit-message hook in each clone:

```sh
git config --local core.hooksPath .githooks
```

## Tests and validation

Do not add mocks, fakes, simulators, recorded responses, or substitute network
environments. Do not add or expand test code without the owner's prior approval
for that specific test. Validate behavior on the real targets and their real
peers, and state honestly what was not checked.

Provider executors mutate cloud control planes. Do not use chargeable cloud
runs as a debugging loop.

## Design expectations

- Keep YAML intuitive and explicit.
- Prefer a typed resource over hidden host state or ad hoc commands.
- Keep Linux and FreeBSD differences behind platform renderers.
- Do not add remote plugin installation or a plugin registry without a separate
  design discussion.

## Documentation

Update public docs when behavior changes:

- `README.md`
- `README.ja.md`
- `docs/` in all four languages

The Markdown under `docs/` is the source. Rendered HTML is not committed;
`website/build.sh` generates it when the site is published.

## Commit style

Use small commits with direct messages. Prefer one behavioral change per commit
when practical. Each commit message carries the Rule-Check, Scope-Check,
Validation, and Cleanup explanations described in AGENTS.md. Release commits
are created by `scripts/release.sh`.

## License

routerd is distributed under the BSD 3-Clause License. By contributing to this
repository, you agree that your contribution is provided under the same
license unless a file explicitly states otherwise.
