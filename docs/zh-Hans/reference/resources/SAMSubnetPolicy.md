# SAMSubnetPolicy

[各资源的配置](../resources.md#samsubnetpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

将分片分配发布为联邦事件，让各节点将 MobilityPool 所有权发现限缩至负责的 IPv4 子网。这是发现范围分配，不是 IP 路由资源。

## 指定条件与引用关系

- sourcePrefix 及所有分片须为 IPv4 CIDR。分片须位于 sourcePrefix 内且不可重叠，但不要求完整覆盖源前缀。每个分片至少需一个非空且不重复的分配节点。
- 分配事件 10 分钟后过期，由协调程序更新。分配缺失、过期或事件存储读取错误时，不应用分片过滤，而非拒绝全部。不可仅以此资源作为授权边界。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMSubnetPolicy
  metadata:
    name: "<name>"
  spec:
    groupRef: <string>
    poolRef: <string>
    shards:
      -
        assignedNodes:
          - <string>
        prefix: <string>
    sourcePrefix: <string>
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.sourcePrefix` | **必填** | string | 必填 IPv4 CIDR，限制所有分片范围，无默认值。用于包含验证，不应用为地址或路由。 |
| `spec.poolRef` | **必填** | string | 必填 MobilityPool metadata.name，作为事件载荷精确匹配键，不是 Kind/name。目前验证只检查非空，不确认资源存在，须符合使用该事件的池名称。 |
| `spec.groupRef` | **必填** | string | 必填 EventGroup 引用，供发布及读取分配使用。运行时须能解析组，结构验证仅检查非空。请与目标 MobilityPool.groupRef 使用相同引用写法。 |
| `spec.shards` | **必填** | array | 必填且不可为空的不重叠子网及负责 nodeRef 列表。同一节点可出现在不同分片。 |
| `spec.shards[].prefix` | **必填** | string | 必填且位于 sourcePrefix 内的 IPv4 CIDR，可等于整个源前缀。拒绝分片间重复或重叠。 |
| `spec.shards[].assignedNodes` | **必填** | array | 必填且不可为空的 nodeRef 列表，同一分片内不可重复。目前不检查 SAMNodeSet 成员资格，值须符合消费端 EventGroup 本机节点标识。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/controller/mobility/shard_controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/shard_controller.go) · [pkg/controller/mobility/discovery.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

此 Kind 未登记常规状态引用的允许字段。
