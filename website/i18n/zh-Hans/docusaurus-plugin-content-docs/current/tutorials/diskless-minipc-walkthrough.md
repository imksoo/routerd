---
title: 无盘 mini PC 教程
---

# 无盘 mini PC 教程

![从 live ISO boot、USB persistence、routerd wizard configuration 到 validation 的 diskless mini PC tutorial flow](/img/diagrams/tutorial-diskless-minipc-walkthrough.png)

本教程说明如何将小型 x86 mini PC 在不安装 OS 至内置磁盘的情况下配置为路由器。
从 routerd Live ISO 启动，将配置保存至 USB。
日志暂存于 RAM，每日一次以压缩归档的形式写出至 USB。

![无盘 mini PC 流程](/img/routerd-diskless-minipc.svg)

## 准备物品

- 具备两个以上网络接口的 mini PC
- 用于 routerd 持久化的 USB 随身盘
- 最新的 `routerd-live.iso`
- 控制台访问
- 可使用 DHCPv4 或静态地址的 WAN
- LAN 交换机或隔离的测试 bridge

## 1. 准备 USB 随身盘

创建一个分区，并以 Live ISO 可挂载的文件系统格式化。
默认建议使用 `ext4`。
一般可移动介质也可使用 `vfat` 或 `exfat`。
请将标签设为 `ROUTERD`，以便 ISO 自动检测。
FAT32 通常在 `blkid` 中显示为 `vfat`。
若为 routerd 专用的 USB 随身盘，`ext4` 最易操作。

以下为在 Linux 终端的操作示例。

```sh
sudo mkfs.ext4 -L ROUTERD /dev/sdX1
```

请将 `/dev/sdX1` 替换为实际的 USB 分区。
请注意勿误格式化其他设备。

## 2. 启动至 Live ISO

从固定 URL 获取 ISO。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-live.iso
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-live.iso.sha256
sha256sum -c routerd-live.iso.sha256
```

从 ISO 启动 mini PC。
同一镜像文件支持视频控制台与串行控制台。

以下为在 Proxmox VE 的操作示例。

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

DHCP 或 RA 的初期测试请使用隔离的 LAN bridge。

![routerd live boot menu](/img/iso-boot/iso-boot-01-grub.png)

ISO 同时启用视频控制台与串行控制台。
在 Proxmox VE 中，交互式向导通常通过 `qm terminal` 比较容易阅读。
VGA 的画面截图作为启动轨迹使用，实际的输入与结果请通过下方的
串行控制台日志确认。

![Alpine boot messages](/img/iso-boot/iso-boot-02-alpine-boot.png)

## 3. 执行向导

以 `root` 登录后，Live ISO 会启动初始配置向导。

![routerd live login and message of the day](/img/iso-boot/iso-boot-03-login-motd.png)

串行控制台会显示如下的 Live ISO 说明与向导开始画面。

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

向导会确认以下项目。

- 路由器名称
- WAN 接口
- WAN IPv4 模式
- LAN 接口
- LAN 地址
- DHCPv4、DNS、NTP、RA、防火墙、NAT44
- 管理路径的放置位置
- USB 持久化

![WAN setup in the routerd live wizard](/img/iso-boot/iso-boot-04-wizard-wan.png)

![LAN setup in the routerd live wizard](/img/iso-boot/iso-boot-05-wizard-lan.png)

以下为实机验证时的串行控制台日志。

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

询问 USB 持久化时，选择 `yes` 并指定 USB 分区。
分区若有 `ROUTERD` 标签，会自动显示为候选项目。

非短时间的测试时，请启用每日一次的 USB 写出任务。
默认日志缓冲为放置于 `/run/routerd/logs` 的 100 MiB。

Live 辅助程序使用 `blkid` 判断 `ext4`、`vfat`、`exfat`。
USB 持久化为减少对 USB 的写入，默认以 `async,noatime` 挂载。
仅在特定测试需要同步写入时，才在内核命令行加入 `routerd.usb_mount=sync`。

选定的 USB 分区挂载至 `/media/routerd-usb`。
保存的配置文件路径为 `/media/routerd-usb/routerd/router.yaml`，
而非 `/mnt/routerd/router.yaml`。

## 4. 确认初次应用

确认完成后，向导会写出以下文件。

```text
/usr/local/etc/routerd/router.yaml
```

之后执行以下命令。

```sh
routerctl validate -f /usr/local/etc/routerd/router.yaml --replace
routerctl plan -f /usr/local/etc/routerd/router.yaml --replace
routerctl apply -f /usr/local/etc/routerd/router.yaml --replace
```

![Wizard summary and first apply](/img/iso-boot/iso-boot-06-wizard-summary.png)

确认状态。

```sh
routerctl status
```

![routerctl status after first apply](/img/iso-boot/iso-boot-07-routerctl-status.png)

phase 变为 `Healthy` 即表示成功。
串行控制台日志中应出现如下状态。

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

## 5. 测试 LAN 客户端

将客户端连接至 LAN 接口或测试 bridge。

客户端应收到以下内容。

- 来自 DHCPv4 pool 的 IPv4 地址
- 指向 routerd 的默认路由
- 指向 routerd 的 DNS 服务器
- 若已启用，指向 routerd 的 NTP 服务器

基本确认如下。

```sh
dig @192.168.10.1 www.google.com A +short
curl -4 https://www.google.com/generate_204
```

若更改了 LAN prefix，请对应调整地址。

PVE 验证中，将临时的 network namespace 连接至隔离的 LAN bridge。
客户端从 routerd 获取租约，并通过 routerd 的 NAT44 与外部通信。

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

## 6. 重新启动确认持久化

保持 USB 随身盘连接，重新启动 mini PC。

启动时，Live ISO 会执行以下步骤。

1. 依已记录设备、`routerd.usb=`、`ROUTERD` 标签的顺序搜索 USB 设备。
2. 将 USB 设备挂载至 `/media/routerd-usb`。
3. 还原 `/media/routerd-usb/routerd/router.yaml`。
4. 以 tmpfs 准备 `/run/routerd/logs`。
5. 应用路由器配置。
6. 启动 Live routerd 守护进程。

登录后确认。

```sh
routerctl status
```

不重新执行向导即可收敛则表示成功。
若配置未还原，且 `/usr/local/etc/routerd/router.yaml` 也不存在，
则会启动配置向导。

## 7. 日志持久化的机制

日志首先写入 RAM。

```text
/run/routerd/logs
```

每日写出任务会将以下内容复制至 USB。

- 当前的 `router.yaml`
- routerd 的状态快照
- 压缩日志归档

这样可避免持续写入 USB 闪存。
超过 tmpfs 上限时，从最旧的文件开始删除。

手动写出时，执行以下命令。

```sh
/usr/share/routerd/live-persistence.sh flush
```

![USB persistence flush](/img/iso-boot/iso-boot-08-usb-flush.png)

实际拔除 USB 设备前，请先执行 flush 与 unmount。

```sh
/usr/share/routerd/live-persistence.sh flush
/usr/share/routerd/live-persistence.sh umount
```

未 unmount 即拔除时，routerd 仍会继续在 RAM 上运行。
系统会输出警告，新的日志在 USB 恢复前暂存于 tmpfs。

## 故障排查

### USB 随身盘未出现在候选清单

从 shell 确认分区。

```sh
blkid
lsblk -f
```

若有需要，通过内核参数明确指定。

```text
routerd.usb=/dev/sdb1
```

### 重新启动后向导再次出现

ISO 未能找到已保存的配置。
挂载 USB 设备后确认。

```sh
mount /dev/sdX1 /media/routerd-usb
ls -l /media/routerd-usb/routerd/router.yaml
```

### 重新启动后没有日志

日志暂存于 RAM。
每日写出任务或手动 flush 执行后，才会保留于 USB。

### LAN 客户端未获取地址

确认向导中选择的 LAN 接口。

```sh
routerctl status --json
ip addr
```

在 Proxmox VE 进行测试时，请确认客户端与 routerd 的 LAN NIC 连接至同一个隔离 bridge。
