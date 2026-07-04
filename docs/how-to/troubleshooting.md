---
title: Troubleshooting
slug: /how-to/troubleshooting
---

# Troubleshooting

![Diagram showing routerd troubleshooting order from routerctl get status and plan intent to OS state, daemon sockets, events, and common DHCP, DNS, and conntrack checks](/img/diagrams/how-to-troubleshooting.png)

When investigating routerd, first separate **what routerd intends** from **what the host actually has**. Verify routerd's view, then compare against the OS state.

## Triage order

1. `routerctl get status` — overall view.
2. `routerctl describe <kind>/<name>` — focus on a specific resource.
3. `routerctl plan` — what would change next.
4. OS commands (`ip`, `nft`, `ss`, `journalctl`) — actual host state.
5. The relevant daemon's `/v1/status` and event log.

## DHCPv6-PD

```bash
curl --unix-socket /run/routerd/dhcpv6-client/wan-pd.sock http://unix/v1/status
tail -n 20 /var/lib/routerd/dhcpv6-client/wan-pd/events.jsonl
```

Look for:

- `phase` is `Bound`.
- `currentPrefix` is populated.
- `renewAt` is in the future.
- The event log shows `Reply` and `Renew` records.

If the prefix is **not** `Bound`, IPv6 RA, AAAA, and DHCPv6 should be paused on the LAN. routerd's safety contract is to stop advertising stale prefixes.

## DHCPv4

```bash
curl --unix-socket /run/routerd/dhcpv4-client/wan.sock http://unix/v1/status
```

Confirm `DHCPv4Client` is `Bound`. If you need an immediate renewal, `POST /v1/commands/renew` triggers it.

## dnsmasq

In current routerd, dnsmasq is scoped to DHCPv4, DHCPv6, DHCP relay, and Router Advertisement. DNS resolution is handled by `routerd-dns-resolver`.

Check that the generated dnsmasq configuration:

- Contains the expected `dhcp-range`.
- Has `port=0` (DNS handling disabled — that's `routerd-dns-resolver`'s job).
- References `dhcp-script=/usr/local/libexec/routerd/dhcp-event-relay` so lease changes are forwarded to routerd.
- Has `enable-ra` when RA is part of the configuration.

## DNS resolver

```bash
sudo curl --unix-socket /run/routerd/dns-resolver/<resource>.sock http://unix/v1/healthz
dig @<lan-ip> router.lan.example.org A
dig @<lan-ip> example.com A
```

Verify in this order:

- The resolver listens on the expected addresses and ports (`ss -lnup`).
- Local authoritative zones answer (manual records and DHCP-derived records from `DNSZone`).
- Conditional forwarders reach their target upstream (`dig @<lan-ip> <forwarded-domain>`).
- The default upstream answers via the expected protocol (DoH / DoT / TCP / plain UDP). Inspect `/v1/status` for the resolver status and upstream health.

## DS-Lite

```bash
ip -6 tunnel show
ip route show default
nft list table ip routerd_nat
```

If the AFTR FQDN does not resolve, check the `DNSResolver` `forward` source for the AFTR domain. Public DNS often cannot resolve AFTR records that are scoped to a specific access network.

## conntrack

Some environments do not expose `/proc/net/nf_conntrack`. In that case, routerd falls back to sysctl-derived counters. An empty per-flow list does not necessarily mean NAT is broken; check the `routerctl get connections` summary instead.

## Things to avoid during diagnosis

- Do not run an old DHCP client (or a manual test daemon) on the production WAN at the same time as routerd. Two clients sending DHCPv6-PD on the same interface can confuse the upstream's lease state.
- Do not flush `nf_conntrack` while changing routes; routerd intentionally avoids that, and clearing it kills established sessions.
- Do not edit `/usr/local/etc/routerd/router.yaml` on a host while ad-hoc YAML overrides exist elsewhere on the same host. Keep one canonical config file per host so reconcile remains predictable.

## See also

- [State and ownership](../concepts/state-and-ownership.md)
- [Reconcile loop](../operations/reconcile)
