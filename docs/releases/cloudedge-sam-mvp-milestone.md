# CloudEdge / Selective Address Mobility — experimental MVP, multi-cloud lab-validated

Status: **experimental** (lab-validated; NOT recommended-stable)
Branch: `cloudedge-mvp` · Date: 2026-05-29

## Summary

CloudEdge Selective Address Mobility (SAM) MVP is multi-cloud lab-validated. Azure×PVE
and AWS×PVE both passed the same-subnet /32 mobility smoke: a cloud VM (`.7`) and an
on-prem/PVE VM (`.9`) communicated **bidirectionally (ping + SSH) over a
routerd-to-routerd WireGuard overlay, without NAT and without changing either client's
default gateway**, while appearing to be on the same logical subnet.

This is **not full L2 extension**. SAM captures selected /32 IPv4 addresses and delivers
them over the overlay, preserving source/destination addresses.

## Validated

| Scenario | Result | Evidence |
|---|---|---|
| Azure×PVE same-subnet /32 mobility | PASS / clean | `docs/releases/evidence/cloudedge-sam-azure-pve-20260529.md` |
| AWS×PVE same-subnet /32 mobility | PASS / clean (Azure-parity, first run) | `docs/releases/evidence/cloudedge-sam-aws-pve-20260529.md` |

Both runs are clean — no manual workarounds. AWS passed on the first run with **no
AWS-specific code changes**.

## Proven abstraction

- **capture — provider-specific**: Azure NIC secondary private IP + NIC IP forwarding;
  AWS ENI secondary private IPv4 + EC2 source/destination check disabled.
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
- OCI / GCP not yet validated.
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
