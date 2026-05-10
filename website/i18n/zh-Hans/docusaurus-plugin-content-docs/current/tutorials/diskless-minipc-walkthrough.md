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
推荐使用 `ext4`。`vfat` 和 `exfat` 也可用于简单的可移动介质。FAT32 通常会被 `blkid` 显示为 `vfat`。

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
在 Proxmox VE 中，交互式向导通常更适合通过 `qm terminal` 查看。
VGA 截图主要作为启动证据，实际输入和结果请看下面的 serial console 文本。

![Alpine boot messages](/img/iso-boot/iso-boot-02-alpine-boot.png)

## 运行配置向导

以 `root` 登录。live ISO 会启动 `install.sh configure`。

![routerd live login and message of the day](/img/iso-boot/iso-boot-03-login-motd.png)

serial console 会显示类似下面的启动和向导入口：

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

向导会询问 WAN、LAN、LAN 地址、DHCP、DNS、NTP、RA、firewall、NAT44、
管理接口，以及 USB persistence。

![WAN setup in the routerd live wizard](/img/iso-boot/iso-boot-04-wizard-wan.png)

![LAN setup in the routerd live wizard](/img/iso-boot/iso-boot-05-wizard-lan.png)

实机验证时的 serial console 输入如下：

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

选择 USB persistence 后，指定 USB 分区。若标签是 `ROUTERD`，通常会自动列出。
live helper 会用 `blkid` 检测 `ext4`、`vfat` 和 `exfat`。选中的分区会挂载到 `/media/routerd-usb`，保存的配置路径是 `/media/routerd-usb/routerd/router.yaml`，不是 `/mnt/routerd/router.yaml`。

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
serial log 中应有如下状态：

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

## 测试 LAN client

```sh
dig @192.168.10.1 www.google.com A +short
curl -4 https://www.google.com/generate_204
```

PVE 验证中，临时 network namespace 连接到隔离 LAN bridge。
client 从 routerd 取得 lease，并通过 routerd NAT44 访问外网：

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

## 重启测试

保留 USB，重启 mini PC。live ISO 会按记录设备、`routerd.usb=`、`ROUTERD` 标签的顺序查找 USB。找到后会挂载到 `/media/routerd-usb`，还原 `/media/routerd-usb/routerd/router.yaml`，准备 `/run/routerd/logs`，并自动套用配置。如果没有还原到配置，且 `/usr/local/etc/routerd/router.yaml` 也不存在，系统会启动配置向导。

日志先留在 tmpfs。每天一次的 flush job 会把配置、状态快照和压缩日志写回 USB。

手动 flush：

```sh
/usr/share/routerd/live-persistence.sh flush
```

![USB persistence flush](/img/iso-boot/iso-boot-08-usb-flush.png)
