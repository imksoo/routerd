---
title: nDPI エージェントのネイティブパッケージ
---

# nDPI エージェントのネイティブパッケージ

routerd の通常の Linux リリースアーカイブは `CGO_ENABLED=0` でビルドし、同梱する routerd のバイナリをすべて静的リンクに保ちます。任意の `routerd-ndpi-agent-libndpi` アーカイブは、ネイティブの nDPI 分類が必要なホスト向けの例外パッケージです。

このアーカイブに含まれるのは、次のものだけです。

- `bin/routerd-ndpi-agent`
- `share/doc/README.md`
- `share/doc/VERSION`
- `share/doc/TARGET`

このバイナリは `CGO_ENABLED=1 -tags libndpi` でビルドし、対象システムの `libndpi` ランタイムにリンクします。すでに通常の routerd アーカイブをインストール済みのホストで、それを上書きするためのものです。

## インストール

通常の routerd リリースアーカイブと、対応するネイティブエージェントのアーカイブを両方ダウンロードし、インストーラーに 1 つのトランザクションとして適用させます。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz.sha256
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-ndpi-agent-libndpi-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-ndpi-agent-libndpi-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-ndpi-agent-libndpi-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh --with-ndpi \
  --with-ndpi-archive ./routerd-ndpi-agent-libndpi-linux-amd64.tar.gz
```

ホストには、アーカイブのビルド時と同じ共有ライブラリ ABI を持つ `libndpi` ランタイムパッケージが必要です。Debian/Ubuntu では、任意のランタイム依存関係を次でインストールします。

```sh
sudo apt-get install libndpi-bin
```

ネイティブバックエンドが有効になっているかを確認します。

```sh
sudo curl --silent --unix-socket /run/routerd/ndpi-agent/default.sock \
  http://unix/v1/status
```

応答に `"libndpiLoaded": true` が含まれているはずです。

## アップグレード時の注意

通常の routerd アーカイブには、既定の静的な `routerd-ndpi-agent` が含まれます。
アップグレード時、`install.sh` は、既存のネイティブエージェントの `selftest` が `"libndpiLoaded": true` を返し、かつアーカイブ側のエージェントが返さない場合に、既存のネイティブエージェントを保持します。

ネイティブのアプリケーション層分類が必要なホストでは、通常のインストーラーを `--with-ndpi` 付きで実行します。最終的にインストールされたエージェントが `"libndpiLoaded": true` を返さない場合、インストーラーは失敗します。これにより、静的なフォールバックがネイティブ nDPI の意図を黙って満たしてしまうことを防ぎます。

新規インストールの場合や、ネイティブエージェントのアーカイブを正本として明示したい場合は、`--with-ndpi-archive PATH` を渡します。インストーラーは、アーカイブのターゲットマーカーを検証し、安全でない tar パスを拒否し、隣接する `.sha256` ファイルがあれば検証し、アーカイブ側のエージェントが `"libndpiLoaded": true` を返すかを確認します。オーバーレイが失敗した場合は、インストール全体をロールバックします。

## 設定

`routerd-dpi-classifier` は、`--engine auto` または `--engine ndpi-agent` と、エージェントのソケットを指す `--ndpi-agent-socket` を指定して構成する必要があります。
`auto` を推奨します。ネイティブエージェントが利用できない場合に、組み込みの分類器へフォールバックするためです。

非推奨の `--ndpi-reader` オプションは、ネイティブの nDPI 分類を有効にしません。
