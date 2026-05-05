---
title: Getting started
---

# Getting started

This tutorial shows the safest first loop:

1. write a small router resource file
2. validate it
3. inspect the plan
4. run a dry application
5. only then run the daemon

The first pass should not change the host network.

## 1. Check interface names

```bash
ip link
```

The examples use `ens18` for WAN, `ens19` for LAN, and `ens20` for management.
Use the names from your host.

Keep the management path separate from the interface being changed. Do not
test a first configuration over the same interface that routerd is about to
adopt.

## 2. Start with interfaces and host bootstrap

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: first-router
spec:
  resources:
    - apiVersion: system.routerd.net/v1alpha1
      kind: Package
      metadata:
        name: router-host-tools
      spec:
        packages:
          - os: ubuntu
            names: [dnsmasq, nftables, conntrack, iproute2]

    - apiVersion: system.routerd.net/v1alpha1
      kind: SysctlProfile
      metadata:
        name: router-linux
      spec:
        profile: router-linux

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

`Package` and `SysctlProfile` make host preparation part of the router intent.
They are useful because router features often require OS tools and forwarding
settings before protocol resources can work.

## 3. Validate

```bash
routerd validate --config first-router.yaml
```

Validation checks the resource shape before routerd touches the host.

## 4. Inspect the plan

```bash
routerd plan --config first-router.yaml
```

Use the plan to catch accidental interface names, missing dependencies, and
host artifacts that routerd would create.

## 5. Dry apply

```bash
routerd apply --config first-router.yaml --once --dry-run
```

Dry application exercises resource loading, dependency ordering, and generated
artifacts without committing network changes.

## 6. Run the daemon when the plan is safe

```bash
sudo routerd serve --config first-router.yaml
```

In production, install a `SystemdUnit` resource or a systemd unit file so that
`routerd serve` starts on boot.

## 7. Inspect status

```bash
routerctl status
routerctl events --limit 20
routerctl connections --limit 50
```

The next tutorials add LAN DHCP, RA, DNS, route policy, NAT44, and DS-Lite.
