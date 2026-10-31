---
title: Linux PPPoE runtime test
---

# Linux PPPoE runtime test

This opt-in fixture uses the release routerd worker and its supervised
routerd-pppoe-client, an independent rp-pppoe access concentrator, real pppd,
and the Linux PPP kernel. A disposable VM is required.

The product archive and authenticated Ubuntu ppp / pppoe packages must be
hashed before use. Extract packages into a private directory; do not install
them or start a shared service. All pppd shared libraries must already resolve.
Prepare the VM's PPP kernel prerequisites and record their previous state.

    sudo bash scripts/linux-pppoe-runtime-smoke.sh --disposable-vm \
      /root/FRESH-ROOT-OWNED-0700-DIR /root/release/bin /root/private-deps pap

Use chap for an independent CHAP trial. The work directory must be fresh.
The internal mode checks its PID, network and mount namespace boundaries before
any effect. The fixture isolates the client and peer in network namespaces and hides
/etc, /run, and /var/lib/routerd behind private mounts. It restores an
initially missing empty /usr/lib/pppd mountpoint after its child exits.
It checks negotiation, both traffic directions, actual controller publication,
stop/start, process completion, and PPP interface withdrawal. Final success is
printed only after namespace cleanup and outer interface comparison.

Preserve failures and inspect their actual cause before any repeat. The initial
fixture qualifies negotiated IPv6CP/link-local traffic only, not global IPv6 routing, default-route selection, provider
specific authentication, or uninterrupted availability. The legacy renderer's
IPv6 and route options must be traced separately from the helper's runtime
path; an IPv4 result does not qualify them.

The initial PAP attempt negotiated and passed bidirectional IPv4 traffic,
but the immediate restart did not connect within the original 20-second fixture
deadline. Its raw result remains a non-pass. The peer still held its single
session briefly; the current 70-second observation allows pppd's unmodified
persist/holdoff cycle and records actual connection time. A longer observation
does not imply immediate or uninterrupted recovery.

## Observed qualification

The release 9a90c9b31452de4b6be43c3a308e7f367fed4ccc was tested on an
isolated Ubuntu 24.04 VM with kernel 6.8.0-124-generic. Archive SHA256:
f393761a9646bc04da8bc8440ddf4dfaf5bf790c8c67713d5e8ac8e7d0ae5848.
The actual routerd and helper SHA256 values were
d6a9b57d40b5b2344069faff4b89e7d93879a23bbd433db030131f432dd9c762 and
c7dd6f2fd4e2f3849a4dcf9a53934b20d722174887533d9fbcb8eb13602e1517.

With fixture b1d86b0f, separate PAP and CHAP trials negotiated real sessions,
published the matching session through the actual controller and SQLite, and
passed three replies in each IPv4/IPv6 link-local direction before and after
reconnection. Immediate restart recovered through the existing persist cycle
in approximately 34 seconds. These are packet and caller results, not a
qualification of the Internet access path or uninterrupted availability.

The complete gate has not passed. In the PAP trial, the parent routerd returned
exit 0 before its DOWN PPP interface disappeared. The helper subsequently
saved Idle/Stopped. This is an ordering observation, not permanent interface
retention. The CHAP trial did not reproduce that ordering, but both fixtures
checked the independent peer's child too early after its server exited. The
current cleanup waits up to five seconds for only that peer namespace to drain.
The original non-pass results remain preserved.

After both trials, independent checks confirmed the original router was
Healthy; package inventory, original router files, shared APT files, outer
interface identity and kernel module names matched their initial values.
No trial PPP processes or IPsec SAs/policies remained.

## Boundary with the 1.0 implementation

The 1.0 baseline e3b49dbb and this release both launch the helper using pppd.
The helper options do not forward the API's IPv6/defaultRoute fields. The
legacy render.PPPoE writer has those options, but its old applyPPPoEConfig
function has no caller in that baseline. Link-local IPv6 worked through the
observed pppd defaults; this does not prove the API flag or global-route
behavior. Keep that remaining qualification separate from the packet results.

The independent peer termination sequence follows the native
[pppd signal contract](https://ppp.samba.org/pppd.html) and rp-pppoe's packaged
pppoe-server manual. No product lifetime, clock or protocol options are changed
to obtain the result.
