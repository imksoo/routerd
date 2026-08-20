---
title: 基本 IPv4 NAT 网关
sidebar_position: 10
---

# 基本 IPv4 NAT 网关

![DHCP WAN、routerd 管理的 LAN 网关、DHCPv4、NAT44 与区域资源组成的 IPv4 网关](/img/diagrams/config-example-basic-ipv4-nat.png)

这是一个适合实验室阅读的家用 IPv4 网关形状：WAN 通过 DHCPv4 获得地址，LAN 使用 `192.168.10.1/24`，客户端经 NAT44 访问外部网络。完整 YAML 在 `examples/example-basic-ipv4-nat.yaml`。

先在隔离 Ubuntu Server VM 上测试，并保留控制台。这个示例不是把一台正在承载生产网络的设备改造成路由器的快捷命令。

## 拓扑

```text
Internet / 上游路由器
          |
        wan（DHCPv4）
          |
      routerd 主机
          |
  lan（192.168.10.1/24）
          |
      LAN 客户端
```

| 任务 | 主要资源 |
| --- | --- |
| 声明 WAN 和 LAN | `Interface/wan`、`Interface/lan` |
| 获取 WAN IPv4 | `DHCPv4Client/wan-dhcpv4` |
| 持有 LAN 网关地址 | `IPv4StaticAddress/lan-base` |
| 分配 LAN 地址 | `DHCPv4Server/lan-dhcpv4` |
| 让 LAN IPv4 出网 | `NAT44Rule/lan-to-wan` |

## 关键配置：现代 NAT 字段

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: lan-to-wan
  spec:
    type: masquerade
    egressInterface: wan
    sourceRanges:
      - 192.168.10.0/24
    excludeDestinationCIDRs:
      - 10.0.0.0/8
      - 172.16.0.0/12
      - 192.168.0.0/16
```

这是当前的 `NAT44Rule` 形状：使用 `type`、`egressInterface` 和 `sourceRanges`。`masquerade` 适合 WAN 地址会变化的 DHCP 上游。排除的私有目的网段不会被 NAT，以免破坏内部、VPN 或管理路由。

下列资源把 LAN 地址和 DHCP 地址池联系在一起：

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4StaticAddress
  metadata:
    name: lan-base
  spec:
    interface: lan
    address: 192.168.10.1/24

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: lan-dhcpv4
  spec:
    interface: lan
    addressPool:
      start: 192.168.10.100
      end: 192.168.10.199
      leaseTime: 12h
    gatewayFrom:
      resource: IPv4StaticAddress/lan-base
      field: address
    dnsServers:
      - 1.1.1.1
      - 1.0.0.1
```

这个第一轮实验直接向客户端公布外部 DNS 解析器 `1.1.1.1` 和 `1.0.0.1`，这样路由器自身还不需要承担 DNS 服务。请先确认学校、家庭、单位或上游网络允许使用这些公共解析器；若网络策略要求指定 DNS，或你要让路由器提供本地名称、过滤或条件转发，请改为获准的 DNS 地址，或先配置 `DNSResolver`，再公布路由器的 LAN 地址。

## 先用本地命令检查

以具有 `sudo` 权限的本地用户运行下列独立检查；它们不需要 daemon，也不会应用网络变更。

```bash
LAB_DIR="$(mktemp -d)"
sudo routerd validate --config examples/example-basic-ipv4-nat.yaml
sudo routerd apply --config examples/example-basic-ipv4-nat.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
rm -rf "$LAB_DIR"
```

不要用未启动服务的 `routerctl validate` 或 `routerctl plan` 来替代它们。

服务真实运行后，才可以查询资源。首次安装请保留 `sudo`；若管理员通过 `routerd` 组授予本地 socket 访问，加入后必须重新登录才会生效：

```bash
sudo routerctl get status
sudo routerctl describe DHCPv4Client/wan-dhcpv4
sudo routerctl describe NAT44Rule/lan-to-wan
```

## 关于防火墙

完整示例还包含 `FirewallZone` 和 `FirewallPolicy`，用来说明 NAT 与区域策略如何组合。但防火墙功能仍处于预发布的基础实现阶段，不是完整的安全边界。不要把 NAT 成功、dry-run 成功或一张 nftables 表存在当成公网暴露已经安全的证明。

## 常见调整

- 将 `ens18`、`ens19` 改为实际接口名。
- 若 `192.168.10.0/24` 与上游、VPN 或管理网重叠，改成一个未使用的私有网段。
- 若需要可信的访客隔离，使用 VLAN、独立 SSID 或独立物理端口；不要只依赖同一二层 LAN 上的 MAC 分类。
