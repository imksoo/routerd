---
title: 更新履歴
---

# 更新履歴

routerd は現在プレリリースのソフトウェアです。この変更履歴では、リソースモデルが形になっていく過程の意味のある変更を記録します。

## 未リリース

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
