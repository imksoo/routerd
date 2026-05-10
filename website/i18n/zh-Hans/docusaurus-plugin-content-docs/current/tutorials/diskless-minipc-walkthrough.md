---
title: 无磁盘 mini PC 教程
---

# 无磁盘 mini PC 教程

本教程说明如何用 routerd live ISO，把小型 x86 mini PC 做成不需要内置磁盘的路由器。
配置保存在 USB，日志先写入 RAM，再每天一次压缩写回 USB。

![无磁盘 mini PC 流程](/img/routerd-diskless-minipc.svg)

## 需要的东西

- 至少两个网络接口的 mini PC
- 用于保存 routerd 配置的 USB 盘
- 最新的 `routerd-live.iso`
- 控制台访问
- 可用 DHCPv4 或静态地址的 WAN
- LAN switch 或隔离的测试 bridge

## 准备 USB

创建一个分区，格式化成 live ISO 可挂载的文件系统，并把标签设为 `ROUTERD`。

```sh
sudo mkfs.ext4 -L ROUTERD /dev/sdX1
```

请把 `/dev/sdX1` 换成实际 USB 分区。

## 启动 live ISO

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

早期测试 DHCP 或 RA 时，请使用隔离的 LAN bridge。

![routerd live boot menu](/img/iso-boot/iso-boot-01-grub.png)

ISO 同时启用 video console 和 serial console。

![Alpine boot messages](/img/iso-boot/iso-boot-02-alpine-boot.png)

## 运行配置向导

以 `root` 登录。live ISO 会启动 `install.sh configure`。

![routerd live login and message of the day](/img/iso-boot/iso-boot-03-login-motd.png)

向导会询问 WAN、LAN、LAN 地址、DHCP、DNS、NTP、RA、firewall、NAT44、
管理接口，以及 USB persistence。

![WAN setup in the routerd live wizard](/img/iso-boot/iso-boot-04-wizard-wan.png)

![LAN setup in the routerd live wizard](/img/iso-boot/iso-boot-05-wizard-lan.png)

选择 USB persistence 后，指定 USB 分区。若标签是 `ROUTERD`，通常会自动列出。

## 确认套用

向导会写入：

```text
/usr/local/etc/routerd/router.yaml
```

然后执行验证、计划和一次性套用。

![Wizard summary and first apply](/img/iso-boot/iso-boot-06-wizard-summary.png)

```sh
routerctl status
```

![routerctl status after first apply](/img/iso-boot/iso-boot-07-routerctl-status.png)

状态应为 `Healthy`。

## 测试 LAN client

```sh
dig @192.168.10.1 www.google.com A +short
curl -4 https://www.google.com/generate_204
```

## 重启测试

保留 USB，重启 mini PC。live ISO 会挂载 USB、还原 `router.yaml`、
准备 `/run/routerd/logs`，并自动套用配置。

日志先留在 tmpfs。每天一次的 flush job 会把配置、状态快照和压缩日志写回 USB。

手动 flush：

```sh
/usr/share/routerd/live-persistence.sh flush
```

![USB persistence flush](/img/iso-boot/iso-boot-08-usb-flush.png)
