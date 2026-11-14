# WireGuardInterface

[各资源的配置](../resources.md#wireguardinterface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

创建或配置 WireGuard 设备，通过 wg setconf 应用完整对等端配置。地址分配与普通路由由其他资源配置。

## 指定条件与引用关系

- 运行时需提供 privateKey 或 privateKeyFile，否则维持 Pending/PrivateKeyMissing。两者并非互斥：优先使用直接指定的密钥，但应用时仍会检查或创建 privateKeyFile。
- 静态 WireGuardPeer.interface 引用此资源的 metadata.name，而非 ifname。导入的对等端按 Kind/name 合并；较后来源覆盖较早来源，明确声明的同名 WireGuardPeer 覆盖生成的对等端。
- 使用注册式 direct SAMPeerGroup 时，必须放在 peersFrom 最后，前方须有非 optional 的 SAMRRSet。运行时也需相同注册策略的可用 RR 对等端；direct 不可用或不兼容时仍保留 RR 连接。
- listenPort 会保留该 UDP 端口，不可与其他配置的监听冲突。Linux 在指定正数端口时也会加入 INPUT UDP 允许规则。Up 表示接口已配置，不保证对等端握手成功。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardInterface
  metadata:
    name: "<name>"
  spec:
    ifname: "<string>" # 选填; string
    listenPort: 1 # 选填; integer; minimum=1; maximum=65535
    mtu: 576 # 选填; integer; minimum=576; maximum=9216
    peersFrom: # 选填; array
      - # object
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    privateKey: "<string>" # 选填; string
    privateKeyFile: "<string>" # 选填; string
    selfNodeRef: "<string>" # 选填; string
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.ifname` | OS 设备名称，默认 metadata.name。目前即使指定 ifname，验证仍检查 metadata.name 的空白、斜线及 15 字符上限，未对 ifname 应用相同检查。 |
| `spec.selfNodeRef` | 导入时排除的本机 SAM 节点标识，默认 Router.metadata.name。这是 nodeRef 值，不是 Kind/name 状态引用。 |
| `spec.privateKey` | 直接指定 WireGuard 私钥，默认为空，优先于 privateKeyFile。使用 WireGuard Base64 密钥格式；配置验证不保证密钥有效，应用时 wg 或文件密钥检查仍可能失败。 |
| `spec.privateKeyFile` | 路由器上的可选密钥文件路径。实际应用时，缺少的文件以 0600 创建，新父目录为 0700。已有文件须含有效的 32 字节 Base64 私钥；空文件或无法读取会失败，不会覆盖。不是环境变量文件。 |
| `spec.listenPort` | 可选 UDP 监听端口 1–65535。省略或 0 时不输出 ListenPort，由 WireGuard 选择或保留端口；routerd 不为 0 添加明确的端口例外。 |
| `spec.mtu` | 可选 MTU 576–9216。省略或 0 保留已有值或 OS 创建时默认值；此处不执行 wg-quick 的 MTU 计算。 |
| `spec.peersFrom` | 可选且有顺序的 SAM 对等端来源列表，默认为空。非 optional 来源缺少时接口维持 Pending/PeersFromPending，静态对等端不会跳过此条件。已解析但为空的列表不同于来源缺失。 |
| `spec.peersFrom[].resource` | 必填来源，格式为 SAMNodeSet/name、SAMPeerGroup/name、SAMEnrollmentPolicy/name 或 SAMRRSet/name，不使用 field。此处验证语法，解析对等端时查找来源。会跳过自身及没有 WireGuard 数据的节点。 |
| `spec.peersFrom[].optional` | 默认 false。true 允许来源缺失而不使接口待处理，但不会全面忽略格式错误的来源。注册式 direct 组另适用上述保留 RR 的规则。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go) · [pkg/wireguard/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/wireguard/wireguard.go) · [pkg/render/firewall_holes.go](https://github.com/imksoo/routerd/blob/main/pkg/render/firewall_holes.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `publicKey`, `selfNodeRef`, `listenPort`, `fwmark`, `peerCount`, `hostFirewall`, `peersFrom`, `pendingSources`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
