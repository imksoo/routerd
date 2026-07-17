---
title: HA 路由器的 NAT44 会话同步
slug: /how-to/nat44-session-sync
---

# HA 路由器的 NAT44 会话同步

![NAT44SessionSync 从 active 路由器 dump conntrack SNAT 条目、通过 SSH 恢复、将 insert 失败输出到 standby status 的流程](/img/diagrams/how-to-nat44-session-sync.png)

`NAT44SessionSync` 是一个资源，用于在共享 LAN 侧网关角色的 2 台
routerd 节点间，将 active 节点的 NAT44 conntrack 会话同步到 standby
节点。启动时 routerd 会执行一次快照恢复，随后持续处理 conntrack
事件并向各 target 发送增量更新。

通常通过 `spec.when` 确保仅 active 节点运行。在 VRRP 构成中，
以本地 `VirtualAddress` 的 role 作为条件是基本做法。

## 同步目标 NAT 规则

引用持有要同步的 SNAT 地址的 NAT 规则。动态 SNAT 地址从
`NAT44Rule` 的 status 读取。因此，在 session sync 激活之前，
NAT44 controller 需要已解析 `snatAddress`。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44SessionSync
  metadata:
    name: dslite-abc-sessions
  spec:
    mode: event-stream
    natRules:
      - NAT44Rule/lan-to-dslite-a
      - NAT44Rule/lan-to-dslite-b
      - NAT44Rule/lan-to-dslite-c
    excludeNatRules:
      - NAT44Rule/lan-to-dslite-ra
    targets:
      - name: standby
        host: routerd-standby.lan.example
        user: routerd
        restoreCommand: [sudo, conntrack]
    when:
      state:
        VirtualAddress/lan-vip.role:
          equals: master
```

如果地址是固定的，可以通过 `snatAddresses` 直接指定。

```yaml
spec:
  snatAddresses: [192.0.0.2, 192.0.0.3, 192.0.0.4]
```

## 恢复机制

controller 执行以下操作：

```bash
conntrack --dump -o extended -n <snat-address>
```

`extended` 输出包含 conntrack mark。routerd 将每行转换为
delete-then-insert 恢复脚本，通过 SSH 发送到 target。
维持 `ct mark` 对于将现有流保持在相同的出口路径上至关重要。

`restoreCommand` 的默认值为 `[conntrack]`。如果 target user 需要
权限提升，请指定 `[sudo, conntrack]`。

## 确认

```bash
routerctl describe NAT44SessionSync/dslite-abc-sessions
routerd serve --controllers nat44-session-sync --config router.yaml
```

当 `spec.when` 为 false 时，状态为 `Pending` / `WhenFalse`。当引用的
`NAT44Rule` 尚未解析 `snatAddress` 时，状态为 `Pending` /
`SNATAddressPending`。
