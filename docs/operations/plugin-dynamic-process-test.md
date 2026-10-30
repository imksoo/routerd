# Plugin and dynamic configuration process checks

Run `sh scripts/plugin-dynamic-process-smoke.sh` from a checkout with the Go
toolchain in `go.mod`, `/bin/sh`, and `sha256sum`. `GO` can name that toolchain's
absolute executable. The script uses one build worker and private temporary
files. It starts no shared service and changes no host network configuration.

The first check uses the production EventSubscription controller, its default
plugin runner, an independent executable, and SQLite. It stops an invocation
while its actual process is alive, waits for controller acknowledgement, checks
that the process no longer exists, then retries through a new owner. A real
exit status 7 publishes nothing. A later successful invocation publishes its
resource. Reopening SQLite does not replay an already acknowledged event.

The second check uses a separately built production `routerctl` executable.
Each invocation is an independent process. It verifies the actual stdin request,
manual publication, durable generations, and rendered effective configuration.
An allowed mask suppresses a startup route; removing its DynamicOverridePolicy
rejects the mask. A failing plugin preserves the previous generation and its
original expiry. Actual wall-clock expiry restores the startup route. Another
successful process advances the generation and restores the dynamic route.

These are local plugin protocol, process lifetime, persistence, and effective
configuration checks. They do not install a kernel route, qualify forwarding,
exercise provider credentials, execute actionPlans, or replace independent
network-peer/platform qualification. Evidence remains in the printed temporary
directory and includes the executed revision, source hashes, binary hash, and
both test logs. Ordinary package tests skip the separate-binary check unless
`ROUTERD_RUN_PLUGIN_PROCESS=1` and `ROUTERCTL_BINARY` are explicitly supplied.

## Entry points still missing

In both the 1.0 baseline `e3b49dbb` and the reviewed 1.5 base `f294bb23`,
`DynamicConfigSource` is decoded and validated, but no runtime caller consumes
its `pluginRef`, `ttl`, `mergePolicy`, or `triggers`. Likewise, declared
`Plugin.spec.triggers` are validated and displayed without scheduling a plugin.
These declarations are not a working periodic or event-bus refresh mechanism.
The effective lifetime in the demonstrated manual path comes from
`PluginResult.status.ttl`; it is not bounded by `DynamicConfigSource.spec.ttl`.

The reachable execution paths are explicit `routerctl plugin run` and an
EventSubscription naming a Plugin. DynamicOverridePolicy is consumed when the
effective configuration is built. The missing DynamicConfigSource scheduler is
an unfinished advertised entry point; these passing checks do not mark the
whole plugin-and-dynamic-configuration feature complete.
