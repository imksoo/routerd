# CloudEdge / SAM — 合併前盤點 (Azure x PVE + AWS x PVE + OCI x PVE 冒煙測試)

日期: 2026-05-29 (2026-05-30 以 OCI x PVE 更新) · 分支 `cloudedge-mvp` · 目的:
對 3 次乾淨冒煙測試期間觀察到的手動干預、設定人因工程、routerd 功能缺口進行
盤點。確定 experimental main 合併和後續跟進的範圍。

## 1. 冒煙測試期間的手動變通 — 全部已由 routerd 原生解決

| 變通方案 (當時為手動) | 解決方案 |
|---|---|
| Azure: 輔助 `/32` 被客體 OS 自動新增 (cloud-init/netplan) → `ip addr del` + suppress | **#41 / 439ec316** — provider-secondary-ip de-assign 強制 |
| Azure: `wg setconf <tempfile>` EACCES → `/dev/stdin` | **#43 / 439ec316** — WireGuard 的 stdin 方式套用 |
| Azure: 舊的 `routerd_filter` nft 表丟棄轉發 → 手動刪除 | **#42 / 439ec316** doctor 警告 + 文件; **#47 / f60e7d9a** nft 所有權診斷 |
| `routerctl describe` 無 `-o` → 純文字輸出 | **#45 / 40a99208** |
| AWS: 輔助 `.9` 一時出現在 OS 上 | **無手動步驟** — routerd de-assign (#41) 自動處理 (驗證修復跨 provider 通用化) |
| OCI: 低 PMTU underlay 下 TCP 黑洞 (ping OK, SSH/scp 逾時) | **#53 / 3c540656** — PMTU/MSS clamp 變為 FirewallZone 無關 + 類型無關; 為 SAM 轉發路徑匯出 `routerd_mss` (透過 `hybrid.EstimateMTU` 得到 MSS 1300)。#50 已預測。 |
| OCI: Ubuntu 映像預設 `iptables` reject-all FORWARD/INPUT 阻塞 WG/overlay 轉發 | **#52** — `doctor hybrid` 偵測 + 顯示所需主機規則; 主機防火牆由主機側處理 (routerd 不自動供應, 僅警告) |

→ 冒煙測試期間的 routerd 級修復現已全部由 routerd 自身處理。AWS 執行中無需任何修復。OCI 執行中發現了 #53 PMTU/MSS 缺口 (實際 bug, 已在 routerd 核心修復) 和 #52 主機防火牆前提條件 (設計上由主機側處理, doctor 偵測)。

## 2. 主機/雲端引導 — 手動 (部署缺口, 大部分不在 routerd 核心範圍內)

- routerd tarball 的建置/複製/安裝、systemd 單元的建立/啟用、即時設定的放置、
  validate/dry-run/apply 的執行 — 手動。未來: 實驗室引導指令碼 / 黃金映像;
  與發現已有的 OS 引導自動化相關。(後續跟進。)
- 執行時期前提條件 (`wireguard-tools`、`tcpdump`、`jq`、`curl`) 的安裝 — 手動;
  應作為 routerd 執行時期前提條件寫入文件 / 在打包中處理。(後續跟進。)
- AWS: user-data apt 遇到鏡像同步失敗 → 手動 `apt` 重試 (實驗室引導的脆弱性)。
- AWS: PVE router07 的 DHCP/guest-agent 前提失敗 → 使用靜態 mgmt IP 重新建立
  (PVE 實驗室自動化, 不是 routerd)。

## 3. 設定人因工程 (設定描述的粗糙之處) — 可操作

- **WireGuardPeer.allowedIPs 需與捕獲目標的 `/32` (+ overlay `/32`) 手動匹配** —
  與 `RemoteAddressClaim` 的隱式耦合; 容易出錯 (寬泛 allowedIPs 問題)。
  候選: WG peer 的 allowedIPs 是否覆蓋各傳遞 `/32` 的 validation / `doctor` 交叉檢查
  (或自動匯出)。**最高價值的人因工程修復。** (後續跟進。)
- `nicRef`: Azure 的完整 ARM ID vs AWS 的 ENI ID — provider 格式差異, 手動查找,
  容易出錯。候選: provider 別文件 + 輕量級驗證。(後續跟進。)
- `capture.interface` (proxy-arp) 必須為實際 OS NIC 名 (ens21/eth1) — 手動確認。
- overlay `/32`、共享子網、`ownerSide`、`domain.peerRef` vs `delivery.peerRef` 需
  手動對齊; 兩個 peerRef 部分冗餘。(後續跟進: 簡化/明確化。)
- `configureOSAddress=false` 的語義在 #41 之前是模糊的 (現已明確為 "routerd
  強制 OS 本地不存在")。
- `doctor` 的 FORWARD 策略跳過在 Azure 時可讀性差 (`exit status 1`); AWS 時有改善。

## 4. WireGuard 金鑰供應 — 手動

- private/public 金鑰的產生、放置、公鑰交換全部手動; routerd 僅讀取 `privateKeyFile`。
  候選: 不存在時自動產生 + 公開公鑰用於交換。(後續跟進。)
- (實驗室 SSH 金鑰臨時放置在用戶端用於用戶端發起的 SSH 證據, 之後刪除 — 僅測試工具,
  不在 routerd 範圍內。)

## 5. Provider 供應 — 設計上手動 (routerd MVP 範圍外)

- Azure: RG/VNet/子網/NSG/公共 IP/NIC/VM/磁碟, NIC 輔助 `.9`, NIC IP 轉發,
  啟動/deallocate — 設計上手動 (MVP 無雲端 API mutation; actionPlan /
  CloudProviderProfile 是未來的鉤子)。
- AWS: VPC/子網/IGW/路由表/SG/EIP/EC2/ENI 輔助 `.9`, source/dest check 停用,
  停止 — 設計上手動。
- PVE: VM/網橋/NIC — 實驗室基礎設施, 設計上手動。

## experimental 合併的要點

- 資料平面和冒煙測試中的修復為 routerd 原生, 已在 **3 個雲端**
  (Azure / AWS / OCI) 驗證, 全部乾淨。
- 多雲測試的效果: OCI 的低 PMTU underlay 發現了 **routerd 核心的實際 bug**
  (#53 — PMTU/MSS clamp 被 FirewallZone 門控, 因此 SAM 在任何雲上均無 clamp;
  僅在 underlay PMTU 足夠低時才表現為黑洞)。修復是通用的
  (FirewallZone 無關 + 介面類型無關) 且對家用路由器安全。
- 剩餘的手動操作為 **設計上手動 (provider 供應, MVP 範圍外)** 或
  **experimental 的粗糙之處** (allowedIPs/nicRef/peerRef/金鑰的設定人因工程、
  主機引導、OCI 主機防火牆前提條件 #52)。這些是
  **experimental** 標籤的依據, 非合併阻塞項, 作為後續跟進追蹤。
