# Contributing to routerd

routerd is pre-release router control-plane software. Contributions are
welcome, but changes that touch networking, firewalling, routing, installers,
or host service management must be reviewed carefully.

## Development setup

Use Go 1.24 or newer. The Makefile is for development tasks:

```sh
make test
make check-schema
make validate-example
make website-build
```

End users install from release archives with `install.sh`. Do not add a second
installer path to the Makefile.

## Before opening a pull request

Run:

```sh
make test
make check-schema
make validate-example
make website-build
```

If you changed shell scripts, also run:

```sh
shellcheck packaging/install.sh packaging/uninstall.sh scripts/*.sh
```

If you changed resource API fields, regenerate schema:

```sh
make generate-schema
```

Enable the optional pre-commit hook if you want the local safety checks to run
before every commit:

```sh
cp scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

## Design expectations

- Keep YAML intuitive and explicit.
- Prefer a typed resource over hidden host state or ad hoc commands.
- Keep Linux, NixOS, and FreeBSD differences behind platform renderers.
- Do not mutate host networking in normal unit tests.
- Use isolated network namespace tests for Linux network changes.
- Do not add remote plugin installation or a plugin registry without a separate
  design discussion.

## Documentation

Update public docs when behavior changes:

- `README.md`
- `README.ja.md`
- `docs/`
- `website/i18n/ja/docusaurus-plugin-content-docs/current/`

Use `internal/notes/` for lab-specific validation notes. Do not put lab notes
under `docs/`, because the website publishes them.

## Commit style

Use small commits with direct messages. Prefer one behavioral change per commit
when practical. Release commits are created by `scripts/release.sh`.

## License

routerd is distributed under the BSD 3-Clause License. By contributing to this
repository, you agree that your contribution is provided under the same
license unless a file explicitly states otherwise.
