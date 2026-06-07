---
title: 从 FreeBSD 开始
---

# 从 FreeBSD 开始

![从 release archive install 到 rc.d、rc.conf.d、pf、dnsmasq、mpd5 render 与 apply validation 的 FreeBSD getting started flow](/img/diagrams/tutorial-freebsd-getting-started.png)

FreeBSD 使用与 Ubuntu 和 NixOS 相同的 routerd 资源模型。
但生成的主机产物对应 FreeBSD 的机制。
routerd 负责处理 `rc.conf.d`、`rc.d` script、`pf.conf`、`dhclient.conf`、
dnsmasq 配置、`mpd5.conf`，以及 DS-Lite 用的动态 `ifconfig gif` 操作。

本教程以 FreeBSD 14 系为前提。
发布安装程序的安装位置为 `/usr/local` 之下。
参考配置请使用 `examples/freebsd-edge.yaml`。

## 1. 从发布归档文件安装

从 [GitHub Releases](https://github.com/imksoo/routerd/releases) 获取 FreeBSD 用的
归档文件，并在路由器上执行随附的安装程序。

```sh
fetch https://github.com/imksoo/routerd/releases/latest/download/routerd-freebsd-amd64.tar.gz
fetch https://github.com/imksoo/routerd/releases/latest/download/routerd-freebsd-amd64.tar.gz.sha256
cat routerd-freebsd-amd64.tar.gz.sha256
sha256 routerd-freebsd-amd64.tar.gz
tar -xzf routerd-freebsd-amd64.tar.gz
sudo ./install.sh
```

`install.sh` 会安装 FreeBSD 通常所需的软件包。
对象为 `ca_root_nss`、`curl`、`dnsmasq`、`wireguard-tools`、`mpd5`、
`bind-tools`、`tcpdump`、`jq`、`chrony`、`strongswan`。
同时安装 Tailscale 时，使用 `sudo ./install.sh --with-tailscale`。
FreeBSD 的 base system 包含 `ifconfig`、`route`、`sysctl`、`service`、`sysrc`、
`pfctl`、`pflog0`、`netstat`、`sockstat`、`ping`、`traceroute`。
依赖软件包清单可通过 `./install.sh --list-deps` 确认。

## 2. 放置路由器配置

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 examples/freebsd-edge.yaml /usr/local/etc/routerd/router.yaml
```

应用前，请编辑接口名称、地址与密码。
初次操作时，请将管理用 SSH 放置于独立接口，或事先准备 hypervisor 控制台。

## 3. 验证并确认生成的文件

首先验证配置。

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
```

接着将 FreeBSD 用的产物生成至临时目录。

```sh
rm -rf /tmp/routerd-freebsd-render
routerd render freebsd \
  --config /usr/local/etc/routerd/router.yaml \
  --out-dir /tmp/routerd-freebsd-render
```

主要输出如下。

- `rc.conf.d-routerd`
- `dhclient.conf`
- `mpd5.conf`
- `pf.conf`
- `dnsmasq.conf`
- `install-packages.sh`
- `rc.d-*`

应用至实际主机前，请先确认内容。

```sh
less /tmp/routerd-freebsd-render/rc.conf.d-routerd
less /tmp/routerd-freebsd-render/pf.conf
less /tmp/routerd-freebsd-render/dnsmasq.conf
```

## 4. 了解 FreeBSD 侧的角色

routerd 将资源对应至以下 FreeBSD 机制。

| 机制 | 角色 |
| --- | --- |
| `rc.conf.d-routerd` | 接口别名、转发、克隆接口、静态路由、`pf`、`pflog`、`mpd5` 的启用 |
| `rc.d-*` script | dnsmasq、防火墙日志记录器、healthcheck、Tailscale、DHCP client 等受管理守护进程 |
| `pf.conf` | zone 过滤、受管理服务的开口、NAT、防火墙日志 |
| `pflog0` | `routerd-firewall-logger` 读取的防火墙日志 |
| `dnsmasq.conf` | DHCPv4、DHCPv6、DHCP relay、RA |
| `dhclient.conf` | 接管的上游接口的 DHCPv4 client 行为 |
| `mpd5.conf` | PPPoE 的 bundle、link、认证、MTU/MRU、默认路由 |
| `ifconfig gif` | 静态 `rc.conf` 不足时的动态 DS-Lite tunnel 应用 |

## 5. 应用

先确认计划。

```sh
routerd plan --config /usr/local/etc/routerd/router.yaml
```

生成的文件与计划符合预期后，应用配置。

```sh
sudo routerd apply --config /usr/local/etc/routerd/router.yaml
```

routerd 在加载 `pf.conf` 前以 `pfctl -nf` 验证。
dnsmasq 也在重新启动前以 `dnsmasq --test` 验证配置。

## 6. 确认状态与日志

确认 routerd 状态。

```sh
routerctl status
routerctl events --limit 20
```

追踪系统日志。

```sh
tail -f /var/log/routerd.log
```

确认 pf 状态。

```sh
sudo pfctl -ss -v
```

通过 `pflog0` 确认防火墙日志。

```sh
sudo tcpdump -n -e -ttt -i pflog0
```

启用 `FirewallEventLog` 后，routerd 会导入 `pflog0` 的内容。
导入的日志可通过 `routerctl` 与 Web 管理界面确认。

## 相关项目

- [支持的平台](../platforms.md)
- [WAN 侧服务](./wan-side-services.md)
- [基本防火墙](./basic-firewall.md)
