---
title: インストールとアップグレード
---

# インストールとアップグレード

ルーターホストにはリリースアーカイブから導入します。
アーカイブには、実行ファイル、サービステンプレート、設定例、インストーラーが
含まれます。
ルーターホストに Go や Makefile は不要です。

## クイックインストール

[GitHub Releases](https://github.com/imksoo/routerd/releases) から、OS と
アーキテクチャーに合うアーカイブを取得します。

Linux amd64:

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

Linux arm64 では `linux-arm64` アーカイブを使います。

FreeBSD amd64:

```sh
fetch https://github.com/imksoo/routerd/releases/latest/download/routerd-freebsd-amd64.tar.gz
fetch https://github.com/imksoo/routerd/releases/latest/download/routerd-freebsd-amd64.tar.gz.sha256
cat routerd-freebsd-amd64.tar.gz.sha256
sha256 routerd-freebsd-amd64.tar.gz
tar -xzf routerd-freebsd-amd64.tar.gz
sudo ./install.sh
```

FreeBSD arm64 では `freebsd-arm64` アーカイブを使います。
latest release には `routerd-vYYYYMMDD.HHmm-linux-amd64.tar.gz` のような
版番号付きアーカイブもあります。
特定の版に固定する場合は、版番号付きアーカイブを使います。

Linux 用アーカイブには、`CGO_ENABLED=0` で静的リンクした routerd バイナリを
含めます。
配置先ホストの glibc 版には依存しません。
`dnsmasq`、`nft`、`ip`、`conntrack`、`tcpdump` などの実行時ツールは、
引き続き `install.sh` が導入または確認します。

native nDPI による application classification が必要なホストでは、対応する
`routerd-ndpi-agent-libndpi-linux-amd64.tar.gz` も取得し、通常のアーカイブと
同じ install transaction で明示的に適用します。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-ndpi-agent-libndpi-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-ndpi-agent-libndpi-linux-amd64.tar.gz.sha256
sha256sum -c routerd-ndpi-agent-libndpi-linux-amd64.tar.gz.sha256
sudo ./install.sh --with-ndpi \
  --with-ndpi-archive ./routerd-ndpi-agent-libndpi-linux-amd64.tar.gz
```

`--with-ndpi` は、最終的に install された `routerd-ndpi-agent` が
`libndpiLoaded: true` を返さない場合に失敗します。そのため、static fallback
agent が native nDPI 要件を黙って満たしたことにはなりません。

`install.sh` は新規導入かアップグレードかを自動判定します。
実行ファイルを `/usr/local/sbin` に配置し、サービステンプレートを導入します。
また、`/usr/local/etc/routerd/router.yaml.sample` を作成します。
既存の `/usr/local/etc/routerd/router.yaml` は上書きしません。

## ライブ ISO で試す

リリースページでは、Alpine ベースの起動可能なライブ ISO も公開します。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-live.iso
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-live.iso.sha256
sha256sum -c routerd-live.iso.sha256
```

ISO を Proxmox VE のテスト VM に接続して起動します。
コンソールには routerd の初期設定手順が表示されます。
root でログインすると、同じ `install.sh configure` ウィザードを起動できます。
ISO はデモや短時間の試用に使います。
永続的なルーターとして使う場合は、リリースアーカイブからディスクへ導入します。

ライブ ISO はビデオコンソールとシリアルコンソールの両方を有効にします。
Proxmox VE では、シリアルソケットを追加し、`qm terminal` で接続します。

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

DHCP や RA を試す場合は、`net1` に隔離された LAN ブリッジを使います。
シリアルコンソールは 115200 8N1 です。
ウィザードはプレーンテキストで表示します。
そのため、`qm terminal`、フレームバッファーコンソール、最小構成の端末で同じように動きます。

ライブ ISO には 2 つの動作があります。

- **一時デモモード:** USB ストレージを選びません。
  設定とログは RAM 上に置かれ、再起動で消えます。
- **永続ルーターモード:** ウィザードで USB パーティションを選びます。
  ウィザードは `router.yaml` を USB デバイスへ保存します。
  次回起動時は ISO が USB デバイスをマウントし、設定を復元して自動的に反映します。

永続モードでは、USB パーティションに `ROUTERD` というラベルを付けます。
リムーバブルデバイスが複数ある場合は、カーネル引数に
`routerd.usb=/dev/sdX1` を指定できます。
helper は `blkid` で `ext4`、`vfat`、`exfat` を判定します。
既定では `async,noatime` でマウントします。
同期書き込みを明示したい場合だけ、`routerd.usb_mount=sync` を指定します。

ログは `/run/routerd/logs` の tmpfs に一時保存します。
ウィザードでは、1 日 1 回の書き出しジョブを有効にできます。
このジョブは設定、状態スナップショット、圧縮したログアーカイブを USB デバイスへコピーします。
既定の tmpfs ログ上限は 100 MiB です。
上限を超えた場合は、古いログファイルから削除します。

USB を安全に取り外す場合は、次を実行します。

```sh
/usr/share/routerd/live-persistence.sh flush
/usr/share/routerd/live-persistence.sh umount
```

配置、mount option、Alpine `lbu` の動きは
[Operations → USB 永続化](./operations/usb-persistence) を参照してください。

`routerd-live-vYYYYMMDD.HHmm.iso` のような版番号付き ISO も公開します。

## 実行時の依存パッケージ

既定では、`install.sh` が既知の OS パッケージを導入します。
一覧だけ確認するには、次を実行します。

```sh
./install.sh --list-deps
```

別の仕組みでパッケージを管理する場合は、自動導入を止めます。

```sh
sudo ./install.sh --no-install-deps
```

依存パッケージだけ導入することもできます。

```sh
sudo ./install.sh --deps-only
```

Tailscale は任意です。
導入する場合は `--with-tailscale` を付けます。

```sh
sudo ./install.sh --with-tailscale
```

### Debian / Ubuntu

インストーラーは `apt-get` を使い、次を導入します。

```text
ca-certificates curl dnsmasq-base nftables wireguard-tools chrony bind9-dnsutils tcpdump cron jq ppp pppoe conntrack iproute2 iputils-ping iputils-tracepath net-tools kmod radvd strongswan-swanctl iptables
```

### Fedora 系

インストーラーは `dnf` を使い、次を導入します。

```text
ca-certificates curl dnsmasq nftables wireguard-tools chrony bind-utils tcpdump cronie jq ppp rp-pppoe conntrack-tools iproute iputils traceroute kmod radvd strongswan iptables
```

### Arch 系

インストーラーは `pacman` を使い、次を導入します。

```text
ca-certificates curl dnsmasq nftables wireguard-tools chrony bind tcpdump cronie jq ppp rp-pppoe conntrack-tools iproute2 iputils traceroute kmod radvd strongswan iptables
```

### Alpine

インストーラーは `apk` を使い、次を導入します。

```text
alpine-conf ca-certificates curl dnsmasq nftables wireguard-tools chrony bind-tools tcpdump cronie jq ppp ppp-pppoe conntrack-tools iproute2 iputils iputils-tracepath kmod radvd strongswan iptables util-linux e2fsprogs dosfstools exfatprogs
```

`alpine-conf` は `lbu` を提供します。
routerd はライブ ISO で `lbu` を使い、ルーター設定と選択したローカル状態を USB メディアへ保存します。

### FreeBSD

インストーラーは `pkg` を使い、次を導入します。

```text
ca_root_nss curl dnsmasq wireguard-tools mpd5 bind-tools tcpdump jq chrony strongswan
```

FreeBSD の `pf`、`ifconfig`、`route`、`sysctl`、`service`、`sysrc`、`cron`、
`netstat`、`sockstat`、`ping`、`traceroute` は基本システムの機能です。
パッケージとしては導入せず、コマンドの存在だけ確認します。

### NixOS

NixOS では、パッケージ状態を NixOS 設定に残すべきです。
`install.sh` は NixOS を検出した場合、`nix-env` は実行せず警告を出します。
NixOS 設定、または routerd の `Package` リソースで宣言してください。
リリースインストーラーで `/usr/local/sbin/routerd` の実行ファイルを配置することは
できますが、NixOS では systemd unit の導入、有効化、再起動は行いません。
routerd サービスは NixOS module で宣言的に管理してください。

## アップグレード

新しいアーカイブを展開し、同じインストーラーを実行します。

```sh
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

`/usr/local/sbin/routerd` が存在する場合、インストーラーはアップグレードモードに
切り替わります。
古い `routerd --version` と新しい `routerd --version` を表示します。
実行ファイルとサービステンプレートを置き換え、設定と状態を保持します。
routerd サービスが起動中であれば再起動します。
systemd ホストでは、再起動した `routerd.service` の status socket を待ち、
routerd が管理する unit ファイルの更新が落ち着いた後で、更新が必要な
routerd ヘルパーサービスだけを再起動します。
削除済みのアップグレード前バイナリを実行している場合、またはヘルパーの
プロセス起動後に unit ファイルが更新されている場合にだけ再起動します。
`/etc/systemd/system/routerd.service` が routerd の設定で管理されている場合は、
アーカイブに含まれるテンプレートで上書きせず、その unit を保持します。

置き換えるファイルは `*.backup.YYYYMMDDHHMMSS` に退避します。
途中で失敗した場合は、一時バックアップから復元します。

routerd 自身が `routerd.service` を `generated service artifacts` リソースとして管理している場合、
unit file の変更は慎重に扱います。
apply の途中で自分自身を直接再起動するのではなく、`systemd-run` で少し遅らせた
self-restart を予約します。
VRRP または ingress service リソースを同じ設定に含む場合は、生成される
`routerd.service` に keepalived 用の書き込み可能 path と capability を自動追加します。
BGP は長寿命 `routerd-bgp` daemon を local gRPC Unix socket で制御するため、
FRR group や FRR runtime directory は不要です。

よく使うオプション:

```sh
sudo ./install.sh --no-restart
sudo ./install.sh --dry-run
sudo ./install.sh --verbose
sudo ./install.sh --no-config-update
```

## 配置先

| 項目 | Linux | FreeBSD |
| --- | --- | --- |
| 設定 | `/usr/local/etc/routerd/router.yaml` | `/usr/local/etc/routerd/router.yaml` |
| 設定例 | `/usr/local/etc/routerd/router.yaml.sample` | `/usr/local/etc/routerd/router.yaml.sample` |
| 実行ファイル | `/usr/local/sbin` | `/usr/local/sbin` |
| サービステンプレート | `/etc/systemd/system/routerd.service` | `/usr/local/etc/rc.d/routerd` |
| 実行時ソケット | `/run/routerd` | `/var/run/routerd` |
| 永続状態 | `/var/lib/routerd` | `/var/db/routerd` |

インストーラーは次の状態を削除しません。

- `/usr/local/etc/routerd/router.yaml`
- `/var/lib/routerd`
- `/var/db/routerd`
- `/run/routerd`
- `/var/run/routerd`
- `/var/log/otelcol`

## 最初の設定

最初の試用では、組み込みの初期設定ウィザードを使えます。

```sh
sudo ./install.sh configure
```

ウィザードは WAN インターフェース、LAN インターフェース、LAN アドレス、
LAN 向けサービス、管理経路の置き場所、任意の USB 永続化を確認します。
生成した候補は `/usr/local/etc/routerd/router.yaml.configure` に保存します。
既存の設定がある場合は差分を表示します。
確認後に `/usr/local/etc/routerd/router.yaml` へ導入します。
その後、`routerd validate`、`routerd plan`、`routerd apply --once` を実行します。

自動化では、環境変数で値を渡して質問を省略できます。

```sh
sudo ROUTERD_WAN_INTERFACE=ens18 \
  ROUTERD_LAN_INTERFACE=ens19 \
  ROUTERD_LAN_ADDRESS=192.168.10.1/24 \
  ROUTERD_LAN_CIDR=192.168.10.0/24 \
  ROUTERD_MGMT_MODE=lan \
  ROUTERD_ENABLE_USB_PERSISTENCE=no \
  ./install.sh configure --non-interactive --yes
```

ライブ ISO で USB 永続化を使う場合は、次の値を指定します。

```sh
sudo ROUTERD_ENABLE_USB_PERSISTENCE=yes \
  ROUTERD_USB_DEVICE=/dev/sdb1 \
  ROUTERD_USB_FLUSH=yes \
  ROUTERD_LOG_TMPFS_LIMIT=100M \
  ./install.sh configure --non-interactive --yes
```

YAML ファイルの生成だけ行う場合は、`--no-apply` を使います。

```sh
sudo ./install.sh configure --no-apply
```

手動で設定することもできます。
設定例をコピーし、インターフェース名などを編集します。

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 /usr/local/etc/routerd/router.yaml.sample /usr/local/etc/routerd/router.yaml
sudo vi /usr/local/etc/routerd/router.yaml
```

検証し、計画を確認します。

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run
```

管理経路が安全なことを確認してから反映します。

```sh
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

一度だけの反映が正常なら、サービスを起動します。

```sh
sudo systemctl enable --now routerd.service
```

FreeBSD では次のようにします。

```sh
sudo sysrc routerd_enable=YES
sudo service routerd start
```

## アンインストール

リリースアーカイブには `uninstall.sh` も含まれます。
既定では、実行ファイル、サービステンプレート、実行時ファイルを削除します。
設定と状態は残します。

```sh
sudo ./uninstall.sh --yes
```

削除範囲を広げる場合は、明示的に指定します。

```sh
sudo ./uninstall.sh --yes --purge-config
sudo ./uninstall.sh --yes --purge-state
sudo ./uninstall.sh --yes --all
```

`--dry-run` で削除内容だけ確認できます。

## 開発者向けワークフロー

Makefile は開発用です。
テスト、ビルド、スキーマ生成、設定例の検証、Web サイトビルド、
リリースアーカイブ作成に使います。

```sh
make test
make check-schema
make validate-example
make website-build
make dist ROUTERD_OS=linux GOARCH=amd64 VERSION="$(git describe --tags --abbrev=0)"
```

利用者向けの導入経路として Makefile は使いません。
リリースアーカイブと `install.sh` が標準の配置方法です。
