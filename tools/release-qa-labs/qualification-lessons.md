# Qualification preparation: R1–R12 findings and admission checks

These attempts exposed avoidable defects in **test preparation, collection and
assessment**. Product improvements do not justify those reruns. This record
separates causes so that adding documentation is not mistaken for fixing code.
The R1–R12 labels below refer to the existing-router regression campaign of
2026-10-09/10, not twelve four-cloud provisioning runs. R3 and R10 stopped at
certification; R9 was preparation only. Their original outcomes remain unchanged.
Actual billing and the amount of wasted spend have not been established.

## What failed, and which kind of correction it needs

“Missing” means the runbook lacked an operational requirement; “ambiguous” means
it did not specify the evidence semantics. “Procedure” identifies a preparation
step not performed, including the existing requirement to reproduce locally
before using live tests. “Code” identifies an incorrect producer, collector or
predicate. More than one category can apply. These are root-cause categories,
not replacements for the historical wrapper's fixed classification field.

| Run | Observed failure and classification | Correction / offline rejection | Issue → PR | Formal elapsed time |
| --- | --- | --- | --- | --- |
| R1 | Aggregate counters could not attribute a positive command amid background ARP. Missing attribution specification; incomplete test design. | Packet accounting initially added; later R8/R9 showed its limit. Require explicit target/sequence/write evidence now. | [1279](https://github.com/imksoo/routerd/issues/1279) → [1280](https://github.com/imksoo/routerd/pull/1280) | 9m26s |
| R2 | Slow receipt timestamps lost valid TTL; valid parenthesized tcpdump syntax was missed; shortened final polling falsely suggested a stalled controller. Ambiguous receipt/poll semantics plus collector/parser code defects. | First completed full-identity SELECT, structured parser, normal final polling; reject backdated or late receipts and real stalls. | [1281](https://github.com/imksoo/routerd/issues/1281) → [1282](https://github.com/imksoo/routerd/pull/1282) | 8m06s |
| R3 | Copied phase JSON retained the previous run ID. Preparation/procedure error; the runbook lacked an explicit pre-freeze phase-ID command. Certification correctly rejected it before live work. | Read JSON structurally and check both phase identities and bound bytes before freezing. | [1283](https://github.com/imksoo/routerd/issues/1283) → [1284](https://github.com/imksoo/routerd/pull/1284) | Not started |
| R4 | Generated baseline omitted `management_address`; actual rollout consumer had not been exercised. Producer defect and procedure omission; required baseline fields/consumer rehearsal were undocumented. | Rebuild from saved bound guest/media inputs, compare generated fields, require complete actual consumer evidence. | [1285](https://github.com/imksoo/routerd/issues/1285) → [1284](https://github.com/imksoo/routerd/pull/1284) | 37s |
| R5 | A DB receipt suppressed additional positive-control opportunities even though attribution was inconclusive. Ambiguous distinction between receipt and usable control; scheduler design defect. | Independent unconditional ARP opportunities; receipt cannot cancel them. Reject overlapping lifetimes or incomplete expiry tail. | [1286](https://github.com/imksoo/routerd/issues/1286) → [1287](https://github.com/imksoo/routerd/pull/1287) | 40m39s |
| R6 | Conservative clock bounds placed real traffic about 0.11–0.13s before coordinator dispatch. Missing clock-margin derivation; stimulus timing design defect. The freshness rejection was correct. | Real measured sender lead greater than twice the maximum clock bound, within one absolute deadline; negative clock-extreme tests. | [1288](https://github.com/imksoo/routerd/issues/1288) → [1289](https://github.com/imksoo/routerd/pull/1289) | 8m05s |
| R7 | Reconcile completion counter was treated as a fixed heartbeat while serial commands were completing normally. Ambiguous progress semantics; evaluator defect. | Every original pair must show reconcile completion, or covered command completions followed by a successful reconcile. A true stall remains rejected. | [1290](https://github.com/imksoo/routerd/issues/1290) → [1291](https://github.com/imksoo/routerd/pull/1291) | 29m47s |
| R8 | Adjacent commands and autonomous traffic made aggregate/quiet-window attribution inconclusive. The test model still could not prove command identity; counterexamples should have been tested earlier. | Explicit target-bound successful command record, with actual captured writes; reject absent/stale/wrong-target records. | [1292](https://github.com/imksoo/routerd/issues/1292) → [1293](https://github.com/imksoo/routerd/pull/1293) | 29m43s |
| R9 | Proposed counter-only batch closure was disproved offline by carry-in/carry-out work. No formal run was started. This was a successful preflight rejection of an inadequate proof model. | Retain the counterexample; do not substitute aggregate totals for explicit records. | [1292](https://github.com/imksoo/routerd/issues/1292) → [1293](https://github.com/imksoo/routerd/pull/1293) | Not started |
| R10 | ISO parser did not understand the retained `ISO_BYTES` metadata; later a preparation-only key violated the already closed contract schema. Parser/contract-construction defects and incomplete procedure: only the custom driver had been replayed. | Exact storage grammar and malformed-line negatives; authoritative schema on final bytes, separately from driver replay. | [1298](https://github.com/imksoo/routerd/issues/1298) → [1299](https://github.com/imksoo/routerd/pull/1299); [1300](https://github.com/imksoo/routerd/issues/1300) → [1301](https://github.com/imksoo/routerd/pull/1301) | Live not started |
| R11 | 100ms scheduling target became a fatal read deadline despite a 400ms evidence limit; failed sample was lost, stale “healthy” progress allowed later stimuli, and the short capture hid the primary error. Collector/health/assessment defects; prescribed coverage semantics were not implemented correctly. | Retain slow/error stages, enforce actual coverage, check fresh process-bound health before every stimulus, recover raw evidence and report the first error. | [1302](https://github.com/imksoo/routerd/issues/1302) → [1303](https://github.com/imksoo/routerd/pull/1303) | 80m44s |
| R12 | Command completed inside the original controller interval, but its first status observation was just outside. Evaluator boundary defect and missing boundary regression. | Replay saved reads with original command JSON; retain the interval and 400ms limit. Missing metadata, partial writes, gaps and no later reconcile still fail. | [1304](https://github.com/imksoo/routerd/issues/1304) → [1305](https://github.com/imksoo/routerd/pull/1305) | 62m05s |

Formal time totals 4h29m12s; R1 start to R12 sealing spans 12h26m10s.
The difference includes preparation, diagnosis, code/CI, separate four-provider
QA, release building and evidence preservation; it is not all idle wait or all
billable cloud runtime. Full observation/TTL windows explain part of each run's
duration, but repeating them because a parser, producer or boundary was untested
was avoidable. R3/R4/R10 needed complete preparation validation; R1/R5/R8 needed
stronger counterexamples earlier; R11/R12 needed scheduler and boundary tests.

Historical basis: the retained per-run `HANDOFF`/`RESULT` and raw evidence,
linked issue/PR records, and runbook revisions preceding each fix. In particular,
`03c94aad^` lacked ARP attribution instructions; `1814ec2e^` lacked the phase-ID
pre-freeze command; `1358c7a6` added the baseline producer/consumer procedure;
`70d3a135` already required 400ms command coverage; `f8096ce7` added the public
final-contract validation command. Do not describe all these failures as missing
documentation or claim that a later runbook already existed for an earlier run.

## Required preparation and commands

Use Python 3 with the standard library and a complete checkout of the reviewed
QA revision, including `scripts`, helpers, tests and fixtures. Copying only the
certifier script is insufficient. Keep provider credentials and private raw
observations out of the public repository. Retain the exact artifact checksum,
actual observer retry settings, boot/process identities, saved guest/media reads,
phase plans and all source hashes. These are prerequisites, not optional defaults.

Before acquiring a paid lifecycle or freezing a new retained-router run:

```sh
make release-qa-preflight-test
python3 scripts/release_certification.py validate-contract \
  --contract prospective-contract.json --environment "$environment" \
  --topology "$topology" --providers "$providers" > contract-validation.json
```

The second command runs the authoritative schema/artifact validation and the
mandatory offline gate against the **actual prospective bytes**. Repeat it after
any input or hash-binding edit. `contractSha256` must match the frozen file.
Preparation notes belong outside the strict contract, not in unknown properties.
The source-only Make target alone does not validate a run's real preparation.

The gate in [`scripts/release_qa_preflight.py`](../../scripts/release_qa_preflight.py)
executes the fixed local positive/negative suite; network socket creation and
subprocess execution are forbidden within that suite. Missing modules, failed
checks, skipped required checks and an empty suite fail. The JSON report records
the source and fixture hashes. Certification repeats the gate before invoking
its driver and writes `*.offline-preflight.json`; qualification preflight repeats
it before starting its driver. The paid precheck repeats it before provider
capacity/substrate calls, retaining `preflight/offline-regressions.json`.
CI runs the same Make target. No prior PASS file bypasses execution.

For phased retained-media contracts, `execution.qualification_plans` binds both
`canary` and `final` paths/SHA256s and their JSON `runId`/`stage`. When nodes are
specified, `baseline_builder_binding` is mandatory. Its bound `evidence` contains
current-run saved guest/media inputs and `generatedBaseline`. The gate rebuilds
each node with `rollout_baseline.build_rollout_baseline()` and compares **every
produced field**, including nonempty management addresses and configuration/media
identity. Missing, duplicate or foreign nodes, failed transports and changed bytes
are rejected. Extra caller metadata does not replace any required field.

The bound `actual_all10_prestop` report must cover the exact declared node set
(the historical field name does not prescribe a node count), the same baseline
and builder report, successful bound guest/media transports, and all successful
nonempty guest/media checks. Its evaluator,
caller and common source hashes must match `execution.script_sha256` and current
files. This checks the actual consumer's saved evidence, not just a producer's
“success” flag. The private adapter must run its evaluator/command-construction
positive and negative tests before producing that evidence; public helper tests
cannot prove that a private caller invokes the helper correctly. Bind the private
adapter tests and their report in the run contract as well. Current live freshness,
management reachability and environment certification remain independent gates.

## Saved data and negative controls

[`tests/fixtures/arp-r12-boundary.json`](tests/fixtures/arp-r12-boundary.json)
contains three actual controller reads and 50 actual counter reads, with source
SHA256 and an explicit redaction/selection record. Timestamps, counts, sequence
571 and three writes are retained; identities use documentation values. It is a
**diagnostic extract**, not replacement formal evidence. The R11 fixture retains
the original collector/sampler error and short-capture duration; the missing
failed read timing is not invented.

[`test_saved_arp_observations.py`](tests/test_saved_arp_observations.py) requires
the R12 positive boundary to pass and variants without the record, later
reconcile or coverage, and with partial writes/wrong sequence, to fail. It checks
that the saved R11 primary error wins over an older healthy-looking snapshot.
Other mandatory tests cover full-identity receipt/TTL, parenthesized ARP syntax,
wrong targets, background traffic, fixed independent opportunities, real clock
lead/deadline, true stalls, 170ms reads within 400ms coverage, over-limit gaps,
terminal failure over stale progress, PID reuse, dead capture, baseline omissions and ISO metadata.
Progress age and counter sampler errors are diagnostic while the owned capture
remains alive; they do not create a new stimulus admission deadline.

[`test_release_qa_preflight.py`](tests/test_release_qa_preflight.py) also exercises
the admission boundary: a failed gate must leave the certification driver
uncalled. Preparation negatives use current hashes for bad logical inputs, so
hash agreement alone cannot make a copied run ID or incomplete baseline pass.
A saved-data PASS proves this tooling correction on those inputs; it never
changes an old run's FAIL into PASS or proves new live qualification.

## When to run again

After a formal failure, retain its inputs, raw errors, verdict and cleanup/zero
inventory evidence. Diagnose first. Add the failing saved case and a nearby
negative/positive counterexample, fix the producer/collector/predicate, and run
both the public gate and actual private caller tests. Do not replay an expensive
run unchanged to see whether it happens to pass. An unexplained failure remains
unresolved; if necessary, design the smallest bounded observation that can
identify it before committing another full lifecycle.

Only a newly frozen, fully validated contract and current certification may
start the next formal run. Preserve observation/TTL/coverage/traffic thresholds;
do not trim matrices or overwrite failed evidence. A QA-only correction with
unchanged product bytes does not by itself require rebuilding the ISO or repeating
already completed four-provider product qualification. Select the required
remaining live scope from the changed behavior and release policy, and reuse
valid evidence only with its exact identity and stated limits. This change must
not interrupt an in-flight frozen run or start duplicate cloud tests.
