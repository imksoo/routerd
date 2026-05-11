# Phase 3.5 release artifact check

Date: 2026-05-11

Scope: inspect release artifacts and release workflow health.

## Findings

The requested tag `v20260511.1428` exists as a tag workflow run, but the GitHub release was not published.

```text
gh run list --workflow release.yaml
completed failure Release v20260511.1428
```

Root cause from the failing job log:

```text
pkg/otel TestResourceAttributesMergeDefaultsEnvAndExplicit
service.version = "v20260511.1428", want "v20260511.1240"
```

This was a test bug. The test now expects `version.Version`, so future tags do not fail when the version changes.

## Latest successful release

Latest published release at check time:

```text
v20260511.1240
```

Artifacts present:

```text
routerd-linux-amd64.tar.gz
routerd-linux-arm64.tar.gz
routerd-freebsd-amd64.tar.gz
routerd-freebsd-arm64.tar.gz
routerd-live-v20260511.1240.iso
routerd-live.iso
matching .sha256 files
```

## Regression found

Downloading `routerd-live.iso` and `routerd-live.iso.sha256` worked, but checksum verification failed from a standalone download directory because the checksum file contained a build path:

```text
5f1b...  dist/iso/routerd-live.iso
sha256sum: dist/iso/routerd-live.iso: No such file or directory
```

This affects release usability even though the artifact itself is present.

## Fix

`make dist` and `scripts/build-live-iso.sh` now write checksum files from inside the artifact directory, so `.sha256` entries contain only basenames.

Local verification:

```text
cd dist/linux-amd64
sha256sum -c routerd-linux-amd64.tar.gz.sha256
routerd-linux-amd64.tar.gz: OK
```

The next release should publish self-contained checksum files.
