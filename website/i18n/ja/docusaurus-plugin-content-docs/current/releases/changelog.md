---
title: 更新履歴
---

# 更新履歴

routerd は現在プレリリースのソフトウェアです。リソースモデルが形になっていく過程で、振る舞いとして意味のある変更を記録していきます。

## 未リリース

- FreeBSD へのリモート導入を改善。Linux 作業端末から実行しても、`ROUTERD_OS=freebsd` で FreeBSD 向けバイナリをビルドし、FreeBSD の実行時ディレクトリを使うようになりました。
- リモート依存確認で `jq`、FreeBSD の `dhcp6c`、`mpd5`、`sysrc` を確認するようになりました。
- FreeBSD の DHCPv6-PD 出力を、パッケージ版の KAME `dhcp6c` が受け付ける構文に変更。
- FreeBSD の PPPoE 出力で `mpd5` の設定を生成し、管理対象の `PPPoEInterface` セッションがある場合は `mpd5` の rc.d サービスを起動できるようになりました。
- リソース所有と取り込みの基礎を導入。すべてのリソース種別が管理意図を出すようになり、ローカル所有台帳がホスト側の構成物を記録、`routerd adopt --candidates` が読み取り専用で取り込み候補を表示、反映時には経路と nftables 関連の管理対象構成物の残置候補を報告するようになりました。
- `routerd adopt --apply` を追加。ホストの状態を変更せず、一致した取り込み候補を台帳に記録します。dry-run でない反映が成功した場合も、台帳が自動で更新されます。
- 台帳で所有が確認できる DS-Lite トンネル、routerd の nftables テーブル、routerd の systemd サービスについて、残置物のクリーンアップを追加。
- `PathMTUPolicy` を追加。実効 MTU を IPv6 RA で広告し、転送 TCP の MSS を nftables でクランプします。
- `firewall.routerd.net/v1alpha1` に最小ファイアウォール用の `Zone`、`FirewallPolicy`、`ExposeService` を追加。
- `HealthCheck.spec.role` を追加。リンク、次ホップ、インターネット、サービス、ポリシー集約のいずれを見ているチェックなのかを区別できるようになりました。
- routerd.net 用の Docusaurus サイトの土台を追加。Cloudflare Pages の英日構成を前提にしています。
- `NTPClient` を追加。固定サーバを使う `systemd-timesyncd` 構成を管理します。
- dnsmasq の DHCP / DNS について、`listenInterfaces` の許可リストを必須化し、DNS の待ち受けアドレスをルータ自身のアドレスに絞るよう変更。
- `LogSink` でリモート syslog 送出に対応。
- `IPv4DefaultRoutePolicy` の候補が `IPv4PolicyRouteSet` を参照できるようになり、健全な宛先のコネクション追跡マークを保ったまま動かせるようになりました。
- PPPoE インターフェースの構成出力と、routerd 管理の systemd ユニットを追加。

## 0.1.0 計画時点の基準

- インターフェース、IPv4 静的アドレス、DHCP の最小実装、プラグイン、予行実行、JSON 状態出力、systemd サービスの配置を含む初期リソースモデル。
