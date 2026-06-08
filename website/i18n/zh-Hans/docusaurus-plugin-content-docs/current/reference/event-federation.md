# Event Federation 参考

![展示从观测到的本地事实到 EventGroup、routerd-eventd push 分发、EventSubscription 匹配、plugin 派生的 DynamicConfigPart 输出的 Event Federation 示意图](/img/diagrams/reference-event-federation.png)

> 实验性（CloudEdge）。关于设计和不变条件，请参见 [ADR 0006: CloudEdge Event Federation](../adr/0006-event-federation.md)；
> 关于实践示例，请参见 how-to 的
> [Event Federation subscription](../how-to/event-federation-subscription.md)。

Event Federation 是一种机制，通过 overlay 在 routerd 节点之间交换**类型化的观测事实**
（例如："此客户端 IPv4 已被观测到"、"此地址已过期"），订阅者将匹配的事件通过 plugin
转换为派生配置。它是[选择性地址移动性](./selective-address-mobility)下的控制平面基础设施，
一个节点上观测到的地址会成为另一个节点的 `RemoteAddressClaim`（capture）。

模型是**幂等观测事实事件的 at-least-once 分发**。事件是关于世界的不可变描述
（"observed"），而非命令式指令。对于从同一事件重新导出相同状态的接收者来说是 no-op。

## Kind

### `EventGroup`

节点参加的总线。每个节点在每个组中拥有一个 identity。

| 字段 | 含义 |
|---|---|
| `nodeName` | 此节点在组内的 identity。作为 `sourceNode` 印刻在发布的事件上。 |
| `retention` | 本地存储保留事件的数量/时长上限。空/零 = 无限制。 |
| `auth` | 对等分发（push）用的 HMAC 密钥材料。 |
| `listen` | 入站对等 push 的接收器绑定（`address`）。空 = 仅 push（无接收器）。 |
| `replayWindow` | 用于重放保护而接受的消息时间戳偏差上限的 Go duration（默认 `5m`）。 |

### `EventPeer`

此节点向其 push 事件的远程节点。

| 字段 | 含义 |
|---|---|
| `groupRef` | 此对等方所属的 `EventGroup`（必需）。 |
| `nodeName` | 远程对等方的节点 identity（必需）。 |
| `endpoint` | push 目标的基础 URL。例：`http://10.99.0.7:8787`（push 时必需）。 |
| `direction` | 分发方向。仅支持 `push`。为空时默认 `push`。 |
| `types` | 可选的事件类型允许列表。为空时全部分发。 |
| `subjectPrefixes` | 可选的主题前缀允许列表。为空时全部分发。 |

### `EventSubscription`

将匹配的事件转换为发出 `DynamicConfigPart` 的 plugin 调用。

| 字段 | 含义 |
|---|---|
| `groupRef` | 消费的 `EventGroup`。 |
| `match` | 按类型/主题匹配事件的条件。 |
| `trigger.pluginRef` | 匹配事件时调用的 `Plugin`。 |
| `trigger.batchWindow` | 将匹配事件聚合为一次调用的 Go duration。 |
| `trigger.debounce` | 在最后一个匹配事件之后延迟调用的 Go duration。 |

## `routerctl federation` CLI

```
routerctl federation event emit  --group <g> --type <topic> --subject <entity> [--source-node <n>] [--ttl <dur>] [--payload k=v ...]
routerctl federation event list  --group <g>
routerctl federation event deliveries --group <g>
```

`emit` 将观测事实记录到本地存储（例如：
`--type routerd.client.ipv4.observed --subject 10.88.60.9/32`）。`list` 显示已记录的
事件，`deliveries` 显示每个对等方的 push 分发状态。

> 自我捕获保护（ADR 0006 的 no-feedback-loop 不变条件）：节点不得为自身通过本地
> `RemoteAddressClaim` capture 的地址发出 `routerd.client.ipv4.observed`。否则，
> 已分发的 capture 地址将作为新观测回环。

## 传输 — `routerd-eventd`

`routerd-eventd@<group>` 是每组一个的长寿命守护进程（Linux 上由生成的 systemd unit
管理，FreeBSD 上由 rc.d 管理），执行以下操作：

- 将本地记录的事件通过 HTTP **push** 到每个 `EventPeer`，并使用组 HMAC 签名。
  接收方验证签名并拒绝 `replayWindow` 之外的消息。
- 按对等方/事件记录**分发**状态，限制 at-least-once 重试的范围并使其可观测。
- 根据组的 `retention` **修剪**本地事件存储。

outbox 具有 `sourceNode` 保护，接收到的事件不会被转发回发起方（无分发循环）。

## Subscription -> plugin -> DynamicConfigPart 流程

1. 节点发出观测事实（`routerctl federation event emit`，或未来的 observer）。
2. `routerd-eventd` 向对等方分发，各对等方记录到自身的事件存储。
3. 对等方的 `EventSubscription` 匹配事件并调用 `trigger.pluginRef`
   （通过 `batchWindow` / `debounce` 聚合）。
4. plugin 返回 `DynamicConfigPart`（例如：`RemoteAddressClaim`），
   [dynamic-config](./dynamic-config.md) 链将其整合到 effective config 并
   reconcile 到数据平面。

这样运维人员编写的 intent 保持声明式。运维人员声明 group/peers/subscription，
claim、capture、action plan 均为**派生**产物。
