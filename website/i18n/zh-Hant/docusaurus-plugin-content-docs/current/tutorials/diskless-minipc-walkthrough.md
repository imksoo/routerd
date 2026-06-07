---
title: 無碟 mini PC 教學
---

# 無碟 mini PC 教學

![從 live ISO boot、USB persistence、routerd wizard configuration 到 validation 的 diskless mini PC tutorial flow](/img/diagrams/tutorial-diskless-minipc-walkthrough.png)

本教學說明如何將小型 x86 mini PC 在不安裝 OS 至內建磁碟的情況下設定為路由器。
從 routerd Live ISO 開機，將設定儲存至 USB。
日誌暫存於 RAM，每日一次以壓縮封存的形式寫出至 USB。

![無碟 mini PC 流程](/img/routerd-diskless-minipc.svg)

## 準備物品

- 具備兩個以上網路介面的 mini PC
- 用於 routerd 持久化的 USB 隨身碟
- 最新的 `routerd-live.iso`
- 主控台存取
- 可使用 DHCPv4 或靜態位址的 WAN
- LAN 交換器或隔離的測試 bridge

## 1. 準備 USB 隨身碟

建立一個分割區，並以 Live ISO 可掛載的檔案系統格式化。
預設建議使用 `ext4`。
一般可移動媒體也可使用 `vfat` 或 `exfat`。
請將標籤設為 `ROUTERD`，以便 ISO 自動偵測。
FAT32 通常在 `blkid` 中顯示為 `vfat`。
若為 routerd 專用的 USB 隨身碟，`ext4` 最易操作。

以下為在 Linux 終端機的操作範例。

```sh
sudo mkfs.ext4 -L ROUTERD /dev/sdX1
```

請將 `/dev/sdX1` 替換為實際的 USB 分割區。
請注意勿誤格式化其他裝置。

## 2. 開機至 Live ISO

從固定 URL 取得 ISO。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-live.iso
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-live.iso.sha256
sha256sum -c routerd-live.iso.sha256
```

從 ISO 開機 mini PC。
同一映像檔支援視訊主控台與序列主控台。

以下為在 Proxmox VE 的操作範例。

```sh
qm create 200 \
  --name routerd-live-demo \
  --memory 1536 \
  --cores 2 \
  --ostype l26 \
  --serial0 socket \
  --vga std \
  --boot order=ide2 \
  --ide2 local:iso/routerd-live.iso,media=cdrom \
  --net0 virtio,bridge=vmbr0 \
  --net1 virtio,bridge=vmbr490
qm start 200
qm terminal 200
```

DHCP 或 RA 的初期測試請使用隔離的 LAN bridge。

![routerd live boot menu](/img/iso-boot/iso-boot-01-grub.png)

ISO 同時啟用視訊主控台與序列主控台。
在 Proxmox VE 中，互動式精靈通常透過 `qm terminal` 比較容易閱讀。
VGA 的畫面擷取作為開機軌跡使用，實際的輸入與結果請透過下方的
序列主控台日誌確認。

![Alpine boot messages](/img/iso-boot/iso-boot-02-alpine-boot.png)

## 3. 執行精靈

以 `root` 登入後，Live ISO 會啟動初始設定精靈。

![routerd live login and message of the day](/img/iso-boot/iso-boot-03-login-motd.png)

序列主控台會顯示如下的 Live ISO 說明與精靈開始畫面。

```text
Welcome to Alpine Linux 3.23
Kernel 6.18.22-0-lts on x86_64 (/dev/ttyS0)

localhost login: root
routerd live v20260510.1811

Run the setup wizard:
  /usr/share/routerd/install.sh configure

Starting routerd setup wizard. Press Ctrl+C to skip.
routerd initial configuration wizard

Available interfaces:
  - lo
  - eth0
  - eth1
```

精靈會確認以下項目。

- 路由器名稱
- WAN 介面
- WAN IPv4 模式
- LAN 介面
- LAN 位址
- DHCPv4、DNS、NTP、RA、防火牆、NAT44
- 管理路徑的放置位置
- USB 持久化

![WAN setup in the routerd live wizard](/img/iso-boot/iso-boot-04-wizard-wan.png)

![LAN setup in the routerd live wizard](/img/iso-boot/iso-boot-05-wizard-lan.png)

以下為實機驗證時的序列主控台日誌。

```text
Router name [routerd-router]: routerd-live-router-test
WAN interface: eth0
WAN IPv4 mode (dhcp/static) [dhcp]: dhcp
Default DNS upstreams when DHCP DNS is unavailable [1.1.1.1]: 1.1.1.1
LAN interface: eth1
LAN address/CIDR [192.168.10.1/24]: 192.168.99.1/24
LAN client CIDR [192.168.99.0/24]: 192.168.99.0/24
Enable DHCPv4 server? (yes/no) [yes]: yes
DHCPv4 pool start [192.168.99.100]:
DHCPv4 pool end [192.168.99.200]:
Enable DHCPv6 stateless service? (yes/no) [no]: no
Enable IPv6 RA? (yes/no) [no]: no
Enable DNS resolver? (yes/no) [yes]: yes
Enable NTP server? (yes/no) [yes]: yes
Enable 3-role firewall? (yes/no) [yes]: yes
Enable NAT44 from LAN to WAN? (yes/no) [yes]: yes
Management placement (separate/lan) [lan]: lan
Save config to USB for diskless persistence? (yes/no) [no]: no
generated candidate config: /usr/local/etc/routerd/router.yaml.configure
Install this config as router.yaml? (yes/no) [no]: yes
```

詢問 USB 持久化時，選擇 `yes` 並指定 USB 分割區。
分割區若有 `ROUTERD` 標籤，會自動顯示為候選項目。

非短時間的測試時，請啟用每日一次的 USB 寫出工作。
預設日誌緩衝為放置於 `/run/routerd/logs` 的 100 MiB。

Live 輔助程式使用 `blkid` 判斷 `ext4`、`vfat`、`exfat`。
USB 持久化為減少對 USB 的寫入，預設以 `async,noatime` 掛載。
僅在特定測試需要同步寫入時，才在核心命令列加入 `routerd.usb_mount=sync`。

選定的 USB 分割區掛載至 `/media/routerd-usb`。
儲存的設定檔路徑為 `/media/routerd-usb/routerd/router.yaml`，
而非 `/mnt/routerd/router.yaml`。

## 4. 確認初次套用

確認完成後，精靈會寫出以下檔案。

```text
/usr/local/etc/routerd/router.yaml
```

之後執行以下指令。

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

![Wizard summary and first apply](/img/iso-boot/iso-boot-06-wizard-summary.png)

確認狀態。

```sh
routerctl status
```

![routerctl status after first apply](/img/iso-boot/iso-boot-07-routerctl-status.png)

phase 變為 `Healthy` 即表示成功。
序列主控台日誌中應出現如下狀態。

```json
{
  "apiVersion": "control.routerd.net/v1alpha1",
  "kind": "Status",
  "status": {
    "phase": "Healthy",
    "generation": 1,
    "resourceCount": 14
  }
}
```

## 5. 測試 LAN 用戶端

將用戶端連接至 LAN 介面或測試 bridge。

用戶端應收到以下內容。

- 來自 DHCPv4 pool 的 IPv4 位址
- 指向 routerd 的預設路由
- 指向 routerd 的 DNS 伺服器
- 若已啟用，指向 routerd 的 NTP 伺服器

基本確認如下。

```sh
dig @192.168.10.1 www.google.com A +short
curl -4 https://www.google.com/generate_204
```

若更改了 LAN prefix，請對應調整位址。

PVE 驗證中，將暫時的 network namespace 連接至隔離的 LAN bridge。
用戶端從 routerd 取得租約，並透過 routerd 的 NAT44 與外部通訊。

```text
inet 192.168.99.186/24
default via 192.168.99.1 dev veth-rtest

dig @192.168.99.1 www.google.com A +short
142.251.156.119
142.251.150.119
142.251.151.119
...

curl -4 https://www.google.com/generate_204
http_code=204 remote_ip=142.251.156.119 time_total=0.024397

curl http://192.168.99.1:8080/
http_code=200 remote_ip=192.168.99.1 time_total=0.000537
```

## 6. 重新開機確認持久化

保持 USB 隨身碟連接，重新開機 mini PC。

開機時，Live ISO 會執行以下步驟。

1. 依已記錄裝置、`routerd.usb=`、`ROUTERD` 標籤的順序搜尋 USB 裝置。
2. 將 USB 裝置掛載至 `/media/routerd-usb`。
3. 還原 `/media/routerd-usb/routerd/router.yaml`。
4. 以 tmpfs 準備 `/run/routerd/logs`。
5. 套用路由器設定。
6. 啟動 Live routerd 常駐程式。

登入後確認。

```sh
routerctl status
```

不重新執行精靈即可收斂則表示成功。
若設定未還原，且 `/usr/local/etc/routerd/router.yaml` 也不存在，
則會啟動設定精靈。

## 7. 日誌持久化的機制

日誌首先寫入 RAM。

```text
/run/routerd/logs
```

每日寫出工作會將以下內容複製至 USB。

- 目前的 `router.yaml`
- routerd 的狀態快照
- 壓縮日誌封存

這樣可避免持續寫入 USB 快閃記憶體。
超過 tmpfs 上限時，從最舊的檔案開始刪除。

手動寫出時，執行以下指令。

```sh
/usr/share/routerd/live-persistence.sh flush
```

![USB persistence flush](/img/iso-boot/iso-boot-08-usb-flush.png)

實際拔除 USB 裝置前，請先執行 flush 與 unmount。

```sh
/usr/share/routerd/live-persistence.sh flush
/usr/share/routerd/live-persistence.sh umount
```

未 unmount 即拔除時，routerd 仍會繼續在 RAM 上運作。
系統會輸出警告，新的日誌在 USB 回復前暫存於 tmpfs。

## 疑難排解

### USB 隨身碟未出現在候選清單

從 shell 確認分割區。

```sh
blkid
lsblk -f
```

若有需要，透過核心引數明確指定。

```text
routerd.usb=/dev/sdb1
```

### 重新開機後精靈再次出現

ISO 未能找到已儲存的設定。
掛載 USB 裝置後確認。

```sh
mount /dev/sdX1 /media/routerd-usb
ls -l /media/routerd-usb/routerd/router.yaml
```

### 重新開機後沒有日誌

日誌暫存於 RAM。
每日寫出工作或手動 flush 執行後，才會保留於 USB。

### LAN 用戶端未取得位址

確認精靈中選擇的 LAN 介面。

```sh
routerctl status --json
ip addr
```

在 Proxmox VE 進行測試時，請確認用戶端與 routerd 的 LAN NIC 連接至同一個隔離 bridge。
