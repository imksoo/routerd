---
title: Bootstrap a router host declaratively
---

# Bootstrap a router host declaratively

routerd can describe most first-boot host preparation in the router YAML. The
goal is not to replace an installer, but to keep the router-specific drift out
of ad hoc shell history.

## Package dependencies

Use `Package` for OS packages that routerd controllers and managed helper
daemons need.

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Package
metadata:
  name: router-service-dependencies
spec:
  packages:
    - os: ubuntu
      manager: apt
      names:
        - dnsmasq
        - nftables
        - conntrack
        - kmod
        - wireguard-tools
        - tailscale
    - os: alpine
      manager: apk
      names:
        - dnsmasq
        - nftables
        - conntrack-tools
        - iproute2
        - wireguard-tools
        - tailscale
    - os: freebsd
      manager: pkg
      names:
        - dnsmasq
        - wireguard-tools
        - mpd5
```

## Kernel modules

Use `KernelModule` for Linux kernel modules that must be loaded before
firewall, conntrack, WireGuard, or NFLOG integrations become useful.

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: KernelModule
metadata:
  name: router-kernel-modules
spec:
  modules:
    - nf_conntrack
    - nfnetlink_log
    - wireguard
  runtime: true
  persistent: true
  optional: true
```

On Ubuntu and Debian, `runtime: true` runs `modprobe` and `persistent: true`
writes `/etc/modules-load.d/90-routerd-<name>.conf`. On NixOS, routerd records
the resource as declarative-only because modules should be owned by the NixOS
configuration. On FreeBSD, the resource is reported as unsupported.

## Sysctl profile

Use `SysctlProfile` for forwarding, conntrack accounting, and router-oriented
kernel defaults. Override only the values that differ from the profile.

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: SysctlProfile
metadata:
  name: router-runtime
spec:
  profile: router-linux
  runtime: true
  persistent: true
  overrides:
    net.netfilter.nf_conntrack_udp_timeout: "60"
```

## Existing host networking

Use `NetworkAdoption` when the base OS already has DHCP or resolver behavior
that conflicts with routerd's resource model. It is the documented place for
networkd and resolved drop-ins instead of one-off edits under `/etc/systemd`.

Use `SystemdUnit` for explicit local units that should be installed and enabled
by routerd. routerd-managed DHCP, DNS, PPPoE, healthcheck, Tailscale, and helper
daemon units are generated from their own resource kinds; do not duplicate
those units manually unless you are intentionally adopting a local service.

On Alpine/OpenRC, `routerd render alpine --out-dir <dir>` can render OpenRC
scripts for explicit `SystemdUnit`, managed dnsmasq, `routerd-healthcheck`,
DHCP clients, DNS resolver, firewall logger, PPPoE, and Tailscale. During
apply, routerd installs those scripts under `/etc/init.d` and uses `rc-update`
and `rc-service` only when the current OpenRC state needs a change.
Synthesized DNS resolver scripts are rendered but not enabled or started until
runtime config materialization is available outside the controller loop.
It does not emulate systemd-only concepts. `NetworkAdoption` drop-ins for
systemd-networkd/resolved, systemd sandboxing fields, and timesyncd ownership
remain unsupported on OpenRC until they have native Alpine semantics.

## Apply order

For a remote router, keep the operational order conservative:

1. Install the routerd binaries and a minimal config.
2. Validate the full config.
3. Run a dry-run apply.
4. Confirm management interface and SSH are protected.
5. Apply.
6. Verify `routerctl status`, forwarding, DNS, and the Web Console.
