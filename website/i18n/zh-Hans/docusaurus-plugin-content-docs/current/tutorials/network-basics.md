---
title: 使用 routerd 前的网络基础
sidebar_position: 0
---

# 使用 routerd 前的网络基础

试用 routerd 前，不必先背完所有网络术语。本页只说明第一个教程需要的最小概念。

## 先记住这张图

```text
Internet / 学校 / 运营商网络
                |
               WAN
                |
          [ routerd 主机 ]
                |
               LAN
                |
       电脑、手机、游戏主机
```

- **WAN** 是连接运营商、学校或上游路由器的一侧。
- **LAN** 是你自己管理的一侧，例如家庭、教室或测试网络。
- **路由器** 在 WAN 和 LAN 之间转发数据包；它不是 Wi-Fi AP，也不是 Ethernet switch。

## 六个先会用到的词

| 名词 | 白话说明 |
| --- | --- |
| IP 地址 | 设备在网络上的地址，像投递地址。 |
| Gateway | 设备要前往本 LAN 以外时，先交给的设备；小型网络通常就是路由器。 |
| DHCP | 自动给设备分配 IP 地址、gateway，以及常见 DNS server 的服务。 |
| DNS | 把 `example.com` 这类名称换成 IP 地址的电话簿。 |
| NAT | 让多台 LAN 设备共用一条对外 IPv4 连接的方法。 |
| `/24` | 表示 `192.168.10.1` 到 `192.168.10.254` 这类一小段 IPv4 LAN 的简写。 |

routerd 把这些选择写成 YAML。`Router` 文件是一个装有 **resource（组件）**
列表的盒子。每个 resource 有 `kind`（种类）、`metadata.name`（你取的标签）和
`spec`（详细资料）。

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: my-lab-router
spec:
  resources:
    # 在这里列出 Interface、DHCPv4Server、NAT44Rule 等组件。
```

不需要死记 resource 名称。教程会一次加入一个工作，并在需要时链接到参考文档。

## 安全地开始

第一次实验请使用隔离的 Ubuntu Server VM 或备用电脑。保留 Proxmox/VM console、
serial console，或独立的 management NIC。不要一开始就改动承载家庭、学校或工作网络的
唯一一台路由器。

`routerd validate` 与 `routerd apply --once --dry-run` 用来检查配置文件而不提交网络变更。
`routerd apply --once` 与 `routerd serve` 可能会改变主机网络。只有在有 console 或独立
管理路径时才执行它们。

:::caution Firewall 边界
routerd 是 pre-release 软件。firewall resource 仍属于 groundwork，并非安全认证。
不要把一份示例当成 Internet-facing router 的唯一安全边界。
:::

## 下一步

1. 在隔离的 Ubuntu Server VM 上[安装 routerd](../install-and-upgrade.md)。
2. [执行安全的第一次检查](./getting-started.md)，不改动网络。
3. 有 console 路径后，再[建立第一台 IPv4 实验路由器](./first-router.md)。
