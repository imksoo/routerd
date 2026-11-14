# NAT44Rule

[各资源的配置](../resources.md#nat44rule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

通过输出接口或 EgressRoutePolicy 所选设备转换路由 IPv4 源地址。NAT 本身不允许转发，也不分配转换地址。

## 指定条件与引用关系

- 运行控制器请使用 type + egressInterface/egressPolicyRef + sourceRanges。type 必须为 masquerade/snat；至少一个输出选择与一个 IPv4 源范围必填。两种输出选择均有时，egressInterface 优先。
- snat 必须且只能提供 snatAddress 或 snatAddressFrom 其中之一；masquerade 禁止两者。动态来源遵循通用 resource/field 验证，无可用地址时维持 Pending；optional 不会跳过 SNAT 地址需求。
- 替代 outboundInterface/sourceCIDRs/translation 形式可通过验证并出现在计划，但运行 NAT44 控制器不会转成渲染规则；Linux 可能维持 Pending 或产生错误而不应用。静态 PF 仅支持此形式的 interfaceAddress/address，不支持 pool；portMapping 无生成效果。请勿视为可用运行配置。
- 两种形式互斥。替代形式需 outboundInterface、非空 IPv4 sourceCIDRs 与 translation.type，禁止传统 type/egress/sourceRanges/目标/SNAT 字段。未使用的 translation.address/addresses 不互斥拒绝，但仅所选分支会使用。
- Linux 将 destinationCIDRs 与 destinationSetRefs 作 OR，再排除指定 CIDR/集合。PF 应用静态目标与排除 CIDR，但忽略两个地址集合引用列表。规则按 metadata.name 排序，不按 YAML 顺序。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: "<name>"
  spec:
    [ destinationCIDRs:
        - <string>
    ]
    [ destinationSetRefs:
        - <string>
    ]
    [ egressInterface: <string> ]
    [ egressPolicyRef: <string> ]
    [ excludeDestinationCIDRs:
        - <string>
    ]
    [ excludeDestinationSetRefs:
        - <string>
    ]
    [ outboundInterface: <string> ]
    [ snatAddress: <string> ]
    [ snatAddressFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ sourceCIDRs:
        - <string>
    ]
    [ sourceRanges:
        - <string>
    ]
    [ translation:
        [ address: <string> ]
        [ addresses:
            - <string>
        ]
        [ portMapping:
            [ end: <integer> ]
            [ start: <integer> ]
            [ type: ("auto" | "preserve" | "range") ]
        ]
        type: ("interfaceAddress" | "address" | "pool")
    ]
    [ type: ("masquerade" | "snat") ]
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
| `spec.type` | **条件必填** | string; enum=["masquerade", "snat"] | 运行形式必填：masquerade 使用输出地址，snat 使用提供地址。无默认；不可搭配替代形式。 |
| `spec.egressInterface` | **条件必填** | string | 可选单独配置逻辑接口/隧道名称，指定时须在接口/DSLite 索引中存在，优先于 egressPolicyRef。 |
| `spec.egressPolicyRef` | **条件必填** | string | egressInterface 缺省时使用的单独 EgressRoutePolicy 名称。需要 status.phase=Applied 与非空 selectedDevice。NAT44 验证不检查存在性，错名或未解析会 Pending。 |
| `spec.sourceRanges` | **条件必填** | array | 运行形式必填非空 IPv4 CIDR OR 列表。不自动发现 LAN，与 sourceCIDRs/translation 形式互斥。 |
| `spec.destinationCIDRs` | *选填* | array | 可选 IPv4 目标 OR 列表，默认空。Linux 与 destinationSetRefs 取并集；PF 使用静态 CIDR。 |
| `spec.destinationSetRefs` | *选填* | array | 可选已有 IPAddressSet/<name> 引用，不可重复。默认空，Linux 有效，PF 忽略。 |
| `spec.excludeDestinationCIDRs` | *选填* | array | 可选从此 NAT 选择排除的 IPv4 目标，默认空。PF 在此规则转换前生成 no-nat。 |
| `spec.excludeDestinationSetRefs` | *选填* | array | 可选已有 IPAddressSet/<name> 排除，默认空。仅 Linux 有效，PF 忽略。 |
| `spec.snatAddress` | **条件必填** | string | type=snat 的纯 IPv4 转换地址，与 snatAddressFrom 必须且只能择一。masquerade 禁用。不会分配地址。 |
| `spec.snatAddressFrom` | **条件必填** | object | 仅供 snat 的通用 resource/field/optional 来源。先读状态，再读支持的声明资源字段；无可用值时 Pending。请指定提供 IPv4 地址的字段。 |
| `spec.outboundInterface` | **条件必填** | string | 替代验证形式必填的单独接口名称；如上所述运行控制器不支持。不可搭配 egressInterface/egressPolicyRef。 |
| `spec.sourceCIDRs` | **条件必填** | array | 替代形式必填非空 IPv4 前缀，无默认。运行控制器不使用。 |
| `spec.translation` | **条件必填** | object | 替代形式的转换对象，搭配 outboundInterface/sourceCIDRs。通过验证与出现计划不代表运行安装。 |
| `spec.translation.type` | **条件必填** | string; enum=["interfaceAddress", "address", "pool"] | 替代形式必填 interfaceAddress/address/pool。静态 PF 仅支持前两者，运行控制器不应用此形式。 |
| `spec.translation.address` | **条件必填** | string | translation.type=address 时必填纯 IPv4，默认空。其他类型不使用。 |
| `spec.translation.addresses` | **条件必填** | array | type=pool 时必填非空纯 IPv4 列表。接受并规划，但当前运行控制器与静态 PF 未实现。 |
| `spec.translation.portMapping` | *选填* | object | 可选替代形式端口策略，仅验证与规划。当前 Linux 运行与 PF 不强制这些配置。 |
| `spec.translation.portMapping.type` | *选填* | string; enum=["auto", "preserve", "range"] | auto（默认）、preserve 或 range。auto/preserve 禁止 start/end，range 需要两端。当前无生成效果。 |
| `spec.translation.portMapping.start` | *选填* | integer; minimum=1; maximum=65535 | 仅 range 必填起点，1–65535 且 <= end。默认 0 为未指定，当前仅验证。 |
| `spec.translation.portMapping.end` | *选填* | integer; minimum=1; maximum=65535 | 仅 range 必填终点，1–65535 且 >= start。默认 0 为未指定，当前仅验证。 |
| `spec.when` | *选填* | object | 可选通用启用条件，默认无条件。未启用或已筛除规则不纳入有效 NAT 生成。条件与模式限制请见通用字段。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/nat44/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/nat44/controller.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `egressInterface`, `snatAddress`, `dryRun`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/dslite-lan-range-snat.yaml)
