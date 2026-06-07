---
title: USB 永続化
---

# USB 永続化

![Diagram showing USB persistence on the live ISO from boot-time config media discovery through mounted router.yaml and secrets restore, tmpfs log buffering, daily or manual flush, and safe unmount](/img/diagrams/operations-usb-persistence.png)

routerd のライブ ISO は、ディスクレスルーターとして動作できます。
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
  secrets/
  logs/
  state/
```

起動時には、`/usr/share/routerd/live-persistence.sh init` が設定メディアを探します。
まず、記録済みのデバイスを確認します。
次に、カーネルコマンドラインの `routerd.usb=` を確認します。
最後に、`ROUTERD_CONFIG` または `ROUTERD` ラベルのデバイスを探します。
書き込み可能なパーティションは、永続化用に使います。Proxmox の `media=cdrom` 設定 ISO のような読み取り専用の ISO9660/UDF CD-ROM メディアは、設定の取り込み専用として扱い、書き出し（flush）は無効にします。

選択したパーティションは `/media/routerd-usb` にマウントします。
ヘルパーは、ホスト固有の設定を先に探し、その後に汎用の設定を探します。

- `/media/routerd-usb/routerd/hosts/<hostname>.yaml`
- `/media/routerd-usb/routerd/hosts/<mac>.yaml`。MAC はコロン区切り、または小文字の詰めた表記を使えます。
- `/media/routerd-usb/routerd/router.yaml`

設定が見つかれば、`/usr/local/etc/routerd/router.yaml` へコピーします。
その後、ライブ ISO の起動処理が設定を反映します。受け入れテストや障害調査のために、取得元と SHA256 を `/run/routerd/live-config-source` と `/run/routerd/live-config-sha256` に保存します。
secrets は apply の前に復元します。helper は次の配置をこの順に探します。

- `routerd/hosts/<hostname>/secrets/`
- `routerd/hosts/<mac>/secrets/`。MAC はコロン区切り、または小文字の詰めた表記を使えます。
- `routerd/secrets/`

各 file は `/usr/local/etc/routerd/secrets` に mode `0600` で配置します。
保存済みの設定がなく、`/usr/local/etc/routerd/router.yaml` もなければ、設定ウィザードを起動します。

## ファイルシステム

ライブヘルパーは `blkid` でファイルシステムを判定します。
判定結果に応じて、マウントオプションを切り替えます。

| ファイルシステム | 既定のマウントオプション | メモ |
| --- | --- | --- |
| `ext4` | `rw,async,noatime` | 永続ルーター用途では第一候補です。 |
| `vfat` | `rw,async,noatime,utf8,shortname=mixed` | 単純な USB メモリーで便利です。Unix のパーミッションはありません。 |
| `exfat` | `rw,async,noatime` | 大容量の USB メモリーをデスクトップ OS と共用しやすい形式です。 |
| `iso9660` / `udf` | `ro,noatime` | 読み取り専用の設定取り込みメディアです。永続化の書き出しは無効です。 |

FAT32 は、通常 `blkid` では `vfat` として表示されます。
ライブヘルパーは、FAT32 と決め打ちでマウントしません。
ファイルシステムの種別を判定してから、対応するオプションを選びます。

既定は `async,noatime` です。
USB フラッシュへの書き込みを減らすためです。
デバッグや保守的な書き込み確認を優先する場合は、次のカーネルパラメーターを指定します。

```text
routerd.usb_mount=sync
```

既定値を明示する場合は、`routerd.usb_mount=async` を使います。

## ログバッファー

実行時のログは tmpfs に一時保存します。

```text
/run/routerd/logs
```

既定の上限は 100 MiB です。
上限を超えた場合は、古いファイルから削除します。

日次の書き出しジョブを有効にすると、`/etc/periodic/daily/routerd-usb-flush` が次を USB へコピーします。

- 現在の `router.yaml`
- `/usr/local/etc/routerd/secrets` の file
- `/var/lib/routerd` の状態アーカイブ
- `/var/db/routerd` の状態アーカイブ
- `/run/routerd/logs` の圧縮ログアーカイブ

手動でも書き出せます。

```sh
/usr/share/routerd/live-persistence.sh flush
```

`save-config` も、`/usr/local/etc/routerd/secrets` が存在する場合は
`routerd/secrets/` へコピーします。長期運用で removable media 側の Unix permission
も必要な場合は、vfat/exfat より ext4 を使ってください。

## 安全な取り外し

永続化用のマウントが有効なまま、USB デバイスを抜かないでください。
先に、ライブヘルパーで書き出しとアンマウントを実行します。

```sh
/usr/share/routerd/live-persistence.sh flush
/usr/share/routerd/live-persistence.sh umount
```

現在の状態は、次で確認します。

```sh
/usr/share/routerd/live-persistence.sh status
```

予期せず USB デバイスが抜かれた場合でも、routerd は RAM 上で動作を続けます。
ライブヘルパーは警告を出します。
再接続してマウントするまで、USB のパスを永続保存先としては扱いません。

## Alpine lbu

ISO には Alpine の `lbu` が含まれます。
ライブヘルパーは、routerd 用のパスを lbu の include list に追加します。

```text
/usr/local/etc/routerd
/var/lib/routerd
/var/db/routerd
/etc/periodic/daily/routerd-usb-flush
```

設定の保存や状態の書き出しの後に、ヘルパーが `lbu commit` を実行します。
通常は、`lbu` を直接実行する必要はありません。

## よく使うコマンド

候補のデバイスを表示します。

```sh
/usr/share/routerd/live-persistence.sh list-devices
```

設定を USB へ保存します。

```sh
/usr/share/routerd/live-persistence.sh save-config /dev/sdb1 /usr/local/etc/routerd/router.yaml yes 100M
```

復元は、起動時に自動で行います。
シェルから起動時の処理を再実行する場合は、次を使います。

```sh
/usr/share/routerd/live-persistence.sh init
```
