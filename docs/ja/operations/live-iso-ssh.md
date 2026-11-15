---
title: ライブ ISO での SSH リモート管理
---

# ライブ ISO での SSH リモート管理

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

## ホスト鍵の固定

`spec.sshHostKeys` にパスフレーズのない Ed25519、RSA、ECDSA 秘密鍵をそれぞれ `privateKey` として指定できます。同じ鍵種は複数指定できません。鍵種に応じて `/etc/ssh/ssh_host_ed25519_key`、`ssh_host_rsa_key`、`ssh_host_ecdsa_key` と公開鍵を更新し、sshd の設定を検証してからサービスを再読み込みします。変更がなければ再読み込みせず、失敗時は次の周期で再試行します。指定しない鍵種は変更せず、設定を削除しても適用済みの鍵は消去しません。全て省略した場合は起動時生成を維持します。

起動直後、適用前には一時的な鍵が提示されます。秘密鍵は構成ファイルと世代履歴に保存されます。設定者は routerd が読み取る前に、入力ファイルと config ディスクの権限を適切に設定し、バックアップへのアクセスも制限してください。routerd は秘密鍵を含む履歴の保存前に状態データベースと既存の WAL 等を最大 0600 の権限に制限します。WebConsole の構成表示では秘密鍵を伏せます。

```yaml
spec:
  sshHostKeys:
    - privateKey: |
        -----BEGIN OPENSSH PRIVATE KEY-----
        （秘密鍵）
        -----END OPENSSH PRIVATE KEY-----
  resources: []
```

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

- [USB 永続化](usb-persistence.md) — config ディスクのレイアウトとデバイス検出
