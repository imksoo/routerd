# routerd

[プロジェクトサイトとドキュメント: routerd.net](https://routerd.net/)

`routerd` は、YAML に書いたルーター設定を読み取り、Linux マシンをルーターとして動かすための OS 設定へ反映するプログラムです。場当たり的なシェルコマンドの集まりではなく、Git で確認しやすいルーター設定として管理することを目指しています。

インターフェース、DHCP、DNS 転送、IPv6 プレフィックス委譲、PPPoE、DS-Lite、NAT、ポリシールーティング、最小限の標準拒否ファイアウォール、ヘルスチェック、状態確認を扱えます。まだプレリリースなので、リモートのルーターへ反映する前に必ず計画表示と予行実行を確認してください。

現在の主な対象は Ubuntu Server です。インストール先は `/usr/local` 配下を基本にし、将来のパッケージ化でも包みやすい形にしています。

## 現在できること

- YAML によるルーター設定
- Kubernetes 風の `apiVersion` / `kind` / `metadata.name` / `spec`
- インターフェース別名の解決
- IPv4/IPv6 アドレス計画
- systemd-networkd 追加設定による IPv6 プレフィックス委譲
- pppd/rp-pppoe による PPPoE インターフェース設定
- dnsmasq による DHCPv4、DHCPv6/RA、DNS 転送/キャッシュ
- 実行中の sysctl 管理
- syslog/journald または信頼済みローカルログプラグインへの内部イベント出力
- nftables による最小ファイアウォールポリシーと IPv4 送信元 NAT
- nftables の印付けと `ip rule` による IPv4 ポリシールーティング
- IPv6 RA による経路 MTU 広告と TCP MSS 調整
- DS-Lite ipip6 トンネル、複数トンネルのハッシュ分散
- Unix ドメインソケット上の HTTP+JSON デーモン操作 API
- クライアントコマンド `routerctl`
- 状態 JSON
- 予行実行と計画表示
- 信頼済みローカルプラグインの土台

まだ限定的なもの:

- リモートからのプラグイン導入
- 完全な巻き戻し

最小構成の範囲は作業上の目安であり、固定された約束ではありません。小さな設計変更で将来の移行コストを下げられる場合や、ルーターとしての安全性を上げられる場合は、初期の前提にこだわらず見直します。

英語版は [README.md](README.md) にあります。

## 必要なもの

- Go 1.24 以上
- `make`
- `iproute2`
- `jq`
- `dnsmasq`
- `nftables`
- `conntrack`
- PPPoE を使う場合は `pppd`

Ubuntu 例:

```sh
sudo apt-get update
sudo apt-get install -y golang-go make iproute2 jq dnsmasq nftables conntrack ppp
```

`conntrack` は、複数 DS-Lite トンネルのポリシールーティングや通信ごとの印の診断にも使います。
PPPoE リソースは、ディストリビューションの PPP パッケージに含まれる pppd と rp-pppoe プラグインを使います。

## ビルド

```sh
make build
```

または:

```sh
go build ./cmd/routerd
go build ./cmd/routerctl
```

生成物は `bin/routerd` と `bin/routerctl` に作られます。

手元のビルド依存を確認する場合:

```sh
make check-build-deps
```

YAML 入力用のスキーマを Go の型から再生成する場合:
操作 API の JSON Schema と OpenAPI 定義も同時に生成します。

```sh
make generate-schema
make check-schema
```

## インストール

手元のソースからインストールする場合:

```sh
sudo make install
```

このインストール処理は単純なファイル配置に寄せています。あとで ports、dpkg、その他のパッケージ方式から同じ配置を包みやすくするためです。必要に応じて `PREFIX`、`DESTDIR`、`SYSCONFDIR`、`PLUGINDIR`、`RUNDIR`、`STATEDIR`、`SYSTEMDUNITDIR` を上書きできます。

一時ディレクトリへ配置を作る例:

```sh
make install DESTDIR=/tmp/routerd-root
```

インストール用 tar アーカイブを作る場合:

```sh
make dist
```

Go や make のないリモートのテストホストへ入れる場合:

```sh
make remote-install REMOTE_HOST=user@router.example
```

リモートホストの依存確認:

```sh
make check-remote-deps REMOTE_HOST=user@router.example
```

設定ファイルだけをリモートホストへ入れる場合:

```sh
make remote-install-config REMOTE_HOST=user@router.example CONFIG=path/to/router.yaml
```

systemd を使う Linux でユニットを明示的に入れる場合:

```sh
sudo make install-systemd
```

## テスト

```sh
make test
```

または:

```sh
go test ./...
```

## よく使うコマンド

```sh
make validate-example
make dry-run-example
make website-build
```

直接実行する場合:

```sh
routerd validate --config examples/router-lab.yaml
routerd plan --config examples/router-lab.yaml
routerd adopt --config examples/router-lab.yaml --candidates
routerd reconcile --config examples/router-lab.yaml --once --dry-run
routerd serve --config examples/router-lab.yaml --socket /run/routerd/routerd.sock
routerctl status
routerctl show napt --limit 20
routerctl plan
```

実反映:

```sh
sudo routerd reconcile --config /usr/local/etc/routerd/router.yaml --once
```

`reconcile --once` は、routerd が管理する netplan、networkd 追加設定、dnsmasq、nftables、sysctl、DS-Lite トンネル、ポリシールーティングを反映できます。リモートルーターで実行する前に、`plan` と `dry-run` を確認してください。cloud-init や既存 netplan が管理しているインターフェースを不用意に奪わないよう、routerd は引き継ぎが必要な状態を検出して計画に出します。
既に実機上にある設定を routerd の管理下へ入れる場合は、`routerd adopt --candidates`
で候補を確認し、問題がなければ `routerd adopt --apply` で実機状態を変えずに
ローカル台帳へ記録します。

## ドキュメント

- [API v1alpha1 日本語](docs/api-v1alpha1.ja.md)
- [リソース所有](docs/resource-ownership.ja.md)
- [操作 API v1alpha1 日本語](docs/control-api-v1alpha1.ja.md)
- [プラグインプロトコル 日本語](docs/plugin-protocol.ja.md)
- [はじめに](docs/tutorials/getting-started.md)
- [更新履歴](docs/releases/changelog.md)
- [英語 README](README.md)

公開サイトは `website/` 配下にあり、Docusaurus でビルドします。英語版と日本語版を Cloudflare Pages に公開する構成です。Cloudflare Pages ではルートディレクトリを `website`、ビルドコマンドを `npm ci && npm run build`、出力ディレクトリを `build` にします。`routerd.net` は Cloudflare Pages の独自ドメインとして追加します。

## 既定パス

- 設定ファイル: `/usr/local/etc/routerd/router.yaml`
- プラグインディレクトリ: `/usr/local/libexec/routerd/plugins`
- 本体コマンド: `/usr/local/sbin/routerd`
- クライアントコマンド: `/usr/local/sbin/routerctl`

Linux:

- 実行時ディレクトリ: `/run/routerd`
- 状態保存ディレクトリ: `/var/lib/routerd`
- 状態ファイル: `/run/routerd/status.json`
- 操作用ソケット: `/run/routerd/routerd.sock`
- ロックファイル: `/run/routerd/routerd.lock`
