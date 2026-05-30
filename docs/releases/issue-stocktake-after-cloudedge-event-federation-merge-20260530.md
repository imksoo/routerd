# Issue Stocktake — after CloudEdge SAM + Event Federation merge (2026-05-30)

Status: READ-ONLY stocktake. No issue was closed, commented, labeled, or created.
All actions below are **proposals** for the orchestrator/user to apply.

## Summary

PR #54 (`event-federation → main`) is **MERGED** (merge commit `baeaff16`). It is a strict
superset of `cloudedge-mvp` and lands CloudEdge/SAM + Event Federation Phase 1 / 1.5 / 2 / 3 as
**experimental** — no release tag, not recommended-stable. PR #49 (`cloudedge-mvp → main`) was
**CLOSED as superseded** by #54.

Four issues remain OPEN (#50, #51, #52, #53), all CloudEdge SAM and all carrying the now-stale
`branch cloudedge-mvp` label. Of these:

- **#53 and #50 are effectively resolved** by the merged zone-independent PMTU/MSS clamp
  (commit `3c540656`) — the codex OCI×PVE retest comment on both confirms PASS (routerd_mss
  present, MSS 1300, doctor hybrid PASS, ping/SSH/scp all pass). Safe to close.
- **#52 is partially resolved** — the merged `doctor hybrid` now *detects/warns* on reject-all
  FORWARD/INPUT host firewall, but the documentation deliverable (OCI image firewall bootstrap
  in the SAM how-to) is the remaining open part. Keep open as docs follow-up, or close-with-note
  if the doctor warning is deemed sufficient.
- **#51 (wizard OCI provider) is unaffected by the merge** — the wizard is a lab prototype, not
  core; no OCI provider generation was added. Keep open; it is a natural Phase 4.1 candidate.

No open or closed issue blocks the Phase 4.0 least-privilege plugin context framework (Plugin
context allowlist + secret redaction). That work is greenfield and should be a **new** issue.

## Merged baseline

- main = `baeaff16` (PR #54 merge commit).
- PR #54 MERGED 2026-05-30T12:20 — "Experimental: CloudEdge SAM + Event Federation Phase 1–3".
- PR #49 CLOSED (superseded by #54).
- Experimental: **no release tag**, not promoted to recommended-stable.
- Relevant merged commits:
  - `3c540656` — zone-independent PMTU/MSS clamp for SAM forwarded paths (#53) + doctor hybrid
    PMTU/firewall checks (#52). Touches `pkg/render/mtu.go`, `cmd/routerctl/doctor.go`, golden
    SAM fixtures, `docs/adr/0006-event-federation.md`.
  - `713233b0` — OCI×PVE SAM clean smoke + 3-cloud parity record.
  - Event Federation Phase 1→3 chunks (`9c785db8` … `515fe7e8`), Phase 2/3 smoke evidence
    (`docs/releases/evidence/cloudedge-event-federation-{transport,subscription}-20260530.md`).
- Closed-context issues: #41–#48 (and earlier #2–#40) closed during SAM/earlier work. Notably
  #12 ("MSS clamp can raise lower MSS / ignores source iface MTU") and #9 ("routerd_mss reported
  as orphan") are the historical MSS lineage behind #50/#53; #42 ("forwarded /32 dropped by
  FORWARD policy — doctor visualize") and #48 ("doctor hybrid classify FORWARD skip reasons") are
  the closed precursors to #52's doctor work.

## Issue classification table

| # | title | current status | effect of #54 merge | recommended action | safe to close? |
|---|-------|----------------|----------------------|--------------------|----------------|
| 53 | SAM OCI: TCP/SSH stalls after ping without MSS handling | OPEN, no labels; codex retest comment = PASS | **Resolved** by `3c540656` (zone-independent clamp → MSS 1300; OCI retest PASS) | **Close** (done-by-main-merge) | **Yes** |
| 50 | SAM: surface/derive PMTU/MSS for wg-hybrid delivery paths | OPEN, `enhancement` + `branch cloudedge-mvp`; codex retest = PASS | **Resolved** by `3c540656` (clamp derived + `doctor hybrid` PMTU/MSS warn); OCI retest PASS | **Close** (done-by-main-merge); relabel before/at close | **Yes** |
| 52 | SAM OCI: Ubuntu image iptables rejects WireGuard/FORWARD | OPEN, `documentation` + `branch cloudedge-mvp`; codex retest = doctor warns + lab bootstrap | **Partial**: doctor now warns (`3c540656`); the **docs how-to** part is still open | **Keep** as docs follow-up (or close-with-note); relabel | No (docs part) |
| 51 | cloudedge-sam wizard: add OCI provider support | OPEN, `enhancement` + `branch cloudedge-mvp` | **Unaffected**: wizard is lab prototype, no core change; OCI provider gen not added | **Keep** (still-relevant / Phase 4.1 candidate); relabel | No |

Taxonomy mapping:
- **done-by-main-merge**: #53, #50
- **docs-i18n / docs follow-up**: #52 (residual documentation part)
- **still-relevant / phase4.1-follow-up**: #51, and #52's doctor-FORWARD-pattern enhancement
- **phase4.0-blocker**: none
- **superseded-by-#54**: PR #49 (already closed); no issue
- **obsolete-duplicate**: none

## Recommended closures (draft comments)

### #53 — close as done-by-main-merge
> Resolved by `3c540656` (merged in PR #54 → main `baeaff16`). The PMTU/MSS clamp was gated on
> FirewallZone; SAM is a zone-less forwarding plane, so no clamp was derived and TCP black-holed
> on the OCI low-PMTU underlay while ICMP passed. The fix derives a FirewallZone-independent,
> interface-type-agnostic MSS clamp for RemoteAddressClaim delivery paths using the effective
> overlay MTU (≈1392 inner → MSS 1300). OCI×PVE retest PASS: `routerd_mss` present both sides
> (MSS 1300), `doctor hybrid` PASS, bidirectional ping/SSH (source preserved) and 100MiB scp x3
> all pass; 3-cloud clean parity. Closing. (Experimental — no release tag.)

### #50 — close as done-by-main-merge
> Resolved by `3c540656` (PR #54 → main `baeaff16`). SAM delivery paths now derive a scoped TCP
> MSS clamp, and `doctor hybrid` surfaces PMTU/MSS posture (warns when a SAM delivery path lacks
> a clamp) — exactly the two behaviors requested here. The OCI retest that previously had
> `routerd_mss` absent now emits MSS 1300 and passes ping/SSH/scp in both directions. Closing.
> (Relabel note: `branch cloudedge-mvp` is stale — that branch is merged and PR #49 closed.)

### #52 — keep open as docs follow-up (or close-with-note)
> Partially addressed by `3c540656` (PR #54 → main `baeaff16`): `doctor hybrid` now detects and
> warns on reject-all FORWARD/INPUT host firewall that blocks wg/overlay forwarding, surfacing
> the required host config rather than auto-mutating it. Retest confirmed: doctor warned before
> bootstrap, PASS after the scoped lab allow rules. **Still open**: documenting the OCI Ubuntu
> image firewall bootstrap prerequisites (UDP/51820 INPUT, FORWARD `<vnic> ↔ wg-hybrid`) in the
> CloudEdge SAM how-to. Keeping open as a documentation task; relabel `documentation` only.

### #51 — keep open (Phase 4.1 candidate)
> Not addressed by PR #54 — the cloudedge-sam wizard is a lab prototype, and no OCI provider
> generation was added to core during the merge. Keeping open. This folds naturally into the
> Phase 4.1 provider actionPlan plugin work (provider profile generation for aws/azure/oci).
> Relabel: drop `branch cloudedge-mvp`.

## Recommended relabels — `branch cloudedge-mvp` is stale

All four open issues (#50, #51, #52, #53) carry `branch cloudedge-mvp`. That branch is now merged
into main via PR #54 and the corresponding PR #49 is closed, so the label no longer points at a
live branch.

Proposed (do **not** apply here):
- #50, #53: remove `branch cloudedge-mvp` at close.
- #51: replace `branch cloudedge-mvp` → keep `enhancement` (optionally add a Phase 4.1 / cloudedge
  tracking label if one is introduced).
- #52: remove `branch cloudedge-mvp`, keep `documentation`.

Consider introducing a stable `cloudedge` or `event-federation` label to replace the
branch-scoped one for any future tracking.

## Recommended new / follow-up issues (drafts — not created)

1. **i18n: ja/zh translation of Event Federation how-to + reference**
   Translate the event-federation-subscription how-to and the federation reference page into
   ja (正本) and zh-Hans/zh-Hant per the docs locale policy. Currently English-only after Phase 3
   merge. (New issue; no existing match.)

2. **FreeBSD rc.d supervision for `routerd-eventd`**
   Phase 2 added EventGroup auto-supervision via controller/systemd (`1791cd5a`). Add the FreeBSD
   rc.d equivalent so `routerd-eventd` is supervised on FreeBSD routers (router04 parity).
   (New issue; no existing match.)

3. **EventSubscription batchWindow / debounce precise timer**
   Phase 3 EventSubscriptionController polls + dedups; add a precise debounce/batchWindow timer so
   bursty events coalesce deterministically before plugin invocation (ties to the ADR 0006
   hysteresis / anti-flap invariant). (New issue.)

4. **Observer self-capture invariant (Phase 4 loop-prevention)**
   Enforce the ADR 0006 invariant that a router never re-emits an event for an address it captured
   itself (loop prevention). Add a regression test/guard in the observe→federate path before
   provider plugins start mutating cloud state. (New issue; Phase 4 prerequisite.)

5. **Lab cleanup: router03 / router05 left on the `515fe7e8` binary**
   Phase 3 lab smoke binaries were left deployed on router03/router05. Track redeploying to the
   merged main artifact (or reverting to the recommended-stable build) so lab routers are not
   stranded on an experimental commit. (New issue; lab-cleanup.)

6. **Phase 4.1: provider actionPlan plugins (aws/azure/oci) — dry-run**
   Implement provider actionPlan plugins that turn RemoteAddressClaim into provider API calls
   (AWS/Azure/OCI secondary-IP assignment), starting dry-run/observe-only. **Subsumes #51**
   (wizard OCI provider generation) — link #51 as the OCI slice rather than filing a duplicate.
   (New issue; Phase 4.1.)

7. **Phase 4.0: Plugin context allowlist + secret redaction**
   Least-privilege plugin context framework. Redaction policy A: inline secrets redacted, secret
   file paths omitted, `SecretValueSourceSpec` omitted, no full `router.yaml` exposed, no provider
   credentials exposed, no provider mutation from the context layer. This is the **Phase 4.0
   blocker** for all provider-mutating plugins. (New issue; no existing match — greenfield.)

## Phase 4.0 blockers (explicit)

**None of the four open issues (#50–#53) block Phase 4.0** (least-privilege plugin context
allowlist + secret redaction; preventing accidental provider mutation / credential exposure).
A scan of closed issues (#2–#48) found **no** plugin-context, secret-exposure, or
credential-redaction issue either. The Phase 4.0 framework is greenfield work and should be filed
as new issue #7 above before any provider actionPlan plugin (Phase 4.1) is allowed to mutate.

## Phase 4.1 candidates

- **#51** — wizard OCI provider support → feeds provider profile generation; subsumed by the
  Phase 4.1 provider actionPlan plugins issue (draft #6).
- **#50 / #52 / #53** — already resolved, but their PMTU/firewall *provider-context* knowledge
  (effective overlay MTU, host FORWARD posture) informs what data the Phase 4.1 provider plugins
  need surfaced through the Phase 4.0 context allowlist. No reopen needed; reference as design
  input.

## No stable promotion / no release tag

The CloudEdge SAM + Event Federation work landed on main via PR #54 as **experimental only**.
There is **no release tag** and it is **not** promoted to recommended-stable. Release tagging
remains a user decision and is out of scope for this stocktake.
