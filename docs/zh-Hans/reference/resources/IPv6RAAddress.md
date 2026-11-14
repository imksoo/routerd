# IPv6RAAddress

[各资源的配置](../resources.md#ipv6raaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

声明主机接收IPv6 RA/SLAAC，属于RA client，不是向LAN通告的IPv6RouterAdvertisement。

## 指定条件与引用关系

- managed控制主机RA配置。FreeBSD生成accept_rtadv/rtsold；Linux networkd接管保留/启用RA，将DHCPv6移出networkd时也由drop-in抑制RA触发DHCPv6。不保证收到可用上游RA。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RAAddress
  metadata:
    name: "<name>"
  spec:
    interface: <string>
    [ managed: <boolean> ]
    [ required: <boolean> ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.interface` | **必填** | string | 必填Router接口索引中的逻辑名称，不加Interface/，必须在声明配置存在，无默认值。 |
| `spec.managed` | *选填* | boolean | 默认true；false不要求主机RA配置，仅选择观察意图，与Interface的managed独立。 |
| `spec.required` | *选填* | boolean | 默认false，当前观察/生成路径不读取，不要求apply返回前完成SLAAC地址获取。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/render/freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `interface`, `address`
