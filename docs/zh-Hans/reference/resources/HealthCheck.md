# HealthCheck

[各资源的配置](../resources.md#healthcheck) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

探测 IP 目标并发布 Healthy/Unhealthy 状态供路由选择。成功仅代表配置的协议与路径通过。

## 指定条件与引用关系

- 运行时请明确指定 target。targetSource 与 interface 影响验证及计划显示，但目前生成器直接传入 target，不会自动解析默认网关或 DS-Lite 远端目标；辅助程序拒绝空目标。
- 来源绑定与 socket mark 从引用此检查的 EgressRoutePolicy 候选／目标推导，不能以 HealthCheck YAML 直接设置。推导值冲突时无法选出唯一路径。DS-Lite 关联探测需要可用的绑定。
- TCP 仅检查连接建立。DNS 通过目标服务器解析 example.com 的 A 记录。HTTP 对 http://<target>:<port>/ 发送 GET，将含 4xx 的 200–499 视为成功，不检查特定应用 URL 或响应内容。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: HealthCheck
  metadata:
    name: "<name>"
  spec:
    addressFamily: "ipv4" # 选填; string; enum=["ipv4", "ipv6"]
    enabled: true # 选填; boolean
    healthyThreshold: 1 # 选填; integer; minimum=1
    interface: "<string>" # 选填; string
    interval: "<string>" # 选填; string
    port: 0 # 选填; integer; minimum=0; maximum=65535
    protocol: "" # 选填; string; enum=["", "icmp", "tcp", "dns", "http"]
    role: "link" # 选填; string; enum=["link", "next-hop", "internet", "service", "policy"]
    target: "<string>" # 选填; string
    targetSource: "auto" # 选填; string; enum=["auto", "static", "defaultGateway", "dsliteRemote"]
    timeout: "<string>" # 选填; string
    type: "ping" # 选填; string; enum=["ping"]
    unhealthyThreshold: 1 # 选填; integer; minimum=1
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
| `spec.enabled` | 默认 true；false 禁用探测。 |
| `spec.type` | 默认 ping，仅接受 ping。TCP/DNS/HTTP 由 protocol 选择。 |
| `spec.role` | 可选分类 link/next-hop/internet/service/policy；验证与计划默认 next-hop。不会在运行时选择目标或协议。 |
| `spec.addressFamily` | 可选 ipv4/ipv6。目标验证将省略视为 ipv4，IPv6 目标须明确指定 ipv6。target 必须是 IP 字面值，不是主机名解析配置。 |
| `spec.target` | 符合 addressFamily 的纯 IP 地址。targetSource 为 static 时验证必填，目前辅助程序在所有模式下实际都需要目标；无可用运行时默认值。 |
| `spec.targetSource` | 验证默认 auto，也接受 static/defaultGateway/dsliteRemote。目前生成器不实现自动目标查询，请见上述运行限制。 |
| `spec.interface` | 可选的逻辑接口／PPPoESession／DSLiteTunnel 名称，指定时必须存在于共用接口／隧道索引。此字段本身不会转为目前辅助程序的 --source-interface。 |
| `spec.protocol` | 默认 icmp；tcp/dns/http 选择上述检查。HTTP 为明文，不是 HTTPS。 |
| `spec.port` | 0–65535，默认 0 使用协议默认：TCP 443、DNS 53、HTTP 80。ICMP 不使用端口。 |
| `spec.interval` | 正值 Go duration；辅助程序／运行时默认 30s。目前计划观察器省略时显示 60s，并非辅助程序默认。 |
| `spec.timeout` | 每次探测的正值 Go duration，默认 3s。这是探测超时，不是路由器重启期限。 |
| `spec.healthyThreshold` | 转为 Healthy 所需连续成功次数，默认 1。失败会重置成功计数。请使用正整数；明确的 0 不符合模式最小值。 |
| `spec.unhealthyThreshold` | 转为 Unhealthy 所需连续失败次数，默认 3。成功会重置失败计数；未达阈值时为 Failing。 |
| `spec.when` | 可选的通用启用条件，省略表示无条件。嵌套 state/all/any 及条件请见通用字段。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/healthcheck/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/healthcheck/engine.go) · [pkg/healthcheck/dslite_binding.go](https://github.com/imksoo/routerd/blob/main/pkg/healthcheck/dslite_binding.go) · [pkg/render/healthcheck_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/healthcheck_systemd.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [cmd/routerd-healthcheck/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-healthcheck/main.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `role`, `target`, `sourceAddress`, `sourceInterface`, `protocol`, `consecutiveFailed`, `lastCheckedAt`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
