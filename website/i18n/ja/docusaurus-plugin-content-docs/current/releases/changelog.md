---
title: 更新履歴
---

# 更新履歴

routerd は現在 pre-release software です。この changelog では resource model が形になっていく過程の意味のある変更を記録します。

## Unreleased

- `routerd.net` 用の Docusaurus documentation site scaffold を追加。
- `routerd.net` 向け Cloudflare Pages deployment document を追加。
- 日本語 documentation locale を追加。
- static `systemd-timesyncd` 設定用の `NTPClient` を追加。
- dnsmasq の `listenInterfaces` allow-list を追加。
- dnsmasq の DNS bind address を router self address に絞るように変更。
- `LogSink` による remote syslog 設定を追加。
- `IPv4DefaultRoutePolicy` が active candidate として `IPv4PolicyRouteSet` を参照できるように変更。
- PPPoE interface render と systemd unit management を追加。

## 0.1.0 Planning Baseline

- interface、static IPv4、DHCP stub、plugin、dry-run、status JSON、systemd service layout の初期 resource model。
