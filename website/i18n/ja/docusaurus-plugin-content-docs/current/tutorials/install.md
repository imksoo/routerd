---
title: インストール
sidebar_position: 1
---

# インストール

![リリースアーカイブを Ubuntu Server VM に入れ、設定を検証してからサービスを起動する流れ](/img/diagrams/tutorial-install.png)

このページは、最初の練習用の **Ubuntu Server VM** に routerd を入れる手順です。
本物のルーターや、SSH しか入る方法がない機械では始めないでください。
ハイパーバイザーの画面、シリアルコンソール、または別の管理 NIC を用意します。

## 0. VM を安全に用意する

VM には、次のように役割を分けると安全です。

- 管理用 NIC（ネットワークにつなぐ口）: SSH などで VM に入るための線。最初は routerd に任せません。
- WAN 用 NIC: 上流のテスト用ネットワークにつなぐ線。
- LAN 用 NIC: テスト用クライアントだけをつなぐ線。

WAN と LAN は、普段のネットワークから分けます。WAN 用 NIC や LAN 用 NIC が
止まっても、コンソールから VM を操作できることを先に確かめてください。

## 1. リリースアーカイブを入手する

次は Linux amd64 の例です。まず [GitHub Releases](https://github.com/imksoo/routerd/releases)
で、使う版と CPU を確認してください。

```sh
RELEASE=v20260707.1514
curl -fLO https://github.com/imksoo/routerd/releases/download/${RELEASE}/routerd-linux-amd64.tar.gz
curl -fLO https://github.com/imksoo/routerd/releases/download/${RELEASE}/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

arm64 の Ubuntu Server では、ファイル名の `linux-amd64` を `linux-arm64` に
変えます。`install.sh` は必要な実行時パッケージを確認し、`routerd` と
`routerctl` を `/usr/local/sbin` に置きます。Go や Makefile は不要です。

新しい導入では、インストーラーだけでサービスは起動しません。設定を読んで
安全を確かめてから、あとで自分で起動します。

```sh
routerd --version
```

## 2. 次のページで YAML を作る

設定例は `/usr/local/etc/routerd/router.yaml.sample` に置かれますが、最初は
そのままサービスを起動しません。次の [はじめに](./getting-started.md) で
`first-router.yaml` を実際に作り、`routerd validate` と使い捨ての場所での
dry-run を行います。このページだけでは `first-router.yaml` はまだ存在しません。

## 3. サービスを起動するのは dry-run の後

次のページで WAN と LAN の名前、管理経路、dry-run の出力を確認してから、
設定を標準の場所へ置き、サービスを起動します。`routerctl` はサービスが起動して
ローカルソケットを作った後にだけ使います。初回は `sudo routerctl get status` の
ように `sudo` を付けます。

## Ubuntu 以外について

FreeBSD には rc.d と導入の土台があり、NixOS にも導入用の土台があります。
ただし、Ubuntu と同じネットワーク設定の生成や機能がそろっているわけではありません。
初めて試す手順としては Ubuntu Server を使い、他の OS は
[対応プラットフォーム](../platforms.md) を読んでから進めてください。

## 次に読むもの

- [はじめに](./getting-started.md) — YAML の作成、検証、dry-run
- [インストールとアップグレード](../install-and-upgrade.md) — 更新、削除、配置先
