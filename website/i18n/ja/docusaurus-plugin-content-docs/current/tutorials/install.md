---
title: インストール
sidebar_position: 1
---

# インストール

![リリースアーカイブから routerd を導入し、依存パッケージとサービステンプレートを入れ、設定と状態を保持して validate-plan-dry-run する流れ](/img/diagrams/tutorial-install.png)

routerd はリリースアーカイブから導入します。
ルーターホストに Go や Makefile は不要です。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

Linux arm64 ホストでは `routerd-linux-arm64.tar.gz` を使います。

FreeBSD では `routerd-freebsd-amd64.tar.gz` を取得し、同じ `./install.sh` を実行します。
FreeBSD arm64 ホストでは `routerd-freebsd-arm64.tar.gz` を使います。
特定の版に固定したい場合は、リリースページにある版番号付きアーカイブを使います。

Linux 用アーカイブには、静的リンクした routerd バイナリを含めます（`CGO_ENABLED=0`）。
ルーターホストの glibc 版には依存しません。

インストーラーは次を行います。

- 対応するパッケージマネージャーで実行時パッケージを導入します。
- 実行ファイルを `/usr/local/sbin` に配置します。
- systemd または rc.d のサービステンプレートを配置します。
- `/usr/local/etc/routerd/router.yaml.sample` を作成します。
- 既存の `/usr/local/etc/routerd/router.yaml` は保持します。
- `/var/lib/routerd` または `/var/db/routerd` の状態は保持します。
- 読み取り専用の状態ソケットがある場合は `routerctl status` を実行します。

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

routerctl validate -f /usr/local/etc/routerd/router.yaml --replace
routerctl plan -f /usr/local/etc/routerd/router.yaml --replace
```

管理経路が残ることを確認してから反映します。

```sh
sudo routerctl apply -f /usr/local/etc/routerd/router.yaml --replace
```

OS 別のパッケージ一覧、アップグレード、アンインストール、開発者向けのリリース手順は [インストールとアップグレード](../install-and-upgrade.md) を参照してください。

ディスクに導入せず試す場合は `routerd-live.iso` を起動します。
root でログインすると、同じ `install.sh configure` ウィザードが起動します。
Proxmox VE の `qm terminal` によるシリアルコンソールにも対応します。
ウィザードで USB 永続化を選べば、ライブ ISO をディスクレスの永続ルーターとして使えます。
USB 永続化を選ばない場合は一時的なデモとして動作し、再起動で設定が消えます。
