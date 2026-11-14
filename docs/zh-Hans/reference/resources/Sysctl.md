# Sysctl

[各资源的配置](../resources.md#sysctl) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

协调一项内核sysctl，并可写入持久sysctl.d文件；没有指向其他routerd资源的引用字段。

## 指定条件与引用关系

- runtime与persistent均false时记为Skipped/RuntimeDisabled，不应用值；两者可以同时为true。
- 同一Router不可重复Sysctl键。Linux的NAT44SessionSync使用conntrackd时，另须显式声明net.netfilter.nf_conntrack_tcp_be_liberal，启用runtime、optional=false，且value与有效expectedValue均为1。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: Sysctl
  metadata:
    name: "<name>"
  spec:
    [ compare: ("" | "exact" | "atLeast") ]
    [ expectedValue: <string> ]
    key: <string>
    [ optional: <boolean> ]
    [ persistent: <boolean> ]
    [ runtime: <boolean> ]
    value: <string>
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.key` | **必填** | string | 必填内核键，例如net.ipv4.ip_forward，无默认值；禁止空格、tab、换行及/，不是资源引用。 |
| `spec.value` | **必填** | string | 传给sysctl -w的必填非空字符串，持久化时也使用此值。YAML中看似数字的值须加引号，无默认值。 |
| `spec.expectedValue` | *选填* | string | 用于运行时偏差比较的预期值，默认为value，可处理写入值与观测表示不同的情况。拒绝仅空白的值。 |
| `spec.compare` | *选填* | string; enum=["", "exact", "atLeast"] | 默认exact，将空白规范化后按字符串比较。atLeast逐一比较有符号整数，要求元素数相同且每个当前值不小于预期值。 |
| `spec.runtime` | *选填* | boolean | 是否应用到当前内核，默认true；false时仍可在persistent=true下写入持久配置。 |
| `spec.persistent` | *选填* | boolean | 将key=value写入routerd的sysctl.d文件，默认false，与runtime独立。 |
| `spec.optional` | *选填* | boolean | 默认false。true时读取失败记为Skipped/ReadFailedOptional并跳过此项后续应用；不会忽略任意写入或持久化错误。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/sysctl.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sysctl.go) · [pkg/sysctlprofile/compare.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/compare.go) · [pkg/sysctlprofile/persistent.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/persistent.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `key`, `value`, `dryRun`
