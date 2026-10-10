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


## Mandatory test and harness design rules

These rules apply to unit tests, offline fixtures, runtime smoke, release qualification and reusable old helpers, templates and copy sources, including code without a current caller. Every test change must identify its product requirement, required direct evidence and observation limits.

- Preserve observations and diagnostic logs; separate them from product acceptance. Journal text, progress counters, latency statistics and whole-host diagnostics must not become acceptance conditions without a concrete product requirement.
- Judge PASS or product FAIL using completed, attributable direct evidence of that requirement. API/SSH instability, acquisition failure, missing or malformed input, incomplete guest commands and timeouts must remain observation or infrastructure outcomes. Never convert them into zero, no violation, successful rejection or measured product failure.
- Reduce arbitrary speed assertions, repeated probes and duplicate gates. Keep contractual deadlines and a bounded watchdog to stop hung work; distinguish watchdogs from product performance measurements.
- Before deleting temporary data, retain raw failed attempts, source identity, exits, timestamps and cleanup outcomes. Continue cleanup only for resources this invocation owns. Keep cleanup failure visible and preserve product evidence.
- Do not repeat a failed real trial without investigating its cause. Stop at the existing retry, time or cost budget; report failure, uncertainty, remaining budget and proposed fix before further trials. Reacquire only justified failed or missing observations, with bounds and retained attempts.
- Audit the actual entry, selected source paths and hashes, dependencies and final result propagation. Candidate and mock validation proves that scope; it cannot prove that a running entry selects the candidate or that real forwarding works.
- Distinguish implementation, prepared candidate, deployed code, saved-original replay, deliberately modified fixture and unverified behavior. Preserve original negative evidence; never rewrite a historical failure as PASS.
- Apply these rules before reusing old or unconnected code. Keep sealed historical evidence unchanged; repair reusable sources. An absent current caller does not excuse a defect.

These rules do not weaken real functional requirements, safety or ownership checks, or spending limits. Missing necessary evidence cannot establish PASS. Keep proof of normal communication and direct violations; remove only unjustified coupling to diagnostics.

Good: curl exit 0 and HTTP 200 with required body/path evidence can PASS when optional latency is missing; record the omission. Bad: a failed WireGuard read proves no forbidden AllowedIP; a forced invalid-apply timeout counts as successful rejection; a same-stack ping proves tunnel forwarding.

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

When a change touches `webconsole/`, its checked-in static assets, the shared
quality workflow, or the Makefile, CI also runs the Web Console-specific gate:
`npm ci`, high-severity audits for all and production dependencies, TypeScript
type-checking, a production build, and a generated-asset drift check.

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
