# IPv4StaticAddress

[各资源的配置](../resources.md#ipv4staticaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

在逻辑接口声明静态IPv4地址，由controller协调配置，不是DHCP租约。

## 指定条件与引用关系

- interface引用与address/prefix必填。同逻辑接口及掩码后网络前缀的两个静态资源，即使主机地址不同也拒绝；与相同组合的VirtualAddress重叠也拒绝。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4StaticAddress
  metadata:
    name: "<name>"
  spec:
    address: <string>
    [ allowOverlap: <boolean> ]
    [ allowOverlapReason: <string> ]
    [ exclusive: <boolean> ]
    interface: <string>
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
| `spec.interface` | **必填** | string | 必填Router接口索引中的逻辑名称，不加Interface/，必须在声明配置存在，无默认值。 |
| `spec.address` | **必填** | string | 必填含前缀长度的IPv4主机地址，例如192.0.2.1/24，无默认值或隐含/32。解析器检查前缀语法，运行时使用IPv4操作。 |
| `spec.exclusive` | *选填* | boolean | 默认false。当前观察器警告exclusive清理未实现，运行时地址controller也不读取此标记。额外地址的整体协调独立，false不保证保留手动地址。 |
| `spec.allowOverlap` | *选填* | boolean | 默认false，保留供解码但当前验证与地址协调不使用，不会跳过重复/冲突检查。 |
| `spec.allowOverlapReason` | *选填* | string | 默认空，仅保留声明，不授权重叠或改变allowOverlap效果。 |
| `spec.when` | *选填* | object | 启用条件，省略不设限制；state/all/any、unknown及时间条件请见共用条件参考。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `interface`, `ifname`, `address`, `dryRun`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/basic-static.yaml)
