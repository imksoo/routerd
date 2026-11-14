# NAT44SessionSync

[各资源的配置](../resources.md#nat44sessionsync) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

使用 SSH 事件流或持续运行的 conntrackd FTFW 对等链路复制 Linux conntrack 状态，不是 FreeBSD PF 状态同步。

## 指定条件与引用关系

- mode 默认 event-stream。两种模式的 YAML 验证均需要非空 snatAddresses 或 natRules。event-stream 另需 targets；conntrackd 需 conntrackd.interface/localAddress/peerAddress 且 when 必须空。
- event-stream 先协调快照，再消费 conntrack -E extended 事件，通常最多 5 秒或 512 操作一批，经 SSH 应用新增/删除命令。支持 IPv4 TCP/UDP/ICMP；缺少状态的 TCP 新增跳过。不复制完整 TCP 窗口/序号跟踪，并非完整已有 TCP 故障切换机制。
- conntrackd 模式两端持续复制，停止旧 SSH 工作。snatAddresses/natRules/excludeNatRules 虽验证，但不用于 conntrackd 筛选；生成器允许 TCP/UDP/ICMP，排除回环与 ignoreIPv4。targets/conntrackCommand/interval 不控制 conntrackd。
- Linux conntrackd 验证另需明确、非 optional 的 Sysctl net.netfilter.nf_conntrack_tcp_be_liberal，value=1、有效 expectedValue=1 且 runtime 启用。仅有 SysctlProfile 不足。通信/ignoreIPv4 使用纯 IPv4；不自动发现对等节点或分发密钥。
- 请仅使用一个 conntrackd 资源，生成的实例共享 /run/routerd/conntrackd.ctl 与锁路径。当前 routerd-vrrp-vmac 升主集成明确使用 /etc/conntrackd/routerd-dslite-sessions.conf，因此预期 metadata.name=dslite-sessions；任意名称不会自动集成其缓存。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44SessionSync
  metadata:
    name: "<name>"
  spec:
    [ conntrackCommand: <string> ]
    [ conntrackd:
        [ ignoreIPv4:
            - <string>
        ]
        [ interface: <string> ]
        [ localAddress: <string> ]
        [ peerAddress: <string> ]
        [ port: <integer> ]
    ]
    [ excludeNatRules:
        - <string>
    ]
    [ interval: <string> ]
    [ mode: ("event-stream" | "conntrackd") ]
    [ natRules:
        - <string>
    ]
    [ snatAddresses:
        - <string>
    ]
    [ targets:
        -
          host: <string>
          [ name: <string> ]
          [ restoreCommand:
              - <string>
          ]
          [ sshOptions:
              - <string>
          ]
          [ user: <string> ]
    ]
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
| `spec.mode` | *选填* | string; enum=["event-stream", "conntrackd"] | event-stream（默认）或 conntrackd。前者通过 SSH 转发所选组合；后者在 Linux systemd 运行原生 FTFW 复制。 |
| `spec.interval` | *选填* | string | 不支持轮询间隔。event-stream 拒绝非空值，conntrackd 接受但不使用。 |
| `spec.conntrackCommand` | *选填* | string | event-stream 可执行文件名称/路径，默认 conntrack。视为单一可执行文件，不是 shell 命令行；conntrackd 忽略。 |
| `spec.snatAddresses` | **条件必填** | array | 选择 event-stream 组合的纯 IPv4 列表，默认空。与 natRules 解析地址合并、去重排序。此列表或 natRules 至少一项必填；conntrackd 不作复制筛选。 |
| `spec.natRules` | **条件必填** | array | 可选 NAT44Rule/name 或单独规则名，仅验证格式不验证存在。event-stream 先读声明 snatAddress，再读 status.snatAddress；任一未排除规则未解析会使工作 Pending。不解析 translation.address/pool。 |
| `spec.excludeNatRules` | *选填* | array | 可选 NAT44Rule/name 或单独名称，在解析 natRules 时跳过。默认空，不扣除明确 snatAddresses，也不筛选 conntrackd。 |
| `spec.targets` | **条件必填** | array | event-stream 必填非空列表，每个目标经 SSH 接收恢复操作。conntrackd 不使用。 |
| `spec.targets[].name` | *选填* | string | 可选描述/状态名称，默认空；实际目标由 host 决定。 |
| `spec.targets[].host` | **条件必填** | string | 必填非空 SSH 主机或 SSH 别名。远端用户须有执行 conntrack 恢复的权限，不自动配置凭证。 |
| `spec.targets[].user` | *选填* | string | 可选远端 SSH 用户，默认空交由 SSH 配置；非空生成 user@host。 |
| `spec.targets[].sshOptions` | *选填* | array | 可选 SSH 参数列表，未明确覆盖时加入 BatchMode=yes 与 ConnectTimeout=10。每项为单一参数，不可含换行。命令操作另有实现的 2m 超时。 |
| `spec.targets[].restoreCommand` | *选填* | array | 可选远端 conntrack 命令 argv，默认 [conntrack]。可用独立参数提供权限包装，如 [sudo, -n, conntrack]。不是定期完整状态传输命令。 |
| `spec.conntrackd` | **条件必填** | object | conntrackd 模式必填对象，event-stream 不使用。两个 HA 节点均须持续运行并允许复制链路。 |
| `spec.conntrackd.interface` | **条件必填** | string | 必填复制接口。有配置逻辑别名时解析，否则视为内核名称。此处无普通接口资源存在检查；生成器拒绝空白。 |
| `spec.conntrackd.localAddress` | **条件必填** | string | 必填纯 IPv4 本机复制地址，须已配置于主机；不会分配地址。 |
| `spec.conntrackd.peerAddress` | **条件必填** | string | 必填纯 IPv4 远端复制目标，不配置发现或组播成员。 |
| `spec.conntrackd.port` | *选填* | integer; minimum=1; maximum=65535 | UDP 复制端口，省略/0 在生成时为 3780，正值 1–65535。Go 运行接受 0 作默认，但 JSON Schema 最小值为 1。 |
| `spec.conntrackd.ignoreIPv4` | *选填* | array | 可选纯 IPv4 排除列表，YAML 默认空。生成器固定加入 127.0.0.1 并排除 IPv6 ::1，IPv4 去重排序，生成时拒绝无效值。 |
| `spec.when` | *选填* | object | event-stream 的可选通用启用条件，默认无条件，可控制 SSH 工作。conntrackd 必须完全空白，让备用端也持续复制。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/nat44sessionsync.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/nat44sessionsync.go) · [pkg/controller/chain/filesync.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/filesync.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/render/conntrackd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/conntrackd.go) · [cmd/routerd-vrrp-vmac/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-vrrp-vmac/main.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `mode`, `streamState`, `sessionCount`, `remoteSessionCount`, `targetCount`, `snatAddresses`, `targets`, `deleteOK`, `deleteMissing`, `deleteFailed`, `pruneOK`, `pruneMissing`, `pruneFailed`, `pruneCandidateCount`, `insertOK`, `insertExisting`, `insertFailed`, `queuedEventCount`, `lastBatchEvents`, `resyncCount`, `syncedAt`, `lastEventAt`, `lastBatchAt`, `lastResyncAt`, `dryRun`
