# IPv4Route

[各资源的配置](../resources.md#ipv4route) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

安装 IPv4 单播或黑洞路由，可从资源状态取得设备与网关。

## 指定条件与引用关系

- blackhole 不可搭配 device、deviceFrom、gateway、gatewayFrom 或 preferredSource。unicast 运行时必须能解析设备；直连路由可省略网关。
- 动态值优先于 device/gateway 固定值；无法解析时使用固定值。没有可用值时，来源的 optional 不会跳过 DeviceMissing 或 GatewayMissing。引用仍须指向已声明的资源，并明确指定该资源提供的字段。
- Linux 使用 ip route replace；preferredSource 对应 src，仅正数 metric 会传入。FreeBSD 使用 route change/add 与 -ifa，此路径不应用 metric。Installed 不代表端到端连接成功。
- ready_when 与 device/gateway 中的 ${...} 表达式已移除，验证会拒绝。请使用 dependsOn 与 deviceFrom/gatewayFrom。preferredSource 也不支持状态表达式。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4Route
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
    destination: "<string>" # 必填; string
    device: "<string>" # 选填; string
    deviceFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    gateway: "<string>" # 选填; string
    gatewayFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    metric: 0 # 选填; integer; minimum=0
    preferredSource: "<string>" # 选填; string
    type: "unicast" # 选填; string; enum=["unicast", "blackhole"]
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.destination` | 必填的目标 IPv4 CIDR。默认路由请使用 0.0.0.0/0，而非 default。目前验证仅解析前缀，未在此拒绝 IPv6，但此资源与控制器供 IPv4 使用。 |
| `spec.type` | 默认 unicast。blackhole 丢弃流量，不可指定设备、网关或源地址。 |
| `spec.device` | 可选的逻辑接口或 OS 设备名称，deviceFrom 无值时使用。unicast 为空时为 Pending/DeviceMissing；OS 设备不存在时为 DeviceNotReady。blackhole 禁用。 |
| `spec.deviceFrom` | 可选的设备状态值来源；resource 与 field 遵循通用引用规则。规范化后第一个值优先于 device。默认无来源，blackhole 禁用。 |
| `spec.gateway` | 可选的纯 IPv4 下一跳，默认为空，表示通过设备且无网关的路由。也是 gatewayFrom 无法解析时的替代值。blackhole 禁用。 |
| `spec.gatewayFrom` | 可选的状态来源，遵循通用 resource/field 规则。第一个规范化值优先于 gateway。设有来源但来源与固定值均无值时，维持 Pending/GatewayMissing。blackhole 禁用。 |
| `spec.preferredSource` | 本机发起流量的可选首选 IPv4 源地址，默认不指定。应用时必须已是本机地址，否则为 Pending/PreferredSourceNotLocal。不会分配地址或执行 SNAT。blackhole 禁用。 |
| `spec.metric` | 可选的非负整数，默认 0。Linux 仅传入正值；FreeBSD 应用时忽略。其他条件相同时通常优先使用较低 metric。 |
| `spec.dependsOn` | 可选的通用资源依赖条件 AND 列表，默认为空。不满足时以 DependsOnFalse 跳过安装。resource/field/phase/equals/notEmpty/optional 请见通用字段。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `type`, `destination`, `device`, `gateway`, `preferredSource`, `metric`, `dryRun`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/lan-advertise-with-community.yaml)
