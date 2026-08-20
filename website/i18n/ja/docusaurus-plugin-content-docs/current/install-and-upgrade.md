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
RELEASE=v20260707.1514
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

設定の置き場所は `/usr/local/etc/routerd/router.yaml` です。
まずは [はじめに](./tutorials/getting-started.md) の小さな YAML を
`first-router.yaml` として作ってください。サービスがまだ動いていない段階では、
`routerctl` を使わず、`routerd` 本体で確認します。

```sh
LAB_DIR="$(mktemp -d)"
sudo routerd validate --config ./first-router.yaml
sudo routerd apply --config ./first-router.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
```

`validate` は YAML の書き方を調べます。dry-run は、依存関係と生成する内容を
調べますが、本当のネットワーク変更は行いません。state、ledger、status の
保存先を `LAB_DIR` の下にしているので、普段使う状態ファイルにも触れません。

:::tip state、ledger、status とは

- **state** は、routerd が見た状態を保存するデータベースです。
- **ledger** は、routerd が所有するファイルや設定の記録です。
- **status** は、その dry-run の結果を書いた JSON ファイルです。

初回の dry-run では、どれも使い捨ての場所を指定します。

:::

## サービスを起動する

ここから先は VM のネットワークを変える可能性があります。コンソールを開いたまま、
インターフェース名、管理経路、dry-run の出力を確認してから実行します。

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 ./first-router.yaml /usr/local/etc/routerd/router.yaml
sudo systemctl enable --now routerd.service
sudo systemctl is-active routerd.service
```

サービスが起動して初めて、`routerctl` でローカルソケットの状態を確認できます。

```sh
sudo routerctl get status
sudo routerctl get events --limit 20
```

## 更新する

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
