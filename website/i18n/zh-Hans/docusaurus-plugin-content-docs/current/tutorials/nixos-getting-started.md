---
title: 从 NixOS 开始
---

# 从 NixOS 开始

NixOS 使用 routerd 的完整 resource model。推荐做法是通过声明式 NixOS 配置来驱动 routerd 管理的服务,不要依赖 transient systemd unit。

## 推荐的起始范围

在 NixOS 上,先从 daemon 型 WAN 服务开始。DHCPv6-PD、DHCPv4 client lease、PPPoE session、HealthCheck、dnsmasq、firewall logging、nftables 启用,以及主 `routerd.service`,都可以写进生成的 NixOS module。基础服务能以 `nixos-rebuild test` 干净收敛后,再加入更多 router resource。

## 生成的产物

routerd 会把 systemd units 写入 `/etc/nixos/routerd-generated.nix`。用下列命令应用:

```bash
sudo nixos-rebuild test
sudo nixos-rebuild switch
```

生成的 units 会以明确 binary path 启动 routerd daemons,并带有合适的 `RuntimeDirectory`、`StateDirectory`、`ProtectSystem=strict` 与 capability 清单。

## 为什么不用 transient units

NixOS 上放在 `/run/systemd/system` 的 unit 不是系统配置的一部分。重启或执行 `nixos-rebuild switch` 后会被移除。若要跨重启与 rebuild 保留 unit,就必须把 unit 声明在 NixOS 配置中。routerd 通过写入 `/etc/nixos/routerd-generated.nix` 达成这一点。

## 当前覆盖范围

已实现:

- `routerd-dhcpv6-client` 的 systemd unit 生成
- `routerd-dhcpv4-client` 的 systemd unit 生成
- `routerd-pppoe-client` 的 systemd unit 生成
- `Package`、`SysctlProfile`、`NetworkAdoption`、`SystemdUnit` 的 NixOS module 生成
- `nixos-rebuild switch` 后 DHCPv6-PD 到达 `Bound`
- dnsmasq、DNS resolver、HealthCheck、firewall logger、Tailscale、DHCPv4 client、DHCPv6 client、PPPoE client service 可通过生成 module 声明
- NAT、firewall、policy routing、Path MTU resource 需要 nftables 时自动启用
- `nixos-rebuild switch` 失败时尝试 `nixos-rebuild switch --rollback`
- WireGuard / Tailscale / VXLAN 已在 NixOS / Linux / FreeBSD 间确认
- VRF 以 systemd-networkd 的 native netdev 生成

各平台细节请参考 [支持的平台](../platforms.md)。

## 下一步

接着请在英文或日文文档中查看 install、first router、WAN-side services 的教程。
