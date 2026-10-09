---
title: インストールとアップグレード
---

# インストールとアップグレード

![リリースアーカイブの取得、検証、導入、設定の確認、サービス起動、更新の流れ](/img/diagrams/install-and-upgrade.png)

routerd はリリースアーカイブから導入します。ルーターホストに Go や Makefile は
必要ありません。最初の対象は Ubuntu Server です。

:::caution 初回は隔離した VM で

初めての導入は、普段の回線を運ぶルーターでは行わないでください。
隔離した Ubuntu Server VM、VM コンソール、変更する NIC とは別の管理経路を
用意します。`routerd.service` を起動するとネットワークが変わることがあります。

:::

## Ubuntu Server への新規導入

[GitHub Releases](https://github.com/imksoo/routerd/releases) で、使う版と CPU に
合うアーカイブを選びます。次は Linux amd64 の例です。

```sh
RELEASE=v20260914.0729
curl -fLO https://github.com/imksoo/routerd/releases/download/${RELEASE}/routerd-linux-amd64.tar.gz
curl -fLO https://github.com/imksoo/routerd/releases/download/${RELEASE}/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

arm64 では `linux-amd64` を `linux-arm64` に読み替えます。
インストーラーは実行時パッケージを確認し、実行ファイルと systemd の
サービス定義、設定例を置きます。新規導入では設定がないため、サービスを
勝手に開始しません。

```sh
routerd --version
```

## 最初の設定は、起動前に確認する

[安全なはじめ方](./tutorials/getting-started.md)でオフラインの確認を行い、
[最初のルーター](./tutorials/first-router.md)で DHCP/NAT の完成した設定、起動、
状態、LAN 端末の通信を順に確認します。`router.yaml.sample` は ISP 固有の設定も
含む大きな例です。インターフェース名だけ直せば使える既定値ではありません。
通常の新規 `./install.sh` はファイルを配置しますが、サービスは起動しません。

## 更新する

更新時には、現行routerdが生成した`routerd.service`を保持します。
`Managed by routerd`というコメントだけでは旧形式と判断しません。
削除済みの`--controller-chain`を含むunitは移行対象です。
設定の世代切替では、設定が変わらない管理下DHCPクライアントを維持します。
VRRPの`gracefulActivation`はVIPの初回公開を待機し、公開済みのMASTERでは
一時的な条件低下によるVIP撤去を行いません。BACKUP等への降格時は撤去します。
継続保有には同一アドレス・インターフェースの正常公開済みMASTER記録が必要です。
正常公開履歴は現在の観測状態と分離し、観測エラーでは保持します。
降格・VIP不在の確認・公開失敗では失効し、異なるアドレスやインターフェースへ流用しません。
アドレスだけが残り公開記録がない場合は、初回のreadiness判定を適用します。

更新も、まず VM コンソールと管理経路を確認してから行います。新しいアーカイブを
展開し、同じ `install.sh` を実行します。

```sh
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

既存の `/usr/local/etc/routerd/router.yaml` と状態ディレクトリは保持されます。
すでに `routerd.service` が動いている場合、インストーラーはサービスを
再起動することがあります。更新後は、サービスが動いていることを確かめてから
状態を見ます。

```sh
sudo systemctl is-active routerd.service
sudo routerctl get status
```

BGP など、短い再起動でも影響が出る機能を本番で使っている場合は、保守時間を
取り、[変更履歴](./releases/changelog.md) と運用手順を確認してください。

## よく使う導入オプション

```sh
./install.sh --list-deps
sudo ./install.sh --no-install-deps
sudo ./install.sh --deps-only
sudo ./install.sh --dry-run
```

`--dry-run` は、インストーラーが置くファイルや実行するサービス操作を表示する
ためのものです。routerd の設定を確認する dry-run とは別です。

## 配置先

| 項目 | Ubuntu Server |
| --- | --- |
| 設定 | `/usr/local/etc/routerd/router.yaml` |
| 設定例 | `/usr/local/etc/routerd/router.yaml.sample` |
| 実行ファイル | `/usr/local/sbin/routerd`、`/usr/local/sbin/routerctl` |
| systemd サービス | `/etc/systemd/system/routerd.service` |
| 実行時ソケット | `/run/routerd` |
| 永続状態 | `/var/lib/routerd` |

## ライブ ISO

短いデモには Ubuntu ベースのライブ ISO も使えます。VM コンソールで試せますが、
初めて YAML とネットワークの関係を学ぶなら、上の Ubuntu Server VM の手順の方が
確認しやすく安全です。USB に設定を保存するディスクレス mini PC の手順は
[ディスクレス mini PC](./tutorials/diskless-minipc-walkthrough.md) を参照してください。

## アンインストール

リリースアーカイブの `uninstall.sh` は、既定では実行ファイルとサービス定義を
削除し、設定と状態は残します。

```sh
sudo ./uninstall.sh --yes
```

設定や状態まで削除する操作は、内容をバックアップしてから明示的なオプションで
行ってください。先に `--dry-run` で削除予定を確認できます。

## Ubuntu 以外の対応範囲

FreeBSD と NixOS には、導入場所やサービス管理の土台があります。しかし、
Ubuntu と同じネットワーク設定の生成が使えることを意味しません。
[対応プラットフォーム](./platforms.md) を確認し、最初の学習には Ubuntu Server を
使ってください。
