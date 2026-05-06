---
title: 支援的平台
---

# 支援的平台

routerd 以跨 OS 為前提設計,但每個平台使用的主機整合方式不同。本頁列出 routerd 在各平台會使用的具體 OS 介面,方便你在套用 router 設定前檢查產生的檔案與執行時擁有範圍。

## Linux (Ubuntu / Debian)

Linux 是目前驗證最多的部署目標。原始碼安裝預設放在 `/usr/local`。

routerd 在 Linux 上使用下列 OS 介面:

- systemd unit 檔案
- `/run/routerd` 與 `/var/lib/routerd`,分別保存執行時資料與持久狀態
- dnsmasq,負責 DHCPv4、DHCPv6、DHCP relay 與 Router Advertisement
- nftables,負責封包過濾與 NAT
- conntrack,負責連線觀測
- iproute2,負責 interface 與路由
- pppd / rp-pppoe,負責 PPPoE
- WireGuard、Tailscale、strongSwan、radvd

即使在 Ubuntu 上,routerd 也不假設套件已預先安裝。請用 `Package` resource 宣告相依套件。參考清單如下:

| 分類 | 套件 |
| --- | --- |
| Runtime | `dnsmasq-base`, `nftables`, `conntrack`, `iproute2`, `ppp`, `wireguard-tools`, `tailscale`, `tailscale-archive-keyring`, `strongswan-swanctl`, `radvd` |
| Diagnostics | `dnsutils`, `iputils-ping`, `iputils-tracepath`, `tcpdump`, `traceroute`, `net-tools` |
| OS control | `procps`, `systemd`, `kmod` |

`routerd-dhcpv6-client`、`routerd-dhcpv4-client`、`routerd-pppoe-client` 與 `routerd-healthcheck` 在 Linux 上以 systemd service 執行。

## NixOS

NixOS 使用與 Ubuntu 相同的 routerd resource model,但啟用流程走 NixOS module。routerd 不寫 transient systemd unit,而是產生 `/etc/nixos/routerd-generated.nix`,再由 `nixos-rebuild test` / `nixos-rebuild switch` 啟用。

已實作:

- `routerd-dhcpv6-client` 的 systemd unit 生成
- `routerd-dhcpv4-client` 的 systemd unit 生成
- `routerd-pppoe-client` 的 systemd unit 生成
- `Package`、`SysctlProfile`、`NetworkAdoption`、`SystemdUnit` 的 NixOS module 生成
- `routerd apply --dry-run` 觸發 `nixos-rebuild test`
- `routerd apply` 觸發 `nixos-rebuild switch`
- `nixos-rebuild switch` 失敗時嘗試 `nixos-rebuild switch --rollback`
- `nixos-rebuild` 前後的 generation 記錄
- DHCPv6-PD 到達 `Bound`
- DHCP 或 RA resource 需要 dnsmasq 時產生 `routerd-dnsmasq` service
- DNS resolver、HealthCheck、firewall logger、Tailscale、DHCPv4 client、DHCPv6 client、PPPoE client service 生成
- NAT、firewall、policy routing、Path MTU resource 需要 nftables 時產生 `networking.nftables.enable = true`
- WireGuard、Tailscale、VXLAN,以及 native systemd-networkd VRF 生成
- NixOS native network 宣告無法表達的 Linux runtime resource,由 NixOS 啟用後的 `routerd.service` 調整

在 NixOS 上,請把 routerd 需要的命令放進 `systemd.services.routerd.path`。當 `Package` resource 寫了 `os: nixos` 時,routerd 不會在執行時以命令方式安裝套件。它會把套件寫進 `/etc/nixos/routerd-generated.nix` 的 `environment.systemPackages`,再交給 `nixos-rebuild` 啟用系統 profile。

| 分類 | 套件 |
| --- | --- |
| Runtime | `dnsmasq`, `nftables`, `conntrack-tools`, `iproute2`, `ppp`, `wireguard-tools`, `tailscale`, `strongswan`, `radvd` |
| Diagnostics | `bind`, `iputils`, `tcpdump`, `traceroute`, `nettools` |
| OS control | `procps`, `systemd`, `kmod` |

## FreeBSD

FreeBSD 使用與 Ubuntu 相同的 routerd resource model,但映射到 FreeBSD 主機機制。DHCPv6-PD client 透過 `daemon(8)` 執行,並維持持久 lease。routerd 不使用 Linux 專用機制,而是把 resource 對應到 FreeBSD native 的 `rc.conf`、`rc.d`、`pf`、`mpd5`、`ifconfig` 與 dnsmasq。

已實作:

- DHCPv6-PD daemon 與持久 lease
- WireGuard 與 Linux / NixOS 互通
- VXLAN over WireGuard
- 透過產生的 `mpd5.conf`、`mpd_enable` 與 `mpd5` service restart 提供 PPPoE
- 透過 `pkg` 安裝 `Package`
- `render freebsd --out-dir` 產生可審閱的 `install-packages.sh`
- FreeBSD 風格的 `rc.conf.d` 輸出,包含 `gateway_enable`、`ipv6_gateway_enable`、`cloned_interfaces`、`ifconfig_*`、`static_routes`、`ipv6_static_routes`、`pf_enable`、`pflog_enable`、`mpd_enable`
- `routerd render freebsd --out-dir` 產生 `dhclient.conf`、`mpd5.conf`、`pf.conf`、dnsmasq 設定與 `rc.d` scripts
- 從 `FirewallZone`、`FirewallPolicy`、`FirewallRule` 產生 pf 規則
- 從 `IPv4SourceNAT` 與 `NAT44Rule` 產生 pf NAT
- 對產生的 `pf.conf` 自動執行 `pfctl -nf` 驗證與 `pfctl -f` 套用
- 從 `pfctl -ss -v` 取得相當於 conntrack 的 traffic flow
- 透過 BPF 直接讀取 `pflog0` 取得 firewall log,避免依賴各版本 tcpdump 文字格式
- 以管理中的 dnsmasq 提供 DHCPv4、DHCPv6 與 Router Advertisement
- 在 `/var/db/routerd/dnsmasq` 保存 dnsmasq lease
- service restart 前以 `dnsmasq --test` 驗證 dnsmasq 設定
- 針對 routerd 擁有的 DHCP、DNS、RA、DHCPv6-PD、DS-Lite、WireGuard、healthcheck traffic 自動開 pf 洞
- DNS resolver daemon 可在 FreeBSD 上建置;`viaInterface` 可指定 `fib:<n>` 以綁定 FIB 的 upstream routing
- cloud VPN `IPsecConnection` 會驗證並產生 strongSwan `swanctl` 連線定義;雲端 gateway 實通驗證依部署環境進行
- 從 `SystemdUnit` 產生、安裝 rc.d script,並以 `service <name> onestart` 啟用
- `routerd-healthcheck` 的 rc.d script 生成
- `routerd-firewall-logger` 的 rc.d script 生成,並直接讀取 `pflog0`
- `TailscaleNode` 的 rc.d script 生成
- PPPoE 共存時,dnsmasq 的 rc.d ordering 會排在 `mpd5` 之後
- Static DS-Lite gif tunnel 生成
- 從 static AFTR IPv6、AFTR FQDN、或 delegated-address local source 動態套用 DS-Lite

FreeBSD 不使用 Linux 專用的 nftables、conntrack、iproute2。`Package` 範例宣告 FreeBSD native 替代項:`pf` 與 `pflog0` 來自 base system,PPPoE 使用 `mpd5`,DS-Lite 使用 `ifconfig gif`,LAN DHCP/RA 使用 dnsmasq,WireGuard、Tailscale、strongSwan 使用 ports 套件。

| 分類 | 套件 |
| --- | --- |
| Runtime | `dnsmasq`, `wireguard-tools`, `tailscale`, `strongswan`, `mpd5` |
| Diagnostics | `bind-tools`, `tcpdump` |
| Base system | `ifconfig`, `sysctl`, `service`, `sysrc`, `netstat`, `sockstat`, `ping`, `traceroute` |

`routerd render freebsd --out-dir <dir>` 會產生:

- `rc.conf.d-routerd`
- `dhclient.conf`
- `mpd5.conf`
- `pf.conf`
- `dnsmasq.conf`
- `rc.d-*`

`routerd apply` 會安裝產生的 `pf.conf`,用 `pfctl -nf` 驗證,再用 `pfctl -f` 套用。dnsmasq 會先以 `dnsmasq --test` 驗證再啟動。生成的 rc.d scripts 會以 `service <name> onestart` 啟動。靜態 `rc.conf` 生成不足以描述的動態 DS-Lite tunnel,會用 `ifconfig gif` 套用。正式導入流量前,請先用 `routerd render freebsd` 審閱與離線驗證。

## OS 抽象化實作方針

加入新的 OS 固有行為時,不要在 business logic 直接讀 `runtime.GOOS`。請使用 `pkg/platform` 層 (`platform.Features`) 或 Go build tags 明確界線。對不在支援範圍的 OS,優先在 validation 或 planning 階段明確報錯,不要等到執行時才出現意外失敗。
