# Hostname

[各资源的配置](../resources.md#hostname) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

管理OS主机名，与Router.metadata.name及资源名不同。

## 指定条件与引用关系

- 多个受管Hostname在协调时失败；managed=false条目被controller忽略。Linux使用hostnamectl，FreeBSD使用sysrc与hostname。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: Hostname
  metadata:
    name: "<name>"
  spec:
    hostname: <string>
    [ managed: <boolean> ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.hostname` | **必填** | string | 必填非空OS主机名，无默认值，拒绝空格、tab、换行与/；不是DNSZone或Interface引用。 |
| `spec.managed` | *选填* | boolean | 默认false，不变更主机名。true观察当前/静态名称并按需变更；dry-run不调用变更命令。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/hostname.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/hostname.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `hostname`
