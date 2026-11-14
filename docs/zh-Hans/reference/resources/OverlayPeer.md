# OverlayPeer

[各资源的配置](../resources.md#overlaypeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

描述供 HybridRoute 与路径 MTU 策略派生使用的现有传输；本资源不自行建立 WireGuard、Tailscale、IPsec 或隧道。

## 指定条件与引用关系

- role、nodeID、underlay.type 必填。常规验证在 wireguard/ipip/gre/fou/gue 要求 underlay.interface，HybridRoute 展开时则所有类型均需要，含 tailscale/ipsec/route。
- ipip/gre/fou/gue 的 underlay.interface 裸名须引用已声明 TunnelInterface，但不验证 mode 与 underlay.type 相符。WireGuard 可使用外部管理设备，仅警告不拒绝；其他设备也须事先可用。
- nodeID/remote.nodeID/role 为描述信息，不提供发现、认证、HA 选举或自动拓扑匹配；无 when/dependsOn。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: OverlayPeer
  metadata:
    name: "<name>"
  spec:
    nodeID: <string>
    [ pathMTU:
        [ forceFragmentIPv4: <boolean> ]
    ]
    [ remote:
        [ address: <string> ]
        [ nodeID: <string> ]
    ]
    role: ("onprem" | "cloud")
    underlay:
      [ address: <string> ]
      [ interface: <string> ]
      type: ("wireguard" | "tailscale" | "ipsec" | "route" | "ipip" | "gre" | "fou" | "gue")
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.role` | **必填** | string; enum=["onprem", "cloud"] | 必填 onprem 或 cloud，无默认值，不创建云资源或选择云厂商。 |
| `spec.nodeID` | **必填** | string | 必填非空本机节点身份标签，不解析为主机或外部账户。 |
| `spec.underlay` | **必填** | object | 必填传输描述，实际网络传输须另行配置。 |
| `spec.underlay.type` | **必填** | string; enum=["wireguard", "tailscale", "ipsec", "route", "ipip", "gre", "fou", "gue"] | 必填 wireguard/tailscale/ipsec/route/ipip/gre/fou/gue，选择路由展开与 MTU 开销估算，不创建守护进程。 |
| `spec.underlay.interface` | *选填* | string | 生成 IPv4Route 所用设备或逻辑接口，路由展开时必填。隧道模式使用 TunnelInterface 裸名，外部管理设备使用实际 OS 名称，不自动发现。 |
| `spec.underlay.address` | *选填* | string | 可选纯 IPv4/IPv6 描述，验证为 IP。当前 HybridRoute 目标派生不将其用作网关或本机绑定，也不分配到接口。 |
| `spec.remote` | *选填* | object | 可选远端描述，默认空，不注册或协商对端。 |
| `spec.remote.nodeID` | *选填* | string | 可选远端身份标签，默认空，不用于实时发现或授权。 |
| `spec.remote.address` | *选填* | string | 可选 IP 字符串。仅 ipsec 路由展开会以有效 IPv4 作 gateway，无效/IPv6/空值为设备路由。其他类型不以此作网关，OverlayPeer 验证也未单独检查其为 IP。 |
| `spec.pathMTU` | *选填* | object | 可选转发路径 MTU 选项，默认不要求强制分片，不会加密底层。 |
| `spec.pathMTU.forceFragmentIPv4` | *选填* | boolean | 默认 false，true 仅限 wireguard/ipip/gre/fou/gue。通过 Linux nftables 对符合派生转发路径的过大 IPv4 清除 DF，不是全局隧道标志或 IPv6 分片。FreeBSD 跳过此控制器，此路径无同等 DF 清除动作。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/hybrid/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/hybrid/hybrid.go) · [pkg/render/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/render/mtu.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/mtu.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `nodeID`, `role`, `underlayType`, `underlayInterface`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
