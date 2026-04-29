---
title: Getting started
sidebar_position: 0
---

# Getting started

If you are new to routerd, work through these four short tutorials in
order. Each one is small, has a clear stopping point, and adds one
concept on top of the previous step.

1. **[Install](./install)** — build, install layout, dry-run, enable the
   daemon.
2. **[First router](./first-router)** — declare interfaces, get a WAN
   IPv4 address, set a LAN static address.
3. **[LAN-side services](./lan-side-services)** — managed dnsmasq for
   DHCP, DNS, RA. Optionally IPv6 prefix delegation.
4. **[Basic firewall](./basic-firewall)** — IPv4 SNAT and a default-deny
   home-router preset.

After these four you have a working small router and a feel for the
"add one resource, apply, verify" loop.

## Before you start

- routerd is still v1alpha1 software. Run through this on a lab VM or a
  host you can reach through a console before pointing it at a remote
  router.
- If you haven't already, skim the [concepts](../concepts/what-is-routerd)
  section. The tutorials assume you know what `apply`, `Resource`, and
  `metadata.name` mean.

## More involved tutorials

- [Router lab](./router-lab) — a more realistic full configuration once
  you've finished the four-step path.
- [NixOS getting started](./nixos-getting-started) — the NixOS path,
  using the Nix module instead of the systemd unit.
