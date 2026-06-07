---
title: 狀態與擁有權
slug: /concepts/state-and-ownership
sidebar_position: 5
---

# 狀態與擁有權

routerd 將宣告的意圖與觀測到的狀態分開處理。
YAML 是使用者管理的意圖。
SQLite、租約檔案、events.jsonl 則是 routerd 及專屬常駐程式觀測到的狀態。

![lifecycle GC 圖：effective config、ownership ledger、object status 與 host inventory 輸入 GC planner 和 teardown registry](/img/diagrams/lifecycle-gc.png)

## 狀態的存放位置

正式版安裝時，設定的正本存放在 `/usr/local/etc/routerd/router.yaml`。
routerd 執行檔存放在 `/usr/local/sbin`。

Linux 上的狀態存放位置如下所示。

| 種類 | 範例 |
| --- | --- |
| routerd 狀態資料庫 | `/var/lib/routerd/routerd.db` |
| DHCPv6-PD 租約 | `/var/lib/routerd/dhcpv6-client/wan-pd/lease.json` |
| DHCPv4 租約 | `/var/lib/routerd/dhcpv4-client/wan/lease.json` |
| PPPoE 狀態 | `/var/lib/routerd/pppoe-client/<name>/state.json` |
| 健康檢查狀態 | `/var/lib/routerd/healthcheck/<name>/state.json` |
| 執行時 socket | `/run/routerd/.../*.sock` |

FreeBSD 上，設定與執行檔同樣存放在 `/usr/local` 下。
執行時 socket 存放在 `/var/run/routerd`。
持久狀態存放在 `/var/db/routerd`。

## 擁有權的概念

routerd 在主機端建立的設定物件，各自有其擁有來源資源。
例如 dnsmasq 設定由 DHCP 和 RA 各資源產生（render），`routerd-dns-resolver` 的設定由 `DNSResolver` 和 `DNSZone` 產生，nftables 的 NAT 表格由 `NAT44Rule` 產生。
從多個通道彙集而來的 TCP MSS clamp 表格則由最上層的 `Router` 擁有。

明確掌握擁有來源後，可以判斷以下事項：

- 這個設定物件是否允許 routerd 修改。
- 從 YAML 中刪除資源時，是否也可以刪除主機端的對應設定。
- 只是納管既有設定，還是由 routerd 全新建立。

owner key 是 `apiVersion/kind/name`；apply generation 不屬於該 identity。
resource status 包含 owner 與 lifecycle metadata，使 stale cleanup path 也能區分
routerd-managed resource 與 adopted/external object。

## lifecycle GC

routerd 儲存具體 host artifact 的 ownership ledger，以及 resource-specific teardown
所需的 object status。在 apply、serve startup 與 delete flow 中，generic GC planner
會將這些記錄與 apply 使用的同一份 effective config 比較。effective config 包含
`when` filtering 之後的 startup YAML、active dynamic config 與產生的 SAM resource。

GC plan 可以表示 owned artifact 刪除、resource teardown、ledger row forget、stale status
row 刪除、event 記錄，以及破壞性 cleanup 前所需的 state backup。不支援的 OS integration
會被跳過，adopted 或 externally managed status 會被保留。

resource 對應的 artifact map 與 teardown contract 請參閱
[資源擁有權](../resource-ownership.md)。

## 不使用過時狀態

租約和觀測值雖然方便，但持續使用過時的值是危險的。
特別是 DHCPv6-PD 的前綴，只有在確認為 Bound 狀態時才會向下游展開。
無法確認時，會停止套用 AAAA 記錄、RA、DHCPv6 伺服器和 LAN IPv6 位址。

## 事件

routerd 及專屬常駐程式會將狀態變化記錄為事件。
事件保存在 SQLite 的 `events` 表格，以及各常駐程式的 `events.jsonl` 中。
EventRule 和 DerivedEvent 會利用這些事件和狀態，產生虛擬的狀態變化。

## 套用世代

status 中顯示的 `generation` 是最後完成的套用世代編號。
此值在 `routerctl apply` 更新主機端意圖、並將套用完成記錄至 SQLite 時遞增。
這不是調和（reconcile）循環的執行次數。
dry-run 計畫、常駐程式事件、健康檢查、控制器鏈的定期調和均不會使其遞增。
新的套用世代會儲存當時套用的 YAML 快照。
Web 管理介面使用此快照，以唯讀方式顯示世代歷史記錄，以及世代間的差異（unified diff）。
在 YAML 儲存功能導入之前的記錄仍作為歷史保留，但無法作為差異顯示的對象。

## 有狀態封包過濾器

在 Linux 上，routerd 以一次 `nft -f` 交易更新 nftables 的管理表格。
產生（render）的規則集會在必要時建立管理表格。
之後在同一個 nftables 批次中清空表格，並載入新的鏈。
防火牆區域的介面 set 或 client-policy 的 MAC set 等由 routerd 擁有的具名 set，
在重新定義之前只會刪除受管理的 set。
這樣可以防止已刪除的 set 元素在重新載入後殘留。
不會將執行中的 NAT 表格或過濾器表格刪除後重建。
因此，即使 routerd 重新啟動或進行一般設定變更，現有的 conntrack 項目仍會保留在核心的狀態表格中。

在 FreeBSD 上，routerd 以 `pfctl -f` 載入產生的 pf 規則。
pf 在重新載入規則時，只要不明確清除狀態，就會保留現有的狀態表格。
routerd 的一般套用處理不會清除 pf 的狀態。
