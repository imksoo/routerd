---
title: 支援的平台
---

# 支援的平台

![Diagram showing supported platforms with Linux systemd primary integration, Alpine OpenRC live ISO support, NixOS module activation, FreeBSD rc.d and pf groundwork, and pkg/platform feature-gated implementation rules](/img/diagrams/platforms.png)

routerd 以跨 OS 為前提設計。
各平台所使用的主機端機制因 OS 而異。
本頁明確列出 routerd 在各平台使用的 OS 功能。
套用前，請先確認產生的檔案與執行時期的擁有範圍。

## Linux (Ubuntu / Debian)

以使用 systemd 的 Linux 為主要目標。
發布安裝程式的預設安裝位置為 `/usr/local` 之下。
展開 Linux 用的發布封存檔後，執行 `sudo ./install.sh`。
安裝程式可透過 `apt-get`、`dnf`、`apk`、`pacman` 之一安裝執行時期套件。

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
初次準備時，`install.sh` 會安裝實用的預設套件組合。
持續的宣告式管理請透過 `Package` 資源宣告相依關係。
參考套件清單如下。

| 分類 | 套件 |
| --- | --- |
| Runtime | `dnsmasq-base`, `nftables`, `conntrack`, `iproute2`, `ppp`, `wireguard-tools`, `tailscale`, `tailscale-archive-keyring`, `strongswan-swanctl`, `radvd` |
| Diagnostics | `dnsutils`, `iputils-ping`, `iputils-tracepath`, `tcpdump`, `traceroute`, `net-tools` |
| OS 控制 | `procps`, `systemd`, `kmod` |

`routerd-dhcpv6-client`、`routerd-dhcpv4-client`、`routerd-pppoe-client`、`routerd-healthcheck` 在 Linux 上以 systemd 服務運作。

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

## Alpine Linux

Alpine 是 Live ISO 與最小配置安裝主機的 Linux 目標。
目前尚未達到與 Ubuntu 同等的支援水準。
routerd 在可用範圍內使用 Linux 的 data plane 工具，但服務啟用方面仍有尚待解決的 OpenRC 課題。

已實作的項目如下。

- Alpine 的 Live ISO 開機與 USB 持久化
- `install.sh` 透過 `apk` 安裝相依套件
- `pkg/platform` 中的 Alpine 偵測與 `HasOpenRC`
- `Package` 資源的 `os: alpine` / `manager: apk`
- Alpine 的 `install.sh --list-deps` 以及最小限度的 `Package` validate / plan 路徑的 CI smoke coverage
- `routerd render alpine --out-dir` 產生 OpenRC script 與 dnsmasq 設定
- 明確的 `generated service artifacts`、受管理的 dnsmasq、`routerd-healthcheck`、DHCPv4 / DHCPv6 client、DNS resolver、防火牆日誌記錄器、PPPoE、Tailscale 的 OpenRC script 產生（render）
- 套用時透過 `rc-update` / `rc-service` 啟用；狀態未變更時不重複執行 enable / start / restart 的確認
- 針對已安裝 Alpine guest 的 `make alpine-vm-smoke` 測試框架
- Alpine 用的 nftables、conntrack、iproute2、dnsmasq、PPP、WireGuard、strongSwan、radvd、診斷套件名稱整理

達到與 Ubuntu 同等水準前的待辦事項如下。

- Live ISO bootstrap 以外的已安裝主機網路所有權
- 將 Alpine 已安裝主機的 smoke 測試框架升級為一般 VM CI 工作，持續確認 OpenRC 啟用與實際 package-manager 指令路徑
- 針對仍未對應 OpenRC、僅支援 systemd 的資源補充詳細文件

| 分類 | 套件 |
| --- | --- |
| Runtime | `dnsmasq`, `nftables`, `conntrack-tools`, `iproute2`, `ppp`, `ppp-pppoe`, `wireguard-tools`, `strongswan`, `radvd` |
| Diagnostics | `bind-tools`, `iputils`, `iputils-tracepath`, `tcpdump` |
| OS 控制 | `alpine-conf`, `kmod`, `util-linux`, `e2fsprogs`, `dosfstools`, `exfatprogs` |

## NixOS

NixOS 使用與 Ubuntu 相同的 routerd 資源模型。
但套用方式經由 NixOS 模組。
不寫暫時性的 systemd unit，而是產生 `/etc/nixos/routerd-generated.nix`，
再透過 `nixos-rebuild test` / `nixos-rebuild switch` 啟用。

已實作的項目如下。

- NixOS 的啟用、重新開機後復原、DHCPv6-PD、dnsmasq 的 LAN 服務、DNS resolver、DS-Lite、nftables 的 NAT 與防火牆、HealthCheck、Web 管理介面的世代差異、OpenTelemetry 傳送的實機驗證
- `routerd-dhcpv6-client` 的 systemd unit 產生
- `routerd-dhcpv4-client` 的 systemd unit 產生
- `routerd-pppoe-client` 的 systemd unit 產生
- `Package` override、`SysctlProfile`、衍生主機執行時期成果物、`generated service artifacts` 的 NixOS 模組產生
- `nixos-rebuild test` / `nixos-rebuild switch` 整合
- `nixos-rebuild switch` 失敗時嘗試 `nixos-rebuild switch --rollback`
- `nixos-rebuild` 前後的世代（generation）記錄
- DHCPv6-PD 到達 `Bound`
- DHCP 或 RA 資源需要 dnsmasq 時產生 `routerd-dnsmasq` 服務
- `routerd-dnsmasq` 服務中使用 NixOS 系統 profile 內的絕對路徑，並指定以 root 執行，以免在 systemd 保護設定下依賴 `PATH` 搜尋或降權行為
- DNS resolver、HealthCheck、防火牆日誌記錄器、Tailscale、DHCPv4 client、DHCPv6 client、PPPoE client 的服務產生
- NAT、防火牆、策略路由、Path MTU 資源需要 nftables 時產生 `networking.nftables.enable = true`
- WireGuard、Tailscale、VXLAN、systemd-networkd VRF 的產生
- NixOS 原生網路宣告無法表達的 Linux 執行時期資源，由啟用後的 `routerd.service` 進行調和（reconcile）

在 NixOS 上，請將 routerd 所需的指令放入 `systemd.services.routerd.path`。
`install.sh` 偵測到 NixOS 時，不會執行 `nix-env`，只輸出警告。
NixOS 的套件狀態請以宣告式管理。
`Package` 資源若寫了 `os: nixos`，routerd 不會在執行時安裝套件。
`routerd render nixos` 會產生 `environment.systemPackages`。

NixOS 啟用後的清單如下。

| 領域 | 目前擁有者 | 備註 |
| --- | --- | --- |
| 套件與 routerd 服務路徑 | 產生的 NixOS 模組 | `Package` 資源會對應至 `environment.systemPackages`。routerd 不呼叫 `nix-env`。 |
| 輔助常駐程式服務定義 | 產生的 NixOS 模組 | DHCPv4、DHCPv6、PPPoE、HealthCheck、防火牆日誌記錄器、Tailscale、dnsmasq 以 Nix 的 systemd 服務表示。 |
| nftables 啟用 | 產生的 NixOS 模組 | NAT、防火牆、策略路由、Path MTU 資源有需求時輸出 `networking.nftables.enable = true`。 |
| 僅執行時期的網路變更 | 啟用後的 `routerd.service` | 動態的 DS-Lite、暫時性的路由判斷、status 衍生的變更需要執行時期的調和（reconcile）。 |
| 舊執行時期 dnsmasq unit 的清理 | 啟用後的 `routerd.service` | 移轉時暫時保留，用於刪除舊的 `/run/systemd/system/routerd-dnsmasq.service` 成果物。已安裝主機歷經一個發布週期後刪除。 |

| 分類 | 套件 |
| --- | --- |
| Runtime | `dnsmasq`, `nftables`, `conntrack-tools`, `iproute2`, `ppp`, `wireguard-tools`, `tailscale`, `strongswan`, `radvd` |
| Diagnostics | `bind`, `iputils`, `tcpdump`, `traceroute`, `nettools` |
| OS 控制 | `procps`, `systemd`, `kmod` |

## FreeBSD

FreeBSD 同樣使用與 Ubuntu 相同的 routerd 資源模型。
套用目標為 FreeBSD 的主機機制。
DHCPv6-PD client 透過 `daemon(8)` 執行，穩定維持租約。
routerd 不使用 Linux 用的機制，而是將資源對應至 FreeBSD 的 `rc.conf`、`rc.d`、`pf`、`mpd5`、`ifconfig`、dnsmasq。
展開 FreeBSD 用的發布封存檔後，執行 `sudo ./install.sh`。
安裝程式透過 `pkg` 安裝 ports 套件，並只對 base system 的指令進行確認，不另行安裝。

已實作的項目如下。

- DHCPv6-PD 常駐程式與租約持久化
- WireGuard 與 Linux / NixOS 的互通
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

`ClientPolicy` 目前為 Linux 專用的防火牆功能。
使用 nftables 的 Ethernet 來源位址 set 隔離訪客裝置。
FreeBSD pf 無法在 routed filter 路徑以相同模型處理，因此 routerd 明確將此資源標示為不支援。
- `TailscaleNode` 的 rc.d script 產生
- 靜態 DS-Lite gif tunnel 的產生（render）
- 從靜態 AFTR IPv6、AFTR FQDN、委派位址衍生的本地來源動態套用 DS-Lite
- 雲端 VPN 用 `IPsecConnection` 的驗證，以及 strongSwan `swanctl` 連線定義的產生。與雲端閘道的實際連通性確認依環境個別進行

FreeBSD 不使用 Linux 專用的 nftables / conntrack / iproute2。
`Package` 的範例宣告 FreeBSD 側的替代品。
pf 與 `pflog0` 使用 base system，PPPoE 使用 `mpd5`，DS-Lite 使用 `ifconfig gif`，
LAN 的 DHCP/RA 使用 dnsmasq，WireGuard、Tailscale、strongSwan 使用 ports 套件。

| 分類 | 套件 |
| --- | --- |
| Runtime | `dnsmasq`, `wireguard-tools`, `tailscale`, `strongswan`, `mpd5` |
| Diagnostics | `bind-tools`, `tcpdump` |
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

## Platform parity backlog

Ubuntu、NixOS、FreeBSD、Alpine 相互比較時的已知差異。

| 領域 | 目前差異 | 待辦事項 |
| --- | --- | --- |
| CI / runtime coverage | CI 在 Ubuntu 上執行 unit test 與 Linux static check。Alpine 有不依賴主機的安裝程式相依性 smoke、最小限度的 `Package` validate / plan coverage 及已安裝主機的 smoke 測試框架，但 Alpine 的啟用尚未成為一般 VM 工作。FreeBSD 在發布時進行 cross build，NixOS 的啟用也尚未成為 VM 工作。 | 新增 FreeBSD VM、NixOS VM、Alpine VM 的 smoke 工作，涵蓋 validate、plan、實際 package-manager 確認、服務啟用、renderer 語法確認。 |
| Alpine 的服務管理員 | Alpine 有明確的 `generated service artifacts`、受管理的 dnsmasq、`routerd-healthcheck`、DHCP client、DNS resolver、防火牆日誌記錄器、PPPoE、Tailscale 的 OpenRC 產生（render）。套用時的啟用使用 `rc-update` / `rc-service`，狀態未變更時避免重複執行 enable / start / restart。DNS resolver script 會產生，但在執行時期設定實體化（materialize）加入前不執行 enable / start。 | 推進 OpenRC 用的 DNS resolver 執行時期設定實體化（materialize）、已安裝主機網路所有權的擴展、Alpine smoke 測試框架的 CI 升級。 |
| NixOS 殘留的命令式部分 | NixOS 產生模組，啟用交由 `nixos-rebuild` 處理。僅執行時期的網路變更與舊 dnsmasq unit 的清理殘留於啟用後的 `routerd.service`。此清理是為了第一個包含產生的 NixOS dnsmasq 服務擁有權的發布而刻意保留。 | 該發布週期後刪除舊 dnsmasq 清理，對於 NixOS 原生宣告可表達的部分減少啟用後的調和（reconcile），並對剩餘的僅執行時期資源新增測試。 |
| FreeBSD 的功能例外 | `ClientPolicy` 依賴 nftables 的 Ethernet 來源位址 set，為 Linux 專用。 | 在找到可保留相同隔離語義的設計之前，明確拒絕。 |
| 套件 bootstrap | Ubuntu、Alpine、FreeBSD 可命令式安裝套件。NixOS 刻意產生套件宣告。schema、validation、範例、安裝程式相依性清單、CI smoke coverage 已更新至包含 `apk`。 | 對 `apt`、`apk`、`pkg`、Nix 宣告的 schema、validation、安裝程式套件清單、範例、產生文件保持同步。 |

## OS 抽象化的實作方針

新增 OS 固有行為時，請勿在 business logic 層直接讀取 `runtime.GOOS`。
使用 `pkg/platform` 層（`platform.Features`）或 Go 的 build tag 明確界定邊界。
對不在支援範圍的 OS，優先在 validation 或 planning 階段明確報錯，
而非等到執行時才發生預期外的失敗。
