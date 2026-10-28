# routerd 1.5 development proposal

This branch proposes a compatibility-breaking revision of routerd. These rules
apply to development on this branch; they do not declare a released version or
change the policy of main until the proposal is accepted.

## Goal and scope

Deliver the existing router functions with smaller, understandable code and real
interoperability. KISS and robustness are the highest engineering priorities.
Judge completion by forwarding, peer/client results, withdrawal, failure handling,
and recovery. Code volume, coverage, reports, and passing unit tests are supporting
evidence, not proof that a router function works.

Start from routerd's production paths and operational knowledge. Read relevant
callers before changing a feature. Reuse useful protocol and OS behavior, and
selectively port proven routerd2 fixes. Neither existing architecture nor a new
architecture is a requirement in itself. The working plan is
[docs/design/routerd15-plan.md](docs/design/routerd15-plan.md).

## Compatibility decision

The user authorizes replacing configuration, internal APIs/IPC, and event/state DB
formats for this revision. Configurations may be rewritten and old databases may
be discarded at cutover. Do not build old-format readers, migrations, dual writes,
version negotiation, or fallback implementations merely to preserve them.

This is permission to change formats, not to remove existing product functions or
to delete a running router's data during development. Preserve needed recovery
semantics in the chosen new design. Lease validity, managed-resource ownership,
and durable pending operations have functional meaning independent of old schemas.
External network protocols still need interoperability with independent peers.

## Real communication and robustness

- Establish the operator problem, intended peer/client, result, and smallest
  useful demonstration. Exercise the actual binary and kernel early.
- Send correct, simple protocol messages. Accept usable input whose meaning is
  clear; ignore unknown optional information where permitted. Discard an unusable
  independent option without discarding unrelated usable information.
- Preserve packet bounds, exchange/peer identity, authentication, resource bounds,
  and actual protocol requirements. Reject at the narrowest necessary scope.
  Do not infer receive rejection rules from our encoder or test fixtures.
- Resolve uncertain behavior using applicable protocol specifications, established
  peers, captures, and operational evidence. Do not encode invented restrictions
  into tests. Do not add speculative device-specific compatibility frameworks.
- Represent absent, stale, and unknown observations honestly. Recheck after a wait
  when time or concurrent changes can invalidate the premise.
- Report implementation, local checks, actual communication, and target-device
  qualification separately. Missing observations do not prove success or failure.

## Architecture and ownership

- Trace input, decisions, effects, and result publication as one visible path.
  Give each mutable state and external side effect a clear owner.
- Separate operator intent, current observations, derived decisions, and applied
  results. Publish an applied result only after its effect succeeds. Preserve
  failures rather than fabricating success.
- Decode and normalize external data at its owning boundary; pass concrete typed
  values inward. Confine dynamic dependency values and current generic status
  maps to their boundaries while replacing their consumers coherently.
- Do not add new untyped application contracts using any, interface{}, arbitrary
  maps, reflection, or encoded payloads. Upstream APIs accepting any and real
  generic algorithms are allowed without forwarding wrappers.
- Do not serialize typed values to clone them or pass them between functions.
  Use read-only sharing, ownership transfer, or explicit copies of mutable parts.
- Prepare static configuration work once per configuration. Observation updates
  should use the prepared values and current observations, not rebuild the world.
- Separate read-only decisions from writes. Planning should not need to execute
  controllers against a cloned database.
- Retain persistence for a concrete recovery, pending-operation, or history-based
  feature. Ordinary display history must not unnecessarily block forwarding.
- Remove conflicting ownership and duplicate state before adding guards, caches,
  retries, queues, registries, or managers. Necessary retries have one owner and
  bounded lifetime. Choose process boundaries for actual privilege or isolation.

## Direct Go and useful checks

- Use go.mod's toolchain, the standard library, and suitable existing dependencies.
  Prefer concrete types and ordinary functions. Define small interfaces at actual
  consuming boundaries; avoid mock-shaped production APIs and unused wrappers.
- Keep dependencies directed. Protocol rules do not depend on CLI or orchestration.
  Keep cohesive operations together; do not impose file/function-length quotas.
- Make goroutine and resource lifetime explicit. Pass cancellation through blocking
  work; give recovery that outlives a request its own bounded owner.
- Handle errors at the responsible operation. Do not swallow a failed effect or
  report success on a closed store. Avoid duplicate logs and repeated validation.
- Remove superseded code, unused parameters/results, obsolete tests, and temporary
  scaffolding in the feature path being changed. Avoid a repository-wide cleanup
  campaign as a prerequisite to useful functionality.
- Add focused tests for concrete behavior or reproduced failures. Use the actual
  implementation. Avoid exhaustive hypothetical rejection matrices, algorithm
  copies, global replacement hooks, and parallel product simulators.
- Use and update Makefile targets for relevant local checks. Do not wait for GitHub
  Actions. Broaden checks when changes or failures justify it. Never claim an
  unrun test passed or weaken acceptance to hide a failure.

## Execution

Use isolated VMs or explicit netns fixtures for network mutations; normal unit
tests must not change host networking. Check management access and recovery before
changing a real router. Use existing task authorization without repeated approval.
No production deployment is implied by implementing a feature.

Protect concurrent edits and stage explicit paths. Keep credentials and private
infrastructure details out of commits. Retain licenses and notices when reusing
material. Use the project build paths; Linux Go release binaries use CGO_ENABLED=0
except explicitly separate components requiring native libraries.

Update affected schemas and operator documentation together with contract changes;
do not maintain discarded formats. Keep this policy in English and explain work
in Japanese. Report the executed revision, relevant setup, observed result, and
unverified scope concisely.
