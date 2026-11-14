# TrafficFlowLog

[各资源的配置](../resources.md#trafficflowlog) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

将连接快照记录至 SQLite 流量数据库，并可从 DPI 缓存补充信息。不是抓包，也不记录每个数据包。

## 指定条件与引用关系

- 观察器选择第一个可解码的 TrafficFlowLog，即使它已禁用；后续启用的资源不会覆盖。请配置单个资源以确定记录行为。
- 成功观察 conntrack 后才记录；观察不可用时该轮不记录。连接查询上限为 10000 条。每次观察器协调时采样，通常每 30 秒一次，可由运行选项调整。
- DPI 补充会读取第一个启用的 FirewallEventLog 数据库，使用一小时内符合连接组合的分类缓存。include 标志不保证成功补充；两者均 false 时仍可使用可用缓存。保留期限由 LogRetention 的 trafficFlows 管理。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: TrafficFlowLog
  metadata:
    name: "<name>"
  spec:
    enabled: true # 选填; boolean
    includeApplicationLayer: true # 选填; boolean
    includeTLSSNI: true # 选填; boolean
    path: "<string>" # 选填; string
    source: "" # 选填; string; enum=["", "conntrack"]
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.enabled` | 默认 false。true 启用所选资源的记录，不会使不可用的连接来源变为可用。 |
| `spec.path` | 路由器上的 SQLite 数据库路径，正常验证在 enabled 时必填。读取器有内部 /var/lib/routerd/traffic-flows.db 后备值，但不免除启用时的路径要求。 |
| `spec.source` | 仅接受省略或 conntrack，实际标签默认 conntrack。不用于选择其他采集器或任意后端名称。 |
| `spec.includeApplicationLayer` | 默认 false。与 enabled 同为 true 时请求 DPI 辅助服务及 Linux DPI 观察规则，并在资源状态的 applicationLayer 记录 nDPI 可用性。不保证 libndpi 加载或分类存在。 |
| `spec.includeTLSSNI` | 默认 false。与 enabled 同为 true 时请求相同 DPI 辅助程序及观察流程。不是 TLS 解密、SNI 采集保证或独立存储脱敏／过滤开关；加密或未观察到的名称可能不可用。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/controller/conntrackobserver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/conntrackobserver/controller.go) · [pkg/render/dpi_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dpi_systemd.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `path`, `sinks`
