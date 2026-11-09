# routerd 1.1 development rules

## Authority and scope

The latest explicit instructions from the owner govern this work. This file and
docs/design/routerd11-mvp.md replace conflicting legacy development, testing,
compatibility, and release procedures. Do not weaken these rules yourself.

There is one user. Build on routerd 1.0 for that user's homert, k8s-rt, pve-rt,
and Cloud SAM. Preserve their actual configurations and required behavior.
Remove unused features, configuration fields, APIs, helpers, dependencies,
documentation, tests, and CI jobs. Hypothetical users, possible future use,
existing tests, and the mere existence of code are not reasons to keep them.
Trace runtime generation, references, startup, renewal, recovery, and removal
before declaring a dependency unused. Examples are not live configurations.

## Apply the configuration

Configuration is the source of the settings routerd applies. Apply must repair
missing or changed configured objects and remove objects removed from the
configuration. Leave unrelated host settings and manually added objects alone,
including on interfaces referenced by the configuration. Do not track or check
ownership or creation history. Retain valid DHCP/PD/BGP/SAM and other dynamic
state required by configured operation; do not treat it as static YAML.

## Real operation first

Make a small useful change, build it, and take it to the existing real target
and its real peers promptly. Do not wait for a full rewrite, exhaustive audit,
test suite, coverage target, or new verification framework.
Do not expand the task or acceptance scope without the owner's instruction.

Mocks, fakes, simulators, and substitute network environments are prohibited.
Do not build a new test VM fleet or namespace harness instead of using the
specified targets. Existing PVE or cloud routers are real deployment targets;
use their actual kernel, product processes, peers, and traffic.

## Tests are an exception

Do not create or expand test code without prior, explicit owner approval for
that specific test. Before proposing one, explain the exact contract or fault,
what a compiler or real-target check cannot establish, and its creation and
maintenance cost. Interface and calling-contract tests follow the same rule.
Approval for a test does not authorize a mock, fake, or simulated environment.

Tautologies, implementation-mirroring assertions, canned-success checks, and
checks that only confirm a configured mock answer are prohibited. Creating
them is a development-policy violation and wasted work, not zero-value credit.
These prohibitions apply retroactively. Remove existing prohibited tests and
their test-only adapters, fixtures, abstractions, and CI work. Do not repair
them merely to preserve a green suite. Preserve production dependencies.

Do not impose an arbitrary test deadline or elapsed-time assertion. A timing
failure needs an explicit product requirement, protocol rule, or demonstrated
fault. Twelve-second success is not failure because a test author chose ten
seconds. Remove unsupported deadlines in existing tests. Hang protection is
separate: inspect actual progress and diagnostics before attributing a stall.
Ten minutes is an example of a suspicious wait, not a universal timeout.
Keep required production protocol and operational timers distinct.

## Implementation

Use concrete types and direct control flow. Decode and validate at real
boundaries; do not repeatedly turn typed values into maps, JSON, or YAML.
Keep unavoidable untyped external payloads at the boundary.
Give mutable state one responsible implementation. Remove replaced paths and
unused arguments. Do not introduce speculative compatibility, fallback chains,
universal engines, test-only product hooks, or receipt frameworks.
Retain the leases, identities, deadlines, and exclusion needed by real
protocols and configured HA/SAM operation. Do not hide OS or process errors.

## Working discipline

Read this file and the current MVP before editing, after a context handoff,
and before committing. At task start, briefly state the specific change and
real-target check. Routine work inside the authorized scope proceeds without
repeated permission requests. Exceptions to prohibitions require owner approval
before the prohibited work begins; general best-practice claims are insufficient.

Keep changes small. Inspect the staged diff against the authorized task.
Commit and publish meaningful work promptly; report concrete progress,
revision, real observations, and remaining work. Do not stop development merely
to wait for unrelated CI. Do not claim live validation that did not occur.
Do not merge, release, replace production routers, or delete the 1.5/2.0
branches/repositories unless that action's authorization and conditions hold.

A violation is a negative result. Remove the violating change and its
unnecessary dependencies, report the wasted work and cause concisely, and
resume the authorized task. Do not defend it by citing test counts or code
already written. Do not create another framework to enforce this rule.

## Commit review and cleanup

Enable the repository hook in each working clone:

~~~sh
git config --local core.hooksPath .githooks
~~~

Each commit message must contain specific explanations under these headings:
- Rule-Check: which applicable rules the actual diff follows.
- Scope-Check: why additions are necessary and what unnecessary work was removed.
- Validation: actual commands/observations; state pending real checks honestly.
- Cleanup: files/caches removed, or exact task-owned items to remove after publication.

A bare OK, done, none, or equivalent is not an explanation. The commit-msg hook
rejects missing/empty explanations and prints the AGENTS.md reminder. Its
presence does not prove semantic compliance. Do not bypass, disable, replace,
or evade the hook or these rules. API-created commits need the same review.

Enforce restricted actions with existing administrator-managed permissions
outside the agent's write access. Do not build a new enforcement framework or
make its completion a prerequisite for useful changes or real deployment.
Blocking new tests must not block removing existing prohibited tests.

Keep raw evidence, logs, captures, dumps, and temporary reports outside tracked
source files. Commit a concise useful result, not bulky execution residue.
After that result is committed and published, remove the task's raw artifacts
and temporary outputs. If raw residue was mistakenly tracked, remove it from
the current tree as well. At each completed work segment, clear task-owned
build/test caches, download/extraction caches, and temporary environments.
Do not delete source, active configuration, credentials, deliverables awaiting
publication, or another active task's data. Do not retain waste as evidence.
Report any cleanup that could not be completed; never silently leave it behind.
