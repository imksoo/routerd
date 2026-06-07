---
title: 調和（reconcile）與刪除
---

# 調和（reconcile）與刪除

![Diagram showing reconcile and removal from validate, plan, and dry-run preflight through effective desired view construction to owner-reference GC planner cleanup with backup and event recording](/img/diagrams/operations-reconcile.png)

routerd 會比較 YAML 所宣告的意圖與主機的現況。
若有差異，則建立計畫（plan），必要時可先透過 dry-run 確認後再套用。

## 標準流程

```bash
routerd validate --config router.yaml
routerd plan     --config router.yaml
routerd apply    --config router.yaml --once --dry-run
routerd apply    --config router.yaml --once
```

對遠端路由器執行正式 `apply` 前，請先確認管理路徑（SSH、主控台、hypervisor 主控台）在變更後仍能保持連線。

## 常駐模式

```bash
routerd serve --config router.yaml
```

在 serve 模式下，routerd 會回應匯流排上的事件，只重新評估受影響範圍的資源。
輸入來源包含 DHCPv6-PD 租約更新、健康檢查結果、衍生事件，以及 inotify 偵測到的設定變更。

控制器的 dry-run 旗標依擁有範圍個別生效。
`--controller runtime-dry-run-ingress=false` 表示由 IngressService 控制器實際執行健康狀態的選擇，
以及 IngressService 所衍生的 nftables DNAT/hairpin 規則的實際套用。
獨立的 `NAT44Rule` 與 `LocalServiceRedirect` 則繼續透過
`--controller runtime-dry-run-nat=false` 個別控制。

當設定中存在 `IngressService`、`PortForward`、NAT、BGP、靜態路由或策略路由等需要轉送的資源時，
routerd 會自動推導所需的執行時期 sysctl。
`routerd apply --once` 會觀測、計畫並產生（render）衍生設定，但不會反映到主機。
反映動作由 `routerd serve` 在控制器調和（reconcile）過程中逐步收斂完成。
因此，一次性的 apply 僅用於設定驗證與成果物產生，
常駐程式與執行時期核心的生命週期則由長時間運作的控制器所擁有。

## drift 確認

routerd 不以狀態資料庫作為唯一的事實依據。
狀態儲存記錄的是前次 apply 時的觀測內容，但各控制器
在決定是否略過處理前，也會確認自己所管理的實際主機狀態。
例如，systemd unit 的 enabled/active 狀態、dnsmasq 是否以預期的設定檔執行、
DHCPv4 租約位址是否仍存在於介面上，以及受管理的 nftables 資料表是否存在於主機上。

這在重新開機後、手動變更失敗後，或升級中途中斷後尤為重要。
即使狀態資料庫顯示為 Applied，OS 側的狀態可能已產生偏移。
控制器不應直接信任前次的 status 記錄，而應將 OS 狀態收斂至 YAML 所宣告的內容。

## 衍生資源

部分主機物件不直接在 YAML 中撰寫，而是從較高層次的意圖自動產生。
例如 `routerd.service`、`routerd-healthcheck@*.service`、防火牆日誌常駐程式、
DPI 輔助服務都是衍生的服務 unit。產生的資源可透過以下指令確認。

```bash
routerctl show derived-resources
```

預設只顯示從目前設定衍生的資源。
不來自目前設定的舊 status 記錄會隱藏，以避免看起來像是仍在運作的資源。
清理舊狀態資料庫時，可加上 `--include-stale` 查看。

若 YAML 中殘留已刪除或不支援的資源 Kind，routerd 不會靜默忽略，
而是直接讓設定讀取失敗。

## 受管理項目的清理

當資源從 YAML 移除時，擁有該資源的控制器只會刪除或停用自己所擁有的成果物。
已無對應 `HealthCheck` 的 `routerd-healthcheck@*.service` 會被停用並刪除。
NAT44 規則歸零時，受管理的 `routerd_nat` 資料表或 pf anchor 會被清空。
`state: absent` 的 `generated service artifacts` 會刪除已產生的 unit，
只在 unit 存在且仍處於 enabled/active 狀態時才執行停止。

若舊 status 記錄屬於目前 schema 中不存在的資源 Kind，
可使用 `routerctl delete --force <kind>/<name>` 刪除。
同一 kind/name 存在於多個 API 群組時，請加上 `--api-version <version>`，
避免 routerd 誤判刪除目標。

防火牆產生時，會保留受管理的 nftables 資料表，並以單次 `nft -f` 批次重新載入。
防火牆 zone 的介面 set 與 client-policy 的 MAC set 等 named set，
routerd 會先刪除受管理的 set 再重新定義，避免已移除的元素殘留。
一般的 apply 不會刪除並重建整個 filter 資料表。

## 刪除

routerd 只刪除可確認擁有權的成果物（即 routerd 先前建立或明確接管的物件）。
不會觸及第三方設定或手動變更。

支援以世代為單位的回滾：`routerctl rollback --list` 會列出過去 apply 記錄的世代，
`routerctl rollback --to <generation>` 透過正常的 apply 流程重新套用已儲存的 Router YAML。
回滾會重新套用宣告的設定與 routerd 管理的產物；但**不會**還原 conntrack、kernel 瞬時狀態、
守護程式執行時期狀態，或在 routerd 帳本之外對主機所做的任何變更。包含刪除的變更，
請務必先執行 `routerd plan` 與 `routerd apply --dry-run` 確認刪除清單後再套用。

## 相關項目

- [狀態與擁有權](../concepts/state-and-ownership.md)
- [套用與產生（render）](../concepts/apply-and-render.md)
- [疑難排解](../how-to/troubleshooting.md)
