---
title: 套用、產生與調和
slug: /concepts/apply-and-render
sidebar_position: 4
---

# 套用、產生與調和

![同一份資源圖如何經過 routerd 驗證、dry-run、常駐服務與 routerctl 客戶端](/img/diagrams/concept-apply-and-render.png)

這些詞容易混在一起，實際上代表不同風險。先辨別它們，才不會把「檢查一份檔案」誤當成「正在修改路由器」。

| 操作 | 執行者 | 是否需要常駐 daemon | 是否會改變主機網路 |
| --- | --- | --- | --- |
| `routerd validate --config FILE` | 本機 `routerd` | 不需要 | 不會 |
| `routerd apply --once --dry-run …` | 本機 `routerd` | 不需要 | 不會提交網路變更 |
| `routerd apply --once …` | 本機 `routerd` | 不需要 | 會，可能改變網路 |
| `routerd serve --config FILE` | 常駐 `routerd` | 它本身就是 daemon | 會，可能改變網路 |
| `routerctl …` | 客戶端 | 需要 `routerd serve` | 由 daemon 執行請求 |

## 驗證：先看檔案是否合法

`routerd validate` 會讀取 YAML，檢查資源種類、必要欄位、值的格式與明顯的參照錯誤。這是服務啟動前應該使用的命令。

```bash
routerd validate --config first-router.yaml
```

它不會檢查真實網線、上游 ISP 或封包是否可達；它回答的是「這份 routerd 設定是否有效」。

## Dry-run：把一次套用限制在暫存位置

dry-run 會計算資源順序、衍生項目和產生意圖，但不提交網路變更。請替它建立新的暫存目錄，避免報告或輸出落到系統預設位置：

```bash
workdir=$(mktemp -d)
routerd apply --once --dry-run --skip-service-manager --config first-router.yaml --status-file "$workdir/status.json" --state-file "$workdir/state.db" --ledger-file "$workdir/ledger.db" --netplan-file "$workdir/50-routerd.yaml" --dnsmasq-file "$workdir/dnsmasq.conf" --dnsmasq-service-file "$workdir/routerd-dnsmasq.service" --nftables-file "$workdir/routerd-nat.nft"
```

`--state-file`、`--ledger-file`、`--status-file` 和各個產生檔參數都指向暫存路徑。dry-run 仍可能讀取主機狀態來形成計畫，因此它不是完全脫離主機的模擬器。

## 套用與 serve：真的會影響主機

不帶 `--dry-run` 的 `routerd apply --once` 會做一次真實套用；`routerd serve` 則持續執行、維護 Unix socket，並在需要時調和資源。兩者都可能改變位址、路由、服務和 nftables 規則。

只有在已準備主控台或獨立管理網卡、確認管理連線不會被移除後，才執行真實套用。一般 Ubuntu 安裝由 systemd 啟動 `routerd serve`。

## routerctl：對執行中的 routerd 提出要求

`routerctl` 不會直接在離線狀態處理候選 YAML，而是將請求交給運行中的 `routerd serve`：

```bash
sudo routerctl get status
sudo routerctl describe Interface/lan
sudo routerctl validate -f candidate.yaml --replace
sudo routerctl plan -f candidate.yaml --replace
```

前兩個指令讀取執行期狀態；後兩個讓 daemon 驗證或計畫候選設定。若 socket 無法連線，先啟動或檢查 `routerd serve`，不要誤認成 YAML 驗證失敗。

`routerctl apply -f candidate.yaml --replace` 也只適用於運行中的服務：它要求 daemon 寫入候選設定並調和。它不是比 `routerd apply --once` 更安全的離線替代品。

## 產生與調和

**產生（render）** 是把資源轉換為主機使用的內容，例如 dnsmasq 設定、nftables 規則集或 systemd 單元。是否真的修改主機，取決於目前是 validate、dry-run、真實 apply 還是 serve。

**調和（reconcile）** 是 serve 模式持續縮小「YAML 期望的狀態」與「主機目前狀態」差距的處理。例如 DHCPv6-PD 更新前綴後，相關 LAN 位址、RA 和路由會再次被評估。

:::caution 防火牆邊界
routerd 的防火牆資源仍在預發布的基礎實作階段，並非通用規則語言或安全認證。dry-run 和產生成功也不能取代對真實介面、VLAN、公開服務和回程路徑的安全審查。
:::
