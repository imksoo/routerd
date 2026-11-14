# SysctlProfile

[各资源的配置](../resources.md#sysctlprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

按名称应用路由器用sysctl集合，并可逐键覆盖；不是按名称引用Sysctl资源。

## 指定条件与引用关系

- 覆盖现有键仅改变值，保留比较方式与optional。新增键采用exact比较及optional=false。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: SysctlProfile
  metadata:
    name: "<name>"
  spec:
    [ overrides:
        [ <name>: <string> ]
    ]
    [ persistent: <boolean> ]
    profile: ("router-linux" | "router-freebsd")
    [ runtime: <boolean> ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.profile` | **必填** | string; enum=["router-linux", "router-freebsd"] | 必填router-linux或router-freebsd，不按OS自动选择。FreeBSD集合启用IPv4/IPv6转发；Linux还包含队列、socket、TCP及可选conntrack调优。完整键值请见实现链接。 |
| `spec.runtime` | *选填* | boolean | 将集合各项应用到当前内核；默认true。 |
| `spec.persistent` | *选填* | boolean | 为各项写入持久sysctl.d文件；默认false，与runtime独立。 |
| `spec.overrides` | *选填* | object | 内核sysctl键到非空白字符串值的映射，默认空；替换现有键值并新增其他键。拒绝含空白或/的键及空白值，不是资源引用。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/sysctl.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sysctl.go) · [pkg/sysctlprofile/profile.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/profile.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `profile`, `dryRun`
