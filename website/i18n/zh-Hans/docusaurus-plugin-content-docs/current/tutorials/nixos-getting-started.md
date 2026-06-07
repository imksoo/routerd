---
title: 从 NixOS 开始
---

# 从 NixOS 开始

![展示 routerd release binary、generated NixOS module、declarative service、nixos-rebuild test/switch 与 rollback 的 NixOS getting started flow](/img/diagrams/tutorial-nixos-getting-started.png)

NixOS 是 routerd 的主要辅助平台。
在 NixOS 上，建议通过声明式 NixOS 配置来管理 routerd 的管理服务，而非使用临时的 systemd unit。
routerd 的可执行文件从 release archive 安装。
不过，OS 软件包请通过 NixOS 配置管理。
`install.sh` 不会以 `nix-env` 安装软件包，只会输出警告。

## 推荐的起始方式

在 NixOS 上，请先以声明式方式管理守护进程型的 WAN 侧服务。
DHCPv6-PD、DHCPv4 客户端租约、PPPoE 连接、HealthCheck、dnsmasq、防火墙日志记录、nftables 启用，以及主要的 `routerd.service`，都可以通过生成的 NixOS 模块来描述。
请先确认基础服务能以 `nixos-rebuild test` 正常收敛，再加入其他路由器资源。

## 生成的产物

routerd 会将 systemd unit 写入 `/etc/nixos/routerd-generated.nix`。使用下列命令应用：

```bash
sudo nixos-rebuild test
sudo nixos-rebuild switch
```

生成的 unit 会以明确的路径启动 routerd 守护进程，
并具备适当的 `RuntimeDirectory`、`StateDirectory`、`ProtectSystem=strict` 与所需的 capability。

## 为何不使用临时 unit

在 NixOS 上，放在 `/run/systemd/system` 的 unit 不属于系统配置的一部分。
重新启动或执行 `nixos-rebuild switch` 后就会消失。
若要让 unit 在重新启动和重新构建后仍然保留，就必须在 NixOS 配置中声明。
routerd 通过写入 `/etc/nixos/routerd-generated.nix` 来实现这一点。

## 目前支持范围

已实现的功能如下。

- `routerd-dhcpv6-client` 的 systemd unit 生成
- `routerd-dhcpv4-client` 的 systemd unit 生成
- `routerd-pppoe-client` 的 systemd unit 生成
- `Package` override、`SysctlProfile`、derived host runtime artifact、`generated service artifacts` 的 NixOS 模块生成
- `nixos-rebuild switch` 后 DHCPv6-PD 能达到 `Bound` 状态
- dnsmasq、DNS 解析器、HealthCheck、防火墙日志记录器、Tailscale、DHCPv4 客户端、DHCPv6 客户端、PPPoE 客户端服务可通过生成的模块声明
- NAT、firewall、policy routing、Path MTU 资源所需的 nftables 自动启用
- `nixos-rebuild switch` 失败时尝试执行 `nixos-rebuild switch --rollback`
- WireGuard / Tailscale / VXLAN 已确认可在 NixOS / Linux / FreeBSD 之间运行

各平台的详细说明请参阅 [支持的平台](../platforms.md)。

## 相关项目

- [安装](./install.md)
- [创建第一台路由器](./first-router.md)
- [WAN 侧服务](./wan-side-services.md)
