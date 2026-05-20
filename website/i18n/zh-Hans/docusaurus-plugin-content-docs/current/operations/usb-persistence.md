---
title: USB 持久化
---

# USB 持久化

routerd live ISO 可以作为无盘路由器运行。在这种模式下，正在运行的系统保存在 RAM 中，只有选定的路由器状态会保存到 USB 设备。

这适合从可移动介质启动的 mini PC。它不需要永久内置磁盘，也能在重启后保留路由器配置。

## 布局

启用 USB 持久化后，routerd 会在选定分区上使用以下布局。

```text
routerd/
  router.yaml
  usb-device
  usb-flush-enabled
  log-limit
  logs/
  state/
```

启动时，`/usr/share/routerd/live-persistence.sh init` 会尝试查找 config media。它先检查记录过的设备，再检查 kernel command line 上的 `routerd.usb=`，最后查找标签为 `ROUTERD_CONFIG` 或 `ROUTERD` 的设备。可写入的分区会用于 persistence。Proxmox `media=cdrom` config ISO 这类 read-only ISO9660/UDF CD-ROM media 只用于 config import，flush 会停用。

选中的分区会挂载到 `/media/routerd-usb`。如果存在已保存的 `/media/routerd-usb/routerd/router.yaml`，它会被复制到 `/usr/local/etc/routerd/router.yaml`，然后由 live ISO 的启动流程应用。如果没有找到已保存配置，并且 `/usr/local/etc/routerd/router.yaml` 也不存在，ISO 会启动配置向导。

## 文件系统

live helper 使用 `blkid` 检测文件系统，并根据文件系统选择 mount option。

| 文件系统 | 默认 mount option | 说明 |
| --- | --- | --- |
| `ext4` | `rw,async,noatime` | 持久化路由器用途的首选。 |
| `vfat` | `rw,async,noatime,utf8,shortname=mixed` | 适合简单 USB 介质。没有 Unix 权限。 |
| `exfat` | `rw,async,noatime` | 适合与桌面操作系统共用的大容量 USB 介质。 |
| `iso9660` / `udf` | `ro,noatime` | read-only config import media。persistence flush 会停用。 |

FAT32 在 `blkid` 输出中通常显示为 `vfat`。live helper 不会先按 FAT32 硬编码挂载，而是先检测文件系统类型，再选择对应的挂载选项。

默认使用 `async,noatime`，因为它可以减少对 USB flash 的写入压力。调试或需要更保守写入行为时，可以传入以下 kernel parameter。

```text
routerd.usb_mount=sync
```

也可以用 `routerd.usb_mount=async` 明确指定默认行为。

## 日志缓冲

运行时日志先缓存在 tmpfs 中。

```text
/run/routerd/logs
```

默认缓冲上限是 100 MiB。超过上限时，会先删除最旧的文件。

如果启用每日写出任务，`/etc/periodic/daily/routerd-usb-flush` 会把以下内容复制到 USB。

- 当前 `router.yaml`
- `/var/lib/routerd` 的状态归档
- `/var/db/routerd` 的状态归档
- `/run/routerd/logs` 的压缩日志归档

也可以手动写出。

```sh
/usr/share/routerd/live-persistence.sh flush
```

## 安全移除

不要在持久化 mount 仍然有效时拔出 USB 设备。请先让 live helper 写出并卸载。

```sh
/usr/share/routerd/live-persistence.sh flush
/usr/share/routerd/live-persistence.sh umount
```

可以用以下命令查看当前状态。

```sh
/usr/share/routerd/live-persistence.sh status
```

如果设备被意外拔出，routerd 会继续从 RAM 运行。live helper 会记录警告，并在设备重新插入和 mount 前，不再把 USB 路径视为持久存储。

## Alpine lbu

ISO 包含 Alpine `lbu`。live helper 会把 routerd 路径加入 lbu include list。

```text
/usr/local/etc/routerd
/var/lib/routerd
/var/db/routerd
/etc/periodic/daily/routerd-usb-flush
```

保存配置或写出状态后，helper 会执行 `lbu commit`。通常不需要直接运行 `lbu`。

## 常用命令

列出候选设备。

```sh
/usr/share/routerd/live-persistence.sh list-devices
```

把配置保存到 USB。

```sh
/usr/share/routerd/live-persistence.sh save-config /dev/sdb1 /usr/local/etc/routerd/router.yaml yes 100M
```

恢复会在启动时自动执行。如果需要从 shell 强制重新运行启动逻辑，可以执行：

```sh
/usr/share/routerd/live-persistence.sh init
```
