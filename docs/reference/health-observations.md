# Current Health observations

A Health sample identifies the normalized probe that produced it: target,
protocol, address family, port, declared source/interface binding, mark, timing
and thresholds. The daemon importer, advisory egress selection and policy routing
compare that identity with the current resolved configuration. Changing a target
or binding cannot reuse the preceding probe's success.

Missing, expired, future-dated or inconsistent samples are unknown. SQLite reads
project their current phase to `Unknown` without rewriting the retained sample.
Historical daemon events and display fields cannot restore current reachability.
The worker resets threshold state when probe identity or sample freshness changes,
so an expired healthy result cannot grant a new failure-grace period.

The worker publishes the typed sample through the current private IPC contract.
Its probe durations and thresholds use decimal JSON strings to preserve exact
values through generic status storage. Old identityless samples do not authorize
routing; restart the worker to collect a new sample at cutover.

This contract identifies the configured probe and its freshness. Current native
route/source lifetime checks and independent two-WAN forwarding qualification
remain separate work; the sample alone does not establish those properties.
