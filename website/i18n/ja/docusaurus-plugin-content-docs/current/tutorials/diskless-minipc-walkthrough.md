---
title: ディスクレス mini PC チュートリアル
---

# ディスクレス mini PC チュートリアル

このチュートリアルでは、小型 x86 mini PC を内蔵ディスクへ OS を導入せずに
ルーター化します。
routerd ライブ ISO から起動し、設定を USB に保存します。
ログは RAM に一時保存し、1 日 1 回だけ USB へ圧縮アーカイブを書き出します。

![ディスクレス mini PC の流れ](/img/routerd-diskless-minipc.svg)

## 用意するもの

- ネットワークインターフェースを 2 つ以上持つ mini PC
- routerd 永続化用の USB メモリー
- 最新の `routerd-live.iso`
- コンソールアクセス
- DHCPv4 または静的アドレスを使える WAN
- LAN スイッチまたは隔離されたテストブリッジ

## 1. USB メモリーを準備する

1 つのパーティションを作り、ライブ ISO がマウントできるファイルシステムで
フォーマットします。
ISO が自動検出できるように、ラベルを `ROUTERD` にします。

Linux 端末での例です。

```sh
sudo mkfs.ext4 -L ROUTERD /dev/sdX1
```

`/dev/sdX1` は実際の USB パーティションに置き換えてください。
誤ったデバイスをフォーマットしないでください。

## 2. ライブ ISO を起動する

固定 URL から取得します。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-live.iso
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-live.iso.sha256
sha256sum -c routerd-live.iso.sha256
```

mini PC を ISO から起動します。
同じイメージで、ビデオコンソールとシリアルコンソールの両方を使えます。

Proxmox VE の例です。

```sh
qm create 200 \
  --name routerd-live-demo \
  --memory 1536 \
  --cores 2 \
  --ostype l26 \
  --serial0 socket \
  --vga serial0 \
  --boot order=ide2 \
  --ide2 local:iso/routerd-live.iso,media=cdrom \
  --net0 virtio,bridge=vmbr0 \
  --net1 virtio,bridge=vmbr490
qm start 200
qm terminal 200
```

DHCP や RA の初期試験では、隔離された LAN ブリッジを使います。

## 3. ウィザードを実行する

`root` でログインします。
ライブ ISO が初期設定ウィザードを起動します。

ウィザードは次を確認します。

- ルーター名
- WAN インターフェース
- WAN IPv4 モード
- LAN インターフェース
- LAN アドレス
- DHCPv4、DNS、NTP、RA、firewall、NAT44
- 管理経路の置き場所
- USB 永続化

USB 永続化を聞かれたら `yes` を選び、USB パーティションを指定します。
パーティションに `ROUTERD` ラベルがあれば、自動的に候補へ出ます。

短時間の試験でなければ、1 日 1 回の USB 書き出しジョブを有効にします。
既定のログバッファーは `/run/routerd/logs` の 100 MiB です。

## 4. 初回反映を確認する

確認後、ウィザードは次を書きます。

```text
/usr/local/etc/routerd/router.yaml
```

その後、次を実行します。

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

状態を確認します。

```sh
routerctl status
```

phase が `Healthy` になれば成功です。

## 5. LAN クライアントを試す

LAN インターフェースまたはテストブリッジへクライアントを接続します。

クライアントは次を受け取るはずです。

- DHCPv4 pool からの IPv4 アドレス
- routerd を向く default route
- routerd を向く DNS server
- 有効化した場合は routerd を向く NTP server

基本確認です。

```sh
dig @192.168.10.1 www.google.com A +short
curl -4 https://www.google.com/generate_204
```

LAN prefix を変えた場合は、アドレスを読み替えてください。

## 6. 再起動して永続化を確認する

USB メモリーを接続したまま mini PC を再起動します。

起動時にライブ ISO は次を行います。

1. USB デバイスをマウントします。
2. `routerd/router.yaml` を復元します。
3. `/run/routerd/logs` を tmpfs として準備します。
4. ルーター設定を反映します。
5. ライブ routerd デーモンを起動します。

ログイン後に確認します。

```sh
routerctl status
```

ウィザードを再実行せずに収束すれば成功です。

## 7. ログ永続化の仕組み

ログはまず RAM に書きます。

```text
/run/routerd/logs
```

日次書き出しジョブは、次を USB へコピーします。

- 現在の `router.yaml`
- routerd の状態スナップショット
- 圧縮ログアーカイブ

これにより、USB flash への常時書き込みを避けます。
tmpfs の上限を超えた場合は、古いファイルから削除します。

手動で書き出す場合は、次を実行します。

```sh
/usr/share/routerd/live-persistence.sh flush
```

## トラブルシューティング

### USB メモリーが候補に出ない

シェルからパーティションを確認します。

```sh
blkid
lsblk -f
```

必要なら、カーネル引数で明示します。

```text
routerd.usb=/dev/sdb1
```

### 再起動後にまたウィザードが出る

ISO が保存済み設定を見つけられていません。
USB デバイスをマウントして確認します。

```sh
mount /dev/sdX1 /mnt
ls -l /mnt/routerd/router.yaml
```

### 再起動後にログがない

ログは RAM に一時保存します。
日次書き出しジョブ、または手動 flush を実行した後に USB へ残ります。

### LAN クライアントにアドレスが出ない

ウィザードで選んだ LAN インターフェースを確認します。

```sh
routerctl status --json
ip addr
```

Proxmox VE で試す場合は、クライアントと routerd の LAN NIC が同じ隔離ブリッジに
接続されていることを確認してください。
