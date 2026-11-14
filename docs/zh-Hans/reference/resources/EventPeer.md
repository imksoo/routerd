# EventPeer

[各资源的配置](../resources.md#eventpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

声明 EventGroup 的一个远端发送目标。传输认证与本机标识由组提供。

## 指定条件与引用关系

- groupRef、nodeName 不可为空，endpoint 必填。groupRef 匹配 EventGroup 的单独 metadata.name，不是 EventGroup/name。EventPeer 验证不检查其存在；找不到组就不加入配送配置。
- 静态节点按 nodeName 覆盖导入节点；同名静态条目以后者为准。types 与 subjectPrefixes 两类均须匹配，各列表内为 OR。区分大小写；types 使用完整字符串，不是主题通配符。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventPeer
  metadata:
    name: "<name>"
  spec:
    direction: "push" # 选填; string; enum=["push"]
    endpoint: "<string>" # 选填; string
    groupRef: "<string>" # 必填; string
    nodeName: "<string>" # 必填; string
    subjectPrefixes: # 选填; array
      - "<string>" # string
    types: # 选填; array
      - "<string>" # string
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.groupRef` | 必填的单独 EventGroup 名称，对等传输需要组的 auth.secretFile。无默认值。 |
| `spec.nodeName` | 必填远端标识，也是去重与配送记录键。YAML 名称不同但 nodeName 相同时，不会成为独立节点。 |
| `spec.endpoint` | 必填基础 URL，例如 http://192.0.2.10:8787。推送端移除末尾斜线后附加 /v1/events，请勿自行加入此路径。本机 YAML 验证不检查 URL、协议或可达性。 |
| `spec.direction` | 仅接受 push 或空，默认 push。生成的是向外推送目标，不是拉取客户端。 |
| `spec.types` | 可选的事件类型完全匹配允许列表，默认空允许全部类型。指定条目不可为空。 |
| `spec.subjectPrefixes` | 可选、区分大小写的 subject 前缀列表。默认空允许全部 subject；匹配任一前缀即可通过此类条件。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventfederation/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventfederation/controller.go) · [pkg/eventd/push.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/push.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
