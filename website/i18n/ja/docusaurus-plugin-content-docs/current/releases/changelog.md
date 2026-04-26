---
title: 更新履歴
---

# 更新履歴

routerd は現在プレリリースのソフトウェアです。この変更履歴では、リソースモデルが形になっていく過程の意味のある変更を記録します。

## 未リリース

- リソース所有と既存設定取り込みの基礎を追加。現在あるすべてのリソース種別が
  artifact intent を出し、ローカル台帳、読み取り専用の取り込み候補表示、
  管理対象の routing/nftables artifact の orphan 表示を持つように変更。
- `routerd adopt --apply` と、dry-run ではない reconcile 成功後の台帳更新を追加。
- 台帳で所有が分かる DS-Lite tunnel、routerd nftables table、routerd systemd
  service の orphan cleanup を追加。
- IPv6 RA MTU 広告と nftables TCP MSS clamp のために `PathMTUPolicy` を追加。
- `firewall.routerd.net/v1alpha1` に最小ファイアウォールリソースとして `Zone`、`FirewallPolicy`、`ExposeService` を追加。
- `HealthCheck.spec.role` を追加し、リンク、次ホップ、インターネット、サービス、ポリシーのヘルスチェックの意味を区別できるように変更。
- `routerd.net` 用の Docusaurus ドキュメントサイトを追加。
- `routerd.net` 向け Cloudflare Pages 前提の Docusaurus website を追加。
- 日本語ドキュメントを追加。
- 静的な `systemd-timesyncd` 設定用の `NTPClient` を追加。
- dnsmasq の `listenInterfaces` 許可リストを追加。
- dnsmasq の DNS 待ち受けアドレスをルーター自身のアドレスに絞るように変更。
- `LogSink` によるリモート syslog 設定を追加。
- `IPv4DefaultRoutePolicy` が有効候補として `IPv4PolicyRouteSet` を参照できるように変更。
- PPPoE インターフェース出力と systemd ユニット管理を追加。

## 0.1.0 計画時点の基準

- インターフェース、静的 IPv4、DHCP の下書き実装、プラグイン、予行実行、JSON 状態出力、systemd サービス配置の初期リソースモデル。
