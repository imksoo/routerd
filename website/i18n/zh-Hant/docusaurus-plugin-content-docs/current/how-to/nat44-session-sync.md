---
title: HA 路由器的 NAT44 工作階段同步
slug: /how-to/nat44-session-sync
---

# HA 路由器的 NAT44 工作階段同步

![NAT44SessionSync 從 active 路由器 dump conntrack SNAT 項目、透過 SSH 還原、將 insert 失敗輸出到 standby status 的流程](/img/diagrams/how-to-nat44-session-sync.png)

`NAT44SessionSync` 是一個資源，用於在共享 LAN 側閘道角色的 2 台
routerd 節點間，將 active 節點的 NAT44 conntrack 工作階段同步到 standby
節點。啟動時 routerd 會執行一次快照還原，隨後持續處理 conntrack
事件並向各 target 傳送增量更新。

通常透過 `spec.when` 確保僅 active 節點運行。在 VRRP 架構中，
以本機 `VirtualAddress` 的 role 作為條件是基本做法。

## 同步目標 NAT 規則

參照持有要同步的 SNAT 位址的 NAT 規則。動態 SNAT 位址從
`NAT44Rule` 的 status 讀取。因此，在 session sync 啟用之前，
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

如果位址是固定的，可以透過 `snatAddresses` 直接指定。

```yaml
spec:
  snatAddresses: [192.0.0.2, 192.0.0.3, 192.0.0.4]
```

## 還原機制

controller 執行以下操作：

```bash
conntrack --dump -o extended -n <snat-address>
```

`extended` 輸出包含 conntrack mark。routerd 將每行轉換為
delete-then-insert 還原指令碼，透過 SSH 傳送到 target。
維持 `ct mark` 對於將現有流保持在相同的出口路徑上至關重要。

`restoreCommand` 的預設值為 `[conntrack]`。如果 target user 需要
權限提升，請指定 `[sudo, conntrack]`。

## 確認

```bash
routerctl describe NAT44SessionSync/dslite-abc-sessions
routerd serve --controllers nat44-session-sync --config router.yaml
```

當 `spec.when` 為 false 時，狀態為 `Pending` / `WhenFalse`。當參照的
`NAT44Rule` 尚未解析 `snatAddress` 時，狀態為 `Pending` /
`SNATAddressPending`。
