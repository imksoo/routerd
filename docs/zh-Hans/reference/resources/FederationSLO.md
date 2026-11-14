# FederationSLO

[各资源的配置](../resources.md#federationslo) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

配置 routerctl doctor 诊断联合发送与订阅时使用的每组阈值，不改变发送重试或 Plugin 执行策略。

## 指定条件与引用关系

- groupRef 必填且须为已有 EventGroup 名称。每个组最多一个 FederationSLO。所有数值须非负；应用零值默认值后，有效 lagWarnSeconds 必须严格小于有效 lagFailSeconds。
- 延迟达到阈值（>=）即 WARN/FAIL；订阅数量严格超过允许最大值才 WARN/FAIL。即将过期使用剩余 TTL 严格小于阈值；待发或失败发送可使该诊断由 WARN 升为 FAIL。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: FederationSLO
  metadata:
    name: "<name>"
  spec:
    delivery: # 选填; object
      expiresSoonSeconds: 0 # 选填; integer; minimum=0
      lagFailSeconds: 0 # 选填; integer; minimum=0
      lagWarnSeconds: 0 # 选填; integer; minimum=0
    groupRef: "<string>" # 必填; string
    subscription: # 选填; object
      maxFailedRuns: 0 # 选填; integer; minimum=0
      maxPendingRuns: 0 # 选填; integer; minimum=0
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.groupRef` | 必填单独 EventGroup metadata.name，不是 EventGroup/name。验证存在性与每组唯一性。 |
| `spec.delivery` | 可选的发送诊断阈值，省略字段使用下列默认值。不是传输超时或重试配置。 |
| `spec.delivery.lagWarnSeconds` | 非负秒数，省略或 0 为 60。最大测量发送延迟 >= 此值时为 WARN，除非已达 FAIL 阈值。 |
| `spec.delivery.lagFailSeconds` | 非负秒数，省略或 0 为 180。最大测量发送延迟 >= 此值时为 FAIL，须大于有效 WARN 阈值。 |
| `spec.delivery.expiresSoonSeconds` | 非负秒数，省略或 0 为 120。事件剩余 TTL 低于此值视为即将过期。0 无法禁用检查，无期限事件另行处理。 |
| `spec.subscription` | 可选的订阅执行诊断容许数，默认 pending/failed 容许均为 0。不限制控制器队列或重试次数。 |
| `spec.subscription.maxPendingRuns` | 非负数量，默认 0。待处理数大于此值时为 WARN，但若失败数已导致 FAIL 则优先 FAIL。 |
| `spec.subscription.maxFailedRuns` | 非负数量，默认 0。失败数大于此值时为 FAIL，不改变订阅最多三次的尝试策略。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [cmd/routerctl/doctor_federation.go](https://github.com/imksoo/routerd/blob/main/cmd/routerctl/doctor_federation.go)
