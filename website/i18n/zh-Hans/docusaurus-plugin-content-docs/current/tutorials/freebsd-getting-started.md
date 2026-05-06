---
title: 从 FreeBSD 开始
---

# 从 FreeBSD 开始

FreeBSD 使用与 Ubuntu 和 NixOS 相同的 routerd resource model,但主机产物是 FreeBSD native。routerd 会生成 `rc.conf.d`、`rc.d` scripts、`pf.conf`、`dhclient.conf`、dnsmasq 配置、`mpd5.conf`,以及 DS-Lite 使用的动态 `ifconfig gif` 操作。

本教程假设 FreeBSD 14.x,并使用 `/usr/local` 作为源码安装位置。参考配置请使用 `examples/freebsd-edge.yaml`。

## 1. 在开发主机构建

一般做法是在开发机上构建 routerd,再把 binaries 复制到 FreeBSD router。这样可以让 router 保持简洁,不用在 edge host 上放完整 Go build 环境。

```bash
make build
```

复制 binaries:

```bash
scp bin/routerd bin/routerctl bin/routerd-* admin@freebsd-router:/tmp/
```

在 router 上安装:

```sh
sudo install -d -m 0755 /usr/local/sbin
sudo install -m 0755 /tmp/routerd /usr/local/sbin/routerd
sudo install -m 0755 /tmp/routerctl /usr/local/sbin/routerctl
sudo install -m 0755 /tmp/routerd-* /usr/local/sbin/
```

## 2. 安装 FreeBSD 包

请在 YAML 中用 `Package` 声明包。首次 bootstrap 时,可以手动安装同一组包,或审阅生成的 `install-packages.sh`。

```sh
sudo pkg install -y dnsmasq bind-tools wireguard-tools tailscale strongswan mpd5
```

FreeBSD base system 已提供 `ifconfig`、`sysctl`、`service`、`sysrc`、`pfctl`、`pflog0`、`netstat`、`sockstat`、`ping`、`traceroute`。

## 3. 放置 router 配置

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 examples/freebsd-edge.yaml /usr/local/etc/routerd/router.yaml
```

应用前,请修改 interface 名称、address 与 secret。第一次执行时,请把管理 SSH 放在独立 interface,或准备 hypervisor console。

## 4. 验证并审阅生成文件

验证配置:

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
```

把 FreeBSD 产物生成到临时目录:

```sh
rm -rf /tmp/routerd-freebsd-render
routerd render freebsd \
  --config /usr/local/etc/routerd/router.yaml \
  --out-dir /tmp/routerd-freebsd-render
```

预期文件包括:

- `rc.conf.d-routerd`
- `dhclient.conf`
- `mpd5.conf`
- `pf.conf`
- `dnsmasq.conf`
- `install-packages.sh`
- `rc.d-*`

应用到实机前请先审阅:

```sh
less /tmp/routerd-freebsd-render/rc.conf.d-routerd
less /tmp/routerd-freebsd-render/pf.conf
less /tmp/routerd-freebsd-render/dnsmasq.conf
```

## 5. 理解 FreeBSD 主机接口

routerd 会把 resource 对应到下列 FreeBSD 组件:

| 组件 | 责任 |
| --- | --- |
| `rc.conf.d-routerd` | Interface alias、forwarding、cloned interface、static route、`pf`、`pflog`、`mpd5` enablement |
| `rc.d-*` scripts | dnsmasq、firewall logger、healthcheck、Tailscale、DHCP clients 等 routerd 管理 daemons |
| `pf.conf` | Zone filtering、service holes、NAT、firewall logging |
| `pflog0` | `routerd-firewall-logger` 的 firewall log source |
| `dnsmasq.conf` | DHCPv4、DHCPv6、DHCP relay、Router Advertisement |
| `dhclient.conf` | 被接管 uplink 的 FreeBSD DHCPv4 client 行为 |
| `mpd5.conf` | PPPoE bundle、link、authentication、MTU/MRU 与 default route 行为 |
| `ifconfig gif` | 静态 `rc.conf` 不足时的动态 DS-Lite tunnel 应用 |

## 6. 应用

先检查 plan:

```sh
routerd plan --config /usr/local/etc/routerd/router.yaml
```

生成文件与 plan 都符合预期后再应用:

```sh
sudo routerd apply --config /usr/local/etc/routerd/router.yaml
```

routerd 会在加载 `pf.conf` 前用 `pfctl -nf` 验证。重新启动 dnsmasq 前,也会用 `dnsmasq --test` 验证配置。

## 7. 检查状态与日志

查看 routerd 状态:

```sh
routerctl status
routerctl events --limit 20
```

跟踪系统日志:

```sh
tail -f /var/log/routerd.log
```

查看 pf state:

```sh
sudo pfctl -ss -v
```

通过 `pflog0` 查看 firewall log:

```sh
sudo tcpdump -n -e -ttt -i pflog0
```

启用 `FirewallLog` 后,routerd 也会把 `pflog0` 条目导入 firewall log store,供 `routerctl` 与 Web Console 使用。

## 下一步

接着请在 platform matrix 中确认 OS 差异,并在英文或日文文档中查看 WAN-side services 与 basic firewall 教程。
