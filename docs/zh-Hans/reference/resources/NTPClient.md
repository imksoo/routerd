# NTPClient

[各资源的配置](../resources.md#ntpclient) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

管理或观察向上游同步时间的客户端。

## 指定条件与引用关系

- managed=true须声明servers/serverFrom/fallbackServers至少一项，static还要求servers本身。无法解析上游时客户端为Pending/NoServers；管理chrony时禁用系统systemd-timesyncd。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPClient
  metadata:
    name: "<name>"
  spec:
    fallbackServers: # 选填; array
      - "<string>" # string
    interface: "<string>" # 选填; string
    managed: true # 选填; boolean
    provider: "systemd-timesyncd" # 选填; string; enum=["systemd-timesyncd", "chrony", "ntpd"]
    serverFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    servers: # 选填; array
      - "<string>" # string
    source: "static" # 选填; string; enum=["static", "auto", "dhcp", "dhcpv6"]
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
| `spec.provider` | 客户端实现，通常默认systemd-timesyncd，FreeBSD默认ntpd，也接受chrony。ntpd需要FreeBSD rc.d，systemd-timesyncd需要平台支持。 |
| `spec.managed` | 默认false，仅记录Observed，不配置或启动时间服务；管理文件与服务时设true。 |
| `spec.source` | 默认static，使用servers。auto/dhcp/dhcpv6均依serverFrom解析值、fallbackServers、servers的顺序选择，不会自动查找DHCP来源。 |
| `spec.interface` | 选填逻辑接口名，默认空；指定时Router整体验证要求存在。NTP controller不使用它选择来源或绑定服务。 |
| `spec.servers` | 上游主机名或IP，默认空；managed=true且source省略或static时必填。各项须为非空白单一值，不是资源引用。 |
| `spec.serverFrom` | 显式值来源列表，默认空。例如DHCPv4Client/wan的ntpServers或DHCPv6PrefixDelegation/wan-pd的sntpServers。仅用于auto/dhcp/dhcpv6，每个resource与field须存在。 此解析器不使用optional标记，缺值即选下一种替代来源。 |
| `spec.fallbackServers` | 备用上游，默认空；动态来源的serverFrom无值时采用。systemd-timesyncd还会生成FallbackNTP，chrony与ntpd不生成独立备用指令。 |
| `spec.when` | 启用条件，省略不设限制；state/all/any、unknown及时间条件请见共用条件参考。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/ntp.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ntp.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/router-lab.yaml)
