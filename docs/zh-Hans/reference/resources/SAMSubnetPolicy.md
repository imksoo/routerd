# SAMSubnetPolicy

[各资源的配置](../resources.md#samsubnetpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

将分片分配发布为联邦事件，让各节点将 MobilityPool 所有权发现限缩至负责的 IPv4 子网。这是发现范围分配，不是 IP 路由资源。

## 指定条件与引用关系

- sourcePrefix 及所有分片须为 IPv4 CIDR。分片须位于 sourcePrefix 内且不可重叠，但不要求完整覆盖源前缀。每个分片至少需一个非空且不重复的分配节点。
- 分配事件 10 分钟后过期，由协调程序更新。分配缺失、过期或事件存储读取错误时，不应用分片过滤，而非拒绝全部。不可仅以此资源作为授权边界。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMSubnetPolicy
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # 必填; string
    poolRef: "<string>" # 必填; string
    shards: # 必填; array
      - # object
        assignedNodes: # 必填; array
          - "<string>" # string
        prefix: "<string>" # 必填; string
    sourcePrefix: "<string>" # 必填; string
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.sourcePrefix` | 必填 IPv4 CIDR，限制所有分片范围，无默认值。用于包含验证，不应用为地址或路由。 |
| `spec.poolRef` | 必填 MobilityPool metadata.name，作为事件载荷精确匹配键，不是 Kind/name。目前验证只检查非空，不确认资源存在，须符合使用该事件的池名称。 |
| `spec.groupRef` | 必填 EventGroup 引用，供发布及读取分配使用。运行时须能解析组，结构验证仅检查非空。请与目标 MobilityPool.groupRef 使用相同引用写法。 |
| `spec.shards` | 必填且不可为空的不重叠子网及负责 nodeRef 列表。同一节点可出现在不同分片。 |
| `spec.shards[].prefix` | 必填且位于 sourcePrefix 内的 IPv4 CIDR，可等于整个源前缀。拒绝分片间重复或重叠。 |
| `spec.shards[].assignedNodes` | 必填且不可为空的 nodeRef 列表，同一分片内不可重复。目前不检查 SAMNodeSet 成员资格，值须符合消费端 EventGroup 本机节点标识。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/controller/mobility/shard_controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/shard_controller.go) · [pkg/controller/mobility/discovery.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery.go)
