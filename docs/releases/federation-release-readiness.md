# Federation Release Readiness

Entry point for the CloudEdge Event Federation release status.

## Phase completion

| Phase | Scope | Status | Evidence |
|-------|-------|--------|----------|
| Phase 1 | Event envelope, EventGroup, SQLite store, CLI | done | [checkpoint](event-federation-checkpoint.md) |
| Phase 1.5 | EventPeer, EventSubscription Kinds + validation | done | [checkpoint](event-federation-checkpoint.md) |
| Phase 2 | Peer delivery, HMAC, retry, prune | done | [transport evidence](evidence/cloudedge-event-federation-transport-20260530.md) |
| Phase 3 | Subscription → plugin → RemoteAddressClaim | done | [subscription evidence](evidence/cloudedge-event-federation-subscription-20260530.md) |
| Phase 4 | Provider actionPlan plugins, dry-run | done | [ADR 0007](../adr/0007-provider-action-execution.md) |
| Phase 5 | Provider action execution (gated) | done | [AWS](evidence/cloudedge-phase5-aws-provider-executor-smoke-20260530.md), [Azure](evidence/cloudedge-phase5-azure-provider-executor-smoke-20260531.md), [OCI](evidence/cloudedge-phase5-oci-provider-executor-smoke-20260531.md) |
| P1 | Federation pipeline observability (14 OTel metrics) | done | [observability how-to](../how-to/federation-delivery-observability.md) |
| P2 | Doctor federation checks, delivery summary | done | [changelog](changelog.md) |
| P3 | FederationSLO Kind, SLO JSON, remediation plan | done | PR #541 |
| **P4** | **Operational qualification & release candidate** | **in progress** | this document |

## Architecture references

- [ADR 0006: Event Federation](../adr/0006-event-federation.md)
- [ADR 0007: Provider Action Execution](../adr/0007-provider-action-execution.md)
- [Federation delivery observability](../how-to/federation-delivery-observability.md)

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
7. **security** — HMAC / timestamp / malformed event rejection
8. **multi-group** — per-group SLO isolation

Evidence template: [`evidence/federation-p4-operational-qualification-TEMPLATE.md`](evidence/federation-p4-operational-qualification-TEMPLATE.md)

## Auto-remediation readiness

See [federation-remediation-readiness-matrix.md](federation-remediation-readiness-matrix.md) for the P5+ readiness classification of all 7 remediation actions.

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
