---
title: Contributing
---

# Contributing

routerd is pre-release router control-plane software. Contributions are
welcome, but changes that touch networking, firewalling, routing, installers,
or host service management need careful review.

The canonical contributor guide is in the repository root:

- [`CONTRIBUTING.md`](https://github.com/imksoo/routerd/blob/main/CONTRIBUTING.md)

## Local checks

Before opening a pull request, run:

```sh
make test
make check-schema
make validate-example
make website-build
```

When shell scripts change, also run `shellcheck` against the changed scripts.

## Pre-commit hook

routerd ships an optional pre-commit hook:

```sh
cp scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

The hook runs Go tests and schema checks before a commit.

## License

routerd is distributed under the BSD 3-Clause License:

```text
Copyright (c) 2026 Kirino Minato <kirino.minato@gmail.com> (https://github.com/imksoo) and routerd contributors
```

By contributing to this repository, you agree that your contribution is provided
under the same license unless a file explicitly states otherwise.
