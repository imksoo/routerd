---
title: ディスクレス mini PC チュートリアル
---

# ディスクレス mini PC チュートリアル

![ライブ ISO 起動と USB 永続化から routerd ウィザード設定と検証へ進むディスクレス mini PC チュートリアルの流れ](/img/diagrams/tutorial-diskless-minipc-walkthrough.png)

このチュートリアルでは、小型 x86 mini PC を、内蔵ディスクへ OS を導入せずに
ルーター化します。
routerd ライブ ISO から起動し、設定を USB に保存します。
ログは RAM に一時保存し、1 日 1 回だけ圧縮アーカイブを USB へ書き出します。

![ディスクレス mini PC の流れ](/img/routerd-diskless-minipc.svg)

## 用意するもの

- ネットワークインターフェースを 2 つ以上持つ mini PC
- routerd 永続化用の USB メモリー
- 最新の `routerd-live.iso`
- コンソールアクセス。Proxmox VE では `qm terminal` でシリアルコンソールを使えます。
- DHCPv4 または静的アドレスを使える WAN
- LAN スイッチまたは隔離されたテストブリッジ

## 1. USB メモリーを準備する

パーティションを 1 つ作り、ライブ ISO がマウントできるファイルシステムで
フォーマットします。
既定では `ext4` を推奨します。
単純なリムーバブルメディアなら `vfat` と `exfat` も使えます。
ISO が自動検出できるように、ラベルを `ROUTERD` にします。
FAT32 は `blkid` では通常 `vfat` として表示されます。
routerd 専用の USB メモリーなら、`ext4` が扱いやすいです。

Linux 端末での例を示します。

```sh
sudo mkfs.ext4 -L ROUTERD /dev/sdX1
```

`/dev/sdX1` は、実際の USB パーティションに置き換えてください。
誤ったデバイスをフォーマットしないように注意してください。

## 2. ライブ ISO を起動する

固定 URL から取得します。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-live.iso
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-live.iso.sha256
sha256sum -c routerd-live.iso.sha256
```

mini PC を ISO から起動します。
同じイメージで、ビデオコンソールとシリアルコンソールの両方を使えます。

Proxmox VE での例を示します。

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

DHCP や RA の初期試験では、隔離された LAN ブリッジを使います。

![routerd ライブ起動メニュー](/img/iso-boot/iso-boot-01-grub.png)

ISO は、ビデオコンソールとシリアルコンソールの両方を有効にします。
Proxmox VE では、対話式ウィザードは通常 `qm terminal` の方が読みやすいです。
VGA の画面キャプチャは起動の証跡として使い、実際の入力と結果は下の
シリアルコンソールログで確認します。

![起動メッセージ](/img/iso-boot/iso-boot-02-alpine-boot.png)

## 3. ウィザードを実行する

`root` でログインすると、ライブ ISO が初期設定ウィザードを起動します。

![routerd ライブのログインと MOTD](/img/iso-boot/iso-boot-03-login-motd.png)

シリアルコンソールでは、ライブ ISO の案内とウィザードの開始が次のように
表示されます。

```text
routerd live v20260510.1811

localhost login: root

Run the setup wizard:
  /usr/share/routerd/install.sh configure

Starting routerd setup wizard. Press Ctrl+C to skip.
routerd initial configuration wizard

Available interfaces:
  - lo
  - eth0
  - eth1
```

ウィザードは次を確認します。

- ルーター名
- WAN インターフェース
- WAN IPv4 モード
- LAN インターフェース
- LAN アドレス
- DHCPv4、DNS、NTP、RA、firewall、NAT44
- 管理経路の置き場所
- USB 永続化

![routerd ライブウィザードの WAN 設定](/img/iso-boot/iso-boot-04-wizard-wan.png)

![routerd ライブウィザードの LAN 設定](/img/iso-boot/iso-boot-05-wizard-lan.png)

実機検証時のシリアルコンソールログを次に示します。

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

USB 永続化を聞かれたら `yes` を選び、USB パーティションを指定します。
パーティションに `ROUTERD` ラベルが付いていれば、自動的に候補へ表示されます。

短時間の試験でなければ、1 日 1 回の USB 書き出しジョブを有効にします。
既定のログバッファーは `/run/routerd/logs` に置かれる 100 MiB です。

live helper は、`blkid` で `ext4`、`vfat`、`exfat` を判定します。
USB 永続化は、USB への書き込みを減らすために、既定で `async,noatime` として
マウントします。
特定の試験で同期書き込みが必要な場合だけ、kernel command line に
`routerd.usb_mount=sync` を追加します。

選択した USB パーティションは `/media/routerd-usb` にマウントします。
保存先の設定ファイルは `/media/routerd-usb/routerd/router.yaml` であり、
`/mnt/routerd/router.yaml` ではありません。

## 4. 初回反映を確認する

確認が終わると、ウィザードは次を書き出します。

```text
/usr/local/etc/routerd/router.yaml
```

その後、次を実行します。

```sh
routerctl validate -f /usr/local/etc/routerd/router.yaml --replace
routerctl plan -f /usr/local/etc/routerd/router.yaml --replace
routerctl apply -f /usr/local/etc/routerd/router.yaml --replace
```

![ウィザードの要約と初回適用](/img/iso-boot/iso-boot-06-wizard-summary.png)

状態を確認します。

```sh
routerctl get status
```

![初回適用後の routerctl get status](/img/iso-boot/iso-boot-07-routerctl-status.png)

phase が `Healthy` になれば成功です。
シリアルログでは、次のような状態が返ってきます。

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

## 5. LAN クライアントを試す

LAN インターフェースまたはテストブリッジへ、クライアントを接続します。

クライアントは、次を受け取るはずです。

- DHCPv4 プールからの IPv4 アドレス
- routerd を向くデフォルトルート
- routerd を向く DNS サーバー
- 有効化した場合は、routerd を向く NTP サーバー

基本的な確認は次のとおりです。

```sh
dig @192.168.10.1 www.google.com A +short
curl -4 https://www.google.com/generate_204
```

LAN prefix を変えた場合は、アドレスを読み替えてください。

PVE 検証では、隔離 LAN ブリッジに一時的な network namespace を接続しました。
クライアントは routerd からリースを受け取り、routerd の NAT44 を経由して
外部へ通信できました。

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

## 6. 再起動して永続化を確認する

USB メモリーを接続したまま、mini PC を再起動します。

起動時に、ライブ ISO は次を行います。

1. 記録済みデバイス、`routerd.usb=`、`ROUTERD` ラベルの順で USB デバイスを探します。
2. USB デバイスを `/media/routerd-usb` にマウントします。
3. `/media/routerd-usb/routerd/router.yaml` を復元します。
4. `/run/routerd/logs` を tmpfs として準備します。
5. ルーター設定を反映します。
6. ライブ routerd デーモンを起動します。

ログイン後に確認します。

```sh
routerctl get status
```

ウィザードを再実行せずに収束すれば成功です。
設定が復元されず、`/usr/local/etc/routerd/router.yaml` もない場合は、
設定ウィザードが起動します。

## 7. ログ永続化の仕組み

ログは、まず RAM へ書き込みます。

```text
/run/routerd/logs
```

日次の書き出しジョブは、次を USB へコピーします。

- 現在の `router.yaml`
- routerd の状態スナップショット
- 圧縮ログアーカイブ

こうすることで、USB フラッシュメモリーへの常時書き込みを避けます。
tmpfs の上限を超えた場合は、古いファイルから削除します。

手動で書き出す場合は、次を実行します。

```sh
/usr/share/routerd/live-persistence.sh flush
```

![USB 永続化のフラッシュ](/img/iso-boot/iso-boot-08-usb-flush.png)

USB デバイスを物理的に抜く前に、flush と unmount を実行します。

```sh
/usr/share/routerd/live-persistence.sh flush
/usr/share/routerd/live-persistence.sh umount
```

unmount せずに抜いた場合でも、routerd は RAM 上で動作を続けます。
警告を出し、新しいログは USB が戻るまで tmpfs に保持します。

## トラブルシューティング

### USB メモリーが候補に出ない

シェルからパーティションを確認します。

```sh
blkid
lsblk -f
```

必要なら、カーネル引数で明示的に指定します。

```text
routerd.usb=/dev/sdb1
```

### 再起動後にまたウィザードが出る

ISO が保存済みの設定を見つけられていません。
USB デバイスをマウントして確認します。

```sh
mount /dev/sdX1 /media/routerd-usb
ls -l /media/routerd-usb/routerd/router.yaml
```

### 再起動後にログがない

ログは RAM に一時保存しています。
日次の書き出しジョブ、または手動の flush を実行した後に、USB へ残ります。

### LAN クライアントにアドレスが出ない

ウィザードで選んだ LAN インターフェースを確認します。

```sh
routerctl get status -o json
ip addr
```

Proxmox VE で試す場合は、クライアントと routerd の LAN NIC が同じ隔離ブリッジに
接続されていることを確認してください。
