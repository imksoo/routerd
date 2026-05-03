---
title: リソースモデル
slug: /concepts/resource-model
sidebar_position: 3
---

# リソースモデル

routerd の設定は、最上位の `Router` と、その中に並ぶリソースで構成します。
各リソースは Kubernetes に近い形を持ちます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DHCPv6PrefixDelegation
metadata:
  name: wan-pd
spec:
  interface: wan
```

## 共通フィールド

- `apiVersion`: リソースが属する API グループと版です。
- `kind`: リソースの種類です。
- `metadata.name`: 同じ `kind` の中で一意な名前です。
- `spec`: 利用者が宣言する意図です。
- `status`: routerd や専用デーモンが観測した状態です。

設定ファイルでは主に `spec` を書きます。
`status` は制御 API、状態データベース、デーモンの `/v1/status` で確認します。

## API グループ

routerd は次の API グループを使います。

| グループ | 用途 |
| --- | --- |
| `routerd.net/v1alpha1` | 最上位の `Router` |
| `net.routerd.net/v1alpha1` | インターフェース、DHCP、DNS、経路、トンネル、WAN 選択 |
| `firewall.routerd.net/v1alpha1` | ファイアウォール方針の土台 |
| `system.routerd.net/v1alpha1` | ホスト名、sysctl、NTP、NixOS 連携 |
| `plugin.routerd.net/v1alpha1` | 信頼済みローカルプラグイン |

`routerd.io` のような仮のグループは使いません。

## 依存関係

リソースは他のリソースを名前で参照します。
たとえば `IPv6DelegatedAddress` は `DHCPv6PrefixDelegation` を参照し、`DSLiteTunnel` は `DHCPv6Information` や `DNSResolverUpstream` の結果を参照します。

依存元がまだ準備できていない場合、リソースは `Pending` になります。
準備できると `Applied`、`Bound`、`Up`、`Installed`、`Healthy` などの段階に進みます。

## ready_when

一部のリソースは `ready_when` で適用条件を指定できます。
Phase 2-A では `any_of` による OR 条件も使えるようになりました。
たとえば DS-Lite は、DHCPv6 情報要求から AFTR が得られる場合と、静的に指定した AFTR FQDN を解決できる場合のどちらでも準備完了にできます。

## 所有参照

`ownerRefs` は、あるリソースが別のリソースに従属することを表します。
親が準備できない場合、子は古くなった構成を出し続けません。
DHCPv6-PD が失われたときに、古い LAN IPv6 設定を残さないための重要な仕組みです。
