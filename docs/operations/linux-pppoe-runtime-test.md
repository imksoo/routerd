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
The fixture isolates the client and peer in network namespaces and hides
/etc, /run, and /var/lib/routerd behind private mounts. It restores an
initially missing empty /usr/lib/pppd mountpoint after its child exits.
It checks negotiation, both traffic directions, actual controller publication,
stop/start, process completion, and PPP interface withdrawal. Final success is
printed only after namespace cleanup and outer interface comparison.

Preserve failures and inspect their actual cause before any repeat. The initial
fixture does not qualify IPv6CP, IPv6 routing, default-route selection, provider
specific authentication, or uninterrupted availability. The legacy renderer's
IPv6 and route options must be traced separately from the helper's runtime
path; an IPv4 result does not qualify them.

Publication checkpoint: preparation only; no Linux PPP native pass is claimed.
