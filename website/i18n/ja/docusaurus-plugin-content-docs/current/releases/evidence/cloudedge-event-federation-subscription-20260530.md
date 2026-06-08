# CloudEdge Event Federation Phase 3 subscription スモーク

Result: PASS

日付: 2026-05-30
ブランチ/ビルド: event-federation / 515fe7e8d086
ビルドコマンド: `make dist`

エビデンスバンドル:
`/home/imksoo/routerd-labs/event-federation/evidence/20260530T111612Z-phase3-subscription-515fe7e8`

## トポロジー

スモークは Phase 2 と同じ PVE のみのペアを使用しました。クラウド VM は起動していません。

- 送信側: router03 / 192.168.123.125 / `router03.lain.local`
- 受信側: router05 / 192.168.123.127 / `router05.lain.local`
- EventGroup: `cloudedge-event-smoke`
- 送信側 EventGroup nodeName: `onprem-event-node`
- 受信側 EventGroup nodeName: `cloud-event-node`
- 受信側 listen: `169.254.250.5:9443`
- 送信側 EventPeer endpoint: `http://169.254.250.5:9443`
- AddressMobilityDomain: `cloudedge-same-subnet`
- Plugin 実行ファイルパス: `/usr/local/libexec/routerd/plugins/event-to-remote-claim/bin/event-to-remote-claim`

送信側と受信側の設定は以下から適用:

- `examples/event-federation/sender-onprem.yaml`
- `examples/event-federation/receiver-cloud.yaml`

受信側の plugin パスに stdin をログ出力した後、ビルド済みのサンプルプラグインバイナリを `event-to-remote-claim.real` として実行するラッパーを設置しました。これにより plugin の出力を変更せずに `PluginRequest.spec.events` のエビデンスを取得できました。

## デプロイ

- `515fe7e8` の `make dist` が完了。
- `routerd`、`routerctl`、`routerd-eventd` を両ノードにデプロイ。
- サンプルプラグインは `CGO_ENABLED=0 GOOS=linux` で別途ビルドし、router05 にインストール。
- 生成された両方の設定が `routerd check` をパス。
- 両ノードで `routerd-eventd@cloudedge-event-smoke.service` がアクティブ。
- 受信側の `ss` で `169.254.250.5:9443` のリスナーを確認。
- オーバーレイの到達性が双方向でパス:
  - router03 -> `169.254.250.5`: 3/3 ping、0% loss
  - router05 -> `169.254.250.3`: 3/3 ping、0% loss

## メインアサーション

イベント:

- ID: `evt-phase3-smoke-20260530T112250Z`
- Type: `routerd.client.ipv4.observed`
- Subject: `10.88.60.9/32`
- SourceNode: `onprem-event-node`
- Payload domain: `cloudedge-same-subnet`
- Payload ownerSide: `onprem`

結果:

- 送信側から `cloud-event-node` への配信: `delivered`、attempts `1`
- 受信側の federation ストアに同一イベント ID が存在
- EventSubscription 実行:
  - subscription: `EventSubscription/cloud-claims`
  - plugin: `event-to-remote-claim`
  - status: `succeeded`
  - attempts: `1`
  - dynamic source: `EventSubscription/cloud-claims/07634fdff9b3235c`
- Plugin 実行:
  - trigger type: `federation-subscription`
  - trigger topic: `cloud-claims`
  - exit code: `0`
  - status: `succeeded`
- `routerctl dynamic list -o json` でアクティブな DynamicConfigPart が 1 つ表示。
- `routerctl dynamic render -o yaml` の表示:
  - kind: `RemoteAddressClaim`
  - name: `onprem-10-88-60-9`
  - address: `10.88.60.9/32`
  - domainRef: `cloudedge-same-subnet`
  - ownerSide: `onprem`
  - capture.type: `provider-secondary-ip`
  - capture.providerRef: `example-provider`
  - capture.nicRef: `example-nic-ref`
  - delivery.peerRef: `onprem-main`
  - delivery.tunnelInterface: `wg-hybrid`

レンダリングされた claim にはプロヴェナンスアノテーションが付与:

- `routerd.net/dynamic-source: EventSubscription/cloud-claims`
- `routerd.net/event-group: cloudedge-event-smoke`
- `routerd.net/event-id: evt-phase3-smoke-20260530T112250Z`
- `routerd.net/event-subject: 10.88.60.9/32`

キャプチャされた PluginRequest には `spec.events` 配下に同一の ID、subject、source node、payload を持つメインイベントが含まれていました。

## ネガティブチェック

重複冪等性: PASS

- `evt-phase3-smoke-20260530T112250Z` を再 emit しても新たな subscription 実行は発生しなかった。
- メインイベントの配信は attempts `1` のまま。
- DynamicConfigPart の数は `1` のまま。
- レンダリングされた `RemoteAddressClaim/onprem-10-88-60-9` の数は `1` のまま。
- Plugin リクエストログは成功リクエスト 1 件のまま。

非マッチイベント: PASS

- イベント ID: `evt-phase3-nonmatch-20260530T112250Z`
- ownerSide: `cloud`
- トランスポート配信: `delivered`
- 受信側がイベントを格納。
- subscription 実行は作成されなかった。
- `10.88.60.10/32` の DynamicConfigPart やレンダリングされた claim はなし。

期限切れイベント: PASS

- イベント ID: `evt-phase3-expired-20260530T112250Z`
- ObservedAt: `2026-05-30T11:14:07Z`
- ExpiresAt: `2026-05-30T11:14:08Z`
- 送信側配信クエリ: `null`
- 受信側は期限切れイベントを受信しなかった。
- `10.88.60.11/32` の subscription 実行やレンダリングされた claim はなし。

Plugin 失敗リトライ上限: PASS

- イベント ID: `evt-phase3-pluginfail-20260530T112250Z`
- ラボ専用の `EventSubscription/cloud-claims-fail` にマッチ。
- 失敗 plugin が exit code `42` で終了。
- `event_subscription_runs` は `status=failed`、`attempts=3` で終了。
- 失敗した plugin 実行の行が 3 件記録。
- `10.88.60.66/32` の DynamicConfigPart は作成されなかった。

## スコープチェック

- プロバイダーアクションは実行されていない。
- クラウドリソースの作成、起動、停止、変更はなし。
- Phase 4 の actionPlan 実行は試行されていない。
- SAM データプレーン apply は実行されていない; RemoteAddressClaim は `routerctl dynamic render` 内にのみ存在。
- ARP observer、プロバイダー固有の plugin、DynamicConfigPart consumer パスは使用されていない。

## 判定

Phase 3 コントロールプレーン自動化がパス:

manual emit -> transport -> EventSubscription match -> plugin.Run ->
DynamicConfigPart -> `routerctl dynamic render` RemoteAddressClaim。

Phase 4 は未着手。

## Pre-flight ノート

Pre-flight はスモークが実行に入った後に要求されました。メインパスがパスし、生成された PluginResult/DynamicConfigPart により以下が遡及的に確認されました:

- payload domain が `AddressMobilityDomain.metadata.name` (`cloudedge-same-subnet`) と一致
- plugin 実行ファイルが存在し呼び出された (`event-to-remote-claim`、exit 0)
- 受信側の hybrid コンテキストが完全 (レンダリングされた `RemoteAddressClaim` が受信側設定に対して
  `domainRef` / `delivery.peerRef` / `capture.providerRef` を解決し、`dynamic render` バリデーションをパス)
- プロバイダーミューテーションは試行されていない

つまり pre-flight はスキップされていません — メインパス PASS が設定/コンテキストの正しさを証明しました。
