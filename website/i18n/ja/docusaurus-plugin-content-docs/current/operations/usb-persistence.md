---
title: USB 永続化
---

# USB 永続化

routerd ライブ ISO は、ディスクレスルーターとして動作できます。
このモードでは、実行中のシステムを RAM に置きます。
選択したルーター状態だけを USB デバイスへ保存します。

これは、リムーバブルメディアから起動する mini PC 向けです。
内蔵ディスクを使わずに、再起動後も設定を維持できます。

## 配置

USB 永続化を有効にすると、選択したパーティションに次の配置を作ります。

```text
routerd/
  router.yaml
  usb-device
  usb-flush-enabled
  log-limit
  logs/
  state/
```

起動時は `/usr/share/routerd/live-persistence.sh init` が USB デバイスを探します。
最初に記録済みデバイスを確認します。
次に kernel command line の `routerd.usb=` を確認します。
最後に `ROUTERD_CONFIG` または `ROUTERD` ラベルのパーティションを探します。

選択したパーティションは `/media/routerd-usb` に mount します。
helper は host 固有の config を先に探し、その後 generic config を探します。

- `/media/routerd-usb/routerd/hosts/<hostname>.yaml`
- `/media/routerd-usb/routerd/hosts/<mac>.yaml`。MAC は colon 区切りまたは
  lowercase compact 表記を使えます。
- `/media/routerd-usb/routerd/router.yaml`

config が見つかれば `/usr/local/etc/routerd/router.yaml` へコピーします。
その後、ライブ ISO の起動処理が設定を反映します。受入テストと troubleshooting 用に、
source と SHA256 を `/run/routerd/live-config-source` と
`/run/routerd/live-config-sha256` に保存します。
保存済み設定がなく、`/usr/local/etc/routerd/router.yaml` もなければ、
設定ウィザードを起動します。

## ファイルシステム

live helper は `blkid` でファイルシステムを判定します。
判定結果に応じて mount option を切り替えます。

| ファイルシステム | 既定の mount option | メモ |
| --- | --- | --- |
| `ext4` | `rw,async,noatime` | 永続ルーター用途では第一候補です。 |
| `vfat` | `rw,async,noatime,utf8,shortname=mixed` | 単純な USB メモリーで便利です。Unix permission はありません。 |
| `exfat` | `rw,async,noatime` | 大容量 USB メモリーを desktop OS と共用しやすい形式です。 |

FAT32 は通常 `blkid` では `vfat` として表示されます。
live helper は FAT32 と決め打ちで mount しません。
ファイルシステム種別を判定してから、対応する option を選びます。

既定は `async,noatime` です。
USB flash への書き込みを減らすためです。
デバッグや保守的な書き込み確認を優先する場合は、次の kernel parameter を指定します。

```text
routerd.usb_mount=sync
```

既定値を明示する場合は `routerd.usb_mount=async` を使います。

## ログバッファー

実行時ログは tmpfs に一時保存します。

```text
/run/routerd/logs
```

既定の上限は 100 MiB です。
上限を超えた場合は、古いファイルから削除します。

日次書き出しジョブを有効にすると、
`/etc/periodic/daily/routerd-usb-flush` が次を USB へコピーします。

- 現在の `router.yaml`
- `/var/lib/routerd` の状態アーカイブ
- `/var/db/routerd` の状態アーカイブ
- `/run/routerd/logs` の圧縮ログアーカイブ

手動でも書き出せます。

```sh
/usr/share/routerd/live-persistence.sh flush
```

## 安全な取り外し

永続化用の mount が有効なまま USB デバイスを抜かないでください。
先に live helper で flush と unmount を実行します。

```sh
/usr/share/routerd/live-persistence.sh flush
/usr/share/routerd/live-persistence.sh umount
```

現在の状態は次で確認します。

```sh
/usr/share/routerd/live-persistence.sh status
```

予期せず USB デバイスが抜かれた場合でも、routerd は RAM 上で動作を続けます。
live helper は警告を出します。
再接続して mount するまで、USB パスを永続保存先として扱いません。

## Alpine lbu

ISO には Alpine `lbu` が含まれます。
live helper は routerd 用のパスを lbu include list に追加します。

```text
/usr/local/etc/routerd
/var/lib/routerd
/var/db/routerd
/etc/periodic/daily/routerd-usb-flush
```

設定保存や状態書き出しの後、helper が `lbu commit` を実行します。
通常は `lbu` を直接実行する必要はありません。

## よく使うコマンド

候補デバイスを表示します。

```sh
/usr/share/routerd/live-persistence.sh list-devices
```

設定を USB へ保存します。

```sh
/usr/share/routerd/live-persistence.sh save-config /dev/sdb1 /usr/local/etc/routerd/router.yaml yes 100M
```

復元は起動時に自動で行います。
shell から起動時処理を再実行する場合は次を使います。

```sh
/usr/share/routerd/live-persistence.sh init
```
