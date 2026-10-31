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
fixture qualifies negotiated IPv6CP/link-local traffic only. Global IPv6
routing, default-route selection, provider-specific authentication and
uninterrupted availability remain outside that qualification. The legacy renderer's
IPv6 and route options must be traced separately from the helper's runtime
path; an IPv4 result does not qualify them.

The initial PAP attempt negotiated and passed bidirectional IPv4 traffic,
but the immediate restart did not connect within the original 20-second fixture
deadline. Its raw result remains a non-pass. The peer still held its single
session briefly; the current 70-second observation allows pppd's unmodified
persist/holdoff cycle and records actual connection time. A longer observation
does not imply immediate or uninterrupted recovery.

## Observed qualification

The qualified product is full commit
`2b2793b21a8d1dada768757d108e56fd34344fba`, version `1.5-dev-2b2793b2`,
tested on Ubuntu 24.04 / kernel 6.8.0-124-generic. The fixture originated at
`3a66efa4724f1d03cd0148ee54667b38e0225b8d`; its bytes are identical in the
qualified product source. These are separate source identities.

The formal archive is 97,017,219 bytes, SHA256
`e3dc45eb315de11c2e98650993a3154595bfdbaf29d33f78066a6b650d2afb25`.
The actual routerd and helper SHA256 values are
`c629302f53bd857e192699250eba4eb813e0825b5e11648a8e88bc7b19a240f9` and
`c5843dffc2475cffbe1760742527e73887efcb84d68402cb658aa2fae023b2df`.
The executed fixture SHA256 is
`4137a8a538a7923d16e265e199195d46617bf84486c1e6a5c83208b9edebb903`.

| Trial | Initial and reconnected packets | Reconnect | Completion and restoration |
| --- | --- | --- | --- |
| PAP | IPv4/IPv6 link-local, both directions, 3/3 replies each | 33 seconds | PASS |
| CHAP | IPv4/IPv6 link-local, both directions, 3/3 replies each | 34 seconds | PASS |

Both trials used the actual Runner, supervisor, helper, pppd, status socket and
SQLite. The parent returned exit 0 after the PPP interface was absent. The peer
server returned exit 0 and its namespace drained within the bounded cleanup.
Final success was emitted after cleanup and outer interface comparison.

The earlier non-pass evidence remains preserved: the parent could return while
its DOWN PPP interface was still disappearing. No permanent interface retention
was established. The supervisor completion fix joins its existing child
cancellation/completion channels. The same strict parent-completion condition
passed with the qualified package.

Eighteen final independent checks passed. Original service PID and restart
count, configuration/binary hashes, shared APT files, package inventory,
interfaces, routes, rules, namespaces and module names matched the baseline.
The original router was Healthy without current controller errors. No trial
workers, capture processes, PPP processes, IPsec SAs or policies remained.
Raw header logs, 16 packet-probe logs, native transmit/receive snapshots, and
cleanup/restoration hashes are retained in the qualification manifest.

This is a qualification of the tested PAP/CHAP session paths. It does not cover
`authMethod: both`, global IPv6/default routes, provider-specific
authentication, Internet access, uninterrupted availability or FreeBSD.

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
