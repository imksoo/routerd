# ClusterNetworkRoute

[各资源的配置](../resources.md#clusternetworkroute) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

通过明确指定的下一跳，配置前往 Kubernetes Pod 与 Service 网络的 IPv4 路由。不会自动发现集群 CIDR 或节点。

## 指定条件与引用关系

- pods 与 services 合计至少需要一个 CIDR，via 至少需要一个条目。仅接受 IPv4，两个列表之间也不允许重复或重叠的网段。
- 每个 CIDR 与 via 的组合会生成名为 <name>-pods-NN-via-NN 或 <name>-services-NN-via-NN 的 IPv4StaticRoute，编号从 1 开始。若已明确声明相同 apiVersion、kind、name 的资源，则优先使用该资源并跳过生成。
- weight 转换为路由 metric，并非 ECMP 流量分配权重：省略或 0 为 100；1–999 为 1000 − weight。正值越大，metric 越低；0 与 900 的 metric 相同。
- 生成的路由也受 IPv4StaticRoute 实现限制：FreeBSD 会输出，但目前 Linux 流程不安装生成的静态路由。展开与验证成功并不证明能正常转发。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: ClusterNetworkRoute
  metadata:
    name: "<name>"
  spec:
    [ pods:
        [ cidrs:
            - <string>
        ]
    ]
    [ services:
        [ cidrs:
            - <string>
        ]
    ]
    via:
      -
        interface: <string>
        nextHop: <string>
        [ weight: <integer> ]
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
| `spec.pods` | *选填* | object | 可选的 Pod 网络 CIDR 组；不会自动补入 CIDR。 |
| `spec.pods.cidrs` | *选填* | array | Pod 网络的 IPv4 前缀。默认为空；services.cidrs 为空时必填。组内及组间均不可重复或重叠。 |
| `spec.services` | *选填* | object | 可选的 Service 网络 CIDR 组；不会自动补入 CIDR。 |
| `spec.services.cidrs` | *选填* | array | Service 网络的 IPv4 前缀。默认为空；pods.cidrs 为空时必填。两个组均适用相同的重复与重叠限制。 |
| `spec.via` | **必填** | array | 必填且不可为空的下一跳列表。每个条目均应用于所有 CIDR。相同 interface 与 nextHop 组合即使 weight 不同也会被拒绝。 |
| `spec.via[].interface` | **必填** | string | 必填，为同一 Router 中声明的逻辑接口名称。必须能在配置的接口索引中解析，并非状态值来源。 |
| `spec.via[].nextHop` | **必填** | string | 必填，为不含 CIDR 的 IPv4 下一跳地址，不接受资源引用。没有默认值或自动发现。 |
| `spec.via[].weight` | *选填* | integer; minimum=0; maximum=999 | 可选整数 0–999，默认为 0。按上述规则转换为 metric，不是流量比例。 |
| `spec.when` | *选填* | object | 可选的通用状态条件；省略表示无条件。state/all/any 的语义与模式限制请见通用字段。先筛选条件，再展开生成的路由。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/cluster_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/api/cluster_routes.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go) · [pkg/render/networkd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/networkd.go) · [pkg/render/freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `pods`, `services`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/k8s-cluster-routes.yaml)
