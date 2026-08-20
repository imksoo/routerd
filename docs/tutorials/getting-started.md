---
title: Getting started safely
---

# Getting started safely

![Diagram showing the safe first routerd loop from interface discovery and a small YAML config to validate, dry-run, live apply, serve, and routerctl get status](/img/diagrams/tutorial-getting-started.png)

This is the first route through routerd for someone learning how routers work.
It deliberately starts with a file check, not with a change to a network.

:::caution Use a lab for the first live change
Use an isolated Ubuntu Server VM or a spare computer. Keep a hypervisor,
serial, or physical console open, or keep a separate management NIC that this
configuration does not change. Do **not** start by changing the only router
for your home, school, or work network.

`routerd apply --once` and `routerd serve` can change network state. The first
two checks below do not commit network changes when you use the temporary paths
shown here.
:::

Before continuing, read [Network basics](./network-basics.md). It explains WAN,
LAN, DHCP, DNS, NAT, and the `/24` notation used in the examples.

## 1. Install on an Ubuntu Server lab host

Ubuntu Server is the primary, most exercised target. Install a release archive
with [Install and upgrade](../install-and-upgrade.md). FreeBSD and NixOS have
groundwork and selected integration paths; they are not the recommended first
lab target.

## 2. Find the real interface names

```bash
ip -br link
```

Examples often use `ens18` for WAN and `ens19` for LAN. Those are placeholders,
not universal names. Do not adopt the interface that carries your only SSH
connection during a first experiment.

## 3. Start with a small description

Save this as `first-router.yaml`. It only describes two existing interfaces;
the next tutorial adds a complete LAN DHCP and NAT lab.

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
        ifname: ens18 # replace with the lab WAN interface
        adminUp: true
        managed: false
        owner: external

    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: ens19 # replace with the lab LAN interface
        adminUp: true
        managed: true
        owner: routerd
```

Each item has a `kind` (what it is), a `metadata.name` (the short name used by
other items), and a `spec` (the desired settings).

## 4. Check the YAML without a running daemon

`routerd validate` reads the file directly. It is the right first command when
`routerd.service` is not running yet.

```bash
routerd validate --config first-router.yaml
```

## 5. Run a non-destructive preview

Use temporary state, ledger, and status paths. This exercises loading,
dependency ordering, and rendering without applying the network change or
writing routerd's normal state files.

```bash
workdir=$(mktemp -d)
routerd apply --config first-router.yaml --once --dry-run \
  --state-file "$workdir/state.db" \
  --ledger-file "$workdir/ledger.db" \
  --status-file "$workdir/status.json"
```

Inspect the output and remove the temporary directory when you are done:

```bash
rm -rf "$workdir"
```

## 6. Make a live change only from the lab console

When the interface names and intent are correct, a one-shot apply changes the
host and exits:

```bash
sudo routerd apply --config first-router.yaml --once
```

For a persistent router, put the reviewed file at
`/usr/local/etc/routerd/router.yaml` and start the installed service:

```bash
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 first-router.yaml /usr/local/etc/routerd/router.yaml
sudo systemctl enable --now routerd.service
```

On FreeBSD, use the rc.d service documented in
[Install and upgrade](../install-and-upgrade.md) instead.

## 7. Use `routerctl` after the service starts

`routerctl` is a client for the running local `routerd` daemon. It is useful
after the service has created its Unix sockets:

```bash
sudo routerctl get status
sudo routerctl get events --limit 20
sudo routerctl get connections --limit 50
```

On a fresh installation, use `sudo` for these commands. You can later add an
operator to the `routerd` group and start a new login session for read-only
status access without `sudo`.

When the daemon is running, `routerctl validate`, `routerctl plan`, and
`routerctl apply` submit a candidate configuration to that daemon. They are not
standalone first-install commands.

## Next steps

- [Bring up the first lab router](./first-router.md) — DHCP, DNS advertisement,
  and IPv4 NAT in an isolated lab
- [LAN-side services](./lan-side-services.md) — local DNS, DHCPv6, RA, and NTP
- [WAN-side services](./wan-side-services.md) — DHCPv6-PD, PPPoE, and DS-Lite
