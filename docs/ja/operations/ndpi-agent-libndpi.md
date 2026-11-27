---
title: nDPI エージェントのネイティブパッケージ
---

# nDPI エージェントのネイティブパッケージ

routerd の Linux 用バイナリは `CGO_ENABLED=0` でビルドし、静的リンクにしています。任意の `routerd-ndpi` Debian パッケージは、ネイティブの nDPI 分類が必要なホスト向けの例外です。含まれるのは `/usr/local/sbin/routerd-ndpi-agent` だけで、`CGO_ENABLED=1 -tags libndpi` でビルドし、システムの `libndpi` にリンクしています。`routerd` パッケージには nDPI エージェントを含めません。

## 導入

`routerd` パッケージと並べて導入します。対応する `libndpi` のランタイムは、パッケージの依存関係から apt が導入します。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-ndpi-linux-amd64.deb
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-ndpi-linux-amd64.deb.sha256
sha256sum -c routerd-ndpi-linux-amd64.deb.sha256
sudo apt install ./routerd-ndpi-linux-amd64.deb
```

パッケージを更新すると、`routerd-ndpi-agent.service` が有効または起動中であれば再起動します。ネイティブのバックエンドが有効かは、次で確認します。

```sh
sudo curl --silent --unix-socket /run/routerd/ndpi-agent/default.sock \
  http://unix/v1/status
```

応答に `"libndpiLoaded": true` が含まれていれば有効です。

## 設定

`FirewallEventLog` リソースを有効にします。routerd は
`routerd-firewall-logger` と `routerd-ndpi-agent` を起動し、ロガーから
エージェントの Unix ソケットへ採取したパケットを直接送ります。別の分類プロセスや
組み込み分類器への代替処理はありません。
