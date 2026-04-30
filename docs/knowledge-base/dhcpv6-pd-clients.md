# DHCPv6-PD client implementations and how to choose

routerd delegates WAN-side DHCPv6 Prefix Delegation to the operating system's DHCPv6 client.
This page summarises lab observations from a NTT FLET'S Hikari Next + PR-400NE home gateway
deployment, the differences between client implementations seen there, and why routerd's
NTT profiles recommend KAME/WIDE `dhcp6c`.

## Comparison

| Implementation | Licence | Upstream status | Behaviour under NTT NGN |
| --- | --- | --- | --- |
| KAME/WIDE `dhcp6c` (`wide-dhcpv6-client` / `net/dhcp6`) | BSD-style | Upstream `wide-dhcpv6` stopped at 2008-06-15. Distros patch-maintain. The `opnsense/dhcp6c` fork is BSD-3 and active | Carries IA Prefix lifetimes from the previous Reply into Renew/Rebind. Sends packet shapes similar to NEC IX commercial routers |
| systemd-networkd | LGPL-2.1+ | Active | Has been observed to send Renew/Rebind with IA Prefix `pltime=0 vltime=0` (systemd issue #16356). NTT HGW silently drops these as a "release-like" signal |
| dhcpcd | BSD 2-clause | Active | Handles IPv4/RA/DHCPv6/PD in one daemon. Lab tests show its Solicit includes Vendor-Class and ORO codes (e.g. opt_82, opt_83), so it is not the minimal shape that proves to work |
| odhcp6c | GPL-2.0 | Active under the OpenWrt project, rarely packaged outside OpenWrt | Widely used in OpenWrt. NTT FLET'S Cross (10G Hikari) deployments report eight-hour disconnections (OpenWrt issue #13454). Needs more validation under PR-400NE |

## routerd selection matrix

routerd keeps multiple client paths because the useful default differs by OS
and profile. `spec.client` may always be set explicitly. When it is empty,
`routerd apply` resolves a default for the current host. Lab runs can override
that choice for one apply with `--override-client` and `--override-profile`.

| OS | Client | NTT profile status | Notes |
| --- | --- | --- | --- |
| FreeBSD | `dhcp6c` | verified default | Current production path for FreeBSD. Uses KAME/WIDE `dhcp6c`; routerd keeps service changes idempotent to preserve client state. |
| FreeBSD | `dhcpcd` | known-bad warning | Lab test produced DUID-LLT when the DUID file was removed, and still received no HGW response after forcing DUID-LL. See Section L of the acquisition notes. |
| Ubuntu/Linux | `dhcp6c` | verified default for `ntt-*` | Current Linux NTT-profile default. Avoids observed systemd-networkd Renew/Rebind lifetime 0/0 packets. |
| Ubuntu/Linux | `networkd` | known-bad warning for `ntt-*` | Useful for generic Linux PD, but measured Renew/Rebind packets with IA Prefix lifetime 0/0 against the NTT HGW. |
| Ubuntu/Linux | `dhcpcd` | candidate | Renderer exists for lab use. Keep explicit until acquisition and Renew are verified for the target environment. |
| NixOS | `dhcpcd` | candidate default for `ntt-*` | Chosen because nixpkgs does not currently provide a straightforward WIDE `dhcp6c` package path. Needs additional lab confirmation. |
| NixOS | `networkd` | known-bad warning for `ntt-*` | Same networkd Renew/Rebind lifetime concern as other Linux hosts. |

Known-bad entries are not validation errors. routerd emits an apply warning and
a `KnownNGCombination` event, then continues. This keeps controlled experiments
possible while making the risk visible.

## Behaviour of NTT home gateways (PR-400NE family)

From lab observations and public documentation, the working model is:

1. **Acquisition window after reboot.** After a HGW reboot the LAN-side DHCPv6 server takes
   a few minutes to begin answering fresh Solicits. Once ready, even the minimal Solicit form
   gets Advertise/Reply quickly.
2. **Renew/Request always honoured during normal uptime.** Renew packets that include the
   Server Identifier and the existing IA_PD Prefix are answered by the HGW regardless of time.
   This was confirmed by observing a NEC IX router refresh its lease at the T1 boundary.
3. **`pltime=0 vltime=0` IA Prefix is silently dropped.** The HGW treats this as the client
   indicating it no longer wants the prefix. systemd-networkd has been observed sending Renew
   with `pltime=0 vltime=0` and getting no Reply.
4. **Reply source UDP port is ephemeral.** Advertise/Reply arrives at UDP destination 546 but
   the source port is not 547 (observed example: 49153). Captures filtered on `udp port 547`
   alone miss the responses; use `udp port 546 or udp port 547`.

## Failure modes

| Symptom | Where to look | Likely cause |
| --- | --- | --- |
| Solicit gets no reply right after HGW reboot | Acquisition window not yet open | HGW still preparing. Wait a few minutes and re-evaluate |
| Solicit gets no reply during normal uptime | Solicit has no Server Identifier | Client lost its lease state. Recovery typically requires a HGW reboot to reopen the acquisition window |
| Renew gets no reply during normal uptime | Renew with Server Identifier present, but no Reply | Inspect IA_PD Prefix lifetimes. If `pltime:0 vltime:0` then the client is silently dropped |
| HGW lease table has the MAC but the LAN side has no prefix | OS-side lease pickup is broken | Example: networkd + netplan logging `Could not set DHCP-PD address: Invalid argument` |

## How routerd handles this

- The default Linux client is `systemd-networkd`. NTT profiles (`ntt-ngn-direct-hikari-denwa`,
  `ntt-hgw-lan-pd`) recommend `IPv6PrefixDelegation.spec.client: dhcp6c` so that KAME/WIDE
  `dhcp6c` is used instead.
- FreeBSD always uses `dhcp6c` from `net/dhcp6`. Base `dhclient` does not handle DHCPv6-PD.
- `routerd apply` preserves the OS DHCPv6 client's in-memory lease. Services are not restarted
  unless rc.conf or drop-in files actually change.
- Observability does not treat a derived LAN address as proof of a healthy PD lease.
  Last Reply timestamp, OS client lease state, and `routerctl describe ipv6pd/<name>`
  output are the planned sources of truth.

## 2026-04-30 lab findings (PR-400NE, sustained uptime)

A multi-VM lab session against a continuously running PR-400NE produced the
following evidence-graded notes. They expand on the points above and motivate
a separate page on the bootstrap path: see
[NTT NGN HGW PD acquisition under sustained uptime](./ntt-ngn-pd-acquisition.md).

- **observe**: 34 distinct Solicit variants (OUI sweep, Vendor-Class, User-Class,
  hop-limit and flow-label permutations, prefix hint with and without IA address,
  RS→RA→Solicit ordering, retransmit cadence variants) all failed against a HGW
  that had not been rebooted recently. None received Advertise or Reply.
- **observe**: At the same instant, a NEC IX2215 on the same LAN refreshed its
  existing lease on every T1 boundary (11/11 successes during the test window).
  The HGW's DHCPv6 server is therefore healthy; only the new-binding path is
  blocked.
- **believe**: The HGW separates an "acquisition window" (open for a few minutes
  after a HGW reboot, during which fresh Solicits are answered) from a
  "steady state" (during which only Renew/Request/Confirm/Information-Request
  with a known Server Identifier are answered). New Solicits in steady state
  are silently dropped.
- **measure**: Sending a synthesised `Request` (msg-type 3) carrying the HGW's
  `Server Identifier` plus an `IA_PD` claim from a routerd lab VM produced an
  immediate `Reply` with a fresh /60 binding — confirming that the HGW does
  honour the RFC 8415 §18.2.10.1 INIT-REBOOT-style Request even when its
  Solicit path is blocked. See the dedicated page for transcripts.
- **assert**: routerd's WAN profile must be able to drive Solicit, Request,
  Renew, Rebind, Release, Confirm and Information-Request directly. Stock OS
  clients limit themselves to the canonical Solicit→Advertise→Request bootstrap
  and cannot recover from this state without a HGW reboot. This is a primary
  motivation for the routerd active controller path described in
  `docs/design-notes.md` Section 5.2.

## Renew acceptance hypothesis (2026-04-30)

Byte-level captures of three Renew implementations side by side support the
following hypothesis. routerd should treat it as the working contract until
contradicted.

- **observe**: A unicast Renew (UDP 547 to the HGW global address) is always
  answered, but the answer is a `status-code 5 (UseMulticast)` Reply within ~4 ms.
  WIDE `dhcp6c` re-sends the same `xid` over multicast to ff02::1:2 and the HGW
  silently drops the retry — believed to be xid-replay suppression.
- **measure**: A multicast Renew that succeeded (IX2215, 11/11) had:
  `T1=7200`, `T2=12600`, `IA_PD` carrying the bound prefix with non-zero
  `pltime`/`vltime`, the `reconfigure-accept` option (20), and a fresh `xid`.
- **measure**: A multicast Renew that failed (WIDE `dhcp6c` after the
  UseMulticast bounce) had matching T1/T2 and IA_PD lifetimes but no
  `reconfigure-accept` option and the same xid as the failed unicast attempt.
- **measure**: A multicast Renew that failed (routerd active controller during
  the same session) had `T1=0`, `T2=0`, no `reconfigure-accept` and a fresh xid.
- **believe**: The HGW's multicast Renew acceptance test is approximately
  "fresh xid AND non-zero T1/T2 AND `reconfigure-accept` option present". Any
  one of these missing causes silent drop. This must be re-validated by an
  ablation pass; until then, routerd sends Renew with all three.

## DHCPv6 client implementation gotchas (2026-04-30)

- **WIDE `dhcp6c`** sends Renew as unicast first when it has cached the server
  link-local address. Result: the UseMulticast bounce path above. routerd's
  profile sets `dhcp6c` to multicast Renew where possible, and treats unicast
  Renew as a deprecated path.
- **WIDE `dhcp6c`** does not include `reconfigure-accept` in Solicit or Renew it
  emits. Its receive path for option 20 is implemented but the send path is not.
  Patching the send path or running routerd's active controller is required.
- **WIDE `dhcp6c`** stores its DUID at `/var/db/dhcp6c_duid` (FreeBSD) or
  `/var/lib/dhcp6/dhcp6c_duid` (Ubuntu). When forcing a `DUID-LL` (type 0x0003),
  `hardware-type` must be `0x0001` (Ethernet) — anything else violates the
  expectations the HGW enforces on NTT NGN.
- **systemd-networkd** can emit Renew/Rebind with IA Prefix `pltime=0 vltime=0`
  (systemd issue #16356). The HGW treats that as a release-like signal and
  silently drops the request, breaking the binding without informing the client.
  routerd's NTT profiles route around networkd by selecting `dhcp6c`.

## Server Identifier derivation from RA

- **observe**: The HGW's DHCPv6 `Server Identifier` is a `DUID-LL` constructed
  as `0003 0001` (DUID type 3, hardware type Ethernet) followed by the HGW's
  LAN-side MAC address (lab example `1c:b1:7f:73:76:d8`).
- **observe**: The same MAC address is exposed in the source link-local address
  of every Router Advertisement the HGW emits, encoded as modified EUI-64.
- **assert**: routerd derives the expected `Server Identifier` by listening for
  a single Router Advertisement, recovering the MAC by inverting the
  modified-EUI-64 transform on the source link-local, and prepending
  `0003 0001`. Operators may override this in the resource spec when needed.
- **assert**: This means routerd can recover from a fully cold start with no
  prior DHCPv6 transaction history: receive RA, compute Server-ID, send
  Request, receive Reply — provided the HGW honours Request without prior
  Solicit (see `ntt-ngn-pd-acquisition.md`).

## Reconfigure key handling

- **observe**: The HGW's first PD Reply after a fresh acquisition includes an
  `Authentication` option with `proto=reconfigure`, `algorithm=HMAC-MD5`,
  `RDM=mono` and a 16-byte `reconfig-key value`. This is the key the HGW will
  use to authenticate any future Reconfigure (RFC 8415 §18.2.10) it sends.
- **observe**: Subsequent Renew Replies on the same binding may omit the
  Authentication option entirely.
- **assert**: routerd must persist the Reconfigure key from the first Reply and
  treat it as authoritative until a new Reply explicitly rotates it. The key
  is held in `objects.status._variables` next to the rest of the lease state,
  not exposed in the public resource view.

## References

- systemd issue #16356 — DHCPv6 Renew not resetting valid/preferred lifetimes
- OpenWrt issue #13454 — odhcp6c eight-hour disconnections under NTT 10G Hikari (FLET'S Cross)
- OpenWrt forum: "Server Unicast DHCPv6 option causes my ISP to ignore Renew packages"
- `opnsense/dhcp6c` — actively maintained BSD-3 fork of the KAME WIDE-DHCPv6 client
- NEC UNIVERGE IX FLET'S IPv6 IPoE configuration guide — example of a working commercial router on the same network
- RFC 8415 §18.2.1 (Solicit), §18.2.4 (Request), §18.2.5 (Confirm), §18.2.6 (Renew),
  §18.2.10 (Reconfigure), §18.2.10.1 (INIT-REBOOT considerations)
- rixwwd public lab notes — observation that PR-400NE Reply uses ephemeral source UDP port (e.g. 49153)
- sorah public lab notes — DUID-LL composition observed under PR-400NE
