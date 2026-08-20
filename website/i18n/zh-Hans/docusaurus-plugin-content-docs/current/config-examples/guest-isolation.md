---
title: 访客与 IoT 设备隔离
sidebar_position: 60
---

# 访客与 IoT 设备隔离：先理解限制

![共享 LAN 上的 ClientPolicy：受信任设备、访客或 IoT MAC 地址和管理网络](/img/diagrams/config-example-guest-isolation.png)

`ClientPolicy` 可以把列出的 MAC 地址当作访客或 IoT 设备：允许它们访问互联网，同时表达不应访问受信任 LAN 和管理网络的意图。完整 YAML 在 `examples/guest-mode.yaml`。

它适合学习共享 LAN 上的分类方式，但**它不能把一个共享的二层网络变成真正隔离的网络**。

## 关键限制：同一二层网络仍可直接通信

当访客设备和受信任设备接在同一个交换机、同一个未分 VLAN 的 Wi-Fi SSID 或同一个以太网广播域时，它们之间的流量可能根本不经过路由器。路由器上的规则看不到这种直接的二层通信。

要实现真正的访客隔离，应使用：

- 单独的 VLAN，并让交换机和 AP 正确标记、隔离端口；
- 单独的访客 SSID 映射到该 VLAN；
- 或独立的物理端口/交换机和独立子网。

MAC 地址是网卡在二层网络中使用的硬件标识，也可以被伪造或被设备的“私有 MAC”功能改变。因此，MAC 分类不是高安全场景的身份认证机制。

VLAN 是由交换机或无线 AP 在二层划分出的虚拟网络，不只是给同一根网线换一个 IPv4 网段。只有交换机、AP 和路由器都按同一 VLAN 设计配置，访客流量才会被迫经过应有的三层策略点。

## ClientPolicy 的最小形状

下例假定 `lan` 已是一个 `trust` 区域中的 `Interface`：

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: guest-devices
  spec:
    mode: include
    macs:
      - 18:ec:e7:33:12:6c
    isolation:
      lanInternet: allow
      lanLAN: deny
      lanMgmt: deny
      mDNSBroadcast: deny
```

`mode: include` 表示只有列出的 MAC 地址被当作访客；其余设备保持原有分类。`mode: exclude` 则相反：列出的设备受信任，其余设备视为访客。

为便于识别设备，可以配合 `DHCPv4Reservation` 固定名称和 IPv4 地址；这不会解决同一二层网络绕过路由器的问题。

:::caution 不要把它当成唯一安全边界
routerd 的防火墙和 ClientPolicy 仍处于预发布的基础实现阶段。这个 Linux/nftables 示例不是 VLAN 设计、无线隔离、交换机配置或安全审计的替代品。要保护管理网络和可信设备，请先做二层分段，再把路由器策略作为额外的一层。
:::

## 安全检查

先使用本地 `routerd` 检查文件。以下独立检查需要具有 `sudo` 权限的本地用户，不需要 daemon，也不会应用网络变更：

```bash
LAB_DIR="$(mktemp -d)"
sudo routerd validate --config examples/guest-mode.yaml
sudo routerd apply --config examples/guest-mode.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
rm -rf "$LAB_DIR"
```

不应使用 `routerctl validate` 或 `routerctl plan` 来替代它们。当 `routerd serve` 已在有控制台保护的真实主机上运行后，才查询结果；首次安装请保留 `sudo`。若管理员通过 `routerd` 组授予本地 socket 访问，加入后必须重新登录才会生效：

```bash
sudo routerctl get status
sudo routerctl describe ClientPolicy/guest-devices
```

这个例子针对 Linux 的 nftables 路径。不要假定 FreeBSD 或 NixOS 会以完全相同的 MAC/二层方式执行；部署前请查看[支持的平台](../platforms.md)。
