# WireGuardPeer

[各资源的配置](../resources.md#wireguardpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

定义已声明 WireGuardInterface 上的对等端。AllowedIPs 配置 WireGuard 的加密路由；此控制器不会自动创建普通 IP 路由。

## 指定条件与引用关系

- 对等端是接口完整 wg setconf 配置的一部分，并非独立服务。同 metadata.name 的明确对等端会覆盖 peersFrom 生成的对等端，即使 interface 不同。
- presharedKey 优先于 presharedKeyFile，两者不互斥。只有未直接指定时才读文件并移除首尾空白；缺少、无法读取或空文件会使实际应用失败。
- Connected 表示观察到握手时间戳，经过时间另行报告；不保证握手是最近发生或目标可达。有 endpoint 但无握手时为 Waiting/NoHandshakeYet。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardPeer
  metadata:
    name: "<name>"
  spec:
    allowedIPs: # 必填; array
      - "<string>" # string
    endpoint: "<string>" # 选填; string
    interface: "<string>" # 必填; string
    persistentKeepalive: 0 # 选填; integer; minimum=0; maximum=65535
    presharedKey: "<string>" # 选填; string
    presharedKeyFile: "<string>" # 选填; string
    publicKey: "<string>" # 必填; string
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.interface` | 必填，同一 Router 中已有 WireGuardInterface 的 metadata.name，仅填 OS ifname 不足以建立引用。 |
| `spec.publicKey` | 必填的对等端 WireGuard Base64 公钥。验证仅检查非空，不解码确认长度；应用时由 wg 检查。 |
| `spec.allowedIPs` | 必填且不可为空的 IPv4/IPv6 CIDR 列表，控制发送数据包的对等端选择及接收数据包允许的源地址。不会补入前缀或创建普通路由。 |
| `spec.endpoint` | 可选的对等端 host:UDP-port，IPv6 使用 [IPv6]:port。空值省略 Endpoint，可从收到的数据包学习端点。routerd 配置验证不检查语法或 DNS 可达性，应用时由 wg 处理。 |
| `spec.persistentKeepalive` | 持续保活数据包间隔秒数，整数 0–65535；默认 0 表示禁用。不是握手成功期限。 |
| `spec.presharedKey` | 可选的额外对称密钥，使用 WireGuard Base64 格式，默认无。与 presharedKeyFile 并用不报互斥错误，优先使用此值。 |
| `spec.presharedKeyFile` | 可选的路由器本机文件，内容为预共享密钥本身，不是环境变量文件。缺少时不会生成密钥，仅在 presharedKey 为空时读取。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/wireguard/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/wireguard/wireguard.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `latestEndpoint`, `latestHandshake`, `handshakeAgeSeconds`, `transferRxBytes`, `transferTxBytes`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-wireguard.yaml)
