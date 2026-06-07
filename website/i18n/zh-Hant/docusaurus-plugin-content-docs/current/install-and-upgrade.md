---
title: 安裝與升級
---

# 安裝與升級

![Diagram showing routerd install and upgrade from release archive download and install.sh through first router.yaml validation, dry-run apply, serve mode, preserved config and state, and uninstall](/img/diagrams/install-and-upgrade.png)

透過發布封存檔將 routerd 安裝至路由器主機。
封存檔包含執行檔、服務範本、設定範例及安裝程式。
路由器主機上不需要 Go 或 Makefile。

## 快速安裝

從 [GitHub Releases](https://github.com/imksoo/routerd/releases) 取得符合您作業系統與架構的封存檔。

Linux amd64：

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

Linux arm64 請使用 `linux-arm64` 封存檔。

FreeBSD amd64：

```sh
fetch https://github.com/imksoo/routerd/releases/latest/download/routerd-freebsd-amd64.tar.gz
fetch https://github.com/imksoo/routerd/releases/latest/download/routerd-freebsd-amd64.tar.gz.sha256
cat routerd-freebsd-amd64.tar.gz.sha256
sha256 routerd-freebsd-amd64.tar.gz
tar -xzf routerd-freebsd-amd64.tar.gz
sudo ./install.sh
```

FreeBSD arm64 請使用 `freebsd-arm64` 封存檔。
最新發布也提供附版本號的封存檔，格式如 `routerd-vYYYYMMDD.HHmm-linux-amd64.tar.gz`。
若需固定於特定版本，請使用附版本號的封存檔。

Linux 封存檔包含以 `CGO_ENABLED=0` 靜態連結的 routerd 二進位檔，
因此不依賴部署目標主機的 glibc 版本。
`dnsmasq`、`nft`、`ip`、`conntrack`、`tcpdump` 等執行時期工具，
仍由 `install.sh` 負責安裝或確認。

若主機需要以 native nDPI 進行應用程式識別，請另外取得對應的
`routerd-ndpi-agent-libndpi-linux-amd64.tar.gz`，並在一般封存檔的安裝流程中明確套用。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-ndpi-agent-libndpi-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-ndpi-agent-libndpi-linux-amd64.tar.gz.sha256
sha256sum -c routerd-ndpi-agent-libndpi-linux-amd64.tar.gz.sha256
sudo ./install.sh --with-ndpi \
  --with-ndpi-archive ./routerd-ndpi-agent-libndpi-linux-amd64.tar.gz
```

加上 `--with-ndpi` 時，安裝後的 `routerd-ndpi-agent` 若未回傳 `libndpiLoaded: true`，
安裝程序即會失敗。此設計確保靜態回退代理不會在未實際支援 native nDPI 的情況下靜默通過。

`install.sh` 會自動判斷是全新安裝還是升級。
它會將執行檔放置於 `/usr/local/sbin`，並安裝服務範本。
同時會建立 `/usr/local/etc/routerd/router.yaml.sample`，
但不會覆寫現有的 `/usr/local/etc/routerd/router.yaml`。

## 使用 Live ISO 試用

發布頁面也提供以 Alpine 為基礎的可開機 Live ISO。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-live.iso
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-live.iso.sha256
sha256sum -c routerd-live.iso.sha256
```

將 ISO 掛載至 Proxmox VE 的測試 VM 並開機。
主控台會顯示 routerd 的初始設定步驟。
以 root 登入後，可啟動相同的 `install.sh configure` 精靈。
ISO 適合示範或短時間試用。
若要作為正式路由器使用，請從發布封存檔安裝至磁碟。

Live ISO 同時啟用視訊主控台與序列主控台。
在 Proxmox VE 中，請新增序列插槽，並以 `qm terminal` 連線。

```sh
qm create 200 \
  --name routerd-live-demo \
  --memory 1536 \
  --cores 2 \
  --ostype l26 \
  --serial0 socket \
  --vga serial0 \
  --boot order=ide2 \
  --ide2 local:iso/routerd-live.iso,media=cdrom \
  --net0 virtio,bridge=vmbr0 \
  --net1 virtio,bridge=vmbr490
qm start 200
qm terminal 200
```

測試 DHCP 或 RA 時，請在 `net1` 使用隔離的 LAN 橋接。
序列主控台設定為 115200 8N1。
精靈以純文字顯示，因此無論使用 `qm terminal`、Framebuffer 主控台或最小化終端機，操作體驗均相同。

Live ISO 有兩種操作模式：

- **暫時示範模式：** 不選取 USB 儲存裝置。
  設定與日誌保存於 RAM，重新開機後消失。
- **持久路由器模式：** 在精靈中選取 USB 分割區。
  精靈會將 `router.yaml` 儲存至 USB 裝置。
  下次開機時，ISO 會掛載 USB 裝置並還原設定，自動套用。

持久模式下，USB 分割區需標記為 `ROUTERD`。
若有多個可卸除式裝置，可在核心參數中指定 `routerd.usb=/dev/sdX1`。
輔助工具以 `blkid` 辨別 `ext4`、`vfat`、`exfat`。
預設以 `async,noatime` 掛載。
僅在明確需要同步寫入時，才指定 `routerd.usb_mount=sync`。

日誌暫存於 `/run/routerd/logs` 的 tmpfs。
精靈可啟用每日一次的寫出作業，
將設定、狀態快照及壓縮日誌封存複製至 USB 裝置。
tmpfs 日誌上限預設為 100 MiB，
超出上限時，依序刪除較舊的日誌檔案。

安全卸除 USB 時，請執行：

```sh
/usr/share/routerd/live-persistence.sh flush
/usr/share/routerd/live-persistence.sh umount
```

有關部署位置、掛載選項及 Alpine `lbu` 的行為，
請參閱 [Operations → USB 持久化](./operations/usb-persistence)。

也提供附版本號的 ISO，格式如 `routerd-live-vYYYYMMDD.HHmm.iso`。

## 執行時期相依套件

預設情況下，`install.sh` 會安裝已知的 OS 套件。
若只要查看套件清單，請執行：

```sh
./install.sh --list-deps
```

若以其他機制管理套件，可停用自動安裝：

```sh
sudo ./install.sh --no-install-deps
```

也可以只安裝相依套件：

```sh
sudo ./install.sh --deps-only
```

Tailscale 為選用項目，安裝時請加上 `--with-tailscale`：

```sh
sudo ./install.sh --with-tailscale
```

### Debian / Ubuntu

安裝程式使用 `apt-get` 安裝以下套件：

```text
ca-certificates curl dnsmasq-base nftables wireguard-tools chrony bind9-dnsutils tcpdump cron jq ppp pppoe conntrack iproute2 iputils-ping iputils-tracepath net-tools kmod radvd strongswan-swanctl iptables
```

### Fedora 系

安裝程式使用 `dnf` 安裝以下套件：

```text
ca-certificates curl dnsmasq nftables wireguard-tools chrony bind-utils tcpdump cronie jq ppp rp-pppoe conntrack-tools iproute iputils traceroute kmod radvd strongswan iptables
```

### Arch 系

安裝程式使用 `pacman` 安裝以下套件：

```text
ca-certificates curl dnsmasq nftables wireguard-tools chrony bind tcpdump cronie jq ppp rp-pppoe conntrack-tools iproute2 iputils traceroute kmod radvd strongswan iptables
```

### Alpine

安裝程式使用 `apk` 安裝以下套件：

```text
alpine-conf ca-certificates curl dnsmasq nftables wireguard-tools chrony bind-tools tcpdump cronie jq ppp ppp-pppoe conntrack-tools iproute2 iputils iputils-tracepath kmod radvd strongswan iptables util-linux e2fsprogs dosfstools exfatprogs
```

`alpine-conf` 提供 `lbu`。
routerd 在 Live ISO 中使用 `lbu` 將路由器設定及選定的本地狀態儲存至 USB 媒體。

### FreeBSD

安裝程式使用 `pkg` 安裝以下套件：

```text
ca_root_nss curl dnsmasq wireguard-tools mpd5 bind-tools tcpdump jq chrony strongswan
```

FreeBSD 的 `pf`、`ifconfig`、`route`、`sysctl`、`service`、`sysrc`、`cron`、
`netstat`、`sockstat`、`ping`、`traceroute` 均為基本系統功能，
不透過套件安裝，僅確認指令是否存在。

### NixOS

在 NixOS 上，套件狀態應保留在 NixOS 設定中。
`install.sh` 偵測到 NixOS 時，不會執行 `nix-env`，而是輸出警告。
請在 NixOS 設定或 routerd 的 `Package` 資源中宣告套件。
發布安裝程式可將 `/usr/local/sbin/routerd` 執行檔放置到位，
但在 NixOS 上不會安裝、啟用或重啟 systemd 單元。
routerd 服務請透過 NixOS module 以宣告式方式管理。

## 升級

解壓新版封存檔，執行相同的安裝程式即可：

```sh
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

若 `/usr/local/sbin/routerd` 已存在，安裝程式會切換為升級模式。
此時會顯示舊版與新版的 `routerd --version`，
取代執行檔與服務範本，同時保留設定與狀態。
若 routerd 服務正在執行，則會重新啟動。
在 systemd 主機上，安裝程式會等待重啟後的 `routerd.service` 狀態插槽就緒，
待 routerd 管理的單元檔更新穩定後，僅重啟需要更新的 routerd 輔助服務。
僅在輔助程式執行的是已刪除的升級前二進位，或輔助程式啟動後單元檔有更新時，才會重啟。
若 `/etc/systemd/system/routerd.service` 已由 routerd 設定管理，
則不以封存檔範本覆寫，保留該單元。

被取代的檔案會備份為 `*.backup.YYYYMMDDHHMMSS`。
中途失敗時，會從暫時備份中還原。

若 routerd 本身將 `routerd.service` 作為產生的服務成品資源進行管理，
對單元檔的變更會謹慎處理。
套用過程中不會直接重啟自身，而是透過 `systemd-run` 預排一個略有延遲的自我重啟。
若同一設定中包含 VRRP 或 ingress 服務資源，
產生的 `routerd.service` 會自動加入 keepalived 所需的可寫路徑與 capability。
BGP 透過本地 gRPC Unix 插槽控制長期存活的 `routerd-bgp` 常駐程式，
因此不需要 FRR group 或 FRR 執行時目錄。

常用選項：

```sh
sudo ./install.sh --no-restart
sudo ./install.sh --dry-run
sudo ./install.sh --verbose
sudo ./install.sh --no-config-update
```

## 安裝位置

| 項目 | Linux | FreeBSD |
| --- | --- | --- |
| 設定 | `/usr/local/etc/routerd/router.yaml` | `/usr/local/etc/routerd/router.yaml` |
| 設定範例 | `/usr/local/etc/routerd/router.yaml.sample` | `/usr/local/etc/routerd/router.yaml.sample` |
| 執行檔 | `/usr/local/sbin` | `/usr/local/sbin` |
| 服務範本 | `/etc/systemd/system/routerd.service` | `/usr/local/etc/rc.d/routerd` |
| 執行時期插槽 | `/run/routerd` | `/var/run/routerd` |
| 持久狀態 | `/var/lib/routerd` | `/var/db/routerd` |

安裝程式不會刪除以下狀態：

- `/usr/local/etc/routerd/router.yaml`
- `/var/lib/routerd`
- `/var/db/routerd`
- `/run/routerd`
- `/var/run/routerd`
- `/var/log/otelcol`

## 初始設定

初次試用時，可使用內建的初始設定精靈：

```sh
sudo ./install.sh configure
```

精靈會依序詢問 WAN 介面、LAN 介面、LAN 位址、
LAN 服務、管理路徑的放置位置，以及選用的 USB 持久化。
產生的候選設定儲存於 `/usr/local/etc/routerd/router.yaml.configure`，
若已有現有設定，則顯示差異。
確認後，安裝至 `/usr/local/etc/routerd/router.yaml`，
接著依序執行 `routerd validate`、`routerd plan`、`routerd apply --once`。

自動化時，可透過環境變數傳遞值以略過提問：

```sh
sudo ROUTERD_WAN_INTERFACE=ens18 \
  ROUTERD_LAN_INTERFACE=ens19 \
  ROUTERD_LAN_ADDRESS=192.168.10.1/24 \
  ROUTERD_LAN_CIDR=192.168.10.0/24 \
  ROUTERD_MGMT_MODE=lan \
  ROUTERD_ENABLE_USB_PERSISTENCE=no \
  ./install.sh configure --non-interactive --yes
```

在 Live ISO 上使用 USB 持久化時，請指定以下值：

```sh
sudo ROUTERD_ENABLE_USB_PERSISTENCE=yes \
  ROUTERD_USB_DEVICE=/dev/sdb1 \
  ROUTERD_USB_FLUSH=yes \
  ROUTERD_LOG_TMPFS_LIMIT=100M \
  ./install.sh configure --non-interactive --yes
```

若只需產生 YAML 檔案而不套用，請使用 `--no-apply`：

```sh
sudo ./install.sh configure --no-apply
```

也可以手動設定。
複製設定範例，編輯介面名稱等項目：

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 /usr/local/etc/routerd/router.yaml.sample /usr/local/etc/routerd/router.yaml
sudo vi /usr/local/etc/routerd/router.yaml
```

驗證並確認計畫：

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run
```

確認管理路徑安全後再套用：

```sh
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

單次套用正常完成後，啟動服務：

```sh
sudo systemctl enable --now routerd.service
```

在 FreeBSD 上請執行：

```sh
sudo sysrc routerd_enable=YES
sudo service routerd start
```

## 解除安裝

發布封存檔也包含 `uninstall.sh`。
預設情況下，它會刪除執行檔、服務範本及執行時期檔案，保留設定與狀態。

```sh
sudo ./uninstall.sh --yes
```

若要擴大刪除範圍，請明確指定：

```sh
sudo ./uninstall.sh --yes --purge-config
sudo ./uninstall.sh --yes --purge-state
sudo ./uninstall.sh --yes --all
```

使用 `--dry-run` 可僅確認將被刪除的內容。

## 開發者工作流程

Makefile 供開發用途使用，
包含測試、建置、Schema 產生、設定範例驗證、網站建置及發布封存檔製作。

```sh
make test
make check-schema
make validate-example
make website-build
make dist ROUTERD_OS=linux GOARCH=amd64 VERSION="$(git describe --tags --abbrev=0)"
```

Makefile 不作為使用者的安裝路徑。
標準安裝方式為發布封存檔搭配 `install.sh`。
