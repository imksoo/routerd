# SelfAddressPolicy

[各资源的配置](../resources.md#selfaddresspolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

描述供支持的静态 DHCPv6 渲染使用的有序自身地址选择策略，不会分配接口地址。

## 指定条件与引用关系

- 当前实时 DHCPv6 LAN 生成不使用 DHCPv6Server.selfAddressPolicy 或 dnsSource: self。策略可影响独立静态渲染器，但声明不会让运行中的服务器通告自身 DNS，实时路径请用明确 dnsServers/dnsServerFrom。
- 引用为已声明 Interface/IPv6DelegatedAddress 裸名，即使来源分支不使用仍验证存在。本资源无 when/dependsOn。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: SelfAddressPolicy
  metadata:
    name: "<name>"
  spec:
    addressFamily: "ipv4" # 必填; string; enum=["ipv4", "ipv6"]
    candidates: # 必填; array
      - # object
        address: "<string>" # 选填; string
        addressSuffix: "<string>" # 选填; string
        delegatedAddress: "<string>" # 选填; string
        interface: "<string>" # 选填; string
        matchSuffix: "<string>" # 选填; string
        ordinal: 1 # 选填; integer; minimum=1
        source: "delegatedAddress" # 必填; string; enum=["delegatedAddress", "interfaceAddress", "static"]
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.addressFamily` | 必填 ipv4 或 ipv6，验证接受两者；当前选择使用方为旧 DHCPv6 静态渲染器，ipv4 不会提供独立实时地址选择服务。 |
| `spec.candidates` | 必填非空有序候选列表，支持的静态渲染器依序选第一个成功值，全部失败则报错；明确引用的策略不自动补候选。 |
| `spec.candidates[].source` | 必填 delegatedAddress/interfaceAddress/static。delegatedAddress 仅限 IPv6，禁止 address/interface；interfaceAddress 需 interface，static 需符合 addressFamily 的 address。后两者多余字段通常不拒绝。 |
| `spec.candidates[].interface` | interfaceAddress 必填的已声明逻辑接口，delegatedAddress 禁用；即使所选来源不使用，提供时仍验证存在。 |
| `spec.candidates[].delegatedAddress` | delegatedAddress 来源必填的 IPv6DelegatedAddress 裸名，资源须存在；支持的渲染器由该资源接口观测前缀派生，不是通用状态 field。 |
| `spec.candidates[].address` | static 来源必填且符合 addressFamily 的纯地址，delegatedAddress 禁用，不会自动分配到 OS。 |
| `spec.candidates[].addressSuffix` | delegatedAddress 的可选 IPv6 后缀，省略继承引用资源 addressSuffix；其他来源不使用此值选择。 |
| `spec.candidates[].matchSuffix` | interfaceAddress 的可选 IPv6 模式，各非零字节须相符，零字节不限制，并非传统连续后缀掩码。设置后选第一个符合的全局 IPv6，忽略 ordinal。 |
| `spec.candidates[].ordinal` | matchSuffix 空值时的 interfaceAddress 从一开始索引，省略或 0 为 1，模式下限 1，负值验证拒绝。顺序按观测列表，不保证固定身份；越界则尝试下一候选。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go)
