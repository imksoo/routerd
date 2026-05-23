---
title: 应用与生成
slug: /concepts/apply-and-render
sidebar_position: 4
---

# 应用与生成

routerd 有几个日常运行中常用的操作。
本页统一说明文档中使用的术语。

## 验证

`routerd validate` 确认 YAML 的格式。
可检测 Kind 名称、必填字段、值范围，以及明显的依赖性错误。

```bash
routerd validate --config /usr/local/etc/routerd/router.yaml
```

## 查看计划

`routerd plan` 显示准备对主机执行的操作内容。
在应用至正式路由器之前，可确认管理连接是否会中断、是否有意外的路由变更。

```bash
routerd plan --config /usr/local/etc/routerd/router.yaml
```

## 模拟执行

`--dry-run` 在不变更主机的情况下，仅确认应用结果。
routerd 在新控制器开发与实机验证初期，以模拟执行作为默认模式。

```bash
routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run
```

## 应用

`routerd apply` 依照 YAML 的意图变更主机。
只执行一次时加上 `--once`。
若要持续运行，请使用 `routerd serve`。

```bash
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once
sudo routerd serve --config /usr/local/etc/routerd/router.yaml
```

## 生成（render）

文档中的「生成」，是指 routerd 组装 dnsmasq 配置、nftables 配置、systemd 单元、NixOS 配置等面向主机的文件。
仅完成生成并不代表主机会立即变更。
是否实际应用，取决于应用处理与模拟执行的指定方式。

当前的 routerd 中，dnsmasq 不负责 DNS 响应。
针对 dnsmasq 只生成 DHCPv4、DHCPv6、中继、RA 的配置。
DNS 监听、本地区域、条件式转发、加密 DNS 由 `DNSResolver` 负责。
`DNSResolver` 是 `routerd-dns-resolver` 的运行配置。

## 调和（Reconcile）

在守护进程模式下，routerd 接收事件并重新评估必要的资源。
这个「缩小意图与现有状态差距的处理」，在本文档中称为调和（reconcile）。
例如，DHCPv6-PD 的 Renew 后前缀发生变化，调和会依序传递至 LAN 地址、RA、DNS 响应、DS-Lite 路由。
