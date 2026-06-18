{/* This file is a template — copy it, replace TEMPLATE with the run date, remove this comment, and fill in. */}

# Federation P4 Operational Qualification — Evidence

Fill in after running `scripts/cloudedge-federation-qualification.sh`.
Copy this file, replace TEMPLATE with the run date, and commit the filled version.

## Run metadata

| Field | Value |
|-------|-------|
| Run ID | `(runId from run-metadata.json)` |
| Commit | `(git short SHA)` |
| Date | `(YYYY-MM-DD)` |
| Branch | `feat/p4-federation-operational-qualification` |
| Harness | `scripts/cloudedge-federation-qualification.sh` |
| Cycles | `(N)` |
| Duration/scenario | `(N)s` |

## Topology

| Role | Host | Provider | Config digest |
|------|------|----------|---------------|
| Sender | `(hostname)` | `(pve/aws/azure/oci)` | `(sha256)` |
| Receiver | `(hostname)` | `(pve/aws/azure/oci)` | `(sha256)` |

## Scenario results

| # | Scenario | Result | Duration | Notes |
|---|----------|--------|----------|-------|
| 1 | healthy | | | Baseline delivery + doctor PASS |
| 2 | partition | | | Peer partition → violation → recovery |
| 3 | ttl-refresh | | | TTL refresh across partition boundary |
| 4 | restart | | | eventd restart recovery |
| 5 | subscription | | | Subscription failure + recovery |
| 6 | config-fault | | | Expected-peer / config fault detection |
| 7 | security | | | HMAC / malformed rejection |
| 8 | multi-group | | | Multi-group SLO isolation |

Overall: **(pass/fail)**

## Doctor snapshots

### Before qualification

```json
(paste doctorBefore from healthy.json)
```

### After qualification

```json
(paste doctorAfter from last scenario)
```

## Remediation plan (plan-only)

```json
(paste remediationPlan from healthy.json)
```

## Metrics verification

| Metric | Observed | Expected | Status |
|--------|----------|----------|--------|
| `routerd_eventd_delivery_total` | | &gt;0 | |
| `routerd_eventd_delivery_lag_seconds` | | within SLO threshold | |
| `routerd_eventd_repush_total` | | ≥0 | |
| `routerd_eventd_stale_ttl_total` | | 0 after recovery | |
| `routerd_eventd_accepted_total` | | &gt;0 | |
| `routerd_eventd_duplicate_total` | | ≥0 | |
| `routerd_eventd_reject_total` | | ≥0 (&gt;0 for security scenario) | |

## Evidence files

Machine-readable JSON evidence is in the `--evidence-dir` output directory:

- `run-metadata.json` — run parameters and topology
- `healthy.json` — baseline scenario
- `partition.json` — partition/recovery scenario
- `ttl-refresh.json` — TTL refresh scenario
- `restart.json` — eventd restart scenario
- `subscription.json` — subscription failure scenario
- `config-fault.json` — config fault detection scenario
- `security.json` — security rejection scenario
- `multi-group.json` — multi-group isolation scenario

## Security note

This document contains no secrets, HMAC keys, or endpoint credentials.
Config digests are SHA-256 hashes of the config file, not the config contents.
