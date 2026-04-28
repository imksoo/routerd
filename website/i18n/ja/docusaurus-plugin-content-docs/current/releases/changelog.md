---
title: 更新履歴
---

# 更新履歴

routerd は現在プレリリースのソフトウェアです。リソースモデルが形になっていく過程で、振る舞いとして意味のある変更を記録していきます。

## 未リリース

- CLI と制御 API の動詞を `apply` に揃えました。`routerd reconcile`、
  `routerctl reconcile`、制御 API の反映操作は、それぞれ `routerd apply`、
  `routerctl apply`、`/apply` に変わりました。YAML の `spec.reconcile`
  という設定名はそのままです。
- `routerctl get` と `routerctl describe` を追加し、`routerctl` を kubectl に近い動詞で分けました。`get` は望む設定、`describe` は人が読むための状態・イベント・所有台帳、`show` は従来通りの全部入り表示です。
- SQLite の保存形式を、Kubernetes の考え方に近い世代、オブジェクト、所有台帳、
  イベントの形へ作り直しました。反映処理の世代とイベントを通常の記録として
  扱い、直前の二表だけの SQLite スキーマからは自動で移行します。
- `routerctl show` を整理。`routerctl show <種別>` と `routerctl show <種別>/<名前>` で、リソース定義、実機状態、所有台帳、routerd の状態履歴をまとめて見られるようになりました。表、JSON、YAML、差分、台帳のみ、取り込み候補のみの表示に対応しています。NAPT やコネクション追跡の情報は `IPv4SourceNAT` の観測状態に移しました。
- DHCPv6-PD の状態記録を整理。プレフィックスや識別子の個別キーを、構造化された `ipv6PrefixDelegation.<name>.lease` に移すようになりました。
- FreeBSD/KAME `dhcp6c` の DUID ファイルを NTT 系プロファイルで管理するようになりました。実効 DUID 型が `link-layer` の場合、DUID-LL 以外のファイルは退避し、MAC アドレスから作った DUID-LL を書き込みます。
- 記録上まだ有効な DHCPv6-PD リースがローカルで見えなくなった場合、反映時に OS 側クライアントへ一度だけ更新を促すようになりました。
- FreeBSD へのリモート導入を改善。Linux 作業端末から実行しても、`ROUTERD_OS=freebsd` で FreeBSD 向けバイナリをビルドし、FreeBSD の実行時ディレクトリを使うようになりました。
- リモート依存確認で `jq`、FreeBSD の `dhcp6c`、`mpd5`、`sysrc` を確認するようになりました。
- FreeBSD の DHCPv6-PD 出力を、パッケージ版の KAME `dhcp6c` が受け付ける構文に変更。
- FreeBSD の PPPoE 出力で `mpd5` の設定を生成し、管理対象の `PPPoEInterface` セッションがある場合は `mpd5` の rc.d サービスを起動できるようになりました。
- `IPv6PrefixDelegation.spec.convergenceTimeout` を追加。DHCPv6-PD が収束している間、直前まで見えていた委譲プレフィックスを短時間維持します。NTT 系プロファイルでは既定値を 5 分にしています。
- FreeBSD の反映処理で、下流側の `ifconfig` 出力から委譲プレフィックスを観測し、`IPv6DelegatedAddress` の安定アドレスを追加するようになりました。`dhcp6c` は設定変更時または停止時だけ再起動します。
- FreeBSD の反映処理で、保存済みのプレフィックス委譲リースから LAN 側の
  `IPv6DelegatedAddress` を導出できるようになりました。dnsmasq は
  `routerd_dnsmasq` rc.d サービスで管理し、IPv6 既定経路がない場合は
  `rtsol` で上流 RA の取得を促します。
- FreeBSD の `dhcp6c` を `-n` 付きで起動し、必要な再起動では SIGUSR1 で止めてから起動し直すようにしました。不要な DHCPv6 Release を避けるためです。
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
