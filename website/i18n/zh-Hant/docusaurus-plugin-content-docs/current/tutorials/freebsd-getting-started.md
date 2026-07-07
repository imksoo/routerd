---
title: 從 FreeBSD 開始
---

# 從 FreeBSD 開始

![從 release archive install 到 rc.d、rc.conf.d、pf、dnsmasq、mpd5 render 與 apply validation 的 FreeBSD getting started flow](/img/diagrams/tutorial-freebsd-getting-started.png)

FreeBSD 使用與 Ubuntu 相同的 routerd 資源模型。
但產生的主機成果物對應 FreeBSD 的機制。
routerd 負責處理 `rc.conf.d`、`rc.d` script、`pf.conf`、`dhclient.conf`、
dnsmasq 設定、`mpd5.conf`，以及 DS-Lite 用的動態 `ifconfig gif` 操作。

本教學以 FreeBSD 14 系為前提。
發布安裝程式的安裝位置為 `/usr/local` 之下。
參考設定請使用 `examples/freebsd-edge.yaml`。

## 1. 從發布封存檔安裝

從 [GitHub Releases](https://github.com/imksoo/routerd/releases) 取得 FreeBSD 用的
封存檔，並在路由器上執行隨附的安裝程式。

```sh
fetch https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-freebsd-amd64.tar.gz
fetch https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-freebsd-amd64.tar.gz.sha256
cat routerd-freebsd-amd64.tar.gz.sha256
sha256 routerd-freebsd-amd64.tar.gz
tar -xzf routerd-freebsd-amd64.tar.gz
sudo ./install.sh
```

`install.sh` 會安裝 FreeBSD 通常所需的套件。
對象為 `ca_root_nss`、`curl`、`dnsmasq`、`wireguard-tools`、`mpd5`、
`bind-tools`、`tcpdump`、`jq`、`chrony`、`strongswan`。
同時安裝 Tailscale 時，使用 `sudo ./install.sh --with-tailscale`。
FreeBSD 的 base system 包含 `ifconfig`、`route`、`sysctl`、`service`、`sysrc`、
`pfctl`、`pflog0`、`netstat`、`sockstat`、`ping`、`traceroute`。
相依套件清單可透過 `./install.sh --list-deps` 確認。

## 2. 放置路由器設定

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 examples/freebsd-edge.yaml /usr/local/etc/routerd/router.yaml
```

套用前，請編輯介面名稱、位址與密碼。
初次操作時，請將管理用 SSH 放置於獨立介面，或事先準備 hypervisor 主控台。

## 3. 驗證並確認產生的檔案

首先驗證設定。

```sh
routerctl validate -f /usr/local/etc/routerd/router.yaml --replace
```

接著將 FreeBSD 用的成果物產生至暫存目錄。

```sh
rm -rf /tmp/routerd-freebsd-render
routerd render freebsd \
  --config /usr/local/etc/routerd/router.yaml \
  --out-dir /tmp/routerd-freebsd-render
```

主要輸出如下。

- `rc.conf.d-routerd`
- `dhclient.conf`
- `mpd5.conf`
- `pf.conf`
- `dnsmasq.conf`
- `install-packages.sh`
- `rc.d-*`

套用至實際主機前，請先確認內容。

```sh
less /tmp/routerd-freebsd-render/rc.conf.d-routerd
less /tmp/routerd-freebsd-render/pf.conf
less /tmp/routerd-freebsd-render/dnsmasq.conf
```

## 4. 了解 FreeBSD 側的角色

routerd 將資源對應至以下 FreeBSD 機制。

| 機制 | 角色 |
| --- | --- |
| `rc.conf.d-routerd` | 介面別名、轉送、複製介面、靜態路由、`pf`、`pflog`、`mpd5` 的啟用 |
| `rc.d-*` script | dnsmasq、防火牆日誌記錄器、healthcheck、Tailscale、DHCP client 等受管理常駐程式 |
| `pf.conf` | zone 過濾、受管理服務的開口、NAT、防火牆日誌 |
| `pflog0` | `routerd-firewall-logger` 讀取的防火牆日誌 |
| `dnsmasq.conf` | DHCPv4、DHCPv6、DHCP relay、RA |
| `dhclient.conf` | 接管的上游介面的 DHCPv4 client 行為 |
| `mpd5.conf` | PPPoE 的 bundle、link、認證、MTU/MRU、預設路由 |
| `ifconfig gif` | 靜態 `rc.conf` 不足時的動態 DS-Lite tunnel 套用 |

## 5. 套用

先確認計畫。

```sh
routerctl plan -f /usr/local/etc/routerd/router.yaml --replace
```

產生的檔案與計畫符合預期後，套用設定。

```sh
sudo routerctl apply -f /usr/local/etc/routerd/router.yaml --replace
```

routerd 在載入 `pf.conf` 前以 `pfctl -nf` 驗證。
dnsmasq 也在重新啟動前以 `dnsmasq --test` 驗證設定。

## 6. 確認狀態與日誌

確認 routerd 狀態。

```sh
routerctl get status
routerctl get events --limit 20
```

追蹤系統日誌。

```sh
tail -f /var/log/routerd.log
```

確認 pf 狀態。

```sh
sudo pfctl -ss -v
```

透過 `pflog0` 確認防火牆日誌。

```sh
sudo tcpdump -n -e -ttt -i pflog0
```

啟用 `FirewallEventLog` 後，routerd 會匯入 `pflog0` 的內容。
匯入的日誌可透過 `routerctl` 與 Web 管理介面確認。

## 相關項目

- [支援的平台](../platforms.md)
- [WAN 側服務](./wan-side-services.md)
- [基本防火牆](./basic-firewall.md)
