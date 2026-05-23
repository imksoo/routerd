---
title: 設計備忘錄
---

# 設計備忘錄

本文件記錄 routerd 中值得保留的設計決策。
內容僅保留現行程式碼所遵循的原則，以及未來變更時應恪守的方針，而非過往試錯的時序日誌。

## 1. 常駐程式契約（Daemon contract）

具有狀態的處理由專用常駐程式負責。
為使工具端能夠統一處理，所有常駐程式均公開相同的介面：

- Unix domain socket 上的 HTTP+JSON API
- `/v1/status`
- `/v1/healthz`
- `/v1/events`
- `/v1/commands/reload`
- `/v1/commands/renew`
- `/v1/commands/stop`
- 狀態或租約檔案
- `events.jsonl`（僅追加）

此契約適用於 `routerd-dhcpv6-client`、`routerd-dhcpv4-client`、`routerd-pppoe-client`、`routerd-healthcheck`。

## 2. DHCPv6-PD

DHCPv6-PD 由 `routerd-dhcpv6-client` 負責擁有。不再有為 OS 內建客戶端產生設定的路徑。

在一般的 residential gateway 環境中，標準的 solicit / advertise / request / renew 加上租約持久化與 T1 renew 即已足夠。
依照現行方針，不使用為迴避損壞環境而設計的過度重傳機制。

## 3. 誠實的 LAN 廣告

DHCPv6-PD 若未處於 `Bound` 狀態，routerd 不會向 LAN 輸出過時的 IPv6 資訊。
這適用於 RA、DHCPv6 server、AAAA 記錄，以及從前綴衍生的 LAN 位址。
原則是「損壞的狀態，就如實呈現損壞」。不會持續散佈無法到達的前綴。

## 4. DS-Lite

部分接取網路的 DHCPv6 information-request 不會回傳 AFTR 選項。
因此 `DSLiteTunnel` 將 `aftrFQDN` 或 `aftrIPv6` 的靜態指定視為正規路徑，而非退回選項。

AFTR 的 FQDN 在公眾 DNS 上往往無法解析。請使用 AFTR domain 專用的 `DNSForwarder`，並搭配從 DHCPv6 information status 讀取電信業者內部解析器的 `DNSUpstream.addressFrom` 進行轉送。

## 5. 事件整合

routerd 具有程序內事件匯流排。控制器收到事件後，僅重新評估受影響的資源。

高層次的整合使用以下 Kind：

- `EgressRoutePolicy`
- `EventRule`
- `DerivedEvent`
- `HealthCheck`

`EventRule` 以事件串流為輸入，產生另一個事件串流。
`DerivedEvent` 從觀測到的狀態合成 asserted / retracted 的虛擬事件。

## 6. Tier S 構成要素

WireGuard、Tailscale、IPsec、VRF、VXLAN 是 Tier S（SOHO / 分支機構）的構成要素。
WireGuard 與 VXLAN-over-WireGuard 已確認可在支援的 OS 之間互通。
`TailscaleNode` 負責處理 exit node 與 subnet router 的廣告。
這樣設計是為了避免將所有 VPN 硬塞進同一種抽象形式。

不建立抽象的 `VPNTunnel` 資源。
WireGuard、Tailscale、IPsec，以及未來的 SoftEther 整合，分別以獨立的 Kind 新增。
原因是各自的狀態機差異甚大，若強行合併為多態的單一 Kind，將喪失語義清晰性。

## 7. OpenRC 服務產生（render）

Alpine 使用 OpenRC 而非 systemd。
OpenRC 支援先以 renderer 而非 applier 的形式開始實作。
`routerd render alpine --out-dir` 會輸出可供審閱的 init script 及相關設定，讓使用者在 routerd 變更 OpenRC 狀態之前，能先確認已部署主機的行為。

初期支援的 OpenRC 範圍刻意保持精簡：

- 從明確的 `generated service artifacts` 資源轉換為 OpenRC script
- 自動產生 `routerd-healthcheck` script
- 當 DHCP 或 RA 資源需要 dnsmasq 時，自動產生受管理的 dnsmasq script
- 自動產生 DHCPv4 / DHCPv6 客戶端、防火牆日誌記錄、PPPoE、Tailscale 的 script
- DNS 解析器 script，但在解析器的執行時期設定能夠在控制器循環之外實體化之前，不啟用或啟動

這樣做是為了避免陷入相容性死胡同。
API 形式暫時維持 `generated service artifacts`，但只有明確具備 init script 語義的欄位才會轉換為 OpenRC，具體包括 `ExecStart`、`ExecStartPre`、environment、working directory、user/group、runtime/state/log directory。
systemd sandboxing、networkd、resolved、timesyncd 的語義，不在 OpenRC 上模擬。

套用時的啟動以 `HasOpenRC` 進行分支。
僅在內容或模式變更時才寫入 script；透過 `rc-update show default` 確認註冊狀態後，再執行 add / del；透過 `rc-service <name> status` 確認後，再執行 start / restart / stop。
與 systemd 端相同，若期望狀態與檔案均未變更，不重複執行服務管理員指令。

下一個實作階段，是將 Alpine 已部署主機的煙霧測試套件納入一般 VM job。

## 8. 待解事項

- 具狀態防火牆的正式生產運用。`FirewallRule` 支援 ICMP type 比對、
  多個埠、nftables rate limit、每個來源的連線數限制。
  今後將著重改善規則群組與上層政策的易用性，
  而非追求基本表達式的全面覆蓋。
- LAN 端的 DoH 代理。
- 面向 Tier C 的 OSPF 等動態路由整合。
- 高可用性（leader 選舉、容錯控制平面）。
- 生產環境的可觀測性（OpenTelemetry 收集器與遠端日誌接收端）。
- 在家用線路上以 routerd 作為唯一 WAN 路由器長期運行的驗證。
