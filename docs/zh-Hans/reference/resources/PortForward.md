# PortForward

[各资源的配置](../resources.md#portforward) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

将 TCP/UDP 端口转发至单一 IPv4 端点，可选内部回送 NAT。Linux 生成 DNAT 与转发允许，FreeBSD 使用 rdr pass。

## 指定条件与引用关系

- listen/target 必填。target 需要且只能提供 address/addressFrom 其中之一与 port；固定目标必须是纯 IPv4，不接受主机名或 CIDR。listen 两者均省略时匹配接口所有目标。
- 引用须存在并提供对应字段，但 NAT 生成器仅读声明的 IPv4StaticAddress.address/VirtualAddress.address。optional=true 可在支持来源未解析时省略规则，不会启用任意动态来源。PortForward 没有后端健康检查。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: PortForward
  metadata:
    name: "<name>"
  spec:
    hairpin: # 选填; object
      enabled: true # 选填; boolean
      interfaces: # 选填; array
        - "<string>" # string
      mode: "" # 选填; string; enum=["", "auto", "manual", "off"]
    listen: # 必填; object
      address: "<string>" # 选填; string
      addressFrom: # 选填; object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
      interface: "<string>" # 必填; string
      port: 1 # 必填; integer; minimum=1; maximum=65535
      protocol: "tcp" # 必填; string; enum=["tcp", "udp"]
    target: # 必填; object
      address: "<string>" # 选填; string
      addressFrom: # 选填; object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
      port: 1 # 必填; integer; minimum=1; maximum=65535
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
| `spec.listen` | 必填 IPv4 入口端点，创建 NAT/防火墙规则，不创建应用程序监听或分配地址。 |
| `spec.listen.interface` | 必填单独已声明逻辑接口名称，不是 Kind/name。须存在配置索引并可解析 OS ifname。 |
| `spec.listen.address` | 可选纯 IPv4 目标地址，与 addressFrom 互斥。省略时匹配监听接口上任意目标地址。 |
| `spec.listen.addressFrom` | 可选通用 resource/field/optional 引用，与 address 互斥。NAT/静态生成支持声明的 IPv4StaticAddress.address 或 VirtualAddress.address，不读任意运行时状态。普通引用验证通过不保证生成器支持。 |
| `spec.listen.protocol` | 必填 tcp/udp，无默认值。选择转发流量，不决定健康检查协议。 |
| `spec.listen.port` | 必填目标端口 1–65535，无范围、列表或默认值。 |
| `spec.hairpin` | 可选经公开地址返回内部的 NAT 配置，启用时需可解析 listen 地址。PortForward 默认 off，IngressService 默认 auto。 |
| `spec.hairpin.enabled` | 默认 false。mode 省略时 true 让 PortForward 使用 manual，IngressService 仍默认 auto。明确 off 即使 enabled=true 也禁用。 |
| `spec.hairpin.mode` | auto/manual/off。auto 在 listen 与后端同属声明 IPv4 子网或相同私有 /24 时选 listen 接口，不发现任意 LAN 路由。manual 需 enabled=true 与 interfaces 才生成规则。 |
| `spec.hairpin.interfaces` | 可选单独声明逻辑接口名，不可重复，启用 manual 时需非空。auto 可另加入 listen 接口。IPv4 DNAT 加 masquerade/PF SNAT 让回程经路由器。 |
| `spec.when` | 可选通用启用条件，默认无条件。不成立时从有效生成排除；条件与模式限制请见通用字段。 |
| `spec.target` | 必填单一内部端点，不分配 IP 或启动服务。 |
| `spec.target.address` | 纯 IPv4 目标，与 target.addressFrom 必须择一，无默认。 |
| `spec.target.addressFrom` | 通用 resource/field/optional 引用，与 listen.addressFrom 相同声明字段限制，须解析为 IPv4。 |
| `spec.target.port` | 必填内部 TCP/UDP 端口 1–65535，独立于 listen.port。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/ingress.go](https://github.com/imksoo/routerd/blob/main/pkg/render/ingress.go) · [pkg/render/status_source.go](https://github.com/imksoo/routerd/blob/main/pkg/render/status_source.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `listenAddress`, `target`, `dryRun`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-port-forward-web.yaml)
