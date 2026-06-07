---
title: 资源模型
slug: /concepts/resource-model
sidebar_position: 3
---

# 资源模型

![从 Router YAML 到 reference、dependency ordering、owner key、status、event 和 ledger artifact 的 routerd resource model](/img/diagrams/concept-resource-model.png)

routerd 的配置由最上层的 `Router` 以及其下并列的多个资源所构成。
每个资源的格式与 Kubernetes 相近。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DHCPv6PrefixDelegation
metadata:
  name: wan-pd
spec:
  interface: wan
```

## 公共字段

- `apiVersion`：资源所属的 API 组与版本。
- `kind`：资源的类别（Kind）。
- `metadata.name`：在相同 `kind` 中唯一的名称。
- `spec`：用户声明的意图。
- `status`：routerd 或专属守护进程观测到的状态。

配置文件中主要编写的是 `spec`。
`status` 则通过控制 API、状态数据库，以及守护进程的 `/v1/status` 来确认。

## API 组

routerd 使用以下 API 组。

| 组 | 用途 |
| --- | --- |
| `routerd.net/v1alpha1` | 最上层的 `Router` |
| `net.routerd.net/v1alpha1` | 接口、DHCP、DNS、路由、隧道、WAN 选择、连接流量记录 |
| `firewall.routerd.net/v1alpha1` | 防火墙区域、策略、规则、记录 |
| `system.routerd.net/v1alpha1` | 主机名、软件包、sysctl、网络接管、systemd 单元、NTP、日志转发、Web 管理界面 |
| `plugin.routerd.net/v1alpha1` | 受信任的本地插件 |

不使用 `routerd.io` 这类临时组名称。

## 依赖关系

资源以名称互相引用。
例如 `IPv6DelegatedAddress` 引用 `DHCPv6PrefixDelegation`，`DSLiteTunnel` 引用 `DHCPv6Information` 和 `DNSResolver` 的结果。

当被引用的资源尚未就绪时，资源会维持在 `Pending` 状态。
待引用资源就绪后，会依次进入 `Applied`、`Bound`、`Up`、`Installed`、`Healthy` 等阶段。

## dependsOn

部分资源可通过 `dependsOn` 指定应用的前置条件。
`dependsOn` 中需明确指定所引用的资源及其状态字段。

```yaml
dependsOn:
  - resource: DHCPv6PrefixDelegation/wan-pd
    phase: Bound
  - resource: Interface/lan
    phase: Up
```

若要使用其他资源的状态值，不在一般字段中编写表达式，而是使用
`deviceFrom`、`gatewayFrom`、`addressFrom`、`ipv4From`、`ipv6From`、
`prefixFrom`、`rdnssFrom`、`addressFrom` 等专用字段。

```yaml
deviceFrom:
  resource: DSLiteTunnel/ds-lite
  field: interface
```

## 拥有引用

`ownerRefs` 表示某个资源从属于另一个资源。
当父资源尚未就绪时，子资源不会持续输出过时的配置。
这是一个重要机制，用于防止 DHCPv6-PD 丢失时遗留旧有的 LAN IPv6 配置。
依赖委派前缀的 LAN IPv6 地址、RA、DNS 记录、DS-Lite，在父资源尚未就绪期间均不会输出过时状态。
