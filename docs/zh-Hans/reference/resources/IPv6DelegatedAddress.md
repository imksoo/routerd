# IPv6DelegatedAddress

[各资源的配置](../resources.md#ipv6delegatedaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

从 DHCPv6PrefixDelegation 前缀派生并安装本机 IPv6 地址。

## 指定条件与引用关系

- prefixDelegation 须为现有 DHCPv6PrefixDelegation 裸名，interface 也须存在于 Router 索引。常规主用安装需要可用 Bound 前缀、就绪链路及成立的依赖条件。HA VMAC 预置可在主用获取前使用新鲜同步快照。
- 派生前缀长为 max(64,委派长度)。subnetID 以十六进制移至该边界后与基底 OR，suffix 仅取剩余主机位再 OR。当前不检查 subnetID 是否落在委派块内，请选有效范围。
- Linux 使用 ip address、FreeBSD 使用 ifconfig。主用应用后读回地址才公布 Applied，暂定或 DAD 中可保持 Pending。前缀变更可将旧地址保留为弃用直到有效期，/128 作主机地址而非直连子网。
- sendRA/announce 不会让当前地址控制器启动 RA 服务，须另设 IPv6RouterAdvertisement 或 DHCPv6Server。已移除的 prefixSource/ready_when 会被拒绝。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata:
    name: "<name>"
  spec:
    addressSuffix: "<string>" # 必填; string
    announce: true # 选填; boolean
    dependsOn: # 选填; array
      - # object
        equals: "<string>" # 选填; string
        field: "<string>" # 选填; string
        notEmpty: true # 选填; boolean
        optional: true # 选填; boolean
        phase: "<string>" # 选填; string
        resource: "<string>" # 必填; string
    interface: "<string>" # 必填; string
    prefixDelegation: "<string>" # 必填; string
    prefixLength: 1 # 选填; integer; minimum=1; maximum=128
    sendRA: true # 选填; boolean
    subnetID: "<string>" # 选填; string
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
| `spec.prefixDelegation` | 必填 DHCPv6PrefixDelegation metadata.name 裸名，不是 Kind/name 或状态表达式，无默认值。 |
| `spec.interface` | 必填已声明的目标逻辑接口，最终 OS 名称由 Router 解析。 |
| `spec.subnetID` | 可选十六进制字符串，默认 0；"10" 为 0x10 而非十进制十，可加 0x。未严格检查容纳范围，请选委派块内编号。 |
| `spec.addressSuffix` | 必填 IPv6 后缀如 ::1，仅使用派生前缀边界以下的主机位，不作完整绝对地址，无自动 ::1 默认值。 |
| `spec.prefixLength` | 省略或 0 保留派生长度，通常 /64。运行时接受相同长度或 128；其他明确长度虽可通过 1–128 模式，应用仍失败。/128 避免建立通常的直连前缀路由。 |
| `spec.sendRA` | 布尔值默认 false，部分主机依赖或转发准备路径使用，但不是实时地址控制器的自动 RA 开关。 |
| `spec.announce` | 布尔值默认 false，实时 RA 服务限制同 sendRA，不保证主动邻居或 RA 通告。 |
| `spec.when` | 可选通用条件，默认无条件；条件为假可经明确清理路径移除派生地址。 |
| `spec.dependsOn` | 可选通用 AND 依赖列表，默认空。不成立时为 Pending/DependsOnFalse。HA 预置对 PD/VMAC 前提有特定处理，请用实际资源 phase。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/pdclient/derive.go](https://github.com/imksoo/routerd/blob/main/pkg/pdclient/derive.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
