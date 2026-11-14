# VRF

[各资源的配置](../resources.md#vrf) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

声明Linux VRF设备、路由表与成员意图。当前生成计划/artifact标识并保护路由表，但没有创建VRF/连接成员的controller；计划输出不证明VRF已创建。

## 指定条件与引用关系

- members须为Router已声明的逻辑接口，拒绝自身成员。routeTable也用于策略路由清理时保留该表，BGP的VRF配置亦可引用。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VRF
  metadata:
    name: "<name>"
  spec:
    ifname: "<string>" # 选填; string
    members: # 选填; array
      - "<string>" # string
    routeTable: 1 # 必填; integer; minimum=1; maximum=4294967295
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.ifname` | 内核VRF名称，默认metadata.name；最多15字符，不可含空白或/。 |
| `spec.routeTable` | 必填正路由表ID，无默认值。Schema为1–4294967295，当前Go验证检查下限。 |
| `spec.members` | 逻辑接口名列表，默认空，不加Kind/；每项须存在且不可为本VRF的metadata.name，实际连接未实现。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/apply/artifacts.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/artifacts.go) · [pkg/controller/chain/policyroute.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/policyroute.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `ifname`, `routeTable`, `members`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/multi-instance-bgp.yaml)
