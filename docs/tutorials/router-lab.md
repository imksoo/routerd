---
title: Router Lab
---

# Router Lab

This tutorial explains the shape of `examples/router-lab.yaml`. It is a compact
router configuration that demonstrates the resources routerd is growing around.

The example is not meant to be pasted blindly onto a production host. Interface
names, prefixes, credentials, and upstream behavior are environment-specific.

## Topology

The lab model uses:

- `wan`: upstream Ethernet interface
- `lan`: downstream Ethernet interface
- DHCPv4 on WAN
- DHCPv6-PD on WAN
- static IPv4 on LAN
- dnsmasq for LAN DHCP/DNS/RA
- DS-Lite tunnel resources
- default route policy with health checks

## Validate And Inspect

```bash
routerd validate --config examples/router-lab.yaml
routerd plan --config examples/router-lab.yaml
```

The plan should show DNS/DHCP service only on the configured LAN interface. A
server must list the interfaces it serves through `listenInterfaces`; scopes are
rejected if they bind to an interface the server did not explicitly allow.

## DHCP And DNS

The lab config uses:

```yaml
kind: IPv4DHCPServer
spec:
  server: dnsmasq
  managed: true
  listenInterfaces:
    - lan
```

For IPv6, `dnsSource: self` selects the delegated LAN address such as
`pd-prefix::3`. dnsmasq advertises that DNS server through DHCPv6 and RA RDNSS,
which is important for Android clients.

## NTP And Syslog

The lab also demonstrates local system resources:

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

When `interface` is set, routerd renders the NTP server onto that systemd
networkd link. `LogSink` can send local routerd events to syslog locally or to a
remote syslog endpoint.

## Apply Carefully

On a real host:

```bash
sudo /usr/local/sbin/routerd reconcile \
  --config /usr/local/etc/routerd/router.yaml \
  --once \
  --dry-run
```

Only remove `--dry-run` after you have verified that routerd will not take over
interfaces still owned by cloud-init or another network manager.
