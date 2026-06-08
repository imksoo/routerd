# CloudEdge SAM AWS x PVE 冒煙測試證據

日期: 2026-05-29

分支/建置: `cloudedge-mvp`, `routerd f60e7d9a`

Result: PASS (乾淨 — 無手動變通; Azure 對等)

這是 Selective Address Mobility 首次在第 2 個公有雲 (AWS VPC/EC2) 上執行的驗證。無需任何 AWS 特定的程式碼變更, provider-secondary-ip 捕獲 + de-assign 加固和 WireGuard stdin apply (在 Azure 週期中實作) 按設計通用化。provider 特定的工作僅在供應側 (AWS ENI 輔助 IP + EC2 source/destination check 停用)。

## 拓撲

- 雲端用戶端 (AWS EC2): `10.88.60.7/24`
- 本地用戶端 (PVE VM): `10.88.60.9/24`
- 雲端路由器 (AWS EC2): 主 `10.88.60.4/24`, ENI 輔助捕獲 `10.88.60.9`
- 本地路由器 (PVE, router07): `10.88.60.1/24` (`vmbr470`)
- Overlay: `wg-hybrid`, `169.254.120.1/32` (雲端) <-> `169.254.120.2/32` (本地)
- 區域: ap-northeast-1。WireGuard: 本地 -> AWS 公共端點, persistent keepalive。

## AWS 捕獲前提條件 (供應側)

- ENI: 主 `10.88.60.4`, 輔助私有 IPv4 `10.88.60.9`。
- EC2 source/destination check: DISABLED (AWS 對應 Azure NIC IP 轉發)。
- routerd-cloud 客體 OS: routerd 將 `10.88.60.9` 從本地位址中刪除
  (`provider-secondary-ip` + `configureOSAddress=false` de-assign 強制)。

## 斷言 (全部 PASS)

- 雲端傳遞路由: `10.88.60.9 dev wg-hybrid metric 120`。
- 本地: `10.88.60.7` 的 proxy ARP; 傳遞路由 `10.88.60.7 dev wg-hybrid metric 120`。
- 階段 A: AWS 路由器 NIC 的 tcpdump 捕獲到 `.7 -> .9` ICMP request/reply。
- `.7 -> .9` ping 3/3 (0% loss); `.9 -> .7` ping 3/3 (0% loss)。
- SSH 雙向, 來源位址保留:
  - `SSH_CONNECTION=10.88.60.7 ... 10.88.60.9 22`
  - `SSH_CONNECTION=10.88.60.9 ... 10.88.60.7 22`
- 無 NAT; 兩個用戶端的預設閘道均未變更。
- doctor hybrid: AWS 側 overall pass (pass 10 / warn 0 / fail 0 / skip 1);
  PVE 側 overall pass (pass 13 / warn 0 / fail 0 / skip 1)。

## 備註

- 無 AWS 特定的故障; 未提交新 issue。
- Azure x PVE 對 (router06) 未變更。
- 成本: EC2 執行個體在證據捕獲後停止 (為重新執行保留); EIP/EBS 保留至完全 teardown。完整的本地證據包:
  `routerd-labs/cloudedge-sam/evidence/20260529T233145Z-aws-pve-f60e7d9a`。
