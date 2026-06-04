---
title: DHCP lease sync for HA routers
slug: /how-to/dhcp-lease-sync
---

# DHCP lease sync for HA routers

Use `DHCPLeaseSync` when two routerd nodes share a LAN service role and the
active node must keep the standby node's dnsmasq lease file warm. The resource
is intended for active-to-standby sync; it should normally be gated by a
`VirtualAddress` role so the backup does not copy stale leases back to the
active node.

The complete example is in `examples/dhcp-lease-sync-ha.yaml`.

## Configure a persistent lease file

Point both DHCP server resources at the same persistent dnsmasq lease file:

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: lan-dhcpv4
  spec:
    interface: lan
    leaseFile: /var/lib/routerd/dnsmasq/dnsmasq.leases
    addressPool:
      start: 192.168.30.100
      end: 192.168.30.199

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: lan-dhcpv6
  spec:
    interface: lan
    mode: stateful
    leaseFile: /var/lib/routerd/dnsmasq/dnsmasq.leases
    addressPool:
      start: fd00:30::100
      end: fd00:30::1ff
```

`/var/lib/routerd` survives service restarts and standby promotion. Avoid
placing the authoritative DHCP server lease file under `/run` when the node may
reboot or fail over.

## Sync only from the active node

Gate `DHCPLeaseSync` on the local `VirtualAddress` status. The sync runs only
while the VIP role is `master`:

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPLeaseSync
  metadata:
    name: lan-leases
  spec:
    leaseFile: /var/lib/routerd/dnsmasq/dnsmasq.leases
    interval: 30s
    targets:
      - name: standby
        host: routerd-standby.lan.example
        user: routerd
        path: /var/lib/routerd/dnsmasq/dnsmasq.leases
    when:
      state:
        VirtualAddress/lan-vip.role:
          equals: master
```

On promotion, the standby starts from its last synced lease file instead of an
empty lease database.

## SSH requirements

`DHCPLeaseSync` uses `rsync` over SSH. Prepare non-interactive SSH before
enabling the resource:

- Create or install an SSH key for the routerd process user on the active node.
- Install the public key in `authorized_keys` for `target.user` on the standby
  node.
- Preload or manage `known_hosts` for `target.host`; `BatchMode=yes` prevents
  interactive host-key prompts.
- Ensure the target user can create the destination directory and write the
  lease file.

routerd adds these SSH defaults when `target.sshOptions` does not override the
same key:

```text
-o BatchMode=yes -o ConnectTimeout=10
```

It also adds `rsync --timeout=60` and runs each sync command with a controller
context deadline. Operators can override the SSH options with
`target.sshOptions` and the rsync timeout with `target.options`:

```yaml
targets:
  - host: routerd-standby.lan.example
    user: routerd
    path: /var/lib/routerd/dnsmasq/dnsmasq.leases
    sshOptions:
      - -o
      - ConnectTimeout=5
    options:
      - --timeout=30
```

## Check it

```bash
routerd validate --config examples/dhcp-lease-sync-ha.yaml
routerd apply --config examples/dhcp-lease-sync-ha.yaml --once --dry-run
routerctl describe VirtualAddress/lan-vip
routerctl describe DHCPLeaseSync/lan-leases
```

When the node is not master, `routerd serve` filters the resource through
`spec.when`. When the node is master and the lease file exists, status should
move to `Synced`; if the lease file is missing, it stays `Pending` until dnsmasq
creates it.
