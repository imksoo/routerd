# IPv6RAAddress

[各资源的配置](../resources.md#ipv6raaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

声明主机接收IPv6 RA/SLAAC，属于RA client，不是向LAN通告的IPv6RouterAdvertisement。

## 指定条件与引用关系

- managed控制主机RA配置。FreeBSD生成accept_rtadv/rtsold；Linux networkd接管保留/启用RA，将DHCPv6移出networkd时也由drop-in抑制RA触发DHCPv6。不保证收到可用上游RA。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RAAddress
  metadata:
    name: "<name>"
  spec:
    interface: "<string>" # 必填; string
    managed: true # 选填; boolean
    required: true # 选填; boolean
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.interface` | 必填Router接口索引中的逻辑名称，不加Interface/，必须在声明配置存在，无默认值。 |
| `spec.managed` | 默认true；false不要求主机RA配置，仅选择观察意图，与Interface的managed独立。 |
| `spec.required` | 默认false，当前观察/生成路径不读取，不要求apply返回前完成SLAAC地址获取。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/render/freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go)
