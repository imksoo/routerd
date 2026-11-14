# VXLANTunnel

[各资源的配置](../resources.md#vxlantunnel) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

管理具有静态泛洪目标及可选桥接的 Linux VXLAN 设备，不配置组播发现或动态 MAC 学习。

## 指定条件与引用关系

- 操作控制器在非 Linux 系统直接返回，不应用配置。此资源应使用 Linux 目标，模式验证通过不代表 FreeBSD 可运行。
- when 采用关闭优先：false、未解析、过期或开机前的状态会先停用再删除设备，不只是跳过更新。每个引用状态默认有效 30 秒，可用 maxAge 覆盖。验证拒绝接受 unknown 的条件。
- tcpMSSClamp 需要 bridge 与明确指定的 mtu >=1280。生成规则时还需真正的 Bridge 资源及可解析的成员接口；全局引用验证仅确认共享接口索引中的存在。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: VXLANTunnel
  metadata:
    name: "<name>"
  spec:
    [ bridge: <string> ]
    [ ifname: <string> ]
    localAddress: <string>
    [ mtu: <integer> ]
    [ outerDF: ("" | "inherit" | "set" | "unset") ]
    [ peers:
        - <string>
    ]
    [ tcpMSSClamp: <boolean> ]
    [ udpPort: <integer> ]
    underlayInterface: <string>
    vni: <integer>
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
| `spec.ifname` | *选填* | string | OS VXLAN 名称，默认 metadata.name，最多 15 字符，不可含空白或斜线。 |
| `spec.vni` | **必填** | integer; minimum=1; maximum=16777215 | 必填 VXLAN 网络标识符，整数 1–16777215，须与目标远端二层网段一致。 |
| `spec.localAddress` | **必填** | string | 必填的外层本机 IP 地址，不提供动态状态来源。验证只确认 IP 格式，不检查本机分配或与对等端地址族一致。 |
| `spec.peers` | *选填* | array | 可选的远端外层 IP 列表，默认为空。同步为 00:00:00:00:00:00 的 permanent self 泛洪 FDB 条目，移除多余泛洪条目。空列表不提供静态远端泛洪目标。 |
| `spec.underlayInterface` | **必填** | string | 必填同一 Router 中声明的逻辑接口，会解析为 OS 设备名称。无状态来源或自动选择。 |
| `spec.udpPort` | *选填* | integer; minimum=1; maximum=65535 | 外层 UDP 目标端口，省略或 0 默认 4789，明确范围为 1–65535。 |
| `spec.mtu` | **条件必填** | integer; minimum=0 | 可选的内层设备 MTU 576–9216，省略或 0 使用 OS/已有 MTU。即使模式下限为 0，tcpMSSClamp 仍要求明确值 >=1280。 |
| `spec.bridge` | **条件必填** | string | 可选的已声明网桥名称，解析为 OS 名称并接为 VXLAN master，默认不连接。tcpMSSClamp 时必填，请使用 Bridge 资源而非其他接口类型。 |
| `spec.outerDF` | *选填* | string; enum=["", "inherit", "set", "unset"] | 外层 IPv4 DF 策略，默认 inherit，也可用 set/unset，传入 ip link VXLAN df。不配置内层地址或路由。 |
| `spec.when` | *选填* | object | 可选的通用 state/all/any 条件，另适用上述关闭优先及新鲜度规则。省略表示无条件；接受 unknown 的条件不可启用此隧道。 |
| `spec.tcpMSSClamp` | *选填* | boolean | 默认 false。true 为此 VXLAN 与 Bridge 成员端口之间创建 Linux bridge 族 IPv4/IPv6 TCP SYN MSS 规则。取 VXLAN/网桥/成员的最小正 MTU，IPv4 减 40、IPv6 减 60，不处理任意路由流量。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/vxlan_tunnel.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/vxlan_tunnel.go) · [pkg/vxlan/vxlan.go](https://github.com/imksoo/routerd/blob/main/pkg/vxlan/vxlan.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `ifname`, `vni`
