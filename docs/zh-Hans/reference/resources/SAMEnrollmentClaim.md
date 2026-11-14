# SAMEnrollmentClaim

[各资源的配置](../resources.md#samenrollmentclaim) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

声明提交 SAM 注册的 leaf 自有身份、传输与移动意图。到期及撤销是 RR/控制器管理状态，不属于 leaf join HMAC。

## 指定条件与引用关系

- 本机服务的请求需已有 SAMEnrollmentPolicy。由 SAMEnrollmentClient 引用的请求可指定本机不存在的远端策略，由 RR 在提交时执行接纳/HMAC/授权检查。本机结构验证通过不等于接纳成功。
- 配置 joinTokenFrom 时需 nonce/timestamp/HMAC，audience 须符合配置值，且同一策略内 nonce 不可冲突。HMAC 为规范 leaf 载荷的 SHA-256，选择 direct 时包含 directMesh:true；更改签名身份/意图须重新签名。
- 已撤销或过期请求排除于派生拓扑。无拥有地址的 direct 请求仍可保留传输邻接，但不允许导入移动路由；策略整体范围不代表无限制 direct /32 授权。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClaim
  metadata:
    name: "<name>"
  spec:
    bgp: # 选填; object
      asn: 1 # 选填; integer; minimum=1
      routerID: "<string>" # 选填; string
    directMesh: true # 选填; boolean
    endpoint: "<string>" # 选填; string
    expiresAt: "<string>" # 选填; string
    joinAudience: "<string>" # 选填; string
    joinHMAC: "<string>" # 选填; string
    joinNonce: "<string>" # 选填; string
    joinTimestamp: "<string>" # 选填; string
    leafID: "<string>" # 必填; string
    mobility: # 选填; object
      ownedAddresses: # 选填; array
        - "<string>" # string
    policyRef: "<string>" # 必填; string
    revoked: true # 选填; boolean
    rrSetRef: "<string>" # 选填; string
    tunnelAddress: "<string>" # 必填; string
    wireGuard: # 选填; object
      allowedIPs: # 选填; array
        - "<string>" # string
      endpoint: "<string>" # 选填; string
      persistentKeepalive: 0 # 选填; integer; minimum=0; maximum=65535
      publicKey: "<string>" # 必填; string
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.policyRef` | 必填 SAMEnrollmentPolicy/name，除 SAMEnrollmentClient 用于远端接纳的 leaf 请求外，必须在本机存在。 |
| `spec.rrSetRef` | 结构上可选，但 SAMEnrollmentClient 运行时需 SAMRRSet/name。策略配置 rrSetRef 时须相等。此为运行时 RR 拓扑名称，不是需编写的静态资源。 |
| `spec.leafID` | 必填稳定节点身份，须符合 allowedLeafIDs.pattern 且在策略内唯一，用作生成的 nodeRef。 |
| `spec.joinAudience` | 可选且纳入签名的 audience；启用 join-token 认证且策略 audience 非空时须相等。 |
| `spec.joinNonce` | 可选且纳入签名的 nonce，配置 joinHMAC 或使用 join-token 策略时必填。同一策略内重复会拒绝，此 YAML 字段不自动生成。 |
| `spec.joinTimestamp` | 可选且纳入签名的 RFC3339/RFC3339Nano 时间戳。join-token 策略或以策略 TTL 检查明确 expiresAt 时必填；有此时间时，TTL 也限制从此起的请求寿命。 |
| `spec.joinHMAC` | 规范请求的可选十六进制 HMAC-SHA256 签名，join-token 策略时必填。是签名，不是 join 密钥或 ControlAPI Bearer 令牌。 |
| `spec.tunnelAddress` | 必填纯 IPv4 地址或 /32 CIDR，须在 policy.tunnelAddressPrefixes 授权范围且在策略内唯一，不自动分配。 |
| `spec.endpoint` | 可选外层隧道 IP 端点，policy.endpointPrefixes 非空时须位于该 IPv4 允许列表，与 WireGuard UDP 端点不同。 |
| `spec.wireGuard` | 可选公开 WireGuard 数据，任一子字段非空时需 publicKey，不包含私钥。 |
| `spec.wireGuard.publicKey` | 提供 WireGuard 数据时必填公钥，非空密钥在策略内须唯一，此处不自动生成。 |
| `spec.wireGuard.endpoint` | 可选 WireGuard UDP host:port（1–65535）。前缀接纳要求端点 IP 可解析，使用 wireGuard.endpointPrefixes 或 policy.endpointPrefixes；不可假设主机名会通过接纳。 |
| `spec.wireGuard.allowedIPs` | 可选额外 IPv4 CIDR，各项须包含于 policy.wireGuard.allowedExtraIPPrefixes。空值无额外前缀，生成对等端另包含 tunnelAddress /32。 |
| `spec.wireGuard.persistentKeepalive` | 整数秒 0–65535，默认 0 在生成拓扑时使用策略 WireGuard keepalive，非零覆盖默认值。 |
| `spec.directMesh` | 默认 false 仅使用 RR 对等端。true 请求加入策略 direct 对等组并纳入签名，不跳过策略启用或拥有 /32 接纳检查。 |
| `spec.mobility` | 可选声明移动所有权，默认无拥有地址，本身不发现客户端。 |
| `spec.mobility.ownedAddresses` | 由策略 mobilityPrefixes/MobilityPool 引用授权的纯 IPv4 地址或 /32。同一策略的有效请求间不可重复；direct BGP 导入仅限接纳的拥有 /32。 |
| `spec.bgp` | 可选且纳入签名的 BGP 身份意图，不是 BGPRouter 资源引用。 |
| `spec.bgp.asn` | 签名请求中的可选 uint32 ASN，解码默认 0。模式要求提供时 >=1，但结构验证不另要求非零。不是覆盖策略传输 peerASN。 |
| `spec.bgp.routerID` | 可选 IPv4 BGP 标识符，无派生默认值。非空 ID 在策略内须唯一，不创建 BGPRouter。 |
| `spec.expiresAt` | 可选 RR/管理员拥有的 RFC3339 到期时间，不纳入 join HMAC。接纳通常分配策略 TTL 或默认 5 分钟到期。两者提供时明确期限不可超过 joinTimestamp+策略 TTL，过期则排除。 |
| `spec.revoked` | 默认 false，true 为 RR/管理员撤销状态，不纳入 join HMAC，阻止拓扑接纳。leaf 无法重发签名以清除已保存撤销。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/samenrollment/enrollment.go](https://github.com/imksoo/routerd/blob/main/pkg/samenrollment/enrollment.go) · [cmd/routerd/sam_enrollment_submit.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/sam_enrollment_submit.go) · [pkg/controller/mobility/enrollmentclient.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/enrollmentclient.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)
