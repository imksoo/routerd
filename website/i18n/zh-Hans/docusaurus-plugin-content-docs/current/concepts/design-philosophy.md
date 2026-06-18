---
title: 设计理念
slug: /concepts/design-philosophy
sidebar_position: 2
---

# 设计理念

![routerd 从 YAML 意图到专用 daemon、事件、平台 renderer 和安全边界的设计原则](/img/diagrams/concept-design-philosophy.png)

routerd 将路由器视为「资源的集合体」，而非「配置文件的堆积」。本页说明实现上的判断依据。

## 以 YAML 作为意图的核心

YAML 表达路由器的意图。routerd 比较 YAML 与主机的现有状态，仅应用必要的差异。重视的是可反复阅读的定义，而非对操作步骤的记忆。

## 将有状态的处理分配给专用守护进程

DHCPv6-PD、DHCPv4、PPPoE、健康检查等处理，各自具有计时器、重启后的状态恢复，以及事件历史记录。若将这些塞入一次性的命令，租约更新与故障时的观测就会变得不稳定。

因此，routerd 将有状态的处理以小型专用守护进程的形式运行。守护进程将租约与内部状态存储至文件，并通过 Unix domain socket 公开状态。routerd 本体读取该状态，对下游资源进行调和（reconcile）。

## 不将损坏的 IPv6 散布至 LAN

若 DHCPv6-PD 已丢失，却持续广播旧前缀的 RA、AAAA 记录与 LAN 地址，从用户角度看来就是「IPv6 看似存在，却无法通信」的状态。routerd 的设计是在无法确认委派前缀的状态时，停止向下游提供 IPv6。

## 以事件串联小型组件

routerd 不以单一的大型程序处理整个路由器，而是以事件串联小型控制器。例如，DHCPv6-PD 进入 Bound 状态后，状态会依序传递至 LAN 地址、RA、DHCPv6 服务器、DNS 响应、DS-Lite、IPv4 路由。

这种架构使得在哪个阶段停滞一目了然。

## 将 OS 差异封闭在内部

Ubuntu、FreeBSD 对于相同的意图，主机侧的表达方式各异。routerd 通过 `pkg/platform` 的功能标志与各 OS 的生成器，将这些差异封闭在内部。面向用户的资源名称尽可能保持统一形式。

## 现阶段优先采用正确的名称而非兼容性

当前仍处于发布前的 v1alpha1 阶段。过去曾将 DHCP 相关的 Kind 名称与二进制名称，在不保留兼容别名的情况下，整理为 RFC 表记（`DHCPv4*` / `DHCPv6*`）。在发布前的这个阶段，避免留下将来难以更动的错误名称是优先考量。
