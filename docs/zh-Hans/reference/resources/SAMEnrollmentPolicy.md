# SAMEnrollmentPolicy

[各资源的配置](../resources.md#samenrollmentpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

定义 RR 端 SAM leaf 身份、传输地址、可选 join 认证及允许移动所有权的接纳边界。leaf 使用接纳拓扑，不复制此策略。

## 指定条件与引用关系

- RR 输出名称每个策略须唯一，rrSetRef/rrNodeSetRef 成对配置。directMesh 另要求 pair-stable 传输寻址及 RR 备用。
- 同一策略内检查 leaf ID、隧道地址、非空 WireGuard 密钥、拥有地址及非空 BGP router ID 冲突。端点、WireGuard 额外前缀及移动所有权分别授权。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentPolicy
  metadata:
    name: "<name>"
  spec:
    allowedLeafIDs: # 选填; object
      pattern: "<string>" # 选填; string
    directMesh: # 选填; object
      peerGroupRef: "<string>" # 选填; string
    endpointPrefixes: # 选填; array
      - "<string>" # string
    joinAudience: "<string>" # 选填; string
    joinTokenFrom: # 选填; object
      base64: true # 选填; boolean
      env: "<string>" # 选填; string
      file: "<string>" # 选填; string
    mobilityPoolRefs: # 选填; array
      - "<string>" # string
    mobilityPrefixes: # 选填; array
      - "<string>" # string
    rrNodeSetRef: "<string>" # 选填; string
    rrSetRef: "<string>" # 选填; string
    transportProfileRef: "<string>" # 必填; string
    ttl: "<string>" # 选填; string
    tunnelAddressPrefixes: # 必填; array
      - "<string>" # string
    wireGuard: # 选填; object
      allowedExtraIPPrefixes: # 选填; array
        - "<string>" # string
      endpointPrefixes: # 选填; array
        - "<string>" # string
      interface: "<string>" # 必填; string
      persistentKeepalive: 0 # 选填; integer; minimum=0; maximum=65535
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.transportProfileRef` | 必填同一 Router 中已有 SAMTransportProfile/name，将接纳的 leaf 绑定至该传输配置，不是状态来源。 |
| `spec.rrSetRef` | 可选运行时输出名称 SAMRRSet/name，须与 rrNodeSetRef 同时配置且跨注册策略唯一。这是生成快照名称，不是静态编写 SAMRRSet。direct mesh 与 leaf 客户端获取需要此值。 |
| `spec.rrNodeSetRef` | 供应 RR 身份的已有 SAMNodeSet/name，与 rrSetRef 同时必填。该 NodeSet 所有节点须为 routeReflector:true，不在策略内复制 RR 注册信息。 |
| `spec.joinTokenFrom` | 可选共享 HMAC 密钥来源。省略不要求 join HMAC，由受保护 ControlAPI 作为信任边界。提供时 file/env 只能一项，请求须有 nonce/timestamp/HMAC。与 ControlAPI Bearer 认证不同。 |
| `spec.joinTokenFrom.file` | 路由器本机原始 HMAC 密钥文件，与 env 互斥，移除首尾空白。即使离线验证无法读取，实际提交时缺少密钥仍会失败。 |
| `spec.joinTokenFrom.env` | 存放 HMAC 密钥的 routerd 进程环境变量名称，与 file 互斥，不自动加载环境文件。 |
| `spec.joinTokenFrom.base64` | 默认 false，true 对移除首尾空白的来源执行标准 Base64 解码，解码字节作为 HMAC 密钥。Base64 不是加密。 |
| `spec.joinAudience` | 配置 joinTokenFrom 时可选的预期请求 audience，非空值须等于 claim.joinAudience，无默认 audience。 |
| `spec.allowedLeafIDs` | 可选 leaf 身份限制对象，省略不施加正则表达式限制。 |
| `spec.allowedLeafIDs.pattern` | 匹配移除首尾空白 leafID 的 Go 正则表达式。空值允许其他条件有效的任意 ID，不自动加首尾锚点，完整匹配请用 ^...$。 |
| `spec.tunnelAddressPrefixes` | 必填且不可为空的 IPv4 CIDR 列表，授权 claim.tunnelAddress /32 身份，不自动分配地址。 |
| `spec.endpointPrefixes` | 可选外层端点 IPv4 CIDR 允许列表，空值不限制端点前缀。wireGuard.endpointPrefixes 为空时，也作为 WireGuard 端点允许列表。 |
| `spec.wireGuard` | 可选接纳 leaf 的 WireGuard 控制。使用其他操作性 WireGuard 配置时需 interface，仅端点前缀验证不供应 WireGuard 设备。 |
| `spec.wireGuard.interface` | 可选已有 WireGuardInterface metadata.name，不是 Kind/name。限制导入此策略的本机 WireGuard 接口，不自动创建。 |
| `spec.wireGuard.endpointPrefixes` | 可选 IPv4 CIDR，限制请求的 WireGuard 外层端点 IP。空值使用 endpointPrefixes，两者均空则无此前缀限制。 |
| `spec.wireGuard.allowedExtraIPPrefixes` | 可选 IPv4 CIDR，授权额外 claim.wireGuard.allowedIPs。隧道 /32 会另行加入生成的对等端；空值不授权额外前缀。 |
| `spec.wireGuard.persistentKeepalive` | 秒数 0–65535，默认 0。请求 keepalive 为 0 时供生成的 WireGuard 对等端使用，请求的明确非零值优先。 |
| `spec.directMesh` | 可选且限于此策略的 direct leaf mesh 输出，不移除 RR 备用路径。 |
| `spec.directMesh.peerGroupRef` | SAMPeerGroup/name 运行时输出，空值禁用 direct mesh。需要 rrSetRef 及引用传输的 addressingMode: pair-stable，跨策略须唯一，不是静态编写 SAMPeerGroup。 |
| `spec.mobilityPoolRefs` | 可选已有 MobilityPool/name 列表，提供允许的所有权前缀，与 mobilityPrefixes 合并，不要求创建池或云端资源。 |
| `spec.mobilityPrefixes` | 可选额外 CIDR，授权请求的拥有 IPv4 /32，与引用池合并。此值及池引用均空时不授权拥有地址。 |
| `spec.ttl` | 可选 Go duration，实际提交接纳省略时默认租约 5 分钟。配置 TTL 也限制从 joinTimestamp 起的请求寿命及明确 expiresAt。结构验证仅解析时间，但运行时无效或非正寿命会视为过期。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/samenrollment/enrollment.go](https://github.com/imksoo/routerd/blob/main/pkg/samenrollment/enrollment.go) · [cmd/routerd/sam_enrollment_submit.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/sam_enrollment_submit.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go) · [pkg/controller/mobility/enrollmentclient.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/enrollmentclient.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
