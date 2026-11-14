# DHCPv6PrefixDelegation

[各资源的配置](../resources.md#dhcpv6prefixdelegation) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

在上行接口取得 IPv6 委派前缀。下游或主机地址须以 IPv6DelegatedAddress 派生并应用，仅取得前缀不会向客户端通告。

## 指定条件与引用关系

- interface 须为现有逻辑接口名称，默认 client 为 routerd-dhcpv6-client。也识别 networkd、dhcp6c、dhcpcd 并生成平台配置，但实时监督与状态套接字以 routerd 守护进程为主，生命周期支持不应视为等同。
- profile/prefixLength 影响外部客户端配置及还原快照比对。受监督的 routerd-dhcpv6-client 不接收 profile 或 prefix-length 标志，获取时不以此提示长度；clientDUID 会传入。
- 不支持 iaid/duidType YAML 控制。routerd 守护进程 IA_PD IAID 默认 1，未指定 clientDUID 时由上行 MAC 派生 DUID-LL。required 不会实现阻挡启动的要求。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: "<name>"
  spec:
    client: "<string>" # 选填; string
    clientDUID: "<string>" # 选填; string
    interface: "<string>" # 必填; string
    prefixLength: 1 # 选填; integer; minimum=1; maximum=128
    profile: "default" # 选填; string; enum=["default", "ntt-ngn-direct-hikari-denwa", "ntt-hgw-lan-pd"]
    required: true # 选填; boolean
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
| `spec.interface` | 必填同一 Router 的逻辑上行接口。外部 dhcp6c/dhcpcd PD 客户端须与同接口的 DHCPv6Address client 相符。 |
| `spec.client` | 默认 routerd-dhcpv6-client，接受 networkd/dhcp6c/dhcpcd。当前监督也可能合成 routerd DHCPv6 systemd 单元，选外部客户端不普遍保证禁用内置守护进程。 |
| `spec.profile` | 空值/default、ntt-ngn-direct-hikari-denwa 或 ntt-hgw-lan-pd。NTT 在支持的生成路径提供 /60 与 link-layer DUID 默认值，不配置运营商凭据或变更内置命令。 |
| `spec.prefixLength` | 可选 1–128，省略或 0 时 NTT 为 /60，default 无长度提示，不保证服务器分配长度；内置获取限制见上文。 |
| `spec.clientDUID` | 可选无分隔符且偶数长度的十六进制 DUID，拒绝冒号与空白。空值由内置客户端从 MAC 派生 DUID-LL，身份也影响恢复租约是否可复用。 |
| `spec.required` | 布尔值默认 false，会解码但当前不作为必要性或启动等待策略。依赖方应使用实际 Bound 状态及依赖条件。 |
| `spec.when` | 可选通用状态条件，默认无条件。监督客户端按已解析条件筛选，条件为假可在保留声明时停止获取。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/profiles.go](https://github.com/imksoo/routerd/blob/main/pkg/api/profiles.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [cmd/routerd-dhcpv6-client/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dhcpv6-client/main.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
