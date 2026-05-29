# CloudEdge / SAM — pre-merge stocktake (Azure×PVE + AWS×PVE smokes)

Date: 2026-05-29 · Branch `cloudedge-mvp` · Purpose: inventory of manual interventions,
config ergonomics, and routerd capability gaps observed during the two clean smokes,
to scope the experimental main merge and follow-ups.

## 1. Manual workarounds during smokes — ALL now routerd-native / resolved

| Workaround (manual at the time) | Resolution |
|---|---|
| Azure: secondary `/32` auto-added to guest OS (cloud-init/netplan) → `ip addr del` + suppress | **#41 / 439ec316** — provider-secondary-ip de-assign enforcement |
| Azure: `wg setconf <tempfile>` EACCES → `/dev/stdin` | **#43 / 439ec316** — WireGuard apply via stdin |
| Azure: stale `routerd_filter` nft table dropped forwarding → manual delete | **#42 / 439ec316** doctor warn + docs; **#47 / f60e7d9a** nft ownership diagnostics |
| `routerctl describe` lacked `-o` → plain output | **#45 / 40a99208** |
| AWS: secondary `.9` briefly OS-visible | **No manual step** — routerd de-assign (#41) handled it automatically (validates the fix generalizes across providers) |

→ Every smoke-time manual correction is now handled by routerd itself; the AWS run needed
none, confirming provider-generality.

## 2. Host/cloud bootstrap — manual (deployment gap, mostly outside routerd core)

- build/copy/install the routerd tarball, create/enable the systemd unit, place live config,
  run validate/dry-run/apply — manual. Future: lab bootstrap script / golden image; relates
  to the existing OS-bootstrap-automation finding. (Follow-up.)
- install runtime prerequisites (`wireguard-tools`, `tcpdump`, `jq`, `curl`) — manual; should
  be documented as routerd runtime prerequisites / handled in packaging. (Follow-up.)
- AWS: user-data apt hit a mirror-sync failure → manual `apt` retry (lab bootstrap fragility).
- AWS: PVE router07 DHCP/guest-agent assumption failed → recreated with static mgmt IP
  (PVE lab automation, not routerd).

## 3. Config ergonomics (config writability rough edges) — actionable

- **WireGuardPeer.allowedIPs must be hand-matched to the captured `/32` (+ overlay `/32`)** —
  implicit coupling with `RemoteAddressClaim`; easy to get wrong (the broad-allowedIPs issue).
  Candidate: validation / `doctor` cross-check that the WG peer allowedIPs cover each delivered
  `/32` (or auto-derive). **Highest-value ergonomics fix.** (Follow-up.)
- `nicRef`: Azure full ARM ID vs AWS ENI ID — provider-format differences, manual lookup,
  error-prone. Candidate: per-provider doc + light validation. (Follow-up.)
- `capture.interface` (proxy-arp) must be the real OS NIC name (ens21/eth1) — hand-found.
- overlay `/32`, shared subnet, `ownerSide`, and `domain.peerRef` vs `delivery.peerRef` must be
  hand-reconciled; the two peerRefs are partly redundant. (Follow-up: simplify/clarify.)
- `configureOSAddress=false` semantics were ambiguous pre-#41 (now clarified as "routerd
  enforces OS-local absence").
- `doctor` FORWARD-policy skip was hard to read on Azure (`exit status 1`); improved on AWS.

## 4. WireGuard key provisioning — manual

- private/public keys generated, placed, and public keys exchanged by hand; routerd only reads
  `privateKeyFile`. Candidate: generate-if-absent + expose the public key for exchange.
  (Follow-up.)
- (lab SSH key temporarily placed on clients for client-originated SSH evidence, then removed —
  test-harness only, out of routerd scope.)

## 5. Provider provisioning — manual BY DESIGN (routerd MVP scope-out)

- Azure: RG/VNet/subnet/NSG/public IP/NIC/VM/disk, NIC secondary `.9`, NIC IP forwarding,
  start/deallocate — manual, by design (no cloud API mutation in MVP; actionPlan /
  CloudProviderProfile is the future hook).
- AWS: VPC/subnet/IGW/route table/SG/EIP/EC2/ENI secondary `.9`, source/dest check disable,
  stop — manual, by design.
- PVE: VMs/bridges/NICs — lab infra, by design.

## Takeaways for the experimental merge

- The dataplane + the smoke-time corrections are routerd-native and validated on two clouds.
- Remaining manual work is either **by-design (provider provisioning, MVP scope-out)** or
  **experimental rough edges** (config ergonomics around allowedIPs/nicRef/peerRef/keys, and
  host bootstrap). These justify the **experimental** label and are tracked as follow-ups, not
  merge blockers.
