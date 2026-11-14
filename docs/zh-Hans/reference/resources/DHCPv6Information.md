# DHCPv6Information

[各资源的配置](../resources.md#dhcpv6information) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

从使用同接口的DHCPv6-PD daemon获取AFTR、DNS、SNTP与搜索域信息，不启动独立Information-request client。

## 指定条件与引用关系

- 同一逻辑interface的DHCPv6PrefixDelegation须先达到Bound，才读取其daemon socket；无对应PD资源就没有来源。Info-request在bootstrap/转为Bound时触发，不是独立租约生命周期。
- 信息controller使用声明Router而非when筛选后的有效视图，也不求值dependsOn。Ready表示读取daemon状态，不证明收到新信息回复。ready_when会被拒绝，须注意以下运行限制。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Information
  metadata:
    name: "<name>"
  spec:
    dependsOn: # 选填; array
      - # object
        equals: "<string>" # 选填; string
        field: "<string>" # 选填; string
        notEmpty: true # 选填; boolean
        optional: true # 选填; boolean
        phase: "<string>" # 选填; string
        resource: "<string>" # 必填; string
    interface: "<string>" # 必填; string
    request: # 选填; array
      - "<string>" # string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.interface` | 必填接口标识，与DHCPv6PrefixDelegation.spec.interface完全匹配，无默认值；两个声明使用相同逻辑接口名。 |
| `spec.request` | 请求选项声明默认空，当前controller不使用；daemon InfoRequest固定以ORO请求AFTR名称、DNS、SNTP与搜索域。 |
| `spec.when` | 条件声明默认未指定，按共用when规则验证，但当前信息controller不使用它筛选自身工作，不能作为信息请求停止开关。 |
| `spec.dependsOn` | 依赖声明默认空；验证目标/field，但此controller不在读取或请求信息前强制这些条件。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/pdclient/client.go](https://github.com/imksoo/routerd/blob/main/pkg/pdclient/client.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `source`, `aftrName`, `dnsServers`, `sntpServers`, `domainSearch`
