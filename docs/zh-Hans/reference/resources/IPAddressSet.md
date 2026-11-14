# IPAddressSet

[各资源的配置](../resources.md#ipaddressset) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

在支持的 nftables NAT、路由及防火墙规则间共享固定与 DNS 解析的 IP 地址。不创建路由或启动 DNS 服务。

## 指定条件与引用关系

- addresses 或 names 至少一条，两者可并用。地址为单个 IPv4/IPv6，不接受 CIDR。各配置列表内重复会被拒绝；解析结果则合并并去重。
- 使用端以裸名称或 IPAddressSet/name 引用。支持的 nftables 使用端包含 LocalServiceRedirect、NAT44Rule/FlowDNATPinhole、EgressRoutePolicy 与 FirewallRule/FlowPinhole。未被引用时仅报告 Observed/referenced:false，不解析名称或创建内核集合。
- 更新前须由使用端生成器创建 nftables 集合，否则为 Pending/SetMissing。名称通过 /etc/resolv.conf 查询 A/AAAA，不使用 DNSUpstream 引用。部分 DNS 失败仍可应用其余地址；全部失败则为 Pending/ResolveFailed，该轮不替换现有集合。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPAddressSet
  metadata:
    name: "<name>"
  spec:
    addresses: # 选填; array
      - "<string>" # string
    names: # 选填; array
      - "<string>" # string
    refreshInterval: "<string>" # 选填; string
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
| `spec.addresses` | 可选的固定 IPv4/IPv6 列表，默认为空；names 为空时必填。IPv4 映射 IPv6 会转为 IPv4，不展开子网。 |
| `spec.names` | 可选的 DNS 名称，默认为空；addresses 为空时必填。拒绝空名称与去除前后空白后的重复。A/AAAA 结果与固定值合并，不展开通配名称。 |
| `spec.refreshInterval` | 可选 Go duration。默认为最小正 DNS TTL 的一半，最低 1m；无正 TTL 时视为 5m，因此为 2m30s。正配置值只能缩短周期，可低于 1m。0／负值可解析但不缩短。实际更新在控制器协调时执行。 |
| `spec.when` | 可选的通用启用条件，默认无条件。state/all/any 与使用端启用行为请见通用字段。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/controller/chain/ipaddressset.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipaddressset.go) · [pkg/render/local_service_redirect.go](https://github.com/imksoo/routerd/blob/main/pkg/render/local_service_redirect.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `addresses`, `ipv4Addresses`, `ipv6Addresses`, `updatedAt`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)
