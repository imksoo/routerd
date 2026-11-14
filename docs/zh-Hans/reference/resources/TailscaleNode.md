# TailscaleNode

[各资源的配置](../resources.md#tailscalenode) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

通过 routerd 生成的 Linux systemd oneshot 或 FreeBSD rc.d 脚本管理 Tailscale 注册选项，再读取 tailscale status --json。Tailscale/Headscale 服务端批准仍需另行处理。

## 指定条件与引用关系

- authKey 与 authKeyEnv、authKeyFile 互斥。authKeyEnv 可与 authKeyFile 搭配，指定从文件读取的变量名称；仅提供文件时使用 TS_AUTHKEY。
- authKeyFile 是环境变量赋值文件，不是原始密钥，使用 TS_AUTHKEY=tskey-... 或 authKeyEnv 指定的变量。FreeBSD 每行使用一个不加引号的赋值；选定变量缺失、重复或为空会失败。Linux 以 systemd EnvironmentFile 加载。
- present 节点会保留 UDP 41641，拒绝与其他已配置监听冲突。生成的命令为 tailscale up --timeout=30s。Applied/Rendered 表示服务应用，只有观察到 BackendState=Running 才报告 Running，均不保证通告子网可达。
- FreeBSD rc.d 拒绝更改缺少 routerd 管理标记的已有 tailscaled 进程。Linux 使用共享 tailscaled.service 与 routerd-tailscale-<name>.service 注册单元。state: absent 移除注册单元，不代表卸载软件包或删除 tailnet 节点。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: TailscaleNode
  metadata:
    name: "<name>"
  spec:
    [ acceptDNS: <boolean> ]
    [ acceptRoutes: <boolean> ]
    [ advertiseExitNode: <boolean> ]
    [ advertiseRoutes:
        - <string>
    ]
    [ advertiseTags:
        - <string>
    ]
    [ authKey: <string> ]
    [ authKeyEnv: <string> ]
    [ authKeyFile: <string> ]
    [ hostname: <string> ]
    [ loginServer: <string> ]
    [ shieldsUp: <boolean> ]
    [ ssh: <boolean> ]
    [ state: ("" | "present" | "absent") ]
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
| `spec.state` | *选填* | string; enum=["", "present", "absent"] | 默认 present；absent 移除管理的注册服务，不移除软件包或删除控制服务器上的节点。 |
| `spec.hostname` | *选填* | string | 可选的 tailscale up --hostname 值，不是 Router.metadata.name，也不更改 OS 主机名。省略时不传入参数。 |
| `spec.loginServer` | *选填* | string | 可选的 --login-server 控制服务器 URL，例如 Headscale。省略时依 Tailscale 客户端配置或默认值；routerd 不提供 URL 默认值或资源查找。 |
| `spec.authKey` | *选填* | string | 可选的直接注册密钥，与 authKeyEnv/authKeyFile 互斥。若未配置其他来源则不传入密钥参数，依已有登录状态或客户端注册要求处理。 |
| `spec.authKeyEnv` | *选填* | string | 可选的注册密钥环境变量名称，不是密钥本身。可指定 authKeyFile 中的变量。Linux 使用生成服务的环境，不会自动复制 routerd 进程环境。 |
| `spec.authKeyFile` | *选填* | string | 可选的路由器本机环境文件绝对路径，默认读取 TS_AUTHKEY。跨 Linux/FreeBSD 请用不加引号的 NAME=value 格式。不是 Secret 资源引用。 |
| `spec.advertiseExitNode` | *选填* | boolean | 默认 false；true 传入 --advertise-exit-node。不会在控制服务器批准出口节点，也不会让本机改用其他出口节点。 |
| `spec.advertiseRoutes` | *选填* | array | 可选 IPv4/IPv6 CIDR 列表，以逗号分隔传入 --advertise-routes。空列表省略参数，而非明确传入空值。控制服务器所需批准另行处理。 |
| `spec.advertiseTags` | *选填* | array | 可选标签字符串，以逗号分隔传入 --advertise-tags。验证拒绝空值及空白字符，但不强制 tag: 前缀或查验服务器 ACL 权限。空列表省略参数。 |
| `spec.acceptRoutes` | *选填* | boolean | 可选的 --accept-routes 布尔值。省略时不传入参数，由 Tailscale 客户端决定；明确 false 仍会传入，与省略不同。 |
| `spec.acceptDNS` | *选填* | boolean | 可选的 --accept-dns 布尔值，省略时不强制 true 或 false，仅传入明确值。不会配置 routerd DNSResolver/DNSForwarder 资源。 |
| `spec.ssh` | *选填* | boolean | 默认 false 省略参数；true 传入 --ssh 启用 Tailscale SSH 请求。与 OS sshd 及 routerd 管理访问不同。 |
| `spec.shieldsUp` | *选填* | boolean | 可选的 --shields-up 布尔值，省略不传入参数，明确 true/false 才传入。 |
| `spec.when` | *选填* | object | 可选的通用启用条件，省略表示无条件。state/all/any 及字段引用规则请见通用字段。控制生成服务，不会删除 tailnet 节点。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/tailscale_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/tailscale_systemd.go) · [pkg/render/freebsd_rcd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd_rcd.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/tailscale/tailscale.go](https://github.com/imksoo/routerd/blob/main/pkg/tailscale/tailscale.go) · [pkg/render/firewall_holes.go](https://github.com/imksoo/routerd/blob/main/pkg/render/firewall_holes.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `tailnetName`, `peerCount`, `advertiseRoutes`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/tailscale-exit-subnet.yaml)
