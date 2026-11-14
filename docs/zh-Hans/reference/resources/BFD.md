# BFD

[各资源的配置](../resources.md#bfd) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

通过 vtysh 配置及观察 FRR bfdd 会话，供 BGPPeer 故障处理使用。与 GoBGP 为不同后端，BFD Down 可触发 BGP 对等端重置。

## 指定条件与引用关系

- 每个已声明 BFD 至少须由一个 BGPPeer.bfd 引用，BFD peer 必须符合该 BGPPeer 名称或其对等端地址。BGPDynamicPeer 没有 bfd 字段。
- 支持 Linux/FreeBSD，需有 FRR bfdd/vtysh。无接口时为多跳，使用引用 BGPRouter.routerID 作为本机地址；直接 IP 对等端且无接口时要求恰有一个 BGPRouter。指定接口则为单跳。
- BFD 仅检测自身会话故障，Up 不代表所有路由目标可达。GoBGP 在先前 BFD Up 或 BGP 已建立后观察到 Down 才处理，不将所有未知或启动状态视为 Down。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BFD
  metadata:
    name: "<name>"
  spec:
    detectMultiplier: 1 # 选填; integer; minimum=1; maximum=50
    interface: "<string>" # 选填; string
    minRx: "<string>" # 选填; string
    minTx: "<string>" # 选填; string
    peer: "<string>" # 必填; string
    profile: "" # 选填; string; enum=["", "fast", "normal", "slow"]
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
| `spec.peer` | 必填纯 IP 地址或 BGPPeer/name 引用。BGPPeer 引用展开为其所有对等端地址，并从 routerRef 的 routerID 取得本机地址。直接对等端不接受主机名。 |
| `spec.interface` | 可选单跳接口。建议 Interface/name，控制器会解析声明的 Interface.ifname；单独名称虽验证为逻辑接口，却会原样传给 FRR。省略使用多跳。 |
| `spec.profile` | fast 或省略：minRx=minTx=300ms、倍数 3；normal：500ms/500ms/3；slow：1000ms/1000ms/5。明确单独配置优先。 |
| `spec.minRx` | 可选 Go duration 50ms–60000ms，覆盖配置的接收间隔并转为整数毫秒。省略使用 profile。这是 BFD 协议计时器，不是路由器重启期限。 |
| `spec.minTx` | 可选 Go duration 50ms–60000ms，覆盖 profile 发送间隔，输出为整数毫秒。 |
| `spec.detectMultiplier` | 可选整数 1–50；解码为 0 或省略时使用 profile 默认值（3，slow 为 5）。为协商检测间隔倍数，不是 routerd 协调尝试次数。 |
| `spec.when` | 可选的通用启用条件，省略表示无条件。state/all/any、引用验证与模式限制请见通用字段。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/bfd/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bfd/controller.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
