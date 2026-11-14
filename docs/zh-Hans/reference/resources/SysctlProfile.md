# SysctlProfile

[各资源的配置](../resources.md#sysctlprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

按名称应用路由器用sysctl集合，并可逐键覆盖；不是按名称引用Sysctl资源。

## 指定条件与引用关系

- 覆盖现有键仅改变值，保留比较方式与optional。新增键采用exact比较及optional=false。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: SysctlProfile
  metadata:
    name: "<name>"
  spec:
    overrides: # 选填; object
      "<key>": "<string>" # 选填; string
    persistent: true # 选填; boolean
    profile: "router-linux" # 必填; string; enum=["router-linux", "router-freebsd"]
    runtime: true # 选填; boolean
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.profile` | 必填router-linux或router-freebsd，不按OS自动选择。FreeBSD集合启用IPv4/IPv6转发；Linux还包含队列、socket、TCP及可选conntrack调优。完整键值请见实现链接。 |
| `spec.runtime` | 将集合各项应用到当前内核；默认true。 |
| `spec.persistent` | 为各项写入持久sysctl.d文件；默认false，与runtime独立。 |
| `spec.overrides` | 内核sysctl键到非空白字符串值的映射，默认空；替换现有键值并新增其他键。拒绝含空白或/的键及空白值，不是资源引用。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/sysctl.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sysctl.go) · [pkg/sysctlprofile/profile.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/profile.go)
