---
title: BGP / FRR Control Plane Integration Design
---

# BGP / FRR Control Plane Integration Design

This document captures the design of routerd's interaction with the FRR
control plane (vtysh, frr-reload.py, daemon sockets) for BGP and related
routing protocols.

## Problem statement

On Alpine Live ISO + similar FRR builds that disable TCP VTY listening
(`port=0` in `vty_serv_start()`), routerd's prior readiness gate based on
`tcp/2605` (bgpd VTY listen) is permanently false. The controller then
restarts FRR repeatedly instead of running `frr-reload.py` against the
rendered config, leaving FRR with no BGP instance configured (no
`router bgp X` stanza, no `tcp/179` listen).

Manual `frr-reload.py --reload /run/routerd/frr/routerd.conf` recovers the
state, proving that the rendered config is correct and frr-reload.py can
create a BGP instance from a no-instance state.

## OSS-confirmed facts (source-level)

- FRR `lib/vty.c` `vty_serv_start(addr, port, path)`: TCP listening is
  conditional on `port != 0`; the Unix `<daemon>.vty` socket is
  independent (`#ifdef VTYSH`). Distros that disable TCP VTY still expose
  the Unix sockets at `/run/frr/<daemon>.vty` or `/var/run/frr/<daemon>.vty`.
- FRR `tools/frr-reload.py` `is_config_available()`: readiness is
  determined by `vtysh -c "configure"` returning success and not reporting
  "configuration is locked". TCP VTY listen is not consulted.
- `frr-reload.py` handles "new BGP instance" as a net-new context
  (`lines_to_add`), so first-convergence from a no-instance state is in
  scope of the script.
- `--stdout` is logging redirect only; it does not change reload behavior.

## Design

### Readiness probe

The controller probes FRR control plane via a single `vtysh -c "show
running-config"` round trip:

- exit 0 → FRR control plane is reachable. The output is consumed both as
  the readiness signal and as the input to `runningConfigMatches`. One
  round trip serves two purposes.
- exit non-zero with `failed to connect to any daemons` → control
  unavailable. Retry in the same reconcile until the path-specific timeout
  expires, then surface `FRRControlUnavailable` and let periodic
  reconciliation try again.
- exit non-zero with other errors → record stderr in status, treat as
  control unavailable for retry purposes.

TCP-based gates are deleted. Unix socket file existence under
`/run/frr/<daemon>.vty` (and `/var/run/frr/<daemon>.vty`) is captured as a
diagnostic in status, never used as an authoritative gate, because the
file can be present while vtysh round-trip still fails during daemon
init or restart races.

### Reconcile flow

The FRR service state is the entry condition for every reconcile. The
controller treats "FRR is up" as something it must verify and restore on
every cycle, not as a one-time setup step. This is the lesson from the
v2007 hotfix where removing the wrong TCP VTY gate also removed the path
that ever started FRR on first boot.

```
1. Render /run/routerd/frr/routerd.conf and /etc/frr/daemons.
2. Inspect FRR service state via the platform service manager
   (`rc-service frr status` on Alpine / `systemctl is-active frr` on
   systemd platforms):
     - active/running → continue without restarting.
     - inactive/stopped → enable + start FRR.
     - failed → restart FRR.
     - unknown → log + treat as failed.
   This runs every reconcile, independent of whether /etc/frr/daemons
   changed.
3. If /etc/frr/daemons changed:
     enable + restart FRR (in addition to the state-driven action above).
     waitFRRControlReady(ctx, 30s).
4. else:
     waitFRRControlReady(ctx, 5s).
4. If readiness times out:
     status = FRRControlUnavailable (or FRRStarting while still within
     reconcile's local retry budget). Return Pending; periodic reconcile
     (15s default) retries naturally.
5. vtysh -C -f /run/routerd/frr/routerd.conf (syntax validation).
   If non-zero:
     status = FRRSyntaxInvalid (terminal until config corrected).
6. frr-reload.py --reload --stdout /run/routerd/frr/routerd.conf.
   On transient "configuration is locked" output, retry per the existing
   transient-lock backoff (500ms).
   On other non-zero exit:
     status = FRRReloadFailed, store stderr. Return Pending; retry on
     next reconcile.
7. runningConfigMatches via the same vtysh -c "show running-config".
   - rc 0 and contains the rendered `router bgp <asn>` stanza → Healthy.
   - rc 0 and the stanza is missing → mismatch → reload again (or
     escalate to FRRReloadIncomplete after N consecutive verify
     failures, retry).
   - rc non-zero (failed to connect) → FRRControlUnavailable.
```

`waitFRRControlReady` is a reusable helper used both by the
daemon-restart path (longer timeout) and the reload-only path (shorter
timeout). Internally it polls `vtysh -c "show running-config"` until
success or timeout, logging Unix socket file existence each iteration as
diagnostic.

### Status fields

The BGPRouter / BGPPeer status objects expose:

- `LastControlProbeAt`, `LastControlProbeError`: most recent vtysh
  round-trip outcome.
- `LastReloadAttemptAt`, `LastReloadStderr`: most recent frr-reload.py
  invocation, including transient-lock retries.
- `LastReloadDurationMs`, `TransientLockRetries`: operational metrics.
- `Phase` enum extended with:
  - `Healthy`
  - `Pending`
  - `Error`
- Reason / status codes:
  - `FRRStarting` (transient, within reconcile's local retry budget)
  - `FRRControlUnavailable` (timeout exceeded; periodic reconcile retries)
  - `FRRSyntaxInvalid` (terminal; user must correct rendered config)
  - `FRRReloadFailed` (retry on next reconcile)
  - `FRRReloadIncomplete` (reload returned success but runningConfig is
    still missing the rendered stanza; retry on next reconcile)
  - `Healthy`

### Timeout / retry budget

| Path | Timeout | Poll | Periodic reconcile |
|---|---|---|---|
| daemon restart → ready | 30 s | 1 s | inherits 15 s |
| reload-only → ready | 5 s | 500 ms | inherits 15 s |
| transient configure-locked retry | per attempt 500 ms | up to 3 retries | — |

There is no exponential backoff and no absolute fail threshold. Periodic
reconcile naturally retries forever; user intervention is on operator
judgment, surfaced via the explicit reason codes above.

### Duplicate `routerd serve` guard

`scripts/build-live-iso.sh` / `live-autostart.sh` must not start a second
`routerd serve` if one already owns `/run/routerd/routerd.sock`. The guard
keeps autostart idempotent, but the first autostart pass of a boot is also
the config handoff boundary. If a persisted OpenRC runlevel starts
`routerd serve` before USB config restore, `live-autostart.sh` must restart
that service after `apply --once` instead of treating the existing process as
success. That restart is logged with `reason=LiveISOStaleServeRestarted`.
The boot marker lives under `/run/routerd` so each boot re-evaluates this
handoff. Without the duplicate guard, two routerd controllers compete for the
FRR service lock (`flock` on rc-service / systemctl), causing the
`ERROR: frr stopped by something else` symptom seen in Phase 0 evidence.
Without the post-restore restart, the early `serve` process can miss the
restored config and leave BGP at the apply-once `Rendered` handoff state.

This is shipped in the same hotfix as the BGP gate change, as a separate
commit, for independent revert and changelog clarity.

### Healthy gate (AND semantics)

A BGPRouter is `Healthy` only when ALL of the following are observed:

- FRR service state is `active/running` per the platform service
  manager.
- All declared FRR daemons (per `/etc/frr/daemons`) are running, not
  `FAILED`.
- `vtysh -c "show running-config"` returns exit 0.
- `:179` is listening on the configured address (BGP daemon is serving).
- The output contains the rendered `router bgp <our-asn>` stanza.

If any condition fails, the controller surfaces a reason code (per the
status field list) and remains in `Pending` or `Error`. The status path
must never collapse to `Healthy` while FRR is down. The v2007 regression
- routerctl status reported `Healthy` while every FRR daemon was
`FAILED` - is exactly the failure mode this AND gate prevents.

## Acceptance criteria

- Alpine Live ISO boots → exactly one `routerd serve` → BGP `router bgp X`
  appears in `vtysh -c "show running-config"` and `tcp/179` listens
  without manual `frr-reload.py`.
- FRR service starting in `FAILED` state at boot is detected and
  recovered by the controller (no manual `rc-service frr start`).
- `routerctl status` never reports `Healthy` while FRR is down or while
  `:179` is not listening.
- Linux distro with TCP VTY enabled regresses neither.
- `runningConfigMatches` never treats `failed to connect` as match.
- All status reason codes above are produced under the corresponding
  failure mode.

## Test scenarios

1. Alpine first boot: no tcp/2605, vtysh succeeds, running-config minimal
   → reload executed, BGP instance created, `tcp/179` listening.
2. Linux distro first boot (tcp/2605 listens): reload executed; no
   regression in runningConfig diff or status.
3. Broken-state recovery: routerd binary upgrade onto a router that
   already has FRR running with no BGP instance → reload executed without
   manual intervention.
4. Vtysh transient `failed to connect` during daemon restart → controller
   waits within the readiness budget; once vtysh recovers, validate +
   reload proceeds.
5. Vtysh permanently failing → `FRRControlUnavailable` after timeout;
   periodic reconcile retries.
6. `vtysh -C -f` rejects syntax → `FRRSyntaxInvalid`, no reload, no
   churn.
7. `frr-reload.py` exits non-zero → `FRRReloadFailed`, retry next
   reconcile.
8. `frr-reload.py` exits zero but running-config still lacks the rendered
   stanza → `FRRReloadIncomplete`, retry next reconcile.
9. Configure-lock transient → existing transient-lock retry path
   completes successfully.
10. `live-autostart.sh` re-invocation while a serve process holds the
    socket → exits 0 without starting a second process.
11. Alpine Live ISO smoke test (release gate): boot a fresh ISO, observe
    autonomous BGP convergence.
12. Live ISO with a persisted `routerd` OpenRC default-runlevel entry:
    `routerd serve` may be started before USB config restore, but
    `live-autostart.sh` removes the default-runlevel entry and restarts the
    service after config restore + `apply --once`, logging
    `reason=LiveISOStaleServeRestarted`, so BGP reload still converges without
    manual `frr-reload.py`.
13. FRR service starting in FAILED state at boot: routerd must invoke
    `rc-service frr start` (or restart) and recover the daemons without
    manual intervention; status reflects the FAILED state until daemons
    are running.
14. Status accuracy: with FRR forcibly stopped (`rc-service frr stop`)
    after a previously Healthy state, the next reconcile must surface
    `FRRControlUnavailable` or `FRRServiceDown`, not `Healthy`. The
    BGPRouter status' `lastSuccessTime` must not advance during the
    failure window.

## FRR Issue #8403 (graceful-restart exit !=0)

FRR < ~8.4.x can have `frr-reload.py` exit non-zero for configs
containing `bgp graceful-restart`. Alpine Live ISO ships a recent FRR
release, but we should capture `frr -v` as part of Phase 0 evidence and
add a follow-up only if the shipped version is affected. We do not add
speculative version-detect code in the hotfix.

## Architecture follow-up (post-hotfix)

After the hotfix lands, extract the FRR probe / reload responsibility
into `pkg/frr/` with a `Prober` interface and a `DefaultProber` whose
`Probe`, `Validate`, `Reload`, and `RunningConfig` methods encapsulate
all vtysh / frr-reload.py invocations. The BGP controller then becomes a
thin dispatch over `Prober`, mock-testable in isolation, and reusable
from future controllers (OSPF, IS-IS, etc.).

The hotfix itself stays in the BGP controller for minimal diff, with a
clear migration plan into `pkg/frr/` in a subsequent release.

## References

- FRR `lib/vty.c` `vty_serv_start`, `vty_serv_un`
- FRR `tools/frr-reload.py` `is_config_available`, context-diff
- FRR docs: `docs.frrouting.org/en/latest/frr-reload.html`
- FRR Issue #8403 (graceful-restart exit code)
- VyOS `python/vyos/frr.py` (reference: reload without preflight probe)
- Phase 0 evidence on k8s-rt-02 (`/tmp/bgp-pre-reload/`)
