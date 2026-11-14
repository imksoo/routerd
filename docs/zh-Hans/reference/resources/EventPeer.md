# EventPeer

[各资源的配置](../resources.md#eventpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

声明 EventGroup 的一个远端发送目标。传输认证与本机标识由组提供。

## 指定条件与引用关系

- groupRef、nodeName 不可为空，endpoint 必填。groupRef 匹配 EventGroup 的单独 metadata.name，不是 EventGroup/name。EventPeer 验证不检查其存在；找不到组就不加入配送配置。
- 静态节点按 nodeName 覆盖导入节点；同名静态条目以后者为准。types 与 subjectPrefixes 两类均须匹配，各列表内为 OR。区分大小写；types 使用完整字符串，不是主题通配符。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventPeer
  metadata:
    name: "<name>"
  spec:
    [ direction: ("push") ]
    endpoint: <string>
    groupRef: <string>
    nodeName: <string>
    [ subjectPrefixes:
        - <string>
    ]
    [ types:
        - <string>
    ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.groupRef` | **必填** | string | 必填的单独 EventGroup 名称，对等传输需要组的 auth.secretFile。无默认值。 |
| `spec.nodeName` | **必填** | string | 必填远端标识，也是去重与配送记录键。YAML 名称不同但 nodeName 相同时，不会成为独立节点。 |
| `spec.endpoint` | **必填** | string | 必填基础 URL，例如 http://192.0.2.10:8787。推送端移除末尾斜线后附加 /v1/events，请勿自行加入此路径。本机 YAML 验证不检查 URL、协议或可达性。 |
| `spec.direction` | *选填* | string; enum=["push"] | 仅接受 push 或空，默认 push。生成的是向外推送目标，不是拉取客户端。 |
| `spec.types` | *选填* | array | 可选的事件类型完全匹配允许列表，默认空允许全部类型。指定条目不可为空。 |
| `spec.subjectPrefixes` | *选填* | array | 可选、区分大小写的 subject 前缀列表。默认空允许全部 subject；匹配任一前缀即可通过此类条件。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventfederation/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventfederation/controller.go) · [pkg/eventd/push.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/push.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

此 Kind 未登记常规状态引用的允许字段。

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/onprem.yaml)
