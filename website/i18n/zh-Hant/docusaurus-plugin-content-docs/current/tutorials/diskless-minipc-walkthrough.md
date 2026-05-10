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

![Alpine boot messages](/img/iso-boot/iso-boot-02-alpine-boot.png)

## 執行設定精靈

以 `root` 登入。live ISO 會啟動 `install.sh configure`。

![routerd live login and message of the day](/img/iso-boot/iso-boot-03-login-motd.png)

精靈會詢問 WAN、LAN、LAN 位址、DHCP、DNS、NTP、RA、firewall、NAT44、
管理介面，以及 USB persistence。

![WAN setup in the routerd live wizard](/img/iso-boot/iso-boot-04-wizard-wan.png)

![LAN setup in the routerd live wizard](/img/iso-boot/iso-boot-05-wizard-lan.png)

選擇 USB persistence 後，指定 USB 分割區。若標籤是 `ROUTERD`，通常會自動列出。

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

## 測試 LAN client

```sh
dig @192.168.10.1 www.google.com A +short
curl -4 https://www.google.com/generate_204
```

## 重新啟動測試

保留 USB，重新啟動 mini PC。live ISO 會掛載 USB、還原 `router.yaml`、
準備 `/run/routerd/logs`，並自動套用設定。

日誌先留在 tmpfs。每天一次的 flush job 會把設定、狀態快照與壓縮日誌寫回 USB。

手動 flush：

```sh
/usr/share/routerd/live-persistence.sh flush
```

![USB persistence flush](/img/iso-boot/iso-boot-08-usb-flush.png)
