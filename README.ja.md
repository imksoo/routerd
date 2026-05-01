# routerd

[プロジェクトサイトとドキュメント: routerd.net](https://routerd.net/)

routerd は、ルータの設定を YAML で宣言しておくと、その通りに振る舞う Linux ルータを動かしてくれる小さなソフトウェアルータです。手元の YAML を書き換えて反映するだけで、インターフェースの上げ下げ、IPv4/IPv6 アドレス、DHCP、DNS、PPPoE、DS-Lite、NAT、ポリシールーティング、ファイアウォール、経路ヘルスチェックまでが連動して切り替わります。場当たり的なシェルコマンドの代わりに、Git で履歴を追えるルータ設定として運用することを目指しています。

主な対象は Ubuntu Server で、インストール先は /usr/local 配下を基本にしています。NixOS と FreeBSD は二次サポート対象として、ビルド、インストール配置、サービスマネージャ連携の下地まで整えています。pf レンダラ（FreeBSD）や NixOS ネイティブのインターフェース設定など、ホスト連携の一部は移植中です。最新の対応状況は [docs/platforms.ja.md](docs/platforms.ja.md) を参照してください。プレリリースのため、リモートのルータへ反映する前には必ず計画表示と予行実行で挙動を確認してください。

## ルータとして何ができるか

- インターフェースの命名と管理範囲を YAML で宣言し、cloud-init や netplan が握っている設定とぶつからないように調整する。
- WAN 側で DHCPv4 / DHCPv6 / IPv6 プレフィックス委譲 / PPPoE / DS-Lite を使ってアドレスや経路を取得する。
- LAN 側に IPv4 静的アドレスや、委譲プレフィックスから派生させた IPv6 アドレスを配る。
- LAN クライアントへ dnsmasq 経由で DHCPv4、DHCPv6、ルータ広告 (RA)、DNS キャッシュとフォワーディングを提供する。
- IPv4 のソース NAT、ポリシールーティング、複数経路のハッシュ分散を行う。
- 複数の上流（PPPoE と DS-Lite など）にヘルスチェックを掛け、健全な候補へデフォルト経路を切り替える。
- 上流ごとの実効 MTU を求めて、IPv6 RA で広告したり、forward 通信に対して TCP MSS クランプを掛ける。
- nftables の最小プリセットで、家庭用ルータ相当のデフォルト拒否ファイアウォールと、必要なサービス公開だけを記述する。
- sysctl、ホスト名、systemd-timesyncd、内部イベントの送出先を同じ YAML から制御する。
- 反映前の計画表示、予行実行、状態 JSON の取得、Unix ドメインソケット越しの制御 API を備える。
- 信頼済みのローカルプラグインで、リソース固有の処理を拡張できる。

リモートからのプラグイン導入と、OS への変更を網羅的に巻き戻す機能はまだ用意していません。ファイアウォールも汎用ルール言語ではなく、家庭用ルータの安全な既定動作とサービス公開に絞っています。これらは作業上の目安であり、安全性や移行コストの面で見直す価値があれば順次広げていきます。

英語版は [README.md](README.md) にあります。

## 必要なもの

- Go 1.24 以上
- make
- iproute2
- jq
- dnsmasq
- nftables
- conntrack
- IPv4/IPv6 の調査用に dig、ping、tcpdump、tracepath
- PPPoE を使う場合は pppd
- sqlite3 は状態データベースを人が調べる場合だけ任意で使う

Ubuntu の例:

```sh
sudo apt-get update
sudo apt-get install -y golang-go make iproute2 jq dnsmasq nftables conntrack ppp dnsutils iputils-ping iputils-tracepath tcpdump
```

conntrack は、複数 DS-Lite トンネルでのポリシールーティングや、フローごとのマーク状態を診断する用途でも使います。PPPoE のリソースは、ディストリビューションに含まれる pppd と rp-pppoe プラグインから動かします。`dig`、`ping`、`tcpdump`、`tracepath` は、ルータ自身から IPv4/IPv6 の到達性、DNS、経路 MTU、DHCPv6 のパケットを確認するための標準的な調査道具として扱います。SQLite は静的バイナリに組み込まれるため、実行時に `sqlite3` コマンドは不要です。`/var/lib/routerd/routerd.db` を手作業で確認したい場合だけ入れてください。

## ビルド

```sh
make build
```

または:

```sh
go build ./cmd/routerd
go build ./cmd/routerctl
```

成果物は bin/routerd と bin/routerctl に作られます。

ビルドに必要な依存が手元に揃っているかを確認するには:

```sh
make check-build-deps
```

YAML 入力スキーマを Go の型から再生成するには次の手順を踏みます。制御 API の JSON Schema と OpenAPI 定義もまとめて生成します。

```sh
make generate-schema
make check-schema
```

## インストール

手元のソースから入れる場合:

```sh
sudo make install
```

インストール処理はファイル配置に絞って単純にしてあります。あとから ports や dpkg などのパッケージ方式で同じ配置を包めるようにするためです。配置先は `PREFIX`、`DESTDIR`、`SYSCONFDIR`、`PLUGINDIR`、`RUNDIR`、`STATEDIR`、`SYSTEMDUNITDIR` の各変数で上書きできます。

一時ディレクトリへ配置を作る例:

```sh
make install DESTDIR=/tmp/routerd-root
```

インストール一式を tar アーカイブにまとめる場合:

```sh
make dist
```

Go や make がないリモートのテストホストへ入れる場合:

```sh
make remote-install REMOTE_HOST=user@router.example
```

Linux の開発機から FreeBSD のテストホストへ入れる場合は、FreeBSD 向け
バイナリを明示して作ります:

```sh
make remote-install ROUTERD_OS=freebsd REMOTE_HOST=user@router.example
```

リモート側の必須コマンドが揃っているかを確認するには:

```sh
make check-remote-deps REMOTE_HOST=user@router.example CONFIG=examples/router-lab.yaml
```

`CONFIG` を渡すと、その設定で使うリソースに合わせて任意コマンドを確認します。
たとえば `PPPoEInterface` があるときだけ `pppd` を求め、Linux で
`client: dhcp6c` を明示したときだけ `wide-dhcpv6-client` を求めます。

Ubuntu では、現在のソースインストールは `systemd`、`iproute2`、
`dnsmasq`、`nftables`、`conntrack`、`jq` などのホスト側コマンドを
前提にします。`IPv6PrefixDelegation` で `client: dhcp6c` を使う場合は
`wide-dhcpv6-client`、`PPPoEInterface` を使う場合は `pppd` も必要です。
`sqlite3` は状態確認用の任意コマンドです。FreeBSD ではまだ下地段階ですが、基本のネットワーク
コマンドに加えて、`dnsmasq` と `dhcp6` パッケージに含まれる
`dnsmasq`、`dhcp6c`、状態確認で使う `jq` が必要です。

設定ファイルだけをリモートホストに置きたい場合:

```sh
make remote-install-config REMOTE_HOST=user@router.example CONFIG=path/to/router.yaml
```

サービスマネージャ連携を入れる場合、Linux なら systemd ユニット、FreeBSD なら rc.d スクリプトを `make install-service` が自動選択します:

```sh
sudo make install-service
```

OS ごとに直接指定することもできます:

```sh
sudo make install-systemd      # Linux (Ubuntu, NixOS, ...)
sudo make install-rc-freebsd   # FreeBSD rc.d
```

NixOS では `make install` ではなく、リポジトリ直下の flake と `contrib/nix/` 配下の NixOS モジュールを使うことを推奨します。詳細は [contrib/nix/README.md](contrib/nix/README.md) を参照してください。

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
routerd apply --config examples/router-lab.yaml --once --dry-run
routerd serve --config examples/router-lab.yaml --socket /run/routerd/routerd.sock
routerctl status
routerctl get ipv6pd
routerctl describe ipv6pd/wan-pd
routerctl show ipv6pd
routerctl show interface/wan -o yaml
routerctl show ipv4sourcenat/lan-to-wan --diff
routerctl plan
```

`routerctl get <種別>` は `router.yaml` だけを読み、望むリソース定義を表示します。`routerctl get --list-kinds` で設定済みの種類を一覧でき、`-o json` と `-o yaml` で構造化出力できます。`routerctl describe <種別>/<名前>` は調査用の人間向け表示です。観測状態、最近のイベント、所有しているホスト側構成物、最後に触れた反映世代をまとめて表示します。`routerctl show` は引き続き全部入りの表示で、`--diff`、`--ledger`、`--adopt`、`--events`、`--spec`、`--status` を使えます。よく使う略称は `if`、`pd`、`ipv6pd`、`nat`、`dslite`、`pppoe`、`fw`、`zone`、`hostname`、`route` です。NAPT やコネクション追跡の情報は `IPv4SourceNAT` の観測状態として表示するため、独立した `show napt` コマンドはありません。

実機への反映:

```sh
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

`routerd apply --once` は、routerd が管理対象として認識した netplan、systemd-networkd の追加設定、dnsmasq、nftables、sysctl、DS-Lite トンネル、ポリシールーティングを実機へ反映します。リモートのルータで実行する前に、必ず `plan` と `--dry-run` で挙動を確認してください。cloud-init や既存の netplan が掴んでいるインターフェースを不用意に奪わないよう、引き継ぎが必要な状態は計画上で「取り込み待ち」として表示します。

DHCPv6-PD のラボ検証では、`routerd apply --once --override-client <client>` と `--override-profile <profile>` で、その 1 回だけ全 `IPv6PrefixDelegation` のクライアントやプロファイルを上書きできます。YAML は書き換えません。既知の問題がある OS・クライアント・プロファイルの組み合わせは、検証失敗ではなく警告として表示します。

すでに実機にある設定を routerd の管理下に取り込む場合は、まず `routerd adopt --candidates` で候補を確認し、内容に問題がなければ `routerd adopt --apply` で実機の状態を変更せずローカル台帳にだけ所有関係を記録します。

## ドキュメント

- [リソース API v1alpha1](docs/api-v1alpha1.ja.md)
- [リソース所有と反映モデル](docs/resource-ownership.ja.md)
- [制御 API v1alpha1](docs/control-api-v1alpha1.ja.md)
- [プラグインプロトコル](docs/plugin-protocol.ja.md)
- [対応プラットフォーム](docs/platforms.ja.md)
- [はじめに（英語）](docs/tutorials/getting-started.md)
- [Nix / NixOS から始める（英語）](docs/tutorials/nixos-getting-started.md)
- [設計メモとロードマップ](docs/design-notes.ja.md)
- [NTT NGN/HGW の DHCPv6-PD 知識ベース（日本語サイト）](website/i18n/ja/docusaurus-plugin-content-docs/current/knowledge-base/ntt-ngn-pd-acquisition.md)
- [DHCPv6-PD クライアント対応表（日本語サイト）](website/i18n/ja/docusaurus-plugin-content-docs/current/knowledge-base/dhcpv6-pd-clients.md)
- [更新履歴（英語）](docs/releases/changelog.md)
- [英語 README](README.md)

公開サイトは website/ 配下にあり、Docusaurus でビルドします。英語版と日本語版の両方を Cloudflare Pages へ公開する構成です。Cloudflare Pages では、ルートディレクトリを `website`、ビルドコマンドを `npm ci && npm run build`、出力ディレクトリを `build` に設定し、独自ドメインとして `routerd.net` を割り当てます。

## 既定パス

- 設定ファイル: /usr/local/etc/routerd/router.yaml
- プラグインディレクトリ: /usr/local/libexec/routerd/plugins
- 本体コマンド: /usr/local/sbin/routerd
- クライアントコマンド: /usr/local/sbin/routerctl

Linux での実行時パス:

- 実行時ディレクトリ: /run/routerd
- 状態保存ディレクトリ: /var/lib/routerd
- 状態ファイル: /run/routerd/status.json
- 制御ソケット: /run/routerd/routerd.sock
- ロックファイル: /run/routerd/routerd.lock

FreeBSD での実行時パス:

- 実行時ディレクトリ: /var/run/routerd
- 状態保存ディレクトリ: /var/db/routerd
- rc.d スクリプト: /usr/local/etc/rc.d/routerd
