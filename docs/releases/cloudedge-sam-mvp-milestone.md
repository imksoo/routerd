# CloudEdge / Selective Address Mobility — experimental MVP, multi-cloud lab-validated

Status: **experimental** (lab-validated; NOT recommended-stable)
Branch: `cloudedge-mvp` · Date: 2026-05-29 (updated 2026-05-30: OCI added → 3-cloud parity)

## Summary

CloudEdge Selective Address Mobility (SAM) MVP is multi-cloud lab-validated across
**three clouds**. Azure×PVE, AWS×PVE, and OCI×PVE all passed the same-subnet /32
mobility smoke: a cloud VM (`.7`) and an on-prem/PVE VM (`.9`) communicated
**bidirectionally (ping + SSH + 100 MiB scp, source preserved) over a
routerd-to-routerd WireGuard overlay, without NAT and without changing either client's
default gateway**, while appearing to be on the same logical subnet.

This is **not full L2 extension**. SAM captures selected /32 IPv4 addresses and delivers
them over the overlay, preserving source/destination addresses.

## Validated

| Scenario | Result | Evidence |
|---|---|---|
| Azure×PVE same-subnet /32 mobility | PASS / clean | `docs/releases/evidence/cloudedge-sam-azure-pve-20260529.md` |
| AWS×PVE same-subnet /32 mobility | PASS / clean (Azure-parity, first run) | `docs/releases/evidence/cloudedge-sam-aws-pve-20260529.md` |
| OCI×PVE same-subnet /32 mobility | PASS / clean (after PMTU/MSS clamp fix #53) | `routerd-labs/cloudedge-sam/evidence/20260530T031247Z-oci-pve-hardening/summary.md` |

All three runs pass. AWS passed on the first run with **no AWS-specific code changes**.
OCI initially black-holed TCP (ping passed, SSH/scp timed out) on its lower-PMTU
underlay — exactly the failure #50 predicted — because the PMTU/MSS clamp was gated on
`FirewallZone`, which SAM (a pure forwarding plane) does not define, so no `routerd_mss`
clamp was derived on any cloud. The fix (#53) makes the clamp **FirewallZone-independent
and interface-type-agnostic**: it derives an MSS clamp for any forwarded delivery path
whose overlay tunnel MTU is a genuine step-down (effective overlay MTU via
`hybrid.EstimateMTU` → MSS 1300 on OCI). Home routers (PPPoE/DS-Lite) are unchanged
(no `RemoteAddressClaim` → empty forwarded-path set → identical zone output). After the
fix, OCI×PVE is clean with `routerd_mss` present both sides and `doctor hybrid` PASS.

## Proven abstraction

- **capture — provider-specific**: Azure NIC secondary private IP + NIC IP forwarding;
  AWS ENI secondary private IPv4 + EC2 source/destination check disabled; OCI VNIC
  secondary private IP + `skipSourceDestCheck=true`.
- **delivery / claim / doctor — routerd-common**: `RemoteAddressClaim` →
  `/32` delivery route over `wg-hybrid`; on-prem proxy-ARP return capture; no NAT;
  source/destination preserved; `routerctl doctor hybrid`. The provider-secondary-ip
  de-assign hardening and WireGuard stdin apply generalized across both clouds.

## What's in this branch (cloudedge-mvp, vs main)

- Dynamic-config foundation: `DynamicConfigPart` / mask directives /
  `DynamicOverridePolicy`; effective-config = startup + active dynamic parts − masks.
- Plugin runner (observe-only, dry-run): `Plugin` / `DynamicConfigSource` /
  `PluginResult`; actionPlans are display-only.
- L3 hybrid: `OverlayPeer` / `HybridRoute` (lowered into existing IPv4Route install).
- Selective Address Mobility: `AddressMobilityDomain` / `RemoteAddressClaim` /
  `CloudProviderProfile`; Linux dataplane (proxy-ARP capture + /32 overlay delivery +
  provider-secondary-ip OS-address de-assign), `routerctl doctor hybrid`.
- nftables ownership marking for stale-table diagnostics.

## Scope / known limitations (why experimental, not stable)

- No cloud provider API mutation (secondary IP assignment / route tables are
  provisioning-side / manual; actionPlans are display-only).
- SAM live dataplane is Linux-only.
- No full L2 / EVPN / BUM / broadcast-domain extension.
- GCP not yet validated (Azure / AWS / OCI validated; OCI added 2026-05-30).
- OCI Ubuntu images ship a default `iptables` reject-all FORWARD/INPUT that blocks the
  WG/overlay forward path (#52) — surfaced by `doctor hybrid`, fixed host-side (the host
  firewall is out of routerd core scope; routerd warns rather than auto-provisioning).
- Production topology variations not yet exercised.
- Config ergonomics rough edges and manual bootstrap/key steps remain (e.g. WG
  `allowedIPs` must be hand-matched to the captured `/32`; WireGuard keys and host
  package/systemd bootstrap are manual). See the pre-merge stocktake for the full
  inventory: `docs/releases/cloudedge-sam-stocktake-20260529.md`. All smoke-time manual
  *corrections* are now routerd-native (#41/#42/#43/#45/#47); the remaining items are
  by-design provider provisioning or tracked experimental follow-ups.

## Recommendation

Merge to `main` as an **experimental** CloudEdge/SAM MVP feature (documented as
experimental). Stable promotion / release tag are deferred until further validation.
