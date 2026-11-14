# DHCPv4Client

[各资源的配置](../resources.md#dhcpv4client) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

运行受管routerd-dhcpv4-client daemon，应用租约地址，以及可选的默认路由与主机DNS配置。

## 指定条件与引用关系

- interface须存在于声明Router。标识字段作为协议选项发送，不是资源引用。租约到期/Release会移除已应用地址、路由及主机DNS；useRoutes/useDNS不停止租约获取。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Client
  metadata:
    name: "<name>"
  spec:
    [ classID: <string> ]
    [ clientID: <string> ]
    [ hostname: <string> ]
    interface: <string>
    [ requestedAddress: <string> ]
    [ routeMetric: <integer> ]
    [ useDNS: <boolean> ]
    [ useRoutes: <boolean> ]
    [ when:
        [ state:
            [ <name>:
                [ contains: <string> ]
                [ equals: <string> ]
                [ exists: <boolean> ]
                [ for: <string> ]
                [ in:
                    - <string>
                ]
                [ maxAge: <string> ]
                [ status: ("set" | "unset" | "unknown") ]
            ]
        ]
    ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.interface` | **必填** | string | 必填Router接口索引中的逻辑名称，不加Interface/，必须在声明配置存在，无默认值。 |
| `spec.hostname` | *选填* | string | DHCP选项12主机名，默认空即不发送，不自动使用OS主机名，拒绝空白字符。 |
| `spec.requestedAddress` | *选填* | string | 选填偏好IPv4地址，默认未指定，须为不含前缀长度的IPv4。以选项50请求，不保证分配，也不是静态配置。 |
| `spec.classID` | *选填* | string | 以选项60发送的vendor class标识字符串字节，默认空则省略，不做十六进制解码。 |
| `spec.clientID` | *选填* | string | 选项61客户端标识，显式值按字符串字节发送，不解码十六进制；默认为字节1加接口硬件地址。服务器以client ID标识租约时应保持稳定。 |
| `spec.useRoutes` | *选填* | boolean | 默认true，租约有defaultGateway时应用默认路由。false移除先前受管默认路由，但保留租约获取与地址应用，也不隐藏状态中的gateway。 |
| `spec.useDNS` | *选填* | boolean | 默认true；/etc/resolv.conf指向systemd-resolved时用resolvectl，否则写resolv.conf。false清除先前受管DNS，但状态DNS仍可供DNSResolver相关引用。 |
| `spec.routeMetric` | *选填* | integer; minimum=0 | 默认0，用于默认路由metric及平台特定地址协调。Schema要求非负整数，不是路由表ID。 |
| `spec.when` | *选填* | object | 启用条件，省略不设限制；state/all/any、unknown及时间条件请见共用条件参考。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/listen_ports.go](https://github.com/imksoo/routerd/blob/main/pkg/config/listen_ports.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/controller/dhcpv4client/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dhcpv4client/controller.go) · [pkg/dhcpv4client/message.go](https://github.com/imksoo/routerd/blob/main/pkg/dhcpv4client/message.go) · [cmd/routerd-dhcpv4-client/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dhcpv4-client/main.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `interface`, `device`, `currentAddress`, `gateway`, `defaultGateway`, `dnsServers`, `ntpServers`, `domain`, `leaseTime`, `renewAt`, `rebindAt`, `expiresAt`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)
