---
title: フェデレーションイベントを RemoteAddressClaim に変換する
---

# フェデレーションイベントを RemoteAddressClaim に変換する

![フェデレーションされたクライアント観測イベントが EventSubscription にマッチし、プラグインを実行し、RemoteAddressClaim の provenance を持つ DynamicConfigPart を生成する流れ](/img/diagrams/how-to-event-federation-subscription.png)

CloudEdge Event Federation（ADR 0006）は、ある routerd ノードが観測したファクトに対して別のノードが宣言的に反応できる仕組みです。Phase 3 は受信側のループを閉じます: 受信したイベントが `EventSubscription` にマッチし、信頼されたローカルプラグインが実行され、その出力が `routerctl dynamic render` で確認できる `DynamicConfigPart` になります。

このガイドでは、同梱されているプロバイダー非依存のサンプルプラグイン `event-to-remote-claim` を使用します。

## フロー

```
on-prem routerd                         cloud routerd
---------------                         -------------
LAN クライアントを観測
  -> フェデレーションイベント発行 --push-->  イベント受信 (EventGroup)
     routerd.client.ipv4.observed         |
                                          v
                                   EventSubscription マッチ
                                          |
                                          v
                                   Plugin 実行 (event-to-remote-claim)
                                          |
                                          v
                                   PluginResult -> DynamicConfigPart
                                          |
                                          v
                                   routerctl dynamic render
                                     RemoteAddressClaim が表示される
```

1. **発行** — on-prem ノードがクライアントを観測し、共有 `EventGroup` に `routerd.client.ipv4.observed` イベントを発行します。
2. **トランスポート（Phase 2）** — イベントはオーバーレイ経由でクラウドノードの `EventGroup` 受信側にプッシュされます。
3. **マッチ** — クラウドノードの `EventSubscription` が、タイプ（およびオプションで subject プレフィックス / ソースノード）でイベントにマッチします。
4. **プラグイン** — サブスクリプションの `trigger.pluginRef` Plugin がマッチしたイベントを stdin で受け取って実行され、`PluginResult` を返します。
5. **DynamicConfigPart** — routerctl が結果を検証し、来歴アノテーション（`routerd.net/event-id`、`routerd.net/event-group`、`routerd.net/dynamic-source`）が付与された動的設定パーツとして保存します。
6. **レンダー** — `routerctl dynamic render` で、新しい `RemoteAddressClaim` を含む実効設定が表示されます。

## サンプルリソース

- 受信側（クラウド）の配線: [`examples/event-federation/receiver-cloud.yaml`](https://github.com/imksoo/routerd/blob/main/examples/event-federation/receiver-cloud.yaml) — `EventGroup`、`EventSubscription`、`Plugin`、および結果の `RemoteAddressClaim` が解決されるハイブリッドコンテキスト（`OverlayPeer`、`AddressMobilityDomain`、`CloudProviderProfile`）。
- 送信側（on-prem）の配線: [`examples/event-federation/sender-onprem.yaml`](https://github.com/imksoo/routerd/blob/main/examples/event-federation/sender-onprem.yaml) — `EventGroup` + `EventPeer` プッシュターゲット。
- サンプルプラグイン: [`examples/plugins/event-to-remote-claim/`](https://github.com/imksoo/routerd/tree/main/examples/plugins/event-to-remote-claim)。

## 試してみる

サンプルプラグインをビルドしてインストールします:

```sh
go build -o bin/event-to-remote-claim ./examples/plugins/event-to-remote-claim
install -D bin/event-to-remote-claim \
  /usr/local/libexec/routerd/plugins/event-to-remote-claim/bin/event-to-remote-claim
```

受信側の設定を適用し、テストイベントを発行します（通常は Phase 2 が on-prem ノードからイベントを配送します）:

```sh
routerctl federation event emit \
  --state-file /var/lib/routerd/routerd.db \
  --group cloudedge --type routerd.client.ipv4.observed \
  --subject 10.88.60.9/32 --source-node onprem-router \
  --payload address=10.88.60.9/32 \
  --payload domain=cloudedge-same-subnet \
  --payload ownerSide=onprem \
  --payload peerRef=onprem-main \
  --payload providerRef=example-provider \
  --ttl 30m
```

EventSubscription コントローラーが reconcile した後、実効設定をレンダーします:

```sh
routerctl dynamic render \
  --config /usr/local/etc/routerd/router.yaml \
  --state-file /var/lib/routerd/routerd.db
```

`10.88.60.9/32` の `RemoteAddressClaim` がイベントの来歴アノテーション付きで表示されます。

## スコープと安全性

- サンプルプラグインは**プロバイダー非依存**であり、**クラウドへの変更を行いません**。`capture` ブロックは dry-run 意図のプレースホルダーです（`configureOSAddress: false`）。
- アドレスを実際にクレームするためのプロバイダーオペレーション（`actionPlan`）の実行は **Phase 4/5** であり、MVP ではアクションプランは実行しません。
- routerd は設定や秘密をプラグインに渡しません -- 観測されたイベントのみです。
- `EventSubscription.match.types` は必須であるため、サブスクリプションがグループ内のすべてのイベントでプラグインを無差別にトリガーすることはできません。ループを防ぐためには `subjectPrefixes` と `sourceNodes` でさらに絞り込んでください。
