# SAMNodeSet

[各资源的配置](../resources.md#samnodeset) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

声明供传输、事件对等端、WireGuard 与 MobilityPool 成员解析共享的 SAM 节点身份与拓扑。本机提供商捕获/发现配置应放在 MobilityPool 自节点覆盖，不放在此处。

## 指定条件与引用关系

- nodes 不可为空，移除首尾空白后 nodeRef 不可重复。不从 nodeRef 推定 role/site；MobilityPool 等使用端另施加角色及站点要求。
- 配置任一 WireGuard 数据时，publicKey 及非空 allowedIPs 必填。结构验证中 endpoint/transportEndpoint 仍可选，但须符合所选传输。samEndpoint 与 samEndpointFrom 互斥。
- placement 组要求 role=cloud，且组内 site 一致。priority、drain=true 及正数 maxSecondaryIPs 均需非空 placement.group。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMNodeSet
  metadata:
    name: "<name>"
  spec:
    nodes:
      -
        [ eventEndpoint: <string> ]
        [ macAddresses:
            - <string>
        ]
        [ maintenance:
            [ drain: <boolean> ]
        ]
        [ maxSecondaryIPs: <integer> ]
        nodeRef: <string>
        [ placement:
            [ group: <string> ]
            [ priority: <integer> ]
        ]
        [ role: ("" | "onprem" | "cloud") ]
        [ routeReflector: <boolean> ]
        [ samEndpoint: <string> ]
        [ samEndpointFrom:
            field: <string>
            [ optional: <boolean> ]
            resource: <string>
        ]
        [ site: <string> ]
        [ wireGuard:
            [ allowedIPs:
                - <string>
            ]
            [ endpoint: <string> ]
            [ persistentKeepalive: <integer> ]
            [ publicKey: <string> ]
            [ transportEndpoint: <string> ]
        ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.nodes` | **必填** | array | 必填且不可为空的共享节点列表，不自动加入本机节点。 |
| `spec.nodes[].nodeRef` | **必填** | string | 必填稳定节点标识，在此 NodeSet 内唯一。使用端与 EventGroup 本机标识或 SAMTransportProfile.selfNodeRef 匹配，不是 Kind/name 引用。 |
| `spec.nodes[].site` | *选填* | string | 可选站点标签，默认为空，不由 role 派生。MobilityPool 成员及 placement 规则可能要求明确且兼容的 site。 |
| `spec.nodes[].role` | *选填* | string; enum=["", "onprem", "cloud"] | NodeSet 验证接受空值/onprem/cloud，无推定默认值。MobilityPool 成员需实际 onprem/cloud 角色，placement 仅供 cloud。 |
| `spec.nodes[].routeReflector` | *选填* | boolean | 默认 false，标记节点可供注册拓扑选为 RR 身份，不会单独创建 BGPRouter 或 BGP 连接。 |
| `spec.nodes[].eventEndpoint` | *选填* | string | 供生成事件对等端使用的可选绝对 HTTP(S) URL，须含主机，无默认值或状态来源。 |
| `spec.nodes[].samEndpoint` | *选填* | string | 可选的节点外层传输端点 IPv4 地址或 CIDR，不接受 DNS 名称，与 samEndpointFrom 互斥。WireGuard 加密时优先使用 wireGuard.transportEndpoint。 |
| `spec.nodes[].samEndpointFrom` | *选填* | object | 遵循通用 resource/field 规则的可选运行时状态来源。缺少值仍属未解析传输来源，optional 不会补入端点或忽略格式错误地址。 |
| `spec.nodes[].samEndpointFrom.resource` | **条件必填** | string | 使用来源时必填的已声明资源 Kind/name，与固定 samEndpoint 互斥。 |
| `spec.nodes[].samEndpointFrom.field` | **条件必填** | string | 指定 resource 时必填的明确提供状态字段，例如 DHCPv4Client 的 currentAddress。 |
| `spec.nodes[].samEndpointFrom.optional` | *选填* | boolean | 默认 false，不跳过存在/字段验证；此传输使用端仍将端点缺失报告为未解析。 |
| `spec.nodes[].macAddresses` | *选填* | array | 可选 MAC 地址列表，默认为空。ARP 观察意图将其列为忽略的发送端 MAC，避免将 SAM 路由器当客户端重新发现；不是更改 MAC 的请求。 |
| `spec.nodes[].wireGuard` | *选填* | object | 可选公开 WireGuard 传输身份，任一子字段非空时需 publicKey 与 allowedIPs。私钥不应放入 NodeSet 拓扑。 |
| `spec.nodes[].wireGuard.publicKey` | *选填* | string | 配置 wireGuard 时必填的对等端公钥，传入生成的 WireGuardPeer，无派生默认值。 |
| `spec.nodes[].wireGuard.endpoint` | *选填* | string | 可选 WireGuard 外层 UDP host:port，IPv6 使用 [IPv6]:port，端口 1–65535，与内层 transportEndpoint 不同。 |
| `spec.nodes[].wireGuard.transportEndpoint` | *选填* | string | encryption=wireguard 时可选的内层 SAM 隧道 IPv4 地址/CIDR，优先于 samEndpoint/samEndpointFrom，不是 UDP 套接字地址。 |
| `spec.nodes[].wireGuard.allowedIPs` | *选填* | array | 配置 WireGuard 时必填且非空的 IPv4/IPv6 CIDR，传入生成的对等端，不会为每个前缀自动创建普通路由。 |
| `spec.nodes[].wireGuard.persistentKeepalive` | *选填* | integer; minimum=0; maximum=65535 | 整数秒 0–65535，默认 0 禁用持续保活，复制至生成的对等端。 |
| `spec.nodes[].placement` | *选填* | object | 可选共享捕获放置信息，仅 cloud 角色可声明组；成员须有一致 site/role，使用端也须符合提供商身份兼容性。 |
| `spec.nodes[].placement.group` | *选填* | string | 可选放置组名称，默认空值表示不分组选择。priority、drain 或正容量配置时必填。 |
| `spec.nodes[].placement.priority` | *选填* | integer; minimum=0; maximum=1000000 | 整数 0–1000000。省略或 0 按解析成员顺序分配 10、20…并避开明确值；单活选择优先较小的有效数值，非零需 group。 |
| `spec.nodes[].maintenance` | *选填* | object | 可选声明式维护意图，默认不 drain，是共享拓扑信息，不是本机提供商凭据。 |
| `spec.nodes[].maintenance.drain` | *选填* | boolean | 默认 false。true 将组成员排除于活动捕获选择之外，需 placement.group。 |
| `spec.nodes[].maxSecondaryIPs` | *选填* | integer; minimum=0 | 非负整数，默认 0。正值需 placement.group，启用该组按容量分散捕获；0 表示无单个成员容量上限，不更改云端配额。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/mobilityconfig/members.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/members.go) · [pkg/mobilityconfig/normalize.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/normalize.go) · [pkg/controller/mobility/transport.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/transport.go) · [pkg/controller/mobility/arp_observer_intent.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/arp_observer_intent.go) · [pkg/controller/mobility/placement.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/placement.go) · [pkg/controller/mobility/capture_distribution.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/capture_distribution.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `nodeCount`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
