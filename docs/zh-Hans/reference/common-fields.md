# 共用引用与条件字段

## 资源标识与值来源

metadata.name是同一类型内的资源名称。值来源resource使用Kind/名称，不是OS接口名或URL。一般状态引用验证要求目标在同一Router中声明，且目标类型提供指定字段。optional: true也不会跳过这些检查。

```yaml
dnsServerFrom:
  - resource: IPv4StaticAddress/lan-address
    field: address
```

| 字段 | 含义、默认值与条件 |
| --- | --- |
| resource | 来源Kind/名称。使用来源或指定field时必须提供，不支持通配符。 |
| field | 要读取的address、phase、zone等字段，须由目标类型提供。底层读取器虽可默认phase，一般配置验证仍要求在指定resource时显式提供field，请勿省略。 |
| optional | 默认false。让支持此选项的使用方容许运行时值尚未解析，不会允许引用配置中不存在的资源。缺值行为请见各资源说明。 |

使用ValuesFromStoreOrRouter的读取处理，会先从声明配置获取DNSZone.zone或IPv4StaticAddress.address，再读取状态；其他读取器直接使用状态。声明引用不保证动态值已就绪，也不会自行建立执行顺序。

## 依赖条件

dependsOn是条件列表，全部必须成立；省略或空列表不设依赖限制。

```yaml
dependsOn:
  - resource: DHCPv6PrefixDelegation/wan-pd
    phase: Bound
```

| 字段 | 含义、默认值与条件 |
| --- | --- |
| resource | 依赖目标Kind/名称；配置中须有该资源，且提供所读字段。 |
| field | 默认phase；指定phase时不使用此值。 |
| phase | 要求目标phase匹配的简写；非空时优先于field与equals。 |
| equals | 要求任一返回值完全匹配。省略时要求首值非空白。 |
| notEmpty | 默认false，检查规范化后首值非空白。当前读取器已移除空白值，因此此选项通常不增加限制。 |
| optional | 默认false。运行时无值时只有true才通过；不会将已解析但不匹配的值视为通过，也不放宽配置引用验证。 |

## 启用条件

省略when不设条件。非空条件在每一层必须仅用state、all、any其中一种。all与any可嵌套包含条件，但子项不可为空。JSON Schema未列出这两个递归属性，实现仍接受。

```yaml
when:
  all:
    - state:
        DHCPv6PrefixDelegation/wan-pd.phase:
          equals: Bound
    - state:
        HealthCheck/upstream.phase:
          equals: Healthy
```

state以AND组合所有项；all以AND、any以OR组合子条件。资源状态键可用Kind/名称.field、Kind/名称.status.field或其${...}形式。资源格式的键会检查声明目标与提供字段，其他格式作为状态存储键。

| 状态条件 | 含义与省略行为 |
| --- | --- |
| exists | 省略则不限制存在性；true要求set，false要求unset。unknown与unset不同。 |
| equals | 非空值要求set与区分大小写的完全匹配；空值不比较。不存在条件请用exists或status。 |
| in | 非空列表要求set且完全匹配任一项；空列表不限制。 |
| contains | 非空值要求set且包含区分大小写的子串；空值不限制。 |
| status | 选填状态存储状态set、unset或unknown，与资源生命周期phase不同。 |
| for | 选填Go duration，要求观测状态经过时间至少达到此值。这不是整个条件的独立计时器；会验证语法，但不强制正值。 |
| maxAge | 选填正的Go duration，要求状态经过时间不超过此值。与for并用时两项限制均须成立。 |

同一状态的多个比较条件必须全部成立。未解析的unknown可能属于未确定，而非单纯false；部分有效配置路径会保留未确定资源，仅选择确定结果的路径则不会。VXLANTunnel拒绝显式接受unknown的条件。等待与撤回行为请见各资源说明。

实现：[引用验证](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go)、[条件验证](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)、[值与依赖读取](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go)、[条件求值](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go)、[提供字段](https://github.com/imksoo/routerd/blob/main/pkg/api/provides.go)。
