# 対応プラットフォーム

routerd は現時点で1つのプラットフォームを完全対応として扱い、追加の2
プラットフォーム向けに下地を整備しています。コードが提供していない
互換性を運用者が想定しないよう、サポート状況は明示します。

## Tier 1 — Ubuntu (および Debian 系 Linux)

- `/usr/local` 配下のソースインストール構成。
- Makefile によるビルドは既定で `CGO_ENABLED=0` を使い、ソース
  インストールやリモートインストール用 tarball には静的リンクされた
  Go バイナリを入れます。これにより、最小構成のルーターホストや
  NixOS で動的ローダの差に引っかかることを避けます。
- `contrib/systemd/routerd.service` の systemd ユニット。
- ファイアウォール生成では、WAN 側で受ける DHCPv6 クライアント応答を
  UDP 宛先ポート 546 だけで許可します。送信元ポート 547 は要求しません。
  一部のホームゲートウェイがエフェメラルポートから応答するためです。
- CI でビルド・テスト済み。現在実装されているリソース種別（インター
  フェース別名、IPv4 静的/DHCP、dnsmasq による DHCP/DHCPv6/RA、
  systemd-networkd ドロップインによる IPv6 PD、条件付き DNS 転送、
  PPPoE、DS-Lite、nftables による IPv4 ソース NAT、IPv4 ポリシー
  ルーティング、ヘルスチェック付き IPv4 デフォルトルートポリシー、
  リバースパスフィルタ、MTU 伝搬、最小デフォルト拒否のホームルーター
  ファイアウォール、sysctl、ホスト名、systemd-timesyncd、ログ出力先）
  をエンドツーエンドで動作させます。

## Tier 2 — NixOS（下地）

- リポジトリ直下の flake が `buildGoModule` で Ubuntu と同じ Go
  バイナリをビルドし、`contrib/nix/` の NixOS モジュールが routerd を
  systemd のユニットグラフに組み込みます。同じ flake から開発用の
  シェルも利用できます。
- NixOS モジュールは netplan に依存しません。現時点の NixOS 構成では、
  外部で管理されているインターフェースを routerd から観測するか、
  Linux ホストに既に備わっている機能を使う構成を推奨します。
  Ubuntu 用の netplan レンダラは引き続き利用できますが、NixOS の
  実行時依存には含めません。
- NixOS では、`/etc` 配下の設定ファイルをデーモンが書き換えるのでは
  なく、Nix の式として永続設定を生成し、`nixos-rebuild switch` で
  反映します。具体的な流れは、`routerd render nixos` で
  `routerd-generated.nix` を生成し、最小限の `configuration.nix` から
  これを import して `nixos-rebuild switch` を実行し、起動後は
  `routerd serve` が非永続な実行時判断（ヘルスチェックや経路選択など）
  を担う、というものです。手書き側の最小例は
  `examples/nixos-router02-configuration.nix` にあります。
- 現在の NixOS レンダラはホスト設定、依存パッケージ、永続 sysctl、
  基本的な systemd-networkd の `.network` 宣言を生成します。残りの
  リソース種別については、引き続き Nix らしい永続設定の生成を実装して
  いく予定です。
- ルータとして使う NixOS ホストでは、生成される NixOS 設定で組み込みの
  逆方向経路検査を無効にします。そのうえで routerd のファイアウォールが
  他の Linux と同じく、WAN 側の UDP 宛先ポート 546 を送信元ポートに
  依存せず許可します。

## Tier 2 — FreeBSD（下地）

- `pkg/platform` が FreeBSD 用のデフォルトと機能フラグを宣言してい
  ます。`GOOS=freebsd` でのクロスコンパイルは成功します。
- `contrib/freebsd/routerd` の rc.d スクリプトを `make
  install-rc-freebsd` でインストールできます（`uname -s` が `FreeBSD`
  のときは `make install-service` から自動選択されます）。
- Linux の開発機から FreeBSD のテストホスト向けにビルドするときは、
  `make build`、`make dist`、`make remote-install` に
  `ROUTERD_OS=freebsd` を指定します。これにより、インストールされる
  バイナリが FreeBSD 向けになります。
- `routerd render freebsd` は rc.conf に入れる値、dhclient.conf、
  dhcp6c.conf を出力します。実行時の反映では、この限定された範囲を
  `sysrc`、`service netif`、`service dhcp6c` で適用できます。
- FreeBSD ホストでは、基本のネットワークコマンドに加えて、`jq`、
  `dnsmasq`、`dhcp6`、`mpd5` パッケージが必要です。`dhcp6` パッケージには
  DHCPv6-PD の検証で使う `dhcp6c` コマンドと rc.d サービスが含まれます。
- FreeBSD の DHCPv6-PD レンダラは、`dhcp6c` で委譲プレフィックスを
  設定します。ただし、現時点では `IPv6DelegatedAddress.spec.addressSuffix`
  の指定を反映しません。FreeBSD の `dhcp6` パッケージに含まれる KAME
  `dhcp6c` は、この箇所で `sla-id` と `sla-len` は受け付けますが、
  Linux 側と同じようなインターフェース識別子の指定を受け付けないためです。
- FreeBSD の PPPoE は `mpd5` 向けに出力します。`PPPoEInterface`
  リソースは `/usr/local/etc/mpd5/mpd.conf` に反映され、`managed: true`
  のセッションがある場合は `mpd5` の rc.d サービスを起動します。回線側に
  PPPoE セッション数の上限がある場合は、実際に接続する 1 台だけを
  `managed: true` にしてください。
- FreeBSD 向け未実装:
  - nftables の代替となる pf レンダラ（ソース NAT、ファイアウォール）。
  - `service` 経由の dnsmasq 制御。
  - `rtadvd` によるルータ広告の制御。

将来 FreeBSD 向けの pf レンダラを追加するときも、DHCPv6 クライアント
応答は同じ方針にします。WAN 側の UDP 宛先ポート 546 を許可し、送信元
ポートが 547 であることは条件にしません。

これらが揃うまで、FreeBSD 上の `routerd reconcile` が反映できるのは、
上記の対応済みホスト設定、実行時 sysctl、ホスト名に限られます。Linux
固有のホスト連携に依存するリソース種別は、今後のプラットフォーム別
レンダラで対応します。

## プラットフォームの選択方法

`pkg/platform` は Go の build tag によりコンパイル時に OS 固有の
デフォルトを解決します。レンダラと反映処理は `runtime.GOOS` で
はなく `platform.Current()` を参照する想定です。OS 固有の挙動を追加
するときは次の3箇所に分けて入れます。

1. `Defaults` と `Features` を返す build tag 付きの
   `platform_<os>.go`。
2. `platform.Features.HasX` で分岐するレンダラ（依存ライブラリが
   そもそもコンパイルできない場合は build tag）。
3. サービスマネージャ連携を置く `contrib/<os>/` ディレクトリ。

## 当面のスコープ外

- Windows と macOS。
- プロプライエタリな組み込みルーター製品。
- コンテナでの実行。routerd はホストのネットワーク状態を直接
  変更する性質上、ルーター本体で動作することを前提としています。
