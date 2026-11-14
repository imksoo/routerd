# RouterdCluster

[各资源的配置](../resources.md#routerdcluster) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

通过文件租约与OS文件锁协调哪个routerd可以变更状态，不是VRRP选举或对端网络发现。

## 指定条件与引用关系

- 参与者需要共享且锁定语义可靠的leasePath。peers是标识而非端点；租约实现仅用非空列表启用协调，不连接对端或验证成员资格。待机controller以dry-run变更路径运行。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: RouterdCluster
  metadata:
    name: "<name>"
  spec:
    [ identity: <string> ]
    leasePath: <string>
    [ leaseTTL: <string> ]
    peers:
      - <string>
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
| `spec.peers` | **必填** | array | 去除空白与重复后至少两项的必填列表，无默认值；不是Kind/名称引用或实际连接地址。 |
| `spec.leaseTTL` | *选填* | string | 租约期限默认30s，显式值须为5s–10m的Go duration。互斥变更仍由锁控制，期限本身不是网络故障检测器。 |
| `spec.leasePath` | **必填** | string | 必填租约文件绝对路径，不可含换行，无默认值；各主机独立文件不会协调主机间状态。 |
| `spec.identity` | *选填* | string | 本机租约持有者标识，省略使用修剪后os.Hostname()，不是Router.metadata.name。 |
| `spec.when` | *选填* | object | 启用条件，省略不设限制，请见共用条件参考。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/ha/lease.go](https://github.com/imksoo/routerd/blob/main/pkg/ha/lease.go) · [cmd/routerd/cmd_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_apply.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `leader`, `leaseExpiresAt`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/ha-2-node.yaml)
