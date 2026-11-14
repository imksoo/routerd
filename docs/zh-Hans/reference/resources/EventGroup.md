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

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventGroup
  metadata:
    name: "<name>"
  spec:
    auth: # 选填; object
      mode: "" # 选填; string; enum=["", "hmac"]
      secretFile: "<string>" # 选填; string
      secretRef: "<string>" # 选填; string
    listen: # 选填; object
      address: "<string>" # 选填; string
      port: 0 # 选填; integer; minimum=0; maximum=65535
    nodeName: "<string>" # 必填; string
    peersFrom: # 选填; array
      - # object
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    replayWindow: "<string>" # 选填; string
    retention: # 选填; object
      maxAge: "<string>" # 选填; string
      maxEvents: 0 # 选填; integer; minimum=0
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
| `spec.nodeName` | 必填且非空的本机节点标识，标记于本机生成的联合事件。无主机名默认值；搭配 peersFrom 时须匹配 SAMNodeSet.nodeRef。 |
| `spec.peersFrom` | 可选的有序来源列表，默认空。即使所有来源为可选或没有节点，提供此字段仍需要 auth.secretFile。 |
| `spec.peersFrom[].resource` | 必填 SAMNodeSet/<name> 引用。验证格式；来源未声明时按运行时 optional/Pending 处理，不作为普通状态值引用拒绝。 |
| `spec.peersFrom[].optional` | 默认 false：来源缺失会阻止新配置。true 跳过缺失来源，但格式错误或无法解码仍失败。 |
| `spec.retention` | 可选的本机联合事件保留配置，默认无时间或数量限制。仅在 eventd 清理运行时生效。 |
| `spec.retention.maxEvents` | 每组的非负事件数上限，默认 0 为无限。正值在时间清理后保留 observedAt/id 最新的记录。 |
| `spec.retention.maxAge` | 可选 Go 时长，默认空为无限。零或负值可通过验证，运行配置会省略，亦表示无时间限制。 |
| `spec.auth` | 可选的传输完整性配置。当前传输一律使用 HMAC，没有未认证模式。 |
| `spec.auth.mode` | 接受空或 hmac，默认空。传输运行时两者均使用 HMAC，控制器不以此选择其他方案。 |
| `spec.auth.secretRef` | 默认空。接受但当前控制器与辅助程序不解析；不能替代 secretFile，也不作资源引用验证。 |
| `spec.auth.secretFile` | 共享 HMAC 密钥文件路径。空值禁用传输辅助程序；指定接收地址或对等节点时按上述规则必填。不会自动分发密钥。 |
| `spec.listen` | 可选 HTTP 接收绑定，默认地址空、端口 0。接收时请明确设置两者。 |
| `spec.listen.address` | 接收绑定地址，默认空。非空时需要 1–65535 端口与 secretFile。辅助程序不会仅因地址空而禁用接收，请见端口规则。 |
| `spec.listen.port` | 整数 0–65535，默认 0 禁用接收。正值在 address 启用明文 HTTP；本资源没有 TLS 证书字段。 |
| `spec.replayWindow` | 签名请求时间戳偏差容许范围，使用 Go 时长。省略、零或负值为 5m。不是事件保留时间或消息去重 TTL。 |
| `spec.when` | 可选的通用启用条件，默认无条件。有效配置筛选控制传输配置与服务生命周期；条件与模式限制请见通用字段。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventfederation/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventfederation/controller.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/eventd/config.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/config.go) · [pkg/eventd/prune.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/prune.go) · [pkg/eventd/receiver.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/receiver.go) · [cmd/routerd-eventd/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-eventd/main.go) · [pkg/state/federation.go](https://github.com/imksoo/routerd/blob/main/pkg/state/federation.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
