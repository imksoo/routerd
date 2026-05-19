---
title: Apply and render
slug: /concepts/apply-and-render
sidebar_position: 4
---

# Apply and render

There are a few common operations you will use day to day. This page settles the vocabulary used elsewhere in the documentation.

## Validate

`routerd validate` checks the YAML's shape: kind names, required fields, value ranges, obvious dependency errors.

```bash
routerd validate --config /usr/local/etc/routerd/router.yaml
```

## Plan

`routerd plan` shows what routerd is about to do to the host. Before pointing it at a production router, check the plan for anything that would cut the management connection or change a route unexpectedly.

```bash
routerd plan --config /usr/local/etc/routerd/router.yaml
```

## Dry-run

`--dry-run` runs through apply without changing the host, so you only see what would happen. routerd uses dry-run as the default for new controllers and for the early phase of any production rollout.

```bash
routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run
```

## Apply

`routerd apply --once` is a bounded host pass: it validates intent, observes the current host where needed, writes rendered artifacts, records state, and exits. It does not own long-running daemon lifecycle. Starting, enabling, restarting, or reloading managed daemons is the responsibility of `routerd serve --controller-chain`.

```bash
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once
sudo routerd serve --config /usr/local/etc/routerd/router.yaml --controller-chain
```

## Render

When this documentation says "render", it means routerd produces host-side files such as a dnsmasq configuration, an nftables ruleset, a systemd unit, or a NixOS module. Rendering alone does not necessarily change the host — whether the host is updated depends on the apply / dry-run flags.

In current routerd, dnsmasq is no longer responsible for DNS answering. dnsmasq renders DHCPv4, DHCPv6, relay, and RA configuration only. DNS listening, local zones, conditional forwarding, and encrypted DNS are handled by `DNSResolver`, which is the configuration shape for `routerd-dns-resolver`.

## Reconcile

In serve mode, routerd consumes events and re-evaluates only the affected resources. The shrinking-difference loop between intent and current state is what we call **reconcile** throughout these docs. For example, after a DHCPv6-PD renewal changes the prefix, the LAN address, RA, DNS answers, and DS-Lite path are reconciled in sequence.
