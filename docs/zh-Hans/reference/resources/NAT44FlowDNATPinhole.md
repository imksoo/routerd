# NAT44FlowDNATPinhole

[各资源的配置](../resources.md#nat44flowdnatpinhole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

从发出 IPv4 UDP 学习内部端点，按收到 UDP 的目标端口动态 DNAT。仅 Linux nftables，当前 PF 未实现此资源。

## 指定条件与引用关系

- fromInterfaceRefs、outbound、inbound 与源选择器必填。接口与 IPAddressSet 引用须存在于同一 Router。仅 IPv4/UDP，属于端口触发 DNAT，不是普通 conntrack 回复处理。
- 映射键为发出本机 UDP 源端口，值为 SNAT 前内部 IPv4 与相同端口。所有所选接口共享每资源一个映射。不同客户端使用相同源端口会覆盖，远端地址不在键内。
- 入站 DNAT 检查所选输入接口、允许的远端源端口与已学习目标端口。不会将发出目标 CIDR/集合重新应用到入站远端地址，也不创建防火墙转发允许；转换流量仍需防火墙允许。
- 学习映射是内核运行状态，不是配置或持久化数据库。重建 NAT 表或重启会丢失，需发出流量重新学习。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44FlowDNATPinhole
  metadata:
    name: "<name>"
  spec:
    correlation: # 选填; object
      key: "" # 选填; string; enum=["", "localPort"]
    fromInterfaceRefs: # 选填; array
      - "<string>" # string
    inbound: # 必填; object
      sourcePorts: # 必填; array
        - "<string>" # string
    outbound: # 必填; object
      destinationCIDRs: # 选填; array
        - "<string>" # string
      destinationPorts: # 必填; array
        - "<string>" # string
      destinationSetRefs: # 选填; array
        - "<string>" # string
      protocol: "udp" # 必填; string; enum=["udp"]
      sourceCIDRs: # 选填; array
        - "<string>" # string
      sourceSetRef: "<string>" # 选填; string
    timeout: "<string>" # 选填; string
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
| `spec.outbound` | 必填的发出学习匹配。记录匹配的 UDP 流量身份，但不会自行允许流量通过防火墙。 |
| `spec.outbound.sourceSetRef` | 可选已有 IPAddressSet/<name> 源选择器。此字段或 sourceCIDRs 至少一者必填；两者可作 OR。默认空。 |
| `spec.outbound.sourceCIDRs` | 可选 IPv4 源前缀 OR 列表，默认空，至少需要一个源选择器。拒绝 IPv6，生成时掩码主机位。 |
| `spec.outbound.destinationCIDRs` | 可选学习用 IPv4 远端目标，默认空。与 destinationSetRefs 作 OR；两者均无时任意远端地址。 |
| `spec.outbound.destinationSetRefs` | 可选已有 IPAddressSet/<name> 远端目标引用，不可重复，默认空。源与目标两类以 AND 组合。 |
| `spec.outbound.protocol` | 必填 udp，无默认值。此学习机制不支持 TCP/IPv6。 |
| `spec.outbound.destinationPorts` | 必填非空发出 UDP 目标端口：数字 1–65535 或唯一 start-end 范围。范围不可与其他条目混用。 |
| `spec.inbound` | 必填返回流量匹配。sourcePorts 选择远端 UDP 发送者端口，不是本机目标端口。 |
| `spec.inbound.sourcePorts` | 必填非空远端 UDP 源端口列表，与 outbound.destinationPorts 相同的 1–65535/单独范围限制。 |
| `spec.timeout` | 可选严格为正的 Go 时长，默认 3m。每个匹配发出数据包更新学习条目。生成时四舍五入为秒且至少 1s；这是学习状态过期，不是 UDP 协议超时。 |
| `spec.when` | 可选通用启用条件，默认无条件。筛除资源不生成学习映射/集合规则。嵌套条件与模式限制请见通用字段。 |
| `spec.fromInterfaceRefs` | 必填非空输出/输入接口列表，接受单独名称或 Interface/name、PPPoESession/name、DSLiteTunnel/name。发出匹配 oifname，入站匹配同列表的 iifname。 |
| `spec.correlation` | 可选关联对象，仅支持 localPort，省略亦使用相同键。 |
| `spec.correlation.key` | 仅空或 localPort。生成器无其他分支，一律以 UDP 源端口索引学习映射。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/local_service_redirect.go](https://github.com/imksoo/routerd/blob/main/pkg/render/local_service_redirect.go) · [pkg/controller/nat44/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/nat44/controller.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`
