# 対応プラットフォーム

routerd は現時点で 1 つのプラットフォームを完全対応として扱い、追加の 2
プラットフォーム向けは実装途上です。コードでサポートしていない範囲を
運用者が誤解しないよう、サポート状況は明示します。

## Tier 1 — Ubuntu (および Debian 系 Linux)

- `/usr/local` 配下のソースから入れる構成。
- Makefile によるビルドは既定で `CGO_ENABLED=0` を使い、ソース
  インストールやリモートインストール用 tarball には静的リンクされた
  Go バイナリを入れます。これにより、最小構成のルーターホストや
  NixOS で動的ローダの差に引っかかることを避けます。
- `contrib/systemd/routerd.service` の systemd ユニット。
- 実行時の依存には、制御に使う `iproute2`、`jq`、`dnsmasq`、
  `nftables`、`conntrack`、`IPv6PrefixDelegation` で `client: dhcp6c`
  を使う場合の `wide-dhcpv6-client`、PPPoE 利用時の `ppp` に加えて、標準的な
  調査道具として `dnsutils` の `dig`、`iputils-ping` の `ping`、
  `iputils-tracepath` の `tracepath`、`tcpdump` を含めます。
- ファイアウォール生成では、WAN 側で受ける DHCPv6 クライアント応答を
  UDP 宛先ポート 546 だけで許可します。送信元ポート 547 は要求しません。
  一部のホームゲートウェイがエフェメラルポートから応答するためです。
- CI でビルド・テスト済み。現在実装されているリソース種別（インター
  フェース別名、IPv4 静的/DHCP、dnsmasq による DHCP/DHCPv6/RA、
  systemd-networkd ドロップインまたは routerd 管理の `dhcp6c` による IPv6 PD、条件付き DNS 転送、
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
  `examples/nixos-edge-configuration.nix` にあります。
- 現在の NixOS レンダラはホスト設定、依存パッケージ、永続 sysctl、
  基本的な systemd-networkd の `.network` 宣言を生成します。IPv6-PD で
  `client: dhcp6c` を使う場合は `wide-dhcpv6` も含められます。DNS、パケット、
  経路 MTU をその場で確認できるよう、`dnsutils`、`iputils`、`tcpdump`、
  `traceroute` も生成パッケージに含めます。残りのリソース種別については、
  引き続き Nix らしい永続設定の生成を実装していく予定です。
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
  dhcp6c.conf を出力します。実行時の反映では、この範囲を
  `sysrc`、`service netif`、`service dhcp6c`、routerd が管理する
  dnsmasq の rc.d サービスで適用できます。
- FreeBSD ホストでは、基本のネットワークコマンドに加えて、`jq`、
  `dnsmasq`、`dhcp6`、`bind-tools`、`mpd5` パッケージが必要です。
  `dhcp6` パッケージには DHCPv6-PD に使う `dhcp6c` コマンドと rc.d
  サービスが含まれます。`bind-tools` は `dig` のために使います。
  `ping`、`ping6`、`tcpdump`、`traceroute`、`netstat` は FreeBSD
  base にある前提です。
- FreeBSD の DHCPv6-PD レンダラは、`dhcp6c` で委譲プレフィックスを
  設定します。パッケージ版の KAME `dhcp6c` は、下流インターフェースの
  識別子を自身で決めます。routerd はそのアドレスから委譲プレフィックスを
  観測し、プレフィックスが見えている場合に `IPv6DelegatedAddress` の
  安定したサフィックスを二つ目のアドレスとして追加します。routerd は、
  過去に見えた委譲プレフィックスだけを根拠に LAN 側アドレスを設定しません。
- FreeBSD の dnsmasq 管理リソースは
  `/usr/local/etc/rc.d/routerd_dnsmasq` で反映します。生成設定は、
  リースファイルと pid ファイルに `/var/run/routerd` 配下を使います。
- IPv6 転送が有効なルーターでも上流 RA の既定経路を受けられるよう、
  FreeBSD の反映処理は `IPv6DHCPAddress` のある上流に対して
  `net.inet6.ip6.rfc6204w3=1` を有効にし、IPv6 既定経路がない場合は
  `rtsol` を実行します。
- FreeBSD の反映処理では、生成した設定ファイルまたは該当する rc.conf
  値が変わった場合、もしくは `dhcp6c` が動いていない場合にだけ
  `dhcp6c` を再起動します。routerd は DHCPv6 Release の制御を設定として
  持たず、その挙動はパッケージ版の `dhcp6c` サービスに任せます。
- FreeBSD の反映確認は、必ず `routerd apply --once` の経路で行います。
  レンダラの変更を `dhcp6c` へ直接シグナル送信して確認すると、rc.d の
  状態確認、pid ファイル処理、起動時の診断を迂回してしまいます。調査中に
  DHCPv6-PD クライアントを手動で再起動する場合も、パケットを取得しながら
  `service dhcp6c stop` と `service dhcp6c start` を使います。
- `spec.reconcile.protectedInterfaces` に列挙したインターフェースは、
  FreeBSD の反映中に `service netif restart <ifname>` の対象にしません。
  routerd は rc.conf の値を更新することはありますが、稼働中の管理経路は
  その場では落としません。データプレーンの反映で操作経路を失わないためです。
- FreeBSD の PPPoE は `mpd5` 向けに出力します。`PPPoEInterface`
  リソースは `/usr/local/etc/mpd5/mpd.conf` に反映され、`managed: true`
  のセッションがある場合は `mpd5` の rc.d サービスを起動します。回線側に
  PPPoE セッション数の上限がある場合は、実際に接続する 1 台だけを
  `managed: true` にしてください。
- FreeBSD 向け未実装:
  - nftables の代替となる pf レンダラ（ソース NAT、ファイアウォール）。
  - `rtadvd` によるルータ広告の制御。

将来 FreeBSD 向けの pf レンダラを追加するときも、DHCPv6 クライアント
応答は同じ方針にします。WAN 側の UDP 宛先ポート 546 を許可し、送信元
ポートが 547 であることは条件にしません。

これらが揃うまで、FreeBSD 上の `routerd apply` が反映できるのは、
上記の対応済みホスト設定、実行時 sysctl、ホスト名、LAN 側の委譲 IPv6
アドレス、管理対象 dnsmasq です。Linux 固有のホスト連携に依存する
リソース種別は、今後のプラットフォーム別レンダラで対応します。

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
