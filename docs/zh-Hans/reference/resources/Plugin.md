# Plugin

[各资源的配置](../resources.md#plugin) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: plugin.routerd.net/v1alpha1` · `metadata.name`

注册实现routerd插件协议的可执行文件，由显式操作或EventSubscription等使用方调用。

## 指定条件与引用关系

- executable必须是绝对路径。执行时须为存在的普通文件，且至少有一个执行权限位。
- 当前实现不会仅因声明triggers而启动周期或事件调度；它会验证并在plugin list显示，仍须由使用方调用。
- context使用apiVersion、kind、name三项标识资源。目标不存在不会造成配置验证错误，而是在执行时省略该项；秘密信息会脱敏。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: plugin.routerd.net/v1alpha1
  kind: Plugin
  metadata:
    name: "<name>"
  spec:
    [ capabilities:
        - ("observe.cloud" | "observe.providerPrivateIPs" | "propose.dynamicConfig" | "propose.providerAction" | "execute.providerAction")
    ]
    [ context:
        [ resources:
            -
              apiVersion: <string>
              kind: <string>
              name: <string>
        ]
    ]
    [ env:
        [ <name>: <string> ]
    ]
    executable: <string>
    [ timeout: <string> ]
    [ triggers:
        -
          [ every: <string> ]
          [ topic: <string> ]
          type: ("interval" | "event")
    ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.executable` | **必填** | string | 外部程序的绝对路径；必填，无默认值。 |
| `spec.timeout` | *选填* | string | 单次执行时间上限。默认10s；显式指定时须为正的Go duration，例如30s。 |
| `spec.env` | *选填* | object | 以变量名为键的额外环境变量，默认空。runner先设置自身PATH，缺少时使用内置路径，再应用这些值；不继承其他父进程变量。忽略空键及含=的键。 |
| `spec.capabilities` | *选填* | array | 能力声明，默认空。接受observe.cloud、observe.providerPrivateIPs、propose.dynamicConfig、propose.providerAction、execute.providerAction。清单获取须有observe.providerPrivateIPs；供应商操作执行须有execute.providerAction；此列表本身不会启动执行。 |
| `spec.context` | *选填* | object | 控制通过标准输入传入哪些配置资源的允许列表；默认空，不传入配置。 |
| `spec.context.resources` | *选填* | array | 允许的资源标识列表，默认空；不展开通配符。 |
| `spec.context.resources[].apiVersion` | **条件必填** | string | 引用配置资源的API组与版本；每项必填。 |
| `spec.context.resources[].kind` | **条件必填** | string | 引用配置资源的类型；每项必填。 |
| `spec.context.resources[].name` | **条件必填** | string | 引用配置资源的metadata.name；每项必填。 |
| `spec.triggers` | *选填* | array | 触发声明列表，默认空；当前runtime仅验证与显示，请参照上方限制。 |
| `spec.triggers[].type` | **条件必填** | string; enum=["interval", "event"] | 触发类型；必须指定interval或event，无默认值。 |
| `spec.triggers[].every` | *选填* | string | interval时必填，须为正的Go duration；event时此字段不产生周期调度，无默认值。 |
| `spec.triggers[].topic` | *选填* | string | 事件主题声明；选填，默认空，event时验证器也不强制要求。这不是资源名称。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_plugin.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_plugin.go) · [pkg/plugin/runner.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/runner.go) · [pkg/plugin/context.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/context.go) · [pkg/providerinventory/executor.go](https://github.com/imksoo/routerd/blob/main/pkg/providerinventory/executor.go) · [pkg/provideraction/executor.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/executor.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

此 Kind 未登记常规状态引用的允许字段。
