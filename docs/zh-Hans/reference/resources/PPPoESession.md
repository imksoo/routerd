# PPPoESession

[各资源的配置](../resources.md#pppoesession) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

监督运行 routerd-pppoe-client，在 Linux 使用 pppd、FreeBSD 使用 mpd5，并公开协商的 IPv4 会话状态。部分旧字段仅影响手动 FreeBSD render 输出，不影响此守护进程路径。

## 指定条件与引用关系

- interface 必须引用同一 Router 的基本 Interface，不可使用 PPPoESession、隧道等作为底层接口。即使 enabled:false，仍需 username 及 password/passwordFile 其中一项。
- 实际监督流程传入 authMethod、mtu/mru、serviceName/acName、lcpEchoInterval/lcpEchoFailure，不传入 ifname、managed、persist、ipv6、usePeerDNS、lcpInterval/lcpFailure。请勿由另一条手动 FreeBSD render 路径推定实际行为。
- 主守护进程在两个平台默认 MTU/MRU 1454、LCP echo 30 秒/4 次。独立的手动 FreeBSD mpd5 生成路径默认 1492、旧 LCP 20 秒/3 次。配置计划或 Connected 不代表上游实际可达。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: PPPoESession
  metadata:
    name: "<name>"
  spec:
    acName: "<string>" # 选填; string
    authMethod: "chap" # 选填; string; enum=["chap", "pap", "both"]
    defaultRoute: true # 选填; boolean
    enabled: true # 选填; boolean
    ifname: "<string>" # 选填; string
    interface: "<string>" # 必填; string
    ipv6: true # 选填; boolean
    lcpEchoFailure: 0 # 选填; integer; minimum=0
    lcpEchoInterval: 0 # 选填; integer; minimum=0
    lcpFailure: 0 # 选填; integer; minimum=0
    lcpInterval: 0 # 选填; integer; minimum=0
    managed: true # 选填; boolean
    mru: 576 # 选填; integer; minimum=576; maximum=1500
    mtu: 576 # 选填; integer; minimum=576; maximum=1500
    password: "<string>" # 选填; string
    passwordFile: "<string>" # 选填; string
    persist: true # 选填; boolean
    secretEncoding: "plain" # 选填; string; enum=["plain"]
    serviceName: "<string>" # 选填; string
    usePeerDNS: true # 选填; boolean
    username: "<string>" # 必填; string
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.interface` | 必填的底层基本 Interface metadata.name，解析为物理 OS 名称，不是状态值来源。 |
| `spec.ifname` | 可选的 PPP 名称，最多 15 字符，不含空白或斜线；省略时 ppp-<name> 须在 15 字符内。目前主监督流程忽略此字段，由守护进程使用 DefaultIfName；手动 FreeBSD render 才使用此值。实际运行请勿依赖自定义名称。 |
| `spec.enabled` | 默认 true；false 不启动并停止受监督的会话，报告 Disabled，但不跳过配置验证。 |
| `spec.authMethod` | 默认 chap，也接受 pap/both，控制实际 pppd/mpd5 配置中允许的 PAP/CHAP 认证。 |
| `spec.username` | 必填的 PPP 认证用户名，无派生默认值。 |
| `spec.password` | 直接指定明文 PPP 密码，未提供 passwordFile 时必填，与 passwordFile 互斥。 |
| `spec.passwordFile` | 路由器本机原始密码文件，未提供 password 时必填。守护进程启动时读取并移除首尾空白；缺少、无法读取或为空会失败，不是环境变量赋值文件。 |
| `spec.mtu` | 可选 576–1500。省略或 0 时主守护进程为 1454，手动 FreeBSD render 为 1492。PPP MTU 1500 需底层支持，验证不保证可用。 |
| `spec.mru` | 可选接收上限 576–1500；省略或 0 时主守护进程为 1454，手动 FreeBSD render 为 1492。 |
| `spec.serviceName` | 可选 PPPoE 服务选择名称，空值不明确指定，不接受换行。 |
| `spec.acName` | 可选的接入集中器选择名称，空值不明确指定，不接受换行。 |
| `spec.defaultRoute` | 默认 false。true 时主路由清理保留观察到的 PPP 设备已有默认路由，但守护进程配置不创建该路由。手动 FreeBSD render 会输出默认路由指令。实际路径若需自动安装，请明确配置 IPv4Route。 |
| `spec.usePeerDNS` | 默认 false，但主守护进程路径不读取。LinuxPeer 一律输出 usepeerdns，协商 DNS 会公开于 PPP 状态；此字段不控制 routerd DNS 转发或 resolv.conf 应用。 |
| `spec.persist` | 可选布尔值，目前受监督守护进程配置不使用；省略不会建立此资源的重试策略，监督生命周期另行处理。 |
| `spec.lcpInterval` | 仅手动 FreeBSD render 使用的旧式非负间隔，省略或 0 为 20 秒。主守护进程忽略，请用 lcpEchoInterval。 |
| `spec.lcpFailure` | 仅手动 FreeBSD render 使用的旧式非负失败倍数，省略或 0 为 3。主守护进程忽略，请用 lcpEchoFailure。 |
| `spec.lcpEchoInterval` | 非负的 LCP echo 间隔秒数，省略或 0 使用主守护进程默认 30 秒，不表示禁用 echo。手动 FreeBSD render 忽略。 |
| `spec.lcpEchoFailure` | 非负的 LCP echo 失败次数，省略或 0 使用主默认 4。Linux 传入 lcp-echo-failure，FreeBSD 守护进程生成 keep-alive 间隔及间隔×次数。 |
| `spec.ipv6` | 默认 false，目前不传入受监督 PPP 守护进程，其生成配置不输出 IPv6 协商选项，请勿依赖此标志启用 PPP IPv6。 |
| `spec.managed` | 默认 false，但主监督流程不论此值都启动 enabled 会话。仅手动 FreeBSD render 中，true 才将启用的会话加入 mpd5 默认 load 列表。不是实际路径的只读保护。 |
| `spec.secretEncoding` | 仅接受省略或 plain，不会对 password/passwordFile 执行 Base64 解码或加密。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase_status.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase_status.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go) · [pkg/controller/pppoesession/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/pppoesession/controller.go) · [cmd/routerd-pppoe-client/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-pppoe-client/main.go) · [pkg/pppoeclient/client.go](https://github.com/imksoo/routerd/blob/main/pkg/pppoeclient/client.go) · [pkg/render/freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd.go) · [cmd/routerd/cmd_render.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_render.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `interface`, `device`, `currentAddress`, `peerAddress`, `gateway`, `dnsServers`, `connectedAt`, `dryRun`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-pppoe-ipv4-nat.yaml)
