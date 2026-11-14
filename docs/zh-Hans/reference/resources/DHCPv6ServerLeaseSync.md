# DHCPv6ServerLeaseSync

[各资源的配置](../resources.md#dhcpv6serverleasesync) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

以 SSH 上的 rsync 将 DHCPv6 服务器租约文件复制到对等主机，并非 DHCP 故障转移协议。

## 指定条件与引用关系

- source.resource 与至少一个 target.host 必填。不同于状态值引用，常规验证仅检查写法与类型，不验证 DHCPv6Server 是否存在；不存在的名称会回退至平台默认租约路径。
- 仅复制主租约文件；不同于 DHCPv4ServerLeaseSync，此路径不含 dhcp-sticky.db/WAL。目标端使用与本机来源相同的绝对路径，无目标路径覆盖或自动对端服务重新加载。
- 每次执行使用 rsync -a --checksum --delay-updates，默认 rsync 无活动限制 60 秒、命令执行限制 2 分钟。SSH 默认 BatchMode=yes、ConnectTimeout=10，可由 sshOptions 覆盖。Synced 仅表示文件复制成功，不证明对端重新加载租约或客户端连续性。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6ServerLeaseSync
  metadata:
    name: "<name>"
  spec:
    [ command: <string> ]
    [ interval: <string> ]
    source:
      resource: <string>
    targets:
      -
        host: <string>
        [ name: <string> ]
        [ options:
            - <string>
        ]
        [ sshOptions:
            - <string>
        ]
        [ user: <string> ]
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
| `spec.source` | **必填** | object | 必填含 resource 的来源描述，此处无文件路径覆盖。 |
| `spec.source.resource` | **必填** | string | 必填 DHCPv6Server 裸名或 DHCPv6Server/name。使用其非空 leaseFile，否则为 <StateDir>/dnsmasq/dnsmasq.leases。并非状态来源，无 field/optional。 |
| `spec.command` | *选填* | string | 可选可执行文件名或路径，默认 rsync；直接接收 rsync 参数，不是 shell 命令字符串。 |
| `spec.interval` | *选填* | string | Go duration，默认 30s。距上次成功 syncedAt 未达时长则跳过；控制器另以 30 秒周期及订阅事件执行。零或负值仅禁用跳过，不会加快周期调度。 |
| `spec.targets` | **必填** | array | 必填非空对等目标列表，逐一传送文件，第一个失败记录 Error/SyncFailed，不调用远端 routerd API。 |
| `spec.targets[].name` | *选填* | string | 可选状态标签，默认空，不用于选择目标主机或来源。 |
| `spec.targets[].host` | **必填** | string | 必填 SSH 主机或地址，不可含 CR/LF，须事先具备可达路由及非交互 SSH 认证。 |
| `spec.targets[].user` | *选填* | string | 可选 SSH 用户，空值省略 user@ 并由 SSH 配置决定，不可含 CR/LF。 |
| `spec.targets[].sshOptions` | *选填* | array | 可选 SSH 参数列表，组合至 rsync -e。未指定对应键时补上 -o BatchMode=yes 与 -o ConnectTimeout=10，条目不可含 CR/LF。 |
| `spec.targets[].options` | *选填* | array | 可选额外 rsync 参数，附于默认值之后。提供 --timeout 或 --timeout=... 时不补入 --timeout=60，但不改变独立的 2 分钟进程限制；不可含 CR/LF。 |
| `spec.when` | *选填* | object | 可选通用状态条件，默认无条件。应以实际主用角色状态避免备用端写入，本资源不自行协商租约文件所有者。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/controller/chain/filesync.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/filesync.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `command`, `sourceCount`, `targetCount`, `sources`, `targets`, `syncedAt`, `dryRun`
