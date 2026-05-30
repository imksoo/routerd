# CloudEdge + Event Federation — pre-merge stocktake (event-federation → main)

Status: **experimental** (lab-validated foundation; NOT recommended-stable)
Branch: `event-federation` · Head: `8c4821c8` · Date: 2026-05-30
Author: review subagent (facts only; orchestrator finalizes the merge recommendation)

This is a read-only stocktake of the `event-federation` branch as an
experimental-MVP candidate for `main`. It does not change code or merge anything.

## Scope summary

`event-federation` is **36 commits ahead of `main`, 0 behind** (clean fast-forward
possible). It is a strict superset of `cloudedge-mvp`: the `cloudedge-mvp` head
`713233b0` is an ancestor of `event-federation` (confirmed via
`git merge-base --is-ancestor`). So the branch = **CloudEdge/SAM** (all of
`cloudedge-mvp`) **+ Event Federation Phase 1 / 1.5 / 2 / 3**.

What it adds vs `main`:

- **CloudEdge / SAM** (from `cloudedge-mvp`): dynamic-config foundation
  (`DynamicConfigPart` / masks / `DynamicOverridePolicy`), plugin runner
  (observe-only, dry-run; actionPlans display-only), L3 hybrid
  (`OverlayPeer` / `HybridRoute`), Selective Address Mobility
  (`AddressMobilityDomain` / `RemoteAddressClaim` / `CloudProviderProfile`,
  Linux dataplane), zone-independent PMTU/MSS clamp (#53), nft ownership
  diagnostics, `routerctl doctor hybrid`.
- **Event Federation** (ADR 0006): typed observed-event envelope + SQLite local
  store + `routerctl federation event` CLI (Phase 1/1.5); `routerd-eventd`
  transport daemon + `EventGroup` / `EventPeer` Kinds + HMAC push delivery +
  `event_deliveries` + retention prune (Phase 2); `EventSubscription` Kind +
  subscription-triggered plugin → `DynamicConfigPart` (`RemoteAddressClaim`)
  (Phase 3). Three new Kinds total: `EventGroup`, `EventPeer`, `EventSubscription`
  (apiVersion `federation.routerd.net/v1alpha1`).

## Evidence inventory

In-repo evidence/milestone docs (all confirmed present):

| Doc | Result / verdict |
|---|---|
| `docs/releases/cloudedge-sam-mvp-milestone.md` | Azure/AWS/OCI × PVE all **PASS / clean**; 3-cloud parity; experimental |
| `docs/releases/cloudedge-sam-stocktake-20260529.md` | Pre-merge stocktake; rough edges = experimental follow-ups, not blockers |
| `docs/releases/evidence/cloudedge-sam-azure-pve-20260529.md` | Azure × PVE **PASS / clean** |
| `docs/releases/evidence/cloudedge-sam-aws-pve-20260529.md` | AWS × PVE **PASS / clean** (Azure-parity, first run) |
| `docs/releases/event-federation-checkpoint.md` | Phase 1 + 1.5 checkpoint; experimental; not a release tag |
| `docs/releases/evidence/cloudedge-event-federation-transport-20260530.md` | Phase 2 transport smoke **Result: PASS** (7 assertions A–G) |
| `docs/releases/evidence/cloudedge-event-federation-subscription-20260530.md` | Phase 3 subscription smoke **Result: PASS** (main + 4 negative checks) |

Full evidence bundles (and the OCI summary) live in the sibling lab repo
`/home/imksoo/routerd-labs/...`, by the established lab pattern (not committed to
this repo). The transport/subscription evidence bundles referenced
(`routerd-labs/event-federation/evidence/20260530T091652Z-...` and
`...20260530T111612Z-...`) exist on disk.

### Link integrity finding (MINOR, should fix)

`cloudedge-sam-mvp-milestone.md:24` links the OCI evidence as
`routerd-labs/cloudedge-sam/evidence/20260530T031247Z-oci-pve-hardening/summary.md`,
but the actual directory on disk is
`20260530T031247Z-oci-pve-hardening-43a64c55/` (note the missing `-43a64c55`
commit suffix). **The referenced path does not resolve** → broken link. (This
path is into the external lab repo, not a repo-internal doc, so it does not break
the website build, but it is an incorrect reference.) All four
`docs/releases/evidence/*.md` in-repo references in the milestone resolve
correctly.

## Consistency findings

### ADR 0006 status is STALE (MUST-FIX before merge)

`docs/adr/0006-event-federation.md` Status section still says:

> Phase 1 (...) is implemented on `event-federation`. **Phase 2 (peer delivery
> over the overlay) is pending.**

and the Context says **"OCI×PVE in progress"**. Both are now false: Phase 2 AND
Phase 3 are implemented (with PASS smokes), and OCI×PVE passed. The ADR Status
block should be updated to reflect Phase 1–3 implemented and OCI clean.

### Docs site navigation — new docs are ORPHANED (MUST-FIX before merge)

`website/sidebars.ts` is the docs sidebar (default English under `docs/`). The
SAM reference (`reference/selective-address-mobility`) IS wired in
(sidebars.ts:150). But:

- **`docs/how-to/event-federation-subscription.md` is NOT in `website/sidebars.ts`**
  (`grep` count = 0). It is orphaned — the site will not surface it in the
  How-to guides category.
- There is **no dedicated `docs/reference/` federation reference doc** (only
  `dynamic-config.md` and `selective-address-mobility.md` exist under
  `docs/reference/`). If a federation reference page is intended, it does not
  exist yet; if the how-to is the only federation doc, it still needs a sidebar
  entry.

Per project policy (正本 = 日本語 `website/i18n/ja`, Web default = English
`docs/`), the i18n/ja sidebar/translation would also need the entry, but the
sidebar structure is shared (`sidebars.ts`), so adding the how-to to
`sidebars.ts` is the single required wiring change; ja translation content is a
separate (lower-priority, experimental) follow-up.

## API schema generation finding (MUST-FIX before merge)

Generator: `make generate-schema` → `cmd/routerd-schema` →
`schemas/routerd-config-v1alpha1.schema.json` (+ control + control-openapi).
All three schema files ARE tracked in git.

- Running `make generate-schema` (and `make check-schema`) leaves **NO diff** —
  `git status --short schemas/` is clean. So the committed schema is internally
  consistent with the generator.
- **BUT the schema is INCOMPLETE.** `cmd/routerd-schema/main.go` enumerates every
  Kind by hand via `resourceSchema(apiVersion, "Kind", Spec{})`. SAM Kinds are
  registered (lines 327–331: OverlayPeer, HybridRoute, AddressMobilityDomain,
  CloudProviderProfile, RemoteAddressClaim). The **three new federation Kinds —
  `EventGroup`, `EventPeer`, `EventSubscription` — are NOT registered** in the
  generator list. Consequently they do not appear in the generated/published JSON
  schema, and regeneration produces no diff (the generator simply doesn't know
  about them).
- Fix = add three `resourceSchema(api.FederationAPIVersion, "EventGroup"/"EventPeer"/"EventSubscription", api.…Spec{})`
  lines to `cmd/routerd-schema/main.go`, then `make generate-schema` and commit
  the resulting `schemas/` diff. (Not done here — reported for the orchestrator.)

Verification note: `make check-schema` currently **passes** because it only
checks the generator output against committed files; it does NOT detect missing
Kinds. So CI green does not catch this gap.

## make dist / packaging completeness

- `routerd-eventd` IS in `make dist`: Makefile `ROUTERD_RELEASE_BINS` includes
  `$(ROUTERD_EVENTD_BIN)` (Makefile:33–34), `build-daemons` builds it
  (Makefile:74), and dist installs it (Makefile:199). `make -n dist | grep eventd`
  confirms the build + install lines.
- The **example plugin (`examples/plugins/event-to-remote-claim`) is NOT shipped
  by `make dist`** (no Makefile reference; `make -n dist` shows no
  `examples/plugins`). This is **documented**: both
  `examples/plugins/event-to-remote-claim/README.md` ("## Build and install" →
  `go build -o bin/event-to-remote-claim ./examples/plugins/event-to-remote-claim`)
  and `docs/how-to/event-federation-subscription.md:61–64` tell the operator to
  build it separately.
- **Packaging needs no eventd-specific change.** `packaging/install.sh` installs
  all binaries via a generic glob (`for binary in bin/*`, line 1873), so
  `routerd-eventd` is installed automatically. The per-group systemd unit
  `routerd-eventd@<group>.service` is **generated by routerd itself** (via the
  controller chain / `pkg/render/eventd_systemd.go` + `EventGroup` supervision in
  `pkg/controller/eventfederation`), not shipped as a static unit, so
  `install.sh`'s `systemd/*.service` loop does not need it. No static
  `routerd-eventd.service` exists in `contrib/systemd/` (by design — it is a
  templated `@.service`).

## No provider mutation (security / scope gate) — CONFIRMED: NO

Grepped the whole tree (Go source in `pkg/`, `cmd/`, `examples/`):

- **No cloud SDK imports** (`aws-sdk` / `azure-sdk` / `oci-go-sdk` /
  `cloud.google.com` / `github.com/{aws,Azure,oracle}/`) — zero matches.
- **No cloud CLI exec.** The only `exec.Command*` calls touching external tools
  are `pkg/controller/dhcpv4client/controller.go` and
  `cmd/routerd-pppoe-client/main.go` (local DHCP/PPPoE), neither cloud-related.
- `ActionPlan` is **declared display-only**: `pkg/plugin/types.go:85–86`
  ("MVP routerd never executes ActionPlans"); test
  `TestRunRemoteAddressClaimActionPlanIsDisplayOnly` enforces it.
- The example plugin reads `os.Stdin` JSON and writes `os.Stdout` JSON only
  (`examples/plugins/event-to-remote-claim/main.go`) — **no exec, no http, no
  net, no cloud call**; its own header comment states provider action execution
  is out of MVP scope (Phase 4/5).

Definitive: **no executable provider-mutation path exists on this branch.** The
only provider-touching surfaces are declarative specs (`CloudProviderProfile`,
capture type `provider-secondary-ip`), display-only actionPlans, and the
no-cloud-call example plugin.

## Experimental labeling — CONFIRMED

- `cloudedge-sam-mvp-milestone.md`: "Status: **experimental** (lab-validated; NOT
  recommended-stable)"; recommendation explicitly defers stable promotion / release tag.
- `event-federation-checkpoint.md`: "Status: **experimental** (in development;
  NOT recommended-stable)"; "**not** a release tag."
- ADR 0006: "Accepted for **experimental implementation**."
- Phase 2/3 evidence verdicts scope the result to control-plane only and assert no
  provider/cloud mutation occurred.

No doc reviewed implies stable / recommended. No release-tag or stable promotion
is claimed.

## Known gaps

Of the four expected gaps, two are accurately understood as gaps, one is
mischaracterized, and one is undocumented:

1. **FreeBSD rc.d supervision for `routerd-eventd` — NOT implemented (systemd
   only), and NOT documented.** `pkg/render/eventd_systemd.go` renders a systemd
   unit only; there is no rc.d equivalent for eventd. The ADR contains no rc.d /
   FreeBSD note for eventd. → Should be recorded (ADR or here) as an experimental
   platform limitation.
2. **`EventSubscription` `batchWindow` / `debounce` accepted but not honored by a
   precise timer.** Spec fields exist (`pkg/api/specs.go:1298–1303`), but
   `pkg/controller/eventsubscription/controller.go` is **poll-tick batched**
   ("poll + dedup … each tick", lines 4–8) with no precise batch/debounce timer.
   The fields are accepted config but currently informational. → NOT documented as
   a limitation; should be.
3. **Self-push / loop prevention — IMPLEMENTED (this is NOT a gap).** The ADR
   loop-prevention invariant is enforced: `pkg/eventd/outbox.go:78` only pushes
   locally-originated events (`SourceNode == nodeName`); received events are not
   re-pushed. Covered by `TestOutboxLoopPrevention`
   (`pkg/eventd/outbox_test.go`). The separate observer-side invariant ("a node's
   own captured address is not re-emitted as an observed event") belongs to the
   ARP/Clients observer, which is **Phase 4 and not on this branch**, so there is
   nothing to skip yet.
4. **Lab nodes left on the `515fe7e8` build.** The Phase 3 evidence
   (`...subscription-20260530.md`) records deploying `515fe7e8` to router03 +
   router05 but contains **no teardown / revert note**, so per the lab record
   those nodes are presumed left running the Phase 3 build. → Worth an explicit
   lab note (cleanup or intentional-leave).

## Build / test health (final gate)

All run on `event-federation` head `8c4821c8`:

- `gofmt -l pkg cmd examples` → **clean** (no files listed).
- `go build ./...` → **success**.
- `go test ./...` → **1880 passed in 95 packages** (exit 0). No failures.
- `make check-schema` → **passes** (no diff) — but see the schema-incompleteness
  finding above; check-schema does not detect missing Kinds.
- No pre-existing `cmd/routerd` networkd-env test failures were observed in this
  run.

## PR #49 relationship — options (factual; not a recommendation)

PR #49 (`gh pr view 49`): OPEN, **draft**, `cloudedge-mvp → main`, title
"CloudEdge MVP: hybrid routing and selective address mobility". Its content is a
**strict subset** of `event-federation` (head `713233b0` is an ancestor).
`event-federation` is 36 ahead / 0 behind `main` → **clean fast-forward possible**.

- **(a) Retarget/replace #49 with an `event-federation → main` PR.** Single PR
  carrying CloudEdge/SAM + EF Phase 1–3; #49 closed/superseded. One review, one merge.
- **(b) Merge `cloudedge-mvp` via #49 first, then `event-federation`.** Two-step:
  CloudEdge/SAM lands as its own merge, EF follows. More granular history; two
  review/merge cycles; #49 stays meaningful.
- **(c) Single experimental merge of `event-federation`.** Same end-state as (a)
  but framed as one experimental merge; #49 is closed as superseded.

In all cases #49's diff is fully contained in `event-federation`, and the FF is clean.

## Recommendation (finalized)

**Verdict: READY to merge to `main` as an experimental feature.** Build clean,
gofmt clean, 1880 tests green, golden unchanged, no provider-mutation path,
consistent experimental labeling, `make dist` ships `routerd-eventd`, packaging
needs no change. CloudEdge/SAM is 3-cloud lab-validated (PASS/clean) and EF
Phase 1–3 each have a PASS lab smoke (transport + subscription).

The pre-merge hygiene items from the stocktake have been RESOLVED in this same
pass (same branch, committed alongside this doc):

1. **Schema (MUST-FIX) — RESOLVED.** `EventGroup` / `EventPeer` /
   `EventSubscription` registered in `cmd/routerd-schema/main.go`;
   `schemas/routerd-config-v1alpha1.schema.json` regenerated to include them;
   `make check-schema` passes.
2. **ADR 0006 status (MUST-FIX) — RESOLVED.** Status/Context updated to Phase
   1–3 implemented + OCI×PVE clean (3-cloud parity); per-phase markers set; a
   `## Known limitations (experimental)` subsection added.
3. **Docs nav (MUST-FIX) — RESOLVED.** `how-to/event-federation-subscription`
   added to `website/sidebars.ts` (English/default sidebar; ja translation is a
   deferred follow-up, non-blocking — Docusaurus falls back to the source doc).
4. **OCI evidence link (SHOULD-FIX) — RESOLVED.** Corrected to the
   `-43a64c55` dir suffix in `cloudedge-sam-mvp-milestone.md`.
5. **Experimental gaps (SHOULD-FIX) — RESOLVED.** Documented in ADR 0006
   "Known limitations": systemd-only `routerd-eventd` (no FreeBSD rc.d yet);
   `batchWindow`/`debounce` accepted but poll-tick batched (no precise timer).

Remaining (non-blocking, tracked here):

6. **Lab teardown note.** router03/router05 were left on the `515fe7e8` build
   after the Phase 3 smoke (configs were restored to baseline; only the binaries
   were not reverted). Not a `main`-merge blocker; a lab-management note. Revert
   or re-pin those binaries at the next lab touch.
7. **i18n.** ja/zh translations of `event-federation-subscription.md` and a
   dedicated federation reference page are deferred follow-ups.

**Recommended merge-shape:** a single `event-federation → main` PR, with **PR #49
closed as superseded** (its `cloudedge-mvp` content is a strict ancestor/subset
of `event-federation`; clean fast-forward, 0 behind). This is the lowest-overhead
path and keeps one experimental landing. Option (b) — land `cloudedge-mvp` via
#49 first, then `event-federation` — is only worth it if a separate CloudEdge/SAM
history checkpoint is desired; it is not necessary.

**The merge itself and the PR #49 disposition are the maintainer's decision**
(release/merge-to-main is owner-gated). No tag; experimental.
