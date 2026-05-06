---
title: Install
sidebar_position: 1
---

# Install

This page covers installing routerd from source on Ubuntu Server. NixOS and FreeBSD are supported as secondary platforms; for a first evaluation, an Ubuntu lab VM is the smoothest path.

## Ubuntu dependencies

Install the runtime and diagnostics packages first. (You can also declare the same set as a `Package` resource and let routerd surface missing items at startup.)

```bash
sudo apt-get update
sudo apt-get install -y \
  dnsmasq-base nftables conntrack iproute2 \
  iputils-ping iputils-tracepath dnsutils tcpdump traceroute \
  procps ppp wireguard-tools strongswan-swanctl radvd \
  systemd net-tools kmod
```

What each package does:

| Package | Purpose |
| --- | --- |
| `dnsmasq-base` | DHCPv4, DHCPv6, RA |
| `nftables` | NAT, route marks, stateful filtering |
| `conntrack` | Live IPv4/IPv6 connection observation |
| `iproute2` | Addresses, routes, DS-Lite, VRF, VXLAN, WireGuard devices |
| `ppp` | PPPoE (`pppd` + `rp-pppoe.so`) |
| `wireguard-tools` | `wg setconf` and state inspection |
| `strongswan-swanctl` | IPsec for cloud VPN endpoints |
| `radvd` | Optional radvd RA path (dnsmasq is the default) |
| `dnsutils` / `iputils-*` / `tcpdump` / `traceroute` | Verification and incident debugging |
| `procps` / `systemd` / `net-tools` / `kmod` | sysctl, service management, kernel module inspection |

## Build

```bash
make build
```

The main binaries:

- `routerd`
- `routerctl`
- `routerd-dhcpv6-client`
- `routerd-dhcpv4-client`
- `routerd-pppoe-client`
- `routerd-healthcheck`

## Install layout

The standard layout is rooted at `/usr/local`.

| Kind | Path |
| --- | --- |
| Configuration | `/usr/local/etc/routerd/router.yaml` |
| Binaries | `/usr/local/sbin` |
| Plugins | `/usr/local/libexec/routerd/plugins` |
| Runtime sockets | `/run/routerd` |
| Persistent state | `/var/lib/routerd` |

## First validation

After the build, sanity-check the schema and the bundled example:

```bash
make check-schema
make validate-example
make dry-run-example
```

Before applying anything to a production router, run a dry-run apply against the planned configuration:

```bash
routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan     --config /usr/local/etc/routerd/router.yaml
routerd apply    --config /usr/local/etc/routerd/router.yaml --once --dry-run
```

## Run under systemd

The main process runs as `routerd serve`. The DHCPv6-PD, DHCPv4, PPPoE, and healthcheck daemons are separate processes managed by routerd.

For first-time bring-up, make sure your management SSH path stays reachable before enabling automatic start. Keep an out-of-band channel (serial console, hypervisor console) ready before changing WAN-side settings.
