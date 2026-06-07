---
title: Development checks
---

# Development checks

![Diagram showing development checks split between local pre-commit tests, CI pull request validation, and release workflow archive publishing](/img/diagrams/operations-development.png)

routerd uses two separate automation paths.

- The CI workflow checks normal pushes and pull requests.
- The release workflow builds signed release archives after a release tag is pushed.

The release workflow is intentionally separate because it builds multiple
operating system and architecture archives and publishes GitHub Release assets.

## CI workflow

`.github/workflows/ci.yaml` runs on branch pushes and pull requests.
It uses an Ubuntu runner and checks the development surface that should stay
green before review:

```sh
go test ./...
make check-schema
make validate-example
make website-build
```

The CI workflow does not publish release artifacts.
Release archives are created only by the `Release` workflow on date-based tags.

## Pre-commit hook

The repository includes an optional pre-commit hook script:

```sh
ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x scripts/pre-commit.sh
```

After enabling it, `git commit` runs:

```sh
go test ./...
make check-schema
```

If either command fails, the commit is stopped.
This catches schema drift and test failures before they reach CI.

For an emergency local commit, set this environment variable:

```sh
ROUTERD_SKIP_PRE_COMMIT=1 git commit
```

Use that only when the follow-up fix is already clear.
CI still runs after the branch is pushed.
