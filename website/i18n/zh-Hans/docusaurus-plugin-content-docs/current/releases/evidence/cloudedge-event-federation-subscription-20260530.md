# CloudEdge Event Federation Phase 3 subscription 冒烟测试

Result: PASS

日期: 2026-05-30
分支/构建: event-federation / 515fe7e8d086
构建命令: `make dist`

证据包:
`/home/imksoo/routerd-labs/event-federation/evidence/20260530T111612Z-phase3-subscription-515fe7e8`

## 拓扑

冒烟测试使用了与 Phase 2 相同的 PVE 专用对。未启动云 VM。

- 发送侧: router03 / 192.168.123.125 / `router03.lain.local`
- 接收侧: router05 / 192.168.123.127 / `router05.lain.local`
- EventGroup: `cloudedge-event-smoke`
- 发送侧 EventGroup nodeName: `onprem-event-node`
- 接收侧 EventGroup nodeName: `cloud-event-node`
- 接收侧 listen: `169.254.250.5:9443`
- 发送侧 EventPeer endpoint: `http://169.254.250.5:9443`
- AddressMobilityDomain: `cloudedge-same-subnet`
- Plugin 可执行文件路径: `/usr/local/libexec/routerd/plugins/event-to-remote-claim/bin/event-to-remote-claim`

发送侧和接收侧的配置分别从以下文件应用:

- `examples/event-federation/sender-onprem.yaml`
- `examples/event-federation/receiver-cloud.yaml`

在接收侧的 plugin 路径上设置了一个将 stdin 记录到日志的包装器, 然后将构建好的示例插件二进制文件作为 `event-to-remote-claim.real` 执行。这样可以在不修改 plugin 输出的情况下获取 `PluginRequest.spec.events` 的证据。

## 部署

- `515fe7e8` 的 `make dist` 已完成。
- `routerd`、`routerctl`、`routerd-eventd` 已部署到两个节点。
- 示例插件使用 `CGO_ENABLED=0 GOOS=linux` 单独构建并安装到 router05。
- 两个生成的配置均通过 `routerd check`。
- 两个节点上 `routerd-eventd@cloudedge-event-smoke.service` 均处于活动状态。
- 接收侧的 `ss` 确认 `169.254.250.5:9443` 上的监听器。
- overlay 双向可达性通过:
  - router03 -> `169.254.250.5`: 3/3 ping, 0% loss
  - router05 -> `169.254.250.3`: 3/3 ping, 0% loss

## 主断言

事件:

- ID: `evt-phase3-smoke-20260530T112250Z`
- Type: `routerd.client.ipv4.observed`
- Subject: `10.88.60.9/32`
- SourceNode: `onprem-event-node`
- Payload domain: `cloudedge-same-subnet`
- Payload ownerSide: `onprem`

结果:

- 从发送侧到 `cloud-event-node` 的传递: `delivered`, attempts `1`
- 接收侧 federation 存储中存在相同事件 ID
- EventSubscription 执行:
  - subscription: `EventSubscription/cloud-claims`
  - plugin: `event-to-remote-claim`
  - status: `succeeded`
  - attempts: `1`
  - dynamic source: `EventSubscription/cloud-claims/07634fdff9b3235c`
- Plugin 执行:
  - trigger type: `federation-subscription`
  - trigger topic: `cloud-claims`
  - exit code: `0`
  - status: `succeeded`
- `routerctl dynamic list -o json` 显示 1 个活动的 DynamicConfigPart。
- `routerctl dynamic render -o yaml` 的显示:
  - kind: `RemoteAddressClaim`
  - name: `onprem-10-88-60-9`
  - address: `10.88.60.9/32`
  - domainRef: `cloudedge-same-subnet`
  - ownerSide: `onprem`
  - capture.type: `provider-secondary-ip`
  - capture.providerRef: `example-provider`
  - capture.nicRef: `example-nic-ref`
  - delivery.peerRef: `onprem-main`
  - delivery.tunnelInterface: `wg-hybrid`

渲染的 claim 附带了来源注解:

- `routerd.net/dynamic-source: EventSubscription/cloud-claims`
- `routerd.net/event-group: cloudedge-event-smoke`
- `routerd.net/event-id: evt-phase3-smoke-20260530T112250Z`
- `routerd.net/event-subject: 10.88.60.9/32`

捕获的 PluginRequest 在 `spec.events` 下包含具有相同 ID、subject、source node、payload 的主事件。

## 否定检查

重复幂等性: PASS

- 重新 emit `evt-phase3-smoke-20260530T112250Z` 不会产生新的 subscription 执行。
- 主事件的传递保持 attempts `1`。
- DynamicConfigPart 数量保持 `1`。
- 渲染的 `RemoteAddressClaim/onprem-10-88-60-9` 数量保持 `1`。
- Plugin 请求日志保持 1 个成功请求。

非匹配事件: PASS

- 事件 ID: `evt-phase3-nonmatch-20260530T112250Z`
- ownerSide: `cloud`
- 传输传递: `delivered`
- 接收侧存储了事件。
- 未创建 subscription 执行。
- 无 `10.88.60.10/32` 的 DynamicConfigPart 或渲染的 claim。

过期事件: PASS

- 事件 ID: `evt-phase3-expired-20260530T112250Z`
- ObservedAt: `2026-05-30T11:14:07Z`
- ExpiresAt: `2026-05-30T11:14:08Z`
- 发送侧传递查询: `null`
- 接收侧未收到过期事件。
- 无 `10.88.60.11/32` 的 subscription 执行或渲染的 claim。

Plugin 失败重试上限: PASS

- 事件 ID: `evt-phase3-pluginfail-20260530T112250Z`
- 匹配实验室专用的 `EventSubscription/cloud-claims-fail`。
- 失败 plugin 以 exit code `42` 退出。
- `event_subscription_runs` 以 `status=failed`, `attempts=3` 结束。
- 记录了 3 行失败 plugin 执行。
- 未创建 `10.88.60.66/32` 的 DynamicConfigPart。

## 范围检查

- 未执行 provider action。
- 未创建、启动、停止或修改云资源。
- 未尝试 Phase 4 的 actionPlan 执行。
- 未执行 SAM 数据平面 apply; RemoteAddressClaim 仅存在于 `routerctl dynamic render` 中。
- 未使用 ARP observer、provider 特定 plugin、DynamicConfigPart consumer 路径。

## 判定

Phase 3 控制平面自动化通过:

manual emit -> transport -> EventSubscription match -> plugin.Run ->
DynamicConfigPart -> `routerctl dynamic render` RemoteAddressClaim。

Phase 4 尚未开始。

## Pre-flight 备注

Pre-flight 在冒烟测试进入执行后才被要求。主路径通过, 生成的 PluginResult/DynamicConfigPart 追溯确认了以下内容:

- payload domain 与 `AddressMobilityDomain.metadata.name` (`cloudedge-same-subnet`) 匹配
- plugin 可执行文件存在并被调用 (`event-to-remote-claim`, exit 0)
- 接收侧的 hybrid 上下文完整 (渲染的 `RemoteAddressClaim` 针对接收侧配置解析了
  `domainRef` / `delivery.peerRef` / `capture.providerRef`, 并通过 `dynamic render` 验证)
- 未尝试 provider mutation

即 pre-flight 未被跳过 — 主路径 PASS 证明了配置/上下文的正确性。
