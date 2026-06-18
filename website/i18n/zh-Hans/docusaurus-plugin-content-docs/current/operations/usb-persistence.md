---
title: USB 持久化
---

# USB 持久化

![Diagram showing USB persistence on the live ISO from boot-time config media discovery through mounted router.yaml and secrets restore, tmpfs log buffering, daily or manual flush, and safe unmount](/img/diagrams/operations-usb-persistence.png)

routerd 的 Live ISO 可作为无盘路由器运行。
在此模式下，运行中的系统放置于 RAM，
只有选定的路由器状态才会保存至 USB 设备。

这适合从可移动介质启动的 mini PC。
不需要内置磁盘，重新启动后仍可保留配置。

## 目录配置

启用 USB 持久化后，routerd 会在选定分区上创建以下配置。

```text
routerd/
  router.yaml
  usb-device
  usb-flush-enabled
  log-limit
  secrets/
  logs/
  state/
```

启动时，`/usr/share/routerd/live-persistence.sh init` 会搜索配置介质。
首先确认已记录的设备，
接着确认内核命令行的 `routerd.usb=`，
最后搜索标签为 `ROUTERD_CONFIG` 或 `ROUTERD` 的设备。
可写入的分区用于持久化。Proxmox 的 `media=cdrom` 配置 ISO 等只读的 ISO9660/UDF CD-ROM 介质，
仅作为配置导入用途，写出（flush）功能会停用。

选定的分区挂载至 `/media/routerd-usb`。
辅助程序会优先搜索主机专属配置，再搜索通用配置。

- `/media/routerd-usb/routerd/hosts/<hostname>.yaml`
- `/media/routerd-usb/routerd/hosts/<mac>.yaml`（MAC 可使用冒号分隔或小写无分隔格式）
- `/media/routerd-usb/routerd/router.yaml`

找到配置后，复制至 `/usr/local/etc/routerd/router.yaml`，
接着由 Live ISO 的启动流程应用配置。为方便验收测试与故障排查，
来源路径与 SHA256 会分别存储于 `/run/routerd/live-config-source` 与 `/run/routerd/live-config-sha256`。
密钥会在 apply 前恢复。辅助程序按以下顺序查找：

- `routerd/hosts/<hostname>/secrets/`
- `routerd/hosts/<mac>/secrets/`（MAC 可使用冒号分隔或小写无分隔格式）
- `routerd/secrets/`

文件会以 mode `0600` 安装到 `/usr/local/etc/routerd/secrets`。
若无已保存的配置，且 `/usr/local/etc/routerd/router.yaml` 也不存在，则启动配置向导。

## 文件系统

Live 辅助程序使用 `blkid` 判断文件系统，并依判断结果切换挂载选项。

| 文件系统 | 默认挂载选项 | 备注 |
| --- | --- | --- |
| `ext4` | `rw,async,noatime` | 持久化路由器用途的首选。 |
| `vfat` | `rw,async,noatime,utf8,shortname=mixed` | 适合一般 USB 随身盘。无 Unix 权限。 |
| `exfat` | `rw,async,noatime` | 适合与桌面操作系统共用的大容量 USB 随身盘。 |
| `iso9660` / `udf` | `ro,noatime` | 只读配置导入介质。持久化写出停用。 |

FAT32 在 `blkid` 输出中通常显示为 `vfat`。
Live 辅助程序不会直接以 FAT32 硬编码挂载，
而是先判断文件系统类型，再选择对应的选项。

默认使用 `async,noatime`，
以减少对 USB 闪存的写入次数。
若优先考量调试或保守的写入确认，请指定以下内核参数。

```text
routerd.usb_mount=sync
```

若要明确指定默认值，使用 `routerd.usb_mount=async`。

## 日志缓冲

运行时日志暂存于 tmpfs。

```text
/run/routerd/logs
```

默认上限为 100 MiB。
超过上限时，从最旧的文件开始删除。

启用每日写出任务后，`/etc/periodic/daily/routerd-usb-flush` 会将以下内容复制至 USB。

- 当前的 `router.yaml`
- `/usr/local/etc/routerd/secrets` 下的文件
- `/var/lib/routerd` 的状态归档
- `/var/db/routerd` 的状态归档
- `/run/routerd/logs` 的压缩日志归档

也可手动执行写出。

```sh
/usr/share/routerd/live-persistence.sh flush
```

`save-config` 也会在 `/usr/local/etc/routerd/secrets` 存在时，将其复制到持久化设备的
`routerd/secrets/`。若长期运行时需要 removable media 本身保留 Unix 权限，请优先使用
ext4，而不是 vfat/exfat。

## 安全移除

持久化挂载仍有效时，请勿直接拔除 USB 设备。
请先通过 Live 辅助程序执行写出与卸载。

```sh
/usr/share/routerd/live-persistence.sh flush
/usr/share/routerd/live-persistence.sh umount
```

当前状态可通过以下命令确认。

```sh
/usr/share/routerd/live-persistence.sh status
```

即使 USB 设备意外拔除，routerd 仍会继续在 RAM 上运行。
Live 辅助程序会输出警告，在设备重新插入并挂载前，
不再将 USB 路径作为持久存储目的地。

## 常用命令

列出候选设备。

```sh
/usr/share/routerd/live-persistence.sh list-devices
```

将配置保存至 USB。

```sh
/usr/share/routerd/live-persistence.sh save-config /dev/sdb1 /usr/local/etc/routerd/router.yaml yes 100M
```

还原会在启动时自动执行。
若需从 shell 重新执行启动流程，请使用以下命令。

```sh
/usr/share/routerd/live-persistence.sh init
```
