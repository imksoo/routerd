---
title: 無磁碟 mini PC 教學
---

# 無磁碟 mini PC 教學

本教學說明如何用 routerd live ISO，把小型 x86 mini PC 做成不需要內建磁碟的路由器。
設定會儲存在 USB，日誌先寫入 RAM，再每天一次壓縮寫回 USB。

![無磁碟 mini PC 流程](/img/routerd-diskless-minipc.svg)

## 需要的東西

- 至少兩個網路介面的 mini PC
- 用於保存 routerd 設定的 USB 隨身碟
- 最新的 `routerd-live.iso`
- 主控台存取
- 可用 DHCPv4 或靜態位址的 WAN
- LAN switch 或隔離的測試 bridge

## 準備 USB

建立一個分割區，格式化成 live ISO 可掛載的檔案系統，並把標籤設為 `ROUTERD`。
建議使用 `ext4`。`vfat` 和 `exfat` 也可用於簡單的可移動媒體。FAT32 通常會被 `blkid` 顯示為 `vfat`。

```sh
sudo mkfs.ext4 -L ROUTERD /dev/sdX1
```

請把 `/dev/sdX1` 換成實際 USB 分割區。

## 啟動 live ISO

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-live.iso
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-live.iso.sha256
sha256sum -c routerd-live.iso.sha256
```

在 Proxmox VE 中，可以使用 serial console：

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

早期測試 DHCP 或 RA 時，請使用隔離的 LAN bridge。

![routerd live boot menu](/img/iso-boot/iso-boot-01-grub.png)

ISO 同時啟用 video console 和 serial console。
在 Proxmox VE 中，互動式精靈通常更適合透過 `qm terminal` 查看。
VGA 截圖主要作為啟動證據，實際輸入和結果請看下面的 serial console 文字。

![Alpine boot messages](/img/iso-boot/iso-boot-02-alpine-boot.png)

## 執行設定精靈

以 `root` 登入。live ISO 會啟動 `install.sh configure`。

![routerd live login and message of the day](/img/iso-boot/iso-boot-03-login-motd.png)

serial console 會顯示類似下面的啟動和精靈入口：

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

精靈會詢問 WAN、LAN、LAN 位址、DHCP、DNS、NTP、RA、firewall、NAT44、
管理介面，以及 USB persistence。

![WAN setup in the routerd live wizard](/img/iso-boot/iso-boot-04-wizard-wan.png)

![LAN setup in the routerd live wizard](/img/iso-boot/iso-boot-05-wizard-lan.png)

實機驗證時的 serial console 輸入如下：

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

選擇 USB persistence 後，指定 USB 分割區。若標籤是 `ROUTERD`，通常會自動列出。
live helper 會用 `blkid` 偵測 `ext4`、`vfat` 和 `exfat`。選中的分割區會掛載到 `/media/routerd-usb`，保存的設定路徑是 `/media/routerd-usb/routerd/router.yaml`，不是 `/mnt/routerd/router.yaml`。

## 確認套用

精靈會寫入：

```text
/usr/local/etc/routerd/router.yaml
```

然後執行驗證、計畫與一次性套用。

![Wizard summary and first apply](/img/iso-boot/iso-boot-06-wizard-summary.png)

```sh
routerctl status
```

![routerctl status after first apply](/img/iso-boot/iso-boot-07-routerctl-status.png)

狀態應為 `Healthy`。
serial log 中應有如下狀態：

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

## 測試 LAN client

```sh
dig @192.168.10.1 www.google.com A +short
curl -4 https://www.google.com/generate_204
```

PVE 驗證中，臨時 network namespace 連接到隔離 LAN bridge。
client 從 routerd 取得 lease，並透過 routerd NAT44 存取外網：

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

## 重新啟動測試

保留 USB，重新啟動 mini PC。live ISO 會按記錄裝置、`routerd.usb=`、`ROUTERD` 標籤的順序尋找 USB。找到後會掛載到 `/media/routerd-usb`，還原 `/media/routerd-usb/routerd/router.yaml`，準備 `/run/routerd/logs`，並自動套用設定。如果沒有還原到設定，且 `/usr/local/etc/routerd/router.yaml` 也不存在，系統會啟動設定精靈。

日誌先留在 tmpfs。每天一次的 flush job 會把設定、狀態快照與壓縮日誌寫回 USB。

手動 flush：

```sh
/usr/share/routerd/live-persistence.sh flush
```

![USB persistence flush](/img/iso-boot/iso-boot-08-usb-flush.png)
