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

## Initial checkpoint

Implemented: shared typed Health routing observation/policy, priority nftables
result publication after application, and a bounded DHCPv4 hardware-address read.
Focused regression checks and their executed results are recorded below before
publication. Remaining steps and real two-WAN communication are open.

### Executed checks (2026-10-29 JST)

Toolchain: Go 1.25.9 linux/amd64. No live router or production DB was changed.

- PASS: `make check-routerd15-focused` (four complete local packages, focused
  priority result/freshness regressions in chain, and vet of all five packages).
- PASS: Race Detector for healthcheck, egressroute, dhcpv4client and dynamicconfig;
  focused priority result/freshness checks in chain also pass with the detector.
- PASS: `make build-daemons`, using the project's CGO_ENABLED=0 Linux build path.
- NOT PASS: `make check-routerd15` includes the full chain suite. This environment
  prohibits Unix sockets and Netlink access. The unchanged baseline reproduces
  `TestLANAddressControllerPopulatesInterfaceBeforeDependencyCheck`,
  `TestDaemonStatusControllerStoresARPObserverDiagnosticsSeparately`, and the
  panic in `TestEffectivePolicyRouteExcludesWhenFalseDSLiteTargetWithoutMutatingSpec`.
  That baseline comparison establishes pre-existing failures here, not a full
  passing suite on an unrestricted Linux host. The full gate is retained.
- NOT RUN: actual two-WAN daemon/kernel forwarding, peer interoperability,
  physical NIC qualification and production cutover. Shell command fixtures test
  nft application ordering/failure propagation only; they do not execute nft.

The managed runtime status tool and policy snapshot were unavailable in this
session, so no VPN/private-lab readiness is assumed. Publication is a proposal
branch and draft PR; GitHub Actions completion is not awaited.
