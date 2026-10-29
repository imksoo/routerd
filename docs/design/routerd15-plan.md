# routerd 1.5 proposal and implementation plan

Status: proposed on `proposal/routerd15-kiss-runtime`; not a released product.
Baseline: routerd `e3b49dbb69c7de97bab9595884745eb7cbe93d92`.

## Product decision

Improve routerd in place on a separate branch. Preserve its useful networking
behavior while removing duplicate state, conversions, unused execution paths,
and unnecessary abstractions. Configuration and event/state database compatibility
are explicitly not required. Cutover uses rewritten config and a fresh chosen DB;
there is no old-format migration project. Existing features are not implicitly
removed. A new package/process/framework is justified only by the feature's needs.

## Work sequence

1. **Health to PBR (initial implementation).** Decode current status at its
   boundary into a small typed observation. Share the threshold/freshness decision
   between advisory selection and priority routing, using one captured time.
   Publish priority routing results after nftables succeeds; report effect and
   status-write errors. Check stale observations, grace failures, fallback and
   application failure locally, then demonstrate two-WAN failover on an isolated
   VM with the product daemon. This first checkpoint retains the existing store
   and IPC boundaries; it does not claim to finish typed end-to-end transport.
2. **Typed observation transport.** Follow Health daemon -> status acquisition ->
   routing through actual callers. Replace string/map round trips with explicit
   messages and a single owner for each current observation. Update both IPC ends,
   API consumers and schemas together, deleting replaced representations. Keep
   validity/restart behavior explicit; do not add old producer support.
3. **Intent and read-only decisions.** Replace YAML cloning in dynamic config.
   Prepare static references with configuration and pass current observations to
   synchronous decisions. Separate effects so plan/dry-run no longer requires a
   cloned writable DB. Handle expiry, changes during waits and dependent withdrawal
   at their responsible operation; avoid a universal new resource engine.
4. **Persistence and recovery.** Choose one new schema for required leases,
   ownership and pending operations. Separate diagnostic history from control
   decisions. Preserve EventRule history semantics and DHCP callback recovery if
   those features remain enabled. Remove discarded readers/migrations and dual
   paths; test a fresh start and a restart using the new format.
5. **Protocol and OS feature paths.** Work through DHCPv4, DHCPv6-PD/RA, DS-Lite,
   PPPoE, DNS, NAT/firewall and the remaining enabled features. Repair packet bounds
   and exchange handling in the owning receive path; retain useful optional data.
   Reuse selected routerd2 fixes after reading their full callers. Remove unused
   old paths only after checking references and affected builds.
6. **Cutover and release.** Run the existing feature inventory against rewritten
   configuration and independent peers. Exercise restart, loss/return of upstream,
   renewal/expiry, managed-resource cleanup and management recovery. Record packet,
   client and kernel evidence. Publish a version only after its stated scope is
   demonstrated. No production cutover or existing DB deletion occurs implicitly.

Each step is a complete feature path with its obsolete code removed, not an
invitation to finish a new framework before trying actual communication.

## Local and VM checks

`make check-routerd15` runs the affected local Go packages. Ordinary tests must
not mutate the host network. Focused command fixtures can establish ordering and
failure propagation; they cannot establish packet forwarding.

The VM check needs an isolated Linux VM/netns with CAP_NET_ADMIN/CAP_NET_RAW,
Unix sockets, two upstreams and an independent TCP/UDP peer. Start the actual
routerd/Health processes, preserve the management path, send traffic, interrupt
one upstream and restore it. Observe routes/nftables and the traffic path; repeat
with worker restart and stale observations. Record actual setup and revision.
This cloud session has not established that VM access and does not claim this gate.

## Repository-wide implementation checkpoint

The scope is now the whole repository, rather than only Health/PBR. The coverage
record in [routerd15-audit.md](routerd15-audit.md) explicitly distinguishes source
inventory, semantic review, implementation and executed checks. Pending packages
are not declared compliant.

Implemented across existing callers:

- Concrete ResourceSpec types at YAML/JSON boundaries, shared generated decoding
  and ordinary deep copies; no JSON fallback to recover internal spec types.
- Typed current Health observation from the worker to persistence and routing;
  one current-status owner; worker restart restores history without asserting
  previous reachability; failure grace requires a previously healthy path.
- Explicit MTU resource handling; removal of unreachable standalone IPv4/Health
  apply paths and obsolete skipped inspection tests.
- One SQLite schema (user_version 1500) shared by state and the ownership ledger;
  no JSON persistence backend, migration chain, or no-op Save contract. Closed
  stores and invalid status encodings report errors.
- Error-returning state-variable writes, propagated through PD observations and
  ingress drain operations; expiry observation does not delete drain intent.
- Read-only apply planning, including dynamic parts and when filtering. Plan does
  not execute controllers, generate simulated events, create a temporary DB, or
  open ownership data for writing. Apply reacquires observations before effects.
- Cancellation passed to policy route observations and delayed derived events.
- Repository-wide command reachability cleanup, shared IPv4 frame bounds,
  DHCPv6-PD request retry/identity/timer fixes, validated RA reception and worker
  stop/signal ownership. The audit lists remaining protocol and state-write gaps.
- Example inventory tests use their own loopback metadata endpoints and fake
  provider CLIs; Azure inventory honors its configured metadata endpoint.

No released version, full semantic audit, or real network qualification is claimed.

The subsequent failure/recovery pass implements full DHCPv4 exchange ownership,
multi-prefix DHCPv6 and information requests, error-returning conditional reads,
serialized partial config updates, truthful nft/PF acknowledgement and withdrawal,
ICMP exchange correlation, DNS reload/cache/log ownership, current console views
and non-destructive installer behavior. The audit records the executed checks and
the remaining feature inventory work. A further pass connects handover release
to all native owners, journals BGP additions before effects, prepares validated
static declarations once per update, and gives PD one typed current observation.
Mutation responses distinguish committed intent from failed or unconfirmed
effects. These are implementation checkpoints, not completion of steps 3–6.

## Validation

Use go.mod's Go 1.25.9 toolchain and the Makefile targets. `check-routerd15`
retains the complete test/vet gate; `check-routerd15-focused` is a separate local
regression gate, never a substitute for full acceptance. Current executed results
and environment restrictions are tracked in the audit record. GitHub Actions
completion is not awaited.

`check-routerd15-recovery` runs the focused ownership, native-effect boundary,
current-declaration, handover and mutation regressions. It is included in the
focused gate. Its test-owned kernel-command observations and loopback services
do not replace the complete native suite or the VM/independent-peer gates.
