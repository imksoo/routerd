---
title: 将联邦事件转换为 RemoteAddressClaim
---

# 将联邦事件转换为 RemoteAddressClaim

![联邦客户端观测事件匹配 EventSubscription、执行插件、生成带有 RemoteAddressClaim provenance 的 DynamicConfigPart 的流程](/img/diagrams/how-to-event-federation-subscription.png)

CloudEdge Event Federation（ADR 0006）是一种机制，使一个 routerd 节点能够对另一个节点观测到的事实进行声明式响应。Phase 3 闭合了接收端的循环：接收的事件匹配 `EventSubscription`，执行受信任的本地插件，其输出成为可通过 `routerctl dynamic render` 确认的 `DynamicConfigPart`。

本指南使用随附的提供商无关示例插件 `event-to-remote-claim`。

## 流程

```
on-prem routerd                         cloud routerd
---------------                         -------------
观测 LAN 客户端
  -> 发布联邦事件 --push-->  接收事件 (EventGroup)
     routerd.client.ipv4.observed         |
                                          v
                                   EventSubscription 匹配
                                          |
                                          v
                                   Plugin 执行 (event-to-remote-claim)
                                          |
                                          v
                                   PluginResult -> DynamicConfigPart
                                          |
                                          v
                                   routerctl dynamic render
                                     显示 RemoteAddressClaim
```

1. **发布** — on-prem 节点观测到客户端，向共享 `EventGroup` 发布 `routerd.client.ipv4.observed` 事件。
2. **传输（Phase 2）** — 事件通过 overlay 推送到 cloud 节点的 `EventGroup` 接收端。
3. **匹配** — cloud 节点的 `EventSubscription` 按类型（以及可选的 subject 前缀 / 源节点）匹配事件。
4. **插件** — subscription 的 `trigger.pluginRef` Plugin 接收匹配事件的 stdin 并执行，返回 `PluginResult`。
5. **DynamicConfigPart** — routerctl 验证结果，保存为带有 provenance 注解（`routerd.net/event-id`、`routerd.net/event-group`、`routerd.net/dynamic-source`）的动态配置部分。
6. **渲染** — `routerctl dynamic render` 显示包含新 `RemoteAddressClaim` 的有效配置。

## 示例资源

- 接收端（cloud）的配线：[`examples/event-federation/receiver-cloud.yaml`](https://github.com/imksoo/routerd/blob/main/examples/event-federation/receiver-cloud.yaml) — `EventGroup`、`EventSubscription`、`Plugin`，以及解析结果 `RemoteAddressClaim` 的混合上下文（`OverlayPeer`、`AddressMobilityDomain`、`CloudProviderProfile`）。
- 发送端（on-prem）的配线：[`examples/event-federation/sender-onprem.yaml`](https://github.com/imksoo/routerd/blob/main/examples/event-federation/sender-onprem.yaml) — `EventGroup` + `EventPeer` 推送目标。
- 示例插件：[`examples/plugins/event-to-remote-claim/`](https://github.com/imksoo/routerd/tree/main/examples/plugins/event-to-remote-claim)。

## 试一试

构建并安装示例插件：

```sh
go build -o bin/event-to-remote-claim ./examples/plugins/event-to-remote-claim
install -D bin/event-to-remote-claim \
  /usr/local/libexec/routerd/plugins/event-to-remote-claim/bin/event-to-remote-claim
```

应用接收端配置，并发布测试事件（通常由 Phase 2 从 on-prem 节点交付事件）：

```sh
routerctl federation event emit \
  --state-file /var/lib/routerd/routerd.db \
  --group cloudedge --type routerd.client.ipv4.observed \
  --subject 10.88.60.9/32 --source-node onprem-router \
  --payload address=10.88.60.9/32 \
  --payload domain=cloudedge-same-subnet \
  --payload ownerSide=onprem \
  --payload peerRef=onprem-main \
  --payload providerRef=example-provider \
  --ttl 30m
```

EventSubscription controller reconcile 后，渲染有效配置：

```sh
routerctl dynamic render \
  --config /usr/local/etc/routerd/router.yaml \
  --state-file /var/lib/routerd/routerd.db
```

将显示带有事件 provenance 注解的 `10.88.60.9/32` 的 `RemoteAddressClaim`。

## 范围与安全性

- 示例插件是**提供商无关的**，**不执行任何云端变更**。`capture` 块是 dry-run 意图的占位符（`configureOSAddress: false`）。
- 实际发起地址 claim 的提供商操作（`actionPlan`）执行属于 **Phase 4/5**，MVP 中不执行操作计划。
- routerd 不向插件传递配置或 secret — 仅传递观测到的事件。
- `EventSubscription.match.types` 是必需的，因此 subscription 不会在组内所有事件上无差别地触发插件。为防止循环，请使用 `subjectPrefixes` 和 `sourceNodes` 进一步缩小范围。
