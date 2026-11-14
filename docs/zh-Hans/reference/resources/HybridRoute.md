# HybridRoute

[各资源的配置](../resources.md#hybridroute) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

将明确列出的 IPv4 目标网络展开为经 OverlayPeer 的主路由表 IPv4Route。

## 指定条件与引用关系

- 仅支持非默认 IPv4 CIDR，拒绝 default、0.0.0.0/0、IPv6 及非主表。目标前缀掩码化后生成 <资源名>-<CIDR非字母数字连续字符改连字符>，例如 branch-10-20-0-0-16。
- 生成名称须唯一，不可与明确 IPv4Route 名称或目标冲突。同一 HybridRoute 内重复规范化目标也会冲突。HealthCheck 异常仅改变状态，不阻挡安装、撤回路由或改选对端。
- 生成 IPv4Route 使用 peer.underlay.interface 作 device，仅 ipsec 可将 IPv4 remote.address 作 gateway，其他为设备路由。须另行提供隧道/VPN 与实际可达性。Linux 使用正 metric，FreeBSD 底层 IPv4Route 不应用 metric。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: HybridRoute
  metadata:
    name: "<name>"
  spec:
    destinationCIDRs:
      - <string>
    [ healthCheckRef: <string> ]
    [ install:
        [ metric: <integer> ]
        [ table: ("" | "main") ]
    ]
    peerRef: <string>
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.destinationCIDRs` | **必填** | array | 必填非空且不含默认路由的 IPv4 CIDR 列表，不自动补远端网络或发现。 |
| `spec.peerRef` | **必填** | string | 必填现有 OverlayPeer metadata.name 裸名；展开虽可规范化 OverlayPeer/name，常规跨资源验证仍要求裸名。 |
| `spec.install` | *选填* | object | 可选路由安装配置，默认主表、metric 0，不提供策略路由或自动 HA 选择。 |
| `spec.install.table` | *选填* | string; enum=["", "main"] | 仅空值或 main，两者均展开为主表 IPv4Route，拒绝其他表名或编号。 |
| `spec.install.metric` | *选填* | integer; minimum=0 | 非负整数，默认 0，复制至生成 IPv4Route；Linux 仅输出正值，FreeBSD 路径忽略。 |
| `spec.healthCheckRef` | *选填* | string | 可选现有 HealthCheck 裸名，空值不附加健康状态。unhealthy/failed/error 标记 Degraded 但不更改路由，此函数不将缺少或 unknown 状态视为异常。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/hybrid/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/hybrid/hybrid.go) · [pkg/controller/chain/dynamic_effective.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/dynamic_effective.go) · [pkg/controller/chain/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/hybrid.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `routes`, `peerRef`, `defaultRouteUntouched`, `estimatedMTU`

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
