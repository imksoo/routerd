---
title: Dynamic config
slug: /reference/dynamic-config
---

# Dynamic config

Dynamic config 是一种机制，允许受信任的本地源在不编辑 startup-config 的情况下提供
运行时 intent。routerd 从 startup YAML、活跃的 dynamic part 和活跃的 mask 中导出
一个 effective-config。effective-config 是唯一的 reconcile 对象。

本页说明面向 CloudEdge MVP 的 dynamic-config API 形状。plugin runner 可以将验证过的
plugin 输出保存为 `DynamicConfigPart` 记录。源自 `actionPlans` 的 provider action
在 dynamic config 中保持不活跃状态，不会合并到 effective config 中。独立的
provider-action 引擎仅从活跃的 part 导入，并仅在通过 `ProviderActionPolicy`、
审批和 executor-plugin 门控后才执行。

![展示 startup config、DynamicOverridePolicy、受信任的本地 plugin 输出、DynamicConfigPart、effective config、不活跃 actionPlans、action journal、带门控的 executor plugin 路径的 Dynamic config 示意图](/img/diagrams/dynamic-config-provider-actions.png)

## DynamicConfigPart

`DynamicConfigPart` 是来自 dynamic 源的已验证运行时片段。源可以提供常规的
`api.Resource` 对象和指令。

```yaml
apiVersion: config.routerd.net/v1alpha1
kind: DynamicConfigPart
metadata:
  name: oci-inventory
spec:
  source: Plugin/oci-inventory
  generation: 12
  observedAt: "2026-05-29T12:00:00Z"
  expiresAt: "2026-05-29T12:05:00Z"
  digest: sha256:...
  resources:
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: RemoteAddressClaim
      metadata: { name: app-10-0-1-123 }
      spec:
        domainRef: cloudedge-same-subnet
        address: 10.0.1.123/32
        ownerSide: cloud
        capture: { type: provider-secondary-ip, providerRef: oci-prod, providerMode: vnic-private-ip, nicRef: ocid1.vnic.oc1..example }
        delivery: { peerRef: cloud-main, mode: route, tunnelInterface: wg-hybrid }
  directives:
    - op: mask
      target: { apiVersion: net.routerd.net/v1alpha1, kind: IPv4Route, name: cloud-app-static-fallback }
      reason: "RemoteAddressClaim/app-10-0-1-123 is active"
```

| 字段 | 含义 |
| --- | --- |
| `spec.source` | 稳定的源标识符。例：`Plugin/oci-inventory`。 |
| `spec.generation` | 单调递增的源世代号。用于说明和排序。 |
| `spec.observedAt` | 源观测输入的 RFC3339 时间。 |
| `spec.expiresAt` | 此 part 变为不活跃的 RFC3339 时间。 |
| `spec.digest` | 已验证 part 载荷的摘要。 |
| `spec.resources` | 在活跃期间提供给 effective-config 的资源。 |
| `spec.directives` | 合并指令。当前仅支持 `op: mask`。 |
| `spec.actionPlans` | provider action 提案。不是资源。provider-action 引擎仅从活跃的 part 导入，并在执行前应用自身的门控。 |

plugin 通过 `PluginResult.status.ttl` 返回 TTL duration。routerd 将其相对于
`observedAt` 解析，得到保存的 `expiresAt`。

## DynamicConfigSource

`DynamicConfigSource` 是将一个 plugin 绑定到 dynamic config 生成的 startup-config
策略。

```yaml
apiVersion: plugin.routerd.net/v1alpha1
kind: DynamicConfigSource
metadata: { name: oci-inventory }
spec:
  pluginRef: oci-inventory
  ttl: 300s
  mergePolicy:
    conflict: reject
```

MVP 的合并策略仅支持 `conflict: reject`。

## DynamicConfigDirective

MVP 支持以下指令操作。

| 操作 | 含义 |
| --- | --- |
| `mask` | 当指令活跃时，抑制匹配的 startup-config 资源。 |

指令的目标通过 `apiVersion`、`kind`、`name` 标识。目标故意采用精确匹配。
通配符 mask 不在 MVP 范围内。

## DynamicOverridePolicy

`DynamicOverridePolicy` 授权源对选定的资源使用 dynamic 指令。plugin 可以提议 mask，
但 mask 仅在策略允许该源、操作和目标时才会生效。

```yaml
apiVersion: config.routerd.net/v1alpha1
kind: DynamicOverridePolicy
metadata: { name: allow-cloud-plugin-mask }
spec:
  allow:
    - source: Plugin/oci-inventory
      operations: [mask]
      targets:
        - { apiVersion: net.routerd.net/v1alpha1, kind: IPv4Route, name: cloud-app-static-fallback }
```

策略是 startup-config 的 intent。dynamic 源不能为自身授予覆盖权限。

## 合并算法

effective-config 的合并是确定性的。

1. 读取并验证 startup-config。
2. 从状态数据库读取已验证的 dynamic part。
3. 丢弃 `expiresAt` 在合并时间之前的 dynamic part。
4. 将活跃的 dynamic part 按 `source`、然后 `generation`、然后 `metadata.name`
   排序，实现稳定的渲染和诊断。
5. 将活跃的指令与 `DynamicOverridePolicy` 进行匹配评估。
6. 将被允许的活跃 mask 所针对的 startup 资源标记为已抑制。
7. 从未被抑制的 startup 资源和活跃的 dynamic 资源构建 effective-config。
8. 在 reconcile 或 dry-run 输出之前验证生成的 effective-config。

冲突规则：

- dynamic 资源不得替换具有相同 `apiVersion`、`kind`、`metadata.name` 的
  startup 资源。
- 具有相同 identity 的两个活跃 dynamic 资源会产生冲突，除非后续定义了
  源特定的所有权规则。
- 未被许可的指令在合并中被忽略，并作为验证或诊断发现报告。
- 过期的 dynamic part 不提供资源，也不提供 mask。

## mask 语义

mask 是抑制而非删除。startup YAML 不会被修改，git 历史仍由运维人员拥有，
当所有匹配的活跃 mask 过期或被移除时，静态资源将重新变为活跃。

被抑制的资源应显示如下状态。

```yaml
status:
  phase: Suppressed
  maskedBy:
    - Plugin/oci-inventory#12
  maskedUntil: "2026-05-29T12:05:00Z"
```

当多个 mask 针对同一资源时，资源将保持被抑制状态直到最后一个活跃 mask 过期。
`maskedBy` 列出所有活跃的源和世代号，`maskedUntil` 是活跃 mask 中最晚的 `expiresAt`。

MVP 的过期行为是 `onExpire=restoreStatic`。当 mask 过期时，routerd 会在下次合并时
将 startup-config 资源恢复到 effective-config 中。由于 startup 资源未被修改，
不需要破坏性的清理步骤。

## CLI

当前面向运维人员的接口如下。

```text
routerctl dynamic list
routerctl dynamic describe <source-or-part>
routerctl dynamic render
routerctl dynamic diff
routerctl plugin list
routerctl plugin run <name> [--dry-run]
```

`dynamic list` 显示活跃和过期的 part。`dynamic describe` 说明源、世代号、摘要、
资源、指令和有效期限。`dynamic render` 输出 effective-config。`dynamic diff` 比较
startup-config 和 effective-config。`plugin run --dry-run` 在不写入状态数据库的
情况下输出候选的 dynamic part。

参见[混合云边缘设计](../design-hybrid-cloud-edge)和
[Plugin protocol](/docs/reference/plugin-protocol)。
