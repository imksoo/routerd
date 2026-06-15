---
title: ライブ ISO での SSH リモート管理
---

# ライブ ISO での SSH リモート管理

![デフォルトで閉じられたライブ ISO SSH 管理が、routerd.ssh ブートフラグと config メディア上の外部 authorized_keys によってのみ有効化され、公開鍵認証のみの sshd として起動されることを示す図](/img/diagrams/operations-live-iso-ssh.png)

routerd ライブ ISO はデフォルトで SSH デーモンを実行しません。デフォルトでは
閉鎖的な構成となっており、ローカルコンソールとシリアルコンソール（`tty1`、`tty2`、`ttyS0`）のみ
利用できます。これにより、パスワードなしの root アクセスがネットワーク上に露出しません。

ハイパーバイザー VM（Proxmox VE、KVM 等）としてアプライアンスを実行していて
シリアルコンソールのアクセスが不便な場合、ISO イメージに認証情報を組み込むこと
なくオプトイン SSH モードを有効化できます。

## 前提条件

- `authorized_keys` ファイルを含む config ディスク（ラベルが `ROUTERD_CONFIG` または
  `ROUTERD`、あるいは `routerd.usb=` で指定）。
- ブート時にカーネルパラメータを設定する手段（GRUB エントリの編集、またはハイパー
  バイザーから VM のカーネル引数を設定）。

## SSH の有効化

### ステップ 1 — config ディスクに公開鍵を配置

config ディスク（例: `router.yaml` を格納する Proxmox VM ディスク）上の `routerd/`
ディレクトリ内に `authorized_keys` ファイルを作成します。

```text
routerd/
  router.yaml
  authorized_keys       ← このファイルを追加
```

ファイルは標準的な OpenSSH `authorized_keys` フォーマットに従います。

```text
ssh-ed25519 AAAA...yourkey... user@host
```

ホスト固有の鍵ファイルもサポートされています（汎用ファイルより先に検索されます）。

```text
routerd/hosts/<hostname>/authorized_keys
routerd/hosts/<mac>/authorized_keys   (コロン区切りまたはコンパクト小文字)
```

### ステップ 2 — `routerd.ssh=1` でブート

カーネルコマンドラインに `routerd.ssh=1` パラメータを追加します。

**GRUB（ライブ ISO ブートメニュー — `e` を押して編集）:**

```text
linux /boot/vmlinuz-lts ... routerd.ssh=1
```

**Proxmox VE — VM のブート引数を設定:**

```sh
qm set <vmid> --args "-append routerd.ssh=1"
```

または VM の GRUB エントリに一度設定すれば、再起動をまたいで永続化します。

## ブート時の動作

1. `live-persistence.sh init` が config ディスクをマウントし、`router.yaml` を復元。
2. `live-autostart.sh` が依存パッケージをインストール（まだ存在しなければ `openssh`
   を含む）。
3. `live-ssh.sh` がカーネルコマンドラインで `routerd.ssh=1` を確認。
4. フラグが設定されていれば、マウント済みの config ディスク上で `authorized_keys` を
   検索。
5. 見つかった場合、鍵を `/root/.ssh/authorized_keys` にインストールし、
   `ssh-keygen -A` でホスト鍵を生成し、`sshd` を起動。
6. `routerd.ssh=1` が設定されているが `authorized_keys` ファイルが見つからない場合、
   sshd は**起動されず**、`/run/routerd/logs/routerd-ssh.log` に警告がログ出力される。

## セキュリティモデル

| プロパティ | 値 |
| --- | --- |
| デフォルト状態 | SSH 無効 |
| 認証方式 | 公開鍵のみ |
| root のパスワード認証 | 恒久的に無効（`PasswordAuthentication no`） |
| root ログイン | `PermitRootLogin prohibit-password`（鍵認証のみ） |
| ISO 内の認証情報 | なし — 鍵は実行時に config ディスクから取得 |

SSH は明示的なオプトインによってのみ有効化され、外部メディアに認証情報が
提供されている場合にのみ動作します。パスワード認証へのフォールバックはありません。

## トラブルシューティング

**sshd が起動しない場合:**

```sh
cat /run/routerd/logs/routerd-ssh.log
```

よくある原因:
- カーネルコマンドラインに `routerd.ssh=1` がない — `/proc/cmdline` を確認。
- config ディスクがマウントされていない — `/proc/mounts` で `/media/routerd-usb` を確認。
- 期待されるパスに `authorized_keys` が見つからない — `live-ssh.sh` が期待する場所を
  ログに記録している。

**sshd が動作中か確認:**

```sh
pgrep -x sshd
ss -tlnp | grep :22
```

**再起動なしで SSH セットアップを再実行:**

```sh
/usr/share/routerd/live-ssh.sh
```

## 関連項目

- [USB 永続化](./usb-persistence) — config ディスクのレイアウトとデバイス検出
- [Alpine / OpenRC デプロイ](./alpine-deployment) — ライブ ISO のブートパラメータ
