---
title: Release rollback procedure
---

# Release rollback procedure

This procedure keeps a release rollback from silently replacing the router's
current configuration with an old release snapshot. It applies to release
certification and qualification operations. Host-specific execution scripts
belong in the operator's configuration-management repository and must refer to
this procedure.

## Default rollback scope

The default rollback changes **binaries only**. It may restore the selected
release archive, restart the routerd service, and verify readiness and
connectivity. It must not run `routerctl apply --replace`, `routerctl rollback`,
or copy a configuration file into the canonical config path.

This separation matters because a release artifact and a router configuration
have independent lifecycles. A binary rollback must not implicitly replace
newer configuration resources with a snapshot taken before those resources
existed.

## Exceptional configuration rollback

Change configuration only when the incident requires it and the operator has
explicitly selected configuration rollback.

1. Read the canonical configuration from the router at the time of rollback.
2. Derive the rollback candidate from that current canonical configuration; do
   not use a configuration snapshot bundled with, or previously used for, an
   older release.
3. Store the candidate in a tracked evidence path before it is transferred to
   the router.
4. Record the UTC timestamp, target host, candidate path, SHA-256, source
   canonical path and SHA-256, generator or operator identity, and purpose.
5. Transfer only an execution copy to `/tmp`. `/tmp` is not an evidence or
   provenance store and must not be the only surviving copy of a candidate.

## Applying a configuration candidate

For every `routerctl apply`, record the complete command line, standard output,
standard error, and exit code. Also record all of the following before treating
the apply as successful:

- the `ApplyResult` phase;
- the `committed canonical router config` journal event;
- the canonical config mtime and SHA-256 after the apply.

An exit status of zero alone is insufficient. Until the control API reports a
non-success result for an uncommitted apply, an `ApplyResult` phase that does
not commit canonical config, a missing commit event, or an unchanged canonical
mtime is a failed configuration rollback. Stop and preserve evidence rather
than restarting routerd with an ambiguous runtime-only configuration.

## Certification and qualification evidence

The certification manifest or attached evidence must identify the binary
artifact and SHA-256, the selected rollback scope, every configuration
candidate used, and the resulting canonical config SHA-256. A qualification
run may proceed only after the selected rollback path has passed its readiness
and connectivity checks.

See [Release environment certification](release-environment-certification.md)
and [Release qualification policy](release-qualification-policy.md) for the
phase boundaries and qualification gates.
