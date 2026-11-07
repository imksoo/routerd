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
native package, restart the routerd service, and verify readiness and
connectivity. It must not run `routerctl apply --replace`, `routerctl rollback`,
or copy a configuration file into the canonical config path.

For Linux 1.5, select the known-good `.deb` artifacts and use APT. When the
native nDPI addon is installed, supply the matching base and addon versions in
the same transaction because the addon has an exact-version base dependency.
Use `apt-get install --allow-downgrades` with the selected local package paths.
Verify the selected version can read the retained configuration and state in an
isolated check before selecting it for operational recovery.

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
- the canonical commit journal event when emitted;
- the canonical config mtime and SHA-256 after the apply.

An exit status of zero alone is insufficient. Confirm the returned generation
and phase, read back the canonical configuration and compare its contents with
the intended candidate, then check the intended native effects and service
readiness. Keep the commit journal event when it is emitted. A matching
idempotent configuration can retain its checksum and modification time, so
unchanged mtime alone cannot establish rollback failure. If the request times
out, inspect canonical and runtime state before selecting a retry.

## Interrupted activation and saved-generation check

On a disposable packaged Linux VM, prepare two configurations with the same
external loopback Interface and owned IPv4StaticAddress. Change only the owned
address between the candidates. Apply the first candidate and retain its
returned generation. Plan the second candidate and verify that planning has no
native effect. Apply it with `--no-reconcile`, require `Committed`, and confirm
that the canonical file contains the second candidate while the first address
still exists in the kernel.

Kill only the main daemon with SIGKILL. Require systemd to replace its PID
without manual startup, canonical or database reset. The restarted daemon must
apply the second address, remove the first owned address and serve its APIs.
Run `routerctl rollback --timeout 90s --to <first-generation>` on that VM. Require
the first canonical configuration and address to return and the second address
to disappear. Exercise `routerctl delete --timeout 90s IPv4StaticAddress/<name>`
and verify that unrelated configured services remain available. Finish with an
empty configuration and verify resource withdrawal.

This path passed with the formal `2da31049` Linux packages. It verifies a process
crash after durable configuration commit and before activation, plus local
saved-generation rollback. Power-loss storage behavior and lease-owner recovery
need their own applicable checks.

## Certification and qualification evidence

The certification manifest or attached evidence must identify the binary
artifact and SHA-256, the selected rollback scope, every configuration
candidate used, and the resulting canonical config SHA-256. A qualification
run may proceed only after the selected rollback path has passed its readiness
and connectivity checks.

See [Release environment certification](release-environment-certification.md)
and [Release qualification policy](release-qualification-policy.md) for the
phase boundaries and qualification gates.
