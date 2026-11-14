# Interface

[各资源的配置](../resources.md#interface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

为现有OS接口提供metadata.name逻辑名称，供其他资源引用，不创建物理接口。

## 指定条件与引用关系

- 依赖资源的interface使用metadata.name逻辑名称，ifname填写实际设备名。managed=false不会抑制已要求的LinkController启用/MTU变更。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata:
    name: "<name>"
  spec:
    [ adminUp: <boolean> ]
    ifname: <string>
    managed: <boolean>
    [ mtu: <integer> ]
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
| `spec.ifname` | **必填** | string | 必填现有内核设备名，例如ens18，无默认值；不存在时不创建，而记为Down/InterfaceNotFound。 |
| `spec.managed` | **必填** | boolean | 管理声明，解码默认false，但JSON Schema要求显式填写。影响管理/观察信息及周边处理，不保证adminUp/MTU仅观察不变更。 |
| `spec.adminUp` | *选填* | boolean | 默认false，不要求启用；true会启用已存在但关闭的link。false不表示强制关闭。 |
| `spec.mtu` | *选填* | integer; minimum=576; maximum=9216 | 默认0保留当前MTU；非零值须为576–9216，不同时应用。 |
| `spec.when` | *选填* | object | 启用条件，省略不设限制；state/all/any、unknown及时间条件请见共用条件参考。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `ifname`, `addresses`, `ipv4Addresses`, `ipv6Addresses`, `primaryIPv4`, `primaryIPv6`, `macAddress`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)
