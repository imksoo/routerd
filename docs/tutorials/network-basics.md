---
title: Network basics before routerd
sidebar_position: 0
---

# Network basics before routerd

You do not need to know every networking term before trying routerd. This page
gives you the small mental model needed for the first tutorial.

## The picture to keep in mind

```text
Internet / school / provider network
                |
               WAN
                |
          [ routerd host ]
                |
               LAN
                |
       PCs, phones, game consoles
```

- **WAN** is the side that faces a provider, school, or upstream router.
- **LAN** is the side you control: a room, home, class, or test network.
- A **router** moves packets between those two sides. It is not the same thing
  as a Wi-Fi access point or an Ethernet switch.

## Six useful words

| Word | Plain-language meaning |
| --- | --- |
| IP address | A network address, like a delivery address for a device. |
| Gateway | The device a computer sends traffic to when the destination is outside its own LAN. In a small network this is usually the router. |
| DHCP | A service that automatically gives a device an IP address, gateway, and often DNS server. |
| DNS | A phone book that turns a name such as `example.com` into an IP address. |
| NAT | A way for many LAN devices to share one IPv4 connection to the outside. |
| `/24` | A short way to say that addresses such as `192.168.10.1` through `192.168.10.254` belong to one small IPv4 LAN. |

routerd writes these choices as YAML. A `Router` file is a box containing a
list of **resources**. Each resource has a `kind` (what it is), a
`metadata.name` (a label you choose), and a `spec` (the details).

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: my-lab-router
spec:
  resources:
    # Resources such as Interface, DHCPv4Server, and NAT44Rule go here.
```

You do not need to memorize the resource names. The tutorials introduce one
job at a time and link to the reference when you need it.

## Start safely

For a first experiment, use an isolated Ubuntu Server VM or a spare computer.
Keep a Proxmox/VM console, serial console, or a separate management NIC. Do not
start by changing the only router that carries your home, school, or work
connection.

`routerd validate` and `routerd apply --once --dry-run` are for checking a
file without committing network changes. `routerd apply --once` and
`routerd serve` can change the host network. Only run those live commands from
a console or an independent management path.

:::caution Firewall boundary
routerd is pre-release software. Its firewall resources are groundwork, not a
security certification. Do not expose an Internet-facing router or rely on a
single example as your only security boundary.
:::

## Next steps

1. [Install routerd](../install-and-upgrade.md) on an isolated Ubuntu Server VM.
2. [Run the safe first check](./getting-started.md) without changing its network.
3. [Build the first IPv4 lab router](./first-router.md) only after you have a
   console path.
