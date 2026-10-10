# Federation Release Readiness

Entry point for the CloudEdge Event Federation release status.

## Phase completion

| Phase | Scope | Status | Evidence |
|-------|-------|--------|----------|
| Phase 1 | Event envelope, EventGroup, SQLite store, CLI | done | [checkpoint](event-federation-checkpoint) |
| Phase 1.5 | EventPeer, EventSubscription Kinds + validation | done | [checkpoint](event-federation-checkpoint) |
| Phase 2 | Peer delivery, HMAC, retry, prune | done | [transport evidence](evidence/cloudedge-event-federation-transport-20260530) |
| Phase 3 | Subscription/plugin → typed ownership facts → `MobilityPool` plan | done | [historical subscription evidence](evidence/cloudedge-event-federation-subscription-20260530) |
| Phase 4 | Provider actionPlan plugins, dry-run | done | [ADR 0007](../adr/provider-action-execution) |
| Phase 5 | Provider action execution (gated) | done | [AWS](evidence/cloudedge-phase5-aws-provider-executor-smoke-20260530), [Azure](evidence/cloudedge-phase5-azure-provider-executor-smoke-20260531), [OCI](evidence/cloudedge-phase5-oci-provider-executor-smoke-20260531) |
| P1 | Federation pipeline observability (14 OTel metrics) | done | [observability how-to](../how-to/federation-delivery-observability) |
| P2 | Doctor federation checks, delivery summary | done | [changelog](changelog) |
| P3 | FederationSLO Kind, SLO JSON, remediation plan | done | PR #541 |
| **P4** | **Operational qualification & release candidate** | **in progress** | this document |

## Architecture references

- [ADR 0006: Event Federation](../adr/event-federation)
- [ADR 0007: Provider Action Execution](../adr/provider-action-execution)
- [Federation delivery observability](../how-to/federation-delivery-observability)

## Qualification harness

The reusable qualification harness is at `scripts/cloudedge-federation-qualification.sh`.

```bash
scripts/cloudedge-federation-qualification.sh \
  --evidence-dir /tmp/fed-qual \
  --cycles 2 \
  --duration 300 \
  --scenarios healthy,partition,ttl-refresh,restart,subscription,config-fault,security,multi-group
```

8 scenarios are defined:

1. **healthy** — baseline delivery + doctor PASS
2. **partition** — peer network partition → SLO violation → recovery
3. **ttl-refresh** — TTL refresh re-push across partition boundary
4. **restart** — eventd restart recovery (sender + receiver)
5. **subscription** — subscription plugin failure + recovery
6. **config-fault** — expected-peer / config fault detection via doctor
7. **security** — correctly signed malformed/invalid body rejection and bad-HMAC rejection, receiver nonacceptance and current valid delivery
8. **multi-group** — per-group SLO isolation

## Federation qualification evidence

`scripts/cloudedge-federation-qualification.sh` requires positive cycles and all eight scenarios for release qualification. `--allow-skip` permits development subsets; their results never qualify a release. Each attempt uses a fresh evidence directory and event IDs, keeps command stdout/stderr/exit status and stops at the existing `--duration` budget per worker (including cleanup). A failure, incomplete observation or unconfirmed cleanup stops later faults and cycles without retries; retained fault markers require operator review before another attempt.

TTL refresh compares the current receiver event with the sender's extended `ExpiresAt`; an old delivered row or absent `staleTTL` is insufficient. Subscription failure/recovery uses this attempt's event ID and receiver-side post-injection time. Partition isolation records a healthy A/B baseline, current B delivery during the A fault, and group/peer-scoped SLO violations plus remediation with configured thresholds. Identical thresholds are allowed. Security uses correctly signed malformed bodies and a valid body with a bad signature: expected HTTP 400/401 plus receiver nonacceptance and a current valid delivery are required; transport errors and 5xx prove no rejection.

OTel availability, samples and labels are diagnostics: missing data remains inconclusive, never numeric zero or a product failure. The saved binary SHA-256 and product source commit are separate from the QA repository commit. Unknown or mismatched deployed source identity stops before scenario faults. Exit 1 records a measured assertion failure, 2 invalid input/setup, and 3 incomplete observation/provenance. These changes have offline boundary coverage; they do not establish provider or release qualification.

Evidence template: [`evidence/federation-p4-operational-qualification-TEMPLATE.md`](evidence/federation-p4-operational-qualification-TEMPLATE)

## Auto-remediation readiness

See [federation-remediation-readiness-matrix.md](federation-remediation-readiness-matrix) for the P5+ readiness classification of all 7 remediation actions.

Summary: 2 actions are **ready** for auto-execute (retry-failed-deliveries, force-repush-stale-ttl), 4 are **inspect-only**, 1 is **not ready** (configure-peer-endpoint requires operator approval).

## Documentation convergence

| Document | Status |
|----------|--------|
| ADR 0006 | Updated — P1-P3 reflected, FederationSLO Kind listed |
| ADR 0007 | Updated — Phases 5.0-5.1 marked DONE |
| Checkpoint | Historical note added |
| Changelog | P1-P3 + Phase 5 entries added to Unreleased |
| Observability how-to | Updated with P3 per-group SLO contract |

## Release criteria

- [ ] All 8 qualification scenarios PASS on at least one provider pair
- [ ] Doctor JSON output matches FederationSLO contract
- [ ] Remediation plan output is deterministic and diff-stable
- [ ] No secrets in evidence files
- [ ] Documentation converged (all rows above = Updated)
- [ ] CI green on qualification branch
- [ ] Evidence committed to `docs/releases/evidence/`

Development partition subsets that do not select multi-group require no B group. Go on the lab nodes is optional. `CE_BINARY_PROVENANCE_FILE` can point to an existing prepared/release contract: `routerdArtifact.commit` and `execution.candidate_binary_hashes` are bound to the current binary SHA-256. Conflicting runtime commit labels or mismatched hashes remain unconfirmed; a commit argument alone never establishes identity.

The eventd digest comes from the selected systemd unit's MainPID executable (`/proc/<pid>/exe`), with the process and executable checked again after acquisition. A missing or changing process remains unconfirmed; a binary found on PATH does not prove the running service identity. An empty successful OTel label response remains diagnostic inconclusive.

Existing offline assertions compare each WireGuard peer block and complete resource snapshots, the exact restored seven-peer topology for every leaf, seeded transition event identities/times, and transport peer/address associations. SSE request deadlines cancel blocked body reads. The active-stable guard requires the listed files and checks each version token separately. PoC bundles record schema validation as validated, unavailable, or invalid without requiring a new validator gate. Capture stop requires the existing four typed point records; missing or malformed state remains PARTIAL and performs no stop/copy calls. AWS fabric checks bind the requested secondary address to its longest matching route, retaining raw facts and PARTIAL/NOT-RUN. labctl builds only the matching clean current commit and records commit/tree identity; dry runs and prebuilt inputs do not claim a completed build or requested source use. RR stage proof rejects duplicate/conflicting required rows and report gates derive from checked outcomes. These are offline checks, not live qualification.
