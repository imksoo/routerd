---
title: 调和（reconcile）与删除
---

# 调和（reconcile）与删除

routerd 会比较 YAML 所声明的意图与主机的当前状态。
若有差异，则生成计划（plan），必要时可先通过 dry-run 确认后再应用。

## 标准流程

```bash
routerd validate --config router.yaml
routerd plan     --config router.yaml
routerd apply    --config router.yaml --once --dry-run
routerd apply    --config router.yaml --once
```

对远程路由器执行正式 `apply` 前，请先确认管理路径（SSH、控制台、hypervisor 控制台）在变更后仍能保持连接。

## 守护进程模式

```bash
routerd serve --config router.yaml
```

在 serve 模式下，routerd 会响应总线上的事件，只重新评估受影响范围的资源。
输入来源包含 DHCPv6-PD 租约更新、健康检查结果、衍生事件，以及 inotify 检测到的配置变更。

控制器的 dry-run 标志依拥有范围单独生效。
`--controller runtime-dry-run-ingress=false` 表示由 IngressService 控制器实际执行健康状态的选择，
以及 IngressService 所衍生的 nftables DNAT/hairpin 规则的实际应用。
独立的 `NAT44Rule` 与 `LocalServiceRedirect` 则继续通过
`--controller runtime-dry-run-nat=false` 单独控制。

当配置中存在 `IngressService`、`PortForward`、NAT、BGP、静态路由或策略路由等需要转发的资源时，
routerd 会自动推导所需的运行时 sysctl。
`routerd apply --once` 会观测、计划并生成（render）衍生配置，但不会反映到主机。
反映动作由 `routerd serve` 在控制器调和（reconcile）过程中逐步收敛完成。
因此，一次性的 apply 仅用于配置验证与产物生成，
守护进程与运行时内核的生命周期则由长时间运行的控制器所拥有。

## drift 确认

routerd 不以状态数据库作为唯一的事实依据。
状态存储记录的是前次 apply 时的观测内容，但各控制器
在决定是否跳过处理前，也会确认自己所管理的实际主机状态。
例如，systemd unit 的 enabled/active 状态、dnsmasq 是否以预期的配置文件运行、
DHCPv4 租约地址是否仍存在于接口上，以及受管理的 nftables 表是否存在于主机上。

这在重新启动后、手动变更失败后，或升级中途中断后尤为重要。
即使状态数据库显示为 Applied，OS 侧的状态可能已产生偏移。
控制器不应直接信任前次的 status 记录，而应将 OS 状态收敛至 YAML 所声明的内容。

## 衍生资源

部分主机对象不直接在 YAML 中编写，而是从较高层次的意图自动生成。
例如 `routerd.service`、`routerd-healthcheck@*.service`、防火墙日志守护进程、
DPI 辅助服务都是衍生的服务 unit。生成的资源可通过以下命令确认。

```bash
routerctl show derived-resources
```

默认只显示从当前配置衍生的资源。
不来自当前配置的旧 status 记录会隐藏，以避免看起来像是仍在运行的资源。
清理旧状态数据库时，可加上 `--include-stale` 查看。

若 YAML 中残留已删除或不支持的资源 Kind，routerd 不会静默忽略，
而是直接让配置读取失败。

## 受管理项目的清理

当资源从 YAML 移除时，拥有该资源的控制器只会删除或停用自己所拥有的产物。
已无对应 `HealthCheck` 的 `routerd-healthcheck@*.service` 会被停用并删除。
NAT44 规则归零时，受管理的 `routerd_nat` 表或 pf anchor 会被清空。
`state: absent` 的 `generated service artifacts` 会删除已生成的 unit，
只在 unit 存在且仍处于 enabled/active 状态时才执行停止。

若旧 status 记录属于当前 schema 中不存在的资源 Kind，
可使用 `routerctl delete --force <kind>/<name>` 删除。
同一 kind/name 存在于多个 API 组时，请加上 `--api-version <version>`，
避免 routerd 误判删除目标。

防火墙生成时，会保留受管理的 nftables 表，并以单次 `nft -f` 批量重新加载。
防火墙 zone 的接口 set 与 client-policy 的 MAC set 等 named set，
routerd 会先删除受管理的 set 再重新定义，避免已移除的元素残留。
一般的 apply 不会删除并重建整个 filter 表。

## 删除

routerd 只删除可确认拥有权的产物（即 routerd 先前创建或明确接管的对象）。
不会触及第三方配置或手动变更。

支持基于世代的回滚：`routerctl rollback --list` 列出过去 apply 记录的世代，
`routerctl rollback --to <generation>` 通过正常的 apply 流程重新应用已保存的 Router YAML。
回滚会重新应用声明的配置与 routerd 管理的产物；但**不会**还原 conntrack、内核瞬时状态、
守护进程运行时状态，或在 routerd 账本之外对主机所做的任何变更。包含删除的变更，
请务必先执行 `routerd plan` 与 `routerd apply --dry-run` 确认删除清单后再应用。

## 相关项目

- [状态与拥有权](../concepts/state-and-ownership.md)
- [应用与生成（render）](../concepts/apply-and-render.md)
- [故障排查](../how-to/troubleshooting.md)
