---
title: 支援的平台
---

# 支援的平台

routerd 以跨 OS 為前提設計。
各平台所使用的主機端機制因 OS 而異。
本頁明確列出 routerd 在各平台使用的 OS 功能。
套用前，請先確認產生的檔案與執行時期的擁有範圍。

## Linux (Ubuntu / Debian)

以使用 systemd 的 Linux 為主要目標。
發布安裝程式的預設安裝位置為 `/usr/local` 之下。
使用 `sudo apt install ./routerd-linux-amd64.deb` 安裝發布的 Debian 套件。
相依套件為 `ca-certificates`、`iproute2`、`nftables`、`dnsmasq-base`。

routerd 在 Linux 上使用的 OS 功能如下。

- systemd unit
- `/run/routerd` 與 `/var/lib/routerd`（執行時期與持久狀態）
- dnsmasq（DHCPv4 / DHCPv6 / DHCP relay / RA）
- nftables（封包過濾 + NAT）
- conntrack（連線觀測）
- iproute2（介面 + 路由）
- pppd / rp-pppoe（PPPoE）
- WireGuard、Tailscale、strongSwan、radvd

即使在 Ubuntu 上，也不預設套件已預先安裝。
持續的宣告式管理請透過 `Package` 資源宣告相依關係。
參考套件清單如下。

| 分類 | 套件 |
| --- | --- |
| Runtime | `dnsmasq-base`, `nftables`, `conntrack`, `iproute2`, `ppp`, `wireguard-tools`, `tailscale`, `tailscale-archive-keyring`, `strongswan-swanctl`, `radvd` |
| Diagnostics | `dnsutils`, `iputils-ping`, `iputils-tracepath`, `tcpdump`, `traceroute`, `net-tools` |
| OS 控制 | `procps`, `systemd`, `kmod` |

`routerd.service` 僅由 Debian 套件擁有，直接收錄 `contrib/systemd/routerd.service`。主程序以 root 執行，不設定 capability 上限，也不改寫或重新啟動自身單元。已移除 `routerd render systemd-service`。Live ISO 的啟動順序保留在 `routerd.service.d/10-live-setup.conf`。

DHCPv4、DHCPv6-PD、PPPoE、RA 與 mobility ARP observer 和 BGP、DNS 輔助程序一樣，使用每個資源獨立的服務。routerd 解析 `When` 與 HA 角色條件後，透過服務管理器啟動或停止用戶端，不讓用戶端在開機時獨立自動啟動。選擇外部 DHCPv6-PD 用戶端不會啟動內建用戶端。用戶端單元沒有對 `routerd.service` 的 `BindsTo`、`PartOf`、`Requires` 相依，因此重新啟動主程序不會停止它們。FreeBSD 使用現有 rc.d 產生器和原生服務操作，不再使用 Runner 程序監督。用戶端資源的 `When` 為 true 時才啟動或更新；條件未確定時保留現有單元，不啟動、停止或改寫。條件為 false 或從設定刪除時移除單元。停用 PPPoE、選擇外部 DHCPv6-PD 用戶端及 HA 備用節點仍會停止內建用戶端。

套件提供 `routerd.slice`，固定主單元和所有產生的 routerd 服務都屬於此 slice。更新時維護指令碼僅停止 `routerd.service` 和 `routerd.slice`。移除時，`prerm` 還會停用主單元，以 `grep -l` 從 `/etc/systemd/system/*.service` 中選出帶有 `# routerd-managed-service: v1` 的檔案，對每個單元執行 `systemctl disable --now`，再刪除產生檔案。啟用連結由 systemctl 管理；`postrm` 僅重新載入 systemd。這些服務操作及移除清理僅在 systemd 運行時執行。保留維運與 ISO drop-in、設定、資料庫及金鑰。

dnsmasq 管理 DHCP 時啟動 `routerd-dhcp-fingerprint-watcher.service`。watcher 使用 `--journal-unit routerd-dnsmasq.service` 讀取日誌，寫入與 Web Console 相同的 `--db /var/lib/routerd/dhcp-fingerprints.db`。停止及重新啟動隨 dnsmasq 連動。它沒有租約檔案或 ARP 輸入參數；收集需要 DHCP 的 `logDHCP: true`。

Ubuntu 26.04 LTS（`resolute`）已針對受管理的 dnsmasq、nftables、DHCPv6-PD、
委派的 LAN IPv6 位址、控制 API，使用與 Ubuntu 24.04 相同的 Linux
data-plane renderer 完成實機驗證。但在主機 bootstrap 方面，OS 的
網路設定有需注意之處。對於 routerd 所擁有的 DHCPv6-PD 或 LAN RA/DHCPv6
介面，請避免 OS 側的 systemd-networkd 開啟 DHCPv6 client socket。
否則 systemd-networkd 可能比 `routerd-dhcpv6-client` 更早 bind UDP port 546。

Ubuntu 26.04 的 lab 路由器做法是，OS 的 DHCP 僅保留於管理介面，
routerd 所擁有的 WAN/LAN 介面在 OS 層級只設定 link-local。

```yaml
network:
  version: 2
  renderer: networkd
  ethernets:
    ens18:
      dhcp4: false
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
    ens19:
      dhcp4: false
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
    ens20:
      dhcp4: true
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
```

若 WAN link 需要從 RA 取得 IPv6 預設路由，請宣告 WAN 介面及 DHCPv6 / RA 資源。
routerd 會作為 systemd-networkd drop-in 推導出 `IPv6AcceptRA=yes` 與
`[IPv6AcceptRA] DHCPv6Client=no`，因此可在接受 RA 的同時停用 OS 側的 DHCPv6 client。

## FreeBSD

FreeBSD 同樣使用與 Ubuntu 相同的 routerd 資源模型。
套用目標為 FreeBSD 的主機機制。
DHCPv6-PD client 透過 `daemon(8)` 執行，穩定維持租約。
routerd 不使用 Linux 用的機制，而是將資源對應至 FreeBSD 的 `rc.conf`、`rc.d`、`pf`、`mpd5`、`ifconfig`、dnsmasq。
放置 FreeBSD 用發布封存檔中的檔案，並透過 `pkg` 安裝 ports 套件。
步驟見 [FreeBSD 入門](tutorials/freebsd-getting-started.md)。

已實作的項目如下。

- DHCPv6-PD 常駐程式與租約持久化
- WireGuard 與 Linux 的互通
- VXLAN over WireGuard
- 透過 `mpd5.conf`、`mpd_enable`、`mpd5` 服務重啟實現 PPPoE
- `Package` 透過 `pkg` 安裝
- `gateway_enable`、`ipv6_gateway_enable`、`cloned_interfaces`、`ifconfig_*`、`static_routes`、`ipv6_static_routes`、`pf_enable`、`pflog_enable`、`mpd_enable` 的 FreeBSD 風格 `rc.conf.d` 輸出
- `routerd render freebsd --out-dir` 產生 `dhclient.conf`、`mpd5.conf`、`pf.conf`、dnsmasq 設定、`rc.d` script
- 從 `FirewallZone` / `FirewallPolicy` / `FirewallRule` 產生（render）pf 規則
- 從 `NAT44Rule` 產生 pf NAT
- 對產生的 `pf.conf` 執行 `pfctl -nf` 驗證與 `pfctl -f` 套用
- 將 `pfctl -ss -v` 輸出轉換為流量流（traffic flow）
- 透過 BPF 直接讀取 `pflog0` 的防火牆日誌，不依賴 tcpdump 文字格式的差異來解析封包
- DHCPv4、DHCPv6、RA 用的受管理 dnsmasq
- 在 `/var/db/routerd/dnsmasq` 下持久化 dnsmasq 租約
- 服務重啟前以 `dnsmasq --test` 確認設定
- 自動產生 DHCP、DNS、RA、DHCPv6-PD、DS-Lite、WireGuard、HealthCheck 所需的 pf 開口
- 從 `generated service artifacts` 產生 rc.d script
- `routerd-healthcheck` 的 rc.d script 產生
- `routerd-firewall-logger` 的 rc.d script 產生，並直接讀取 `pflog0`

FreeBSD 也支援 `ClientPolicy`。IPv4 使用基於 `DHCPv4Reservation` 的 pf 近似；IPv6 guest identity 必須在 `classification[].ipv6Addresses` 明確宣告。對 FreeBSD 目標，這些穩定位址欄位就是 identity 契約；MAC、OUI、hostname 與 DHCP fingerprint 的 match selector 會被明確拒絕，而不會被靜默忽略。routerd 不會從 IPv4 reservation、MAC、hostname、OUI 或 DHCP fingerprint 推斷 IPv6 identity；明確 IPv6 位址會產生 `inet6` guest-egress deny 規則。
這不等同於 Linux 的 MAC 位址隔離：pf 在 routed filter path 中無法比對 nftables 使用的 Ethernet 來源 selector；privacy 或未列出的 IPv6 位址不在此 slice 內，需要獨立網路隔離（[#849](https://github.com/imksoo/routerd/issues/849)）。
- `TailscaleNode` 的 rc.d script 產生
- 靜態 DS-Lite gif tunnel 的產生（render）
- 從靜態 AFTR IPv6、AFTR FQDN、委派位址衍生的本地來源動態套用 DS-Lite
- 雲端 VPN 用 `IPsecConnection` 的驗證，以及 strongSwan `swanctl` 連線定義的產生。與雲端閘道的實際連通性確認依環境個別進行
- healthcheck 使用原生 `route -n get`，並以 `RTF_PROTO1` 標識 BGP FIB 所有權（包括 replace、withdraw 與保留 foreign route）
- FreeBSD peer 上的 FRR `bfdd` reconcile，以及實機觀測的 Up → Down → Up 回復
- FreeBSD native doctor、KernelModule `kldload` reconcile 與 BGP 專用 `routerd_bgp` rc.d 產生
- FreeBSD 沒有將 probe mark 對應至 request-scoped policy route 的 Linux `SO_MARK` 等價物，因此明確拒絕 fwmark healthcheck（請使用 unmarked route 與 `sourceInterface`/`sourceAddress`）
- FreeBSD 沒有 Linux `IP_FREEBIND` 等價物，因此明確拒絕 non-local DNS resolver bind（必須先指派 address 再啟動 resolver）；outbound DNS 仍可使用獨立的 `sourceInterface: fib:<n>` 機制

ARP/RA observer 常駐程式透過 FreeBSD base system 的 tcpdump/libpcap BPF 路徑擷取資料；proactive ARP write 保留獨立的 direct-BPF descriptor。provisioned native CI 在 disposable VNET 中執行兩個常駐程式，並要求產生預期的 ARP observation event 與 rogue-RA event。帶 tag 的 native DPI backend 支援 FreeBSD ports `ndpi` 5.0 ABI，並由相同 native gate 的 TLS/SNI classification self-test 驗證。

FreeBSD 不使用 Linux 專用的 nftables / conntrack / iproute2。
`Package` 的範例宣告 FreeBSD 側的替代品。
pf 與 `pflog0` 使用 base system，PPPoE 使用 `mpd5`，DS-Lite 使用 `ifconfig gif`，
LAN 的 DHCP/RA 使用 dnsmasq，WireGuard、Tailscale、strongSwan 使用 ports 套件。

| 分類 | 套件 |
| --- | --- |
| Runtime | `dnsmasq`, `wireguard-tools`, `tailscale`, `strongswan`, `mpd5` |
| 選用 native DPI | `ndpi` |
| Diagnostics | `bind-tools` |
| Base system | `ifconfig`, `sysctl`, `service`, `sysrc`, `netstat`, `sockstat`, `tcpdump`, `ping`, `traceroute` |

`routerd render freebsd --out-dir <dir>` 輸出以下內容。

- `rc.conf.d-routerd`
- `dhclient.conf`
- `mpd5.conf`
- `pf.conf`
- `dnsmasq.conf`
- `rc.d-*`

`routerctl apply` 會安裝產生的 `pf.conf`，
並在套用前以 `pfctl -nf` 確認語法。
dnsmasq 也會以 `dnsmasq --test` 確認設定後重新啟動。
套用後以 `pfctl -f` 反映，並以 `service <name> onestart` 啟動產生的 rc.d script。
靜態 `rc.conf` 產生不足以描述的 DS-Lite tunnel 以 `ifconfig gif` 動態套用。
正式投入生產前，請先以 `routerd render freebsd` 確認輸出。

目前的發布認證刻意比可產生的功能清單更窄。FreeBSD 會明確拒絕 `spec.family: ipv6` 的 `EgressRoutePolicy` resource，因為已認證的 PF `route-to` 僅涵蓋 IPv4 static route host。package 的 install/upgrade/uninstall、owned cleanup 與 foreign service preservation 已在 native amd64 與 arm64 認證。`TunnelInterface` 的 gif/GRE dataplane/lifecycle、owned cleanup、foreign interface preservation，credential-free Tailscale external-auth boundary/rollback/foreign preservation，以及 CARP 在三個獨立 FreeBSD VM 上的 failover/recovery/cleanup/foreign preservation，僅有 amd64 native evidence。不主張 Tailscale authenticated enrollment。FreeBSD 14.3 的 GRE key 只控制 outbound encapsulation，kernel 不強制 inbound key matching。

## Platform parity backlog

Ubuntu 和 FreeBSD 相互比較時的已知差異。

| 領域 | 目前差異 | 待辦事項 |
| --- | --- | --- |
| CI / runtime coverage | PR CI 會編譯 FreeBSD amd64/arm64 binary。provisioned FreeBSD 14.3 native evidence 涵蓋完整且不省略的 `go test ./...`、live routerd smoke、ARP/RA observer、native nDPI、amd64/arm64 package lifecycle，以及上述 amd64 TunnelInterface/Tailscale/CARP slice。保留的 VM115 evidence 另涵蓋 route lookup、BFD 與已支援的 PF dataplane slice。 | 不得將 amd64 的 TunnelInterface、Tailscale 或 CARP evidence 概化到 arm64；authenticated Tailscale enrollment 不屬於 release claim。 |
| FreeBSD 的功能限制 | `ClientPolicy` 對 IPv4 使用 DHCPv4 reservation，對 IPv6 使用明確 `classification[].ipv6Addresses` 的 pf 規則；不支援 MAC/L2 比對，也不從 IPv4 reservation 推斷 IPv6。 | 保持明確 address 與 MAC/L2 限制；未列出或 privacy IPv6 位址需獨立網路隔離（[#849](https://github.com/imksoo/routerd/issues/849)）。 |
| IPv6 policy routing | 已認證的 PF `route-to` 僅為 IPv4 static route host source affinity。 | FreeBSD 明確拒絕 `spec.family: ipv6` 的 `EgressRoutePolicy`；這是經核准的 product boundary，而非已實作的 parity（[#904](https://github.com/imksoo/routerd/issues/904)）。 |
| 套件 bootstrap | Ubuntu 和 FreeBSD 可命令式安裝套件。 | 對 `apt`、`pkg` 的 schema、validation、安裝程式套件清單、範例、產生文件保持同步。 |

## OS 抽象化的實作方針

新增 OS 固有行為時，請勿在 business logic 層直接讀取 `runtime.GOOS`。
使用 `pkg/platform` 層（`platform.Features`）或 Go 的 build tag 明確界定邊界。
對不在支援範圍的 OS，優先在 validation 或 planning 階段明確報錯，
而非等到執行時才發生預期外的失敗。
