---
title: 變更記錄
---

# 變更記錄

routerd 的版本歷程。格式遵循 [Keep a Changelog](https://keepachangelog.com/)。
routerd 自 `20260509` 起使用 `yyyymmdd` 格式的日期型版號。
本軟體仍在 v1alpha1 階段，版本之間可能含有破壞性異動。

## 20260509.8

### 修正

- 修正 zh-Hant 與 zh-Hans 文件連結，翻譯頁不再指向尚未翻譯的同語系頁面。
- 在完整翻譯完成前，總覽頁會連到英文正準參考頁。

## 20260509

### 新增

- `EgressRoutePolicy` 現在可以表達 DS-Lite 主路徑、RA 來源 DS-Lite、PPPoE 與 WAN 直連的多階段備援。
- 透過宣告式 `Telemetry` 資源與 OTLP 環境變數傳遞，將 OpenTelemetry 設定擴展到路由器群。
- DS-Lite 範例改用 RFC 6333 的 B4-AFTR link prefix `192.0.0.0/29` 作為隧道內側 IPv4 來源位址。
- `PPPoEInterface.disabled` 與停用的路徑候選允許在 YAML 中保留 PPPoE 備援定義，同時避免正式環境 PPPoE session 外洩。

### 異動

- 版號從 `0.x.y` 改為 `20260509` 這類日期字串。
- Linux nftables 與 FreeBSD pf 的 NAT44 產生方式收斂為按介面產生規則。
- 已在 Linux 與 FreeBSD 驗證 3-role firewall；service hole 會綁定到擁有它的接收入介面。
- FreeBSD pf 支援為 `PathMTUPolicy` 產生 TCP MSS clamp；dnsmasq RA 也會發布 MTU option。

### 修正

- FreeBSD pf 不再將 DHCPv6、WireGuard、VXLAN 的 service hole 擴展到 `wan` zone 的所有介面。
- FreeBSD NAT artifact 現在回報為 `pf.anchor/routerd_nat`。
- NAT 產生前會將 PPPoE 資源名解析為實際 OS 介面名。

## 0.4.0

### 新增

- nftables 的隱含拒絕封包紀錄會由 `routerd-firewall-logger` 接收，並寫入 `firewall-logs.db`。Linux 直接讀取 `nfnetlink`，FreeBSD 透過 `tcpdump` 讀取 `pflog`。
- Web Console 新增「Connections」分頁（即時 conntrack / pf state）、「Clients」分頁（DHCP 租約與流量整合）以及「Firewall」分頁（拒絕排行 + 時間序列）。
- `WebConsole.spec.listenAddressFrom` 與 `DNSResolver` 系列的待聽位址，可由 `Interface/<name>.status.ipv4Addresses` 推導。允許以參考代替字面值。
- 預設啟用 conntrack 計數（`net.netfilter.nf_conntrack_acct=1`），`SysctlProfile/router-linux` 將其納入；`TrafficFlowLog` 因此能聚合 `bytesOut` / `bytesIn`。

### 異動

- 即時連線檢視的 API / CLI 統一命名為 `connections`（舊稱 `conntrack-snapshot`）。請改用 `/api/v1/connections`、`routerctl connections`。IPv6 也納入同一張表。
- NixOS 的宣告式渲染擴充。`Package`（NixOS 套件宣告）、`SysctlProfile`、`NetworkAdoption`、`SystemdUnit` 皆會輸出至 `routerd render nixos`。NixOS 上的 `Package` 不再於執行期安裝，而由產生的 NixOS 設定接管。
- `SystemdUnit` 可產生 FreeBSD `rc.d` 腳本（`routerd render freebsd --out-dir`）。

### 修正

- 當 `Link/<name>` 狀態為空時，`IPv6DelegatedAddress` 不再略過將 PD 派生位址掛上實體介面的步驟。
- `SystemdUnit` 不再對未變動的 active unit 做不必要的重啟。

## 0.3.0

### 新增

- 宣告式 OS bootstrap 資源 `Package` 與 `SysctlProfile`。涵蓋 apt、dnf、nix、pkg 的套件宣告，以及路由器導向的 sysctl 推薦值（`nf_conntrack_max`、socket buffer、TCP/UDP timeout、`ip_forward` 等）。
- `NetworkAdoption` 可由 YAML 關閉 systemd-networkd 的 DHCP / RA。`SystemdUnit` 由 routerd 自身渲染、安裝、啟用 unit 檔案。
- `routerctl events --limit N --topic X --resource K/N -o json` 不再依賴 `sqlite3` 即可檢視 bus event。
- `routerd plan --diff` 提供 apply 前的差異預覽。
- `DNSResolver` 支援 bootstrap forwarder（內部 DNS 為主，公用 DNS 為備援）。

### 異動

- 設定檔的 `${...status.field}` 字串參考改為型別化 `*From` 欄位（`addressFrom`、`ipv4From`、`ipv6From`、`upstreamFrom`、`prefixFrom`、`rdnssFrom`、`dependsOn`）。沒有相容別名。
- controller chain 重構為純 event-loop。共用 `framework.FuncController`（Subscriptions + Bootstrap + PeriodicFunc）與 `eventedStore`，狀態保存時必發 `routerd.resource.status.changed`，由下游 controller 觸發再評估。
- bus event 透過 `slog` 輸出至 systemd journal（`journalctl -u routerd.service -f | grep "routerd event"` 即可追蹤 controller 行為）。高頻事件為 debug 等級。
- 全部 binary 改為靜態連結（`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`）。OS 別套件清單（`dnsmasq-base`、`nftables`、`conntrack`、`iproute2`、`ppp`、`wireguard-tools`、`strongswan-swanctl`、`radvd`、`tcpdump` 等）按 Ubuntu / NixOS / FreeBSD 整理。
- `HealthCheck.sourceInterface` 在 YAML 上以資源名表示，於執行期解析為 OS 介面名。

### 修正

- `SystemdUnit` 之間的 `RuntimeDirectory` 衝突會在重啟時刪除 socket，已透過 `runtimeDirectoryPreserve` 宣告式解決。
- `state: absent` 的 `SystemdUnit` 現可正確判定為 Drifted，並列入 plan 中刪除。
- `SysctlProfile` 觀測時的型別漂移誤判已抑制。

## 0.2.0

### 新增

- 狀態化 firewall：`FirewallZone`、`FirewallPolicy`、`FirewallRule` 產生 nftables 的 `inet routerd_filter` table。
- `EgressRoutePolicy`（原名 `WANEgressPolicy`）新增 `destinationCIDRs`、`gateway`、`gatewaySource`。`HealthCheck` 可透過 `via`、`sourceInterface`、`sourceAddress` 指定 probe 路徑。
- DNS 子系統重構：`DNSZone`（權威區）與 `DNSResolver`（轉發 / 快取）分離。涵蓋本地區、條件式轉發、DoH / DoT / DoQ、明文 UDP DNS。dnsmasq 限定為 DHCPv4 / DHCPv6 / RA / 中繼。
- DS-Lite（`DSLiteTunnel`）、PPPoE（`PPPoESession`、`routerd-pppoe-client`）、DHCPv4 client（`routerd-dhcpv4-client`、`DHCPv4Lease`）。
- NAT44（`NAT44Rule`）與 conntrack 觀測。在無 `/proc/net/nf_conntrack` 環境會退回 sysctl 統計。

### 異動

- `WANEgressPolicy` 改名為 `EgressRoutePolicy`。沒有相容別名。
- DHCP 相關 Kind 與 binary 名稱對齊 RFC 表記法（`routerd-dhcpv4-client`、`routerd-dhcpv6-client`）。沒有相容別名。

## 0.1.0

最初的 v1alpha1 實作。

- 引入 DHCPv6-PD client、daemon contract、event bus、controller framework。
- 實作從 DHCPv6-PD 到 LAN 位址推導再到 DNS 回應的 controller chain。
- 新增 DHCPv6 information-request、DS-Lite（試作）、IPv4 路由、RA、DHCPv6 server、`HealthCheck`、`EventRule`、`DerivedEvent`。

之後出貨前整理過程中，API 名稱與實作策略做了大幅調整。請參考上方 `Unreleased` 與 `examples/` 取得最新使用方式。
