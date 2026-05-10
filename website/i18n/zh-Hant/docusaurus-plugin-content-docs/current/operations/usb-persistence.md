---
title: USB 持久化
---

# USB 持久化

routerd live ISO 可以作為無碟路由器執行。在這種模式下，正在執行的系統保存在 RAM 中，只有選定的路由器狀態會保存到 USB 裝置。

這適合從可移動媒體啟動的 mini PC。它不需要永久內建磁碟，也能在重新啟動後保留路由器設定。

## 佈局

啟用 USB 持久化後，routerd 會在選定分割區上使用以下佈局。

```text
routerd/
  router.yaml
  usb-device
  usb-flush-enabled
  log-limit
  logs/
  state/
```

啟動時，`/usr/share/routerd/live-persistence.sh init` 會嘗試尋找 USB 裝置。它先檢查記錄過的裝置，再檢查 kernel command line 上的 `routerd.usb=`，最後尋找標籤為 `ROUTERD` 的分割區。

選中的分割區會掛載到 `/media/routerd-usb`。如果存在已保存的 `/media/routerd-usb/routerd/router.yaml`，它會被複製到 `/usr/local/etc/routerd/router.yaml`，然後由 live ISO 的啟動流程套用。如果沒有找到已保存設定，且 `/usr/local/etc/routerd/router.yaml` 也不存在，ISO 會啟動設定精靈。

## 檔案系統

live helper 使用 `blkid` 偵測檔案系統，並根據檔案系統選擇 mount option。

| 檔案系統 | 預設 mount option | 說明 |
| --- | --- | --- |
| `ext4` | `rw,async,noatime` | 持久化路由器用途的首選。 |
| `vfat` | `rw,async,noatime,utf8,shortname=mixed` | 適合簡單 USB 媒體。沒有 Unix 權限。 |
| `exfat` | `rw,async,noatime` | 適合與桌面作業系統共用的大容量 USB 媒體。 |

FAT32 在 `blkid` 輸出中通常顯示為 `vfat`。live helper 不會先按 FAT32 硬編碼掛載，而是先偵測檔案系統類型，再選擇對應的掛載選項。

預設使用 `async,noatime`，因為它可以減少對 USB flash 的寫入壓力。除錯或需要更保守寫入行為時，可以傳入以下 kernel parameter。

```text
routerd.usb_mount=sync
```

也可以用 `routerd.usb_mount=async` 明確指定預設行為。

## 日誌緩衝

執行時日誌先緩存在 tmpfs 中。

```text
/run/routerd/logs
```

預設緩衝上限是 100 MiB。超過上限時，會先刪除最舊的檔案。

如果啟用每日寫出任務，`/etc/periodic/daily/routerd-usb-flush` 會把以下內容複製到 USB。

- 目前的 `router.yaml`
- `/var/lib/routerd` 的狀態封存
- `/var/db/routerd` 的狀態封存
- `/run/routerd/logs` 的壓縮日誌封存

也可以手動寫出。

```sh
/usr/share/routerd/live-persistence.sh flush
```

## 安全移除

不要在持久化 mount 仍然有效時拔出 USB 裝置。請先讓 live helper 寫出並卸載。

```sh
/usr/share/routerd/live-persistence.sh flush
/usr/share/routerd/live-persistence.sh umount
```

可以用以下命令查看目前狀態。

```sh
/usr/share/routerd/live-persistence.sh status
```

如果裝置被意外拔出，routerd 會繼續從 RAM 執行。live helper 會記錄警告，並在裝置重新插入和 mount 前，不再把 USB 路徑視為持久儲存。

## Alpine lbu

ISO 包含 Alpine `lbu`。live helper 會把 routerd 路徑加入 lbu include list。

```text
/usr/local/etc/routerd
/var/lib/routerd
/var/db/routerd
/etc/periodic/daily/routerd-usb-flush
```

保存設定或寫出狀態後，helper 會執行 `lbu commit`。通常不需要直接執行 `lbu`。

## 常用命令

列出候選裝置。

```sh
/usr/share/routerd/live-persistence.sh list-devices
```

把設定保存到 USB。

```sh
/usr/share/routerd/live-persistence.sh save-config /dev/sdb1 /usr/local/etc/routerd/router.yaml yes 100M
```

還原會在啟動時自動執行。如果需要從 shell 強制重新執行啟動邏輯，可以執行：

```sh
/usr/share/routerd/live-persistence.sh init
```
