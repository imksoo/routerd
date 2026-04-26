---
title: Router Lab
---

# Router Lab

This tutorial walks through `examples/router-lab.yaml`, a compact
configuration that exercises the resources routerd is growing around. It
is meant for a lab VM with two interfaces, not for direct application to a
production router — interface names, prefixes, credentials, and upstream
behavior are environment-specific.

## What the lab declares

The lab config combines:

- a WAN Ethernet interface with DHCPv4 and DHCPv6 prefix delegation,
- a LAN Ethernet interface with a static IPv4 address and a delegated
  IPv6 address,
- dnsmasq serving DHCP, DNS, and RA on the LAN,
- DS-Lite tunnel resources reaching an AFTR over the WAN,
- a default IPv4 route policy with health checks across multiple
  uplinks.

You can read the YAML alongside the [resource API
reference](/docs/reference/api-v1alpha1) to see how each resource maps
back to a single behavior.

## Validate and inspect first

```bash
routerd validate --config examples/router-lab.yaml
routerd plan --config examples/router-lab.yaml
```

The plan should show DNS and DHCP service only on the configured LAN
interface. Each DHCP server resource lists the interfaces it is allowed
to serve through `listenInterfaces`; scopes that try to bind to an
interface the server does not list are rejected during planning.

## DHCP and DNS shape

The lab runs a single dnsmasq instance and binds it to LAN:

```yaml
kind: IPv4DHCPServer
spec:
  server: dnsmasq
  managed: true
  listenInterfaces:
    - lan
```

For IPv6, `dnsSource: self` makes dnsmasq advertise the delegated LAN
address (for example `pd-prefix::3`) as the DNS server. Because dnsmasq
puts that address into both DHCPv6 DNS and RA RDNSS, Android clients —
which only consume RA RDNSS — pick it up the same way as DHCPv6 clients.

## NTP and event sink

The lab also demonstrates the system-side resources:

```yaml
kind: NTPClient
spec:
  provider: systemd-timesyncd
  managed: true
  source: static
  interface: wan
  servers:
    - pool.ntp.org
```

When `interface` is set, routerd writes a per-link `NTP=` drop-in through
systemd-networkd for that interface. `LogSink` resources route routerd's
own internal events to local journald or syslog, or to a remote syslog
endpoint, without interfering with the rest of the configuration.

## Apply carefully

On a real host, always run a dry-run first:

```bash
sudo /usr/local/sbin/routerd reconcile \
  --config /usr/local/etc/routerd/router.yaml \
  --once \
  --dry-run
```

Drop `--dry-run` only after confirming that routerd is not about to take
over interfaces still owned by cloud-init or another network manager.
When in doubt, run `routerd adopt --candidates` first and follow the
[resource ownership workflow](/docs/reference/resource-ownership) to
record existing artifacts in the local ledger before applying.
