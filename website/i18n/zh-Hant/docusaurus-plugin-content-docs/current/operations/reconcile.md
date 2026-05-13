---
title: Reconcile 與移除
---

# Reconcile 與移除

routerd 會比較 YAML 宣告的意圖與主機目前狀態。兩者不同時，routerd 會計算 plan，可先用 dry-run 預覽，再套用到主機。

## 標準流程

```bash
routerd validate --config router.yaml
routerd plan     --config router.yaml
routerd apply    --config router.yaml --once --dry-run
routerd apply    --config router.yaml --once
```

對遠端路由器執行非 dry-run `apply` 前，請先確認管理路徑（SSH、console、hypervisor console）不會被這次變更切斷。

## 常駐模式

```bash
routerd serve --config router.yaml
```

在 serve 模式中，routerd 會回應 bus 上的事件，只重新評估受影響的 resource。輸入包含 DHCPv6-PD renewal、health-check 結果、derived event，以及 inotify 偵測到的設定變更。

## Drift 檢查

routerd 不會把 status database 當成唯一真相。status store 會記錄前一次 apply 觀測到的內容，但 controller 在決定略過工作前，也會檢查自己負責的主機狀態。
例子包含 systemd unit 的 enabled/active 狀態、dnsmasq 是否使用預期的設定檔執行、DHCPv4 lease 位址是否仍在介面上，以及受管理的 nftables table 是否存在於主機上。

這在重開機、手動修改失敗，或 upgrade 中斷後特別重要：status database 可能仍顯示「Applied」，但 OS 狀態已經 drift。controller 應該把 OS 收斂回 YAML 宣告的狀態，而不是假設前一次 status row 仍然正確。

## 受管理項目的清理

當 resource 從 YAML 消失時，擁有它的 controller 只會移除或停用自己擁有的 artifact。沒有對應 `HealthCheck` resource 的舊 `routerd-healthcheck@*.service` unit 會被 disable 並移除。NAT44 沒有任何規則時，會清空受管理的 `routerd_nat` table 或 pf anchor。`state: absent` 的 `SystemdUnit` 會移除 render 出來的 unit；只有當 unit 存在或仍為 enabled/active 時才會停止它。

Firewall rendering 會保留受管理的 nftables table，並在單一 `nft -f` batch 中重新載入。對 firewall zone interface set 與 client-policy MAC set 這類 named set，routerd 會先 destroy 受管理的 set，再重新定義，避免已移除的元素殘留。正常 apply 不會 destroy 並重建整個 filter table。

## 移除

routerd 只會刪除能判定 ownership 的物件，也就是 routerd 先前建立或明確 adopt 的物件。它不會移除第三方設定或手動變更。

目前不支援完整回滾到先前設定。包含刪除的變更，請務必先執行 `routerd plan` 與 `routerd apply --dry-run`，確認刪除清單後再套用。
