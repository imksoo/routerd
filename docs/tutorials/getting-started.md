---
title: Getting Started
---

# Getting Started

This tutorial walks through the smallest useful routerd workflow on a host with
one WAN interface and one LAN interface. It starts by receiving a DHCPv4 address
on the WAN side, then adds a static LAN address, and only then moves toward
installing and running routerd.

routerd is still v1alpha1 software. Start in a lab VM or a host with console
access before applying it to a remote router.

## 1. Build routerd

```bash
make build
```

The build creates:

- `bin/routerd`
- `bin/routerctl`

## 2. Identify WAN And LAN Interfaces

Start with the physical shape of the machine. In the examples below, `wan`
points at an upstream network and `lan` points at the downstream client network.
Replace the interface names with the names from your host:

```bash
ip link
```

For example, a small router VM might use:

- WAN: `ens18`
- LAN: `ens19`

routerd configs use stable resource names such as `wan` and `lan`, while
`spec.ifname` maps those names to real operating system interfaces.

## 3. First Building Block: WAN DHCPv4

The first useful resource pair is an `Interface` plus an `IPv4DHCPAddress`.
This asks routerd to bring the WAN interface up and receive an IPv4 address from
the upstream network:

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

The repository includes the same shape in `examples/basic-dhcp.yaml`. Validate
and inspect it before changing any network state:

```bash
routerd validate --config examples/basic-dhcp.yaml
routerd reconcile --config examples/basic-dhcp.yaml --once --dry-run
```

The dry-run output is JSON status. It tells you which resources are healthy,
which are drifted, and what routerd would do.

## 4. Add The LAN Address

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

At this stage the host is not yet a full router for clients. DHCP service, DNS,
NAT, IPv6 PD, DS-Lite, PPPoE, and route policy are later resources. Keeping each
piece separate makes the plan easier to review.

## 5. Install The Source Layout

routerd defaults to a `/usr/local` layout:

```bash
sudo make install
sudo install -m 0644 examples/basic-dhcp.yaml /usr/local/etc/routerd/router.yaml
```

Important default paths:

- Config: `/usr/local/etc/routerd/router.yaml`
- Binary: `/usr/local/sbin/routerd`
- Plugins: `/usr/local/libexec/routerd/plugins`
- Runtime: `/run/routerd`
- State: `/var/lib/routerd`

## 6. Reconcile Once

Always run one-shot mode before enabling the daemon:

```bash
sudo /usr/local/sbin/routerd reconcile \
  --config /usr/local/etc/routerd/router.yaml \
  --once \
  --dry-run
```

Remove `--dry-run` only after the plan is expected.

## 7. Enable The Daemon

Install the systemd unit after the one-shot run looks correct:

```bash
sudo make install-systemd
sudo systemctl daemon-reload
sudo systemctl enable --now routerd.service
```

`routerd serve` keeps a local control API socket under `/run/routerd/` and runs
scheduled reconciliation.

## Next Steps

- Read the [resource API reference](/docs/reference/api-v1alpha1).
- Try the [router lab tutorial](/docs/tutorials/router-lab).
- Review the [control API](/docs/reference/control-api-v1alpha1) for status and
  operational tooling.
