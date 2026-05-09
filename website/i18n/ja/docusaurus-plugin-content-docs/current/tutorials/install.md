---
title: インストール
sidebar_position: 1
---

# インストール

routerd はリリースアーカイブから導入します。
ルーターホストに Go や Makefile は不要です。

```sh
curl -LO https://github.com/imksoo/routerd/releases/download/20260509.9/routerd-20260509.9-linux-amd64.tar.gz
tar -xzf routerd-20260509.9-linux-amd64.tar.gz
sudo ./install.sh
```

Linux arm64 ホストでは `routerd-20260509.9-linux-arm64.tar.gz` を使います。

FreeBSD では `routerd-20260509.9-freebsd-amd64.tar.gz` を取得し、同じ
`./install.sh` を実行します。
FreeBSD arm64 ホストでは `routerd-20260509.9-freebsd-arm64.tar.gz` を使います。

インストーラーは次を行います。

- 対応するパッケージマネージャーで実行時パッケージを導入します。
- 実行ファイルを `/usr/local/sbin` に配置します。
- systemd または rc.d のサービステンプレートを配置します。
- `/usr/local/etc/routerd/router.yaml.sample` を作成します。
- 既存の `/usr/local/etc/routerd/router.yaml` は保持します。
- `/var/lib/routerd` または `/var/db/routerd` の状態は保持します。
- 制御ソケットがある場合は `routerctl status` を実行します。

よく使うオプション:

```sh
./install.sh --list-deps
sudo ./install.sh --no-install-deps
sudo ./install.sh --deps-only
sudo ./install.sh --with-tailscale
sudo ./install.sh --dry-run
```

インストール後、設定ファイルを作成して検証します。

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 /usr/local/etc/routerd/router.yaml.sample /usr/local/etc/routerd/router.yaml
sudo vi /usr/local/etc/routerd/router.yaml

routerd validate --config /usr/local/etc/routerd/router.yaml
routerd plan --config /usr/local/etc/routerd/router.yaml
routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run
```

管理経路が残ることを確認してから反映します。

```sh
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

OS 別のパッケージ一覧、アップグレード、アンインストール、開発者向け
リリース手順は [インストールとアップグレード](../install-and-upgrade.md) を
参照してください。
