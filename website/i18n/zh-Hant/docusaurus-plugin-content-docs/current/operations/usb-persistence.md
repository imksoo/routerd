---
title: USB 持久化
---

# USB 持久化

routerd 的 Live ISO 可作為無碟路由器運作。
在此模式下，執行中的系統放置於 RAM，
只有選定的路由器狀態才會儲存至 USB 裝置。

這適合從可移動媒體啟動的 mini PC。
不需要內建磁碟，重新開機後仍可保留設定。

## 目錄配置

啟用 USB 持久化後，routerd 會在選定分割區上建立以下配置。

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

開機時，`/usr/share/routerd/live-persistence.sh init` 會搜尋設定媒體。
首先確認已記錄的裝置，
接著確認核心命令列的 `routerd.usb=`，
最後搜尋標籤為 `ROUTERD_CONFIG` 或 `ROUTERD` 的裝置。
可寫入的分割區用於持久化。Proxmox 的 `media=cdrom` 設定 ISO 等唯讀的 ISO9660/UDF CD-ROM 媒體，
僅作為設定匯入用途，寫出（flush）功能會停用。

選定的分割區掛載至 `/media/routerd-usb`。
輔助程式會優先搜尋主機專屬設定，再搜尋通用設定。

- `/media/routerd-usb/routerd/hosts/<hostname>.yaml`
- `/media/routerd-usb/routerd/hosts/<mac>.yaml`（MAC 可使用冒號分隔或小寫無分隔格式）
- `/media/routerd-usb/routerd/router.yaml`

找到設定後，複製至 `/usr/local/etc/routerd/router.yaml`，
接著由 Live ISO 的開機程序套用設定。為方便驗收測試與障礙排查，
來源路徑與 SHA256 會分別儲存於 `/run/routerd/live-config-source` 與 `/run/routerd/live-config-sha256`。
密鑰會在 apply 前還原。輔助程式按以下順序查找：

- `routerd/hosts/<hostname>/secrets/`
- `routerd/hosts/<mac>/secrets/`（MAC 可使用冒號分隔或小寫無分隔格式）
- `routerd/secrets/`

檔案會以 mode `0600` 安裝到 `/usr/local/etc/routerd/secrets`。
若無已儲存的設定，且 `/usr/local/etc/routerd/router.yaml` 也不存在，則啟動設定精靈。

## 檔案系統

Live 輔助程式使用 `blkid` 判斷檔案系統，並依判斷結果切換掛載選項。

| 檔案系統 | 預設掛載選項 | 備註 |
| --- | --- | --- |
| `ext4` | `rw,async,noatime` | 持久化路由器用途的首選。 |
| `vfat` | `rw,async,noatime,utf8,shortname=mixed` | 適合一般 USB 隨身碟。無 Unix 權限。 |
| `exfat` | `rw,async,noatime` | 適合與桌面作業系統共用的大容量 USB 隨身碟。 |
| `iso9660` / `udf` | `ro,noatime` | 唯讀設定匯入媒體。持久化寫出停用。 |

FAT32 在 `blkid` 輸出中通常顯示為 `vfat`。
Live 輔助程式不會直接以 FAT32 硬式編碼掛載，
而是先判斷檔案系統類型，再選擇對應的選項。

預設使用 `async,noatime`，
以減少對 USB 快閃記憶體的寫入次數。
若優先考量除錯或保守的寫入確認，請指定以下核心參數。

```text
routerd.usb_mount=sync
```

若要明確指定預設值，使用 `routerd.usb_mount=async`。

## 日誌緩衝

執行時日誌暫存於 tmpfs。

```text
/run/routerd/logs
```

預設上限為 100 MiB。
超過上限時，從最舊的檔案開始刪除。

啟用每日寫出工作後，`/etc/periodic/daily/routerd-usb-flush` 會將以下內容複製至 USB。

- 目前的 `router.yaml`
- `/usr/local/etc/routerd/secrets` 下的檔案
- `/var/lib/routerd` 的狀態封存
- `/var/db/routerd` 的狀態封存
- `/run/routerd/logs` 的壓縮日誌封存

也可手動執行寫出。

```sh
/usr/share/routerd/live-persistence.sh flush
```

`save-config` 也會在 `/usr/local/etc/routerd/secrets` 存在時，將其複製到持久化裝置的
`routerd/secrets/`。若長期運作時需要 removable media 本身保留 Unix 權限，請優先使用
ext4，而不是 vfat/exfat。

## 安全移除

持久化掛載仍有效時，請勿直接拔除 USB 裝置。
請先透過 Live 輔助程式執行寫出與卸載。

```sh
/usr/share/routerd/live-persistence.sh flush
/usr/share/routerd/live-persistence.sh umount
```

目前狀態可透過以下指令確認。

```sh
/usr/share/routerd/live-persistence.sh status
```

即使 USB 裝置意外拔除，routerd 仍會繼續在 RAM 上運作。
Live 輔助程式會輸出警告，在裝置重新插入並掛載前，
不再將 USB 路徑作為持久儲存目的地。

## Alpine lbu

ISO 內含 Alpine 的 `lbu`。
Live 輔助程式會將 routerd 的路徑加入 lbu 的 include 清單。

```text
/usr/local/etc/routerd
/var/lib/routerd
/var/db/routerd
/etc/periodic/daily/routerd-usb-flush
```

儲存設定或寫出狀態後，輔助程式會執行 `lbu commit`。
一般情況下不需要直接執行 `lbu`。

## 常用指令

列出候選裝置。

```sh
/usr/share/routerd/live-persistence.sh list-devices
```

將設定儲存至 USB。

```sh
/usr/share/routerd/live-persistence.sh save-config /dev/sdb1 /usr/local/etc/routerd/router.yaml yes 100M
```

還原會在開機時自動執行。
若需從 shell 重新執行開機程序，請使用以下指令。

```sh
/usr/share/routerd/live-persistence.sh init
```
