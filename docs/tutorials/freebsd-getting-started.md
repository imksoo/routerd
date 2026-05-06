---
title: Getting started on FreeBSD
---

# Getting started on FreeBSD

FreeBSD uses the same routerd resource model as Ubuntu and NixOS, but the host
artifacts are FreeBSD-native. routerd renders `rc.conf.d`, `rc.d` scripts,
`pf.conf`, `dhclient.conf`, dnsmasq configuration, `mpd5.conf`, and dynamic
`ifconfig gif` operations for DS-Lite.

This tutorial assumes FreeBSD 14.x and a source install under `/usr/local`.
Use `examples/freebsd-edge.yaml` as the reference configuration.

## 1. Build on a development host

The usual path is to build routerd on your development machine and copy the
binaries to the FreeBSD router. This keeps the router small and avoids needing a
full Go build environment on the edge host.

```bash
make build
```

Copy the binaries:

```bash
scp bin/routerd bin/routerctl bin/routerd-* admin@freebsd-router:/tmp/
```

Install them on the router:

```sh
sudo install -d -m 0755 /usr/local/sbin
sudo install -m 0755 /tmp/routerd /usr/local/sbin/routerd
sudo install -m 0755 /tmp/routerctl /usr/local/sbin/routerctl
sudo install -m 0755 /tmp/routerd-* /usr/local/sbin/
```

## 2. Install FreeBSD packages

Declare the packages in YAML through `Package`. For first bootstrap, install
the same set manually or review the generated `install-packages.sh`.

```sh
sudo pkg install -y dnsmasq bind-tools wireguard-tools tailscale strongswan mpd5
```

The FreeBSD base system already provides `ifconfig`, `sysctl`, `service`,
`sysrc`, `pfctl`, `pflog0`, `netstat`, `sockstat`, `ping`, and `traceroute`.

## 3. Place the router configuration

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 examples/freebsd-edge.yaml /usr/local/etc/routerd/router.yaml
```

Edit interface names, addresses, and secrets before applying. Keep management
SSH on a separate interface or use a hypervisor console during the first run.

## 4. Validate and review generated files

Validate the configuration:

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
```

Render FreeBSD artifacts into a temporary directory:

```sh
rm -rf /tmp/routerd-freebsd-render
routerd render freebsd \
  --config /usr/local/etc/routerd/router.yaml \
  --out-dir /tmp/routerd-freebsd-render
```

Expected files include:

- `rc.conf.d-routerd`
- `dhclient.conf`
- `mpd5.conf`
- `pf.conf`
- `dnsmasq.conf`
- `install-packages.sh`
- `rc.d-*`

Review the output before touching the live host:

```sh
less /tmp/routerd-freebsd-render/rc.conf.d-routerd
less /tmp/routerd-freebsd-render/pf.conf
less /tmp/routerd-freebsd-render/dnsmasq.conf
```

## 5. Understand the FreeBSD host surfaces

routerd maps resources to these FreeBSD components:

| Component | Responsibility |
| --- | --- |
| `rc.conf.d-routerd` | Interface aliases, forwarding, cloned interfaces, static routes, `pf`, `pflog`, and `mpd5` enablement |
| `rc.d-*` scripts | routerd-managed daemons such as dnsmasq, firewall logger, healthcheck, Tailscale, and DHCP clients |
| `pf.conf` | Zone filtering, service holes, NAT, and firewall logging |
| `pflog0` | Firewall log source for `routerd-firewall-logger` |
| `dnsmasq.conf` | DHCPv4, DHCPv6, DHCP relay, and Router Advertisement |
| `dhclient.conf` | FreeBSD DHCPv4 client behavior for adopted uplinks |
| `mpd5.conf` | PPPoE bundle, link, authentication, MTU/MRU, and default-route behavior |
| `ifconfig gif` | Dynamic DS-Lite tunnel application when static `rc.conf` is not enough |

## 6. Apply

Run a plan first:

```sh
routerd plan --config /usr/local/etc/routerd/router.yaml
```

Apply when the generated files and plan are expected:

```sh
sudo routerd apply --config /usr/local/etc/routerd/router.yaml
```

routerd validates `pf.conf` with `pfctl -nf` before loading it. It validates
dnsmasq with `dnsmasq --test` before restarting the service.

## 7. Inspect status and logs

Read routerd status:

```sh
routerctl status
routerctl events --limit 20
```

Follow the system log:

```sh
tail -f /var/log/routerd.log
```

Check pf state:

```sh
sudo pfctl -ss -v
```

Check firewall logging through `pflog0`:

```sh
sudo tcpdump -n -e -ttt -i pflog0
```

If `FirewallLog` is enabled, routerd also imports `pflog0` entries into the
firewall log store for `routerctl` and the Web Console.

## See also

- [Supported platforms](../platforms.md)
- [WAN-side services](./wan-side-services.md)
- [Basic firewall](./basic-firewall.md)
