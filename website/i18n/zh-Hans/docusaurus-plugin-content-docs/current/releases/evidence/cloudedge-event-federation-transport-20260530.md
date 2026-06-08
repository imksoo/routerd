# CloudEdge Event Federation Phase 2 传输冒烟测试

Result: PASS

日期: 2026-05-30
分支/构建: event-federation / f951fd471a7e
构建命令: `make dist`

证据包:
`/home/imksoo/routerd-labs/event-federation/evidence/20260530T091652Z-phase2-transport-f951fd47`

## 拓扑

传输专用冒烟测试使用了 PVE 专用对。Azure、AWS、OCI 的 VM 均未启动。

- 发送侧: router03 / 192.168.123.125 / `router03.lain.local`
- 接收侧: router05 / 192.168.123.127 / `router05.lain.local`
- EventGroup: `cloudedge-event-smoke`
- 发送侧 EventGroup nodeName: `onprem-event-node`
- 接收侧 EventGroup nodeName: `cloud-event-node`
- Overlay: `wg-hybrid`
- 发送侧 overlay 地址: `169.254.250.3/32`
- 接收侧 overlay 地址: `169.254.250.5/32`
- 接收侧 eventd listen: `169.254.250.5:9443`
- 发送侧 EventPeer endpoint: `http://169.254.250.5:9443`

emit 的事件使用 `--source-node onprem-event-node`, 与发送侧 EventGroup 的 `spec.nodeName` 匹配。

## 部署证据

- `make dist` 使用静态 Linux 工件完成。
- 构建 `f951fd471a7e` 的 `routerd`、`routerctl`、`routerd-eventd` 已部署到两个节点。
- 两个生成的配置均通过 `routerd check`。
- 接收侧的 `routerd-eventd@cloudedge-event-smoke.service` 在 `169.254.250.5:9443` 上监听。
- 发送侧的 `routerd-eventd@cloudedge-event-smoke.service` 按预期仅执行 push/prune。
- overlay 双向可达性通过:
  - router03 -> `169.254.250.5`: 3/3 ping, 0% loss
  - router05 -> `169.254.250.3`: 3/3 ping, 0% loss
  - router03 到 `http://169.254.250.5:9443/` 的 curl: eventd 返回 HTTP 404, 证明监听器可达

## 断言

### A. 发送侧本地存储

PASS。发送侧存储了事件:

- ID: `evt-phase2-smoke-20260530T092231Z`
- Group: `cloudedge-event-smoke`
- SourceNode: `onprem-event-node`
- Type: `routerd.client.ipv4.observed`
- Subject: `10.88.60.9/32`

### B. 发送侧传递

PASS。发送侧的传递到达接收侧 peer:

- EventID: `evt-phase2-smoke-20260530T092231Z`
- Peer: `cloud-event-node`
- Status: `delivered`
- Attempts: `1`
- DeliveredAt: `2026-05-30T09:22:41Z`

### C. 接收侧存储

PASS。接收侧存储了相同事件 ID:

- EventID: `evt-phase2-smoke-20260530T092231Z`
- 接收侧 RecordedAt: `2026-05-30T09:22:43Z`
- 首次传递后的接收侧状态: `received=1 duplicate=0 rejected=0 storedEvents=1`

### D. 幂等重复

PASS。重新 emit 相同事件 ID 未在接收侧创建第二个事件。

- 发送侧传递保持 `attempts=1`
- 接收侧仅有 1 个 ID 为 `evt-phase2-smoke-20260530T092231Z` 的事件
- 接收侧状态保持 `received=1 duplicate=0 storedEvents=1`

### E. 非法 HMAC

PASS。携带非法 `X-Routerd-Signature` 的合成 POST 返回:

- HTTP 状态: `401 Unauthorized`
- Body: `bad signature`
- 接收侧存储无变更
- 接收侧状态 `rejected=1` 递增

### F. 过期事件

PASS。过期事件在发送侧本地存储但未被推送。

- 过期 EventID: `evt-expired-20260530T092347Z`
- ObservedAt: `2026-05-30T09:14:02Z`
- ExpiresAt: `2026-05-30T09:14:03Z`
- 发送侧传递查询: `null`
- 接收侧未收到过期事件

### G. 重启后恢复

PASS。发送侧 eventd 停止期间 emit 的新事件在发送侧 eventd 服务重启后被传递。

- 恢复 EventID: `evt-resume-20260530T092347Z`
- emit 前的发送侧 eventd: `inactive`
- 发送侧重启前的接收侧: 仅原始主事件
- 重启后的传递: `delivered`, `attempts=1`, `deliveredAt=2026-05-30T09:24:18Z`
- 恢复后的接收侧状态: `received=2 duplicate=0 rejected=1 storedEvents=2`

## 已知实验室备注

- PVE 接收侧已有防火墙的 default-drop 策略。为使 eventd 能在新 overlay 接口上接收流量, 此次冒烟测试将 `WireGuardInterface/wg-hybrid` 添加到 router05 的既有管理防火墙 zone。
- 添加了 overlay peer 地址的显式 `/32` 路由资源:
  router03 上 `169.254.250.5 dev wg-hybrid metric 120`,
  router05 上 `169.254.250.3 dev wg-hybrid metric 120`。
- 运行手册使用了 `routerctl federation event deliveries --group ...`,
  但当前 CLI 支持通过 `--event-id` 查找传递。断言中使用了
  `--event-id`。
- `make dist` 最初未将 `routerd-eventd` 包含在发布载荷中。
  在工作树中的追加使其包含在 Makefile 的发布构建/安装列表中。

## 判定

CloudEdge Event Federation Phase 2 传输专用冒烟测试通过:

- 本地 emit 持久化到发送侧 SQLite
- outbox 循环向 EventPeer 推送
- 接收侧 HMAC 验证接受有效事件
- 接收侧持久化相同事件 ID
- 发送侧传递变为 `delivered`
- 重复 ID 被幂等处理
- 非法 HMAC 被 401 拒绝
- 过期事件未被传递
- 重启后恢复证明了基于 SQLite 的 outbox 传递
- 未使用 EventSubscription、plugin 触发、DynamicConfigPart、ARP observer、provider action、云 mutation
