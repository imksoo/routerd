# FederationSLO

[各资源的配置](../resources.md#federationslo) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

配置 routerctl doctor 诊断联合发送与订阅时使用的每组阈值，不改变发送重试或 Plugin 执行策略。

## 指定条件与引用关系

- groupRef 必填且须为已有 EventGroup 名称。每个组最多一个 FederationSLO。所有数值须非负；应用零值默认值后，有效 lagWarnSeconds 必须严格小于有效 lagFailSeconds。
- 延迟达到阈值（>=）即 WARN/FAIL；订阅数量严格超过允许最大值才 WARN/FAIL。即将过期使用剩余 TTL 严格小于阈值；待发或失败发送可使该诊断由 WARN 升为 FAIL。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: federation.routerd.net/v1alpha1
  kind: FederationSLO
  metadata:
    name: "<name>"
  spec:
    [ delivery:
        [ expiresSoonSeconds: <integer> ]
        [ lagFailSeconds: <integer> ]
        [ lagWarnSeconds: <integer> ]
    ]
    groupRef: <string>
    [ subscription:
        [ maxFailedRuns: <integer> ]
        [ maxPendingRuns: <integer> ]
    ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.groupRef` | **必填** | string | 必填单独 EventGroup metadata.name，不是 EventGroup/name。验证存在性与每组唯一性。 |
| `spec.delivery` | *选填* | object | 可选的发送诊断阈值，省略字段使用下列默认值。不是传输超时或重试配置。 |
| `spec.delivery.lagWarnSeconds` | *选填* | integer; minimum=0 | 非负秒数，省略或 0 为 60。最大测量发送延迟 >= 此值时为 WARN，除非已达 FAIL 阈值。 |
| `spec.delivery.lagFailSeconds` | *选填* | integer; minimum=0 | 非负秒数，省略或 0 为 180。最大测量发送延迟 >= 此值时为 FAIL，须大于有效 WARN 阈值。 |
| `spec.delivery.expiresSoonSeconds` | *选填* | integer; minimum=0 | 非负秒数，省略或 0 为 120。事件剩余 TTL 低于此值视为即将过期。0 无法禁用检查，无期限事件另行处理。 |
| `spec.subscription` | *选填* | object | 可选的订阅执行诊断容许数，默认 pending/failed 容许均为 0。不限制控制器队列或重试次数。 |
| `spec.subscription.maxPendingRuns` | *选填* | integer; minimum=0 | 非负数量，默认 0。待处理数大于此值时为 WARN，但若失败数已导致 FAIL 则优先 FAIL。 |
| `spec.subscription.maxFailedRuns` | *选填* | integer; minimum=0 | 非负数量，默认 0。失败数大于此值时为 FAIL，不改变订阅最多三次的尝试策略。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [cmd/routerctl/doctor_federation.go](https://github.com/imksoo/routerd/blob/main/cmd/routerctl/doctor_federation.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

此 Kind 未登记常规状态引用的允许字段。
