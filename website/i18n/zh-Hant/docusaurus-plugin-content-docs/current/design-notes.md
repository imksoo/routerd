---
title: 設計備忘錄
---

# 設計備忘錄

![Diagram showing routerd design notes covering daemon contracts, DHCPv6-PD ownership, honest LAN advertisement, DS-Lite AFTR resolution, event coordination, and reusable building blocks](/img/diagrams/design-notes.png)

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

## 7. 待解事項

- 具狀態防火牆的正式生產運用。`FirewallRule` 支援 ICMP type 比對、
  多個埠、nftables rate limit、每個來源的連線數限制。
  今後將著重改善規則群組與上層政策的易用性，
  而非追求基本表達式的全面覆蓋。
- LAN 端的 DoH 代理。
- 面向 Tier C 的 OSPF 等動態路由整合。
- 高可用性（leader 選舉、容錯控制平面）。
- 生產環境的可觀測性（OpenTelemetry 收集器與遠端日誌接收端）。
- 在家用線路上以 routerd 作為唯一 WAN 路由器長期運行的驗證。
