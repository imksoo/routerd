---
title: Development checks
---

# Development checks

![Diagram showing development checks split between local pre-commit tests, CI pull request validation, and release workflow archive publishing](/img/diagrams/operations-development.png)

routerd uses two separate automation paths.

- The CI workflow checks normal pushes and pull requests.
- The release workflow builds release archives and checksums after a release tag is pushed.

The release workflow is intentionally separate because it builds multiple
operating system and architecture archives and publishes GitHub Release assets.


## Current development rules

Follow [AGENTS.md](../../AGENTS.md) and the [routerd 1.1 MVP](../design/routerd11-mvp.md).
Use real targets and peers for operational checks. New or expanded tests require
specific owner approval, and mock, fake, and simulated environments are prohibited.
Keep raw logs and captures out of tracked source; remove task-owned temporary
evidence and caches after publishing the concise result.

Cloud SAM is shelved until a separate owner request and is not a blocker for
the current homert/k8s-rt/pve-rt work. Keep existing shared functionality and
do not create cloud resources or chargeable tests. Completed plans and old
audit verdicts are not a current backlog; consult them only in Git history
when investigating a present failure.

## CI workflow

`.github/workflows/ci.yaml` runs on branch pushes and pull requests.
The shared quality workflow checks Linux and FreeBSD builds, generated schemas,
example configurations, and shell scripts. The website build runs when the
website changes or the workflow explicitly requests it.

When a change touches `webconsole/`, its checked-in static assets, the shared
quality workflow, or the Makefile, CI also runs the Web Console-specific gate:
`npm ci`, high-severity audits for all and production dependencies, TypeScript
type-checking, a production build, and a generated-asset drift check.

The CI workflow does not publish release artifacts.
Release archives are created only by the `Release` workflow on date-based tags.

## Commit hook

Enable the required commit-message hook in each clone:

```sh
git config --local core.hooksPath .githooks
```

Review the actual diff and provide the four explanations required by
[AGENTS.md](../../AGENTS.md). The hook checks their presence, not their accuracy.
