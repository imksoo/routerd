# FirewallFlowPinhole

[各资源的配置](../resources.md#firewallflowpinhole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

为防火墙区域配对学习允许的返回 IPv4 UDP 端点，不转换地址。仅 Linux nftables，当前 PF 省略此资源。

## 指定条件与引用关系

- fromZone/toZone 必须为不同已有 FirewallZone 的单独名称，不允许 self。outbound/inbound 与至少一个源选择器必填，所有 IPAddressSet 引用须存在。
- 发出匹配时更新带期限集合后继续后续规则，不自动允许。反向流量须匹配交换后的源/目标限制、允许远端 UDP 源端口及学习组合，随后在普通明确区域规则前允许。
- 默认 remoteAddress 的键为远端 IPv4 + 本机 IPv4 + 本机 UDP 端口。localEndpoint 从键中省略远端地址，但配置的远端 CIDR/集合仍限制入站源。远端 UDP 目标端口不在学习键内。
- 学习条目是临时内核集合，重建防火墙表或重启会丢失。已有 conntrack 允许与控制例外可能先执行，不能取代完整防火墙策略。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallFlowPinhole
  metadata:
    name: "<name>"
  spec:
    [ correlation:
        [ key: ("" | "remoteAddress" | "localEndpoint") ]
    ]
    fromZone: <string>
    inbound:
      sourcePorts:
        - <string>
    outbound:
      [ destinationCIDRs:
          - <string>
      ]
      destinationPorts:
        - <string>
      [ destinationSetRefs:
          - <string>
      ]
      protocol: ("udp")
      [ sourceCIDRs:
          - <string>
      ]
      [ sourceSetRef: <string> ]
    [ timeout: <string> ]
    toZone: <string>
    [ when:
        [ state:
            [ <name>:
                [ contains: <string> ]
                [ equals: <string> ]
                [ exists: <boolean> ]
                [ for: <string> ]
                [ in:
                    - <string>
                ]
                [ maxAge: <string> ]
                [ status: ("set" | "unset" | "unknown") ]
            ]
        ]
    ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.outbound` | **必填** | object | 必填的发出学习匹配。记录匹配的 UDP 流量身份，但不会自行允许流量通过防火墙。 |
| `spec.outbound.sourceSetRef` | **条件必填** | string | 可选已有 IPAddressSet/<name> 源选择器。此字段或 sourceCIDRs 至少一者必填；两者可作 OR。默认空。 |
| `spec.outbound.sourceCIDRs` | **条件必填** | array | 可选 IPv4 源前缀 OR 列表，默认空，至少需要一个源选择器。拒绝 IPv6，生成时掩码主机位。 |
| `spec.outbound.destinationCIDRs` | *选填* | array | 可选学习用 IPv4 远端目标，默认空。与 destinationSetRefs 作 OR；两者均无时任意远端地址。 |
| `spec.outbound.destinationSetRefs` | *选填* | array | 可选已有 IPAddressSet/<name> 远端目标引用，不可重复，默认空。源与目标两类以 AND 组合。 |
| `spec.outbound.protocol` | **必填** | string; enum=["udp"] | 必填 udp，无默认值。此学习机制不支持 TCP/IPv6。 |
| `spec.outbound.destinationPorts` | **必填** | array | 必填非空发出 UDP 目标端口：数字 1–65535 或唯一 start-end 范围。范围不可与其他条目混用。 |
| `spec.inbound` | **必填** | object | 必填返回流量匹配。sourcePorts 选择远端 UDP 发送者端口，不是本机目标端口。 |
| `spec.inbound.sourcePorts` | **必填** | array | 必填非空远端 UDP 源端口列表，与 outbound.destinationPorts 相同的 1–65535/单独范围限制。 |
| `spec.timeout` | *选填* | string | 可选严格为正的 Go 时长，默认 3m。每个匹配发出数据包更新学习条目。生成时四舍五入为秒且至少 1s；这是学习状态过期，不是 UDP 协议超时。 |
| `spec.when` | *选填* | object | 可选通用启用条件，默认无条件。筛除资源不生成学习映射/集合规则。嵌套条件与模式限制请见通用字段。 |
| `spec.fromZone` | **必填** | string | 必填发出源的已有 FirewallZone metadata.name，须与 toZone 不同，拒绝 self。 |
| `spec.toZone` | **必填** | string | 必填发出目标的已有 FirewallZone metadata.name，返回检查时成为输入源，不允许 self。 |
| `spec.correlation` | *选填* | object | 可选组合键选择，默认 remoteAddress，不是资源引用。 |
| `spec.correlation.key` | *选填* | string; enum=["", "remoteAddress", "localEndpoint"] | remoteAddress（默认）或 localEndpoint。前者包含远端 IPv4，后者仅本机 IPv4 + UDP 端口。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`
