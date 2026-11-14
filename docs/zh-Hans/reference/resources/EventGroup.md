# EventGroup

[各资源的配置](../resources.md#eventgroup) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

声明共享的联合事件组，以及此路由器的标识、对等节点来源、接收与本机保留设置。

## 指定条件与引用关系

- nodeName 必填，metadata.name 为组标识；参与节点使用相同组名与共享密钥。仅标识用途可省略 secretFile，但传输辅助程序仅在提供 secretFile 时启动。Applied 表示运行配置已写入，不保证事件已送达。
- 配置验证在 listen.address、peersFrom 或引用此组的 EventPeer 存在时要求 secretFile，auth.secretRef 不能替代。eventd 启动时文件须存在，移除前后空白后的密钥不可为空。
- 接收器是否启动由 listen.port > 0 决定，并非仅看 address。使用正端口时请明确指定地址；地址空白而端口为正可能绑定所有地址。端口 0 禁用 TCP 接收，但可继续推送与清理。
- peersFrom 导入 SAMNodeSet 中有 eventEndpoint 的节点，排除 nodeRef 等于 nodeName 的自身节点。按远端 nodeName 去重，后来源覆盖前来源，最后由静态 EventPeer 覆盖导入节点。必要来源缺失时维持 Pending 并保留旧配置；可选来源缺失则跳过。
- eventd 在启动及定期处理（默认 1 分钟）执行本机清理。时间与数量限制同时应用；时间按 observedAt，数量保留最新观测事件。这与每个事件的 expiresAt 及本机 EventRule 记录保留不同。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventGroup
  metadata:
    name: "<name>"
  spec:
    [ auth:
        [ mode: ("" | "hmac") ]
        [ secretFile: <string> ]
        [ secretRef: <string> ]
    ]
    [ listen:
        [ address: <string> ]
        [ port: <integer> ]
    ]
    nodeName: <string>
    [ peersFrom:
        -
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ replayWindow: <string> ]
    [ retention:
        [ maxAge: <string> ]
        [ maxEvents: <integer> ]
    ]
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
| `spec.nodeName` | **必填** | string | 必填且非空的本机节点标识，标记于本机生成的联合事件。无主机名默认值；搭配 peersFrom 时须匹配 SAMNodeSet.nodeRef。 |
| `spec.peersFrom` | *选填* | array | 可选的有序来源列表，默认空。即使所有来源为可选或没有节点，提供此字段仍需要 auth.secretFile。 |
| `spec.peersFrom[].resource` | **条件必填** | string | 必填 SAMNodeSet/<name> 引用。验证格式；来源未声明时按运行时 optional/Pending 处理，不作为普通状态值引用拒绝。 |
| `spec.peersFrom[].optional` | *选填* | boolean | 默认 false：来源缺失会阻止新配置。true 跳过缺失来源，但格式错误或无法解码仍失败。 |
| `spec.retention` | *选填* | object | 可选的本机联合事件保留配置，默认无时间或数量限制。仅在 eventd 清理运行时生效。 |
| `spec.retention.maxEvents` | *选填* | integer; minimum=0 | 每组的非负事件数上限，默认 0 为无限。正值在时间清理后保留 observedAt/id 最新的记录。 |
| `spec.retention.maxAge` | *选填* | string | 可选 Go 时长，默认空为无限。零或负值可通过验证，运行配置会省略，亦表示无时间限制。 |
| `spec.auth` | *选填* | object | 可选的传输完整性配置。当前传输一律使用 HMAC，没有未认证模式。 |
| `spec.auth.mode` | *选填* | string; enum=["", "hmac"] | 接受空或 hmac，默认空。传输运行时两者均使用 HMAC，控制器不以此选择其他方案。 |
| `spec.auth.secretRef` | *选填* | string | 默认空。接受但当前控制器与辅助程序不解析；不能替代 secretFile，也不作资源引用验证。 |
| `spec.auth.secretFile` | **条件必填** | string | 共享 HMAC 密钥文件路径。空值禁用传输辅助程序；指定接收地址或对等节点时按上述规则必填。不会自动分发密钥。 |
| `spec.listen` | *选填* | object | 可选 HTTP 接收绑定，默认地址空、端口 0。接收时请明确设置两者。 |
| `spec.listen.address` | *选填* | string | 接收绑定地址，默认空。非空时需要 1–65535 端口与 secretFile。辅助程序不会仅因地址空而禁用接收，请见端口规则。 |
| `spec.listen.port` | *选填* | integer; minimum=0; maximum=65535 | 整数 0–65535，默认 0 禁用接收。正值在 address 启用明文 HTTP；本资源没有 TLS 证书字段。 |
| `spec.replayWindow` | *选填* | string | 签名请求时间戳偏差容许范围，使用 Go 时长。省略、零或负值为 5m。不是事件保留时间或消息去重 TTL。 |
| `spec.when` | *选填* | object | 可选的通用启用条件，默认无条件。有效配置筛选控制传输配置与服务生命周期；条件与模式限制请见通用字段。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventfederation/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventfederation/controller.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/eventd/config.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/config.go) · [pkg/eventd/prune.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/prune.go) · [pkg/eventd/receiver.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/receiver.go) · [cmd/routerd-eventd/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-eventd/main.go) · [pkg/state/federation.go](https://github.com/imksoo/routerd/blob/main/pkg/state/federation.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `group`, `nodeName`, `peers`, `peersFrom`, `pendingSources`, `listenAddress`, `listenPort`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/onprem.yaml)
