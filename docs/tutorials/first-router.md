---
title: Bring up the first lab router
sidebar_position: 2
---

# Bring up the first lab router

![Diagram showing a first router tutorial with DHCPv4 WAN, LAN DHCP, NAT44, validate, dry-run, live apply, and status checks](/img/diagrams/tutorial-first-router.png)

This tutorial gives an isolated LAN client a private IPv4 address, a gateway,
and an IPv4 path to an upstream network. It uses the complete example
[`examples/example-basic-ipv4-nat.yaml`](../../examples/example-basic-ipv4-nat.yaml).

It is a lab tutorial, not a copy-and-paste replacement for a household router.

:::danger Keep a recovery path
Use a VM console, serial console, or a separate management NIC. `ens18` and
`ens19` below are example names. Check yours with `ip -br link`, and never
guess which interface carries the management connection. The example uses the
private range `192.168.10.0/24`; change it if it overlaps with an upstream,
VPN, school, or management network.
:::

## What success looks like

```text
upstream network -- WAN -- routerd host -- LAN -- test client
                         DHCP + NAT
```

1. The upstream gives the WAN an IPv4 address with DHCP.
2. The router gives the LAN test client an address from `192.168.10.100` to
   `192.168.10.199`.
3. The test client uses `192.168.10.1` as its gateway.
4. NAT lets the client send IPv4 traffic through the one upstream address.

Read [Network basics](./network-basics.md) if words such as DHCP, gateway, or
NAT are new.

## 1. Download and adapt the complete lab file

```bash
# The installed release contains router.yaml.sample, not every repository example.
# Download the example from the same release as the installed routerd binary.
ROUTERD_VERSION="$(sudo routerd version | awk '{print $2}')"
curl --fail --location --output first-router.yaml \
  "https://raw.githubusercontent.com/imksoo/routerd/${ROUTERD_VERSION}/examples/example-basic-ipv4-nat.yaml"
```

If you already have a checkout at that same release tag, you can instead copy
`examples/example-basic-ipv4-nat.yaml` from the checkout.

Open `first-router.yaml` and change only these values before the first preview:

- `Interface/wan.spec.ifname`: the lab WAN interface name.
- `Interface/lan.spec.ifname`: the isolated LAN interface name.
- `192.168.10.0/24`, if it conflicts with any connected network.
- The public DNS servers if your lab must use different resolvers.

The file deliberately leaves the WAN externally owned and lets routerd manage
the LAN. It includes a DHCPv4 server, a NAT44 rule, and basic zone resources.

:::caution Firewall scope
routerd's firewall resources are groundwork, not a security certification.
Do not expose an Internet-facing router or rely on this example as the only
security boundary. Keep the first exercise behind an isolated virtual switch or
physical lab network.
:::

## 2. Validate and preview before a daemon exists

```bash
sudo routerd validate --config first-router.yaml

LAB_DIR="$(mktemp -d)"
sudo routerd apply --config first-router.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
rm -rf "$LAB_DIR"
```

These commands do not commit a network change. A validation failure is a reason
to edit the YAML, not to try a live apply.

## 3. Apply from the console

When the preview names the expected interfaces and artifacts, make the change
from the console or a separate management path:

```bash
sudo routerd apply --config first-router.yaml --once
```

For a persistent lab router, copy the reviewed file into the installed location
and start the service:

```bash
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 first-router.yaml /usr/local/etc/routerd/router.yaml
sudo systemctl enable --now routerd.service
```

## 4. Check the whole small path

On the router:

```bash
sudo routerctl get status
sudo routerctl describe DHCPv4Client/wan-dhcpv4
sudo routerctl describe DHCPv4Server/lan-dhcpv4
sudo routerctl describe NAT44Rule/lan-to-wan
```

On a client connected only to the lab LAN, renew its DHCP lease, then check:

```bash
ip route
ping 192.168.10.1
curl -I https://example.com/
```

If the final command fails, separate the problem: first check that the client
received an address and gateway, then that the WAN lease exists, then DNS.
Do not keep applying the file repeatedly while the cause is unknown.

## Next

- [Basic IPv4 NAT gateway](../config-examples/basic-ipv4-nat.md) — a guided
  explanation of this file
- [LAN-side services](./lan-side-services.md) — make the router answer local
  DNS names and add IPv6 only after IPv4 works
- [Basic NAT and firewall policy](./basic-firewall.md) — current firewall scope
  and safer next steps
