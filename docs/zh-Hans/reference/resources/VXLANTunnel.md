# VXLANTunnel

[各资源的配置](../resources.md#vxlantunnel) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

管理具有静态泛洪目标及可选桥接的 Linux VXLAN 设备，不配置组播发现或动态 MAC 学习。

## 指定条件与引用关系

- 操作控制器在非 Linux 系统直接返回，不应用配置。此资源应使用 Linux 目标，模式验证通过不代表 FreeBSD 可运行。
- when 采用关闭优先：false、未解析、过期或开机前的状态会先停用再删除设备，不只是跳过更新。每个引用状态默认有效 30 秒，可用 maxAge 覆盖。验证拒绝接受 unknown 的条件。
- tcpMSSClamp 需要 bridge 与明确指定的 mtu >=1280。生成规则时还需真正的 Bridge 资源及可解析的成员接口；全局引用验证仅确认共享接口索引中的存在。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VXLANTunnel
  metadata:
    name: "<name>"
  spec:
    bridge: "<string>" # 选填; string
    ifname: "<string>" # 选填; string
    localAddress: "<string>" # 必填; string
    mtu: 0 # 选填; integer; minimum=0
    outerDF: "" # 选填; string; enum=["", "inherit", "set", "unset"]
    peers: # 选填; array
      - "<string>" # string
    tcpMSSClamp: true # 选填; boolean
    udpPort: 1 # 选填; integer; minimum=1; maximum=65535
    underlayInterface: "<string>" # 必填; string
    vni: 1 # 必填; integer; minimum=1; maximum=16777215
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
| `spec.ifname` | OS VXLAN 名称，默认 metadata.name，最多 15 字符，不可含空白或斜线。 |
| `spec.vni` | 必填 VXLAN 网络标识符，整数 1–16777215，须与目标远端二层网段一致。 |
| `spec.localAddress` | 必填的外层本机 IP 地址，不提供动态状态来源。验证只确认 IP 格式，不检查本机分配或与对等端地址族一致。 |
| `spec.peers` | 可选的远端外层 IP 列表，默认为空。同步为 00:00:00:00:00:00 的 permanent self 泛洪 FDB 条目，移除多余泛洪条目。空列表不提供静态远端泛洪目标。 |
| `spec.underlayInterface` | 必填同一 Router 中声明的逻辑接口，会解析为 OS 设备名称。无状态来源或自动选择。 |
| `spec.udpPort` | 外层 UDP 目标端口，省略或 0 默认 4789，明确范围为 1–65535。 |
| `spec.mtu` | 可选的内层设备 MTU 576–9216，省略或 0 使用 OS/已有 MTU。即使模式下限为 0，tcpMSSClamp 仍要求明确值 >=1280。 |
| `spec.bridge` | 可选的已声明网桥名称，解析为 OS 名称并接为 VXLAN master，默认不连接。tcpMSSClamp 时必填，请使用 Bridge 资源而非其他接口类型。 |
| `spec.outerDF` | 外层 IPv4 DF 策略，默认 inherit，也可用 set/unset，传入 ip link VXLAN df。不配置内层地址或路由。 |
| `spec.when` | 可选的通用 state/all/any 条件，另适用上述关闭优先及新鲜度规则。省略表示无条件；接受 unknown 的条件不可启用此隧道。 |
| `spec.tcpMSSClamp` | 默认 false。true 为此 VXLAN 与 Bridge 成员端口之间创建 Linux bridge 族 IPv4/IPv6 TCP SYN MSS 规则。取 VXLAN/网桥/成员的最小正 MTU，IPv4 减 40、IPv6 减 60，不处理任意路由流量。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/vxlan_tunnel.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/vxlan_tunnel.go) · [pkg/vxlan/vxlan.go](https://github.com/imksoo/routerd/blob/main/pkg/vxlan/vxlan.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go)
