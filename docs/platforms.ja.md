# 対応プラットフォーム

routerd は現時点で1つのプラットフォームを完全対応として扱い、追加の2
プラットフォーム向けに下地を整備しています。コードが提供していない
互換性を運用者が想定しないよう、サポート状況は明示します。

## Tier 1 — Ubuntu (および Debian 系 Linux)

- `/usr/local` 配下のソースインストール構成。
- `contrib/systemd/routerd.service` の systemd ユニット。
- CI でビルド・テスト済み。現在実装されているリソース種別（インター
  フェース別名、IPv4 静的/DHCP、dnsmasq による DHCP/DHCPv6/RA、
  systemd-networkd ドロップインによる IPv6 PD、条件付き DNS 転送、
  PPPoE、DS-Lite、nftables による IPv4 ソース NAT、IPv4 ポリシー
  ルーティング、ヘルスチェック付き IPv4 デフォルトルートポリシー、
  リバースパスフィルタ、MTU 伝搬、最小デフォルト拒否のホームルーター
  ファイアウォール、sysctl、ホスト名、systemd-timesyncd、ログ出力先）
  をエンドツーエンドで動作させます。

## Tier 2 — NixOS（下地）

- リポジトリ直下の flake が `buildGoModule` で同じ Go バイナリを
  ビルドし、`contrib/nix/` の NixOS モジュールを通じて systemd ユニット
  グラフに routerd を組み込み、開発用シェルも提供します。
- NixOS モジュールは netplan に依存しません。現時点の NixOS 構成では、
  外部管理のインターフェースを観測するか、ホスト上にある Linux の基本
  機能を使う形を推奨します。Ubuntu 用の netplan レンダラは残しますが、
  NixOS の実行時依存にはしません。
- NixOS の永続設定は、デーモンが `/etc` 配下の設定ファイルを書き換える
  のではなく、Nix として生成して `nixos-rebuild switch` で反映します。
  流れは `routerd render nixos` で `routerd-generated.nix` を生成し、
  薄い `configuration.nix` から import し、その後 `routerd serve` が
  非永続の実行時判断を扱う形です。手書き側の例は
  `examples/nixos-router02-configuration.nix` にあります。
- 最初の NixOS レンダラはホスト設定、依存パッケージ、基本的な
  systemd-networkd `.network` 宣言を生成します。より多くのリソース種別
  には、まだ Nix ネイティブな永続レンダリングが必要です。

## Tier 2 — FreeBSD（下地）

- `pkg/platform` が FreeBSD 用のデフォルトと機能フラグを宣言してい
  ます。`GOOS=freebsd` でのクロスコンパイルは成功します。
- `contrib/freebsd/routerd` の rc.d スクリプトを `make
  install-rc-freebsd` でインストールできます（`uname -s` が `FreeBSD`
  のときは `make install-service` から自動選択されます）。
- FreeBSD 向け未実装:
  - nftables の代替となる pf レンダラ（ソース NAT、ファイアウォール）。
  - netplan / systemd-networkd の代替となる rc.conf / `ifconfig`
    ベースのインターフェースレンダラ。
  - pppd / rp-pppoe の代替となる mpd5 もしくは FreeBSD ネイティブの
    PPPoE 連携。
  - `systemctl` の代替となる `service` 経由の dnsmasq オーケスト
    レーション。
  - systemd-networkd の代替となる `rtsold` / `rtadvd` による IPv6
    プレフィックス委譲。

これらが揃うまで、FreeBSD 上の `routerd reconcile` は validate、plan、
dry-run までは動作しますが、Linux 固有のホスト連携に依存するリソース
種別では適用を拒否するか no-op になります。FreeBSD では当面
`routerd validate` と `routerd plan --dry-run` で構成を検証してください。

## プラットフォームの選択方法

`pkg/platform` は Go の build tag によりコンパイル時に OS 固有の
デフォルトを解決します。レンダラと reconciler は `runtime.GOOS` で
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
