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
latest release には `routerd-20260509.16-linux-amd64.tar.gz` のような
版番号付きアーカイブもあります。
特定の版に固定する場合は、版番号付きアーカイブを使います。

`install.sh` は新規導入かアップグレードかを自動判定します。
実行ファイルを `/usr/local/sbin` に配置し、サービステンプレートを導入します。
また、`/usr/local/etc/routerd/router.yaml.sample` を作成します。
既存の `/usr/local/etc/routerd/router.yaml` は上書きしません。

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
ca-certificates curl dnsmasq nftables wireguard-tools chrony bind9-dnsutils tcpdump cron jq ppp pppoe conntrack iproute2 iputils-ping iputils-tracepath net-tools kmod radvd strongswan-swanctl iptables
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

置き換えるファイルは `*.backup.YYYYMMDDHHMMSS` に退避します。
途中で失敗した場合は、一時バックアップから復元します。

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
make dist ROUTERD_OS=linux GOARCH=amd64 VERSION=20260509.16
```

利用者向けの導入経路として Makefile は使いません。
リリースアーカイブと `install.sh` が標準の配置方法です。
