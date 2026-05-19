---
title: Reconcile 与移除
---

# Reconcile 与移除

routerd 会比较 YAML 声明的意图与主机当前状态。两者不一致时，routerd 会计算 plan，可以先用 dry-run 预览，再应用到主机。

## 标准流程

```bash
routerd validate --config router.yaml
routerd plan     --config router.yaml
routerd apply    --config router.yaml --once --dry-run
routerd apply    --config router.yaml --once
```

对远程路由器执行非 dry-run `apply` 前，请先确认管理路径（SSH、console、hypervisor console）不会被这次变更切断。

## 常驻模式

```bash
routerd serve --config router.yaml
```

在 serve 模式中，routerd 会响应 bus 上的事件，只重新评估受影响的 resource。输入包括 DHCPv6-PD renewal、health-check 结果、derived event，以及 inotify 检测到的配置变更。

## Drift 检查

routerd 不会把 status database 当成唯一真相。status store 会记录前一次 apply 观测到的内容，但 controller 在决定跳过工作前，也会检查自己负责的主机状态。
例子包括 systemd unit 的 enabled/active 状态、dnsmasq 是否使用预期的配置文件运行、DHCPv4 lease 地址是否仍在接口上，以及受管理的 nftables table 是否存在于主机上。

这在重启、手动修改失败，或 upgrade 中断后特别重要：status database 可能仍显示 “Applied”，但 OS 状态已经 drift。controller 应该把 OS 收敛回 YAML 声明的状态，而不是假设前一次 status row 仍然正确。

## 受管理项的清理

当 resource 从 YAML 消失时，拥有它的 controller 只会移除或停用自己拥有的 artifact。没有对应 `HealthCheck` resource 的旧 `routerd-healthcheck@*.service` unit 会被 disable 并移除。NAT44 没有任何规则时，会清空受管理的 `routerd_nat` table 或 pf anchor。`state: absent` 的 `generated service artifacts` 会移除 render 出来的 unit；只有当 unit 存在或仍为 enabled/active 时才会停止它。

Firewall rendering 会保留受管理的 nftables table，并在单个 `nft -f` batch 中重新加载。对 firewall zone interface set 与 client-policy MAC set 这类 named set，routerd 会先 destroy 受管理的 set，再重新定义，避免已移除的元素残留。正常 apply 不会 destroy 并重建整个 filter table。

## 移除

routerd 只会删除能判定 ownership 的对象，也就是 routerd 先前创建或明确 adopt 的对象。它不会移除第三方配置或手动变更。

目前不支持完整回滚到先前配置。包含删除的变更，请务必先执行 `routerd plan` 与 `routerd apply --dry-run`，确认删除清单后再应用。
