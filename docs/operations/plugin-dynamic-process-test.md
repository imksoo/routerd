# Plugin and dynamic configuration process checks

Run `sh scripts/plugin-dynamic-process-smoke.sh` from a checkout with the Go
toolchain in `go.mod`, `/bin/sh`, and `sha256sum`. `GO` can name that toolchain's
absolute executable. The script uses one build worker and private temporary
files. It starts no shared service and changes no host network configuration.

## Checks and evidence

The EventSubscription check uses its production controller, default plugin
runner, an independent executable, and SQLite. It stops an invocation while its
actual process is alive, waits for controller acknowledgement, checks that the
process no longer exists, then retries through a new owner. A real exit status 7
publishes nothing. A later successful invocation publishes its resource.
Reopening SQLite does not replay an already acknowledged event.

The CLI check uses a separately built production `routerctl` executable.
Each invocation is an independent process. It verifies the actual stdin request,
manual publication, durable generations, and rendered effective configuration.
An allowed mask suppresses a startup route; removing its DynamicOverridePolicy
rejects the mask. A failing plugin preserves the previous generation and its
original expiry. Actual wall-clock expiry restores the startup route. Another
successful process advances the generation and restores the dynamic route.

The prepared-owner package checks trigger union, independent interval deadlines,
source lifetime bounds, failure retention, compare-and-swap publication,
expiry during a publication wait, effective-change notifications, and
cancellation with actual process reaping. The framework package also checks
that a dedicated startup callback preserves the default and named-event paths.
The chain checks use the production
controller registration and actual executables: interval startup, a second
invocation from the framework's positive deadline, and a named bus event.
Preview and standby checks require no process execution or publication.

Evidence remains in the printed temporary directory. It includes the revision,
Go version, source hashes, the `routerctl` binary hash, and separate
`subscription.log`, `cli.log`, `owner.log` (owner and framework), and
`declared.log` files.
Ordinary package tests skip the separate-binary CLI check unless
`ROUTERD_RUN_PLUGIN_PROCESS=1` and `ROUTERCTL_BINARY` are explicitly supplied.
The script supplies both for that check.

## Declared execution

For each startup configuration, the Plugin controller prepares the executable
specification, allowed context, source lifetime bound, and triggers once. It
takes the union of `Plugin.spec.triggers` and the triggers of every startup
`DynamicConfigSource` whose `pluginRef` names that Plugin. Equal interval
durations and equal event topics are deduplicated.

Interval work is initially due when the controller starts. Each distinct
interval keeps its own deadline. Multiple due intervals produce one invocation
for that Plugin in the same periodic reconciliation. After an interval attempt,
each interval due by completion advances to completion time plus its duration,
including after failure; future deadlines are preserved. A named event invokes
only matching Plugins and does not consume any interval deadline, including
deadlines reached during the event invocation. There is no catch-up burst for
missed intervals. The existing framework owns the single worker and its next
wake-up.

Event triggers use the named bus topic in `topic` and the existing bus matching
rules. An explicitly configured wildcard such as `**` keeps those rules.
A blank topic is not expanded to all notifications. Internal framework startup
and periodic callbacks enter interval processing separately from bus events.
Published bus events named `routerd.controller.bootstrap` or
`routerd.controller.periodic` still match explicit event declarations normally.
An event-only Plugin does not run merely because its worker started.

Direct `Plugin/<name>` publications, automatic or manual, use the shortest
`ttl` among startup DynamicConfigSources referring to that Plugin. Without
such a source there is no additional source bound. A shorter result TTL remains
unchanged. Expiry is measured from the result's original `observedAt`; an
already expired result is rejected, including if it expires while waiting for
publication.

A failed invocation or rejected publication leaves the previously accepted
generation and expiry unchanged. An expired previous part remains inactive
until a successful fresh result is accepted. A successful refresh with the same
effective content does not emit a change notification merely for a new
generation or a later expiry. Changed content and recovery from expiry do emit
one, preventing repeated identical refreshes from feeding an explicit wildcard
subscription indefinitely.

Daemon preview, sandbox, and standby modes neither start automatic Plugin
processes nor consume due work. Manual `routerctl plugin run --dry-run` is a
different operation: it executes the Plugin to print a candidate without
publishing. When the controller context is canceled, its child is stopped and
reaped; a result returned after cancellation cannot be published.

## Qualification boundary

These are local plugin protocol, scheduling, process lifetime, persistence, and
effective configuration checks. They do not install a kernel route, qualify
forwarding, exercise provider credentials, or execute actionPlans. Passing them
does not replace independent network-peer or platform qualification.

The prepared-owner and chain process checks have been executed on Linux.
FreeBSD test binaries can be cross-compiled from the same source, but
cross-compilation alone does not establish FreeBSD process or worker behavior.
Record actual FreeBSD execution separately when performed. No provider/cloud
integration or privileged network mutation is required by this script.

See [Plugin protocol](/plugin-protocol.md) and
[Dynamic config reference](/reference/dynamic-config.md).
