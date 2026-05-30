# CloudEdge SAM AWS x PVE Smoke Evidence

Date: 2026-05-29

Branch/build: `cloudedge-mvp`, `routerd f60e7d9a`

Result: PASS (clean — no manual workarounds; Azure-parity)

This validates Selective Address Mobility against a second public cloud (AWS VPC/EC2)
on the first run, with no AWS-specific code changes — the provider-secondary-ip
capture + de-assign hardening and the WireGuard stdin apply (from the Azure cycle)
generalized as designed. The only provider-specific work is provisioning-side
(AWS ENI secondary IP + EC2 source/destination check disabled).

## Topology

- Cloud client (AWS EC2): `10.88.60.7/24`
- On-prem client (PVE VM): `10.88.60.9/24`
- Cloud router (AWS EC2): primary `10.88.60.4/24`, ENI secondary capture `10.88.60.9`
- On-prem router (PVE, router07): `10.88.60.1/24` on `vmbr470`
- Overlay: `wg-hybrid`, `169.254.120.1/32` (cloud) ↔ `169.254.120.2/32` (on-prem)
- Region: ap-northeast-1. WireGuard: on-prem -> AWS public endpoint, persistent keepalive.

## AWS capture prerequisites (provisioning-side)

- ENI: primary `10.88.60.4`, secondary private IPv4 `10.88.60.9`.
- EC2 source/destination check: DISABLED (AWS equivalent of Azure NIC IP forwarding).
- routerd-cloud guest OS: `10.88.60.9` removed from local addresses by routerd
  (`provider-secondary-ip` + `configureOSAddress=false` de-assign enforcement).

## Assertions (all PASS)

- Cloud delivery route: `10.88.60.9 dev wg-hybrid metric 120`.
- On-prem: proxy ARP for `10.88.60.7`; delivery route `10.88.60.7 dev wg-hybrid metric 120`.
- Stage A: AWS router NIC tcpdump captured `.7 -> .9` ICMP request/reply.
- `.7 -> .9` ping 3/3 (0% loss); `.9 -> .7` ping 3/3 (0% loss).
- SSH bidirectional, source preserved:
  - `SSH_CONNECTION=10.88.60.7 ... 10.88.60.9 22`
  - `SSH_CONNECTION=10.88.60.9 ... 10.88.60.7 22`
- No NAT; both clients' default gateways unchanged.
- doctor hybrid: AWS side overall pass (pass 10 / warn 0 / fail 0 / skip 1);
  PVE side overall pass (pass 13 / warn 0 / fail 0 / skip 1).

## Notes

- No AWS-specific failures; no new issues filed.
- The Azure×PVE pair (router06) was untouched.
- Cost: EC2 instances stopped after evidence capture (kept for reruns); EIP/EBS remain
  until full teardown. Full local evidence bundle:
  `routerd-labs/cloudedge-sam/evidence/20260529T233145Z-aws-pve-f60e7d9a`.
