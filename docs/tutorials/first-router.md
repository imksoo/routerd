---
title: First router
sidebar_position: 2
---

# First router

This tutorial builds the smallest useful router YAML: one WAN interface
that gets an IPv4 address by DHCP, and one LAN interface with a static
address. After this you have a host that can talk upstream and can be
reached on a known LAN address. Adding LAN-side services
(DHCP / DNS / RA) and firewall comes in the next tutorials.

This page assumes you've followed [Install](./install) and have a
routerd binary in `/usr/local/sbin/`.

## 1. Identify the interfaces

Start with the physical shape of the machine:

```bash
ip link
```

A small router VM might have:

- WAN: `ens18`
- LAN: `ens19`

Use these kernel names only inside the `Interface` resource's
`spec.ifname`. Everywhere else you reference the resource by its
`metadata.name` (e.g. `wan`, `lan`). This lets you swap NICs without
touching the rest of the YAML.

## 2. Declare the interfaces

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: first-router
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: ens18
        adminUp: true
        managed: true

    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: ens19
        adminUp: true
        managed: true
```

Two `Interface` resources. `managed: true` means routerd takes ownership
of the interface configuration; `adminUp: true` means routerd brings the
link up.

## 3. Get an IPv4 address on the WAN

Add an `IPv4DHCPAddress` for the WAN:

```yaml
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4DHCPAddress
      metadata:
        name: wan-dhcp4
      spec:
        interface: wan
        client: dhclient
        required: true
```

`spec.interface: wan` references the `Interface` resource by name.
`required: true` means routerd will warn if the lease is missing.

## 4. Give the LAN a static IPv4 address

Add an `IPv4StaticAddress`:

```yaml
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata:
        name: lan-ipv4
      spec:
        interface: lan
        address: 192.168.10.1/24
        exclusive: true
```

`exclusive: true` says routerd should remove other addresses on this
interface that it does not know about. Drop it if other tools are
expected to add addresses.

## 5. Validate and dry-run

Save the file and run validate:

```bash
routerd validate --config /usr/local/etc/routerd/router.yaml
```

If validate is happy, plan the changes:

```bash
sudo routerd apply \
  --config /usr/local/etc/routerd/router.yaml \
  --once --dry-run
```

The output should show two Interface resources to manage, one DHCP lease
to acquire, and one static address to install. Nothing about NAT or
firewall yet — that's intentional.

## 6. Apply

```bash
sudo routerd apply \
  --config /usr/local/etc/routerd/router.yaml \
  --once
```

Verify:

```bash
routerctl get
routerctl describe interface/wan
ip addr show ens18
ip addr show ens19
```

`ip addr` should show a DHCP-assigned address on `ens18` and
`192.168.10.1/24` on `ens19`.

## What this does not do yet

The host can now reach the upstream and be reached on the LAN at
192.168.10.1, but it is not a router for LAN clients yet. There is:

- no DHCP server for clients on the LAN side,
- no LAN DNS service,
- no IPv4 NAT for client traffic going out the WAN,
- no firewall rules.

Each of those is a separate resource. The next two tutorials add them in
small steps:

- [LAN-side services](./lan-side-services) — add dnsmasq for LAN DHCP/DNS.
- [Basic firewall](./basic-firewall) — add NAT and a default-deny home
  router preset.
