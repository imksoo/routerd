---
title: 更新履歴
---

# 更新履歴

routerd は現在プレリリースのソフトウェアです。リソースモデルが形になっていく過程で、振る舞いとして意味のある変更を記録していきます。

## 未リリース

- 破壊的変更: 利用者向けの動詞を `apply` に揃えました。古い
  `reconcile` CLI と制御 API は `routerd apply`、`routerctl apply`、
  `/apply` に置き換わりました。YAML の `spec.reconcile` はそのままです。
- 破壊的変更: 開発中に追加した DHCPv6-PD 回避用の設定を削除しました。
  DHCPv6 の Renew/Rebind と Release は OS 側クライアントに任せます。
- FreeBSD の NTT 系プロファイルでは、KAME `dhcp6c` を `-n` 付きで起動します。
  サービス再起動時に DHCPv6 Release を送らず、Renew/Rebind の時刻管理は
  `dhcp6c` に任せます。
- Linux の `IPv6PrefixDelegation` で `client: dhcp6c` を使えるようにしました。
  routerd が WIDE/KAME 形式の `dhcp6c.conf` と systemd ユニットを生成し、
  NTT ホームゲートウェイ向け構成で IA Prefix 寿命が 0 の Renew/Rebind を避けられるようにします。
- `IPv6PrefixDelegation` に `serverID`、`priorPrefix`、`acquisitionStrategy` を
  追加しました。DHCPv6 の能動制御では、明示された設定値を優先し、無い場合は
  観測済みのリース状態を使います。
- `routerd dhcp6 request|renew|release --resource <名前>` を追加しました。
  Request/Renew は送信ごとに新しい transaction ID を作り、T1/T2 と IA Prefix の寿命を
  0 にせず、Reconfigure Accept を含めます。Release は IA_PD の寿命を 0 にし、
  Reconfigure Accept を含めません。
- FreeBSD の反映処理は、`dhcp6c_flags="-n"` が変わっていない場合に毎回書き直さないようにしました。
  不要な `dhcp6c` 再起動を避け、DHCPv6 クライアントが持つリース状態を守ります。
- `routerctl` に `get`、`describe`、`show` を整理しました。`show` は
  望む設定、実機状態、所有台帳、状態履歴、イベントをまとめて表示し、
  NAPT やコネクション追跡は `IPv4SourceNAT` の観測状態として扱います。
- 状態と所有台帳を SQLite に移し、世代、オブジェクト、構成物、イベント、
  将来用のアクセスログを持つ形にしました。`routerctl describe inventory/host`
  で OS、カーネル、仮想化、サービス管理方式、DMI、コマンドの有無を確認できます。
- DHCPv6-PD の状態は `ipv6PrefixDelegation.<name>.lease` にまとめました。
  NTT 系プロファイルは実 MAC 由来の DUID-LL を既定にし、正確なプレフィックス
  ヒントは既定では出しません。`duidRawData` と `iaid` は移行や冗長構成のための
  明示的な上書きとして残します。
- `IPv6RAAddress` は WAN 側の RA/SLAAC を DHCPv6-PD と分けて扱います。
  DS-Lite の AFTR 名を解決するときに、上流側 IPv6 アドレスと RA の既定経路を使えるようにします。
- ルーター診断用のホストツールを依存関係として整理しました。Linux の遠隔確認では
  `dig`、`ping`、`tcpdump`、`tracepath` を使い、FreeBSD では `dig` と base の
  `ping`、`ping6`、`tcpdump`、`traceroute` を使います。
- dnsmasq の条件付き転送で、IPv6 の上流 DNS アドレスを dnsmasq の
  `server=/domain/addr` 形式で出力するようにしました。
- 反映処理は、可能な場合に状態保存領域の現在の委譲プレフィックスから
  LAN 側 IPv6 アドレスと DS-Lite の送信元アドレスを導出します。委譲
  プレフィックスが変わった後は、管理対象の末尾アドレスを持つ古い
  routerd 派生 IPv6 アドレスを削除します。
- FreeBSD の土台を整理しました。DHCPv6-PD は KAME `dhcp6c`、IPv4 DHCP は
  設定されたクライアント、PPPoE は `mpd5`、LAN サービスは rc.d 管理の
  dnsmasq を使います。リモート導入では FreeBSD 向けバイナリを作り、必要な
  ツールを確認します。
- リソース所有と取り込みの基礎を導入しました。各リソースが管理意図を出し、
  ローカル台帳が所有構成物を記録し、`routerd adopt --candidates` と
  `routerd adopt --apply` で取り込み候補を扱えます。反映時には既知の残置物も
  報告または整理します。
- DS-Lite、PPPoE、IPv4 送信元 NAT、IPv4 既定経路ポリシー、経路集合、
  MTU/MSS 方針、逆方向経路フィルタ、ヘルスチェック種別、最小ファイアウォール、
  NTP、ログ送信、dnsmasq による DHCP/DNS など、ルーターとして必要な主要リソースを
  追加しました。
- NixOS 出力の土台として、ホスト設定、systemd-networkd、パッケージ、
  永続 sysctl、ルーター向けの逆方向経路フィルタ緩和、任意の
  `routerd.service` を出力できるようにしました。
- routerd.net 向けの Docusaurus サイトを追加し、英語と日本語のドキュメントを
  公開できる構成にしました。

## 0.1.0 計画時点の基準

- インターフェース、IPv4 静的アドレス、DHCP の最小実装、プラグイン、予行実行、JSON 状態出力、systemd サービスの配置を含む初期リソースモデル。
