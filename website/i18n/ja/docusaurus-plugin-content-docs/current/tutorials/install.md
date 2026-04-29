---
title: インストール
sidebar_position: 1
---

# インストール

このページでは Linux ホストに routerd をビルド・インストールし、デーモンを動かすところまでを通します。
ここまで終わると、routerd のバイナリ、標準のインストールレイアウト、systemd ユニットがそろいます。
次のチュートリアル [最初のルーター](./first-router) で最小設定を作ります。

routerd は v1alpha1 のソフトウェアです。リモートのルーターに向ける前に、ラボ用の VM や
コンソール接続のあるホストで一度通してください。

## 1. ビルドする

ソースツリーで:

```bash
make build
```

次の 2 つのバイナリが生成されます。

- `bin/routerd` — apply エンジンと serve デーモン
- `bin/routerctl` — 読み取り専用の確認用 CLI

ビルドは既定で `CGO_ENABLED=0` なので、バイナリは静的リンクされた Go です。

## 2. ソースレイアウトをインストールする

routerd は `/usr/local` 配下のレイアウトを使います。ソースインストールと将来の
パッケージ化の両方を意識したものです。

```bash
sudo make install
```

既定パス:

| パス | 用途 |
|---|---|
| `/usr/local/sbin/routerd` | apply / serve バイナリ |
| `/usr/local/sbin/routerctl` | 確認用 CLI |
| `/usr/local/etc/routerd/router.yaml` | ルーター設定 (自分で用意) |
| `/usr/local/libexec/routerd/plugins` | ローカルプラグイン置き場 |
| `/run/routerd` | ランタイムソケットと pid |
| `/var/lib/routerd` | 状態データベース |

## 3. 設定ファイルを置く

例のいずれかをデーモンが見る場所にコピーします。

```bash
sudo install -m 0644 examples/basic-dhcp.yaml /usr/local/etc/routerd/router.yaml
```

最小構成のルーターをこの YAML に何を書くべきかは [最初のルーター](./first-router) で扱います。

## 4. ドライラン apply を実行する

デーモンを有効化する前に、`--dry-run` 付きで apply を回し、計画を確認します。

```bash
sudo /usr/local/sbin/routerd apply \
  --config /usr/local/etc/routerd/router.yaml \
  --once \
  --dry-run
```

出力は構造化された JSON です。どのリソースが正常か、どれがホスト状態とずれているか、
何を routerd が変えようとしているかを表します。すでに別のツール (cloud-init、
netplan) がネットワークを設定しているホストでは、特に念入りに読んでください。
routerd が同じファイルの所有権を取りに行く可能性があります。

計画が意図どおりなら `--dry-run` を外します。

```bash
sudo /usr/local/sbin/routerd apply \
  --config /usr/local/etc/routerd/router.yaml \
  --once
```

## 5. デーモンを有効化する

`apply --once` が良さそうなら、systemd ユニットを入れてデーモンを起動します。

```bash
sudo make install-systemd
sudo systemctl daemon-reload
sudo systemctl enable --now routerd.service
```

デーモンは `/run/routerd/` 配下にコントロールソケットを保ち、YAML を定期的に
再 apply します。`routerctl` も同じソケット経由で状態を読みます。

```bash
routerctl get
routerctl describe interface/wan
```

## ホスト側で何が変わったか

`apply` で書き換わる可能性があるもの:

- `/etc/systemd/network/*.network` のドロップイン
  (`10-netplan-*.network.d/` 配下)
- 管理対象 dnsmasq の `/etc/dnsmasq.d/*.conf`
- NAT とファイアウォールの `/etc/nftables.d/*.conf`
- 状態データベース `/var/lib/routerd/routerd.db`

これらのうち routerd が所有しているものは
[所有台帳](../reference/resource-ownership) に記録されます。
routerd が入れていないファイルは触りません。

## 次へ

- [最初のルーター](./first-router) — 最小の WAN + LAN 構成
- [apply と render](../concepts/apply-and-render) — 今使った動詞を詳しく
- [状態と所有](../concepts/state-and-ownership) — `/var/lib/routerd` の中身
