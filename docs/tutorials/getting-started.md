---
title: Getting Started
---

# Getting Started

This tutorial walks through the smallest useful routerd workflow on a host
that has one WAN interface and one LAN interface. The flow is the same one
you would follow on a real router: confirm the physical layout, get the
WAN side talking to the upstream, give the LAN side an address, then
install routerd and let the daemon take over.

routerd is still v1alpha1 software. Run through this in a lab VM or on a
host you can reach through a console before pointing it at a remote
router.

## 1. Build routerd

```bash
make build
```

This produces:

- `bin/routerd`
- `bin/routerctl`

## 2. Identify the WAN and LAN interfaces

Start with the physical shape of the machine. In the examples below, `wan`
points at the upstream network and `lan` points at the downstream client
network. Replace the kernel names with whatever your host actually uses:

```bash
ip link
```

For example, a small router VM might use:

- WAN: `ens18`
- LAN: `ens19`

Inside the routerd config you stay on stable resource names like `wan` and
`lan`. The `spec.ifname` field is what binds those names to real OS
interfaces, so you can swap hardware without rewriting every reference.

## 3. First building block: WAN DHCPv4

The first useful pair of resources is an `Interface` plus an
`IPv4DHCPAddress`. Together they tell routerd to bring the WAN interface
up and ask the upstream network for an IPv4 address:

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
      kind: IPv4DHCPAddress
      metadata:
        name: wan-dhcp4
      spec:
        interface: wan
        client: dhclient
        required: true
```

The repository ships `examples/basic-dhcp.yaml` with the same shape.
Validate it and look at the dry-run output before changing any network
state:

```bash
routerd validate --config examples/basic-dhcp.yaml
routerd reconcile --config examples/basic-dhcp.yaml --once --dry-run
```

The dry-run output is JSON status: which resources are healthy, which
have drifted from the host, and what routerd would do.

## 4. Add the LAN address

Once the WAN side is understandable, add the LAN interface as a separate
building block. A minimal LAN side only needs an `Interface` and an
`IPv4StaticAddress`:

```yaml
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: ens19
        adminUp: true
        managed: true

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata:
        name: lan-ipv4
      spec:
        interface: lan
        address: 192.168.160.3/24
        exclusive: true
```

At this stage the host is not yet a full router for clients. DHCP service,
DNS, NAT, IPv6 prefix delegation, DS-Lite, PPPoE, and route policy are
later resources. Adding them one at a time keeps the plan output easy to
read.

## 5. Install the source layout

routerd defaults to a `/usr/local` layout:

```bash
sudo make install
sudo install -m 0644 examples/basic-dhcp.yaml /usr/local/etc/routerd/router.yaml
```

Important default paths:

- Config: /usr/local/etc/routerd/router.yaml
- Binary: /usr/local/sbin/routerd
- Plugins: /usr/local/libexec/routerd/plugins
- Runtime dir: /run/routerd
- State dir: /var/lib/routerd

## 6. Reconcile once

Always run one-shot mode before enabling the daemon:

```bash
sudo /usr/local/sbin/routerd reconcile \
  --config /usr/local/etc/routerd/router.yaml \
  --once \
  --dry-run
```

Drop `--dry-run` only after the plan looks the way you expect.

## 7. Enable the daemon

Install the systemd unit after the one-shot run looks correct:

```bash
sudo make install-systemd
sudo systemctl daemon-reload
sudo systemctl enable --now routerd.service
```

`routerd serve` keeps a control API socket under /run/routerd/ and runs
scheduled reconciles. From there you can use `routerctl status` and the
[control API](/docs/reference/control-api-v1alpha1) to ask the daemon
about what it sees.

## Next steps

- Read the [resource API reference](/docs/reference/api-v1alpha1) for the
  full list of behaviors you can declare.
- Try the [router lab tutorial](/docs/tutorials/router-lab) for a more
  realistic configuration.
- Browse the [resource ownership model](/docs/reference/resource-ownership)
  before letting routerd take over an existing router.
